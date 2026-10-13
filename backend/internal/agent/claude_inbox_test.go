package agent

import (
	"agentdeck/internal/store"
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRealClaudeInboxNativeIsolated(t *testing.T) {
	binary := os.Getenv("AGENTDECK_REAL_CLAUDE_INBOX_BINARY")
	if binary == "" {
		t.Skip("explicit opt-in native Claude inbox with a local fake provider")
	}
	for _, inbound := range []string{"accept", "user-accept", "refuse", "default-bypass", "default-bypass-project"} {
		t.Run(inbound, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "python3", "../../testsupport/claude-inbox/main.py")
			cmd.Env = append(os.Environ(), "AD_NATIVE_CLAUDE_BINARY="+binary, "AD_PROBE_INBOUND="+inbound)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { stdin.Close(); cmd.Wait() }()
			type ready struct {
				ID       string `json:"id"`
				Project  string `json:"project"`
				Registry string `json:"registry"`
				Logs     string `json:"logs"`
			}
			response := make(chan ready, 1)
			go func() { var v ready; json.NewDecoder(stdout).Decode(&v); response <- v }()
			var r ready
			select {
			case r = <-response:
			case <-time.After(25 * time.Second):
				t.Fatal("native fixture startup timeout")
			}
			if r.ID == "" {
				t.Fatal("native fixture failed to initialize")
			}
			n := setup(t)
			m := &Manager{ClaudeDesktopInput: true, Store: n.Store, Normalizer: n, Roots: []string{r.Project}, Context: ctx, ClaudeGoalRoot: r.Logs, ClaudeProcesses: r.Registry}
			ss := store.Session{ID: r.ID, Agent: "claude", Project: r.Project, Source: "claude-desktop", Status: "idle"}
			m.Store.Put("sessions", ss.ID, ss)
			if _, found, err := m.claudeInbox(ss); err != nil || !found {
				t.Fatal("actual CLI owner not verified", err)
			}
			request := UUID()
			observed := m.claudeReceiptFiles(ss.ID)
			err = m.Send(ss.ID, "AgentDeck isolated original-session transport fixture.", request)
			expectAccepted := inbound == "accept" || inbound == "user-accept"
			if expectAccepted && err != nil {
				t.Fatal("actual native delivery unconfirmed", err)
			}
			if !expectAccepted && (err == nil || !strings.Contains(err.Error(), "勿重复发送")) {
				t.Fatal("native inbound refusal bypassed", err)
			}
			initialReceipt := err == nil
			if strings.HasPrefix(inbound, "default-bypass") {
				command := "accept\n"
				if inbound == "default-bypass-project" {
					if _, err = stdin.Write([]byte("accept-project\n")); err != nil {
						t.Fatal(err)
					}
					time.Sleep(1500 * time.Millisecond)
					if m.claudeInputRecorded(ss.ID, request, &observed) {
						t.Fatal("project-scoped accept unexpectedly released a held message; reassess native policy support")
					}
					t.Log("this native CLI did not release held input with project-scoped accept")
				}
				if _, err = stdin.Write([]byte(command)); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(12 * time.Second)
				accepted := false
				for time.Now().Before(deadline) {
					if m.claudeInputRecorded(ss.ID, request, &observed) {
						accepted = true
						break
					}
					time.Sleep(100 * time.Millisecond)
				}
				if !accepted {
					t.Fatal("live inbound policy change did not release the held original UUID")
				}
				t.Log("default bypass held input; explicit accept released original UUID without resending")
			}
			if len(m.runs) != 0 {
				t.Fatal("second Claude process launched")
			}
			t.Logf("actual native inbox mode=%s initial same-session receipt=%v second-process=false provider=local-fixture", inbound, initialReceipt)
		})
	}
}

