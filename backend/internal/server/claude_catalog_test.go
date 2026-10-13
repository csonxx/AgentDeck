package server

import (
	"agentdeck/internal/store"
	"testing"
)

func TestClaudeMetadataChangesPushWithoutNewEvent(t *testing.T) {
	s, ts := setupServer(t)
	entry := store.Session{ID: "claude-s", Agent: "claude", Source: "claude-desktop", Title: "Old title", Project: "/project"}
	if err := s.Store.SyncClaudeCatalog([]store.Session{entry}); err != nil {
		t.Fatal(err)
	}
	p := pairPhone(t, s, ts)
	p.connect(t, ts, s)
	p.read(t, "sessions")
	entry.Title = "Renamed on desktop"
	if err := s.Store.SyncClaudeCatalog([]store.Session{entry}); err != nil {
		t.Fatal(err)
	}
	items := p.read(t, "sessions")["sessions"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["title"] != entry.Title || s.Store.LastSeq() != 0 {
		t.Fatal("metadata-only rename failed to reach encrypted phone stream")
	}
	entry.CatalogHidden = true
	s.Store.SyncClaudeCatalog([]store.Session{entry})
	items = p.read(t, "sessions")["sessions"].([]any)
	if len(items) != 0 {
		t.Fatal("archive not pushed")
	}
}
