package agent

import (
	"agentdeck/internal/claudedesktop"
	"agentdeck/internal/store"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type nativeControlFixture struct {
	*nativeDesktopFixture
	mu                    sync.Mutex
	state                 claudedesktop.State
	approvals, interrupts int
	decision              string
	answers               map[string]string
}

func (f *nativeControlFixture) State(context.Context, claudedesktop.Target) (claudedesktop.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, nil
}
func (f *nativeControlFixture) Interrupt(context.Context, claudedesktop.Target) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.interrupts++
	f.state.Running = false
	return nil
}
func (f *nativeControlFixture) Approve(_ context.Context, t claudedesktop.Target, id, decision string, answers map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.state.Permissions {
		if p.ID == id && p.Session == t.Host {
			f.approvals++
			f.decision = decision
			f.answers = answers
			f.state.Permissions = nil
			return nil
		}
	}
	return errors.New("expired")
}

func TestClaudeDesktopPermissionReconnectionAndControl(t *testing.T) {
	m, ss, f, _ := nativeDesktopManager(t)
	c := &nativeControlFixture{nativeDesktopFixture: f, state: claudedesktop.State{Running: true, Permissions: []claudedesktop.Permission{{ID: "approval-1", Session: "local_original", Tool: "AskUserQuestion", Input: map[string]any{"questions": []any{map[string]any{"question": "pick", "header": "choice"}}}}}}}
	m.openClaudeDesktop = func(context.Context) (claudeDesktopClient, error) { return c, nil }
	start := func() (context.CancelFunc, chan error) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- m.followClaudeDesktop(ctx, ss) }()
		return cancel, done
	}
	wait := func(status string, wantEvents int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var s store.Session
			e := m.Store.Get("sessions", ss.ID, &s)
			events, e2 := m.Store.Events(ss.ID, 0, 100)
			if e == nil && e2 == nil && s.Status == status && len(events) == wantEvents {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("native state or approval event not synchronized")
	}
	cancel, done := start()
	wait("waiting", 1)
	cancel()
	<-done
	// A reconnect reads encrypted prior state and must not duplicate the card.
	cancel, done = start()
	wait("waiting", 1)
	if handled, err := m.controlClaudeDesktop(ss, "approval-1", "accept", `{"pick":"one"}`); !handled || err != nil {
		t.Fatal("native approval failed", err)
	}
	wait("running", 2)
	events, _ := m.Store.Events(ss.ID, 0, 100)
	if events[0].Item != "approval-1" || events[0].Status != "pending" || !strings.Contains(events[0].Text, "AskUserQuestion") || events[1].Item != "approval-1" || events[1].Status != "resolved" {
		t.Fatal("approval identity lost")
	}
	if handled, err := m.controlClaudeDesktop(ss, "approval-1", "accept", ""); !handled || err == nil {
		t.Fatal("expired approval reused")
	}
	if handled, err := m.controlClaudeDesktop(ss, "", "", ""); !handled || err != nil {
		t.Fatal("native stop failed", err)
	}
	wait("idle", 2)
	cancel()
	<-done
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.approvals != 1 || c.interrupts != 1 || c.decision != "accept" || c.answers["pick"] != "one" {
		t.Fatal("control resent or changed answer")
	}
	if len(m.runs) != 0 {
		t.Fatal("replacement executor created")
	}
}
