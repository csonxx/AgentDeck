package agent

import (
	"agentdeck/internal/store"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeNativeGoalRecords(t *testing.T) {
	s, e := store.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	n := &Normalizer{Store: s}
	st := State{Session: "claude-goal", CWD: t.TempDir()}
	records := []string{
		`{"type":"attachment","timestamp":"2026-10-10T12:00:00Z","attachment":{"type":"goal_status","sentinel":true,"met":false,"condition":"synthetic condition"}}`,
		`{"type":"active_goal","timestamp":"2026-10-10T12:01:00Z","value":{"condition":"synthetic condition","iterations":3,"set_at":1791633600000,"last_reason":"working"}}`,
		`{"type":"attachment","timestamp":"2026-10-10T12:02:00Z","attachment":{"type":"goal_status","met":true,"condition":"synthetic condition","tokens":123,"durationMs":120000}}`,
		`{"type":"attachment","timestamp":"2026-10-10T12:03:00Z","attachment":{"type":"goal_status","sentinel":true,"met":true,"condition":"synthetic condition"}}`,
	}
	for i, r := range records {
		ev, err := n.Parse([]byte(r), "claude", "log", &st)
		if err != nil || len(ev) != 0 {
			t.Fatal("goal records leaked to transcript")
		}
		var g storedClaudeGoal
		if s.Get("goals", st.Session, &g) != nil {
			t.Fatal("missing goal")
		}
		if i == 0 && g.Goal.Status != "active" || i == 1 && g.Goal.Iterations != 3 || i == 2 && g.Goal.Status != "complete" || i == 3 && g.Goal != nil {
			t.Fatalf("unexpected state at step %d", i)
		}
	}
	_, _ = n.Parse([]byte(records[0]), "claude", "log", &st)
	var g storedClaudeGoal
	_ = s.Get("goals", st.Session, &g)
	if g.Goal != nil {
		t.Fatal("old history revived cleared goal")
	}
}

func TestCurrentGoalsExcludeFinishedRecordsWithoutDeletingHistory(t *testing.T) {
	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, status := range []string{"active", "paused", "complete", "failed"} {
		_ = s.Put("sessions", status, store.Session{ID: status, Agent: "claude", Source: "claude-desktop"})
		_ = s.Put("goals", status, storedClaudeGoal{Goal: &Goal{Session: status, Agent: "claude", Objective: "Synthetic goal", Status: status}})
	}
	m := &Manager{Store: s}
	goals, err := m.Goals()
	if err != nil || len(goals) != 2 {
		t.Fatal("current goal filter", err, len(goals))
	}
	for _, g := range goals {
		if g.Status != "active" && g.Status != "paused" {
			t.Fatal("finished goal exposed")
		}
	}
	var history storedClaudeGoal
	if err := s.Get("goals", "complete", &history); err != nil || history.Goal == nil || history.Goal.Status != "complete" {
		t.Fatal("finished goal history was deleted")
	}
}
func TestGoalNativeReadAndStaleDelete(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	fixture, e := filepath.Abs("../../../scripts/fixture_runtime.py")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(fixture, bin); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	path := filepath.Join(dir, "goals.json")
	t.Setenv("AGENTDECK_FIXTURE_GOALS", path)
	raw := map[string]any{"g": map[string]any{"threadId": "g", "objective": "synthetic goal", "status": "paused", "tokensUsed": 12, "timeUsedSeconds": 30, "createdAt": 1, "updatedAt": 2}}
	data, _ := json.Marshal(raw)
	_ = os.WriteFile(path, data, 0600)
	s, e := store.New(filepath.Join(dir, "store"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_ = s.Put("sessions", "g", store.Session{ID: "g", Agent: "codex", Project: dir, Source: "stream", Managed: true})
	goalDB := filepath.Join(dir, "goals_1.sqlite")
	db, e := sql.Open("sqlite", goalDB)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`CREATE TABLE thread_goals(thread_id TEXT PRIMARY KEY, objective TEXT, status TEXT, token_budget INTEGER, tokens_used INTEGER, time_used_seconds INTEGER, created_at_ms INTEGER, updated_at_ms INTEGER); INSERT INTO thread_goals VALUES ('g','synthetic goal','paused',NULL,12,30,1000,2000)`)
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	t.Setenv("AGENTDECK_FIXTURE_GOAL_DB", goalDB)
	m := &Manager{Context: context.Background(), Store: s, Roots: []string{dir}, CodexGoalPath: goalDB}
	defer m.Close()
	goals, e := m.Goals()
	if e != nil || len(goals) != 1 {
		t.Fatalf("discovery failed: %v", e)
	}
	g := goals[0]
	if g.Tokens != 12 || g.Status != "paused" {
		t.Fatal("native state missing")
	}
	for _, action := range g.Actions {
		if action == "resume" {
			t.Fatal("unverified Codex goal scheduling advertised")
		}
	}
	if e = m.ChangeGoal("g", g.Version, "resume", UUID()); e == nil {
		t.Fatal("unverified Codex goal resume accepted")
	}
	if e = m.ChangeGoal("g", "stale", "delete", UUID()); e == nil {
		t.Fatal("stale command accepted")
	}
	if e = m.ChangeGoal("g", g.Version, "delete", UUID()); e != nil {
		t.Fatal(e)
	}
	goals, e = m.Goals()
	if e != nil || len(goals) != 0 {
		t.Fatal("native clear not reflected")
	}
}
func TestDesktopClaudeGoalDoesNotSilentlyFork(t *testing.T) {
	s, e := store.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	dir := t.TempDir()
	_ = s.Put("sessions", "g", store.Session{ID: "g", Agent: "claude", Project: dir, Source: "claude-desktop"})
	_ = s.Put("goals", "g", storedClaudeGoal{Goal: &Goal{Session: "g", Agent: "claude", Objective: "synthetic", Status: "active"}})
	m := &Manager{Store: s, Roots: []string{dir}}
	gs, e := m.Goals()
	if e != nil || len(gs) != 1 {
		t.Fatal(e)
	}
	if len(gs[0].Actions) != 0 || gs[0].ControlNote == "" {
		t.Fatal("desktop capability invented")
	}
	if e = m.ChangeGoal("g", gs[0].Version, "pause", UUID()); e == nil {
		t.Fatal("desktop goal forked")
	}
	if len(m.runs) != 0 {
		t.Fatal("unexpected native process")
	}
}
