package store

import (
	bolt "go.etcd.io/bbolt"
	"testing"
)

func TestIndexedRecoveryDoesNotResurrectResolvedApproval(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, ev := range []Event{
		{ID: "a", Session: "chat", Kind: "approval", Item: "q", Status: "pending"},
		{ID: "b", Session: "chat", Kind: "approval", Item: "q", Status: "resolved"},
		{ID: "c", Session: "chat", Kind: "approval", Item: "q2", Status: "pending"},
		{ID: "d", Session: "chat", Kind: "approval", Item: "q3", Status: "pending"},
	} {
		if _, _, err = s.Append(ev); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.RecoverManaged(); err != nil {
		t.Fatal(err)
	}
	if pending, err := s.HasPendingApprovals(); err != nil || pending {
		t.Fatal("pending index not drained", err)
	}
	cursor := s.LastSeq()
	if err = s.RecoverManaged(); err != nil {
		t.Fatal(err)
	}
	after := s.LastSeq()
	if after != cursor {
		t.Fatal("resolved approvals repeated on restart")
	}
	for _, item := range []string{"q", "q2", "q3"} {
		ev, err := s.TranscriptItem("chat", item, "approval")
		if err != nil || ev.Status != "resolved" {
			t.Fatal("approval state", item, err)
		}
	}
}

func TestPresentationRepairIsVersioned(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.RepairPresentation(); err != nil {
		t.Fatal(err)
	}
	// A poisoned archived event proves subsequent boots do not scan the archive.
	// Real appends always encrypt/normalize values through Store.
	if err = s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("events")).Put([]byte("fixture-poison"), []byte("not ciphertext"))
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.RepairPresentation(); err != nil {
		t.Fatal("presentation archive rescanned", err)
	}
	if err = s.RecoverManaged(); err != nil {
		t.Fatal("recovery archive rescanned", err)
	}
}
