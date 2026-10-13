package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentdeck/internal/store"
)

func TestClaudeContinuationUsesOriginalIDAndNativeHistory(t *testing.T) {
	n := setup(t)
	root, bin, history := t.TempDir(), t.TempDir(), t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	fixture, err := os.ReadFile("../../../scripts/fixture_runtime.py")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(bin, "claude"), fixture, 0700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AGENTDECK_FIXTURE_SESSION_DIR", history)
	id := UUID()
	os.WriteFile(filepath.Join(history, id), []byte("existing native transcript"), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := &Manager{Store: n.Store, Normalizer: n, Context: ctx, Roots: []string{root}, ClaudeOwner: func(string, int) error { return nil }}
	defer m.Close()
	old := store.Session{ID: id, Agent: "claude", Source: "claude-desktop", Project: root, Title: "Existing desktop chat", Status: "idle"}
	n.Store.Put("sessions", id, old)
	n.Store.AppendCatalogHistory(store.Event{ID: "old", Session: id, Kind: "message", Role: "user", Text: "existing history", Item: "old", Mode: "snapshot"})
	continued, err := m.Create("claude", digest(root)[:24], id)
	if err != nil || continued.ID != id || continued.Parent != "" || continued.Title != old.Title {
		t.Fatal("original identity was not retained", err)
	}
	if err = m.Send(id, "native fixture", UUID()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var s store.Session
		n.Store.Get("sessions", id, &s)
		if s.Status == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("resume did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
	r := m.runs[id]
	args := strings.Join(r.cmd.Args, " ")
	if !strings.Contains(args, "--resume="+id) || strings.Contains(args, "--fork-session") || strings.Contains(args, "--session-id=") {
		t.Fatal("continuation forked instead of resuming")
	}
	ss, _ := n.Store.Sessions()
	if len(ss) != 1 {
		t.Fatal("new session created")
	}
	if err = n.Store.SyncClaudeCatalog([]store.Session{{ID: id, Agent: "claude", Source: "claude-desktop", Title: "Renamed desktop chat", Project: root}}); err != nil {
		t.Fatal(err)
	}
	n.Store.Get("sessions", id, &continued)
	if !continued.Managed || continued.Title != "Renamed desktop chat" || continued.Status != "completed" {
		t.Fatal("adoption broke catalog updates")
	}
	m.ClaudeOwner = func(string, int) error { return errors.New(claudeOwnerBusy) }
	if selected, err := m.Create("claude", digest(root)[:24], id); err != nil || selected.ID != id {
		t.Fatal("read-only continuation selection blocked by executor")
	}
	if err = m.SendDelivery(id, "blocked fixture", UUID(), TurnOptions{}, nil, "send"); err == nil {
		t.Fatal("unverified owner accepted input")
	}
	ss, _ = n.Store.Sessions()
	if len(ss) != 1 {
		t.Fatal("busy fallback forked")
	}
}

func TestClaudeOwnerDetectionDoesNotConfuseForksOrOtherIDs(t *testing.T) {
	id := "native-id"
	for _, args := range []string{"--resume=native-id", "--resume native-id", "--session-id=native-id"} {
		if checkClaudeProcesses("42 /Applications/Claude.app/helper "+args, id, 0) == nil {
			t.Fatal("owner not detected")
		}
	}
	for _, line := range []string{"42 claude --resume=native-id-2", "42 claude --resume=native-id --fork-session --session-id=child", "42 claude --resume=native-id"} {
		if checkClaudeProcesses(line, id, 42) != nil {
			t.Fatal("own process/other session blocked")
		}
	}
	if checkClaudeProcesses("43 claude --resume=native-id --fork-session --session-id=child", id, 0) != nil {
		t.Fatal("old fork owns parent")
	}
}

// Explicit opt-in only: two tiny real provider turns in an isolated project.
func TestRealClaudeOriginalIDResume(t *testing.T) {
	project := os.Getenv("AGENTDECK_REAL_CLAUDE_RESUME_PROJECT")
	if project == "" {
		t.Skip("explicit native Claude resume acceptance only")
	}
	project, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	n := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	m := &Manager{Store: n.Store, Normalizer: n, Roots: []string{project}, Context: ctx}
	defer m.Close()
	s, err := m.Create("claude", digest(project)[:24], "")
	if err != nil {
		t.Fatal(err)
	}
	id := s.ID
	wait := func() {
		deadline := time.Now().Add(75 * time.Second)
		for time.Now().Before(deadline) {
			var current store.Session
			n.Store.Get("sessions", id, &current)
			if current.Status == "failed" {
				t.Fatal("native Claude provider turn failed")
			}
			if current.Status == "completed" {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("native Claude provider response timed out")
	}
	if err = m.Send(id, "Do not use tools or change files. Remember AGENTDECK_NATIVE_RESUME_OK and reply with only that token.", UUID()); err != nil {
		t.Fatal(err)
	}
	wait()
	m.mu.Lock()
	run := m.runs[id]
	delete(m.runs, id)
	m.mu.Unlock()
	run.close()
	<-run.done
	n.Store.Get("sessions", id, &s)
	s.Managed = false
	s.Source = "log"
	s.Parent = ""
	n.Store.Put("sessions", id, s)
	continued, err := m.Create("claude", digest(project)[:24], id)
	if err != nil || continued.ID != id || continued.Parent != "" {
		t.Fatal("native identity changed", err)
	}
	before := n.Store.LastSeq()
	if err = m.Send(id, "Without tools, recall the token I asked you to remember. Reply with only that token.", strings.ReplaceAll(UUID(), "-", "")); err != nil {
		t.Fatal(err)
	}
	wait()
	events, err := n.Store.Events(id, before, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Kind == "message" && e.Role == "assistant" && strings.Contains(e.Text, "AGENTDECK_NATIVE_RESUME_OK") {
			found = true
		}
	}
	if !found {
		t.Fatal("native resume lost prior context")
	}
	m.runs[id].mu.Lock()
	nativeID := m.runs[id].state.Session
	m.runs[id].mu.Unlock()
	if nativeID != id {
		t.Fatal("native runtime reported a different session ID")
	}
	ss, _ := n.Store.Sessions()
	if len(ss) != 1 || ss[0].ID != id {
		t.Fatal("native resume created a new chat")
	}
}
