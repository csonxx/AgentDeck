package agent

func (m *Manager) claudeRuntimePID(id string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.runs[id]; r != nil {
		select {
		case <-r.done:
		default:
			if r.cmd != nil && r.cmd.Process != nil {
				return r.cmd.Process.Pid
			}
		}
	}
	return 0
}
