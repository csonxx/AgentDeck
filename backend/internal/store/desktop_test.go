package store

import (
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
