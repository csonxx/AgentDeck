package store

import (
	"errors"
	"os"
	"time"

	bolt "go.etcd.io/bbolt"
)

type completionMarker struct {
	Revision uint64
	Time     string
	Item     string
}

// Separate encrypted markers survive catalog metadata replacement. A tool's
// completion or title edit must never look like a finished conversation turn.
func (s *Store) recordCompletion(t *bolt.Tx, ev Event) error {
	if ev.Kind != "status" || ev.Status != "completed" && ev.Status != "idle" {
		return nil
	}
	if ev.Status == "idle" {
		var previous Session
		if err := s.get(t, "sessions", ev.Session, &previous); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if previous.Status != "running" && previous.Status != "pending" && previous.Status != "waiting" {
			return nil
		}
	}
	key := "completion/" + ev.Session
	var old completionMarker
	if err := s.get(t, "agentconfig", key, &old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if ev.Source == "desktop-live" && ev.Item != "" && old.Item == ev.Item {
		return nil
	}
	stamp, err := time.Parse(time.RFC3339Nano, ev.Time)
	if err != nil {
		return nil
	}
	if before, err := time.Parse(time.RFC3339Nano, old.Time); err == nil && !stamp.After(before) {
		return nil
	}
	return s.put(t, "agentconfig", key, completionMarker{Revision: ev.Seq, Time: ev.Time, Item: ev.Item})
}