func TestRealClaudeInboxReadOnly(t *testing.T) {
	logs := os.Getenv("AGENTDECK_REAL_CLAUDE_INBOX_LOGS")
	if logs == "" {
		t.Skip("explicit opt-in read-only existing Claude inbox discovery")
	}
	registry := filepath.Join(filepath.Dir(logs), "sessions")
	entries, err := filepath.Glob(filepath.Join(registry, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	n := setup(t)
	count := 0
	for _, p := range entries {
		b, e := os.ReadFile(p)
		if e != nil {
			continue
		}
		var r claudeInboxOwner
		if json.Unmarshal(b, &r) != nil || r.CWD == "" {
			continue
		}
		m := &Manager{ClaudeDesktopInput: true, Store: n.Store, Normalizer: n, Roots: []string{r.CWD}, ClaudeGoalRoot: logs, ClaudeProcesses: registry}
		ss := store.Session{ID: r.Session, Agent: "claude", Project: r.CWD, Source: "claude-desktop"}
		if _, found, e := m.claudeInbox(ss); e == nil && found {
			count++
		}
	}
	if count == 0 {
		t.Fatal("no active compatible inbox verified")
	}
	t.Logf("read-only native owners=%d sockets-connected=0 prompts-sent=0", count)
}

func inboxFixture(t *testing.T) (*Manager, store.Session, string, string, net.Listener) {
	t.Helper()
	n := setup(t)
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	registry := filepath.Join(root, "sessions")
	logs := filepath.Join(root, "projects")
	project := filepath.Join(root, "work")
	for _, p := range []string{registry, project, filepath.Join(logs, "work")} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Unix domain socket paths are short, even if the Go test directory is long.
	socketDir, err := os.MkdirTemp("/tmp", "ad-cc-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(socketDir) })
	sock := filepath.Join(socketDir, "inbox.sock")
	listener, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	os.Chmod(sock, 0600)
	ss := store.Session{ID: UUID(), Agent: "claude", Source: "claude-desktop", Project: project, Title: "fixture", Status: "idle"}
	n.Store.Put("sessions", ss.ID, ss)
	owner := claudeInboxOwner{claudeProcessRecord: claudeProcessRecord{PID: 1234, Session: ss.ID, Start: "fixture birth", Status: "idle", At: 1}, CWD: project, Socket: sock, Protocol: 1}
	b, _ := json.Marshal(owner)
	record := filepath.Join(registry, "1234.json")
	os.WriteFile(record, b, 0644)
	m := &Manager{ClaudeDesktopInput: true, Store: n.Store, Normalizer: n, Roots: []string{project}, ClaudeGoalRoot: logs, ClaudeProcesses: registry, ClaudeProcessSnapshot: func() (map[int]string, error) { return map[int]string{1234: "fixture birth"}, nil }}
	return m, ss, record, filepath.Join(logs, "work", ss.ID+".jsonl"), listener
}

func TestClaudeInboxOriginalIDSendAndNativeUUIDReceipt(t *testing.T) {
	m, ss, _, log, listener := inboxFixture(t)
	received := make(chan map[string]any, 1)
	request := UUID()
	os.WriteFile(log, []byte("{\"type\":\"user\",\"uuid\":\"old\"}\n"), 0600)
	go func() {
		c, e := listener.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		line, _ := bufio.NewReader(c).ReadBytes('\n')
		var v map[string]any
		json.Unmarshal(line, &v)
		received <- v
		time.Sleep(80 * time.Millisecond)
		b, _ := json.Marshal(map[string]any{"type": "user", "sessionId": ss.ID, "uuid": request, "message": v["message"]})
		f, _ := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0600)
		if f != nil {
			f.Write(append(b, '\n'))
			f.Close()
		}
	}()
	hydration := 0
	m.LoadHistory = func(id string) error {
		hydration++
		if id != ss.ID {
			t.Error("wrong history")
		}
		return nil
	}
	continued, err := m.Create("claude", digest(ss.Project)[:24], ss.ID)
	if err != nil || continued.ID != ss.ID || !continued.Managed || continued.Parent != "" {
		t.Fatal("original adoption failed", err)
	}
	if err = m.Send(ss.ID, "native fixture", request); err != nil {
		t.Fatal(err)
	}
	v := <-received
	if v["type"] != "user" || v["uuid"] != request || v["session_id"] != ss.ID || str(obj(v["message"])["content"]) != "native fixture" || v["senderMode"] != nil || v["token"] != nil {
		t.Fatal("unsafe or incorrect native frame")
	}
	sessions, _ := m.Store.Sessions()
	if len(sessions) != 1 || len(m.runs) != 0 || hydration != 2 {
		t.Fatal("fork/second process or no native confirmation")
	}
	events, _ := m.Store.Events(ss.ID, 0, 100)
	if len(events) != 0 {
		t.Fatal("delivery invented transcript echo")
	}
}

func TestClaudeInboxNativeSteerStaysInOwnerProcess(t *testing.T) {
	m, ss, _, log, listener := inboxFixture(t)
	received := make(chan map[string]any, 1)
	request := UUID()
	os.WriteFile(log, []byte("{\"type\":\"user\",\"uuid\":\"old\"}\n"), 0600)
	go func() {
		c, e := listener.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		line, _ := bufio.NewReader(c).ReadBytes('\n')
		var v map[string]any
		json.Unmarshal(line, &v)
		received <- v
		time.Sleep(80 * time.Millisecond)
		b, _ := json.Marshal(map[string]any{"type": "user", "sessionId": ss.ID, "uuid": request, "message": v["message"]})
		f, _ := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0600)
		if f != nil {
			f.Write(append(b, '\n'))
			f.Close()
		}
	}()
	hydration := 0
	m.LoadHistory = func(id string) error {
		hydration++
		if id != ss.ID {
			t.Error("wrong history")
		}
		return nil
	}
	continued, err := m.Create("claude", digest(ss.Project)[:24], ss.ID)
	if err != nil || continued.ID != ss.ID || !continued.Managed || continued.Parent != "" {
		t.Fatal("original adoption failed", err)
	}
	if err = m.SendDelivery(ss.ID, "native fixture", request, TurnOptions{}, nil, "steer"); err != nil {
		t.Fatal(err)
	}
	v := <-received
	if v["type"] != "user" || v["uuid"] != request || v["session_id"] != ss.ID || str(obj(v["message"])["content"]) != "native fixture" || v["senderMode"] != nil || v["token"] != nil {
		t.Fatal("unsafe or incorrect native frame")
	}
	sessions, _ := m.Store.Sessions()
	if len(sessions) != 1 || len(m.runs) != 0 || hydration != 2 {
		t.Fatal("fork/second process or no native confirmation")
	}
	events, _ := m.Store.Events(ss.ID, 0, 100)
	if len(events) != 0 {
		t.Fatal("delivery invented transcript echo")
	}
}

func TestClaudeInboxRefusesOwnerAndSocketBoundaryChanges(t *testing.T) {
	for _, kind := range []string{"birth", "project", "protocol", "socket_symlink", "socket_mode", "registry_mode", "record_symlink", "record_writable"} {
		t.Run(kind, func(t *testing.T) {
			m, ss, record, _, listener := inboxFixture(t)
			_ = listener
			b, _ := os.ReadFile(record)
			var owner claudeInboxOwner
			json.Unmarshal(b, &owner)
			switch kind {
			case "birth":
				m.ClaudeProcessSnapshot = func() (map[int]string, error) { return map[int]string{1234: "reused PID"}, nil }
			case "project":
				owner.CWD = filepath.Dir(ss.Project)
			case "protocol":
				owner.Protocol = 2
			case "socket_symlink":
				p := filepath.Join(filepath.Dir(owner.Socket), "alias.sock")
				os.Symlink(owner.Socket, p)
				owner.Socket = p
			case "socket_mode":
				os.Chmod(owner.Socket, 0666)
			case "registry_mode":
				os.Chmod(m.ClaudeProcesses, 0755)
			case "record_symlink":
				os.Rename(record, record+".old")
				os.Symlink(record+".old", record)
			case "record_writable":
				os.Chmod(record, 0666)
			}
			if kind == "project" || kind == "protocol" || kind == "socket_symlink" {
				b, _ = json.Marshal(owner)
				os.WriteFile(record, b, 0644)
			}
			_, found, err := m.claudeInbox(ss)
			if err == nil && found {
				t.Fatal("unsafe owner accepted")
			}
		})
	}
}

func TestClaudeInboxUnconfirmedDoesNotFallBackOrPretendDelivery(t *testing.T) {
	m, ss, _, log, listener := inboxFixture(t)
	request := UUID()
	// An old matching UUID is not evidence that this delivery was received.
	b, _ := json.Marshal(map[string]any{"type": "user", "sessionId": ss.ID, "uuid": request})
	os.WriteFile(log, append(b, '\n'), 0600)
	go func() {
		c, e := listener.Accept()
		if e == nil {
			bufio.NewReader(c).ReadBytes('\n')
			c.Close()
		}
	}()
	err := m.Send(ss.ID, "held fixture", request)
	if err == nil || err.Error() != claudeInboxIdleUnconfirmed || len(m.runs) != 0 {
		t.Fatal("unconfirmed delivery was retried or accepted", err)
	}
	events, _ := m.Store.Events(ss.ID, 0, 100)
	if len(events) != 0 {
		t.Fatal("unconfirmed send changed history")
	}
}

func TestClaudeInboxUnsupportedOptionsAndImagesNeverReachOwner(t *testing.T) {
	m, ss, _, _, _ := inboxFixture(t)
	for _, o := range []TurnOptions{{Model: "fable"}, {Effort: "high"}} {
		if err := m.SendInput(ss.ID, "fixture", UUID(), o, nil); err == nil {
			t.Fatal("native options ignored")
		}
	}
	if err := m.SendInput(ss.ID, "fixture", UUID(), TurnOptions{}, []InputAttachment{{Name: "x.png", MIME: "image/png", Data: []byte("fixture")}}); err == nil {
		t.Fatal("image silently converted")
	}
	if _, err := m.setOptions(ss, TurnOptions{Effort: "high"}); err == nil {
		t.Fatal("native option saved as if applied")
	}
}

func TestClaudeInboxQueuedInputWaitsAndDispatchesWithoutDeadlock(t *testing.T) {
	m, ss, _, log, listener := inboxFixture(t)
	request := UUID()
	os.WriteFile(log, nil, 0600)
	ss.Managed = true
	ss.Status = "waiting"
	m.Store.Put("sessions", ss.ID, ss)
	d := store.Delivery{ID: request, Session: ss.ID, State: "queued", Created: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)}
	if err := m.Store.EnqueueDelivery(d, queuedInput{Text: "queued fixture"}); err != nil {
		t.Fatal(err)
	}
	m.dispatchDelivery(context.Background(), d)
	var got store.Delivery
	m.Store.Get("deliveries", request, &got)
	if got.State != "queued" {
		t.Fatal("dispatched through native approval wait")
	}
	ss.Status = "idle"
	m.Store.Put("sessions", ss.ID, ss)
	go func() {
		c, e := listener.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		line, _ := bufio.NewReader(c).ReadBytes('\n')
		var v map[string]any
		json.Unmarshal(line, &v)
		b, _ := json.Marshal(map[string]any{"type": "user", "sessionId": ss.ID, "uuid": v["uuid"]})
		f, e := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0600)
		if e == nil {
			f.Write(append(b, '\n'))
			f.Close()
		}
	}()
	done := make(chan struct{})
	go func() { m.dispatchDelivery(context.Background(), d); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("native queue delivery deadlocked")
	}
	m.Store.Get("deliveries", request, &got)
	if got.State != "sent" || len(m.runs) != 0 {
		t.Fatal("queue was not confirmed by original owner")
	}
}
