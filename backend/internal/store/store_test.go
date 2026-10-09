package store

import (
	"bytes"
	bolt "go.etcd.io/bbolt"
	"os"
	"path/filepath"
	"testing"
)

func TestVaultRestartDedupeAndAtRest(t *testing.T) {
	dir := t.TempDir()
	s, e := New(dir)
	if e != nil {
		t.Fatal(e)
	}
	ev := Event{ID: "source:1", Session: "test-session", Agent: "codex", Kind: "message", Role: "user", Text: "UNIQUE_CONFIDENTIAL_测试", Source: "log"}
	a, added, e := s.Append(ev)
	if e != nil || !added || a.Seq != 1 {
		t.Fatal(a, added, e)
	}
	if _, added, e = s.Append(ev); e != nil || added {
		t.Fatal("duplicate appended", e)
	}
	root := bytes.Repeat([]byte{42}, 32)
	if e = s.Put("devices", "phone", Device{ID: "phone", Name: "CONFIDENTIAL_PHONE_NAME", Root: root}); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "vault.db"))
	for _, secret := range [][]byte{[]byte(ev.Text), []byte("CONFIDENTIAL_PHONE_NAME"), root} {
		if bytes.Contains(data, secret) {
			t.Fatal("plaintext on disk")
		}
	}
	for _, p := range []string{"storage.key", "vault.db"} {
		st, _ := os.Stat(filepath.Join(dir, p))
		if st.Mode().Perm() != 0600 {
			t.Fatal("insecure permissions", p)
		}
	}
	s, e = New(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	events, e := s.Events(ev.Session, 0, 100)
	if e != nil || len(events) != 1 || events[0].Text != ev.Text {
		t.Fatal(events, e)
	}
	ss, e := s.Sessions()
	if e != nil || len(ss) != 1 || ss[0].Title != ev.Text {
		t.Fatal(ss, e)
	}
}

func TestPresentationRepairPreservesKeysAndSequence(t *testing.T) {
	s, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	root := bytes.Repeat([]byte{7}, 32)
	_ = s.Put("devices", "phone", Device{ID: "phone", Root: root})
	_, _, _ = s.Append(Event{ID: "context", Session: "s", Agent: "codex", Kind: "message", Role: "user", Text: "<environment_context>private</environment_context>"})
	_, _, _ = s.Append(Event{ID: "human", Session: "s", Agent: "codex", Kind: "message", Role: "user", Text: "修复登录\n保留 <div>内容</div>"})
	if e = s.RepairPresentation(); e != nil {
		t.Fatal(e)
	}
	if s.LastSeq() != 2 {
		t.Fatal("sequence changed")
	}
	var dev Device
	if e = s.Get("devices", "phone", &dev); e != nil || !bytes.Equal(dev.Root, root) {
		t.Fatal("pairing changed")
	}
	evs, _ := s.Events("s", 0, 100)
	if evs[0].Kind != "status" || evs[1].Text != "修复登录\n保留 <div>内容</div>" {
		t.Fatal("presentation repair damaged human content")
	}
	ss, _ := s.Sessions()
	if ss[0].Title != "修复登录 保留 <div>内容</div>" {
		t.Fatal("context title not repaired")
	}
}

func TestRestartInvalidatesApprovalAndAllowsResume(t *testing.T) {
	s, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_ = s.Put("sessions", "managed", Session{ID: "managed", Agent: "codex", Managed: true, Status: "running"})
	_, _, _ = s.Append(Event{ID: "pending", Session: "managed", Agent: "codex", Kind: "approval", Item: "approval-key", Status: "pending", Mode: "snapshot", Text: "request"})
	_ = s.Put("sessions", "managed", Session{ID: "managed", Agent: "codex", Managed: true, Status: "running"})
	if e = s.RecoverManaged(); e != nil {
		t.Fatal(e)
	}
	ss, _ := s.Sessions()
	if ss[0].Status != "interrupted" {
		t.Fatal("stale running state")
	}
	events, _ := s.Events("managed", 1, 100)
	resolved := false
	for _, ev := range events {
		if ev.Kind == "approval" && ev.Item == "approval-key" && ev.Status == "resolved" {
			resolved = true
		}
	}
	if !resolved {
		t.Fatal("phone cache did not receive approval invalidation")
	}
}

func TestSessionIndexMigrationPaginationAndIsolation(t *testing.T) {
	dir := t.TempDir()
	s, e := New(dir)
	if e != nil {
		t.Fatal(e)
	}
	_, _, _ = s.Append(Event{ID: "a1", Session: "a", Agent: "codex", Kind: "message", Text: "one"})
	foreign, _, _ := s.Append(Event{ID: "b1", Session: "b", Agent: "claude", Kind: "message", Text: "unrelated"})
	last, _, _ := s.Append(Event{ID: "a2", Session: "a", Agent: "codex", Kind: "message", Text: "two"})
	_ = s.Put("devices", "owner", Device{ID: "owner", Root: []byte("preserved-owner-root")})
	// Simulate an existing vault made before the index was introduced.
	e = s.db.Update(func(tx *bolt.Tx) error { return tx.DeleteBucket([]byte("sessionevents")) })
	if e != nil {
		t.Fatal(e)
	}
	_ = s.Close()
	s, e = New(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	page, e := s.Events("a", 0, 1)
	if e != nil || len(page) != 1 || page[0].Text != "one" {
		t.Fatal("migration/pagination failed")
	}
	page, e = s.Events("a", page[0].Seq, 1)
	if e != nil || len(page) != 1 || page[0].Seq != last.Seq {
		t.Fatal("resume cursor failed")
	}
	var owner Device
	_ = s.Get("devices", "owner", &owner)
	if string(owner.Root) != "preserved-owner-root" {
		t.Fatal("migration changed owner root")
	}
	// A per-session lookup must not decrypt unrelated rows at all.
	e = s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("events")).Put([]byte(seqKey(foreign.Seq)), []byte("unrelated corrupt ciphertext"))
	})
	if e != nil {
		t.Fatal(e)
	}
	page, e = s.Events("a", 0, 100)
	if e != nil || len(page) != 2 {
		t.Fatal("session lookup scanned unrelated encrypted rows")
	}
	if _, e = s.Events("", 0, 100); e == nil {
		t.Fatal("global scan should still detect corrupt ciphertext")
	}
}
