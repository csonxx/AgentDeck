package store

import (
	"bytes"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"strings"
	"testing"
	"time"
)

func TestDesktopSourcesReconcileWithoutMergingRepeatedMessages(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ev := Event{Session: "desktop", Agent: "codex", Kind: "message", Role: "assistant", Mode: "snapshot", Item: "log-item", ID: "log-1", Text: "Hello", Source: "log"}
	if _, added, e := s.AppendDesktop(ev, false); e != nil || !added {
		t.Fatal("initial log", e)
	}
	ev.ID = "native-1"
	ev.Item = "native-item"
	ev.Source = "desktop-live"
	if _, added, e := s.AppendDesktop(ev, false); e != nil || added {
		t.Fatal("native snapshot duplicated log", e)
	}
	ev.ID = "native-2"
	ev.Text = "Hello world"
	s.AppendDesktop(ev, false)
	ev.ID = "log-2"
	ev.Item = "log-final"
	ev.Source = "log"
	if _, added, e := s.AppendDesktop(ev, false); e != nil || added {
		t.Fatal("flushed final duplicated live snapshot", e)
	}
	page, _ := s.History("desktop", 0, 40)
	if len(page.Events) != 1 || page.Events[0].Text != "Hello world" {
		t.Fatal("live update did not retain original item")
	}
	// Distinct messages with identical text must remain distinct.
	ev.Source = "desktop-live"
	ev.Item = "second-native"
	ev.ID = "native-3"
	s.AppendDesktop(ev, false)
	page, _ = s.History("desktop", 0, 40)
	if len(page.Events) != 2 {
		t.Fatal("repeated messages incorrectly merged")
	}
	// Replaying the same logfile remains idempotent at the event ID boundary.
	ev.Source = "log"
	ev.Item = "second-log"
	ev.ID = "log-3"
	s.AppendDesktop(ev, false)
	page, _ = s.History("desktop", 0, 40)
	if len(page.Events) != 2 {
		t.Fatal("second native/log pair duplicated")
	}
}
func TestDesktopAcceptanceDoesNotOverwriteNativeState(t *testing.T) {
	s, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	before := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	s.Put("sessions", "s", Session{ID: "s", Agent: "codex", Source: "desktop", Updated: before, Status: "idle"})
	receipt := Event{ID: "ack-1", Session: "s", Agent: "codex", Source: "desktop-live", Kind: "status", Status: "pending"}
	if _, added, e := s.MarkDesktopAccepted(receipt, before); e != nil || !added {
		t.Fatal("missing receipt", e)
	}
	s.Append(Event{ID: "run", Session: "s", Agent: "codex", Source: "desktop-live", Kind: "status", Status: "running"})
	receipt.ID = "late-ack"
	if _, added, e := s.MarkDesktopAccepted(receipt, before); e != nil || added {
		t.Fatal("late ack replaced running", e)
	}
	s.Append(Event{ID: "done", Session: "s", Agent: "codex", Source: "desktop-live", Kind: "status", Status: "completed"})
	s.AppendDesktop(Event{ID: "old-log-status", Session: "s", Agent: "codex", Source: "log", Kind: "status", Status: "running", Time: before}, false)
	var session Session
	s.Get("sessions", "s", &session)
	if session.Status != "completed" {
		t.Fatal("stale log reopened completed turn")
	}
}

