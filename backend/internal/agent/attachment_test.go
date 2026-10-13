package agent

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"agentdeck/internal/store"
)

func TestLinkedAttachmentsScopeEncodingAndSourceLine(t *testing.T) {
	n := setup(t)
	root := t.TempDir()
	n.MediaRoots = []string{root}
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4v0z5PwAHOALI/Huk1wAAAABJRU5ErkJggg==")
	_ = os.WriteFile(filepath.Join(root, "图 片.png"), png, 0600)
	_ = os.WriteFile(filepath.Join(root, "readme.md"), []byte("first\nAPI_KEY=hidden\nlast\n"), 0600)
	_ = os.WriteFile(filepath.Join(root, "unlinked.txt"), []byte("not linked"), 0600)
	_ = os.WriteFile(filepath.Join(root, ".env"), []byte("private"), 0600)
	_ = os.Symlink(filepath.Join(root, ".env"), filepath.Join(root, "alias.txt"))
	outside := filepath.Join(t.TempDir(), "outside.md")
	_ = os.WriteFile(outside, []byte("outside"), 0600)
	_ = os.Symlink(outside, filepath.Join(root, "escape.md"))
	text := "![图](<图 片.png>) [文件](readme.md:3) [逃逸](escape.md) [secret](.env) [alias](alias.txt)"
	_, _, _ = n.Store.Append(store.Event{ID: "m", Session: "s", Project: root, Agent: "codex", Kind: "message", Text: text})
	v, e := n.ReadAttachment("s", "%E5%9B%BE%20%E7%89%87.png")
	if e != nil || v.MIME != "image/png" || string(v.Data) != string(png) {
		t.Fatal("encoded image failed", e)
	}
	v, e = n.ReadAttachment("s", "file://"+filepath.Join(root, "readme.md")+":3")
	if e != nil || v.Line != 3 || string(v.Data) != "first\n[REDACTED]\nlast\n" {
		t.Fatal("source link preview/redaction failed", e)
	}
	for _, href := range []string{"unlinked.txt", "escape.md", ".env", "alias.txt", "https://example.com/x", "file://other/readme.md"} {
		if _, e = n.ReadAttachment("s", href); e == nil {
			t.Fatal("unsafe link accepted")
		}
	}
	if _, e = n.ReadAttachment("different", "readme.md"); e == nil {
		t.Fatal("wrong session accepted")
	}
	_ = os.Remove(filepath.Join(root, "图 片.png"))
	if _, e = n.ReadAttachment("s", "图%20片.png"); e == nil {
		t.Fatal("missing file accepted")
	}
}
func TestHistoricalImageRecoveryPreservesChatAndPairing(t *testing.T) {
	n := setup(t)
	root := t.TempDir()
	source := t.TempDir()
	path := filepath.Join(root, "image.png")
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4v0z5PwAHOALI/Huk1wAAAABJRU5ErkJggg==")
	_ = os.WriteFile(path, png, 0600)
	records := []any{map[string]any{"type": "session_meta", "payload": map[string]any{"id": "historic", "cwd": root}}, map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "id": "image", "role": "user", "content": []any{map[string]any{"type": "output_image", "image_url": "file://" + path}}}}}
	body := []byte{}
	for _, r := range records {
		b, _ := json.Marshal(r)
		body = append(body, b...)
		body = append(body, '\n')
	}
	file := filepath.Join(source, "live.jsonl")
	_ = os.WriteFile(file, body, 0600)
	follower := &Follower{Normalizer: n, Roots: []Root{{Agent: "codex", Path: source, Source: "log"}}}
	follower.Scan()
	var before store.Session
	_ = n.Store.Get("sessions", "historic", &before)
	var checkpoint State
	_ = n.Store.Get("checkpoints", digest(file), &checkpoint)
	offset := checkpoint.Offset
	checkpoint.MediaVersion = 0
	_ = n.Store.Put("checkpoints", digest(file), checkpoint)
	_ = n.Store.Put("devices", "paired", store.Device{ID: "paired", Root: []byte("owner-root")})
	n.MediaRoots = []string{root}
	restarted := &Follower{Normalizer: n, Roots: follower.Roots}
	restarted.Scan()
	events, _ := n.Store.Events("historic", 0, 100)
	images := 0
	for _, v := range events {
		if v.Kind == "image" {
			images++
		}
	}
	if images != 1 {
		t.Fatal("historical image not recovered once")
	}
	var after store.Session
	_ = n.Store.Get("sessions", "historic", &after)
	if before != after {
		t.Fatal("recovery changed chat metadata")
	}
	_ = n.Store.Get("checkpoints", digest(file), &checkpoint)
	if checkpoint.Offset != offset || checkpoint.MediaVersion != 1 {
		t.Fatal("recovery changed offset")
	}
	var device store.Device
	_ = n.Store.Get("devices", "paired", &device)
	if string(device.Root) != "owner-root" {
		t.Fatal("pairing changed")
	}
	restarted = &Follower{Normalizer: n, Roots: follower.Roots}
	restarted.Scan()
	again, _ := n.Store.Events("historic", 0, 100)
	if len(again) != len(events) {
		t.Fatal("recovery duplicated events")
	}
}

