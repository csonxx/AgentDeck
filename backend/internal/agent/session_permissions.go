package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"agentdeck/internal/claudedesktop"
	acp "github.com/coder/acp-go-sdk"
)

type PermissionChoice struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
}
type SessionPermissions struct {
	Mode     string             `json:"mode"`
	Label    string             `json:"label"`
	Detail   string             `json:"detail"`
	Source   string             `json:"source"`
	Version  string             `json:"version"`
	Editable bool               `json:"editable"`
	Choices  []PermissionChoice `json:"choices"`
}

func permissionChoices(agent string) []PermissionChoice {
	switch agent {
	case "codex":
		return []PermissionChoice{
			{"read-only", "Read only", "on-request · readOnly · user"},
			{"on-request", "Default permissions", "on-request · workspaceWrite · user"},
			{"auto_review", "Auto review", "on-request · workspaceWrite · auto_review"},
			{"full-access", "Full access", "never · dangerFullAccess · user"},
		}
	case "claude":
		return []PermissionChoice{
			{"default", "Default", "default · 逐项请求授权"}, {"acceptEdits", "Accept edits", "acceptEdits · 自动接受文件编辑"},
			{"auto", "Auto", "auto · 原生自动权限审查"}, {"bypassPermissions", "Bypass permissions", "bypassPermissions · 跳过工具权限确认"},
			{"plan", "Plan", "plan · 规划模式"}, {"dontAsk", "Don't ask", "dontAsk · 自动拒绝需要确认的工具"},
		}
	case "opencode":
		return []PermissionChoice{{"ask", "Ask", "所有工具询问"}, {"allow", "Allow", "允许所有工具"}, {"deny", "Deny", "拒绝所有工具"}}
	}
	return nil
}
func validPermissionMode(agent, mode string) bool {
	for _, c := range permissionChoices(agent) {
		if c.ID == mode {
			return true
		}
	}
	return false
}
func sandboxConfigName(s string) string {
	switch s {
	case "readOnly":
		return "read-only"
	case "workspaceWrite":
		return "workspace-write"
	case "dangerFullAccess":
		return "danger-full-access"
	}
	return ""
}
func codexPermissionParams(mode, project string) map[string]any {
	policy, reviewer := "on-request", "user"
	sandbox := map[string]any{"type": "workspaceWrite", "writableRoots": []string{project}, "networkAccess": false, "excludeSlashTmp": false, "excludeTmpdirEnvVar": false}
	switch mode {
	case "read-only":
		sandbox = map[string]any{"type": "readOnly", "networkAccess": false}
	case "auto_review":
		reviewer = "auto_review"
	case "full-access":
		policy = "never"
		sandbox = map[string]any{"type": "dangerFullAccess"}
	}
	return map[string]any{"approvalPolicy": policy, "approvalsReviewer": reviewer, "sandboxPolicy": sandbox}
}
func codexPermissionState(v map[string]any) map[string]any {
	s := v["sandboxPolicy"]
	if s == nil {
		s = v["sandbox"]
	}
	return map[string]any{"approvalPolicy": v["approvalPolicy"], "approvalsReviewer": v["approvalsReviewer"], "sandboxPolicy": s, "permissions": v["permissions"]}
}
func permissionView(agent string, state map[string]any, source string) SessionPermissions {
	out := SessionPermissions{Source: source, Choices: permissionChoices(agent)}
	switch agent {
	case "codex":
		p, r, s := state["approvalPolicy"], str(state["approvalsReviewer"]), str(obj(state["sandboxPolicy"])["type"])
		if p != nil && s != "" {
			out.Mode = "custom"
			out.Label = "Custom"
			if s == "readOnly" && p == "on-request" {
				out.Mode = "read-only"
				out.Label = "Read only"
			}
			if s == "workspaceWrite" && p == "on-request" {
				out.Mode = "on-request"
				out.Label = "Default permissions"
				if r == "auto_review" || r == "guardian_subagent" {
					out.Mode = "auto_review"
					out.Label = "Auto review"
				}
			}
			if s == "dangerFullAccess" && p == "never" {
				out.Mode = "full-access"
				out.Label = "Full access"
			}
			policy := str(p)
			if policy == "" {
				b, _ := json.Marshal(p)
				policy = string(b)
			}
			out.Detail = policy + " · " + s
			if r != "" {
				out.Detail += " · " + r
			}
		}
	case "claude":
		out.Mode = str(state["mode"])
		if out.Mode == "manual" {
			out.Mode = "default"
		}
		out.Label = out.Mode
		out.Detail = out.Mode
	case "opencode":
		rules := arr(state["permission"])
		out.Mode = "custom"
		out.Label = "Custom rules"
		out.Detail = "原生工具权限规则"
		// OpenCode appends session rules. Its evaluator uses the last match,
		// so a final universal rule overrides all earlier session entries.
		if len(rules) > 0 {
			data, _ := json.MarshalIndent(rules, "", "  ")
			out.Detail = string(data)
			r := obj(rules[len(rules)-1])
			if r["permission"] == "*" && r["pattern"] == "*" {
				out.Mode = str(r["action"])
			}
		}
		if len(rules) == 0 {
			out.Mode = "agent-default"
			out.Label = "Agent / project rules"
			out.Detail = "按 OpenCode 的 Agent / 项目权限规则执行"
		}
	}
	for _, choice := range out.Choices {
		if out.Mode == choice.ID {
			out.Label = choice.Label
		}
	}
	out.Editable = source == "live" && len(out.Choices) > 0
	if out.Mode == "" {
		out.Label = "权限未提供"
	}
	data, _ := json.Marshal(state)
	out.Version = fmt.Sprintf("%x", sha256.Sum256(data))
	return out
}
func (m *Manager) SessionPermissions(id string) (SessionPermissions, error) {
	ss, err := m.sessionForInput(id)
	if err != nil {
		return SessionPermissions{}, err
	}
	ctx, cancel := context.WithTimeout(nonNil(m.Context), 6*time.Second)
	defer cancel()
	if ss.Agent == "codex" && (!ss.Managed || ss.Source == "desktop") {
		if b, ok := m.Desktop.(interface {
			Permissions(context.Context, string, string) (SessionPermissions, error)
		}); ok {
			return b.Permissions(ctx, id, ss.Project)
		}
	}
	if ss.Agent == "claude" {
		target, handled, e := m.claudeDesktopTarget(ss)
		if e != nil {
			return SessionPermissions{}, e
		}
		if handled {
			b, e := m.permissionClaudeBridge(ctx)
			if e != nil {
				return SessionPermissions{}, e
			}
			defer b.Close()
			native, ok := b.(interface {
				PermissionMode(context.Context, claudedesktop.Target) (string, error)
			})
			if !ok {
				return SessionPermissions{}, errors.New("Claude Desktop 版本不支持权限读取")
			}
			mode, e := native.PermissionMode(ctx, target)
			return permissionView("claude", map[string]any{"mode": mode}, "live"), e
		}
	}
	if ss.Agent == "codex" || ss.Agent == "claude" {
		m.mu.Lock()
		r := m.runs[id]
		m.mu.Unlock()
		if r != nil {
			select {
			case <-r.done:
			default:
				r.mu.Lock()
				v := permissionView(ss.Agent, r.permissions, "live")
				r.mu.Unlock()
				return v, nil
			}
		}
		if ss.Agent == "codex" {
			// Permission polling is a read. Resuming here acquires the thread in
			// a second app-server and blocks the already open Desktop conversation.
			if b, ok := m.Desktop.(interface {
				Permissions(context.Context, string, string) (SessionPermissions, error)
			}); ok {
				if current, e := b.Permissions(ctx, id, ss.Project); e == nil {
					return current, nil
				}
			}
			var cached map[string]any
			if m.Store.Get("agentconfig", "codex-last-permissions/"+id, &cached) == nil {
				v := permissionView("codex", cached, "last_turn")
				v.Editable = v.Mode != "" && len(v.Choices) > 0
				return v, nil
			}
			return SessionPermissions{Label: "权限暂不可读", Detail: "原会话当前未连接，读取权限不会启动或接管会话", Source: "unknown"}, nil
		}
		r, e := m.get(id)
		if e != nil {
			return SessionPermissions{}, e
		}
		defer r.releaseUse()
		r.mu.Lock()
		out := permissionView(ss.Agent, r.permissions, "live")
		r.mu.Unlock()
		return out, nil
	}
	connector, _, err := m.externalSession(id)
	if err != nil {
		return SessionPermissions{}, err
	}
	if o, ok := connector.(*openCode); ok {
		var v map[string]any
		if e := o.request(ctx, "GET", "/session/"+url.PathEscape(nativeID(ss)), ss.Project, nil, &v); e != nil {
			return SessionPermissions{}, e
		}
		return permissionView("opencode", map[string]any{"permission": v["permission"]}, "live"), nil
	}
	if c, ok := connector.(*acpConnector); ok {
		r, e := c.get(ctx, ss)
		if e != nil {
			return SessionPermissions{}, e
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.permissionView(), nil
	}
	return SessionPermissions{Label: "由 Agent 管理", Detail: "当前原生接口不提供权限修改", Source: "unsupported"}, nil
}
func (m *Manager) permissionClaudeBridge(ctx context.Context) (claudeDesktopClient, error) {
	if m.openClaudeDesktop != nil {
		return m.openClaudeDesktop(ctx)
	}
	return claudedesktop.Open(ctx)
}
func (m *Manager) ChangePermissions(id, version, mode string) (SessionPermissions, error) {
	lock := m.deliveryLock("permission/" + id)
	lock.Lock()
	defer lock.Unlock()
	ss, e := m.sessionForInput(id)
	if e != nil {
		return SessionPermissions{}, e
	}
	before, e := m.SessionPermissions(id)
	if e != nil {
		return before, e
	}
	if version == "" || version != before.Version {
		return before, errors.New("权限已变更，请刷新后重试")
	}
	if !before.Editable {
		return before, errors.New("当前原生接口不支持修改权限")
	}
	valid := false
	for _, c := range before.Choices {
		if c.ID == mode {
			valid = true
		}
	}
	if !valid {
		return before, errors.New("不支持此权限模式")
	}
	if before.Mode == mode {
		return before, nil
	}
	ctx, cancel := context.WithTimeout(nonNil(m.Context), 10*time.Second)
	defer cancel()
	localRun := m.liveRuntime(id)
	desktopOwner := false
	if ss.Agent == "codex" && localRun == nil && m.Desktop != nil {
		owner, err := m.Desktop.Read(ctx, id)
		desktopOwner = err == nil && owner.ID == id && owner.Host == "local" && owner.Kind == "codex" && (owner.Project == "" || owner.Project == ss.Project)
	}
	if ss.Agent == "codex" && (!ss.Managed || ss.Source == "desktop" || desktopOwner) {
		if b, ok := m.Desktop.(interface {
			SetPermissions(context.Context, string, string, string, string) (SessionPermissions, error)
		}); ok {
			return b.SetPermissions(ctx, id, ss.Project, version, mode)
		}
		return before, errors.New("电脑 Codex 版本不支持权限修改")
	}
	if ss.Agent == "claude" {
		target, handled, err := m.claudeDesktopTarget(ss)
		if err != nil {
			return before, err
		}
		if handled {
			b, err := m.permissionClaudeBridge(ctx)
			if err != nil {
				return before, err
			}
			defer b.Close()
			native, ok := b.(interface {
				SetPermissionMode(context.Context, claudedesktop.Target, string) (string, error)
			})
			if !ok {
				return before, errors.New("Claude Desktop 版本不支持权限修改")
			}
			actual, err := native.SetPermissionMode(ctx, target, mode)
			if err != nil {
				return before, err
			}
			out := permissionView("claude", map[string]any{"mode": actual}, "live")
			if out.Mode != mode {
				return out, errors.New("Claude 未应用此权限模式，请检查电脑端策略")
			}
			return out, nil
		}
	}
	if ss.Agent == "codex" || ss.Agent == "claude" {
		r, err := m.get(id)
		if err != nil {
			return before, err
		}
		defer r.releaseUse()
		if ss.Agent == "codex" {
			r.mu.Lock()
			fresh := permissionView("codex", r.permissions, "live")
			r.mu.Unlock()
			if fresh.Version != version {
				return fresh, errors.New("权限已变更，请刷新后重试")
			}
			params := codexPermissionParams(mode, ss.Project)
			params["threadId"] = id
			if _, e = r.call("thread/settings/update", params); e != nil {
				return before, e
			}
			// This RPC emits the authoritative thread/settings/updated event. Do not
			// report a requested value as applied when that event was not received.
			deadline := time.Now().Add(2 * time.Second)
			for {
				r.mu.Lock()
				out := permissionView("codex", r.permissions, "live")
				r.mu.Unlock()
				if out.Mode == mode {
					return out, m.Store.Put("agentconfig", "permission-choice/"+id, mode)
				}
				if time.Now().After(deadline) {
					return out, errors.New("Codex 尚未确认权限更新，请刷新核对")
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
		if _, e = r.call("set_permission_mode", map[string]any{"subtype": "set_permission_mode", "mode": mode}); e != nil {
			return before, e
		}
		// Claude's native control success acknowledges the effective mode change.
		r.mu.Lock()
		r.permissions = map[string]any{"mode": mode}
		r.mu.Unlock()
		if e = m.Store.Put("agentconfig", "permission-choice/"+id, mode); e != nil {
			return before, e
		}
		return m.SessionPermissions(id)
	}
	connector, _, e := m.externalSession(id)
	if e != nil {
		return before, e
	}
	if o, ok := connector.(*openCode); ok {
		rules := []any{map[string]any{"permission": "*", "pattern": "*", "action": mode}}
		var v map[string]any
		if e = o.request(ctx, "PATCH", "/session/"+url.PathEscape(nativeID(ss)), ss.Project, map[string]any{"permission": rules}, &v); e != nil {
			return before, e
		}
		out, e := m.SessionPermissions(id)
		if e == nil && out.Mode != mode {
			e = errors.New("OpenCode 尚未确认权限更新，请刷新核对")
		}
		return out, e
	}
	if c, ok := connector.(*acpConnector); ok {
		r, e := c.get(ctx, ss)
		if e != nil {
			return before, e
		}
		if _, e = r.conn.SetSessionMode(ctx, acp.SetSessionModeRequest{SessionId: acp.SessionId(nativeID(ss)), ModeId: acp.SessionModeId(mode)}); e != nil {
			return before, e
		}
		r.mu.Lock()
		if r.modes != nil {
			r.modes.CurrentModeId = acp.SessionModeId(mode)
		}
		out := r.permissionView()
		r.mu.Unlock()
		return out, nil
	}
	return before, errors.New("当前原生接口不支持修改权限")
}
func (r *acpRuntime) permissionView() SessionPermissions {
	out := SessionPermissions{Label: "权限未提供", Detail: "Agent 未提供原生会话模式", Source: "unsupported"}
	if r.modes == nil {
		return out
	}
	out.Source = "live"
	out.Mode = string(r.modes.CurrentModeId)
	out.Label = out.Mode
	out.Detail = "ACP 原生会话模式（由 Agent 定义）"
	for _, m := range r.modes.AvailableModes {
		detail := ""
		if m.Description != nil {
			detail = *m.Description
		}
		out.Choices = append(out.Choices, PermissionChoice{string(m.Id), m.Name, detail})
		if m.Id == r.modes.CurrentModeId {
			out.Label = m.Name
		}
	}
	out.Editable = len(out.Choices) > 0
	b, _ := json.Marshal(r.modes)
	out.Version = fmt.Sprintf("%x", sha256.Sum256(b))
	return out
}

func desktopPermissionState(state map[string]any) map[string]any {
	p := codexPermissionState(obj(state["currentPermissions"]))
	for k, v := range codexPermissionState(obj(state["latestThreadSettings"])) {
		if v != nil {
			p[k] = v
		}
	}
	return p
}
func (b *DesktopBridge) Permissions(ctx context.Context, id, project string) (SessionPermissions, error) {
	c, e := b.connect(ctx, id)
	if e != nil {
		return SessionPermissions{}, e
	}
	defer c.conn.Close()
	stop := context.AfterFunc(ctx, func() { c.conn.Close() })
	defer stop()
	s, e := c.snapshot(id, project)
	if e != nil {
		return SessionPermissions{}, e
	}
	return permissionView("codex", desktopPermissionState(s), "live"), nil
}
func (b *DesktopBridge) SetPermissions(ctx context.Context, id, project, version, mode string) (SessionPermissions, error) {
	c, e := b.connectForInput(ctx, id)
	if e != nil {
		return SessionPermissions{}, e
	}
	defer c.conn.Close()
	stop := context.AfterFunc(ctx, func() { c.conn.Close() })
	defer stop()
	s, e := c.snapshot(id, project)
	if e != nil {
		return SessionPermissions{}, e
	}
	before := permissionView("codex", desktopPermissionState(s), "live")
	if before.Version != version {
		return before, errors.New("权限已变更，请刷新后重试")
	}
	settings := codexPermissionParams(mode, project)
	result, e := c.request("thread-follower-update-thread-settings", 2, map[string]any{"conversationId": id, "threadSettings": settings}, c.owner)
	if e != nil {
		return before, e
	}
	if obj(result["result"])["applied"] != true {
		return before, errors.New("电脑 Codex 未应用权限修改")
	}
	s, e = c.snapshot(id, project)
	if e != nil {
		return before, e
	}
	after := permissionView("codex", desktopPermissionState(s), "live")
	if after.Mode != mode {
		return after, errors.New("电脑 Codex 尚未确认权限更新，请刷新核对")
	}
	return after, nil
}

// Replay only a previous explicit choice for a managed session. Default-created
// sessions continue to inherit native project/global settings without overrides.
func (r *runtime) applyPermissionMode(mode string) error {
	if r.session.Agent == "codex" {
		p := codexPermissionParams(mode, r.session.Project)
		p["threadId"] = r.session.ID
		_, e := r.call("thread/settings/update", p)
		return e
	}
	if r.session.Agent == "claude" {
		_, e := r.call("set_permission_mode", map[string]any{"subtype": "set_permission_mode", "mode": mode})
		if e == nil {
			r.mu.Lock()
			r.permissions = map[string]any{"mode": mode}
			r.mu.Unlock()
		}
		return e
	}
	return errors.New("unsupported permission mode")
}
