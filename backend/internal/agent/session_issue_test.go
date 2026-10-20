package agent

import (
	"agentdeck/internal/store"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNativeSessionIssueCompatibility(t *testing.T) {
	cases := []struct {
		native, code string
		blocked      bool
	}{
		{"misalignmentPolicyViolation", "safety_ended", true}, {"contextWindowExceeded", "context_limit", false},
		{"usageLimitExceeded", "usage_limit", false}, {"unauthorized", "authentication", false},
		{"httpConnectionFailed", "network", false}, {"futurePrivateError", "execution_error", false},
	}
	for _, c := range cases {
		t.Run(c.native, func(t *testing.T) {
			state := desktopTestState("s", "/fixture", "Message", "failed")
			turn := obj(arr(state["turns"])[0])
			turn["error"] = map[string]any{"codexErrorInfo": c.native, "message": "PRIVATE_PROVIDER_CONTEXT", "misalignment": map[string]any{"detailedExplanation": "PRIVATE_FINDINGS"}}
			issue := desktopSessionIssue(state)
			if issue.Code != c.code || issue.BlocksInput != c.blocked {
				t.Fatalf("unexpected structured issue: %s", issue.Code)
			}
			b, _ := json.Marshal(issue)
			if strings.Contains(string(b), "PRIVATE") {
				t.Fatal("private provider state leaked")
			}
			e := checkSessionInput(store.Session{Issue: issue})
			var notSent *InputNotSentError
			if c.blocked && (!errors.As(e, &notSent)) || !c.blocked && e != nil {
				t.Fatal("wrong input policy")
			}
		})
	}
	state := desktopTestState("s", "/fixture", "", "failed")
	obj(arr(state["turns"])[0])["error"] = map[string]any{"codexErrorInfo": "misalignmentPolicyViolation", "misalignment": map[string]any{"detailedExplanation": "PRIVATE", "steer": map[string]any{"message": "PRIVATE"}}}
	if desktopSessionIssue(state).Code != "safety_review" {
		t.Fatal("reviewable stop not distinguished")
	}
	obj(arr(state["turns"])[0])["status"] = "futureStatus"
	if desktopTurnStatus(state) != "unknown" || desktopSessionIssue(state).Code != "unsupported_state" {
		t.Fatal("unknown state became ready")
	}
	obj(arr(state["turns"])[0])["status"] = "inProgress"
	state["requests"] = []any{map[string]any{"method": "future/request"}}
	if desktopSessionIssue(state).Code != "desktop_action" {
		t.Fatal("unsupported action silently ignored")
	}
	state["requests"] = []any{map[string]any{"method": "item/tool/requestUserInput"}}
	if desktopSessionIssue(state).Code != "" {
		t.Fatal("supported question blocked normal native controls")
	}
	if openCodeIssue(map[string]any{"name": "APIError", "data": map[string]any{"statusCode": float64(429)}}).Code != "usage_limit" {
		t.Fatal("OpenCode rate limit missing")
	}
}
func TestSessionIssuePersistsAndClearsOnlyOnAuthoritativeStatus(t *testing.T) {
	m := connectorManager(t)
	s := store.Session{ID: "issue", Agent: "codex", Project: m.Projects()[0].Path, Source: "desktop", Managed: true}
	m.Store.Put("sessions", s.ID, s)
	state := desktopTestState(s.ID, s.Project, "Public message", "failed")
	obj(arr(state["turns"])[0])["error"] = map[string]any{"codexErrorInfo": "misalignmentPolicyViolation", "message": "PRIVATE_ERROR"}
	p := &desktopProjection{m: m, session: s, seen: map[string]string{}}
	if err := p.publish(state); err != nil {
		t.Fatal(err)
	}
	var got store.Session
	got = store.Session{}
	m.Store.Get("sessions", s.ID, &got)
	if !got.Issue.BlocksInput {
		t.Fatal("terminal restriction lost during projection")
	}
	obj(arr(state["turns"])[0])["error"] = map[string]any{"codexErrorInfo": "misalignmentPolicyViolation", "misalignment": map[string]any{"detailedExplanation": "PRIVATE", "steer": map[string]any{"message": "PRIVATE"}}}
	if err := p.publish(state); err != nil {
		t.Fatal(err)
	}
	got = store.Session{}
	m.Store.Get("sessions", s.ID, &got)
	if got.Issue.Code != "safety_review" {
		t.Fatal("same-status issue change lost to deduplication")
	}
	if err := m.SendDelivery(s.ID, "must not send", "request", TurnOptions{}, nil, "queue"); err == nil {
		t.Fatal("terminal chat accepted queued input")
	}
	m.Store.PutCatalogSession(s)
	got = store.Session{}
	m.Store.Get("sessions", s.ID, &got)
	if !got.Issue.BlocksInput {
		t.Fatal("catalog replacement lost issue")
	}
	m.Store.Append(store.Event{ID: "old-log", Session: s.ID, Agent: "codex", Source: "log", Kind: "status", Status: "running"})
	got = store.Session{}
	m.Store.Get("sessions", s.ID, &got)
	if !got.Issue.BlocksInput {
		t.Fatal("log reopened terminal owner")
	}
	m.Store.Append(store.Event{ID: "old-tool", Session: s.ID, Agent: "codex", Source: "log", Kind: "tool_result", Status: "completed", Text: "Public tool output"})
	got = store.Session{}
	m.Store.Get("sessions", s.ID, &got)
	if got.Status != "failed" || !got.Issue.BlocksInput {
		t.Fatal("tool result reopened safety stop")
	}
	obj(arr(state["turns"])[0])["status"] = "completed"
	if err := p.publish(state); err != nil {
		t.Fatal(err)
	}
	got = store.Session{}
	m.Store.Get("sessions", s.ID, &got)
	if got.Issue.Code != "" {
		t.Fatal("fresh owner recovery did not clear issue")
	}
}

func TestNativeProtocolErrorsRetainSafeReasons(t *testing.T) {
	n := setup(t)
	fixtures := []struct{ agent, wire, code, status string }{
		{"codex", `{"method":"turn/completed","params":{"turn":{"status":"failed","error":{"codexErrorInfo":"misalignmentPolicyViolation","message":"PRIVATE"}}}}`, "safety_ended", "failed"},
		{"codex", `{"method":"error","params":{"error":{"codexErrorInfo":"usageLimitExceeded"},"willRetry":true}}`, "retrying", "running"},
		{"codex", `{"method":"error","params":{"error":{"codexErrorInfo":{"httpConnectionFailed":{"httpStatusCode":503}}},"willRetry":false}}`, "network", "failed"},
		{"claude", `{"type":"result","is_error":true,"subtype":"error_max_turns","result":"PRIVATE"}`, "output_limit", "failed"},
	}
	for _, f := range fixtures {
		events, e := n.Parse([]byte(f.wire), f.agent, "stream", &State{Session: "fixture"})
		if e != nil || len(events) == 0 {
			t.Fatal("missing native failure")
		}
		last := events[len(events)-1]
		if last.Issue.Code != f.code || last.Status != f.status {
			t.Fatalf("wrong native reason %s/%s", last.Issue.Code, last.Status)
		}
		raw, _ := json.Marshal(last.Issue)
		if strings.Contains(string(raw), "PRIVATE") {
			t.Fatal("raw error copied")
		}
	}
}

func TestOpenCodeFailureSurvivesIdleCleanupAndClearsOnNewTurn(t *testing.T) {
	m := connectorManager(t)
	s := newExternalSession("native-error", "opencode", m.Projects()[0].Path, "opencode-api")
	m.Store.Put("sessions", s.ID, s)
	o := &openCode{m: m}
	emit := func(kind string, fields map[string]any) {
		fields["sessionID"] = "native-error"
		o.event(m.Context, map[string]any{"type": kind, "properties": fields})
	}
	emit("session.error", map[string]any{"error": map[string]any{"name": "ProviderAuthError", "data": map[string]any{"message": "PRIVATE_TOKEN"}}})
	emit("session.status", map[string]any{"status": map[string]any{"type": "idle"}})
	var got store.Session
	m.Store.Get("sessions", s.ID, &got)
	if got.Status != "failed" || got.Issue.Code != "authentication" {
		t.Fatal("idle cleanup hid failure")
	}
	emit("session.status", map[string]any{"status": map[string]any{"type": "busy"}})
	got = store.Session{}
	m.Store.Get("sessions", s.ID, &got)
	if got.Status != "running" || got.Issue.Code != "" {
		t.Fatal("new turn did not clear old error")
	}
	emit("session.status", map[string]any{"status": map[string]any{"type": "future"}})
	got = store.Session{}
	m.Store.Get("sessions", s.ID, &got)
	if got.Status != "unknown" || got.Issue.Code != "unsupported_state" || got.Issue.BlocksInput {
		t.Fatal("unknown status became completed")
	}
}

func TestMissingDesktopObservationDoesNotPretendReadyOrEraseSafety(t *testing.T) {
	m := connectorManager(t)
	s := store.Session{ID: "observation", Agent: "codex", Source: "desktop", Status: "running", Updated: "2026-10-19T00:00:00Z"}
	m.Store.Put("sessions", s.ID, s)
	if err := m.Store.MarkDesktopUnobserved(s.ID); err != nil {
		t.Fatal(err)
	}
	var got store.Session
	m.Store.Get("sessions", s.ID, &got)
	if got.Status != "unknown" {
		t.Fatal("lost owner stayed running")
	}
	got.Status = "failed"
	got.Issue = nativeIssue("safety_ended")
	m.Store.Put("sessions", s.ID, got)
	if err := m.Store.MarkDesktopUnobserved(s.ID); err != nil {
		t.Fatal(err)
	}
	got = store.Session{}
	m.Store.Get("sessions", s.ID, &got)
	if got.Status != "failed" || !got.Issue.BlocksInput {
		t.Fatal("observation failure erased safety restriction")
	}
}
