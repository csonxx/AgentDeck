package server

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agentdeck/internal/agent"
	"agentdeck/internal/store"
)

func TestDownloadLargeFileScopeChangesExpiryAndBoundedChunks(t *testing.T) {
	s, _ := setupServer(t)
	root := t.TempDir()
	n := &agent.Normalizer{Store: s.Store, MediaRoots: []string{root}}
	body := bytes.Repeat([]byte("raw-download-fixture\x00"), 600000)
	path := filepath.Join(root, "large.bin")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	s.Store.Append(store.Event{ID: "link", Session: "s", Project: root, Kind: "message", Text: "[file](large.bin)"})
	meta, err := n.ReadAttachment("s", "large.bin")
	if err != nil || !meta.Download || len(meta.Data) != 0 || meta.Size != int64(len(body)) {
		t.Fatal("large file must return metadata, not an 8MiB error", err)
	}
	d := downloads{}
	defer d.prune(true)
	v, err := d.request(n, Request{Method: "attachment_open", Session: "s", Href: "large.bin"})
	if err != nil {
		t.Fatal(err)
	}
	id := v.(map[string]any)["download"].(string)
	if _, err = (downloads{}).request(n, Request{Method: "attachment_read", Download: id}); err == nil {
		t.Fatal("download crossed connection boundary")
	}
	if _, err = d.request(n, Request{Method: "attachment_open", Session: "other", Href: "large.bin"}); err == nil {
		t.Fatal("download crossed session boundary")
	}
	if _, err = d.request(n, Request{Method: "attachment_read", Download: id, Offset: len(body) + 1}); err == nil {
		t.Fatal("invalid offset accepted")
	}
	version := v.(map[string]any)["version"].(string)
	// A new encrypted connection can reopen the same authorized source at an offset.
	other := downloads{}
	defer other.prune(true)
	reopened, e := other.request(n, Request{Method: "attachment_open", Session: "s", Href: "large.bin", Offset: downloadChunkSize, Version: version})
	if e != nil {
		t.Fatal(e)
	}
	part, e := other.request(n, Request{Method: "attachment_read", Download: reopened.(map[string]any)["download"].(string), Offset: downloadChunkSize})
	if e != nil || !bytes.Equal(part.(map[string]any)["bytes"].([]byte), body[downloadChunkSize:2*downloadChunkSize]) {
		t.Fatal("resume changed bytes", e)
	}
	if _, e = other.request(n, Request{Method: "attachment_open", Session: "s", Href: "large.bin", Version: "wrong"}); e == nil {
		t.Fatal("changed source version accepted")
	}
	h := sha256.New()
	for offset := 0; offset < len(body); {
		r, err := d.request(n, Request{Method: "attachment_read", Download: id, Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		chunk := r.(map[string]any)["bytes"].([]byte)
		if len(chunk) == 0 || len(chunk) > downloadChunkSize {
			t.Fatal("unbounded read")
		}
		h.Write(chunk)
		offset += len(chunk)
	}
	want := sha256.Sum256(body)
	if !bytes.Equal(h.Sum(nil), want[:]) {
		t.Fatal("download bytes changed")
	}
	d[id].last = time.Now().Add(-11 * time.Minute)
	if _, err = d.request(n, Request{Method: "attachment_read", Download: id}); err == nil || len(d) != 0 {
		t.Fatal("idle descriptor retained")
	}
	v, err = d.request(n, Request{Method: "attachment_open", Session: "s", Href: "large.bin"})
	if err != nil {
		t.Fatal(err)
	}
	id = v.(map[string]any)["download"].(string)
	os.Truncate(path, 1)
	if _, err = d.request(n, Request{Method: "attachment_read", Download: id}); err == nil || len(d) != 0 {
		t.Fatal("changed file allowed")
	}
	// A sparse >4GB file proves metadata and offsets are not a raised 8MB cap.
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(5 << 30); err != nil {
		t.Fatal(err)
	}
	f.Close()
	meta, err = n.ReadAttachment("s", "large.bin")
	if err != nil || meta.Size != 5<<30 || len(meta.Data) != 0 {
		t.Fatal("large metadata overflow", err)
	}
}
