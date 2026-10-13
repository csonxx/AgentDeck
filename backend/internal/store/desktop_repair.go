package store

import (
	"agentdeck/internal/presentation"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	bolt "go.etcd.io/bbolt"
	"os"
	"strings"
)

const desktopCitationRepair = "desktop/citation-repair/v1"

// Repair only the materialized presentation. The encrypted source event log,
// session metadata, pairing and sequence are preserved. Opposite-source adjacent
// replies must differ only by a known internal citation block within one user
// turn. Each source is paired at most once. Equal replies in other turns stay.
func (s *Store) repairDesktopCitations(t *bolt.Tx) error {
	var done bool
	if err := s.get(t, "agentconfig", desktopCitationRepair, &done); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if done {
		return nil
	}
	var messages []Event
	if err := t.Bucket([]byte("transcript")).ForEach(func(k, _ []byte) error {
		var ev Event
		if err := s.get(t, "transcript", string(k), &ev); err != nil {
			return err
		}
		if ev.Agent == "codex" && ev.Kind == "message" {
			messages = append(messages, ev)
		}
		return nil
	}); err != nil {
		return err
	}
	type candidate struct {
		event  Event
		paired bool
	}
	var turn []candidate
	session := ""
	for _, ev := range messages {
		if session != ev.Session || ev.Role == "user" {
			turn = nil
			session = ev.Session
		}
		raw := ev.Text
		ev.Text = presentation.Clean(raw)
		match := -1
		var previous Event
		if ev.Role == "assistant" && ev.Text != "" {
			for i := len(turn) - 1; i >= 0; i-- {
				v := turn[i]
				opposite := (v.event.Source == "log" && ev.Source == "desktop-live") || (v.event.Source == "desktop-live" && ev.Source == "log")
				if !v.paired && opposite && presentation.Clean(v.event.Text) == ev.Text && (strings.Contains(v.event.Text, "<oai-mem-citation>") || strings.Contains(raw, "<oai-mem-citation>")) {
					match = i
					previous = v.event
					break
				}
			}
		}
		if match >= 0 {
			alias := desktopAlias{Item: previous.Item, Source: "desktop-live"}
			for _, v := range []Event{previous, ev} {
				if v.Source == "desktop-live" {
					alias.Native = v.Item
				} else {
					alias.Log = v.Item
					alias.LogText = desktopKey("fingerprint", v, ev.Text)
				}
			}
			for kind, id := range map[string]string{"native": alias.Native, "log": alias.Log, "text": ev.Text} {
				if err := s.put(t, "agentconfig", desktopKey(kind, ev, id), alias); err != nil {
					return err
				}
			}
			// Both original item identities resolve to the surviving complete snapshot.
			h := sha256.Sum256([]byte(ev.Session + "\x00" + ev.Item + "\x00" + ev.Kind))
			if err := s.put(t, "itemkeys", hex.EncodeToString(h[:]), previous.First); err != nil {
				return err
			}
			if err := t.Bucket([]byte("transcript")).Delete([]byte(sessionIndexKey(ev.Session, ev.First))); err != nil {
				return err
			}
			if err := t.Bucket([]byte("outline")).Delete([]byte(sessionIndexKey(ev.Session, ev.First))); err != nil {
				return err
			}
			merged := ev
			if previous.Seq > ev.Seq {
				merged = previous
				merged.Text = ev.Text
			}
			merged.Item, merged.First = previous.Item, previous.First
			if err := s.put(t, "transcript", sessionIndexKey(merged.Session, merged.First), merged); err != nil {
				return err
			}
			if err := s.indexOutline(t, merged); err != nil {
				return err
			}
			turn[match] = candidate{event: merged, paired: true}
			continue
		}
		if ev.Text != raw {
			if err := s.put(t, "transcript", sessionIndexKey(ev.Session, ev.First), ev); err != nil {
				return err
			}
			if err := s.indexOutline(t, ev); err != nil {
				return err
			}
		}
		if ev.Role == "assistant" {
			original := ev
			original.Text = raw
			turn = append(turn, candidate{event: original})
		}
	}
	return s.put(t, "agentconfig", desktopCitationRepair, true)
}
