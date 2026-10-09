package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"

	"agentdeck/internal/presentation"
	bolt "go.etcd.io/bbolt"
)

// Keep an encrypted, materialized item index alongside the original event log.
// Paging never splits delta streams; agent resume/fork still uses the full log.
func (s *Store) mergeItem(t *bolt.Tx, ev Event) error {
	if ev.Kind == "status" {
		return nil
	}
	if ev.Item == "" {
		ev.Item = ev.ID
	}
	if ev.Kind == "message" && ev.Mode != "delta" {
		ev.Text = presentation.Clean(ev.Text)
		if ev.Text == "" {
			return nil
		}
	}
	hash := sha256.Sum256([]byte(ev.Session + "\x00" + ev.Item + "\x00" + ev.Kind))
	id := hex.EncodeToString(hash[:])
	var first uint64
	err := s.get(t, "itemkeys", id, &first)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var prev Event
	if first != 0 {
		if err := s.get(t, "transcript", sessionIndexKey(ev.Session, first), &prev); err != nil {
			return err
		}
	} else {
		first = ev.Seq
		if err := s.put(t, "itemkeys", id, first); err != nil {
			return err
		}
	}
	if ev.Mode == "delta" {
		ev.Text = prev.Text + ev.Text
	}
	ev.Mode = "snapshot"
	ev.First = first
	if err := s.put(t, "transcript", sessionIndexKey(ev.Session, first), ev); err != nil {
		return err
	}
	return s.indexOutline(t, ev)
}

type HistoryPage struct {
	Events   []Event `json:"events"`
	Cursor   uint64  `json:"cursor"`
	Before   uint64  `json:"before"`
	HasMore  bool    `json:"has_more"`
	HasNewer bool    `json:"has_newer,omitempty"`
}

// Read backwards by first appearance, returning ascending complete items.
// The live cursor and snapshots come from the same transaction, so events that
// arrive during paging are neither skipped nor appended twice.
func (s *Store) History(session string, before uint64, limit int) (HistoryPage, error) {
	out := HistoryPage{Events: []Event{}}
	if session == "" {
		return out, errors.New("missing session")
	}
	if limit < 1 || limit > 40 {
		limit = 40
	}
	err := s.db.View(func(t *bolt.Tx) error {
		out.Cursor = t.Bucket([]byte("events")).Sequence()
		prefix := session + "\x00"
		c := t.Bucket([]byte("transcript")).Cursor()
		bound := before
		if bound == 0 {
			bound = ^uint64(0)
		}
		k, _ := c.Seek([]byte(sessionIndexKey(session, bound)))
		if k == nil {
			k, _ = c.Last()
		} else {
			k, _ = c.Prev()
		}
		size := 0
		for ; k != nil && strings.HasPrefix(string(k), prefix); k, _ = c.Prev() {
			if len(out.Events) == limit || size >= 512<<10 {
				out.HasMore = true
				break
			}
			var ev Event
			if err := s.get(t, "transcript", string(k), &ev); err != nil {
				return err
			}
			if ev.Session != session {
				return errors.New("invalid transcript index")
			}
			out.Events = append(out.Events, ev)
			size += len(ev.Text)
			out.Before = ev.First
		}
		for i, j := 0, len(out.Events)-1; i < j; i, j = i+1, j-1 {
			out.Events[i], out.Events[j] = out.Events[j], out.Events[i]
		}
		return nil
	})
	return out, err
}

// A live update may refer to an item outside the loaded history window. Return
// its complete snapshot so the client can display it without a missing prefix.
// The cursor still advances over original events, preserving every changed item.
func (s *Store) LiveEvents(session string, after uint64, limit int) ([]Event, uint64, error) {
	events, err := s.Events(session, after, limit)
	if err != nil || len(events) == 0 {
		return events, after, err
	}
	cursor := events[len(events)-1].Seq
	out := []Event{}
	seen := map[string]bool{}
	err = s.db.View(func(t *bolt.Tx) error {
		for _, ev := range events {
			if ev.Kind == "status" {
				out = append(out, ev)
				continue
			}
			item := ev.Item
			if item == "" {
				item = ev.ID
			}
			hash := sha256.Sum256([]byte(ev.Session + "\x00" + item + "\x00" + ev.Kind))
			id := hex.EncodeToString(hash[:])
			if seen[id] {
				continue
			}
			seen[id] = true
			var first uint64
			if err := s.get(t, "itemkeys", id, &first); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return err
			}
			var latest Event
			if err := s.get(t, "transcript", sessionIndexKey(session, first), &latest); err != nil {
				return err
			}
			out = append(out, latest)
		}
		return nil
	})
	return out, cursor, err
}
