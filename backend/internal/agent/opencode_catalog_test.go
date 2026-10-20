package agent

import (
	"agentdeck/internal/store"
	"testing"
)

func TestOpenCodeChildrenStayOutOfCatalogAndKeepHistory(t *testing.T) {
	m := connectorManager(t)
	o := newOpenCode(m, HTTPAgentConfig{})
	project := m.Roots[0]
	root := map[string]any{"id": "root", "directory": project, "title": "Main chat"}
	if _, err := o.importSession(root); err != nil {
		t.Fatal(err)
	}
	// Simulate an older Local that already exposed a child as a normal chat.
	old := newExternalSession("child", "opencode", project, "opencode-api")
	if err := m.Store.Put("sessions", old.ID, old); err != nil {
		t.Fatal(err)
	}
	revision := m.Store.SessionCatalogRevision()
	child := map[string]any{"id": "child", "directory": project, "title": "Explore", "parentID": "root"}
	ss, err := o.importSession(child)
	if err != nil || ss.Parent != "opencode/root" || !ss.CatalogHidden {
		t.Fatalf("child metadata: %v", err)
	}
	if m.Store.SessionCatalogRevision() <= revision {
		t.Fatal("metadata-only correction did not publish catalog revision")
	}
	revision = m.Store.SessionCatalogRevision()
	if _, err = o.importSession(child); err != nil || m.Store.SessionCatalogRevision() != revision {
		t.Fatal("unchanged import bumped catalog")
	}
	o.event(m.Context, map[string]any{"type": "session.status", "properties": map[string]any{"sessionID": "child", "status": map[string]any{"type": "busy"}}})
	o.part(ss, map[string]any{"id": "child-output", "type": "text", "text": "Child result"}, "assistant")
	list, err := m.Store.Sessions()
	if err != nil || len(list) != 1 || list[0].ID != "opencode/root" {
		t.Fatal("child inflated project/recent chat count", err)
	}
	history, err := m.Store.History(ss.ID, 0, 40)
	if err != nil || len(history.Events) != 1 || history.Events[0].Text != "Child result" {
		t.Fatal("child history lost", err)
	}
	// Descendants use the same native rule, independently of import ordering.
	o.event(m.Context, map[string]any{"type": "session.created", "properties": map[string]any{"info": map[string]any{"id": "grandchild", "directory": project, "parentID": "child"}}})
	var nested store.Session
	if m.Store.Get("sessions", "opencode/grandchild", &nested) != nil || nested.Parent != "opencode/child" || !nested.CatalogHidden {
		t.Fatal("nested child exposed")
	}
	// A similarly named ordinary chat is still visible; do not guess from titles.
	if _, err = o.importSession(map[string]any{"id": "ordinary", "directory": project, "title": "Explore (@agent subagent)"}); err != nil {
		t.Fatal(err)
	}
	list, _ = m.Store.Sessions()
	if len(list) != 2 {
		t.Fatal("title-based filtering removed user chat")
	}
}

func TestOpenCodeArchiveVisibilityCanBeRestored(t *testing.T) {
	m := connectorManager(t)
	o := newOpenCode(m, HTTPAgentConfig{})
	info := map[string]any{"id": "archived", "directory": m.Roots[0], "time": map[string]any{"archived": float64(123)}}
	ss, err := o.importSession(info)
	if err != nil || !ss.CatalogHidden {
		t.Fatal("archive exposed", err)
	}
	info["time"] = map[string]any{}
	ss, err = o.importSession(info)
	if err != nil || ss.CatalogHidden {
		t.Fatal("unarchive stayed hidden", err)
	}
}

func TestOpenCodeDeletedSessionLeavesVisibleCatalog(t *testing.T) {
	m := connectorManager(t)
	o := newOpenCode(m, HTTPAgentConfig{})
	ss, err := o.importSession(map[string]any{"id": "owned", "directory": m.Roots[0]})
	if err != nil {
		t.Fatal(err)
	}
	o.part(ss, map[string]any{"id": "output", "type": "text", "text": "kept"}, "assistant")
	o.event(m.Context, map[string]any{"type": "session.deleted", "properties": map[string]any{"info": map[string]any{"id": "owned"}}})
	list, err := m.Store.Sessions()
	if err != nil || len(list) != 0 {
		t.Fatal("deleted native chat remains visible", err)
	}
	h, err := m.Store.History(ss.ID, 0, 40)
	if err != nil || len(h.Events) != 1 {
		t.Fatal("deleting sidebar metadata erased encrypted history", err)
	}
}
