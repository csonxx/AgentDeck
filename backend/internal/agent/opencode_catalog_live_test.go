package agent

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Own blank sessions only: native parent/child API acceptance needs no inference.
func TestRealOpenCodeChildCatalog(t *testing.T) {
	if os.Getenv("AGENTDECK_REAL_CHILDREN") != "1" {
		t.Skip("explicit native child-session acceptance only")
	}
	cfg, err := LoadConnections(filepath.Join(os.Getenv("HOME"), ".agentdeck", "agents.json"))
	if err != nil {
		t.Fatal("connection configuration unavailable")
	}
	m := connectorManager(t)
	o := newOpenCode(m, cfg.OpenCode)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root, err := o.Create(ctx, m.Roots[0], "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if o.request(cleanup, "DELETE", "/session/"+url.PathEscape(nativeID(root)), root.Project, nil, nil) != nil {
			t.Error("owned root cleanup failed")
		}
	}()
	var info map[string]any
	if err = o.request(ctx, "POST", "/session", root.Project, map[string]any{"parentID": nativeID(root), "title": "AD child catalog acceptance"}, &info); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if o.request(cleanup, "DELETE", "/session/"+url.PathEscape(str(info["id"])), root.Project, nil, nil) != nil {
			t.Error("owned child cleanup failed")
		}
	}()
	child, err := o.importSession(info)
	if err != nil || child.Parent != root.ID || !child.CatalogHidden {
		t.Fatal("native child projection failed", err)
	}
	var children []map[string]any
	if err = o.request(ctx, "GET", "/session/"+url.PathEscape(nativeID(root))+"/children", root.Project, nil, &children); err != nil || len(children) != 1 || str(children[0]["id"]) != nativeID(child) {
		t.Fatal("native parent/child relation missing", err)
	}
	list, err := m.Store.Sessions()
	if err != nil || len(list) != 1 || list[0].ID != root.ID {
		t.Fatal("native child inflated visible catalog", err)
	}
	t.Log("native root and child IDs confirmed; visible chat count remains one; no prompt sent")
}
