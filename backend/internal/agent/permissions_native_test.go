package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agentdeck/internal/claudedesktop"
	"agentdeck/internal/store"
)

// Explicit acceptance uses installed CLIs with isolated settings and no real
// provider credentials. No business session, home setting or permission changes.
func TestRealCodexPermissionDefaultsWithoutInference(t *testing.T) {
	binary := os.Getenv("AGENTDECK_PERMISSION_CODEX_BINARY")
	if binary == "" {
		t.Skip("explicit isolated native permission acceptance only")
	}
	for _, tc := range []struct{ approval, sandbox, nativeSandbox, project string }{
		{"never", "danger-full-access", "dangerFullAccess", ""},
		{"on-request", "workspace-write", "workspaceWrite", ""},
		{"on-request", "read-only", "readOnly", ""},
		{"never", "danger-full-access", "workspaceWrite", "trusted-project"},
	} {
		t.Run(tc.sandbox+"/"+tc.project, func(t *testing.T) {
			bin, home := t.TempDir(), t.TempDir()
			if err := os.Symlink(binary, filepath.Join(bin, "codex")); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("CODEX_HOME", home)
			m := connectorManager(t)
			project, err := filepath.EvalSymlinks(m.Projects()[0].Path)
			if err != nil {
				t.Fatal(err)
			}
			config := fmt.Sprintf("approval_policy = %q\nsandbox_mode = %q\nmodel_provider = \"isolated\"\n[model_providers.isolated]\nname = \"Isolated\"\nbase_url = \"http://127.0.0.1:9/v1\"\nwire_api = \"responses\"\nrequires_openai_auth = false\n", tc.approval, tc.sandbox)
			expectedApproval := tc.approval
			if tc.project != "" {
				config += fmt.Sprintf("\n[projects.%q]\ntrust_level = \"trusted\"\n", project)
				dir := filepath.Join(project, ".codex")
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("approval_policy = \"on-request\"\nsandbox_mode = \"workspace-write\"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				expectedApproval = "on-request"
			}
			if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			r, err := m.launch(store.Session{ID: UUID(), Agent: "codex", Project: project}, false)
			if err != nil {
				t.Fatal(err)
			}
			defer r.close()
			// A thread without a turn has no persisted rollout to resume. Read
			// effective permissions directly from start, without model inference.
			v, err := r.call("thread/start", map[string]any{"cwd": project})
			if err != nil {
				t.Fatal(err)
			}
			if str(v["approvalPolicy"]) != expectedApproval || str(obj(v["sandbox"])["type"]) != tc.nativeSandbox {
				t.Fatal("native effective policy differs from isolated settings")
			}
			t.Logf("native approval=%s sandbox=%s project=%s, no inference", expectedApproval, tc.nativeSandbox, tc.project)
		})
	}
}

func TestRealClaudePermissionDefaultsWithIsolatedTool(t *testing.T) {
	binary := os.Getenv("AGENTDECK_PERMISSION_CLAUDE_BINARY")
	if binary == "" {
		t.Skip("explicit isolated native permission acceptance only")
	}
	for _, tc := range []struct {
		global, project string
		prompts         int
	}{
		{"acceptEdits", "", 0},
		{"bypassPermissions", "", 0},
		{"manual", "acceptEdits", 0},
		{"acceptEdits", "manual", 1},
	} {
		t.Run(tc.global+"/"+tc.project, func(t *testing.T) {
			bin, home := t.TempDir(), t.TempDir()
			if err := os.Symlink(binary, filepath.Join(bin, "claude")); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			for _, env := range os.Environ() {
				key, _, _ := strings.Cut(env, "=")
				if strings.HasPrefix(key, "ANTHROPIC_") || strings.HasPrefix(key, "CLAUDE_") {
					t.Setenv(key, "")
				}
			}
			t.Setenv("CLAUDE_CONFIG_DIR", home)
			t.Setenv("ANTHROPIC_API_KEY", "isolated-synthetic-provider")
			t.Setenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1")
			t.Setenv("DISABLE_TELEMETRY", "1")
			settings := func(dir, mode string) {
				t.Helper()
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				b, _ := json.Marshal(map[string]any{"permissions": map[string]any{"defaultMode": mode}, "disableAllHooks": true})
				if err := os.WriteFile(filepath.Join(dir, "settings.json"), b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			settings(home, tc.global)
			m := connectorManager(t)
			project := m.Projects()[0].Path
			if tc.project != "" {
				settings(filepath.Join(project, ".claude"), tc.project)
			}
			target := filepath.Join(project, "synthetic-permission-probe.txt")
			var calls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.Path, "messages") {
					http.NotFound(w, r)
					return
				}
				first := calls.Add(1) == 1
				msg := map[string]any{"id": "msg_policy", "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 1, "output_tokens": 1}}
				frames := []map[string]any{{"type": "message_start", "message": msg}}
				stop := "end_turn"
				if first {
					input, _ := json.Marshal(map[string]string{"file_path": target, "content": "SYNTHETIC_NATIVE_POLICY"})
					frames = append(frames, map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "tool_policy", "name": "Write", "input": map[string]any{}}}, map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(input)}})
					stop = "tool_use"
				} else {
					frames = append(frames, map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}}, map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": "POLICY_NATIVE_OK"}})
				}
				frames = append(frames, map[string]any{"type": "content_block_stop", "index": 0}, map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 1}}, map[string]any{"type": "message_stop"})
				w.Header().Set("Content-Type", "text/event-stream")
				for _, frame := range frames {
					b, _ := json.Marshal(frame)
					fmt.Fprintf(w, "event: %s\ndata: %s\n\n", frame["type"], b)
				}
			}))
			defer provider.Close()
			t.Setenv("ANTHROPIC_BASE_URL", provider.URL)
			ss, err := m.Create("claude", m.Projects()[0].ID, "")
			if err != nil {
				t.Fatal(err)
			}
			if err = m.Send(ss.ID, "Run the synthetic tool supplied by the isolated provider.", UUID()); err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			ctx, cancel := context.WithTimeout(m.Context, 40*time.Second)
			defer cancel()
			completed := false
			for ctx.Err() == nil && !completed {
				events, _ := m.Store.Events(ss.ID, 0, 100)
				for _, ev := range events {
					if ev.Kind == "approval" && ev.Status == "pending" && !seen[ev.Item] {
						seen[ev.Item] = true
						if err := m.Approve(ss.ID, ev.Item, "decline", ""); err != nil {
							t.Fatal(err)
						}
					}
					if ev.Kind == "status" && ev.Status == "completed" {
						completed = true
					}
				}
				time.Sleep(20 * time.Millisecond)
			}
			if !completed || len(seen) != tc.prompts {
				t.Fatalf("native policy mismatch: completed=%v prompts=%d expected=%d", completed, len(seen), tc.prompts)
			}
			_, err = os.Stat(target)
			if (err == nil) != (tc.prompts == 0) {
				t.Fatal("native permission decision and file result disagree")
			}
			t.Logf("global=%s project=%s native prompts=%d synthetic tool result verified", tc.global, tc.project, len(seen))
		})
	}
}

