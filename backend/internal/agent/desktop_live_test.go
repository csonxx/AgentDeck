package agent

import (
	"agentdeck/internal/store"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDesktopReadOnlyLive(t *testing.T) {
	pipe := os.Getenv("AGENTDECK_DESKTOP_SOCKET")
	if pipe == "" {
		t.Skip("explicit read-only desktop acceptance")
	}
	b := &DesktopBridge{Socket: pipe}
	id := os.Getenv("AGENTDECK_DESKTOP_THREAD")
	v, err := b.Read(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if v.ID != id || v.Host != "local" {
		t.Fatal("desktop original thread/project mismatch")
	}
}

// Explicitly enabled read-only live acceptance. No turn is created or sent.
func TestDesktopFollowReadOnlyLive(t *testing.T) {
	socket := os.Getenv("AGENTDECK_DESKTOP_SOCKET")
	if socket == "" {
		t.Skip("explicit read-only desktop acceptance")
	}
	id := os.Getenv("AGENTDECK_DESKTOP_THREAD")
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	b := &DesktopBridge{Socket: socket}
	_, err := b.Read(ctx, id)
	if err != nil {
		t.Fatal("read-only owner discovery failed")
	}
	n := setup(t)
	ss := store.Session{ID: id, Agent: "codex", Project: "", Source: "desktop", Managed: true}
	n.Store.Put("sessions", id, ss)
	p := &desktopProjection{m: &Manager{Store: n.Store, Normalizer: n}, session: ss, seen: map[string]string{}}
	observed := false
	err = b.Follow(ctx, id, func(state map[string]any) error {
		if state["id"] != id || len(desktopTurns(state)) == 0 {
			return errors.New("live state shape mismatch")
		}
		// The discovery reply intentionally carries no cwd. Bind this read-only
		// test to the selected owner's snapshot; production uses the imported,
		// authorized project from Store instead.
		if p.session.Project == "" {
			cwd := str(state["cwd"])
			if !filepath.IsAbs(cwd) {
				return errors.New("unexpected live project scope")
			}
			p.session.Project = cwd
		}
		if err := p.publish(state); err != nil {
			return err
		}
		var got store.Session
		if n.Store.Get("sessions", id, &got) != nil || got.Status == "" {
			return errors.New("native status missing")
		}
		observed = true
		cancel()
		return nil
	})
	if !observed || (err != nil && !errors.Is(err, context.Canceled)) {
		t.Fatalf("live native snapshot projection failed: observed=%v error=%v", observed, err)
	}
}
