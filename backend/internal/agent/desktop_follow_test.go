package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"agentdeck/internal/store"
)

func TestDesktopAdoptionRecoversSkippedLogAndKeepsFollowing(t *testing.T) {
	n := setup(t)
	root := t.TempDir()
	path := filepath.Join(root, "desktop.jsonl")
	meta := `{"type":"session_meta","payload":{"id":"original"}}` + "\n"
	message := func(id, text string) string {
		return fmt.Sprintf(`{"type":"response_item","payload":{"type":"message","role":"assistant","id":%q,"content":[{"type":"output_text","text":%q}]}}`, id, text) + "\n"
	}
	body := meta + message("before", "Before adoption")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	f := &Follower{Normalizer: n, Roots: []Root{{"codex", root, "log"}}}
	f.Scan()
	var ss store.Session
	if err := n.Store.Get("sessions", "original", &ss); err != nil {
		t.Fatal(err)
	}
	ss.Managed, ss.Source = true, "desktop"
	if err := n.Store.Put("sessions", ss.ID, ss); err != nil {
		t.Fatal(err)
	}
	body += message("lost", "Recovered desktop answer")
	os.WriteFile(path, []byte(body), 0600)
	// Reproduce the old release: checkpoint and unchanged-file cache advanced,
	// but the adopted desktop record was never appended to encrypted history.
	k := digest(path)
	f.states[path].Offset = int64(len(body))
	n.Store.Put("checkpoints", k, f.states[path])
	info, _ := os.Stat(path)
	f.stamps[path] = fmt.Sprintf("%d/%d", info.Size(), info.ModTime().UnixNano())
	f.Scan()
	events, _ := n.Store.Events("original", 0, 100)
	if len(events) != 2 || events[1].Text != "Recovered desktop answer" {
		t.Fatal("desktop history did not recover an already skipped record")
	}
	seq := n.Store.LastSeq()
	f.Scan()
	f = &Follower{Normalizer: n, Roots: f.Roots}
	f.Scan()
	if n.Store.LastSeq() != seq {
		t.Fatal("recovery duplicated saved records on rescan/restart")
	}
	body += message("live", "Next desktop answer")
	os.WriteFile(path, []byte(body), 0600)
	f.Scan()
	events, _ = n.Store.Events("original", 0, 100)
	if len(events) != 3 || events[2].Text != "Next desktop answer" {
		t.Fatal("desktop adoption disabled continued log synchronization")
	}
	n.Store.Get("sessions", ss.ID, &ss)
	if !ss.Managed || ss.Source != "desktop" {
		t.Fatal("log recovery changed the desktop executor identity")
	}
}

func TestManagedCLIStillUsesItsNativeStream(t *testing.T) {
	n := setup(t)
	root := t.TempDir()
	path := filepath.Join(root, "cli.jsonl")
	n.Store.Put("sessions", "cli", store.Session{ID: "cli", Agent: "codex", Managed: true, Source: "stream"})
	line := `{"type":"session_meta","payload":{"id":"cli"}}` + "\n" + `{"type":"response_item","payload":{"type":"message","role":"assistant","id":"m","content":[{"type":"output_text","text":"Already delivered by CLI stream"}]}}` + "\n"
	os.WriteFile(path, []byte(line), 0600)
	f := &Follower{Normalizer: n, Roots: []Root{{"codex", root, "log"}}}
	f.Scan()
	if n.Store.LastSeq() != 0 {
		t.Fatal("CLI-managed history was duplicated from its rollout file")
	}
}