func TestRealCodexPermissionEditingWithoutInference(t *testing.T) {
	binary := os.Getenv("AGENTDECK_PERMISSION_CODEX_BINARY")
	if binary == "" {
		t.Skip("explicit isolated native permission acceptance only")
	}
	bin, home := t.TempDir(), t.TempDir()
	if e := os.Symlink(binary, filepath.Join(bin, "codex")); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CODEX_HOME", home)
	m := connectorManager(t)
	ss, e := m.CreatePermission("codex", m.Projects()[0].ID, "", "read-only")
	if e != nil {
		t.Fatal(e)
	}
	before, e := m.SessionPermissions(ss.ID)
	if e != nil || before.Mode != "read-only" {
		t.Fatal("initial native permissions mismatch", e, before.Mode)
	}
	for _, mode := range []string{"on-request", "auto_review", "full-access", "read-only"} {
		actual, e := m.ChangePermissions(ss.ID, before.Version, mode)
		if e != nil {
			t.Fatal(mode, e)
		}
		if actual.Mode != mode || actual.Source != "live" {
			t.Fatal("unconfirmed native mode")
		}
		before = actual
	}
	if _, e = m.ChangePermissions(ss.ID, "stale", "full-access"); e == nil {
		t.Fatal("stale permission edit accepted")
	}
	t.Log("isolated installed Codex: four native permission updates confirmed, no inference")
}
func TestRealClaudePermissionEditingWithoutInference(t *testing.T) {
	binary := os.Getenv("AGENTDECK_PERMISSION_CLAUDE_BINARY")
	if binary == "" {
		t.Skip("explicit isolated native permission acceptance only")
	}
	bin, home := t.TempDir(), t.TempDir()
	if e := os.Symlink(binary, filepath.Join(bin, "claude")); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, env := range os.Environ() {
		k, _, _ := strings.Cut(env, "=")
		if strings.HasPrefix(k, "ANTHROPIC_") || strings.HasPrefix(k, "CLAUDE_") {
			t.Setenv(k, "")
		}
	}
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	t.Setenv("ANTHROPIC_API_KEY", "isolated")
	t.Setenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1")
	t.Setenv("DISABLE_TELEMETRY", "1")
	t.Setenv("DISABLE_ERROR_REPORTING", "1")
	m := connectorManager(t)
	ss, e := m.Create("claude", m.Projects()[0].ID, "")
	if e != nil {
		t.Fatal(e)
	}
	before, e := m.SessionPermissions(ss.ID)
	if e != nil || before.Mode != "default" {
		t.Fatal("initial native permissions mismatch", e, before.Mode)
	}
	for _, mode := range []string{"acceptEdits", "bypassPermissions", "plan", "dontAsk", "default"} {
		actual, e := m.ChangePermissions(ss.ID, before.Version, mode)
		if e != nil {
			t.Fatal(mode, e)
		}
		if actual.Mode != mode {
			t.Fatal("wrong permission acknowledgement")
		}
		before = actual
	}
	t.Log("isolated installed Claude: five native permission changes acknowledged, no inference")
}

