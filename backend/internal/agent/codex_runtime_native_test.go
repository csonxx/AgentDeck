package agent

import (
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

// Opt-in native lifecycle acceptance uses an isolated HOME and CODEX_HOME,
// a loopback-only synthetic Responses provider, and no account credentials.
func TestRealCodexOriginalWriterHandoff(t *testing.T) {
	binary := os.Getenv("AGENTDECK_OWNERSHIP_CODEX_BINARY")
	if binary == "" {
		t.Skip("explicit isolated native ownership acceptance only")
	}
	bin, home := t.TempDir(), t.TempDir()
	if err := os.Symlink(binary, filepath.Join(bin, "codex")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, entry := range os.Environ() {
		k, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(k, "OPENAI_") || strings.HasPrefix(k, "CODEX_") || strings.HasPrefix(k, "ANTHROPIC_") || strings.HasPrefix(k, "CLAUDE_") {
			t.Setenv(k, "")
		}
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", home)
	var mu sync.Mutex
	calls := 0
	retained := false
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" || r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			w.WriteHeader(400)
			return
		}
		raw, _ := json.Marshal(body["input"])
		mu.Lock()
		calls++
		n := calls
		if n == 2 {
			retained = strings.Contains(string(raw), "ownership-first")
		}
		mu.Unlock()
		message := map[string]any{"id": fmt.Sprintf("msg_%d", n), "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "NATIVE_OWNERSHIP_OK", "annotations": []any{}}}}
		response := map[string]any{"id": fmt.Sprintf("resp_%d", n), "object": "response", "created_at": 1, "status": "completed", "model": "fixture-model", "output": []any{message}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}
		frames := []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": response["id"], "object": "response", "created_at": 1, "status": "in_progress", "output": []any{}}},
			{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": message["id"], "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}},
			{"type": "response.content_part.added", "output_index": 0, "content_index": 0, "item_id": message["id"], "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}},
			{"type": "response.output_text.delta", "output_index": 0, "content_index": 0, "item_id": message["id"], "delta": "NATIVE_OWNERSHIP_OK"},
			{"type": "response.output_item.done", "output_index": 0, "item": message},
			{"type": "response.completed", "response": response},
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for i, frame := range frames {
			frame["sequence_number"] = i
			b, _ := json.Marshal(frame)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", frame["type"], b)
		}
	}))
	defer provider.Close()
	config := fmt.Sprintf("model = \"fixture-model\"\napproval_policy = \"never\"\nsandbox_mode = \"read-only\"\nmodel_provider = \"isolated\"\n[model_providers.isolated]\nname = \"Isolated\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nsupports_websockets = false\n[analytics]\nenabled = false\n", provider.URL+"/v1")
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	m := connectorManager(t)
	s, err := m.Create("codex", m.Projects()[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	original := m.liveRuntime(s.ID)
	if err = m.Send(s.ID, "ownership-first", UUID()); err != nil {
		t.Fatal(err)
	}
	awaitWriterExit(t, m, s.ID, original)
	// An independent native app-server now takes the very same persisted ID.
	// A retained native writer would make this real thread/resume fail.
	owner, err := m.launch(s, true)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	resumed, err := owner.call("thread/resume", map[string]any{"threadId": s.ID, "cwd": s.Project})
	if err != nil {
		t.Fatal("native second writer could not acquire original", err)
	}
	if str(obj(resumed["thread"])["id"]) != s.ID {
		t.Fatal("handoff changed native ID")
	}
	owner.mu.Lock()
	owner.busy = true
	owner.mu.Unlock()
	if _, err = owner.call("turn/start", map[string]any{"threadId": s.ID, "input": []any{map[string]any{"type": "text", "text": "ownership-second"}}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		owner.mu.Lock()
		busy := owner.busy
		owner.mu.Unlock()
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second native turn did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	count, contextRetained := calls, retained
	mu.Unlock()
	if count != 2 || !contextRetained {
		t.Fatalf("native history not preserved calls=%d retained=%t", count, contextRetained)
	}
	t.Log("installed Codex: completed original writer exited; independent executor resumed same ID and completed with original context; local provider calls=2")
}
