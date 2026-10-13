package agent

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"agentdeck/internal/store"
)

func TestCodexGoalDiscoveryReadsWALWithoutLoadingThreads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals_1.sqlite")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	_, e = db.Exec(`PRAGMA journal_mode=WAL; CREATE TABLE thread_goals(thread_id TEXT PRIMARY KEY, objective TEXT, status TEXT, token_budget INTEGER, tokens_used INTEGER, time_used_seconds INTEGER, created_at_ms INTEGER, updated_at_ms INTEGER); INSERT INTO thread_goals VALUES ('known','synthetic visible','blocked',100,12,30,123456,234567), ('other','synthetic excluded','active',NULL,0,0,123456,234567)`)
	if e != nil {
		t.Fatal(e)
	}
	// Any attempt to discover via a new native runtime would fail this test.
	t.Setenv("PATH", dir)
	s, e := store.New(filepath.Join(dir, "state"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_ = s.Put("sessions", "known", store.Session{ID: "known", Agent: "codex", Project: dir, Managed: true})
	m := &Manager{Context: context.Background(), Store: s, CodexGoalPath: path}
	gs, e := m.Goals()
	if e != nil || len(gs) != 1 {
		t.Fatalf("read-only WAL discovery failed: %v", e)
	}
	g := gs[0]
	if g.Session != "known" || g.Status != "blocked" || g.Budget == nil || *g.Budget != 100 || g.Created != 123 {
		t.Fatal("native goal metadata not preserved")
	}
	var count int
	if e = db.QueryRow(`SELECT COUNT(*) FROM thread_goals`).Scan(&count); e != nil || count != 2 {
		t.Fatal("discovery changed native goals")
	}
	if _, e = os.Stat(path + "-wal"); e != nil {
		t.Fatal("test did not exercise a live WAL")
	}
}
