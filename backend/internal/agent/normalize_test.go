package agent

import (
	"agentdeck/internal/store"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setup(t *testing.T) *Normalizer {
	s, e := store.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return &Normalizer{Store: s}
}
func TestCodexPresentationOnly(t *testing.T) {
	n := setup(t)
	st := State{Session: "c"}
	for _, raw := range []string{`{"type":"response_item","payload":{"type":"message","role":"system","content":[{"type":"text","text":"secret"}]}}`, `{"type":"response_item","payload":{"type":"reasoning","encrypted_content":"private","summary":[]}}`, `{"type":"turn_context","payload":{"secret":"hidden"}}`} {
		out, e := n.Parse([]byte(raw), "codex", "log", &st)
		if e != nil || len(out) != 0 {
			t.Fatal("private context leaked", out, e)
		}
	}
	out, e := n.Parse([]byte(`{"type":"response_item","payload":{"type":"message","role":"assistant","id":"m","content":[{"type":"output_text","text":"Hello **世界**"}]}}`), "codex", "log", &st)
	if e != nil || len(out) != 1 || out[0].Text != "Hello **世界**" {
		t.Fatal(out, e)
	}
}
func TestDeltaAndSnapshotIdentity(t *testing.T) {
	n := setup(t)
	st := State{Session: "c"}
	delta, _ := n.Parse([]byte(`{"method":"item/agentMessage/delta","params":{"threadId":"c","itemId":"m","delta":"Hello"}}`), "codex", "stream", &st)
	snap, _ := n.Parse([]byte(`{"method":"item/completed","params":{"threadId":"c","item":{"type":"agentMessage","id":"m","text":"Hello world"}}}`), "codex", "stream", &st)
	if len(delta) != 1 || len(snap) != 1 || delta[0].Item != snap[0].Item || delta[0].Mode != "delta" || snap[0].Mode != "snapshot" {
		t.Fatal(delta, snap)
	}
	cs := State{}
	_, _ = n.Parse([]byte(`{"type":"stream_event","session_id":"cl","event":{"type":"message_start","message":{"id":"cm"}}}`), "claude", "stream", &cs)
	d, _ := n.Parse([]byte(`{"type":"stream_event","session_id":"cl","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你好"}}}`), "claude", "stream", &cs)
	v, _ := n.Parse([]byte(`{"type":"assistant","session_id":"cl","message":{"id":"cm","role":"assistant","content":[{"type":"text","text":"你好，世界"}]}}`), "claude", "stream", &cs)
	if len(d) != 1 || len(v) != 1 || d[0].Item != v[0].Item {
		t.Fatal("Claude delta duplicated", d, v)
	}
}
func TestImagesAndRootBoundary(t *testing.T) {
	n := setup(t)
	root := t.TempDir()
	n.MediaRoots = []string{root}
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4v0z5PwAHOALI/Huk1wAAAABJRU5ErkJggg==")
	p := filepath.Join(root, "image.png")
	_ = os.WriteFile(p, png, 0600)
	id, e := n.media(p, root)
	if e != nil || id == "" {
		t.Fatal(e)
	}
	var m store.Media
	if e = n.Store.Get("media", id, &m); e != nil || m.MIME != "image/png" {
		t.Fatal(m, e)
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	_ = os.WriteFile(outside, png, 0600)
	_ = os.Symlink(outside, filepath.Join(root, "escape.png"))
	for _, src := range []string{outside, filepath.Join(root, "escape.png"), "https://example.com/p.png"} {
		if _, e = n.media(src, root); e == nil {
			t.Fatal("unsafe image read", src)
		}
	}
}
func TestFollowerPartialRestartAndNewFile(t *testing.T) {
	n := setup(t)
	root := t.TempDir()
	path := filepath.Join(root, "rollout.jsonl")
	line := `{"type":"session_meta","payload":{"id":"live"}}` + "\n"
	msg := `{"type":"response_item","payload":{"type":"message","role":"assistant","id":"m","content":[{"type":"output_text","text":"实时输出"}]}}` + "\n"
	_ = os.WriteFile(path, []byte(line+msg[:20]), 0600)
	f := &Follower{Normalizer: n, Roots: []Root{{"codex", root, "log"}}}
	f.Scan()
	if n.Store.LastSeq() != 0 {
		t.Fatal("partial record emitted")
	}
	file, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = file.WriteString(msg[20:])
	file.Close()
	f.Scan()
	if n.Store.LastSeq() != 1 {
		t.Fatal("new record missed")
	}
	f = &Follower{Normalizer: n, Roots: []Root{{"codex", root, "log"}}}
	f.Scan()
	if n.Store.LastSeq() != 1 {
		t.Fatal("restart duplicate")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.Run(ctx); close(done) }()
	_ = os.WriteFile(filepath.Join(root, "new.jsonl"), []byte(line+msg), 0600)
	time.Sleep(650 * time.Millisecond)
	cancel()
	<-done
	if n.Store.LastSeq() != 2 {
		t.Fatal("new session not discovered")
	}
}
func TestRedaction(t *testing.T) {
	v := Redact("API_KEY=abc123456 password='secret' Bearer abcdefghijkl sk-1234567890123456 https://a.com/x?signature=abc")
	for _, s := range []string{"abc123456", "secret'", "abcdefghijkl", "1234567890123456", "signature="} {
		if contains(v, s) {
			t.Fatal("secret leaked", v)
		}
	}
}
func contains(s, v string) bool {
	for i := 0; i+len(v) <= len(s); i++ {
		if s[i:i+len(v)] == v {
			return true
		}
	}
	return false
}

func TestMCPImageOutputBecomesEncryptedMedia(t *testing.T) {
	n := setup(t)
	st := State{Session: "c"}
	raw := `{"type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"tool","output":{"content":[{"type":"text","text":"Here is the screenshot"},{"type":"image","mimeType":"image/png","data":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4v0z5PwAHOALI/Huk1wAAAABJRU5ErkJggg=="}]}}}`
	out, e := n.Parse([]byte(raw), "codex", "log", &st)
	if e != nil || len(out) != 2 {
		t.Fatal(out, e)
	}
	if out[0].Kind != "image" || out[1].Kind != "tool_result" || out[1].Text != "Here is the screenshot" {
		t.Fatal(out)
	}
}

func TestClaudeToolStreamingSnapshotAndCompletion(t *testing.T) {
	n := setup(t)
	st := State{Session: "cl", Message: "cm"}
	a, _ := n.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu","name":"Bash"}}}`), "claude", "stream", &st)
	b, _ := n.Parse([]byte(`{"type":"assistant","message":{"id":"cm","role":"assistant","content":[{"type":"tool_use","id":"toolu","name":"Bash","input":{"command":"go test"}}],"stop_reason":"end_turn"}}`), "claude", "stream", &st)
	if len(a) != 1 || len(b) != 2 || a[0].Item != b[0].Item || b[1].Status != "completed" {
		t.Fatal(a, b)
	}
}
func TestInitialWindowKeepsSessionIdentity(t *testing.T) {
	n := setup(t)
	dir := t.TempDir()
	line := `{"type":"session_meta","payload":{"id":"true-session","cwd":"/workspace"}}` + "\n"
	padding := `{"type":"ignored","text":"` + string(make([]byte, 0)) + `"}` + "\n"
	body := line
	for i := 0; i < 80; i++ {
		body += padding
	}
	body += `{"type":"response_item","payload":{"type":"message","role":"assistant","id":"tail","content":[{"type":"output_text","text":"recent"}]}}` + "\n"
	_ = os.WriteFile(filepath.Join(dir, "big.jsonl"), []byte(body), 0600)
	f := &Follower{Normalizer: n, Roots: []Root{{"codex", dir, "log"}}, HistoryBytes: 200}
	f.Scan()
	events, e := n.Store.Events("true-session", 0, 100)
	if e != nil || len(events) != 1 || events[0].Text != "recent" {
		t.Fatal(events, e)
	}
}
func TestIndexTitlesOnlyForImportedSessions(t *testing.T) {
	n := setup(t)
	_, _, _ = n.Store.Append(store.Event{ID: "a", Session: "known", Agent: "codex", Kind: "message", Text: "hello", Source: "log"})
	index := filepath.Join(t.TempDir(), "index.jsonl")
	_ = os.WriteFile(index, []byte(`{"id":"known","thread_name":"PC conversation title","updated_at":"now"}`+"\n"+`{"id":"not-imported","thread_name":"Do not import this private title","updated_at":"now"}`+"\n"), 0600)
	f := &Follower{Normalizer: n, Index: index}
	f.Scan()
	ss, e := n.Store.Sessions()
	if e != nil || len(ss) != 1 || ss[0].Title != "PC conversation title" {
		t.Fatal(ss, e)
	}
	before := n.Store.LastSeq()
	f.Scan()
	if n.Store.LastSeq() != before {
		t.Fatal("index refresh duplicated")
	}
}
