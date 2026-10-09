package store

import (
	"bytes"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutlineAllDialogueShortPreviewsAndDirectOldAnchor(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var first uint64
	for i := 0; i < 245; i++ {
		ev, _, err := s.Append(Event{ID: fmt.Sprint(i), Session: "chat", Kind: "message", Item: fmt.Sprint(i), Role: "user", Text: fmt.Sprintf("Record %d ", i) + strings.Repeat("文", 4000), Mode: "snapshot"})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = ev.Seq
		}
	}
	s.Append(Event{ID: "tool", Session: "chat", Kind: "tool_result", Text: "not a dialogue"})
	s.Append(Event{ID: "foreign", Session: "other", Kind: "message", Text: "foreign"})
	page, err := s.Outline("chat", 0, 200)
	if err != nil || len(page.Entries) != 200 || !page.HasMore {
		t.Fatal("directory is incomplete")
	}
	if len([]rune(page.Entries[0].Preview)) > 161 || page.Entries[0].Key != "244:message" {
		t.Fatal("preview unbounded or unstable identity")
	}
	older, err := s.Outline("chat", page.Before, 200)
	if err != nil || len(older.Entries) != 45 || older.HasMore || older.Entries[44].First != first {
		t.Fatal("directory pagination overlaps or skips oldest")
	}
	window, err := s.HistoryAt("chat", first)
	if err != nil || len(window.Events) != 40 || window.HasMore || !window.HasNewer || window.Events[0].Item != "0" {
		t.Fatal("old jump drained history or lost target")
	}
	if _, err = s.HistoryAt("other", first); err == nil {
		t.Fatal("cross-session anchor accepted")
	}
	s.Append(Event{ID: "updated", Session: "chat", Kind: "message", Item: "0", Text: "Updated entry", Mode: "snapshot"})
	old, err := s.Outline("chat", older.Entries[0].First+1, 200)
	if err != nil || old.Entries[len(old.Entries)-1].Preview != "Updated entry" {
		t.Fatal("outline snapshot stale")
	}
}
func TestOutlineMigrationEncryptedAndRetainsImages(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Append(Event{ID: "m", Session: "s", Item: "m", Kind: "message", Text: "CONFIDENTIAL_DIRECTORY_PREVIEW", Mode: "snapshot"})
	s.Append(Event{ID: "i", Session: "s", Kind: "image", Media: "pic"})
	if err = s.db.Update(func(tx *bolt.Tx) error { return tx.DeleteBucket([]byte("outline")) }); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = New(dir)
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.Outline("s", 0, 200)
	if err != nil || len(page.Entries) != 2 || page.Entries[0].Kind != "image" {
		t.Fatal("upgrade loses historical directory")
	}
	s.Close()
	raw, err := os.ReadFile(filepath.Join(dir, "vault.db"))
	if err != nil || bytes.Contains(raw, []byte("CONFIDENTIAL_DIRECTORY_PREVIEW")) {
		t.Fatal("directory plaintext on disk")
	}
}
