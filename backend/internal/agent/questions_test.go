package agent

import (
	"agentdeck/internal/store"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureQuestion(id string, multiple bool) map[string]any {
	return map[string]any{"id": id, "question": "Choose " + id, "multiple": multiple, "options": []any{map[string]any{"label": "A", "description": "First"}, map[string]any{"label": "B", "description": "Second"}}}
}
func TestQuestionAnswerValidationAndNativeShapes(t *testing.T) {
	qs := []any{fixtureQuestion("one", false), fixtureQuestion("two", true)}
	a, e := parseQuestionAnswers(`{"one":"A","two":["A","B"]}`)
	if e != nil || validateQuestionAnswers(qs, a, "codex") != nil {
		t.Fatal("valid answers rejected")
	}
	if !reflect.DeepEqual(obj(obj(codexQuestionResponse(a)["answers"])["two"])["answers"], []string{"A", "B"}) {
		t.Fatal("array flattened")
	}
	if claudeQuestionResponse(a)["two"] != "A, B" {
		t.Fatal("Claude string contract lost")
	}
	for _, raw := range []string{`{"one":null}`, `{"one":42}`, `{"one":[42]}`, `{"one":{}}`, `null`, `not-json`} {
		if _, e := parseQuestionAnswers(raw); e == nil {
			t.Fatal("malformed answer accepted")
		}
	}
	for _, raw := range []string{`{"one":"A"}`, `{"one":["A","B"],"two":"B"}`, `{"one":"C","two":"A"}`, `{"one":"A","two":["A","A"]}`, `{"one":"A","two":"A","foreign":"B"}`} {
		a, e := parseQuestionAnswers(raw)
		if e == nil && validateQuestionAnswers(qs, a, "codex") == nil {
			t.Fatal("invalid answer accepted")
		}
	}
	obj(qs[0])["isOther"] = true
	a, _ = parseQuestionAnswers(`{"one":"Custom","two":["A","B"]}`)
	if validateQuestionAnswers(qs, a, "codex") != nil {
		t.Fatal("custom rejected")
	}
}
func TestOpenCodeQuestionOriginalSessionArraysExpiryAndReconnect(t *testing.T) {
	m := connectorManager(t)
	project := m.Roots[0]
	ss := newExternalSession("native", "opencode", project, "opencode-api")
	m.Store.Put("sessions", ss.ID, ss)
	foreign := newExternalSession("foreign", "opencode", project, "opencode-api")
	m.Store.Put("sessions", foreign.ID, foreign)
	var active atomic.Bool
	active.Store(true)
	var replies atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("directory") != project {
			t.Error("escaped project")
		}
		switch r.URL.Path {
		case "/question":
			if active.Load() {
				json.NewEncoder(w).Encode([]any{map[string]any{"id": "que_fixture", "sessionID": "native", "questions": []any{fixtureQuestion("ignored", false), fixtureQuestion("ignored", true)}}})
			} else {
				w.Write([]byte(`[]`))
			}
		case "/question/que_fixture/reply":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			if !reflect.DeepEqual(b["answers"], []any{[]any{"A"}, []any{"A", "B"}}) {
				t.Error("positional answers lost")
			}
			replies.Add(1)
			active.Store(false)
			w.Write([]byte(`true`))
		default:
			t.Error("unexpected mutation")
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	o := newOpenCode(m, HTTPAgentConfig{URL: api.URL})
	m.Connectors = map[string]Connector{"opencode": o}
	if e := o.syncQuestions(m.Context, project); e != nil {
		t.Fatal(e)
	}
	seq := m.Store.LastSeq()
	o.syncQuestions(m.Context, project)
	if m.Store.LastSeq() != seq {
		t.Fatal("duplicate question on reconnect")
	}
	if e := m.Approve(foreign.ID, "question/que_fixture", "accept", `{"0":"A","1":["A","B"]}`); e == nil {
		t.Fatal("foreign answered")
	}
	if e := m.Approve(ss.ID, "question/que_fixture", "accept", `{"0":"A"}`); e == nil {
		t.Fatal("incomplete accepted")
	}
	if e := m.Approve(ss.ID, "question/que_fixture", "accept", `{"0":"A","1":["A","B"]}`); e != nil {
		t.Fatal(e)
	}
	if e := m.Approve(ss.ID, "question/que_fixture", "accept", `{"0":"A","1":["A","B"]}`); e == nil || replies.Load() != 1 {
		t.Fatal("expired answer resent")
	}
	o.syncQuestions(m.Context, project)
	h, _ := m.Store.History(ss.ID, 0, 40)
	if len(h.Events) != 1 || h.Events[0].Status != "resolved" {
		t.Fatal("pending not resolved")
	}
}
func TestOpenCodeQuestionSSEStateClearsAfterOfflineReply(t *testing.T) {
	m := connectorManager(t)
	project := m.Roots[0]
	ss := newExternalSession("native", "opencode", project, "opencode-api")
	m.Store.Put("sessions", ss.ID, ss)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/question" {
			t.Error("unexpected route")
		}
		w.Write([]byte(`[]`))
	}))
	defer api.Close()
	o := newOpenCode(m, HTTPAgentConfig{URL: api.URL})
	o.event(m.Context, map[string]any{"type": "question.asked", "properties": map[string]any{"id": "que_sse", "sessionID": "native", "questions": []any{fixtureQuestion("choice", false)}}})
	if e := o.syncQuestions(m.Context, project); e != nil {
		t.Fatal(e)
	}
	h, _ := m.Store.History(ss.ID, 0, 40)
	if len(h.Events) != 1 || h.Events[0].Status != "resolved" || h.Events[0].Text == "" {
		t.Fatal("offline native reply left pending SSE card")
	}
}

