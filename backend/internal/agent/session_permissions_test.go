package agent

import (
	"agentdeck/internal/store"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestPermissionViewReportsNativeReviewerAndCustom(t *testing.T) {
	for _, reviewer := range []string{"auto_review", "guardian_subagent"} {
		v := codexPermissionParams("auto_review", "/authorized")
		v["approvalsReviewer"] = reviewer
		p := permissionView("codex", v, "live")
		if p.Mode != "auto_review" || p.Detail == "" {
			t.Fatal("native auto reviewer lost")
		}
	}
	p := permissionView("codex", map[string]any{"approvalPolicy": "untrusted", "sandboxPolicy": map[string]any{"type": "readOnly"}}, "live")
	if p.Mode != "custom" {
		t.Fatal("custom permissions misrepresented")
	}
	p = permissionView("claude", map[string]any{"mode": "manual"}, "live")
	if p.Mode != "default" {
		t.Fatal("native manual alias lost")
	}
}
func TestNativeDesktopPermissionMutationPreservesSessionAndReadback(t *testing.T) {
	after := codexPermissionParams("auto_review", "/authorized")
	bridge, done := queueFixture(t, nil, "permissions", func(c net.Conn, req map[string]any) error {
		if req["method"] != "thread-follower-update-thread-settings" || req["version"] != float64(2) || req["targetClientId"] != "owner" {
			return fmt.Errorf("wrong native permission endpoint")
		}
		p := obj(req["params"])
		if p["conversationId"] != "original" || obj(p["threadSettings"])["approvalsReviewer"] != "auto_review" {
			return fmt.Errorf("wrong native permission target")
		}
		if e := queueAck(c, req, map[string]any{"applied": true}); e != nil {
			return e
		}
		if _, e := readDesktopFrame(c); e != nil {
			return e
		}
		s := desktopTestState("original", "/authorized", "Fixture", "inProgress")
		s["latestThreadSettings"] = after
		if e := writeDesktopTest(c, map[string]any{"method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"hostId": "local", "conversationId": "original", "change": map[string]any{"type": "snapshot", "conversationState": s}}}); e != nil {
			return e
		}
		_, e := readDesktopFrame(c)
		return e
	})
	version := permissionView("codex", desktopPermissionState(map[string]any{}), "live").Version
	actual, e := bridge.SetPermissions(context.Background(), "original", "/authorized", version, "auto_review")
	if e != nil {
		t.Fatal(e)
	}
	if actual.Mode != "auto_review" {
		t.Fatal("native permission not read back")
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
}
func TestOpenCodePermissionRulesReadModifyAndRecheck(t *testing.T) {
	m := connectorManager(t)
	project := m.Projects()[0].Path
	rules := []any{map[string]any{"permission": "bash", "pattern": "*", "action": "ask"}}
	var patches int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session/native" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("directory") != project {
			t.Error("project lost")
		}
		if r.Method == "PATCH" {
			patches++
			var v map[string]any
			json.NewDecoder(r.Body).Decode(&v)
			rules = append(rules, arr(v["permission"])...)
			if len(v) != 1 {
				t.Error("unrelated session field modified")
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"permission": rules})
	}))
	defer server.Close()
	c := newOpenCode(m, HTTPAgentConfig{URL: server.URL})
	m.Connectors = map[string]Connector{"opencode": c}
	s := newExternalSession("native", "opencode", project, "opencode-api")
	s.Managed = true
	m.Store.Put("sessions", s.ID, s)
	p, e := m.SessionPermissions(s.ID)
	if e != nil || p.Mode != "custom" {
		t.Fatal(e, p.Mode)
	}
	p, e = m.ChangePermissions(s.ID, p.Version, "allow")
	if e != nil || p.Mode != "allow" || patches != 1 {
		t.Fatal(e, p.Mode, patches)
	}
	if _, e = m.ChangePermissions(s.ID, "stale", "deny"); e == nil || patches != 1 {
		t.Fatal("stale edit changed native rules")
	}
}

func TestACPPermissionsUseOnlyAdvertisedNativeModes(t *testing.T) {
	m := connectorManager(t)
	fixture, _ := filepath.Abs("../../../scripts/fixture_acp.py")
	c := newACP(m, "qoder", ACPConfig{Command: "python3", Args: []string{fixture, "--permissions"}})
	m.Connectors = map[string]Connector{"qoder": c}
	ss, e := m.Create("qoder", m.Projects()[0].ID, "")
	if e != nil {
		t.Fatal(e)
	}
	p, e := m.SessionPermissions(ss.ID)
	if e != nil || p.Mode != "ask" {
		t.Fatal(e, p.Mode)
	}
	p, e = m.ChangePermissions(ss.ID, p.Version, "auto")
	if e != nil || p.Mode != "auto" {
		t.Fatal(e, p.Mode)
	}
	if _, e = m.ChangePermissions(ss.ID, p.Version, "bypassPermissions"); e == nil {
		t.Fatal("invented ACP permission mode accepted")
	}
}

func TestCodexPermissionPollingNeverResumesStoredSession(t *testing.T) {
	m := connectorManager(t)
	ss := store.Session{ID: "not-loaded", Agent: "codex", Source: "stream", Managed: true, Project: m.Projects()[0].Path, Status: "completed"}
	if err := m.Store.Put("sessions", ss.ID, ss); err != nil {
		t.Fatal(err)
	}
	// A missing command would fail if a passive read attempted to launch it.
	t.Setenv("PATH", t.TempDir())
	for i := 0; i < 3; i++ {
		got, err := m.SessionPermissions(ss.ID)
		if err != nil || got.Source != "unknown" || got.Editable {
			t.Fatalf("passive read: %v %s", err, got.Source)
		}
	}
	if len(m.runs) != 0 {
		t.Fatal("permission read acquired a runtime")
	}
	var unchanged store.Session
	m.Store.Get("sessions", ss.ID, &unchanged)
	if unchanged != ss {
		t.Fatal("permission read changed the session")
	}
}
