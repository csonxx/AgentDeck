package store

import (
	"errors"
	"strings"

	bolt "go.etcd.io/bbolt"
)

// Tiny encrypted previews let the phone index all imported dialogue without
// decrypting/transferring every message body or individual token event.
type OutlineEntry struct {
	Key     string `json:"key"`
	First   uint64 `json:"first"`
	Kind    string `json:"kind"`
	Role    string `json:"role"`
	Preview string `json:"preview"`
	Time    string `json:"time"`
}
type OutlinePage struct {
	Entries []OutlineEntry `json:"entries"`
	Before  uint64         `json:"before"`
	HasMore bool           `json:"has_more"`
}

func (s *Store) indexOutline(t *bolt.Tx, ev Event) error {
	if ev.Kind != "message" && ev.Kind != "image" && ev.Kind != "notice" {
		return nil
	}
	text := strings.Join(strings.Fields(ev.Text), " ")
	runes := []rune(text)
	if len(runes) > 160 {
		text = string(runes[:160]) + "…"
	}
	if ev.Kind == "image" {
		text = "图片"
	}
	if text == "" {
		return nil
	}
	v := OutlineEntry{Key: ev.Item + ":" + ev.Kind, First: ev.First, Kind: ev.Kind, Role: ev.Role, Preview: text, Time: ev.Time}
	return s.put(t, "outline", sessionIndexKey(ev.Session, ev.First), v)
}
func (s *Store) Outline(session string, before uint64, limit int) (OutlinePage, error) {
	out := OutlinePage{Entries: []OutlineEntry{}}
	if session == "" {
		return out, errors.New("missing session")
	}
	if limit < 1 || limit > 200 {
		limit = 200
	}
	err := s.db.View(func(t *bolt.Tx) error {
		prefix := session + "\x00"
		c := t.Bucket([]byte("outline")).Cursor()
		if before == 0 {
			before = ^uint64(0)
		}
		k, _ := c.Seek([]byte(sessionIndexKey(session, before)))
		if k == nil {
			k, _ = c.Last()
		} else {
			k, _ = c.Prev()
		}
		for ; k != nil && strings.HasPrefix(string(k), prefix); k, _ = c.Prev() {
			if len(out.Entries) == limit {
				out.HasMore = true
				break
			}
			var v OutlineEntry
			if err := s.get(t, "outline", string(k), &v); err != nil {
				return err
			}
			out.Entries = append(out.Entries, v)
			out.Before = v.First
		}
		return nil
	})
	return out, err
}

// The selected record is the first item in a bounded forward window. Older
// paging remains available; a directory jump never drains intervening history.
func (s *Store) HistoryAt(session string, first uint64) (HistoryPage, error) {
	out := HistoryPage{Events: []Event{}}
	if session == "" || first == 0 {
		return out, errors.New("missing anchor")
	}
	err := s.db.View(func(t *bolt.Tx) error {
		prefix := session + "\x00"
		c := t.Bucket([]byte("transcript")).Cursor()
		k, _ := c.Seek([]byte(sessionIndexKey(session, first)))
		if k == nil || string(k) != sessionIndexKey(session, first) {
			return errors.New("unknown anchor")
		}
		var anchor Event
		if err := s.get(t, "transcript", string(k), &anchor); err != nil {
			return err
		}
		pk, _ := c.Prev()
		out.HasMore = pk != nil && strings.HasPrefix(string(pk), prefix)
		k, _ = c.Seek([]byte(sessionIndexKey(session, first)))
		size := 0
		for ; k != nil && strings.HasPrefix(string(k), prefix); k, _ = c.Next() {
			if len(out.Events) == 40 || size >= 512<<10 {
				out.HasNewer = true
				break
			}
			var ev Event
			if err := s.get(t, "transcript", string(k), &ev); err != nil {
				return err
			}
			if ev.Session != session {
				return errors.New("invalid anchor session")
			}
			out.Events = append(out.Events, ev)
			size += len(ev.Text)
		}
		out.Before = first
		out.Cursor = t.Bucket([]byte("events")).Sequence()
		return nil
	})
	return out, err
}
