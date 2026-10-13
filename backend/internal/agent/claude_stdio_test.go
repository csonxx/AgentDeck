package agent

import (
	"agentdeck/internal/claudestdio"
	"agentdeck/internal/store"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeStdioMissingBridgeNeverFallsBackToPeer(t *testing.T) {
	for _, installed := range []bool{false, true} {
		m, ss, _, _, _ := inboxFixture(t)
		m.ControlDir = t.TempDir()
		want := claudeStdioInstall
		if installed {
			os.Mkdir(filepath.Join(m.ControlDir, "cs"), 0700)
			os.WriteFile(filepath.Join(m.ControlDir, "cs", "enabled"), []byte("native-stdin-v1\n"), 0600)
			want = claudeStdioRestart
		}
		if err := m.Send(ss.ID, "fixture", UUID()); err == nil || err.Error() != want {
			t.Fatalf("missing bridge silently fell back: installed=%v err=%v", installed, err)
		} else if !InputDefinitelyNotSent(err) {
			t.Fatal("definite rejection presented as uncertain delivery")
		}
		if len(m.runs) != 0 {
			t.Fatal("second CLI launched")
		}
	}
}

func TestClaudeStdioOriginalReceiptAndImageFrame(t *testing.T) {
	m, ss, _, log, _ := inboxFixture(t)
	root, err := os.MkdirTemp("/tmp", "ad-cs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	root, _ = filepath.EvalSymlinks(root)
	m.ControlDir = root
	cs := filepath.Join(root, "cs")
	os.Mkdir(cs, 0700)
	socket := filepath.Join(cs, "1234.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	os.Chmod(socket, 0600)
	r := claudestdio.Record{PID: 1234, Start: "fixture birth", Project: ss.Project, Socket: socket}
	b, _ := json.Marshal(r)
	os.WriteFile(filepath.Join(cs, "1234.json"), b, 0600)
	received := make(chan claudestdio.Input, 1)
	request := strings.ReplaceAll(UUID(), "-", "")
	go func() {
		c, e := l.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		var input claudestdio.Input
		if json.NewDecoder(c).Decode(&input) != nil {
			return
		}
		received <- input
		json.NewEncoder(c).Encode(claudestdio.Reply{Written: true})
		time.Sleep(50 * time.Millisecond)
		row, _ := json.Marshal(map[string]any{"type": "user", "sessionId": ss.ID, "uuid": input.UUID})
		os.WriteFile(log, append(row, '\n'), 0600)
	}()
	err = m.SendInput(ss.ID, "image fixture", request, TurnOptions{}, []InputAttachment{{ID: UUID(), Name: "image.png", MIME: "image/png", Data: []byte{1, 2, 3}}})
	if err != nil {
		t.Fatal(err)
	}
	input := <-received
	if input.Session != ss.ID || input.UUID != request {
		t.Fatal("original input identity lost")
	}
	blocks, ok := input.Content.([]any)
	if !ok || len(blocks) != 2 || obj(blocks[1])["type"] != "image" {
		t.Fatal("native image input lost")
	}
	if len(m.runs) != 0 {
		t.Fatal("second CLI launched")
	}
}

// The real native process must see ordinary user input even while its peer
// inbox explicitly refuses messages. This catches accidental peer fallback.
func TestRealClaudeStdioOriginalUserInput(t *testing.T) {
	binary := os.Getenv("AGENTDECK_REAL_CLAUDE_INBOX_BINARY")
	wrapper := os.Getenv("AGENTDECK_REAL_CLAUDE_STDIN_WRAPPER")
	if binary == "" || wrapper == "" {
		t.Skip("explicit opt-in real native stdin bridge with local fake provider")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "../../testsupport/claude-inbox/main.py")
	cmd.Env = append(os.Environ(), "AD_NATIVE_CLAUDE_BINARY="+binary, "AD_NATIVE_STDIN_WRAPPER="+wrapper, "AD_PROBE_INBOUND=refuse", "AD_PROBE_BUSY=1")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { in.Close(); cmd.Wait() }()
	decoder := json.NewDecoder(out)
	var ready struct {
		ID       string `json:"id"`
		Project  string `json:"project"`
		Registry string `json:"registry"`
		Logs     string `json:"logs"`
		Control  string `json:"control"`
	}
	if err = decoder.Decode(&ready); err != nil {
		t.Fatal(err)
	}
	n := setup(t)
	m := &Manager{ClaudeDesktopInput: true, Store: n.Store, Normalizer: n, Roots: []string{ready.Project}, Context: ctx, ClaudeGoalRoot: ready.Logs, ClaudeProcesses: ready.Registry, ControlDir: ready.Control}
	ss := store.Session{ID: ready.ID, Agent: "claude", Project: ready.Project, Source: "claude-desktop", Status: "idle"}
	m.Store.Put("sessions", ss.ID, ss)
	owner, found, err := m.claudeInbox(ss)
	if err != nil || !found {
		t.Fatal("native original owner missing", err)
	}
	r, err := claudestdio.ReadRecord(ready.Control+"/cs", owner.PID)
	if err != nil {
		t.Fatal(err)
	}
	// A different session and an arbitrary RPC payload cannot enter the child.
	for _, payload := range []any{claudestdio.Input{Session: UUID(), UUID: UUID(), Content: "wrong session"}, map[string]any{"method": "permission/set", "session_id": ss.ID}} {
		c, e := net.Dial("unix", r.Socket)
		if e != nil {
			t.Fatal(e)
		}
		c.SetDeadline(time.Now().Add(2 * time.Second))
		json.NewEncoder(c).Encode(payload)
		var reply claudestdio.Reply
		e = json.NewDecoder(c).Decode(&reply)
		c.Close()
		if e != nil || reply.Written || reply.Error == "" {
			t.Fatal("invalid bridge input accepted", e)
		}
	}
	request := strings.ReplaceAll(UUID(), "-", "")
	if err = m.Send(ss.ID, "AgentDeck isolated original-session transport fixture.", request); err != nil {
		t.Fatal("native stdin input not confirmed", err)
	}
	// Retry the same local request ID at the bridge: return a duplicate receipt
	// without writing a second user message into the original process.
	c, err := net.Dial("unix", r.Socket)
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(2 * time.Second))
	json.NewEncoder(c).Encode(claudestdio.Input{Session: ss.ID, UUID: request, Content: "AgentDeck isolated original-session transport fixture."})
	var reply claudestdio.Reply
	err = json.NewDecoder(c).Decode(&reply)
	c.Close()
	if err != nil || !reply.Written || !reply.Duplicate {
		t.Fatal("missing duplicate receipt", err)
	}
	var report struct {
		Seen    bool `json:"provider_seen_input"`
		Peer    bool `json:"peer_preamble_seen"`
		Replays int  `json:"native_replay_count"`
		Image   bool `json:"provider_image_seen"`
		Busy    bool `json:"provider_busy"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err = in.Write([]byte("report\n")); err != nil {
			t.Fatal(err)
		}
		if err = decoder.Decode(&report); err != nil {
			t.Fatal(err)
		}
		if report.Seen && report.Busy {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !report.Seen || report.Peer {
		t.Fatalf("native user origin/replay not preserved: seen=%v peer=%v replays=%d", report.Seen, report.Peer, report.Replays)
	}
	if !report.Busy {
		t.Fatal("native busy input was not exercised")
	}
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4v0z5PwAHOALI/Huk1wAAAABJRU5ErkJggg==")
	secondRequest := UUID()
	secondObserved := m.claudeReceiptFiles(ss.ID)
	if err = m.SendDelivery(ss.ID, "AgentDeck isolated image fixture.", secondRequest, TurnOptions{}, []InputAttachment{{ID: UUID(), Name: "fixture.png", MIME: "image/png", Data: png}}, "queue"); err != nil {
		t.Fatal("native image input unconfirmed", err)
	}
	result, _ := m.NativeSubmission(ss.ID, secondRequest).(map[string]string)
	if result["delivery"] != "submitted" {
		t.Fatal("missing honest native submission receipt")
	}
	var updated store.Session
	if m.Store.Get("sessions", ss.ID, &updated) != nil || (updated.Status != "running" && updated.Status != "pending") {
		t.Fatal("native queued input did not update sidebar running state")
	}
	in.Write([]byte("release\n"))
	deadline = time.Now().Add(10 * time.Second)
	secondRecorded := false
	for time.Now().Before(deadline) {
		in.Write([]byte("report\n"))
		if err = decoder.Decode(&report); err != nil {
			t.Fatal(err)
		}
		secondRecorded = secondRecorded || m.claudeInputRecorded(ss.ID, secondRequest, &secondObserved)
		if report.Image && report.Replays == 1 && secondRecorded {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !report.Image || report.Peer || report.Replays != 1 || !secondRecorded {
		t.Fatal("native image/provider origin missing")
	}
	if len(m.runs) != 0 {
		t.Fatal("original owner replaced with a second CLI")
	}
	t.Log("same native owner/UUID confirmed; peer refusal preserved; model saw user input without peer preamble; duplicate input suppressed")
}
