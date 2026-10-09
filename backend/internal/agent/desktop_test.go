package agent

import (
	"agentdeck/internal/store"
	"context"
	"errors"
	"path/filepath"
	"testing"
)

type desktopFixture struct {
	thread       DesktopThread
	sentID, text string
	reject       bool
	reads        int
}

func (f *desktopFixture) Read(context.Context, string) (DesktopThread, error) {
	f.reads++
	if f.reject {
		return DesktopThread{}, errors.New("offline")
	}
	return f.thread, nil
}
func (f *desktopFixture) Send(_ context.Context, id, text, request, project string) error {
	f.sentID = id
	f.text = text
	return nil
}
func TestDesktopOriginalIDAndProjectGuard(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	db, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	original := store.Session{ID: "original", Agent: "codex", Project: root, Title: "original title", Source: "log", Status: "idle"}
	db.Put("sessions", original.ID, original)
	f := &desktopFixture{thread: DesktopThread{ID: original.ID, Kind: "codex", Project: root, Host: "local"}}
	m := &Manager{Store: db, Roots: []string{root}, Desktop: f}
	s, err := m.Create("codex", m.Projects()[0].ID, original.ID)
	if err != nil || s.ID != original.ID || s.Source != "desktop" || !s.Managed {
		t.Fatal("desktop continuation forked or lost original ID")
	}
	ss, _ := db.Sessions()
	if len(ss) != 1 {
		t.Fatal("created duplicate desktop chat")
	}
	if err = m.Send(s.ID, "fixture user message", "req"); err != nil || f.sentID != original.ID {
		t.Fatal("message did not reach exact desktop target")
	}
	f.thread.Project = filepath.Dir(root)
	f.sentID = ""
	if err = m.Send(s.ID, "must not send", "bad"); err == nil || f.sentID != "" {
		t.Fatal("changed desktop project bypassed authorization")
	}
	f.reject = true
	if _, err = m.Create("codex", m.Projects()[0].ID, original.ID); err == nil {
		t.Fatal("offline bridge silently forked")
	}
	if err = m.Interrupt(s.ID); err == nil {
		t.Fatal("unsupported desktop interrupt pretended success")
	}
}
