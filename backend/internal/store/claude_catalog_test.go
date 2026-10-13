package store

import "testing"

func TestClaudeHistoryCannotReplaceDesktopMetadataWithOldExecutionState(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entry := Session{ID: "s", Agent: "claude", Source: "claude-desktop", Title: "Desktop title", Project: "/project", Updated: "2026-10-10T12:00:00Z", Status: "idle"}
	if err = s.SyncClaudeCatalog([]Session{entry}); err != nil {
		t.Fatal(err)
	}
	for _, at := range []string{"2020-01-01T00:00:00Z", ""} {
		if _, _, err = s.AppendCatalogHistory(Event{ID: "old/" + at, Session: "s", Agent: "claude", Source: "log", Kind: "status", Status: "running", Time: at}); err != nil {
			t.Fatal(err)
		}
	}
	var got Session
	s.Get("sessions", "s", &got)
	if got != entry {
		t.Fatal("old or undated history revived an old run")
	}
	if _, _, err = s.AppendCatalogHistory(Event{ID: "new", Session: "s", Agent: "claude", Source: "log", Kind: "status", Status: "running", Time: "2026-10-10T12:01:00Z"}); err != nil {
		t.Fatal(err)
	}
	s.Get("sessions", "s", &got)
	entry.Status = "running"
	if got != entry {
		t.Fatal("live status lost or desktop metadata overwritten")
	}
}

func TestClaudeCatalogRevisionAndHistoryPreservation(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Append(Event{ID: "a", Session: "s", Agent: "claude", Source: "log", Kind: "message", Role: "user", Text: "original"})
	entry := Session{ID: "s", Agent: "claude", Source: "claude-desktop", Title: "Desktop title", Project: "/project", Updated: "2026-01-01T00:00:00Z", Status: "idle"}
	if err = s.SyncClaudeCatalog([]Session{entry}); err != nil {
		t.Fatal(err)
	}
	r := s.SessionCatalogRevision()
	if r == 0 {
		t.Fatal("no catalog revision")
	}
	if err = s.SyncClaudeCatalog([]Session{entry}); err != nil || s.SessionCatalogRevision() != r {
		t.Fatal("unchanged catalog bumped revision", err)
	}
	s.AppendCatalogHistory(Event{ID: "b", Session: "s", Agent: "claude", Source: "log", Kind: "message", Role: "user", Text: "older", Time: "2020-01-01T00:00:00Z"})
	var got Session
	s.Get("sessions", "s", &got)
	if got != entry {
		t.Fatal("hydration changed metadata")
	}
	if err = s.SyncClaudeCatalog(nil); err != nil {
		t.Fatal(err)
	}
	ss, _ := s.Sessions()
	if len(ss) != 0 {
		t.Fatal("deleted desktop record still visible")
	}
	events, err := s.Events("s", 0, 100)
	if err != nil || len(events) != 2 {
		t.Fatal("hidden session lost history", err)
	}
	if err = s.SyncClaudeCatalog([]Session{{ID: "bad", Agent: "codex"}}); err == nil || s.SessionCatalogRevision() != r+1 {
		t.Fatal("invalid catalog not atomic")
	}
}