func TestRealClaudeDesktopPermissionReadbackAndRestore(t *testing.T) {
	path := os.Getenv("AGENTDECK_PERMISSION_DESKTOP_TEST_FILE")
	if path == "" {
		t.Skip("explicit idle synthetic Desktop test session only")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var meta claudeDesktopRecord
	if json.Unmarshal(raw, &meta) != nil || !strings.HasPrefix(meta.Title, "AD_") {
		t.Fatal("not an isolated acceptance session")
	}
	project := meta.Origin
	if project == "" {
		project = meta.CWD
	}
	target := claudedesktop.Target{Host: meta.SessionID, Native: meta.CLI, CWD: meta.CWD, Project: project}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b, e := claudedesktop.Open(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	state, e := b.State(ctx, target)
	if e != nil || state.Running || len(state.Permissions) > 0 {
		t.Fatal("acceptance session is not idle", e)
	}
	original, e := b.PermissionMode(ctx, target)
	if e != nil || !validPermissionMode("claude", original) {
		t.Fatal("native permission mode unavailable", e)
	}
	defer func() {
		restoreCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		actual, e := b.SetPermissionMode(restoreCtx, target, original)
		if e != nil || actual != original {
			t.Error("original Desktop mode not restored", e)
		}
	}()
	selected := "acceptEdits"
	if original == selected {
		selected = "default"
	}
	actual, e := b.SetPermissionMode(ctx, target, selected)
	if e != nil || actual != selected {
		t.Fatal("Desktop permission readback mismatch", e, actual)
	}
	t.Log("idle acceptance Desktop original session permission changed and read back; no messages, restored")
}

func TestRealOpenCodePermissionEditingWithoutInference(t *testing.T) {
	endpoint := os.Getenv("AGENTDECK_PERMISSION_OPENCODE_URL")
	if endpoint == "" {
		t.Skip("explicit native server metadata acceptance only")
	}
	m := connectorManager(t)
	c := newOpenCode(m, HTTPAgentConfig{URL: endpoint})
	m.Connectors = map[string]Connector{"opencode": c}
	ss, e := m.Create("opencode", m.Projects()[0].ID, "")
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if e := c.request(context.Background(), "DELETE", "/session/"+nativeID(ss), ss.Project, nil, nil); e != nil {
			t.Error("test session cleanup failed", e)
		}
	}()
	p, e := m.SessionPermissions(ss.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"ask", "allow", "deny"} {
		p, e = m.ChangePermissions(ss.ID, p.Version, mode)
		if e != nil || p.Mode != mode {
			t.Fatal("native session rules not applied", mode, p.Mode, e)
		}
	}
	t.Log("installed OpenCode server: three native rule changes read back; empty test chat removed, no inference")
}
