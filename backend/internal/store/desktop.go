package store

import (
	"agentdeck/internal/presentation"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	bolt "go.etcd.io/bbolt"
	"os"
	"strings"
	"time"
)

type desktopAlias struct{ Item, Native, Log, Source, LogText string }

// Retain concurrent status/history updates when following an existing owner.
func (s *Store) AdoptCodexDesktop(id, project string) error {
	return s.db.Update(func(t *bolt.Tx) error {
		var current Session
		if err := s.get(t, "sessions", id, &current); err != nil {
			return err
		}
		if current.Agent != "codex" || current.Project != project {
			return errors.New("desktop project mismatch")
		}
		current.Source = "desktop"
		current.Managed = true
		return s.put(t, "sessions", id, current)
	})
}

func desktopKey(kind string, ev Event, value string) string {
	h := sha256.Sum256([]byte(ev.Session + "\x00" + ev.Kind + "\x00" + ev.Role + "\x00" + value))
	return "desktop/" + kind + "/" + hex.EncodeToString(h[:])
}

// Preserve a cached protocol reply's row when its presentation becomes readable.
// Call only for a strictly validated structured user reply, never ordinary text.
func (s *Store) ExistingDesktopReplyItem(session, source, original, text string) (string, error) {
	var item string
	err := s.db.View(func(t *bolt.Tx) error {
		ev := Event{Session: session, Kind: "message", Role: "user"}
		kind := "log"
		if source == "desktop-live" {
			kind = "native"
		}
		keys := []string{desktopKey(kind, ev, original+":0"), desktopKey(kind, ev, original), desktopKey("text", ev, presentation.Clean(text))}
		for _, key := range keys {
			var alias desktopAlias
			if err := s.get(t, "agentconfig", key, &alias); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return err
			}
			if alias.Item != "" {
				item = alias.Item
				break
			}
		}
		return nil
	})
	return item, err
}

// Resolve native snapshots and rollout records atomically without merging
// distinct repeated messages. Alias values are encrypted with the event store.
func (s *Store) reconcileDesktop(t *bolt.Tx, ev *Event) (bool, error) {
	if ev.Kind == "status" && ev.Source == "desktop-live" {
		var ss Session
		if err := s.get(t, "sessions", ev.Session, &ss); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		if ss.Issue != ev.Issue {
			return false, nil
		}
		if ss.Status == ev.Status && ev.Status == "completed" && ev.Item != "" {
			var marker completionMarker
			if err := s.get(t, "agentconfig", "completion/"+ev.Session, &marker); err != nil && !errors.Is(err, os.ErrNotExist) {
				return false, err
			}
			return marker.Item == ev.Item, nil
		}
		return ss.Status == ev.Status && ev.Status != "", nil
	}
	if ev.Mode != "snapshot" || ev.Kind == "status" || ev.Kind == "notice" || ev.Kind == "approval" {
		return false, nil
	}
	if ev.Kind == "message" {
		ev.Text = presentation.Clean(ev.Text)
	}
	native := ev.Source == "desktop-live"
	original := ev.Item
	if original == "" {
		original = ev.ID
		ev.Item = original
	}
	a := desktopAlias{}
	aliasKind := "log"
	if native {
		aliasKind = "native"
	}
	if err := s.get(t, "agentconfig", desktopKey(aliasKind, *ev, original), &a); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if a.Item == "" {
		candidate := desktopAlias{}
		if err := s.get(t, "agentconfig", desktopKey("text", *ev, ev.Text), &candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		if native && candidate.Native == "" && candidate.Source == "log" {
			a = candidate
		}
		if !native && candidate.Native != "" && (candidate.Log == "" || candidate.Log == original || candidate.LogText != desktopKey("fingerprint", *ev, ev.Text)) {
			a = candidate
		}
	}
	if a.Item == "" {
		a.Item = original
	}
	if native {
		a.Native = original
		a.Source = "desktop-live"
	} else {
		a.Log = original
		a.LogText = desktopKey("fingerprint", *ev, ev.Text)
		if a.Source == "" {
			a.Source = "log"
		}
	}
	if a.Native != "" {
		if err := s.put(t, "agentconfig", desktopKey("native", *ev, a.Native), a); err != nil {
			return false, err
		}
	}
	if a.Log != "" {
		if err := s.put(t, "agentconfig", desktopKey("log", *ev, a.Log), a); err != nil {
			return false, err
		}
	}
	ev.Item = a.Item
	if err := s.put(t, "agentconfig", desktopKey("text", *ev, ev.Text), a); err != nil {
		return false, err
	}
	h := sha256.Sum256([]byte(ev.Session + "\x00" + ev.Item + "\x00" + ev.Kind))
	var first uint64
	if err := s.get(t, "itemkeys", hex.EncodeToString(h[:]), &first); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if first != 0 && (native || a.Native != "") {
		var previous Event
		if err := s.get(t, "transcript", sessionIndexKey(ev.Session, first), &previous); err != nil {
			return false, err
		}
		if previous.Text == ev.Text && previous.Media == ev.Media {
			return true, nil
		}
		if !native && previous.Source == "desktop-live" && strings.HasPrefix(previous.Text, ev.Text) {
			return true, nil
		}
	}
	return false, nil
}
func (s *Store) AppendDesktop(ev Event, preserveSession bool) (Event, bool, error) {
	return s.appendMode(ev, preserveSession, true, "")
}

// An acknowledgement must not overwrite a newer native execution state.
func (s *Store) MarkDesktopAccepted(ev Event, expectedUpdated string) (Event, bool, error) {
	return s.appendMode(ev, false, false, expectedUpdated)
}
func olderDesktopEvent(ev Event, ss Session) bool {
	if ev.Source != "log" || ss.Agent != "codex" {
		return false
	}
	a, ea := time.Parse(time.RFC3339Nano, ev.Time)
	b, eb := time.Parse(time.RFC3339Nano, ss.Updated)
	return ea == nil && eb == nil && a.Before(b)
}

// Losing a read-only owner subscription must not leave a stale "running" or
// "ready" claim. CAS prevents overwriting a fresh turn or safety restriction.
func (s *Store) MarkDesktopUnobserved(id string) error {
	var previous Session
	if err := s.Get("sessions", id, &previous); err != nil {
		return err
	}
	if previous.Status == "unknown" || previous.Issue.Code != "" || previous.Updated == "" {
		return nil
	}
	_, _, err := s.appendMode(Event{ID: "desktop-unobserved/" + id + "/" + previous.Updated, Session: id, Agent: previous.Agent, Project: previous.Project, Source: "desktop-live", Kind: "status", Status: "unknown"}, false, false, previous.Updated)
	return err
}
