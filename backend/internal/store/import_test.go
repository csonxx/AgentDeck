package store

import (
	"fmt"
	"testing"
)

func TestImportedBatchIsAtomicAndReplaysIdempotently(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	events := []Event{}
	for i := 0; i < 128; i++ {
		events = append(events, Event{ID: fmt.Sprint(i), Item: fmt.Sprint(i), Session: "s", Agent: "codex", Source: "log", Kind: "message", Role: "assistant", Mode: "snapshot", Text: fmt.Sprint("Reply ", i)})
	}
	out, err := s.AppendImported(events, false)
	if err != nil || len(out) != 128 {
		t.Fatal("batch failed", err)
	}
	out, err = s.AppendImported(events, false)
	if err != nil || len(out) != 0 || s.LastSeq() != 128 {
		t.Fatal("replay not idempotent", err)
	}
	// An invalid kind-specific lookup causes the entire transaction to roll back.
	s.Put("agentconfig", desktopKey("log", Event{Session: "s", Kind: "message", Role: "assistant"}, "bad"), "wrong-shape")
	good := events[0]
	good.ID = "new-good"
	good.Item = "new-good"
	bad := good
	bad.ID = "bad"
	bad.Item = "bad"
	if _, err = s.AppendImported([]Event{good, bad}, false); err == nil {
		t.Fatal("invalid second record committed")
	}
	if s.LastSeq() != 128 {
		t.Fatal("partial batch survived rollback")
	}
}

func BenchmarkImportedRecords(b *testing.B) {
	for _, batch := range []bool{false, true} {
		b.Run(fmt.Sprint("batch=", batch), func(b *testing.B) {
			s, err := New(b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			for n := 0; n < b.N; n++ {
				rows := make([]Event, 1000)
				for i := range rows {
					key := fmt.Sprint(n, "/", i)
					rows[i] = Event{ID: key, Item: key, Session: "s", Agent: "codex", Source: "log", Kind: "tool_result", Mode: "snapshot", Text: "Synthetic output"}
				}
				if batch {
					for i := 0; i < len(rows); i += 125 {
						if _, err := s.AppendImported(rows[i:i+125], false); err != nil {
							b.Fatal(err)
						}
					}
				} else {
					for _, ev := range rows {
						if _, _, err := s.AppendDesktop(ev, false); err != nil {
							b.Fatal(err)
						}
					}
				}
			}
		})
	}
}
