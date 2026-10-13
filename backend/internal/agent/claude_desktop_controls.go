package agent

import (
	"agentdeck/internal/claudedesktop"
	"agentdeck/internal/store"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type claudeDesktopControls interface {
	State(context.Context, claudedesktop.Target) (claudedesktop.State, error)
	Interrupt(context.Context, claudedesktop.Target) error
	Approve(context.Context, claudedesktop.Target, string, string, map[string]string) error
}

func (m *Manager) connectClaudeDesktop(ctx context.Context) (claudeDesktopClient, error) {
	if m.openClaudeDesktop != nil {
		return m.openClaudeDesktop(ctx)
	}
	return claudedesktop.Open(ctx)
}

// Decisions are always checked against the current original Desktop session.
// No subprocess, permission configuration or persistent allow rule is created.
func (m *Manager) controlClaudeDesktop(ss store.Session, key, decision, answer string) (bool, error) {
	if ss.Agent != "claude" || !m.ClaudeDesktopDebugger {
		return false, nil
	}
	target, handled, err := m.claudeDesktopTarget(ss)
	if !handled || err != nil {
		return handled, err
	}
	ctx := m.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	b, err := m.connectClaudeDesktop(ctx)
	if err != nil {
		return true, errors.New(claudeDebuggerUnavailable)
	}
	defer b.Close()
	c, ok := b.(claudeDesktopControls)
	if !ok {
		return true, errors.New("当前 Claude Desktop 不支持此会话操作")
	}
	if key == "" {
		err = c.Interrupt(ctx, target)
	} else {
		var answers map[string]string
		if answer != "" && json.Unmarshal([]byte(answer), &answers) != nil {
			return true, errors.New("审批回答格式无效")
		}
		err = c.Approve(ctx, target, key, decision, answers)
	}
	if err != nil {
		return true, errors.New("Claude 原会话操作未确认，请刷新核对；审批可能已过期")
	}
	if key != "" {
		for ctx.Err() == nil {
			state, e := c.State(ctx, target)
			if e != nil {
				return true, errors.New("Claude 审批结果尚未确认，请刷新核对")
			}
			pending := false
			for _, p := range state.Permissions {
				if p.ID == key && p.Session == target.Host {
					pending = true
				}
			}
			if !pending {
				return true, nil
			}
			select {
			case <-ctx.Done():
			case <-time.After(100 * time.Millisecond):
			}
		}
		return true, errors.New("Claude 审批结果尚未确认，请刷新核对")
	}
	return true, nil
}

func (m *Manager) followClaudeDesktop(ctx context.Context, ss store.Session) error {
	target, handled, err := m.claudeDesktopTarget(ss)
	if err != nil {
		return err
	}
	if !handled {
		return errors.New("Desktop session unavailable")
	}
	b, err := m.connectClaudeDesktop(ctx)
	if err != nil {
		return err
	}
	defer b.Close()
	c, ok := b.(claudeDesktopControls)
	if !ok {
		return errors.New("Desktop state unsupported")
	}
	seen := map[string]string{}
	stateKey := "claude-native-approvals/" + ss.ID
	_ = m.Store.Get("agentconfig", stateKey, &seen)
	if seen == nil {
		seen = map[string]string{}
	}
	appendApproval := func(id, text, status string) error {
		sum := sha256.Sum256([]byte(status + "\x00" + text))
		_, _, e := m.Store.AppendCatalogHistory(store.Event{ID: fmt.Sprintf("claude-native-approval/%s/%s/%x", ss.ID, id, sum), Session: ss.ID, Agent: ss.Agent, Project: ss.Project, Kind: "approval", Item: id, Text: text, Status: status, Mode: "snapshot", Source: "claude-desktop-live"})
		return e
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for ctx.Err() == nil {
		state, err := c.State(ctx, target)
		if err != nil {
			return err
		}
		status := "idle"
		if state.Running {
			status = "running"
		}
		if len(state.Permissions) > 0 {
			status = "waiting"
		}
		if err = m.Store.ObserveClaudeStatus(ss.ID, status, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		active := map[string]string{}
		changed := false
		for _, p := range state.Permissions {
			if p.ID == "" || p.Session != target.Host {
				continue
			}
			text := Redact(jsonText(map[string]any{"tool_name": p.Tool, "input": p.Input, "description": p.Description, "reason": p.Reason}))
			active[p.ID] = text
			if seen[p.ID] != text {
				changed = true
				if err = appendApproval(p.ID, text, "pending"); err != nil {
					return err
				}
			}
		}
		for id := range seen {
			if _, ok := active[id]; !ok {
				changed = true
				if err = appendApproval(id, "", "resolved"); err != nil {
					return err
				}
			}
		}
		seen = active
		if changed {
			if err = m.Store.Put("agentconfig", stateKey, seen); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
	return ctx.Err()
}
