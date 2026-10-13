package agent

import (
	"agentdeck/internal/store"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Goal struct {
	Session     string   `json:"session"`
	Agent       string   `json:"agent"`
	Objective   string   `json:"objective"`
	Status      string   `json:"status"`
	Tokens      int64    `json:"tokens_used"`
	Budget      *int64   `json:"token_budget,omitempty"`
	Seconds     int64    `json:"time_used_seconds"`
	Iterations  int64    `json:"iterations,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	Updated     int64    `json:"updated_at"`
	Created     int64    `json:"created_at"`
	Version     string   `json:"version"`
	Actions     []string `json:"actions"`
	ControlNote string   `json:"control_note,omitempty"`
}
type storedClaudeGoal struct {
	Goal  *Goal
	Stamp string
}

func decodeCodexGoal(id string, raw any) *Goal {
	if raw == nil {
		return nil
	}
	b, e := json.Marshal(raw)
	if e != nil {
		return nil
	}
	var v struct {
		Thread            string `json:"threadId"`
		Objective, Status string
		Tokens            int64  `json:"tokensUsed"`
		Budget            *int64 `json:"tokenBudget"`
		Seconds           int64  `json:"timeUsedSeconds"`
		Created           int64  `json:"createdAt"`
		Updated           int64  `json:"updatedAt"`
	}
	if json.Unmarshal(b, &v) != nil || v.Thread != id || strings.TrimSpace(v.Objective) == "" {
		return nil
	}
	switch v.Status {
	case "active", "paused", "blocked", "usageLimited", "budgetLimited", "complete":
	default:
		return nil
	}
	return &Goal{Session: id, Agent: "codex", Objective: Redact(v.Objective), Status: v.Status, Tokens: v.Tokens, Budget: v.Budget, Seconds: v.Seconds, Created: v.Created, Updated: v.Updated}
}

// Read-only goal discovery never resumes a chat or runs inference. Only the
// explicit user mutation path calls set/clear, with native user provenance.
func (m *Manager) codexGoalControl(fn func(func(string, map[string]any) (map[string]any, error)) error) error {
	ctx := m.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "codex", "app-server")
	cmd.Dir, _ = os.UserHomeDir()
	if m.CodexGoalPath != "" {
		cmd.Env = append(os.Environ(), "CODEX_HOME="+filepath.Dir(m.CodexGoalPath))
	}
	cmd.Stderr = io.Discard
	in, e := cmd.StdinPipe()
	if e != nil {
		return e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	if e = cmd.Start(); e != nil {
		return e
	}
	defer func() { in.Close(); cmd.Process.Kill(); cmd.Wait() }()
	scan := bufio.NewScanner(out)
	scan.Buffer(make([]byte, 64<<10), 8<<20)
	call := func(method string, params map[string]any) (map[string]any, error) {
		id := UUID()
		if e := json.NewEncoder(in).Encode(map[string]any{"id": id, "method": method, "params": params}); e != nil {
			return nil, e
		}
		for scan.Scan() {
			var v map[string]any
			if json.Unmarshal(scan.Bytes(), &v) != nil || str(v["id"]) != id {
				continue
			}
			if v["error"] != nil {
				return nil, errors.New("目标接口不可用，请更新电脑 Agent 后重试")
			}
			return obj(v["result"]), nil
		}
		return nil, errors.New("目标接口未确认，请刷新后检查状态")
	}
	if _, e = call("initialize", map[string]any{"clientInfo": map[string]any{"name": "agentdeck", "version": "0.4.17"}, "capabilities": map[string]any{"experimentalApi": true}}); e != nil {
		return e
	}
	if e = json.NewEncoder(in).Encode(map[string]any{"method": "initialized"}); e != nil {
		return e
	}
	return fn(call)
}
func (m *Manager) decorateGoal(g Goal, s store.Session) Goal {
	g.Actions = []string{}
	if g.Agent == "codex" {
		if s.Managed && s.Source != "desktop" {
			g.Actions = []string{"pause", "delete"}
			g.ControlNote = "此电脑会话的目标需在电脑恢复"
		} else if m.Desktop != nil {
			g.Actions = []string{"pause", "delete"}
			g.ControlNote = "此电脑会话的目标需在电脑恢复"
		}
	} else if s.Managed && s.Source != "claude-desktop" {
		g.Actions = []string{"pause", "resume", "delete"}
	} else {
		g.ControlNote = "此 Claude 目标由电脑会话持有，请在电脑暂停、恢复或清除"
	}
	b, _ := json.Marshal([]any{g.Session, g.Agent, g.Objective, g.Created})
	g.Version = digest(string(b))
	return g
}
func (m *Manager) Goals() ([]Goal, error) {
	m.goalMu.Lock()
	defer m.goalMu.Unlock()
	return m.goalsLocked(false)
}
func (m *Manager) goalsLocked(force bool) ([]Goal, error) {
	if !force && time.Since(m.goalChecked) < 5*time.Second {
		return m.goalCache, nil
	}
	sessions, e := m.Store.Sessions()
	if e != nil {
		return nil, e
	}
	m.scanClaudeGoals(sessions)
	result := []Goal{}
	for _, s := range sessions {
		if s.Agent == "claude" {
			var v storedClaudeGoal
			if m.Store.Get("goals", s.ID, &v) == nil && v.Goal != nil {
				result = append(result, m.decorateGoal(*v.Goal, s))
			}
		}
	}
	nativeGoals, e := m.codexGoals(sessions)
	if e != nil {
		return nil, e
	}
	result = append(result, nativeGoals...)
	m.goalChecked = time.Now()
	m.goalCache = result
	return result, nil
}
func (m *Manager) ChangeGoal(id, version, action, request string) error {
	if action != "pause" && action != "resume" && action != "delete" {
		return errors.New("无效目标操作")
	}
	m.goalMu.Lock()
	defer m.goalMu.Unlock()
	var s store.Session
	if m.Store.Get("sessions", id, &s) != nil {
		return errors.New("会话不存在")
	}
	if _, e := m.allowed(s.Project); e != nil {
		return e
	}
	goals, e := m.goalsLocked(true)
	if e != nil {
		return e
	}
	var current *Goal
	for _, g := range goals {
		if g.Session == id {
			copy := g
			current = &copy
			break
		}
	}
	if current == nil || version == "" || version != current.Version {
		return errors.New("目标已变化，请刷新后重试")
	}
	allowed := false
	for _, a := range current.Actions {
		if a == action {
			allowed = true
		}
	}
	if !allowed {
		return errors.New(current.ControlNote)
	}
	if (current.Status == "complete" || current.Status == "failed") && action != "delete" {
		return errors.New("目标已经完成")
	}
	if action == "pause" && current.Status == "paused" || action == "resume" && current.Status == "active" {
		return nil
	}
	if s.Agent == "codex" {
		if !s.Managed || s.Source == "desktop" {
			bridge, ok := m.Desktop.(interface {
				PauseGoal(context.Context, string, string) error
			})
			if !ok {
				return errors.New("电脑接口暂不支持目标操作")
			}
			if current.Status == "active" {
				if e = bridge.PauseGoal(m.Context, id, s.Project); e != nil {
					return e
				}
			}
		} else if current.Status == "active" {
			if e = m.Interrupt(id); e != nil {
				return e
			}
		}
		e = m.codexGoalControl(func(call func(string, map[string]any) (map[string]any, error)) error {
			// Explicit mutation only: attach the same native thread so its goal
			// RPC is available. Discovery never resumes chats or starts turns.
			if _, err := call("thread/resume", map[string]any{"threadId": id}); err != nil {
				return err
			}
			r, err := call("thread/goal/get", map[string]any{"threadId": id})
			if err != nil {
				return err
			}
			goal := decodeCodexGoal(id, r["goal"])
			if goal == nil || m.decorateGoal(*goal, s).Version != version {
				return errors.New("目标已变化，请刷新后重试")
			}
			method := "thread/goal/set"
			params := map[string]any{"threadId": id, "origin": "user", "status": "paused"}
			if action == "delete" {
				method = "thread/goal/clear"
				delete(params, "status")
			}
			_, err = call(method, params)
			return err
		})
	} else {
		if action == "pause" || action == "delete" {
			e = m.Interrupt(id)
		}
		if e == nil && action == "delete" {
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				m.mu.Lock()
				run := m.runs[id]
				m.mu.Unlock()
				if run == nil {
					break
				}
				run.mu.Lock()
				busy := run.busy
				run.mu.Unlock()
				if !busy {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			e = m.Send(id, "/goal clear", request)
			if e == nil {
				for time.Now().Before(deadline) {
					var state storedClaudeGoal
					if m.Store.Get("goals", id, &state) == nil && state.Goal == nil {
						break
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
		}
		if e == nil && action == "resume" {
			e = m.Send(id, "Continue working toward the active goal.", request)
		}
		if e == nil && action == "pause" {
			current.Status = "paused"
			current.Updated = time.Now().Unix()
			e = m.Store.Put("goals", id, storedClaudeGoal{Goal: current, Stamp: time.Now().UTC().Format(time.RFC3339Nano)})
		}
	}
	m.goalChecked = time.Time{}
	if e != nil {
		return e
	}
	updated, e := m.goalsLocked(true)
	if e != nil {
		return e
	}
	for _, g := range updated {
		if g.Session == id {
			if action == "delete" {
				return errors.New("清除目标尚未确认，请刷新后检查")
			}
			if action == "pause" && g.Status != "paused" {
				return errors.New("暂停目标尚未确认，请刷新后检查")
			}
			return nil
		}
	}
	if action != "delete" {
		return errors.New("目标状态已变化，请刷新后检查")
	}
	return nil
}
func (n *Normalizer) captureClaudeGoal(d map[string]any, st *State) {
	n.goalRecordMu.Lock()
	defer n.goalRecordMu.Unlock()
	if n.Store == nil || st.Session == "" {
		return
	}
	var g *Goal
	recognized := false
	stamp := str(d["timestamp"])
	switch str(d["type"]) {
	case "active_goal":
		recognized = true
		v := obj(d["value"])
		if str(v["condition"]) != "" {
			g = &Goal{Session: st.Session, Agent: "claude", Objective: Redact(str(v["condition"])), Status: "active", Created: int64Number(v["set_at"]) / 1000, Iterations: int64Number(v["iterations"]), Reason: Redact(str(v["last_reason"]))}
		}
	case "attachment":
		v := obj(d["attachment"])
		if v["type"] != "goal_status" {
			return
		}
		recognized = true
		if v["sentinel"] == true && v["met"] == true {
			break
		}
		status := "active"
		if v["met"] == true {
			status = "complete"
		}
		if v["failed"] == true {
			status = "failed"
		}
		if str(v["condition"]) != "" {
			g = &Goal{Session: st.Session, Agent: "claude", Objective: Redact(str(v["condition"])), Status: status, Tokens: int64Number(v["tokens"]), Seconds: int64Number(v["durationMs"]) / 1000, Iterations: int64Number(v["iterations"]), Reason: Redact(str(v["reason"]))}
		}
	}
	if !recognized {
		return
	}
	if stamp == "" {
		stamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if t, e := time.Parse(time.RFC3339Nano, stamp); e == nil {
		stamp = t.UTC().Format(time.RFC3339Nano)
		if g != nil {
			g.Updated = t.Unix()
		}
	}
	var old storedClaudeGoal
	if n.Store.Get("goals", st.Session, &old) == nil {
		a, ea := time.Parse(time.RFC3339Nano, stamp)
		b, eb := time.Parse(time.RFC3339Nano, old.Stamp)
		if ea == nil && eb == nil && a.Before(b) {
			return
		}
		if g != nil && old.Goal != nil && g.Objective == old.Goal.Objective {
			if g.Created == 0 {
				g.Created = old.Goal.Created
			}
			if g.Tokens == 0 {
				g.Tokens = old.Goal.Tokens
			}
		}
	}
	_ = n.Store.Put("goals", st.Session, storedClaudeGoal{Goal: g, Stamp: stamp})
}
func int64Number(v any) int64 { f, _ := v.(float64); return int64(f) }

// Incremental native goal-only index. Scan historical logs once without
// decoding or materializing chat contents; retain only goal records encrypted.
// Do not treat task lists, subagents, or text mentioning /goal as native goals.
type claudeGoalFile struct {
	size     int64
	offset   int64
	modified time.Time
}

func (m *Manager) scanClaudeGoals(sessions []store.Session) {
	if m.ClaudeGoalRoot == "" || m.Normalizer == nil {
		return
	}
	wanted := map[string]bool{}
	for _, s := range sessions {
		if s.Agent == "claude" && !s.CatalogHidden {
			wanted[s.ID] = true
		}
	}
	paths, e := filepath.Glob(filepath.Join(m.ClaudeGoalRoot, "*", "*.jsonl"))
	if e != nil {
		return
	}
	if m.goalFiles == nil {
		m.goalFiles = map[string]claudeGoalFile{}
	}
	for _, path := range paths {
		id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		if !wanted[id] {
			continue
		}
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() {
			continue
		}
		cached := m.goalFiles[path]
		if cached.size == info.Size() && cached.modified.Equal(info.ModTime()) {
			continue
		}
		file, e := os.Open(path)
		if e != nil {
			continue
		}
		offset := cached.offset
		if offset > info.Size() {
			offset = 0
		}
		if _, e = file.Seek(offset, io.SeekStart); e != nil {
			file.Close()
			continue
		}
		reader := bufio.NewReaderSize(io.LimitReader(file, info.Size()-offset), 64<<10)
		line := []byte{}
		oversize := false
		partial := int64(0)
		for {
			fragment, e := reader.ReadSlice('\n')
			partial += int64(len(fragment))
			if !oversize {
				if len(line)+len(fragment) > 16<<20 {
					line = nil
					oversize = true
				} else {
					line = append(line, fragment...)
				}
			}
			if e == bufio.ErrBufferFull {
				continue
			}
			if e == io.EOF {
				break
			}
			if e != nil {
				break
			}
			offset += partial
			partial = 0
			if !oversize && (bytes.Contains(line, []byte(`"goal_status"`)) || bytes.Contains(line, []byte(`"active_goal"`))) {
				var d map[string]any
				if json.Unmarshal(line, &d) == nil {
					m.Normalizer.captureClaudeGoal(d, &State{Session: id})
				}
			}
			line = line[:0]
			oversize = false
		}
		file.Close()
		m.goalFiles[path] = claudeGoalFile{size: info.Size(), offset: offset, modified: info.ModTime()}
	}
}
