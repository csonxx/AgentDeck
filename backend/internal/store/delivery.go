package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	bolt "go.etcd.io/bbolt"
	"os"
)

type Delivery struct {
	ID      string `json:"id"`
	Session string `json:"session"`
	State   string `json:"state"`
	Created string `json:"created"`
	Hash    string `json:"hash"`
}

func (s *Store) EnqueueDelivery(d Delivery, payload any) error {
	b, e := json.Marshal(payload)
	if e != nil {
		return e
	}
	if len(b) > 24<<20 {
		return errors.New("队列附件超过大小限制")
	}
	h := sha256.Sum256(b)
	d.Hash = hex.EncodeToString(h[:])
	return s.db.Update(func(t *bolt.Tx) error {
		var existing Delivery
		if e := s.get(t, "deliveries", d.ID, &existing); e == nil {
			if existing.Session != d.Session || existing.Hash != d.Hash {
				return errors.New("待发消息标识冲突")
			}
			return nil
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		active, session := 0, 0
		if e := t.Bucket([]byte("deliveries")).ForEach(func(k, v []byte) error {
			var x Delivery
			if e := s.get(t, "deliveries", string(k), &x); e != nil {
				return e
			}
			if x.State == "queued" || x.State == "dispatching" {
				active++
				if x.Session == d.Session {
					session++
				}
			}
			return nil
		}); e != nil {
			return e
		}
		if active >= 200 || session >= 20 {
			return errors.New("消息队列已满")
		}
		if e = s.put(t, "deliverypayloads", d.ID, json.RawMessage(b)); e != nil {
			return e
		}
		return s.put(t, "deliveries", d.ID, d)
	})
}
func (s *Store) Deliveries(states ...string) ([]Delivery, error) {
	if len(states) == 0 {
		states = []string{"queued"}
	}
	out := []Delivery{}
	e := s.db.View(func(t *bolt.Tx) error {
		return t.Bucket([]byte("deliveries")).ForEach(func(k, v []byte) error {
			var d Delivery
			if e := s.get(t, "deliveries", string(k), &d); e != nil {
				return e
			}
			for _, state := range states {
				if d.State == state {
					out = append(out, d)
					break
				}
			}
			return nil
		})
	})
	return out, e
}
func (s *Store) TransitionDelivery(id, session, from, to string) (bool, error) {
	changed := false
	e := s.db.Update(func(t *bolt.Tx) error {
		var d Delivery
		if e := s.get(t, "deliveries", id, &d); e != nil {
			return e
		}
		if d.Session != session {
			return errors.New("待发消息与会话不匹配")
		}
		if d.State != from {
			return nil
		}
		d.State = to
		if e := s.put(t, "deliveries", id, d); e != nil {
			return e
		}
		if to == "sent" || to == "cancelled" {
			if e := t.Bucket([]byte("deliverypayloads")).Delete([]byte(id)); e != nil {
				return e
			}
		}
		changed = true
		return nil
	})
	return changed, e
}

// Edit and dispatch compete in the same transaction. An edit cannot revive or
// replace a message that has already been taken by the delivery worker.
func (s *Store) EditDelivery(id, session, version string, payload any) error {
	b, e := json.Marshal(payload)
	if e != nil {
		return e
	}
	if len(b) > 24<<20 {
		return errors.New("队列附件超过大小限制")
	}
	return s.db.Update(func(t *bolt.Tx) error {
		var d Delivery
		if e := s.get(t, "deliveries", id, &d); e != nil {
			return e
		}
		if d.Session != session || d.Hash != version || d.State != "queued" {
			return errors.New("电脑队列已变化，请刷新后重试")
		}
		h := sha256.Sum256(b)
		d.Hash = hex.EncodeToString(h[:])
		if e := s.put(t, "deliverypayloads", id, json.RawMessage(b)); e != nil {
			return e
		}
		return s.put(t, "deliveries", id, d)
	})
}
