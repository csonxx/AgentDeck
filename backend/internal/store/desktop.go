package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	bolt "go.etcd.io/bbolt"
	"os"
	"strings"
	"time"
)

type desktopAlias struct{ Item, Native, Log, Source, LogText string }

func desktopKey(kind string, ev Event, value string) string {
	h := sha256.Sum256([]byte(ev.Session + "\x00" + ev.Kind + "\x00" + ev.Role + "\x00" + value))
	return "desktop/" + kind + "/" + hex.EncodeToString(h[:])
}

// Resolve native snapshots and rollout records atomically without merging
// distinct repeated messages. Alias values are encrypted with the event store.
func (s *Store) reconcileDesktop(t *bolt.Tx, ev *Event) (bool, error) {
	if ev.Kind == "status" && ev.Source == "desktop-live" {
		var ss Session
		if err := s.get(t, "sessions", ev.Session, &ss); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		return ss.Status == ev.Status && ev.Status != "", nil
	}
	if ev.Mode != "snapshot" || ev.Kind == "status" || ev.Kind == "notice" || ev.Kind == "approval" {
		return false, nil
	}
	native := ev.Source == "desktop-live"
	original := ev.Item
	if original == "" {
		original = ev.ID
		ev.Item = original
	}
	a := desktopAlias{}
	if err := s.get(t, "agentconfig", desktopKey("native", *ev, original), &a); err != nil && !errors.Is(err, os.ErrNotExist) {
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
