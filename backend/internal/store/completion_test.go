package store

import "testing"

func TestCompletionMarkerIgnoresToolsMetadataDuplicatesAndOldImports(t *testing.T) {
	s, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	appendEvent := func(ev Event) {
		t.Helper()
		ev.Session = "chat"
		ev.Agent = "codex"
		if _, _, e := s.Append(ev); e != nil {
			t.Fatal(e)
		}
	}
	marker := func() uint64 {
		t.Helper()
		ss, e := s.Sessions()
		if e != nil || len(ss) != 1 {
			t.Fatal(e)
		}
		return ss[0].Completion
	}
	appendEvent(Event{ID: "run", Kind: "status", Status: "running", Time: "2026-10-14T00:00:00Z"})
	appendEvent(Event{ID: "tool", Kind: "tool_result", Status: "completed", Time: "2026-10-14T00:00:01Z"})
	if marker() != 0 {
		t.Fatal("tool completion marked a chat unread")
	}
	appendEvent(Event{ID: "done", Kind: "status", Status: "completed", Time: "2026-10-14T00:00:02Z"})
	first := marker()
	if first == 0 {
		t.Fatal("completion missing")
	}
	appendEvent(Event{ID: "done", Kind: "status", Status: "completed", Time: "2026-10-14T00:00:02Z"})
	appendEvent(Event{ID: "title", Kind: "status", Mode: "title", Text: "Renamed", Time: "2026-10-14T00:00:03Z"})
	appendEvent(Event{ID: "old-import", Kind: "status", Status: "completed", Time: "2026-10-13T00:00:00Z"})
	if marker() != first {
		t.Fatal("duplicate/title/old import advanced completion")
	}
	// Catalog replaces metadata but cannot erase the encrypted completion marker.
	s.Put("sessions", "chat", Session{ID: "chat", Agent: "codex", Status: "completed"})
	if marker() != first {
		t.Fatal("catalog replacement lost marker")
	}
	appendEvent(Event{ID: "new-run", Kind: "status", Status: "running", Time: "2026-10-14T00:01:00Z"})
	appendEvent(Event{ID: "idle", Kind: "status", Status: "idle", Time: "2026-10-14T00:01:01Z"})
	next := marker()
	if next <= first {
		t.Fatal("native idle after running not recognized")
	}
	appendEvent(Event{ID: "idle-snapshot", Kind: "status", Status: "idle", Time: "2026-10-14T00:01:02Z"})
	if marker() != next {
		t.Fatal("idle poll created new completion")
	}
}

func TestDesktopCompletionWhileDisconnectedHasDistinctTurnRevision(t *testing.T) {
	s, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.Put("sessions", "chat", Session{ID: "chat", Agent: "codex", Status: "completed"})
	for i, turn := range []string{"first", "first", "second"} {
		ev := Event{ID: turn + string(rune('a'+i)), Session: "chat", Agent: "codex", Source: "desktop-live", Kind: "status", Status: "completed", Item: "desktop-turn/" + turn, Time: "2026-10-14T00:00:0" + string(rune('1'+i)) + "Z"}
		_, added, err := s.AppendDesktop(ev, false)
		if err != nil || added != (i != 1) {
			t.Fatal("same status lost distinct completion or repeated old turn", i, added, err)
		}
	}
	ss, _ := s.Sessions()
	if ss[0].Completion != 2 {
		t.Fatal("second turn completion was not recorded")
	}
}
