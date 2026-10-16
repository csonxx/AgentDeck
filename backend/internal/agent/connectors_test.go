package agent

import (
	"agentdeck/internal/store"
	"context"
	"encoding/json"
	acp "github.com/coder/acp-go-sdk"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func connectorManager(t *testing.T) *Manager {
	t.Helper()
	db, e := store.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{Store: db, Normalizer: &Normalizer{Store: db}, Roots: []string{t.TempDir()}, Context: ctx}
	t.Cleanup(func() { cancel(); m.Close(); db.Close() })
	return m
}
func awaitConnector(t *testing.T, m *Manager, id, kind, text, status string) store.Event {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		evs, _ := m.Store.Events(id, 0, 500)
		for _, ev := range evs {
			if ev.Kind == kind && (text == "" || ev.Text == text) && (status == "" || ev.Status == status) {
				return ev
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("missing %s / %s event", kind, status)
	return store.Event{}
}
func TestConnectionsConfiguration(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agents.json")
	for _, v := range []string{`{"opencode":{"url":"http://example.com"}}`, `{"unknown":true}`, `{} {}`, `{"opencode":{"url":"https://user:secret@example.com"}}`} {
		os.WriteFile(p, []byte(v), 0600)
		if _, e := LoadConnections(p); e == nil {
			t.Fatal("unsafe config accepted")
		}
	}
	os.WriteFile(p, []byte(`{"opencode":{"url":"http://127.0.0.1:5000"}}`), 0600)
	c, e := LoadConnections(p)
	if e != nil {
		t.Fatal(e)
	}
	m := connectorManager(t)
	m.Configure(c)
	if len(m.Catalog()) != 6 {
		t.Fatal("missing agents")
	}
	os.Chmod(p, 0644)
	if _, e = LoadConnections(p); e == nil {
		t.Fatal("public config accepted")
	}
	os.Chmod(p, 0600)
	link := p + "-link"
	os.Symlink(p, link)
	if _, e = LoadConnections(link); e == nil {
		t.Fatal("symlink config accepted")
	}
}
func TestOpenCodeNativeRoutingAndEvents(t *testing.T) {
	m := connectorManager(t)
	project := m.Projects()[0].Path
	var prompts, approvals atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("directory") != project {
			t.Error("missing original project")
		}
		switch r.URL.Path {
		case "/session":
			var p map[string]any
			if json.NewDecoder(r.Body).Decode(&p) != nil || len(p) != 1 || p["title"] != "新会话" {
				t.Error("session creation overrode provider policy")
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "native", "title": "Original", "directory": project})
		case "/session/status":
			w.Write([]byte(`{}`))
		case "/session/native/prompt_async":
			var p map[string]any
			json.NewDecoder(r.Body).Decode(&p)
			if !strings.HasPrefix(str(p["messageID"]), "msg_") {
				t.Error("missing message ID")
			}
			if len(p) != 2 || p["parts"] == nil {
				t.Error("prompt overrode provider permissions or tools")
			}
			prompts.Add(1)
			w.WriteHeader(204)
		case "/permission":
			w.Write([]byte(`[{"id":"owned","sessionID":"native"},{"id":"other","sessionID":"foreign"}]`))
		case "/permission/owned/reply":
			var p map[string]any
			json.NewDecoder(r.Body).Decode(&p)
			if p["reply"] != "once" {
				t.Error("permanent approval")
			}
			approvals.Add(1)
			w.Write([]byte(`true`))
		case "/session/native/message/msg":
			w.Write([]byte(`{"info":{"role":"assistant"}}`))
		default:
			t.Errorf("unexpected provider route %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	o := newOpenCode(m, HTTPAgentConfig{URL: api.URL})
	m.Connectors = map[string]Connector{"opencode": o}
	ss, e := m.Create("opencode", m.Projects()[0].ID, "")
	if e != nil {
		t.Fatal(e)
	}
	if ss.ID != "opencode/native" || ss.Source != "opencode-api" {
		t.Fatal("wrong native identity")
	}
	if e = m.Send(ss.ID, "synthetic", "request"); e != nil {
		t.Fatal(e)
	}
	if e = m.Approve(ss.ID, "other", "accept", ""); e == nil {
		t.Fatal("foreign approval accepted")
	}
	if e = m.Approve(ss.ID, "owned", "accept", ""); e != nil {
		t.Fatal(e)
	}
	o.event(m.Context, map[string]any{"type": "message.part.updated", "properties": map[string]any{"part": map[string]any{"id": "thought", "sessionID": "native", "messageID": "msg", "type": "reasoning", "text": "thinking"}}})
	awaitConnector(t, m, ss.ID, "thinking", "thinking", "")
	o.event(m.Context, map[string]any{"type": "session.status", "properties": map[string]any{"sessionID": "native", "status": map[string]any{"type": "idle"}}})
	awaitConnector(t, m, ss.ID, "status", "", "completed")
	if prompts.Load() != 1 || approvals.Load() != 1 {
		t.Fatal("wrong provider operation count")
	}
}
func TestACPNativeResumeApprovalAndStop(t *testing.T) {
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
			if ss.ID != name+"/native-fixture" {
				t.Fatal("identity replaced")
			}
			if e = m.Send(ss.ID, "normal", "one"); e != nil {
				t.Fatal(e)
			}
			awaitConnector(t, m, ss.ID, "message", "native reply", "")
			awaitConnector(t, m, ss.ID, "thinking", "reasoned step", "")
			tool := awaitConnector(t, m, ss.ID, "tool", "", "completed")
			if !strings.Contains(tool.Text, "Read fixture") || !strings.Contains(tool.Text, "read result") {
				t.Fatal("tool update lost original content")
			}
			image := awaitConnector(t, m, ss.ID, "image", "", "")
			if image.Media == "" {
				t.Fatal("image not stored")
			}
			awaitConnector(t, m, ss.ID, "status", "", "completed")
			if e = m.Send(ss.ID, "approval", "two"); e != nil {
				t.Fatal(e)
			}
			approval := awaitConnector(t, m, ss.ID, "approval", "", "pending")
			if e = m.Approve(ss.ID, "foreign", "accept", ""); e == nil {
				t.Fatal("foreign approval accepted")
			}
			if e = m.Approve(ss.ID, approval.Item, "accept", ""); e != nil {
				t.Fatal(e)
			}
			awaitConnector(t, m, ss.ID, "message", "once", "")
			deadline := time.Now().Add(time.Second)
			for {
				c.mu.Lock()
				run := c.runs[ss.ID]
				run.mu.Lock()
				busy := run.busy
				run.mu.Unlock()
				c.mu.Unlock()
				if !busy {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("approval turn stuck")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if e = m.Send(ss.ID, "pause", "three"); e != nil {
				t.Fatal(e)
			}
			awaitConnector(t, m, ss.ID, "thinking", "waiting for cancel", "")
			if e = m.Send(ss.ID, "overlap", "four"); e == nil {
				t.Fatal("overlapping turn accepted")
			}
			if e = m.Interrupt(ss.ID); e != nil {
				t.Fatal(e)
			}
			awaitConnector(t, m, ss.ID, "status", "", "interrupted")
			c.Close()
			time.Sleep(30 * time.Millisecond)
			if _, e = c.get(m.Context, ss); e != nil {
				t.Fatal(e)
			}
			if e = m.Send(ss.ID, "normal", "five"); e != nil {
				t.Fatal(e)
			}
			run, _ := c.get(m.Context, ss)
			outside := filepath.Join(t.TempDir(), "secret")
			os.WriteFile(outside, []byte("secret"), 0600)
			if _, e = run.ReadTextFile(m.Context, acp.ReadTextFileRequest{SessionId: "native-fixture", Path: outside}); e == nil {
				t.Fatal("outside project read accepted")
			}
			awaitConnector(t, m, ss.ID, "message", "native reply", "")
		})
	}
}
func TestWorkBuddyOAuthSyncRefreshAndAccountIsolation(t *testing.T) {
	m := connectorManager(t)
	t.Setenv("TEST_WB_SECRET", "fixture-client-secret")
	var grants, sends atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			r.ParseForm()
			if r.Form.Get("client_secret") != "fixture-client-secret" {
				t.Error("secret missing")
			}
			grants.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"access_token": "fixture-access-secret", "refresh_token": "fixture-refresh-secret", "open_id": "owner", "scope": "user.localassistant.readable user.localassistant.invokable", "expires_in": 3600})
		case "/localassistant":
			if r.Header.Get("Authorization") != "Bearer fixture-access-secret" {
				t.Error("wrong credential")
			}
			w.Write([]byte(`{"code":0,"data":{"online":true}}`))
		case "/localassistant/message":
			if r.Method == "POST" {
				var p map[string]any
				if json.NewDecoder(r.Body).Decode(&p) != nil || len(p) != 2 || p["content"] != "synthetic" || p["msg_type"] != "text" {
					t.Error("WorkBuddy input overrode native settings")
				}
				sends.Add(1)
				w.Write([]byte(`{"code":0,"data":{"message_id":"sent"}}`))
			} else {
				w.Write([]byte(`{"code":0,"data":{"messages":[{"message_id":"reply","role":"assistant","content":["WorkBuddy reply"],"created_at":"2026-10-09T01:00:00Z"}]}}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	w := newWorkBuddy(m, WorkBuddyConfig{API: api.URL, ClientID: "fixture", ClientSecretEnv: "TEST_WB_SECRET", RedirectURI: "https://agentdeck.example/v1/agents/workbuddy/callback"})
	m.Connectors = map[string]Connector{"workbuddy": w}
	flow, e := w.StartLogin()
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(str(flow["url"]))
	state := u.Query().Get("state")
	if e = w.CompleteLogin(m.Context, "foreign", "code"); e == nil {
		t.Fatal("foreign callback accepted")
	}
	if e = w.CompleteLogin(m.Context, state, "code"); e != nil {
		t.Fatal(e)
	}
	if e = w.CompleteLogin(m.Context, state, "code"); e == nil {
		t.Fatal("replayed callback accepted")
	}
	if e = w.sync(m.Context); e != nil {
		t.Fatal(e)
	}
	ss := workBuddySession(workBuddyToken{OpenID: "owner"})
	awaitConnector(t, m, ss.ID, "message", "WorkBuddy reply", "")
	foreign := workBuddySession(workBuddyToken{OpenID: "foreign"})
	if e = w.Send(m.Context, foreign, "synthetic", "r"); e == nil {
		t.Fatal("foreign account accepted")
	}
	if e = m.Send(ss.ID, "synthetic", "r"); e != nil {
		t.Fatal(e)
	}
	var token workBuddyToken
	m.Store.Get("agentconfig", "workbuddy/oauth", &token)
	token.Expires = 1
	m.Store.Put("agentconfig", "workbuddy/oauth", token)
	if _, e = w.access(m.Context); e != nil {
		t.Fatal(e)
	}
	if grants.Load() != 2 || sends.Load() != 1 {
		t.Fatal("wrong operation count")
	}
	if w.Info().Stop || w.Info().Create || w.Info().Approve {
		t.Fatal("unsupported operations advertised")
	}
}

func TestACPTerminalUsesNativePermissionDecisionAndBoundsOutput(t *testing.T) {
	m := connectorManager(t)
	fixture, _ := filepath.Abs("../../../scripts/fixture_acp.py")
	c := newACP(m, "qoder", ACPConfig{Command: "python3", Args: []string{fixture}})
	m.Connectors = map[string]Connector{"qoder": c}
	ss, e := m.Create("qoder", m.Projects()[0].ID, "")
	if e != nil {
		t.Fatal(e)
	}
	run, _ := c.get(m.Context, ss)
	limit := 10
	v, e := run.CreateTerminal(m.Context, acp.CreateTerminalRequest{SessionId: "native-fixture", Command: "python3", Args: []string{"-c", "print('a'*100)"}, OutputByteLimit: &limit})
	if e != nil {
		t.Fatal(e)
	}
	events, _ := m.Store.Events(ss.ID, 0, 100)
	for _, event := range events {
		if event.Kind == "approval" {
			t.Fatal("client fabricated a second terminal permission request")
		}
	}
	outside := t.TempDir()
	if _, e = run.CreateTerminal(m.Context, acp.CreateTerminalRequest{SessionId: "foreign", Command: "python3"}); e == nil {
		t.Fatal("foreign session terminal accepted")
	}
	if _, e = run.CreateTerminal(m.Context, acp.CreateTerminalRequest{SessionId: "native-fixture", Command: "python3", Cwd: &outside}); e == nil {
		t.Fatal("terminal directory escaped project")
	}
	ctx, cancel := context.WithTimeout(m.Context, 5*time.Second)
	defer cancel()
	if _, e = run.WaitForTerminalExit(ctx, acp.WaitForTerminalExitRequest{SessionId: "native-fixture", TerminalId: v.TerminalId}); e != nil {
		t.Fatal(e)
	}
	output, e := run.TerminalOutput(ctx, acp.TerminalOutputRequest{SessionId: "native-fixture", TerminalId: v.TerminalId})
	if e != nil {
		t.Fatal(e)
	}
	if !output.Truncated || len(output.Output) > limit || output.ExitStatus == nil {
		t.Fatal("terminal output limit/exit status missing")
	}
	if _, e = run.ReleaseTerminal(ctx, acp.ReleaseTerminalRequest{SessionId: "native-fixture", TerminalId: v.TerminalId}); e != nil {
		t.Fatal(e)
	}
	if _, e = run.TerminalOutput(ctx, acp.TerminalOutputRequest{SessionId: "native-fixture", TerminalId: v.TerminalId}); e == nil {
		t.Fatal("released terminal retained")
	}
}

func TestRealOpenCodeOriginalSessionAndContext(t *testing.T) {
	endpoint := os.Getenv("AGENTDECK_REAL_OPENCODE_URL")
	if endpoint == "" {
		t.Skip("explicit real-provider acceptance only")
	}
	m := connectorManager(t)
	o := newOpenCode(m, HTTPAgentConfig{URL: endpoint})
	m.Connectors = map[string]Connector{"opencode": o}
	go o.Run(m.Context)
	ss, e := m.Create("opencode", m.Projects()[0].ID, "")
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if e = o.request(context.Background(), "DELETE", "/session/"+url.PathEscape(nativeID(ss)), ss.Project, nil, nil); e != nil {
			t.Error("owned probe session cleanup failed")
		}
	}()
	prompts := []string{"Do not use any tools or change any files. Remember the word DECK_CONTEXT_040. Reply with only DECK_CONTEXT_040.", "Without tools, what word did I ask you to remember? Reply with only that word."}
	for _, prompt := range prompts {
		after := m.Store.LastSeq()
		if e = m.Send(ss.ID, prompt, UUID()); e != nil {
			t.Fatal(e)
		}
		deadline := time.Now().Add(120 * time.Second)
		found, complete := false, false
		for time.Now().Before(deadline) {
			events, _ := m.Store.Events(ss.ID, after, 500)
			for _, ev := range events {
				if ev.Kind == "message" && ev.Role == "assistant" && strings.Contains(ev.Text, "DECK_CONTEXT_040") {
					found = true
				}
				if ev.Kind == "status" && ev.Status == "completed" {
					complete = true
				}
				if ev.Kind == "status" && ev.Status == "failed" {
					t.Fatal("real provider failed")
				}
			}
			if found && complete {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !found || !complete {
			t.Fatal("real reply or completion missing")
		}
	}
	original, e := o.Create(m.Context, ss.Project, ss.ID)
	if e != nil || original.ID != ss.ID {
		t.Fatal("native continuation changed identity")
	}
	if e = m.Send(ss.ID, "Without tools or file changes, write the integers from 1 through 1000, one per line.", UUID()); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(20 * time.Second)
	busy := false
	for time.Now().Before(deadline) {
		var states map[string]any
		if e = o.request(m.Context, "GET", "/session/status", ss.Project, nil, &states); e != nil {
			t.Fatal(e)
		}
		if str(obj(states[nativeID(ss)])["type"]) == "busy" {
			busy = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !busy {
		t.Fatal("real provider never entered busy state")
	}
	if e = m.Send(ss.ID, "Do not accept this overlapping turn.", UUID()); e == nil {
		t.Fatal("real provider accepted an overlapping turn")
	}
	if e = m.Interrupt(ss.ID); e != nil {
		t.Fatal(e)
	}
	deadline = time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var states map[string]any
		if e = o.request(m.Context, "GET", "/session/status", ss.Project, nil, &states); e != nil {
			t.Fatal(e)
		}
		if state := str(obj(states[nativeID(ss)])["type"]); state != "busy" && state != "retry" {
			t.Log("actual provider interruption returned the original session to idle")
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("real provider remained busy after interrupt")
}
