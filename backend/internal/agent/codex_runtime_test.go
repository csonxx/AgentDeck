package agent

import (
	"agentdeck/internal/store"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func ownershipFixture(t *testing.T) *Manager {
	t.Helper()
	fixture, e := filepath.Abs("../../../scripts/fixture_runtime.py")
	if e != nil {
		t.Fatal(e)
	}
	bin := t.TempDir()
	if e = os.Symlink(fixture, filepath.Join(bin, "codex")); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return connectorManager(t)
}
func awaitWriterExit(t *testing.T, m *Manager, id string, r *runtime) {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		t.Fatal("completed Codex executor retained writer")
	}
	// done means the native writer exited. Its registry entry is removed just
	// afterwards; wait for that separately rather than using liveRuntime, which
	// intentionally hides already-exited entries.
	deadline := time.Now().Add(time.Second)
	for {
		m.mu.Lock()
		registered := m.runs[id] == r
		m.mu.Unlock()
		if !registered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("retired runtime remained registered")
		}
		time.Sleep(time.Millisecond)
	}
	var s store.Session
	m.Store.Get("sessions", id, &s)
	if s.Status != "completed" && s.Status != "interrupted" {
		t.Fatalf("idle retirement changed completion: %s", s.Status)
	}
}
func TestPhoneCodexCompletionReleasesAndSameIDResumes(t *testing.T) {
	m := ownershipFixture(t)
	s, e := m.Create("codex", m.Projects()[0].ID, "")
	if e != nil {
		t.Fatal(e)
	}
	first := m.liveRuntime(s.ID)
	if e = m.Send(s.ID, "first", UUID()); e != nil {
		t.Fatal(e)
	}
	awaitConnector(t, m, s.ID, "status", "", "completed")
	awaitWriterExit(t, m, s.ID, first)
	if e = m.Send(s.ID, "second", UUID()); e != nil {
		t.Fatal(e)
	}
	// Completion and the second runtime's exit must not replace the original ID.
	deadline := time.Now().Add(5 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		evs, _ := m.Store.Events(s.ID, 0, 500)
		answers := 0
		for _, ev := range evs {
			if ev.Kind == "message" && ev.Role == "assistant" {
				answers++
			}
		}
		if answers >= 2 && m.liveRuntime(s.ID) == nil {
			completed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !completed {
		t.Fatal("second original turn did not complete")
	}
	ss, _ := m.Store.Sessions()
	if len(ss) != 1 || ss[0].ID != s.ID {
		t.Fatal("continued phone chat forked")
	}
	if m.liveRuntime(s.ID) != nil {
		t.Fatal("second completed executor retained writer")
	}
	p, e := m.SessionPermissions(s.ID)
	if e != nil || p.Source != "last_turn" || !p.Editable {
		t.Fatal("idle permissions were lost", e, p.Source)
	}
	if m.liveRuntime(s.ID) != nil {
		t.Fatal("permission polling reacquired writer")
	}
	changed, e := m.ChangePermissions(s.ID, p.Version, "auto_review")
	if e != nil || changed.Mode != "auto_review" {
		t.Fatal("idle permission edit failed", e)
	}
	m.releaseIdleCodex(s.ID)
	if m.liveRuntime(s.ID) != nil {
		t.Fatal("permission edit retained writer")
	}
}
func TestPhoneCodexAfterCompletionFollowsDesktopOriginal(t *testing.T) {
	m := ownershipFixture(t)
	s, e := m.Create("codex", m.Projects()[0].ID, "")
	if e != nil {
		t.Fatal(e)
	}
	r := m.liveRuntime(s.ID)
	if e = m.Send(s.ID, "first", UUID()); e != nil {
		t.Fatal(e)
	}
	awaitWriterExit(t, m, s.ID, r)
	desktop := &desktopFixture{thread: DesktopThread{ID: s.ID, Kind: "codex", Host: "local", Project: s.Project}}
	m.Desktop = desktop
	if e = m.Send(s.ID, "follow computer", UUID()); e != nil {
		t.Fatal(e)
	}
	if desktop.sentID != s.ID || desktop.text != "follow computer" || m.liveRuntime(s.ID) != nil {
		t.Fatal("Desktop continuation launched a competing CLI")
	}
	var actual store.Session
	m.Store.Get("sessions", s.ID, &actual)
	if actual.Source != "desktop" {
		t.Fatal("original owner was not adopted")
	}
}
func TestCodexBusyAndApprovalAreNotRetired(t *testing.T) {
	m := ownershipFixture(t)
	s, e := m.Create("codex", m.Projects()[0].ID, "")
	if e != nil {
		t.Fatal(e)
	}
	r := m.liveRuntime(s.ID)
	if e = m.Send(s.ID, "approval", UUID()); e != nil {
		t.Fatal(e)
	}
	approval := awaitConnector(t, m, s.ID, "approval", "", "pending")
	m.releaseIdleCodex(s.ID)
	select {
	case <-r.done:
		t.Fatal("pending tool approval was interrupted")
	default:
	}
	if e = m.Approve(s.ID, approval.Item, "accept", ""); e != nil {
		t.Fatal(e)
	}
	awaitWriterExit(t, m, s.ID, r)
	m.mu.Lock()
	before := len(m.runs)
	m.mu.Unlock()
	if e = m.Interrupt(s.ID); e == nil {
		t.Fatal("idle stop pretended active")
	}
	if e = m.Approve(s.ID, "expired", "accept", ""); e == nil {
		t.Fatal("expired approval accepted")
	}
	m.mu.Lock()
	after := len(m.runs)
	m.mu.Unlock()
	if after != before {
		t.Fatal("idle stop/approval relaunched CLI")
	}
}
func TestCodexRuntimeLeaseSurvivesCompletionUntilCallerReleases(t *testing.T) {
	m := ownershipFixture(t)
	s, e := m.Create("codex", m.Projects()[0].ID, "")
	if e != nil {
		t.Fatal(e)
	}
	borrowed, e := m.get(s.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Send(s.ID, "quick", UUID()); e != nil {
		t.Fatal(e)
	}
	awaitConnector(t, m, s.ID, "status", "", "completed")
	m.releaseIdleCodex(s.ID)
	select {
	case <-borrowed.done:
		t.Fatal("borrowed RPC executor was closed")
	default:
	}
	borrowed.releaseUse()
	awaitWriterExit(t, m, s.ID, borrowed)
}

func TestCodexActiveGoalRetainsWriterUntilGoalCompletes(t *testing.T) {
	m := ownershipFixture(t)
	s, e := m.Create("codex", m.Projects()[0].ID, "")
	if e != nil {
		t.Fatal(e)
	}
	m.CodexGoalPath = filepath.Join(t.TempDir(), "goals.sqlite")
	db, e := sql.Open("sqlite", m.CodexGoalPath)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	// Match the native Goal database while catalog reads overlap fixture writes.
	if _, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`CREATE TABLE thread_goals(thread_id TEXT PRIMARY KEY, objective TEXT, status TEXT, token_budget INTEGER, tokens_used INTEGER, time_used_seconds INTEGER, created_at_ms INTEGER, updated_at_ms INTEGER)`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO thread_goals VALUES (?, 'synthetic ownership goal','active',NULL,0,0,1000,1000)`, s.ID); e != nil {
		t.Fatal(e)
	}
	r := m.liveRuntime(s.ID)
	if e = m.Send(s.ID, "goal turn", UUID()); e != nil {
		t.Fatal(e)
	}
	awaitConnector(t, m, s.ID, "status", "", "completed")
	m.releaseIdleCodex(s.ID)
	select {
	case <-r.done:
		t.Fatal("active goal executor was interrupted")
	default:
	}
	// The native executor can start another Goal turn without SendInput.
	if e = r.write(map[string]any{"method": "fixture/native-turn-start"}); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(time.Second)
	for {
		r.mu.Lock()
		busy := r.busy
		r.mu.Unlock()
		if busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native continuation was not marked running")
		}
		time.Sleep(time.Millisecond)
	}
	if _, e = db.Exec(`UPDATE thread_goals SET status='complete' WHERE thread_id=?`, s.ID); e != nil {
		t.Fatal(e)
	}
	m.releaseIdleCodex(s.ID)
	select {
	case <-r.done:
		t.Fatal("goal completion interrupted its still-running turn")
	default:
	}
	if e = r.write(map[string]any{"method": "fixture/native-turn-complete"}); e != nil {
		t.Fatal(e)
	}
	awaitWriterExit(t, m, s.ID, r)
}