func TestImageURLToolOutputAndRemoteImages(t *testing.T) {
	n := setup(t)
	st := State{Session: "images"}
	raw := `{"type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"url","output":{"image_url":"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4v0z5PwAHOALI/Huk1wAAAABJRU5ErkJggg=="}}}`
	out, e := n.Parse([]byte(raw), "codex", "log", &st)
	if e != nil || len(out) != 2 || out[0].Kind != "image" || out[0].Media == "" || out[1].Text != "" {
		t.Fatal("tool image URL was not extracted")
	}
	raw = `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_image","image_url":"https://example.com/image.png"}]}}`
	out, e = n.Parse([]byte(raw), "codex", "log", &st)
	if e != nil || len(out) != 1 || out[0].Kind != "image" || out[0].Text != "https://example.com/image.png" || out[0].Media != "" {
		t.Fatal("public HTTPS image not forwarded as a reference")
	}
}

func TestLargeInlineDataImageUsesEncryptedMediaReference(t *testing.T) {
	n := setup(t)
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4v0z5PwAHOALI/Huk1wAAAABJRU5ErkJggg==")
	png = append(png, make([]byte, 200<<10)...)
	raw, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "![output](data:image/png;base64," + base64.StdEncoding.EncodeToString(png) + ")"}}}})
	out, e := n.Parse(raw, "codex", "log", &State{Session: "inline"})
	if e != nil || len(out) != 1 || out[0].Text != "![output](agentdeck-media:"+digest(string(png))+")" {
		t.Fatal("inline image truncated instead of using encrypted media")
	}
}

func TestGeneratedVideoAndOfficeAttachmentsRemainSessionScoped(t *testing.T) {
	n := setup(t)
	root := t.TempDir()
	n.MediaRoots = []string{root}
	n.Store.Put("sessions", "s", store.Session{ID: "s", Project: root})
	for _, entry := range []struct {
		name, mime string
		body       []byte
	}{
		{"movie.mp4", "video/mp4", []byte("\x00\x00\x00\x18ftypisom00000000")},
		{"report.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", []byte("PK\x03\x04OFFICE")},
		{"table.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", []byte("PK\x03\x04SHEET")},
	} {
		path := filepath.Join(root, entry.name)
		os.WriteFile(path, entry.body, 0600)
		if _, e := n.ReadAttachment("s", path); e == nil {
			t.Fatal("unreferenced binary attachment allowed")
		}
		n.Store.Append(store.Event{ID: entry.name, Session: "s", Kind: "message", Role: "assistant", Item: entry.name, Text: "[Output](" + path + ")", Mode: "snapshot"})
		out, e := n.ReadAttachment("s", path)
		if e != nil || out.MIME != entry.mime {
			t.Fatal("linked binary attachment not available", e)
		}
	}
}
