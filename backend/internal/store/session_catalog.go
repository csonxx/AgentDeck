package store

import (
	"errors"
	"os"

	bolt "go.etcd.io/bbolt"
)

// PutCatalogSession publishes metadata-only changes to connected clients too.
// A child-session correction need not produce a new transcript event.
func (s *Store) PutCatalogSession(ss Session) error {
	return s.db.Update(func(t *bolt.Tx) error {
		var old Session
		err := s.get(t, "sessions", ss.ID, &old)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			ss.Issue = old.Issue
			if old.Issue.BlocksInput {
				ss.Status = old.Status
			}
		}
		if err == nil && old == ss {
			return nil
		}
		if err := s.put(t, "sessions", ss.ID, ss); err != nil {
			return err
		}
		var revision uint64
		if err := s.get(t, "generation", "session_catalog", &revision); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return s.put(t, "generation", "session_catalog", revision+1)
	})
}
