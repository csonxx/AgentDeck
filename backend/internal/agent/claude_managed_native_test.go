package agent

import (
	"agentdeck/internal/store"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Uses the actual CLI against an isolated local provider, never home credentials
// or business sessions. Exercises our production launch rather than the wrapper.
func TestRealClaudeManagedCLIStreamingAndResume(t *testing.T) {
	binary := os.Getenv("AGENTDECK_REAL_CLAUDE_MANAGED_BINARY")
	if binary == "" {
		t.Skip("explicit real CLI with synthetic local provider only")
	}
	n := setup(t)
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	bin := t.TempDir()
	if err := os.Symlink(binary, filepath.Join(bin, "claude")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		if strings.HasPrefix(k, "ANTHROPIC_") || strings.HasPrefix(k, "CLAUDE_") {
			t.Setenv(k, "")
		}
	}
	configRoot := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
	t.Setenv("ANTHROPIC_API_KEY", "synthetic-local-provider")
	t.Setenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1")
	t.Setenv("DISABLE_TELEMETRY", "1")
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var mu sync.Mutex
	calls := 0
	contextKept := false
	peer := false
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "messages") {
			http.NotFound(w, r)
			return
		}
		var d map[string]any
		if json.NewDecoder(r.Body).Decode(&d) != nil {
			w.WriteHeader(400)
			return
		}
		b, _ := json.Marshal(d["messages"])
		mu.Lock()
		calls++
		count := calls
		if count >= 2 && strings.Contains(string(b), "managed-first") {
			contextKept = true
		}
		peer = peer || strings.Contains(string(b), "This came from another Claude session")
		mu.Unlock()
		if count == 1 {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		msg := map[string]any{"id": fmt.Sprintf("msg_managed_%d", count), "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 1, "output_tokens": 1}}
		w.Header().Set("Content-Type", "text/event-stream")
		frames := []map[string]any{{"type": "message_start", "message": msg}, {"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}}, {"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": "MANAGED_NATIVE_OK"}}, {"type": "content_block_stop", "index": 0}, {"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 1}}, {"type": "message_stop"}}
		for _, frame := range frames {
			b, _ := json.Marshal(frame)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", frame["type"], b)
		}
	}))
	defer provider.Close()
	t.Setenv("ANTHROPIC_BASE_URL", provider.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := &Manager{Store: n.Store, Normalizer: n, Context: ctx, Roots: []string{root}, ClaudeProcesses: filepath.Join(configRoot, "sessions")}
	defer m.Close()
	ss, err := m.Create("claude", digest(root)[:24], "")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.SendDelivery(ss.ID, "managed-first", UUID(), TurnOptions{}, nil, "send"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(25 * time.Second):
		t.Fatal("native CLI did not reach isolated provider")
	}
	req := UUID()
	if err = m.SendDelivery(ss.ID, "managed-second", req, TurnOptions{}, nil, "send"); err != nil {
		t.Fatal(err)
	}
	q, _ := m.Queue(ss.ID)
	if len(q) != 0 {
		t.Fatal("normal native input was diverted into a workstation queue")
	}
	once.Do(func() { close(release) })
	wait := func() {
		t.Helper()
		end := time.Now().Add(25 * time.Second)
		for time.Now().Before(end) {
			var current store.Session
			n.Store.Get("sessions", ss.ID, &current)
			if current.Status == "completed" {
				return
			}
			if current.Status == "failed" {
				t.Fatal("native managed turn failed")
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatal("native managed turn timed out")
	}
	wait()
	q, _ = m.Queue(ss.ID)
	if len(q) != 0 {
		t.Fatal("queued native input did not drain")
	}
	// Simulate an independently owned, idle native executor: keep it alive,
	// detach it from our runtime table, and let an App send acquire the original
	// ID through its real native status registry. No peer input or copied log.
	m.mu.Lock()
	r := m.runs[ss.ID]
	delete(m.runs, ss.ID)
	m.mu.Unlock()
	owner, err := m.claudeHandoffOwner(ss.ID, 0)
	if err != nil || owner == nil || owner.Status != "idle" || owner.PID != r.cmd.Process.Pid {
		r.close()
		t.Fatal("real native idle owner registry unavailable", err)
	}
	request := UUID()
	if err = m.SendDelivery(ss.ID, "managed-after-handoff", request, TurnOptions{}, nil, "send"); err != nil {
		t.Fatal(err)
	}
	q, _ = m.Queue(ss.ID)
	if len(q) != 0 {
		t.Fatal("idle original send was diverted into workstation queue")
	}
	wait()
	select {
	case <-r.done:
	default:
		t.Fatal("original idle executor was not released")
	}
	q, _ = m.Queue(ss.ID)
	if len(q) != 0 {
		t.Fatal("original-ID handoff did not drain")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 3 || !contextKept || peer {
		t.Fatalf("native path failed calls=%d context=%v peer=%v", calls, contextKept, peer)
	}
	list, _ := m.Store.Sessions()
	if len(list) != 1 || list[0].ID != ss.ID {
		t.Fatal("managed resume changed original identity")
	}
}
