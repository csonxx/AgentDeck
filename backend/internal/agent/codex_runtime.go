package agent

import (
	"agentdeck/internal/store"
	"encoding/json"
	"time"
)

// get leases its result through the caller's native operation. Completion may
// arrive before turn/start's acknowledgement; never retire that in-flight call.
func (r *runtime) releaseUse() {
	r.mu.Lock()
	if r.users > 0 {
		r.users--
	}
	eligible := r.session.Agent == "codex" && r.retireIdle && !r.busy && r.users == 0
	r.mu.Unlock()
	if eligible {
		go r.manager.releaseIdleCodex(r.session.ID)
	}
}
func (r *runtime) retireAfterTurn() {
	r.mu.Lock()
	if r.session.Agent == "codex" {
		r.retireIdle = true
	}
	r.mu.Unlock()
	go r.manager.releaseIdleCodex(r.session.ID)
}

// Active goals can finish after turn/completed. Recheck without taking a writer
// or spawning one timer per notification. Cancellation/exit stops the check.
func (r *runtime) retryIdleRelease() {
	r.mu.Lock()
	if r.idleRetry || r.retiring {
		r.mu.Unlock()
		return
	}
	r.idleRetry = true
	r.mu.Unlock()
	go func() {
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-r.done:
			return
		case <-nonNil(r.manager.Context).Done():
			return
		case <-timer.C:
		}
		r.mu.Lock()
		r.idleRetry = false
		r.mu.Unlock()
		r.manager.releaseIdleCodex(r.session.ID)
	}()
}

// Only AgentDeck's own idle writer is released. No Desktop process, active turn,
// pending approval or borrowed control RPC is interrupted, and no lock is deleted.
func (m *Manager) releaseIdleCodex(id string) {
	m.mu.Lock()
	r := m.runs[id]
	if r == nil || r.session.Agent != "codex" {
		m.mu.Unlock()
		return
	}
	r.mu.Lock()
	if r.retiring {
		done := r.done
		r.mu.Unlock()
		m.mu.Unlock()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
		return
	}
	if !r.retireIdle || r.busy || r.users != 0 || len(r.pending) != 0 || len(r.approvals) != 0 {
		r.mu.Unlock()
		m.mu.Unlock()
		return
	}
	session := r.session
	r.mu.Unlock()
	m.mu.Unlock()
	// A continuing native Goal still owns its executor between turns.
	if goals, err := m.codexGoals([]store.Session{session}); err == nil {
		for _, goal := range goals {
			if goal.Session == id && goal.Status == "active" {
				r.retryIdleRelease()
				return
			}
		}
	} else {
		r.retryIdleRelease()
		return
	}
	m.mu.Lock()
	if m.runs[id] != r {
		m.mu.Unlock()
		return
	}
	r.mu.Lock()
	if r.retiring || !r.retireIdle || r.busy || r.users != 0 || len(r.pending) != 0 || len(r.approvals) != 0 {
		r.mu.Unlock()
		m.mu.Unlock()
		return
	}
	r.retiring = true
	permissions, _ := json.Marshal(r.permissions)
	r.mu.Unlock()
	m.mu.Unlock()
	if len(permissions) > 2 {
		_ = m.Store.Put("agentconfig", "codex-last-permissions/"+id, json.RawMessage(permissions))
	}
	// EOF allows native shutdown to flush and release its writer lock normally.
	_ = r.input.Close()
	select {
	case <-r.done:
	case <-time.After(1500 * time.Millisecond):
		_ = r.cmd.Process.Kill()
		select {
		case <-r.done:
		case <-time.After(1500 * time.Millisecond):
			return
		}
	}
	m.mu.Lock()
	if m.runs[id] == r {
		delete(m.runs, id)
	}
	m.mu.Unlock()
}

// Stop/approval are operations on an existing executor, never reasons to resume
// a completed thread and acquire a new writer lock.
func (m *Manager) borrowLiveRuntime(id string) *runtime {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.runs[id]
	if r == nil {
		return nil
	}
	select {
	case <-r.done:
		return nil
	default:
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.retiring {
		return nil
	}
	r.users++
	return r
}
