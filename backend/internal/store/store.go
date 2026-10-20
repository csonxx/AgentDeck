// Package store encrypts every persistent value, including devices, checkpoints,
// event text and media. The owner-only wrapping key is a host trust boundary.
package store

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agentdeck/internal/presentation"
	"agentdeck/internal/secure"
	bolt "go.etcd.io/bbolt"
)

// SessionIssue contains only allowlisted presentation, never raw provider errors.
type SessionIssue struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Action      string `json:"action,omitempty"`
	BlocksInput bool   `json:"blocks_input,omitempty"`
}
type Event struct {
	Position uint64       `json:"position,omitempty"` // presentation only; never a paging cursor
	Detail   bool         `json:"detail,omitempty"`   // wire summary; full contents remain in the vault
	Issue    SessionIssue `json:"issue,omitzero"`
	Project  string       `json:"project,omitempty"`
	Seq      uint64       `json:"seq"`
	First    uint64       `json:"first,omitempty"`
	ID       string       `json:"id"`
	Session  string       `json:"session"`
	Agent    string       `json:"agent"`
	Kind     string       `json:"kind"`
	Role     string       `json:"role,omitempty"`
	Text     string       `json:"text,omitempty"`
	Item     string       `json:"item,omitempty"`
	Mode     string       `json:"mode,omitempty"` // delta or snapshot, per item
	Status   string       `json:"status,omitempty"`
	Media    string       `json:"media,omitempty"`
	Time     string       `json:"time"`
	Source   string       `json:"source"`
}
type Session struct {
	Issue         SessionIssue `json:"issue,omitzero"`
	Completion    uint64       `json:"completion,omitempty"`
	CompletedAt   string       `json:"completed_at,omitempty"`
	CatalogHidden bool         `json:"catalog_hidden,omitempty"`
	Project       string       `json:"project,omitempty"`
	Managed       bool         `json:"managed,omitempty"`
	Parent        string       `json:"parent,omitempty"`
	ID            string       `json:"id"`
	Agent         string       `json:"agent"`
	Title         string       `json:"title"`
	Source        string       `json:"source"`
	Updated       string       `json:"updated"`
	Status        string       `json:"status"`
}
type Device struct {
	LastConnected string `json:"last_connected,omitempty"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Root          []byte `json:"root"`
	Created       string `json:"created"`
	RelayGrant    string `json:"relay_grant,omitempty"`
}
type Media struct {
	MIME string `json:"mime"`
	Data []byte `json:"bytes"`
}
type Store struct {
	db  *bolt.DB
	key []byte
}

var buckets = []string{"deliveries", "deliverypayloads", "events", "sessionevents", "transcript", "itemkeys", "outline", "sessions", "seen", "devices", "media", "checkpoints", "invites", "gwtenants", "gwnodes", "gwgrants", "relayconfig", "gwflows", "gwoauth", "gwaccounts", "relaypending", "commands", "agentconfig", "generation", "native_settings", "uploads", "uploadchunks", "mediaowners", "goals"}

func New(dir string) (*Store, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	_ = os.Chmod(dir, 0700)
	p := filepath.Join(dir, "storage.key")
	k, e := os.ReadFile(p)
	if os.IsNotExist(e) {
		k = secure.Random(32)
		e = os.WriteFile(p, k, 0600)
	}
	if e != nil {
		return nil, e
	}
	if len(k) != 32 {
		return nil, errors.New("invalid storage key")
	}
	if e = os.Chmod(p, 0600); e != nil {
		return nil, e
	}
	db, e := bolt.Open(filepath.Join(dir, "vault.db"), 0600, &bolt.Options{Timeout: time.Second})
	if e != nil {
		return nil, e
	}
	s := &Store{db, k}
	e = db.Update(func(t *bolt.Tx) error {
		rebuildIndex := t.Bucket([]byte("sessionevents")) == nil
		rebuildItems := t.Bucket([]byte("transcript")) == nil
		rebuildOutline := t.Bucket([]byte("outline")) == nil
		for _, n := range buckets {
			if _, e := t.CreateBucketIfNotExists([]byte(n)); e != nil {
				return e
			}
		}
		if rebuildIndex || rebuildItems {
			if err := t.Bucket([]byte("events")).ForEach(func(k, _ []byte) error {
				var ev Event
				if err := s.get(t, "events", string(k), &ev); err != nil {
					return err
				}
				if rebuildIndex {
					if err := s.put(t, "sessionevents", sessionIndexKey(ev.Session, ev.Seq), ev.Seq); err != nil {
						return err
					}
				}
				if rebuildItems {
					return s.mergeItem(t, ev)
				}
				return nil
			}); err != nil {
				return err
			}
		}
		if rebuildOutline && !rebuildItems {
			if err := t.Bucket([]byte("transcript")).ForEach(func(k, _ []byte) error {
				var ev Event
				if err := s.get(t, "transcript", string(k), &ev); err != nil {
					return err
				}
				return s.indexOutline(t, ev)
			}); err != nil {
				return err
			}
		}
		return s.repairDesktopCitations(t)
	})
	if e != nil {
		db.Close()
	}
	return s, e
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) put(t *bolt.Tx, b, k string, v any) error {
	box, e := secure.SealJSON(s.key, "storage/"+b+"/"+k, 0, 0, v)
	if e != nil {
		return e
	}
	d, e := json.Marshal(box)
	if e != nil {
		return e
	}
	return t.Bucket([]byte(b)).Put([]byte(k), d)
}
func (s *Store) get(t *bolt.Tx, b, k string, v any) error {
	d := t.Bucket([]byte(b)).Get([]byte(k))
	if d == nil {
		return os.ErrNotExist
	}
	var box secure.Box
	if e := json.Unmarshal(d, &box); e != nil {
		return e
	}
	p, e := secure.Open(s.key, "storage/"+b+"/"+k, box)
	if e != nil {
		return e
	}
	return json.Unmarshal(p, v)
}
func (s *Store) Put(b, k string, v any) error {
	return s.db.Update(func(t *bolt.Tx) error { return s.put(t, b, k, v) })
}
func (s *Store) Get(b, k string, v any) error {
	return s.db.View(func(t *bolt.Tx) error { return s.get(t, b, k, v) })
}
func (s *Store) Delete(b, k string) error {
	return s.db.Update(func(t *bolt.Tx) error { return t.Bucket([]byte(b)).Delete([]byte(k)) })
}

// Values decrypts administrative records without exposing wrapping keys. Callers
// must explicitly select a bucket; public APIs never return secret-bearing rows.
func (s *Store) Values(bucket string) ([]json.RawMessage, error) {
	out := []json.RawMessage{}
	err := s.db.View(func(t *bolt.Tx) error {
		return t.Bucket([]byte(bucket)).ForEach(func(k, _ []byte) error {
			var v json.RawMessage
			if err := s.get(t, bucket, string(k), &v); err != nil {
				return err
			}
			out = append(out, v)
			return nil
		})
	})
	return out, err
}
func (s *Store) Devices() ([]Device, error) {
	out := []Device{}
	e := s.db.View(func(t *bolt.Tx) error {
		return t.Bucket([]byte("devices")).ForEach(func(k, v []byte) error {
			var d Device
			if e := s.get(t, "devices", string(k), &d); e != nil {
				return e
			}
			d.Root = nil
			out = append(out, d)
			return nil
		})
	})
	return out, e
}

func (s *Store) RecordDeviceConnection(id string, at time.Time) error {
	return s.db.Update(func(t *bolt.Tx) error {
		var d Device
		if err := s.get(t, "devices", id, &d); err != nil {
			return err
		}
		d.LastConnected = at.UTC().Format(time.RFC3339Nano)
		return s.put(t, "devices", id, d)
	})
}
func (s *Store) Append(ev Event) (Event, bool, error) { return s.append(ev, false) }

// Imported desktop history must not replace the desktop catalog's title,
// original project or activity timestamp with a historical log record.
func (s *Store) AppendCatalogHistory(ev Event) (Event, bool, error) { return s.append(ev, true) }

// Recovered image records advance the stream without changing chat metadata.
func (s *Store) AppendRecoveredImage(ev Event) (Event, bool, error) {
	if ev.Kind != "image" {
		return ev, false, errors.New("recovery only accepts images")
	}
	return s.append(ev, true)
}
func (s *Store) append(ev Event, preserveSession bool) (Event, bool, error) {
	return s.appendMode(ev, preserveSession, false, "")
}
func (s *Store) appendMode(ev Event, preserveSession, desktop bool, expectedUpdated string) (Event, bool, error) {
	var added bool
	err := s.db.Update(func(t *bolt.Tx) error {
		var err error
		ev, added, err = s.appendInTx(t, ev, preserveSession, desktop, expectedUpdated)
		return err
	})
	return ev, added, err
}

// Bounded importer transactions avoid a durable fsync for every tool record.
// Deduplication, aliases, transcript and session status commit together.
func (s *Store) AppendImported(events []Event, catalog bool) ([]Event, error) {
	out := []Event{}
	err := s.db.Update(func(t *bolt.Tx) error {
		for _, ev := range events {
			stored, added, err := s.appendInTx(t, ev, catalog, ev.Agent == "codex", "")
			if err != nil {
				return err
			}
			if added {
				out = append(out, stored)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Store) appendInTx(t *bolt.Tx, ev Event, preserveSession, desktop bool, expectedUpdated string) (Event, bool, error) {
	added := false
	sourceTime := ev.Time
	e := func() error {
		seen := t.Bucket([]byte("seen"))
		if seen.Get([]byte(ev.ID)) != nil {
			return nil
		}
		if expectedUpdated != "" {
			var current Session
			if err := s.get(t, "sessions", ev.Session, &current); err != nil {
				return err
			}
			if current.Updated != expectedUpdated {
				return nil
			}
		}
		if desktop {
			duplicate, err := s.reconcileDesktop(t, &ev)
			if err != nil {
				return err
			}
			if duplicate {
				return nil
			}
		}
		b := t.Bucket([]byte("events"))
		seq, e := b.NextSequence()
		if e != nil {
			return e
		}
		ev.Seq = seq
		if ev.Time == "" {
			ev.Time = time.Now().UTC().Format(time.RFC3339Nano)
		}
		key := seqKey(seq)
		if e = s.put(t, "events", key, ev); e != nil {
			return e
		}
		if e = s.put(t, "sessionevents", sessionIndexKey(ev.Session, seq), seq); e != nil {
			return e
		}
		if e = s.mergeItem(t, ev); e != nil {
			return e
		}
		if e = seen.Put([]byte(ev.ID), []byte{1}); e != nil {
			return e
		}
		if e = s.recordCompletion(t, ev); e != nil {
			return e
		}
		if preserveSession {
			if ev.Agent == "claude" && ev.Source == "log" && ev.Status != "" && ev.Kind != "approval" {
				var ss Session
				if err := s.get(t, "sessions", ev.Session, &ss); err == nil && ss.Source == "claude-desktop" {
					if err = s.applyClaudeStatus(t, &ss, ev.Status, sourceTime, false); err != nil {
						return err
					}
				}
			}
			added = true
			return nil
		}
		var ss Session
		e = s.get(t, "sessions", ev.Session, &ss)
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if ss.ID == "" {
			ss = Session{ID: ev.Session, Agent: ev.Agent, Title: ev.Agent + " 会话", Source: ev.Source, Status: "idle"}
		}
		if ev.Project != "" {
			ss.Project = ev.Project
		}
		if ev.Kind == "message" && ev.Role == "user" && ev.Text != "" && (ss.Title == ev.Agent+" 会话" || ss.Title == "新会话") {
			r := []rune(ev.Text)
			if len(r) > 70 {
				r = r[:70]
			}
			ss.Title = presentation.Title(string(r))
		}
		if ev.Kind == "status" && ev.Mode == "title" {
			ss.Title = ev.Text
		}
		stale := olderDesktopEvent(ev, ss) || (ss.Issue.BlocksInput && ss.Source == "desktop" && ev.Source != "desktop-live" && ev.Status != "")
		if !stale {
			ss.Updated = ev.Time
		}
		if !stale && ev.Kind == "status" {
			// A log tail must not reopen a Desktop safety stop. Only a fresh
			// authoritative owner snapshot may clear that restriction.
			if !(ss.Issue.BlocksInput && ss.Source == "desktop" && ev.Source != "desktop-live") {
				ss.Issue = ev.Issue
			}
		}
		if !stale && ev.Status != "" && ev.Kind != "approval" {
			if ev.Agent == "claude" {
				if err := s.applyClaudeStatus(t, &ss, ev.Status, ev.Time, true); err != nil {
					return err
				}
			} else {
				ss.Status = ev.Status
			}
		}
		if e = s.put(t, "sessions", ss.ID, ss); e != nil {
			return e
		}
		added = true
		return nil
	}()
	return ev, added, e
}
func seqKey(v uint64) string {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return string(b)
}
func sessionIndexKey(session string, seq uint64) string { return session + "\x00" + seqKey(seq) }
func (s *Store) Events(session string, after uint64, limit int) ([]Event, error) {
	out := []Event{}
	if limit < 1 || limit > 100 {
		limit = 100
	}
	err := s.db.View(func(t *bolt.Tx) error {
		bucket := "events"
		start := seqKey(after + 1)
		prefix := ""
		if session != "" {
			bucket = "sessionevents"
			prefix = session + "\x00"
			start = sessionIndexKey(session, after+1)
		}
		c := t.Bucket([]byte(bucket)).Cursor()
		for k, _ := c.Seek([]byte(start)); k != nil; k, _ = c.Next() {
			key := string(k)
			if prefix != "" {
				if !strings.HasPrefix(key, prefix) {
					break
				}
				var seq uint64
				if e := s.get(t, "sessionevents", key, &seq); e != nil {
					return e
				}
				key = seqKey(seq)
			}
			var ev Event
			if e := s.get(t, "events", key, &ev); e != nil {
				return e
			}
			if session != "" && ev.Session != session {
				return errors.New("invalid session index")
			}
			out = append(out, ev)
			if len(out) == limit {
				break
			}
		}
		return nil
	})
	return out, err
}

func (s *Store) Sessions() ([]Session, error) {
	out := []Session{}
	e := s.db.View(func(t *bolt.Tx) error {
		return t.Bucket([]byte("sessions")).ForEach(func(k, v []byte) error {
			var ss Session
			if e := s.get(t, "sessions", string(k), &ss); e != nil {
				return e
			}
			if !ss.CatalogHidden {
				var completion completionMarker
				if e := s.get(t, "agentconfig", "completion/"+ss.ID, &completion); e != nil && !errors.Is(e, os.ErrNotExist) {
					return e
				}
				ss.Completion, ss.CompletedAt = completion.Revision, completion.Time
				out = append(out, ss)
			}
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Updated > out[j].Updated })
	return out, e
}
func (s *Store) LastSeq() uint64 {
	var v uint64
	_ = s.db.View(func(t *bolt.Tx) error { v = t.Bucket([]byte("events")).Sequence(); return nil })
	return v
}
func (s *Store) Stats() string {
	ss, _ := s.Sessions()
	return fmt.Sprintf("sessions=%d events=%d", len(ss), s.LastSeq())
}

// Take atomically consumes a one-time invitation, even with concurrent requests.
func (s *Store) Take(b, k string, v any) error {
	return s.db.Update(func(t *bolt.Tx) error {
		if e := s.get(t, b, k, v); e != nil {
			return e
		}
		return t.Bucket([]byte(b)).Delete([]byte(k))
	})
}

func (s *Store) SetProject(id, project string) error {
	var current Session
	if err := s.Get("sessions", id, &current); err != nil {
		return err
	} else if current.Project == project {
		return nil
	}
	return s.db.Update(func(t *bolt.Tx) error {
		var ss Session
		if err := s.get(t, "sessions", id, &ss); err != nil {
			return err
		}
		if ss.Project == project {
			return nil
		}
		ss.Project = project
		return s.put(t, "sessions", id, ss)
	})
}

// RepairPresentation rewrites only encrypted presentation rows. Pairing roots,
// source offsets, authorization records and event sequences remain intact.
func (s *Store) RepairPresentation() error {
	return s.db.Update(func(t *bolt.Tx) error {
		titles := map[string]string{}
		changed := false
		if err := t.Bucket([]byte("events")).ForEach(func(k, v []byte) error {
			var ev Event
			if err := s.get(t, "events", string(k), &ev); err != nil {
				return err
			}
			if ev.Kind == "message" && ev.Mode != "delta" {
				clean := presentation.Clean(ev.Text)
				if clean != ev.Text {
					changed = true
					ev.Text = clean
					if clean == "" {
						ev.Kind = "status"
					}
					if err := s.put(t, "events", string(k), ev); err != nil {
						return err
					}
				}
				if ev.Role == "user" && clean != "" && titles[ev.Session] == "" {
					titles[ev.Session] = presentation.Title(clean)
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if changed {
			for _, name := range []string{"transcript", "itemkeys", "outline"} {
				if err := t.DeleteBucket([]byte(name)); err != nil {
					return err
				}
				if _, err := t.CreateBucket([]byte(name)); err != nil {
					return err
				}
			}
			if err := t.Bucket([]byte("events")).ForEach(func(k, _ []byte) error {
				var ev Event
				if err := s.get(t, "events", string(k), &ev); err != nil {
					return err
				}
				return s.mergeItem(t, ev)
			}); err != nil {
				return err
			}
		}
		return t.Bucket([]byte("sessions")).ForEach(func(k, v []byte) error {
			var ss Session
			if err := s.get(t, "sessions", string(k), &ss); err != nil {
				return err
			}
			title := presentation.Title(ss.Title)
			if title == "" || strings.Contains(ss.Title, "<environment_context>") || strings.HasPrefix(ss.Title, "# AGENTS.md") {
				title = titles[ss.ID]
			}
			if title == "" {
				title = ss.Agent + " 会话"
			}
			ss.Title = title
			return s.put(t, "sessions", string(k), ss)
		})
	})
}

// RecoverManaged marks work interrupted by a host restart and invalidates old
// approvals with fresh events so already-connected phone caches also update.
func (s *Store) RecoverManaged() error {
	updates := []Event{}
	err := s.db.Update(func(t *bolt.Tx) error {
		if err := t.Bucket([]byte("sessions")).ForEach(func(k, v []byte) error {
			var ss Session
			if e := s.get(t, "sessions", string(k), &ss); e != nil {
				return e
			}
			if ss.Managed && ss.Status == "running" {
				ss.Status = "interrupted"
				updates = append(updates, Event{Session: ss.ID, Agent: ss.Agent, Project: ss.Project, Kind: "status", Status: "interrupted", Source: "stream"})
				return s.put(t, "sessions", ss.ID, ss)
			}
			return nil
		}); err != nil {
			return err
		}
		return t.Bucket([]byte("events")).ForEach(func(k, v []byte) error {
			var ev Event
			if e := s.get(t, "events", string(k), &ev); e != nil {
				return e
			}
			if ev.Kind == "approval" && ev.Status == "pending" {
				ev.Status = "resolved"
				ev.Text = "工作站已重启，原审批失效"
				updates = append(updates, ev)
				return s.put(t, "events", string(k), ev)
			}
			return nil
		})
	})
	if err != nil {
		return err
	}
	for _, ev := range updates {
		ev.ID = fmt.Sprintf("restart:%d:%s", time.Now().UnixNano(), ev.ID)
		ev.Time = time.Now().UTC().Format(time.RFC3339Nano)
		if _, _, err = s.Append(ev); err != nil {
			return err
		}
	}
	return nil
}
