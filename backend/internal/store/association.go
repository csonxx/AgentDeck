package store

import (
	"encoding/json"

	bolt "go.etcd.io/bbolt"
)

// ForgetAssociation clears routing enrollment and unfinished pairing requests
// atomically, retaining conversation history and independently encrypted devices.
func (s *Store) ForgetAssociation() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, key := range []string{"gateway", "name"} {
			if err := tx.Bucket([]byte("relayconfig")).Delete([]byte(key)); err != nil {
				return err
			}
		}
		for _, bucket := range []string{"relaypending", "invites"} {
			if err := tx.DeleteBucket([]byte(bucket)); err != nil {
				return err
			}
			if _, err := tx.CreateBucket([]byte(bucket)); err != nil {
				return err
			}
		}
		return nil
	})
}

// RevokeGatewayLocal removes an identity and all grants it owns in one commit.
func (s *Store) RevokeGatewayLocal(id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket([]byte("gwnodes")).Delete([]byte(id)); err != nil {
			return err
		}
		b := tx.Bucket([]byte("gwgrants"))
		var keys [][]byte
		if err := b.ForEach(func(k, _ []byte) error {
			var grant struct {
				Local string `json:"local"`
			}
			var raw json.RawMessage
			if err := s.get(tx, "gwgrants", string(k), &raw); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &grant); err != nil {
				return err
			}
			if grant.Local == id {
				keys = append(keys, append([]byte(nil), k...))
			}
			return nil
		}); err != nil {
			return err
		}
		for _, k := range keys {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}
