package store

import (
	"errors"
	"os"

	bolt "go.etcd.io/bbolt"
)

// SyncClaudeCatalog reconciles presentation metadata only. Old events and
// checkpoints remain encrypted and recoverable; managed mobile branches stay.
func (s *Store) SyncClaudeCatalog(entries []Session) error {
	return s.db.Update(func(t *bolt.Tx) error {
		wanted := make(map[string]Session, len(entries))
		for _, entry := range entries {
			if entry.ID == "" || entry.Agent != "claude" || entry.Source != "claude-desktop" {
				return errors.New("invalid Claude catalog entry")
			}
			wanted[entry.ID] = entry
		}
		changed := false
		hidden := []Session{}
		if err := t.Bucket([]byte("sessions")).ForEach(func(k, _ []byte) error {
			var old Session
			if err := s.get(t, "sessions", string(k), &old); err != nil {
				return err
			}
			if old.Agent != "claude" || (old.Managed && old.Source != "claude-desktop") || (old.Source != "log" && old.Source != "claude-desktop") {
				return nil
			}
			if _, exists := wanted[old.ID]; !exists && !old.CatalogHidden {
				old.CatalogHidden = true
				hidden = append(hidden, old)
			}
			return nil
		}); err != nil {
			return err
		}
		// Never mutate a Bolt bucket while its ForEach cursor is active.
		for _, entry := range hidden {
			changed = true
			if err := s.put(t, "sessions", entry.ID, entry); err != nil {
				return err
			}
		}
		for id, entry := range wanted {
			var old Session
			err := s.get(t, "sessions", id, &old)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if old.Managed && old.Source != "claude-desktop" {
				continue
			}
			entry.Managed = old.Managed
			entry.Issue = old.Issue
			if old.Managed && old.Updated > entry.Updated {
				entry.Updated = old.Updated
			}
			if old.Status != "" {
				entry.Status = old.Status
			}
			if entry != old {
				changed = true
				if err := s.put(t, "sessions", id, entry); err != nil {
					return err
				}
			}
		}
		if changed {
			var revision uint64
			if err := s.get(t, "generation", "session_catalog", &revision); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return s.put(t, "generation", "session_catalog", revision+1)
		}
		return nil
	})
}

func (s *Store) SessionCatalogRevision() uint64 {
	var revision uint64
	_ = s.Get("generation", "session_catalog", &revision)
	return revision
}
