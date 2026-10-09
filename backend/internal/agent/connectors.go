package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"agentdeck/internal/presentation"
	"agentdeck/internal/store"
)

var AgentNames = map[string]string{"codex": "Codex", "claude": "Claude Code", "opencode": "OpenCode", "qoder": "Qoder", "trae": "TRAE", "workbuddy": "WorkBuddy"}
var AgentOrder = []string{"codex", "claude", "opencode", "qoder", "trae", "workbuddy"}

func KnownAgent(id string) bool { _, ok := AgentNames[id]; return ok }

// Capabilities describe actual adapter operations, independently of visibility.
// Credentials and provider URLs are never included in the phone's catalog.
type AgentInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Configured bool   `json:"configured"`
	Create     bool   `json:"create"`
	Send       bool   `json:"send"`
	Stop       bool   `json:"stop"`
	Approve    bool   `json:"approve"`
	Detail     string `json:"detail"`
}
type Connector interface {
	Info() AgentInfo
	Create(context.Context, string, string) (store.Session, error)
	Send(context.Context, store.Session, string, string) error
	Interrupt(context.Context, store.Session) error
	Approve(context.Context, store.Session, string, string, string) error
	Run(context.Context)
	Close()
}
type HTTPAgentConfig struct {
	URL         string `json:"url"`
	Username    string `json:"username,omitempty"`
	PasswordEnv string `json:"password_env,omitempty"`
}
type ACPConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}
type WorkBuddyConfig struct {
	API             string `json:"api,omitempty"`
	ClientID        string `json:"client_id"`
	ClientSecretEnv string `json:"client_secret_env"`
	RedirectURI     string `json:"redirect_uri"`
}
type ConnectionsConfig struct {
	OpenCode  HTTPAgentConfig `json:"opencode"`
	Qoder     ACPConfig       `json:"qoder"`
	TRAE      ACPConfig       `json:"trae"`
	WorkBuddy WorkBuddyConfig `json:"workbuddy"`
}

func LoadConnections(path string) (ConnectionsConfig, error) {
	c := ConnectionsConfig{Qoder: ACPConfig{Command: "qoder", Args: []string{"--acp"}}, TRAE: ACPConfig{Command: "traecli", Args: []string{"acp", "serve"}}}
	if path == "" {
		return c, nil
	}
	st, e := os.Lstat(path)
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() > 65536 {
		return c, errors.New("agents-config must be an owner-only regular file of at most 64 KiB")
	}
	f, e := os.Open(path)
	if e != nil {
		return c, e
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(st, opened) {
		return c, errors.New("agents-config must be an owner-only regular file")
	}
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return c, errors.New("invalid agents-config JSON")
	}
	if d.Decode(new(any)) != io.EOF {
		return c, errors.New("agents-config contains trailing data")
	}
	if c.OpenCode.URL != "" {
		if e = validateAgentURL(c.OpenCode.URL); e != nil {
			return c, e
		}
	}
	if c.WorkBuddy.API != "" {
		if e = validateAgentURL(c.WorkBuddy.API); e != nil {
			return c, e
		}
	}
	return c, nil
}
func validateAgentURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid agent endpoint")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback()) {
		return nil
	}
	return errors.New("agent endpoint requires HTTPS or loopback HTTP")
}
func (m *Manager) Configure(c ConnectionsConfig) {
	m.Connectors = map[string]Connector{}
	m.Connectors["opencode"] = newOpenCode(m, c.OpenCode)
	m.Connectors["qoder"] = newACP(m, "qoder", c.Qoder)
	m.Connectors["trae"] = newACP(m, "trae", c.TRAE)
	m.Connectors["workbuddy"] = newWorkBuddy(m, c.WorkBuddy)
}
func (m *Manager) Catalog() []AgentInfo {
	out := []AgentInfo{}
	for _, id := range AgentOrder {
		if c := m.Connectors[id]; c != nil {
			out = append(out, c.Info())
			continue
		}
		ok := false
		detail := "尚未配置接入"
		if id == "codex" || id == "claude" {
			_, e := exec.LookPath(id)
			ok = e == nil
			detail = "使用工作站 CLI 登录状态"
			if !ok {
				detail = "请在工作站安装并登录 CLI"
			}
		}
		out = append(out, AgentInfo{ID: id, Name: AgentNames[id], Configured: ok, Create: id == "codex" || id == "claude", Send: true, Stop: true, Approve: true, Detail: detail})
	}
	return out
}
func (m *Manager) RunConnectors(ctx context.Context) {
	for _, c := range m.Connectors {
		go c.Run(ctx)
	}
}
func (m *Manager) externalSession(id string) (Connector, store.Session, error) {
	var s store.Session
	if e := m.Store.Get("sessions", id, &s); e != nil {
		return nil, s, errors.New("会话不存在")
	}
	c := m.Connectors[s.Agent]
	if c == nil {
		return nil, s, errors.New("Agent 未配置")
	}
	if s.Agent != "workbuddy" {
		if _, e := m.allowed(s.Project); e != nil {
			return nil, s, e
		}
	}
	return c, s, nil
}
func (m *Manager) emitExternal(s store.Session, ev store.Event) {
	ev.Session = s.ID
	ev.Agent = s.Agent
	ev.Project = s.Project
	ev.Source = s.Source
	if ev.ID == "" {
		ev.ID = "external/" + UUID()
	}
	if ev.Kind == "message" && ev.Mode != "delta" {
		ev.Text = presentation.Clean(ev.Text)
	}
	ev.Text = Redact(ev.Text)
	if len(ev.Text) > 128<<10 {
		ev.Text = ev.Text[:128<<10] + "\n[输出过长，已截断]"
	}
	_, _, _ = m.Store.Append(ev)
}
func sessionID(agent, native string) string { return agent + "/" + native }
func nativeID(s store.Session) string       { return strings.TrimPrefix(s.ID, s.Agent+"/") }
func newExternalSession(id, agent, path, source string) store.Session {
	return store.Session{ID: sessionID(agent, id), Agent: agent, Project: path, Source: source, Managed: true, Title: "新会话", Status: "idle", Updated: time.Now().UTC().Format(time.RFC3339Nano)}
}