func TestDesktopQuestionProjectionAndReconnect(t *testing.T) {
	m := connectorManager(t)
	ss := store.Session{ID: "original", Agent: "codex", Project: m.Roots[0], Source: "desktop"}
	m.Store.Put("sessions", ss.ID, ss)
	req := map[string]any{"id": float64(42), "method": "item/tool/requestUserInput", "params": map[string]any{"threadId": ss.ID, "questions": []any{fixtureQuestion("choice", false)}}}
	state := map[string]any{"id": ss.ID, "cwd": ss.Project, "requests": []any{req}, "PRIVATE_CONTEXT": "NEVER_SAVE"}
	p := &desktopProjection{m: m, session: ss, seen: map[string]string{}}
	if e := p.publish(state); e != nil {
		t.Fatal(e)
	}
	h, _ := m.Store.History(ss.ID, 0, 40)
	if len(h.Events) != 1 || h.Events[0].Item != desktopQuestionKey(float64(42)) || h.Events[0].Status != "pending" {
		t.Fatal("question not projected")
	}
	seq := m.Store.LastSeq()
	p = &desktopProjection{m: m, session: ss, seen: map[string]string{}}
	p.publish(state)
	if m.Store.LastSeq() != seq {
		t.Fatal("duplicated on reconnect")
	}
	delete(state, "requests")
	p.publish(state)
	if m.Store.LastSeq() != seq {
		t.Fatal("missing field resolved question")
	}
	state["requests"] = []any{}
	p.publish(state)
	h, _ = m.Store.History(ss.ID, 0, 40)
	if len(h.Events) != 1 || h.Events[0].Status != "resolved" || h.Events[0].Text == "" {
		t.Fatal("resolution lost question")
	}
}
func TestNativeQuestionsRecoverAfterLocalRestart(t *testing.T) {
	m := connectorManager(t)
	ss := store.Session{ID: "original", Agent: "codex", Project: m.Roots[0], Source: "desktop"}
	m.Store.Put("sessions", ss.ID, ss)
	req := map[string]any{"id": float64(42), "method": "item/tool/requestUserInput", "params": map[string]any{"threadId": ss.ID, "questions": []any{fixtureQuestion("choice", false)}}}
	state := map[string]any{"id": ss.ID, "cwd": ss.Project, "requests": []any{req}}
	p := &desktopProjection{m: m, session: ss, seen: map[string]string{}}
	p.publish(state)
	if e := m.Store.RecoverManaged(); e != nil {
		t.Fatal(e)
	}
	p = &desktopProjection{m: m, session: ss, seen: map[string]string{}}
	if e := p.publish(state); e != nil {
		t.Fatal(e)
	}
	ev, e := m.Store.TranscriptItem(ss.ID, desktopQuestionKey(float64(42)), "approval")
	if e != nil || ev.Status != "pending" {
		t.Fatal("active native question not restored")
	}
	os := newExternalSession("native", "opencode", ss.Project, "opencode-api")
	m.Store.Put("sessions", os.ID, os)
	o := newOpenCode(m, HTTPAgentConfig{})
	o.questionEvent(os, "que_fixture", `{"questions":[]}`, "pending")
	m.Store.RecoverManaged()
	o.questionEvent(os, "que_fixture", `{"questions":[]}`, "pending")
	ev, e = m.Store.TranscriptItem(os.ID, "question/que_fixture", "approval")
	if e != nil || ev.Status != "pending" {
		t.Fatal("active OpenCode question not restored")
	}
}

