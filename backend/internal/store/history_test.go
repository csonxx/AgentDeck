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

func TestHistoryLargeSessionCompleteItemsPagingAndLiveCursor(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 1; i <= 120; i++ {
		for d := 0; d < 3; d++ {
			_, _, err = s.Append(Event{ID: fmt.Sprintf("%d-%d", i, d), Session: "large", Kind: "message", Item: fmt.Sprint(i), Mode: "delta", Text: "part"})
			if err != nil {
				t.Fatal(err)
			}
		}
		s.Append(Event{ID: fmt.Sprintf("other-%d", i), Session: "other", Kind: "message", Text: "foreign"})
	}
	page, err := s.History("large", 0, 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 40 || !page.HasMore || page.Events[0].Item != "81" || page.Events[39].Text != "partpartpart" {
		t.Fatal("incomplete recent window")
	}
	live, _, err := s.Append(Event{ID: "next", Session: "large", Kind: "message", Item: "120", Mode: "delta", Text: "next"})
	if err != nil {
		t.Fatal(err)
	}
	events, err := s.Events("large", page.Cursor, 100)
	if err != nil || len(events) != 1 || events[0].Seq != live.Seq {
		t.Fatal("live cursor skipped or duplicated output")
	}
	older, err := s.History("large", page.Before, 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(older.Events) != 40 || !older.HasMore || older.Events[0].Item != "41" || older.Events[39].Item != "80" {
		t.Fatal("page overlaps or order changed")
	}
	oldest, err := s.History("large", older.Before, 40)
	if err != nil || oldest.HasMore || len(oldest.Events) != 40 || oldest.Events[0].Item != "1" {
		t.Fatal("oldest page incomplete")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "vault.db"))
	if bytes.Contains(raw, []byte("partpartpart")) {
		t.Fatal("vault is plaintext")
	}
}

func TestHistoryAfterPagesForwardWithoutRepeatsOrSplittingOversizedAnchor(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	anchor, _, err := s.Append(Event{ID: "anchor", Session: "s", Kind: "message", Text: strings.Repeat("x", 600<<10)})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 95; i++ {
		if _, _, err = s.Append(Event{ID: fmt.Sprint(i), Session: "s", Kind: "message", Text: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
		s.Append(Event{ID: fmt.Sprintf("foreign-%d", i), Session: "other", Kind: "message", Text: "foreign"})
	}
	after := anchor.Seq
	count := 0
	for {
		page, err := s.HistoryAfter("s", after)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Events) == 0 {
			t.Fatal("forward page stalled")
		}
		for _, ev := range page.Events {
			count++
			if ev.Session != "s" || ev.First <= after || ev.Text != fmt.Sprint(count) {
				t.Fatal("repeat, skip, foreign item or oversized anchor")
			}
			after = ev.First
		}
		if !page.HasNewer {
			break
		}
	}
	if count != 95 {
		t.Fatal("incomplete forward history")
	}
	last, err := s.HistoryAfter("s", after)
	if err != nil || len(last.Events) != 0 || last.HasNewer {
		t.Fatal("latest boundary not terminal")
	}
}

func TestHistoryMigrationRepairAndCrossPageItemUpdates(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Append(Event{ID: "old", Session: "a", Kind: "message", Role: "user", Item: "m", Text: "<environment_context>hidden</environment_context>Visible", Mode: "snapshot"})
	s.Append(Event{ID: "pic", Session: "a", Kind: "image", Media: "blob"})
	s.Append(Event{ID: "b", Session: "b", Kind: "message", Text: "foreign"})
	if err = s.db.Update(func(tx *bolt.Tx) error {
		if e := tx.DeleteBucket([]byte("transcript")); e != nil {
			return e
		}
		return tx.DeleteBucket([]byte("itemkeys"))
	}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.RepairPresentation(); err != nil {
		t.Fatal(err)
	}
	page, err := s.History("a", 0, 1)
	if err != nil || len(page.Events) != 1 || page.Events[0].Kind != "image" || !page.HasMore {
		t.Fatal("image or isolation lost")
	}
	s.Append(Event{ID: "changed", Session: "a", Kind: "message", Item: "m", Mode: "snapshot", Text: "Updated"})
	older, err := s.History("a", page.Before, 40)
	if err != nil || len(older.Events) != 1 || older.Events[0].Text != "Updated" || older.HasMore {
		t.Fatal("older item update lost")
	}
}

func TestLiveUpdatesOutsideRecentWindowUseFullSnapshots(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Append(Event{ID: "begin", Session: "s", Item: "long", Kind: "thinking", Mode: "delta", Text: "prefix"})
	for i := 0; i < 60; i++ {
		s.Append(Event{ID: fmt.Sprint(i), Session: "s", Kind: "message", Text: "later"})
	}
	recent, _ := s.History("s", 0, 40)
	s.Append(Event{ID: "continue", Session: "s", Item: "long", Kind: "thinking", Mode: "delta", Text: " suffix"})
	events, cursor, err := s.LiveEvents("s", recent.Cursor, 100)
	if err != nil || len(events) != 1 || events[0].Text != "prefix suffix" || events[0].Mode != "snapshot" || events[0].First != 1 || cursor <= recent.Cursor {
		t.Fatal("streamed older item lost its prefix")
	}
}

func TestLiveEventsByteBudgetNeverSkipsDeferredSnapshots(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows := []Event{}
	for i := 1; i <= 10; i++ {
		rows = append(rows, Event{ID: fmt.Sprint(i), Item: fmt.Sprint(i), Session: "s", Agent: "codex", Source: "log", Kind: "tool_result", Mode: "snapshot", Text: strings.Repeat("x", 100<<10)})
	}
	if _, err = s.AppendImported(rows, false); err != nil {
		t.Fatal(err)
	}
	cursor := uint64(0)
	seen := map[string]bool{}
	pushes := 0
	for cursor < s.LastSeq() {
		items, next, err := s.LiveEvents("s", cursor, 100)
		if err != nil {
			t.Fatal(err)
		}
		size := 0
		for _, ev := range items {
			if seen[ev.Item] {
				t.Fatal("snapshot repeated")
			}
			seen[ev.Item] = true
			size += len(ev.Text)
		}
		if len(items) == 0 || size > 256<<10 || next <= cursor {
			t.Fatal("oversized push or stalled cursor")
		}
		cursor = next
		pushes++
	}
	if len(seen) != 10 || pushes != 5 {
		t.Fatal("deferred snapshots lost")
	}
}
