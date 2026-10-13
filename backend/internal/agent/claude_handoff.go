package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var errClaudeActive = errors.New("Claude 原会话仍在执行，尚未交接")

// Only the native registry can authorize a handoff. An argv match alone is
// insufficient evidence to terminate anything. Read-only selection does not
// call this path: it is used only when dispatching an owner's explicit input.
func (m *Manager) claudeHandoffOwner(id string, ownPID int) (*claudeProcessRecord, error) {
	if m.ClaudeOwner != nil {
		return nil, m.ClaudeOwner(id, ownPID)
	}
	snapshot := m.ClaudeProcessSnapshot
	if snapshot == nil {
		snapshot = claudeProcessSnapshot
	}
	processes, err := snapshot()
	if err != nil {
		return nil, errors.New("无法确认 Claude 会话占用状态，请稍后重试")
	}
	paths, err := filepath.Glob(filepath.Join(m.claudeProcessRoot(), "*.json"))
	if err != nil {
		return nil, err
	}
	var owner *claudeProcessRecord
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<10 || !desktopOwned(info) {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var record claudeProcessRecord
		if json.Unmarshal(b, &record) != nil || record.Session != id || record.PID <= 0 || record.PID == ownPID || filepath.Base(path) != fmt.Sprint(record.PID)+".json" || record.Start == "" || processes[record.PID] != strings.Join(strings.Fields(record.Start), " ") {
			continue
		}
		if info.Mode().Perm()&0022 != 0 {
			return nil, errors.New("Claude 进程记录权限不安全")
		}
		if owner != nil {
			return nil, errors.New(claudeOwnerBusy)
		}
		copy := record
		owner = &copy
	}
	if owner == nil {
		return nil, m.checkClaudeOwner(id, ownPID)
	}
	if owner.Parked != "" || owner.At <= 0 || nativeClaudeStatus(owner.Status) == "" {
		return nil, errors.New(claudeOwnerBusy)
	}
	return owner, nil
}

func (m *Manager) acquireClaude(id string) error {
	owner, err := m.claudeHandoffOwner(id, 0)
	if err != nil || owner == nil {
		return err
	}
	if owner.EntryPoint == "claude-desktop" || owner.BridgeSessionID != "" {
		return errors.New("Claude 桌面原进程不能交接；请使用原会话的 Remote Control 连接")
	}
	if owner.Status != "idle" {
		return errClaudeActive
	}
	// Recheck the process birth and native status immediately before SIGTERM.
	// Never force kill, signal the Desktop application, or interrupt a busy turn.
	latest, err := m.claudeHandoffOwner(id, 0)
	if err != nil {
		return err
	}
	if latest == nil {
		return nil
	}
	if *latest != *owner || latest.Status != "idle" {
		return errClaudeActive
	}
	if m.claudeRelease != nil {
		err = m.claudeRelease(*owner)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		comm, e := exec.CommandContext(ctx, "ps", "-p", fmt.Sprint(owner.PID), "-o", "comm=").Output()
		if e != nil {
			return errors.New(claudeOwnerBusy)
		}
		executable := strings.TrimSpace(string(comm))
		local, _ := exec.LookPath("claude")
		local, _ = filepath.EvalSymlinks(local)
		// The Electron application is capital-C Claude; the bundled native CLI
		// is lower-case claude. Version-named binaries must match our CLI path.
		if filepath.Base(executable) != "claude" && (local == "" || executable != local) {
			return errors.New(claudeOwnerBusy)
		}
		p, e := os.FindProcess(owner.PID)
		if e != nil {
			return e
		}
		err = p.Signal(syscall.SIGTERM)
	}
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		return errors.New("Claude 原会话执行交接失败，消息未发送")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, err := m.claudeHandoffOwner(id, 0)
		if err != nil {
			return err
		}
		if current == nil {
			return nil
		}
		if current.PID != owner.PID || current.Start != owner.Start {
			return errors.New("Claude 原会话执行端发生变化，消息未发送")
		}
		if m.Context != nil {
			select {
			case <-m.Context.Done():
				return m.Context.Err()
			default:
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	return errors.New("Claude 原会话尚未完成执行交接，消息未发送")
}