func TestDesktopQuestionReplyOriginalOwnerAndExpiry(t *testing.T) {
	for _, tc := range []struct {
		name    string
		id      any
		project string
		bad     bool
	}{{"numeric", float64(42), "/authorized", false}, {"string", "request-id", "/authorized", false}, {"expired", "missing", "/authorized", true}, {"project", float64(42), "/foreign", true}} {
		t.Run(tc.name, func(t *testing.T) {
			dir, e := os.MkdirTemp("/tmp", "ad-question-")
			if e != nil {
				t.Fatal(e)
			}
			defer os.RemoveAll(dir)
			os.Chmod(dir, 0700)
			path := filepath.Join(dir, "ipc.sock")
			l, e := net.Listen("unix", path)
			if e != nil {
				t.Fatal(e)
			}
			defer l.Close()
			os.Chmod(path, 0600)
			done := make(chan error, 1)
			var sent atomic.Int32
			go func() {
				c, e := l.Accept()
				if e != nil {
					done <- e
					return
				}
				defer c.Close()
				c.SetDeadline(time.Now().Add(5 * time.Second))
				for step := 0; step < 2; step++ {
					v, e := readDesktopFrame(c)
					if e != nil {
						done <- e
						return
					}
					r := map[string]any{}
					if step == 0 {
						r["clientId"] = "follower"
					}
					writeDesktopTest(c, map[string]any{"type": "response", "requestId": v["requestId"], "resultType": "success", "handledByClientId": "owner", "result": r})
				}
				_, e = readDesktopFrame(c)
				if e != nil {
					done <- e
					return
				}
				native := tc.id
				if tc.name == "expired" {
					native = float64(42)
				}
				state := map[string]any{"id": "original", "cwd": "/authorized", "requests": []any{map[string]any{"id": native, "method": "item/tool/requestUserInput", "params": map[string]any{"threadId": "original", "questions": []any{fixtureQuestion("choice", false)}}}}}
				writeDesktopTest(c, map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"hostId": "local", "conversationId": "original", "change": map[string]any{"type": "snapshot", "conversationState": state}}})
				v, e := readDesktopFrame(c)
				if e != nil {
					done <- e
					return
				}
				if v["type"] == "broadcast" {
					done <- nil
					return
				}
				sent.Add(1)
				p := obj(v["params"])
				if v["method"] != "thread-follower-submit-user-input" || v["version"] != float64(1) || v["targetClientId"] != "owner" || p["requestId"] != native || p["conversationId"] != "original" {
					t.Error("incorrect native route")
				}
				if !reflect.DeepEqual(arr(obj(obj(obj(p["response"])["answers"])["choice"])["answers"]), []any{"A"}) {
					t.Error("response lost")
				}
				writeDesktopTest(c, map[string]any{"type": "response", "requestId": v["requestId"], "resultType": "success", "handledByClientId": "owner", "result": map[string]any{"method": "thread-follower-submit-user-input", "result": map[string]any{"ok": true}}})
				_, e = readDesktopFrame(c)
				done <- e
			}()
			e = (&DesktopBridge{Socket: path}).AnswerQuestions(context.Background(), "original", tc.project, desktopQuestionKey(tc.id), "accept", `{"choice":"A"}`)
			if (e != nil) != tc.bad {
				t.Fatal("unexpected result", e)
			}
			if e = <-done; e != nil {
				t.Fatal(e)
			}
			if (sent.Load() == 0) != tc.bad {
				t.Fatal("expired request mutated")
			}
		})
	}
}

func TestACPExplicitNativePermissionChoice(t *testing.T) {
	for _, name := range []string{"qoder", "trae"} {
		t.Run(name, func(t *testing.T) {
			m := connectorManager(t)
			fixture, e := filepath.Abs("../../../scripts/fixture_acp.py")
			if e != nil {
				t.Fatal(e)
			}
			c := newACP(m, name, ACPConfig{Command: "python3", Args: []string{fixture}})
			m.Connectors = map[string]Connector{name: c}
			ss, e := m.Create(name, m.Projects()[0].ID, "")
			if e != nil {
				t.Fatal(e)
			}
			if e = m.Send(ss.ID, "approval", UUID()); e != nil {
				t.Fatal(e)
			}
			ev := awaitConnector(t, m, ss.ID, "approval", "", "pending")
			var p map[string]any
			json.Unmarshal([]byte(ev.Text), &p)
			if len(arr(p["permission_options"])) != 3 {
				t.Fatal("native permission choices lost")
			}
			if e = m.Approve(ss.ID, ev.Item, "accept", `{"permission_option":"not-offered"}`); e == nil {
				t.Fatal("unoffered choice accepted")
			}
			if e = m.Approve(ss.ID, ev.Item, "decline", `{"permission_option":"always"}`); e == nil {
				t.Fatal("mismatched decision accepted")
			}
			if e = m.Approve(ss.ID, ev.Item, "accept", `{"permission_option":"always"}`); e != nil {
				t.Fatal(e)
			}
			awaitConnector(t, m, ss.ID, "message", "always", "")
		})
	}
}
