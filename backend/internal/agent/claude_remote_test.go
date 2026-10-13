package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func remoteFixture(t *testing.T, reply string) (*Manager, string, string, string) {
	t.Helper()
	m, ss, record, _, _ := inboxFixture(t)
	m.ClaudeDesktopInput = false
	b, _ := json.Marshal(claudeProcessRecord{PID: 1234, Session: ss.ID, Start: "fixture birth", Status: "busy", At: 1, ProjectDir: ss.Project, EntryPoint: "claude-desktop", BridgeSessionID: "cse_fixture"})
	if err := os.WriteFile(record, b, 0600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls")
	t.Setenv("AD_REMOTE_CALLS", calls)
	t.Setenv("AD_REMOTE_REPLY", reply)
	// Synthetic protocol fixture: asserts sender-only flags and stdin. Never
	// starts a native execution session or contacts an actual account.
	script := `#!/usr/bin/env python3
import os,sys,json
assert sys.argv[1:] == ['--print','--cloud=cse_fixture','--output-format=json']
assert sys.stdin.read() == 'fixture owner message'
with open(os.environ['AD_REMOTE_CALLS'],'a') as f: f.write('send\n')
print(os.environ['AD_REMOTE_REPLY'])
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	m.claudeRelease = func(claudeProcessRecord) error { t.Fatal("Desktop executor terminated"); return nil }
	return m, ss.ID, record, calls
}

func TestClaudeRemotePeerProtocolIdempotency(t *testing.T) {
	m, id, record, calls := remoteFixture(t, `{"ok":true,"session_id":"cse_fixture"}`)
	request := UUID()
	if err := sendRemotePeerFixture(t, m, id, "fixture owner message", request, TurnOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	if receipt, ok := m.NativeSubmission(id, request).(map[string]string); !ok || receipt["delivery"] != "submitted" {
		t.Fatal("native submission missing")
	}
	if err := sendRemotePeerFixture(t, m, id, "fixture owner message", request, TurnOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	// Idempotency also survives a fresh Manager and disappearance of the owner.
	if err := os.Remove(record); err != nil {
		t.Fatal(err)
	}
	restarted := &Manager{Store: m.Store, Roots: m.Roots, ClaudeProcesses: m.ClaudeProcesses, ClaudeProcessSnapshot: m.ClaudeProcessSnapshot}
	if err := sendRemotePeerFixture(t, restarted, id, "fixture owner message", request, TurnOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(calls)
	if string(data) != "send\n" || len(m.runs) != 0 || len(restarted.runs) != 0 {
		t.Fatal("duplicate send or replacement executor")
	}
	queue, err := m.Queue(id)
	if err != nil || len(queue) != 0 {
		t.Fatal("local queue created", err)
	}
	if err := sendRemotePeerFixture(t, restarted, id, "different input", request, TurnOptions{}, nil); !InputDefinitelyNotSent(err) {
		t.Fatal("request reused for different content")
	}
}

func TestClaudeRemoteAmbiguousResponseNeverRetriesOrFallsBack(t *testing.T) {
	for _, reply := range []string{`{"ok":false}`, `{"ok":true,"session_id":"cse_wrong"}`, `invalid json`, strings.Repeat("x", 70<<10)} {
		t.Run("response", func(t *testing.T) {
			m, id, _, calls := remoteFixture(t, reply)
			request := UUID()
			for i := 0; i < 2; i++ {
				err := sendRemotePeerFixture(t, m, id, "fixture owner message", request, TurnOptions{}, nil)
				if err == nil || InputDefinitelyNotSent(err) {
					t.Fatal("ambiguous response reported as delivered or retryable")
				}
			}
			data, _ := os.ReadFile(calls)
			if string(data) != "send\n" || len(m.runs) != 0 {
				t.Fatal("ambiguous send repeated")
			}
		})
	}
}

func TestClaudeRemoteRejectsUnconnectedOrWrongOwnerBeforeSending(t *testing.T) {
	for _, kind := range []string{"disconnected", "project", "bridge", "birth", "writable", "model", "image"} {
		t.Run(kind, func(t *testing.T) {
			m, id, record, calls := remoteFixture(t, `{"ok":true,"session_id":"cse_fixture"}`)
			b, _ := os.ReadFile(record)
			var owner claudeProcessRecord
			json.Unmarshal(b, &owner)
			o := TurnOptions{}
			var files []InputAttachment
			switch kind {
			case "disconnected":
				owner.BridgeSessionID = ""
			case "project":
				owner.ProjectDir = filepath.Dir(owner.ProjectDir)
			case "bridge":
				owner.BridgeSessionID = "--bad"
			case "birth":
				m.ClaudeProcessSnapshot = func() (map[int]string, error) { return map[int]string{1234: "different"}, nil }
			case "model":
				o.Model = "other-model"
			case "image":
				files = []InputAttachment{{MIME: "image/png", Data: []byte{1}}}
			}
			b, _ = json.Marshal(owner)
			os.WriteFile(record, b, 0600)
			if kind == "writable" {
				os.Chmod(record, 0666)
			}
			err := sendRemotePeerFixture(t, m, id, "fixture owner message", UUID(), o, files)
			if err == nil || kind != "birth" && !InputDefinitelyNotSent(err) {
				t.Fatal("unsafe native owner accepted", err)
			}
			if _, err = os.Stat(calls); !os.IsNotExist(err) {
				t.Fatal("sender started")
			}
			if len(m.runs) != 0 {
				t.Fatal("replacement executor started")
			}
		})
	}
}

func TestClaudeRemoteDesktopNeverAcquiredForApproval(t *testing.T) {
	m, id, _, _ := remoteFixture(t, "")
	if err := m.acquireClaude(id); err == nil {
		t.Fatal("desktop executor acquired")
	}
}

// Only protocol tests invoke the peer transport directly. Production phone
// delivery must not convert paired-owner input into cross-session peer input.
func sendRemotePeerFixture(t *testing.T, m *Manager, id, text, request string, options TurnOptions, files []InputAttachment) error {
	t.Helper()
	ss, err := m.sessionForInput(id)
	if err != nil {
		return err
	}
	handled, err := m.sendClaudeRemotePeer(ss, text, request, options, files)
	if !handled && err == nil {
		return os.ErrNotExist
	}
	return err
}

func TestClaudeRemotePhoneInputNeverUsesPeerTransport(t *testing.T) {
	m, id, _, calls := remoteFixture(t, `{"ok":true,"session_id":"cse_fixture"}`)
	for _, mode := range []string{"send", "queue", "steer"} {
		request := UUID()
		for i := 0; i < 2; i++ {
			err := m.SendDelivery(id, "fixture owner message", request, TurnOptions{}, nil, mode)
			if !InputDefinitelyNotSent(err) || !strings.Contains(err.Error(), claudeRemotePeerOnly) {
				t.Fatalf("phone input passed through peer transport: %v", err)
			}
		}
	}
	if _, err := os.Stat(calls); !os.IsNotExist(err) {
		t.Fatal("peer sender started")
	}
	if len(m.runs) != 0 {
		t.Fatal("replacement executor started")
	}
	q, err := m.Queue(id)
	if err != nil || len(q) != 0 {
		t.Fatal("phone input silently queued", err)
	}
}
