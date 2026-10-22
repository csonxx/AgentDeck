package agent

import "context"

// PrepareUpdate is conservative: it never interrupts running tasks, pending
// approvals, goal execution or queued input. The caller must also serialize
// public commands while it acquires this lease.
func (m *Manager) PrepareUpdate(ctx context.Context) bool {
	if !m.updateMu.TryLock() {
		return false
	}
	defer m.updateMu.Unlock()
	if m.updateDraining {
		return true
	}
	sessions, err := m.Store.Sessions()
	if err != nil {
		return false
	}
	for _, ss := range sessions {
		switch ss.Status {
		case "running", "pending", "waiting":
			return false
		}
	}
	for _, state := range []string{"queued", "dispatching", "unknown"} {
		rows, err := m.Store.Deliveries(state)
		if err != nil || len(rows) != 0 {
			return false
		}
	}
	m.mu.Lock()
	for _, r := range m.runs {
		r.mu.Lock()
		busy := r.busy || len(r.approvals) != 0 || r.claudeInputs != 0
		r.mu.Unlock()
		if busy {
			m.mu.Unlock()
			return false
		}
	}
	m.mu.Unlock()
	// Active native goals may resume without new input; wait until paused/done.
	goals, err := m.Goals()
	if err != nil || ctx.Err() != nil {
		return false
	}
	for _, goal := range goals {
		if goal.Status == "active" {
			return false
		}
	}
	if pending, err := m.Store.HasPendingApprovals(); err != nil || pending {
		return false
	}
	m.updateDraining = true
	return true
}
func (m *Manager) ResumeAfterUpdate() {
	m.updateMu.Lock()
	m.updateDraining = false
	m.updateMu.Unlock()
}

// HoldForUpdate prevents new work until a trial worker is verified.
func (m *Manager) HoldForUpdate() {
	m.updateMu.Lock()
	m.updateDraining = true
	m.updateMu.Unlock()
}
