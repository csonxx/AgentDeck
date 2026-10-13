package store

import (
	"errors"
	bolt "go.etcd.io/bbolt"
	"os"
	"time"
)

type claudeStatusClock struct {
	Time   string
	Status string
}

// Status has its own source clock. Catalog activity can be written after an
// end_turn, so comparing status against Session.Updated drops valid completion.
func (s *Store) applyClaudeStatus(t *bolt.Tx, ss *Session, status, at string, authoritative bool) error {
	stamp, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil
	}
	key := "claude/status/" + ss.ID
	var clock claudeStatusClock
	if err = s.get(t, "agentconfig", key, &clock); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if clock.Time != "" {
		previous, e := time.Parse(time.RFC3339Nano, clock.Time)
		if e == nil && stamp.Before(previous) {
			return nil
		}
	} else if !authoritative && status == "running" {
		// A bounded historical import alone cannot revive an old unfinished turn.
		previous, e := time.Parse(time.RFC3339Nano, ss.Updated)
		if e == nil && stamp.Before(previous) {
			return nil
		}
	}
	if clock.Time != at || clock.Status != status {
		if err = s.put(t, "agentconfig", key, claudeStatusClock{Time: at, Status: status}); err != nil {
			return err
		}
	}
	if ss.Status == status {
		return nil
	}
	ss.Status = status
	if err = s.put(t, "sessions", ss.ID, *ss); err != nil {
		return err
	}
	var revision uint64
	if err = s.get(t, "generation", "session_catalog", &revision); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.put(t, "generation", "session_catalog", revision+1)
}

// A bounded tail observation establishes the latest native state without
// importing the entire conversation or changing its title/project/activity.
func (s *Store) ObserveClaudeStatus(id, status, at string) error {
	return s.db.Update(func(t *bolt.Tx) error {
		var ss Session
		if err := s.get(t, "sessions", id, &ss); err != nil {
			return err
		}
		if ss.Agent != "claude" || ss.Source != "claude-desktop" {
			return nil
		}
		return s.applyClaudeStatus(t, &ss, status, at, true)
	})
}
