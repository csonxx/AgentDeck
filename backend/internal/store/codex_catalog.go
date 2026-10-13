package store

import (
	"errors"
	"os"

	bolt "go.etcd.io/bbolt"
)

// SyncCodexVisibility changes sidebar metadata only, preserving encrypted
// history, checkpoints and explicit mobile/desktop-managed conversations.
// An authoritative native catalog also retires orphaned log imports.
func (s *Store) SyncCodexVisibility(visible map[string]bool, authoritative bool) error {
	return s.db.Update(func(t *bolt.Tx) error {
		updates := []Session{}
		if err := t.Bucket([]byte("sessions")).ForEach(func(k, _ []byte) error {
			var ss Session
			if err := s.get(t, "sessions", string(k), &ss); err != nil {
				return err
			}
			if ss.Agent != "codex" || ss.Managed || ss.Source != "log" {
				return nil
			}
			show, known := visible[ss.ID]
			if !known && !authoritative {
				return nil
			}
			if ss.CatalogHidden != !show {
				ss.CatalogHidden = !show
				updates = append(updates, ss)
			}
			return nil
		}); err != nil {
			return err
		}
		for _, ss := range updates {
			if err := s.put(t, "sessions", ss.ID, ss); err != nil {
				return err
			}
		}
		if len(updates) == 0 {
			return nil
		}
		var revision uint64
		if err := s.get(t, "generation", "session_catalog", &revision); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return s.put(t, "generation", "session_catalog", revision+1)
	})
}