func TestDesktopCitationSnapshotsInBothArrivalOrders(t *testing.T) {
	for _, nativeFirst := range []bool{true, false} {
		t.Run(fmt.Sprint(nativeFirst), func(t *testing.T) {
			s, err := New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			native := Event{ID: "native-1", Item: "native-item", Session: "s", Agent: "codex", Kind: "message", Role: "assistant", Mode: "snapshot", Source: "desktop-live", Text: "Completed."}
			log := native
			log.ID = "log-1"
			log.Item = "log-item"
			log.Source = "log"
			log.Text += "\n<oai-mem-citation>metadata</oai-mem-citation>"
			events := []Event{native, log}
			if !nativeFirst {
				events = []Event{log, native}
			}
			for _, ev := range events {
				if _, _, err := s.AppendDesktop(ev, false); err != nil {
					t.Fatal(err)
				}
			}
			p, _ := s.History("s", 0, 40)
			if len(p.Events) != 1 || p.Events[0].Text != "Completed." {
				t.Fatal("final reply duplicated or citation leaked")
			}
			// A revised logfile snapshot retains its alias, even when text changed.
			log.ID = "log-2"
			log.Text = "Completed. More details.\n<oai-mem-citation>metadata</oai-mem-citation>"
			s.AppendDesktop(log, false)
			native.ID = "native-2"
			native.Text = "Completed. More details."
			s.AppendDesktop(native, false)
			p, _ = s.History("s", 0, 40)
			if len(p.Events) != 1 || p.Events[0].Text != native.Text {
				t.Fatal("updated log/native identity split")
			}
			s.AppendDesktop(Event{ID: "user", Item: "user", Session: "s", Agent: "codex", Kind: "message", Role: "user", Mode: "snapshot", Source: "log", Text: "Again"}, false)
			native.ID = "native-next"
			native.Item = "native-next"
			s.AppendDesktop(native, false)
			log.ID = "log-next"
			log.Item = "log-next"
			s.AppendDesktop(log, false)
			p, _ = s.History("s", 0, 40)
			if len(p.Events) != 3 {
				t.Fatal("identical reply in another turn was removed")
			}
		})
	}
}

func TestDesktopCitationRepairPreservesRawHistoryAndPairing(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	root := []byte("encrypted-device-root")
	s.Put("devices", "d", Device{ID: "d", Root: root})
	seqs := []Event{
		{ID: "a", Item: "a", Source: "desktop-live", Role: "assistant", Text: "Done"},
		{ID: "progress", Item: "progress", Source: "desktop-live", Role: "assistant", Text: "A separate progress update"},
		{ID: "b", Item: "b", Source: "log", Role: "assistant", Text: "Done\n<oai-mem-citation>private</oai-mem-citation>"},
		{ID: "u", Item: "u", Source: "log", Role: "user", Text: "Repeat"},
		{ID: "c", Item: "c", Source: "log", Role: "assistant", Text: "Done\n<oai-mem-citation>private</oai-mem-citation>"},
		{ID: "d", Item: "d", Source: "desktop-live", Role: "assistant", Text: "Done"},
		{ID: "e", Item: "e", Source: "log", Role: "assistant", Text: "Actually different"},
		{ID: "f", Item: "f", Source: "desktop-live", Role: "assistant", Text: "Done"},
	}
	for i, ev := range seqs {
		ev.Session = "s"
		ev.Agent = "codex"
		ev.Kind = "message"
		ev.Mode = "snapshot"
		stored, _, err := s.Append(ev)
		if err != nil {
			t.Fatal(err)
		}
		stored.First = stored.Seq
		seqs[i] = stored
	}
	// Simulate a pre-fix projection; normal appends already clean metadata.
	err = s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket([]byte("agentconfig")).Delete([]byte(desktopCitationRepair)); err != nil {
			return err
		}
		for _, ev := range seqs {
			if err := s.put(tx, "transcript", sessionIndexKey("s", ev.First), ev); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	p, _ := s.History("s", 0, 40)
	if len(p.Events) != 6 {
		t.Fatalf("wrong repaired count %d", len(p.Events))
	}
	for _, ev := range p.Events {
		if strings.Contains(ev.Text, "oai-mem-citation") {
			t.Fatal("metadata visible")
		}
	}
	raw, _ := s.Events("s", 0, 100)
	if len(raw) != 8 || !strings.Contains(raw[2].Text, "oai-mem-citation") {
		t.Fatal("source history changed")
	}
	live, _, err := s.LiveEvents("s", 0, 100)
	if err != nil || len(live) != 6 {
		t.Fatal("live events reintroduced duplicate", err, len(live))
	}
	outline, _ := s.Outline("s", 0, 200)
	if len(outline.Entries) != 6 {
		t.Fatal("outline duplicate remained")
	}
	var device Device
	if s.Get("devices", "d", &device) != nil || !bytes.Equal(device.Root, root) {
		t.Fatal("pairing changed")
	}
	native := seqs[2]
	native.ID = "new-log"
	native.Text = "Done updated"
	s.AppendDesktop(native, false)
	p, _ = s.History("s", 0, 40)
	if len(p.Events) != 6 {
		t.Fatal("repaired identity did not survive new update")
	}
	s.Close()
	s, err = New(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, _ = s.History("s", 0, 40)
	if len(p.Events) != 6 {
		t.Fatal("migration not idempotent")
	}
}
