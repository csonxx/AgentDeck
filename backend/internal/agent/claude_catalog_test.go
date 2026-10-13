package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agentdeck/internal/store"
)

func TestClaudeDesktopCatalogLazyHistoryAndReconciliation(t *testing.T) {
	n := setup(t)
	root, index := t.TempDir(), t.TempDir()
	project := t.TempDir()
	native := filepath.Join(root, "project")
	os.MkdirAll(filepath.Join(native, "old", "subagents"), 0700)
	metaPath := filepath.Join(index, "local_desktop.json")
	writeMeta := func(title string, archived bool) {
		b, _ := json.Marshal(claudeDesktopRecord{SessionID: "local_desktop", CLI: "old", CWD: "/different-worktree", Origin: project, Title: title, Created: 1000, Activity: 2000, Archived: archived})
		if err := os.WriteFile(metaPath, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeMeta("Desktop title", false)
	log := []byte(`{"type":"user","sessionId":"old","cwd":"/different-worktree","uuid":"user1","message":{"role":"user","content":"Old question"}}` + "\n")
	path := filepath.Join(native, "old.jsonl")
	os.WriteFile(path, log, 0600)
	old := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(path, old, old)
	os.WriteFile(filepath.Join(native, "old", "subagents", "child.jsonl"), log, 0600)
	n.Store.Put("sessions", "extra", store.Session{ID: "extra", Agent: "claude", Source: "log"})
	n.Store.Put("sessions", "mobile", store.Session{ID: "mobile", Agent: "claude", Source: "runtime", Managed: true})
	f := &Follower{Normalizer: n, Roots: []Root{{"claude", root + string(os.PathSeparator), "log"}}, ClaudeIndex: index, Lookback: 72 * time.Hour, HistoryBytes: 8 << 20}
	f.Scan()
	ss, err := n.Store.Sessions()
	if err != nil || len(ss) != 2 || n.Store.LastSeq() != 0 {
		t.Fatalf("catalog must list old chat without eager history: count=%d seq=%d err=%v", len(ss), n.Store.LastSeq(), err)
	}
	var s store.Session
	n.Store.Get("sessions", "old", &s)
	if s.Title != "Desktop title" || s.Project != project {
		t.Fatal("desktop title/project lost")
	}
	if err = f.LoadClaudeHistory("old"); err != nil {
		t.Fatal(err)
	}
	if n.Store.LastSeq() != 1 {
		t.Fatal("selected old history missing or child included")
	}
	n.Store.Get("sessions", "old", &s)
	if s.Title != "Desktop title" || s.Project != project || s.Updated != time.UnixMilli(2000).UTC().Format(time.RFC3339Nano) {
		t.Fatal("log overwrote catalog")
	}
	f.Scan()
	if n.Store.LastSeq() != 1 {
		t.Fatal("duplicate history")
	}
	revision := n.Store.SessionCatalogRevision()
	writeMeta("Renamed", true)
	f.claude.checked = time.Time{}
	f.Scan()
	ss, _ = n.Store.Sessions()
	if len(ss) != 1 || ss[0].ID != "mobile" || n.Store.SessionCatalogRevision() <= revision {
		t.Fatal("archive did not reconcile sidebar")
	}
	if err = f.LoadClaudeHistory("old"); err == nil {
		t.Fatal("archived history allowed")
	}
	// A partially written desktop file must not empty or corrupt the last catalog.
	os.WriteFile(metaPath, []byte("{"), 0600)
	f.claude.checked = time.Time{}
	f.Scan()
	if f.claude.entries["old"].Title != "Renamed" {
		t.Fatal("corrupt catalog replaced last valid state")
	}
}

func TestClaudeMissingLogIsListedAndExplained(t *testing.T) {
	n := setup(t)
	index := t.TempDir()
	b, _ := json.Marshal(claudeDesktopRecord{SessionID: "local_missing", CLI: "missing", CWD: t.TempDir(), Title: "Missing history", Activity: 1000})
	os.WriteFile(filepath.Join(index, "local_missing.json"), b, 0600)
	f := &Follower{Normalizer: n, ClaudeIndex: index}
	f.Scan()
	ss, _ := n.Store.Sessions()
	if len(ss) != 1 || ss[0].ID != "missing" {
		t.Fatal("missing log disappeared from catalog")
	}
	if err := f.LoadClaudeHistory("missing"); err == nil {
		t.Fatal("missing history falsely succeeded")
	}
}
