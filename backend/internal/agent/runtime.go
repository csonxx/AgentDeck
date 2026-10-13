package agent

import (
	"agentdeck/internal/secure"
	"agentdeck/internal/store"
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}
type Manager struct {
	// Legacy stdio adapter test seam; original Desktop input uses the separate
	// owner-enabled debugger path and must never fall back to a replacement CLI.
	ClaudeDesktopInput bool
	// Owner opt-in to the official Desktop's local main-process debugger.
	ClaudeDesktopDebugger bool
	ClaudeDesktopSessions string
	openClaudeDesktop     func(context.Context) (claudeDesktopClient, error)

	ClaudeOwner           func(string, int) error
	ClaudeProcesses       string
	ClaudeProcessSnapshot func() (map[int]string, error)
	claudeRelease         func(claudeProcessRecord) error // isolated process tests
	deliveryMu            sync.Mutex
	deliveryLocks         map[string]*sync.Mutex
	deliveryCancel        context.CancelFunc
	deliveryWG            sync.WaitGroup

	desktopMu          sync.Mutex
	desktopLeases      map[string]*desktopLease
	desktopClosed      bool
	desktopContext     context.Context
	desktopStop        context.CancelFunc
	desktopWG          sync.WaitGroup
	Store              *store.Store
	Normalizer         *Normalizer
	ReadSettings       func(string) (SessionSettings, error)
	LoadHistory        func(string) error
	Roots              []string
	Context            context.Context
	Desktop            DesktopController
	Connectors         map[string]Connector
	VoiceCommand       []string
	Binary, ControlDir string
	voiceMu            sync.Mutex
	voiceWorker        *voiceWorker
	catalogMu          sync.Mutex
	catalogs           map[string]modelEntry
	ClaudeGoalRoot     string
	CodexGoalPath      string
	goalFiles          map[string]claudeGoalFile
	goalMu             sync.Mutex
	goalCache          []Goal
	goalChecked        time.Time
	usageMu            sync.Mutex
	usageCache         map[string]AccountUsage
	ClaudeUsageFile    string
	mu                 sync.Mutex
	runs               map[string]*runtime
}
type approval struct {
	ID     any
	Method string
	Params map[string]any
}
type runtime struct {
	manager      *Manager
	session      store.Session
	cmd          *exec.Cmd
	input        io.WriteCloser
	writeMu      sync.Mutex
	mu           sync.Mutex
	pending      map[string]chan map[string]any
	approvals    map[string]approval
	done         chan struct{}
	turn         string
	busy         bool
	claudeInputs int
	state        State
	options      TurnOptions
	settings     SessionSettings
}

func UUID() string {
	b := secure.Random(16)
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
func (m *Manager) allowed(path string) (string, error) {
	if path == "" {
		return "", errors.New("缺少项目目录")
	}
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", errors.New("项目目录不存在")
	}
	p, err = filepath.Abs(p)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(p)
	if err != nil || !info.IsDir() {
		return "", errors.New("项目目录不可用")
	}
	for _, root := range m.Roots {
		r, e := filepath.EvalSymlinks(root)
		if e != nil {
			continue
		}
		r, _ = filepath.Abs(r)
		rel, e := filepath.Rel(r, p)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return p, nil
		}
	}
	return "", errors.New("项目不在工作站授权目录内")
}
func (m *Manager) Projects() []Project {
	paths := map[string]bool{}
	for _, root := range m.Roots {
		if p, e := m.allowed(root); e == nil {
			paths[p] = true
			entries, _ := os.ReadDir(p)
			for _, d := range entries {
				if d.IsDir() && !strings.HasPrefix(d.Name(), ".") {
					if sub, e := m.allowed(filepath.Join(p, d.Name())); e == nil {
						paths[sub] = true
					}
				}
			}
		}
	}
	ss, _ := m.Store.Sessions()
	for _, s := range ss {
		if p, e := m.allowed(s.Project); e == nil {
			paths[p] = true
		}
	}
	out := []Project{}
	for p := range paths {
		out = append(out, Project{digest(p)[:24], filepath.Base(p), p})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
func (m *Manager) Create(agent, project, parent string) (store.Session, error) {
	if !KnownAgent(agent) {
		return store.Session{}, errors.New("未知 Agent")
	}
	path := ""
	for _, p := range m.Projects() {
		if p.ID == project {
			path = p.Path
			break
		}
	}
	if path == "" {
		return store.Session{}, errors.New("请选择已授权项目")
	}
	var old store.Session
	if parent != "" {
		if e := m.Store.Get("sessions", parent, &old); e != nil || old.Agent != agent || old.Project != path {
			return store.Session{}, errors.New("会话与项目不匹配")
		}
		if old.Source == "claude-desktop" && m.LoadHistory != nil {
			if err := m.LoadHistory(parent); err != nil {
				return store.Session{}, err
			}
		}
	}
	if agent != "codex" && agent != "claude" {
		connector := m.Connectors[agent]
		if connector == nil {
			return store.Session{}, errors.New("Agent 接入尚未配置")
		}
		return connector.Create(m.Context, path, parent)
	}
	// Desktop chats stay on their original ID and execute in the existing app.
	// Never silently fall back to forking after a bridge error.
	if parent != "" && agent == "codex" && m.Desktop != nil && (!old.Managed || old.Source == "desktop") {
		thread, err := readDesktopForInput(m.Desktop, m.Context, parent)
		if err != nil {
			return old, err
		}
		if thread.Kind != "codex" || thread.Host != "local" || thread.ID != parent || thread.Project != "" && thread.Project != path {
			return old, errors.New("电脑会话与授权项目不匹配")
		}
		old.Managed = true
		old.Source = "desktop"
		if err = m.Store.Put("sessions", old.ID, old); err != nil {
			return old, err
		}
		return old, nil
	}
	// Continuing Claude retains the native ID and its history. There is no
	// implicit fork, copied transcript or newly generated session ID.
	if parent != "" && agent == "claude" {
		if strings.HasPrefix(old.ID, "claude-desktop/") {
			return old, errors.New("此 Claude 会话没有可恢复的本机 ID")
		}
		_, _, err := m.claudeInbox(old)
		if err != nil {
			return old, err
		}
		// Selecting a chat never interrupts its current executor.
		old.Managed = true
		if err := m.Store.Put("sessions", old.ID, old); err != nil {
			return old, err
		}
		return old, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runs == nil {
		m.runs = map[string]*runtime{}
	}
	for id, run := range m.runs {
		select {
		case <-run.done:
			delete(m.runs, id)
		default:
		}
	}
	if len(m.runs) >= 16 {
		return store.Session{}, errors.New("已达到同时打开会话上限")
	}
	ss := store.Session{ID: UUID(), Agent: agent, Title: "新会话", Project: path, Source: "stream", Managed: true, Parent: parent, Status: "idle", Updated: time.Now().UTC().Format(time.RFC3339Nano)}
	r, e := m.launch(ss, false)
	if e != nil {
		return ss, e
	}
	if agent == "codex" {
		method := "thread/start"
		params := map[string]any{"cwd": path, "approvalPolicy": "on-request", "sandbox": "workspace-write"}
		if parent != "" {
			method = "thread/fork"
			params["threadId"] = parent
		}
		response, err := r.call(method, params)
		if err != nil {
			r.close()
			return ss, err
		}
		ss.ID = str(obj(response["thread"])["id"])
		if ss.ID == "" {
			r.close()
			return ss, errors.New("Codex 未返回会话标识")
		}
		r.mu.Lock()
		r.session = ss
		r.state.Session = ss.ID
		r.mu.Unlock()
	}
	if e = m.Store.Put("sessions", ss.ID, ss); e != nil {
		r.close()
		return ss, e
	}
	if parent != "" {
		cursor := uint64(0)
		for {
			events, err := m.Store.Events(parent, cursor, 100)
			if err != nil {
				r.close()
				return ss, err
			}
			if len(events) == 0 {
				break
			}
			for _, ev := range events {
				cursor = ev.Seq
				ev.ID = "fork/" + ss.ID + "/" + ev.ID
				ev.Session = ss.ID
				ev.Source = "stream"
				_, _, _ = m.Store.Append(ev)
			}
		}
		_ = m.Store.Put("generation", ss.ID, m.Options(parent))
		ss.Title = old.Title
		ss.Status = "idle"
		ss.Updated = time.Now().UTC().Format(time.RFC3339Nano)
		_ = m.Store.Put("sessions", ss.ID, ss)
	}
	m.runs[ss.ID] = r
	return ss, nil
}
func (m *Manager) get(id string) (*runtime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.runs[id]; r != nil {
		select {
		case <-r.done:
			delete(m.runs, id)
		default:
			return r, nil
		}
	}
	var ss store.Session
	if e := m.Store.Get("sessions", id, &ss); e != nil || !ss.Managed {
		return nil, errors.New("请先选择继续此会话")
	}
	if _, e := m.allowed(ss.Project); e != nil {
		return nil, e
	}
	// A newly created Claude chat has not written native history yet. Restart
	// it with the same session-id; only a chat with a started turn can resume.
	resume := !(ss.Agent == "claude" && ss.Status == "idle" && ss.Source == "stream" && ss.Parent == "")
	if ss.Agent == "claude" && resume {
		if e := m.acquireClaude(ss.ID); e != nil {
			return nil, e
		}
	}
	r, e := m.launch(ss, resume)
	if e != nil {
		return nil, e
	}
	if ss.Agent == "codex" {
		if _, e = r.call("thread/resume", map[string]any{"threadId": id, "cwd": ss.Project, "approvalPolicy": "on-request", "sandbox": "workspace-write"}); e != nil {
			r.close()
			return nil, e
		}
	}
	if m.runs == nil {
		m.runs = map[string]*runtime{}
	}
	m.runs[id] = r
	return r, nil
}
func (m *Manager) Send(id, text, request string) error {
	return m.SendInput(id, text, request, TurnOptions{}, nil)
}
func (m *Manager) SendInput(id, text, request string, o TurnOptions, files []InputAttachment) error {
	if len(text) > 64<<10 || strings.TrimSpace(text) == "" && len(files) == 0 {
		return errors.New("消息为空或超过 64 KiB")
	}
	var ss store.Session
	var err error
	ss, err = m.sessionForInput(id)
	if err != nil {
		return err
	}
	if ss.Agent == "claude" {
		if !m.ClaudeDesktopInput {
			if handled, err := m.sendClaudeRemote(ss, text, request, o, files); handled {
				return err
			}
		}
		owner, native, err := m.claudeInbox(ss)
		if err != nil {
			return err
		}
		if native {
			return m.sendClaudeInbox(ss, owner, text, request, o, files)
		}
	}
	o, err = m.setOptions(ss, o)
	if err != nil {
		return err
	}
	if o.Model != "" && len(files) > 0 {
		for _, file := range files {
			if strings.HasPrefix(file.MIME, "image/") {
				choices, e := m.Models(ss.Agent, ss.ID)
				if e != nil {
					return e
				}
				for _, c := range choices {
					if c.ID == o.Model && !c.Images {
						return errors.New("所选模型不支持图片输入")
					}
				}
				break
			}
		}
	}
	if m.Store.Get("sessions", id, &ss) == nil && ss.Agent != "codex" && ss.Agent != "claude" {
		c, ss, e := m.externalSession(id)
		if e != nil {
			return e
		}
		if sender, ok := c.(interface {
			SendInput(context.Context, store.Session, string, string, TurnOptions, []InputAttachment) error
		}); ok {
			return sender.SendInput(m.Context, ss, text, request, o, files)
		}
		for _, file := range files {
			if strings.HasPrefix(file.MIME, "image/") {
				return errors.New("当前 Agent 接口未提供图片输入")
			}
		}
		if len(files) > 0 {
			_, display, e := m.prepareInput(id, text, request, files)
			if e != nil {
				return e
			}
			text = display
		}
		return c.Send(m.Context, ss, text, request)
	}
	if m.Store.Get("sessions", id, &ss) == nil && ss.Source == "desktop" {
		if m.Desktop == nil {
			return errors.New("电脑 Codex 桥接未配置")
		}
		if _, err := m.allowed(ss.Project); err != nil {
			return err
		}
		thread, err := readDesktopForInput(m.Desktop, m.Context, id)
		if err != nil {
			return err
		}
		if thread.Kind != "codex" || thread.Host != "local" || thread.ID != id || thread.Project != "" && thread.Project != ss.Project {
			return errors.New("电脑会话与授权项目不匹配")
		}
		inputs, display, e := m.prepareInput(id, text, request, files)
		if e != nil {
			return e
		}
		if sender, ok := m.Desktop.(interface {
			SendInput(context.Context, string, string, string, string, TurnOptions, []any) error
		}); ok {
			err = sender.SendInput(m.Context, id, text, request, ss.Project, o, inputs)
		} else if o.Model != "" || len(files) > 0 {
			err = errors.New("电脑桥接不支持模型或附件输入")
		} else {
			err = m.Desktop.Send(m.Context, id, text, request, ss.Project)
		}
		if err != nil {
			return err
		}
		_, _, err = m.Store.MarkDesktopAccepted(store.Event{ID: "desktop-accepted/" + request, Session: id, Agent: ss.Agent, Project: ss.Project, Kind: "status", Status: "pending", Source: "desktop-live"}, ss.Updated)
		if err != nil {
			return err
		}
		_, _, err = m.Store.AppendDesktop(store.Event{ID: "desktop-input/" + request, Session: id, Agent: ss.Agent, Project: ss.Project, Kind: "message", Role: "user", Item: request + ":0", Text: Redact(display), Mode: "snapshot", Source: "desktop-live"}, false)
		if err == nil {
			m.keepDesktopSend(id)
		}
		return err
	}
	r, e := m.get(id)
	if e != nil {
		return e
	}
	if ss.Agent == "claude" {
		if e := m.checkClaudeOwner(id, r.cmd.Process.Pid); e != nil {
			return e
		}
	}
	inputs, display, e := m.prepareInput(id, text, request, files)
	if e != nil {
		return &InputNotSentError{Err: e}
	}
	r.mu.Lock()
	wasBusy := r.busy
	if wasBusy && ss.Agent != "claude" {
		r.mu.Unlock()
		return &InputNotSentError{Err: errors.New("正在执行，请等待或停止当前任务")}
	}
	if ss.Agent == "claude" {
		r.claudeInputs++
	}
	r.busy = true
	r.mu.Unlock()
	r.emit(store.Event{Kind: "status", Status: "running"})
	if r.session.Agent == "codex" {
		var result map[string]any
		params := map[string]any{"threadId": id, "input": inputs}
		if o.Model != "" {
			params["model"] = o.Model
		}
		if o.Effort != "" {
			params["effort"] = o.Effort
		}
		applySpeed(params, o)
		result, e = r.call("turn/start", params)
		if e == nil {
			r.mu.Lock()
			r.turn = str(obj(result["turn"])["id"])
			r.mu.Unlock()
		}
	} else {
		r.emit(store.Event{ID: "input/" + id + "/" + request, Kind: "message", Role: "user", Text: Redact(display), Item: request + ":0", Mode: "snapshot"})
		e = r.write(map[string]any{"type": "user", "uuid": request, "session_id": id, "message": map[string]any{"role": "user", "content": claudeInput(inputs)}, "parent_tool_use_id": nil})
	}
	if e != nil {
		r.mu.Lock()
		if ss.Agent == "claude" {
			r.claudeInputs--
		}
		r.busy = wasBusy
		r.mu.Unlock()
		r.emit(store.Event{Kind: "notice", Text: e.Error()})
		r.emit(store.Event{Kind: "status", Status: "failed"})
	}
	return e
}
func (m *Manager) Interrupt(id string) error {
	var ss store.Session
	if m.Store.Get("sessions", id, &ss) == nil && ss.Agent == "claude" {
		if handled, err := m.controlClaudeDesktop(ss, "", "", ""); handled {
			return err
		}
		_, native, err := m.claudeInbox(ss)
		if err != nil {
			return err
		}
		if native {
			return errors.New("请在电脑 Claude 原会话中处理停止或工具审批")
		}
	}
	if m.Store.Get("sessions", id, &ss) == nil && ss.Agent != "codex" && ss.Agent != "claude" {
		c, ss, e := m.externalSession(id)
		if e != nil {
			return e
		}
		return c.Interrupt(m.Context, ss)
	}
	var desktopSession store.Session
	if m.Store.Get("sessions", id, &desktopSession) == nil && desktopSession.Source == "desktop" {
		return errors.New("请在电脑 Codex 中处理停止或工具审批")
	}
	r, e := m.get(id)
	if e != nil {
		return e
	}
	if r.session.Agent == "codex" {
		r.mu.Lock()
		turn := r.turn
		r.mu.Unlock()
		_, e = r.call("turn/interrupt", map[string]any{"threadId": id, "turnId": turn})
	} else {
		_, e = r.call("interrupt", map[string]any{"subtype": "interrupt"})
	}
	return e
}
func (m *Manager) Approve(id, key, decision, answer string) error {
	if decision != "accept" && decision != "decline" {
		return errors.New("无效审批决定")
	}
	var ss store.Session
	if m.Store.Get("sessions", id, &ss) == nil && ss.Agent == "claude" {
		if handled, err := m.controlClaudeDesktop(ss, key, decision, answer); handled {
			return err
		}
		_, native, err := m.claudeInbox(ss)
		if err != nil {
			return err
		}
		if native {
			return errors.New("请在电脑 Claude 原会话中处理停止或工具审批")
		}
	}
	if m.Store.Get("sessions", id, &ss) == nil && ss.Agent != "codex" && ss.Agent != "claude" {
		c, ss, e := m.externalSession(id)
		if e != nil {
			return e
		}
		return c.Approve(m.Context, ss, key, decision, answer)
	}
	var desktopSession store.Session
	if m.Store.Get("sessions", id, &desktopSession) == nil && desktopSession.Source == "desktop" {
		return errors.New("请在电脑 Codex 中处理停止或工具审批")
	}
	r, e := m.get(id)
	if e != nil {
		return e
	}
	r.mu.Lock()
	a, ok := r.approvals[key]
	if ok {
		delete(r.approvals, key)
	}
	r.mu.Unlock()
	if !ok {
		return errors.New("审批已过期")
	}
	responses := map[string]string{}
	_ = json.Unmarshal([]byte(answer), &responses)
	answerFor := func(key string) string {
		if value, ok := responses[key]; ok {
			return value
		}
		return answer
	}

	var result any
	if r.session.Agent == "claude" {
		v := map[string]any{"behavior": "deny", "message": "用户拒绝"}
		if decision == "accept" {
			v = map[string]any{"behavior": "allow", "updatedInput": a.Params["input"]}
		}
		if str(a.Params["tool_name"]) == "AskUserQuestion" && decision == "accept" {
			input := obj(a.Params["input"])
			answers := map[string]any{}
			for _, q := range arr(input["questions"]) {
				answers[str(obj(q)["question"])] = answerFor(str(obj(q)["question"]))
			}
			input["answers"] = answers
			v["updatedInput"] = input
		}
		result = map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": a.ID, "response": v}}
	} else {
		v := map[string]any{"decision": "decline"}
		switch a.Method {
		case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
			if decision == "accept" {
				v["decision"] = "accept"
			}
		case "item/permissions/requestApproval":
			v = map[string]any{"permissions": map[string]any{}, "scope": "turn"}
			if decision == "accept" {
				v["permissions"] = a.Params["permissions"]
			}
		case "item/tool/requestUserInput", "tool/requestUserInput":
			answers := map[string]any{}
			for _, q := range arr(a.Params["questions"]) {
				answers[str(obj(q)["id"])] = map[string]any{"answers": []string{answerFor(str(obj(q)["id"]))}}
			}
			v = map[string]any{"answers": answers}
		default:
			v = map[string]any{"action": "decline", "content": nil}
		}
		result = map[string]any{"id": a.ID, "result": v}
	}
	if e = r.write(result); e != nil {
		return e
	}
	r.emit(store.Event{Kind: "approval", Item: key, Mode: "snapshot", Text: Redact(jsonText(a.Params)), Status: "resolved"})
	return nil
}
func (m *Manager) Close() {
	m.closeDeliveries()
	m.closeDesktop()
	for _, c := range m.Connectors {
		c.Close()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		r.close()
	}
}
func (m *Manager) launch(ss store.Session, resume bool) (*runtime, error) {
	args := []string{"app-server"}
	if ss.Agent == "claude" {
		args = []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--permission-mode", "default", "--permission-prompt-tool", "stdio"}
		if resume {
			args = append(args, "--resume="+ss.ID)
		} else {
			args = append(args, "--session-id="+ss.ID)
		}
	}
	opts := m.Options(ss.ID)
	if ss.Agent == "claude" {
		if opts.Model != "" {
			args = append(args, "--model="+opts.Model)
		}
		if opts.Speed != "" {
			args = append(args, "--settings", fmt.Sprintf(`{"fastMode":%t}`, opts.Speed == "fast"))
		}
		if opts.Effort != "" {
			args = append(args, "--effort="+opts.Effort)
		}
	}
	ctx := m.Context
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, ss.Agent, args...)
	cmd.Dir = ss.Project
	cmd.Stderr = io.Discard
	input, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	output, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		return nil, fmt.Errorf("%s 启动失败，请在工作站安装并登录 CLI", ss.Agent)
	}
	r := &runtime{manager: m, session: ss, cmd: cmd, input: input, pending: map[string]chan map[string]any{}, approvals: map[string]approval{}, done: make(chan struct{}), state: State{Session: ss.ID, CWD: ss.Project}, options: opts, settings: SessionSettings{Model: opts.Model, Effort: opts.Effort, Speed: opts.Speed, Source: "live"}}
	go r.read(output)
	var err error
	if ss.Agent == "codex" {
		_, err = r.call("initialize", map[string]any{"clientInfo": map[string]any{"name": "agentdeck", "title": "AgentDeck", "version": "0.4.1"}})
		if err == nil {
			err = r.write(map[string]any{"method": "initialized"})
		}
	} else {
		_, err = r.call("initialize", map[string]any{"subtype": "initialize", "hooks": nil})
	}
	if err != nil {
		r.close()
		return nil, err
	}
	return r, nil
}
func (r *runtime) close() { _ = r.input.Close(); _ = r.cmd.Process.Kill() }
func (r *runtime) write(v any) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return json.NewEncoder(r.input).Encode(v)
}
func (r *runtime) call(method string, params map[string]any) (map[string]any, error) {
	id := UUID()
	ch := make(chan map[string]any, 1)
	r.mu.Lock()
	r.pending[id] = ch
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.pending, id); r.mu.Unlock() }()
	v := map[string]any{"id": id, "method": method, "params": params}
	if r.session.Agent == "claude" {
		v = map[string]any{"type": "control_request", "request_id": id, "request": params}
	}
	if e := r.write(v); e != nil {
		return nil, e
	}
	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()
	select {
	case d := <-ch:
		if d["error"] != nil {
			return nil, errors.New(Redact(jsonText(d["error"])))
		}
		return obj(d["result"]), nil
	case <-r.done:
		return nil, errors.New(r.session.Agent + " 进程已退出，请检查工作站登录状态")
	case <-timer.C:
		return nil, errors.New(r.session.Agent + " 接口响应超时")
	}
}
func (r *runtime) emit(ev store.Event) {
	if ev.ID == "" {
		ev.ID = "runtime/" + UUID()
	}
	r.mu.Lock()
	ev.Session = r.session.ID
	ev.Agent = r.session.Agent
	ev.Project = r.session.Project
	r.mu.Unlock()
	ev.Source = "stream"
	_, _, _ = r.manager.Store.Append(ev)
}
func (r *runtime) read(reader io.Reader) {
	defer func() {
		_ = r.cmd.Wait()
		close(r.done)
		r.mu.Lock()
		busy := r.busy
		r.busy = false
		r.mu.Unlock()
		if busy {
			r.emit(store.Event{Kind: "notice", Text: "Agent 进程退出；检查登录或网络后重试。"})
			r.emit(store.Event{Kind: "status", Status: "failed"})
		}
	}()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	for scanner.Scan() {
		raw := append([]byte(nil), scanner.Bytes()...)
		var d map[string]any
		if json.Unmarshal(raw, &d) != nil {
			continue
		}
		id := str(d["id"])
		response := d
		if str(d["type"]) == "control_response" {
			resp := obj(d["response"])
			id = str(resp["request_id"])
			response = map[string]any{"result": resp["response"]}
			if str(resp["subtype"]) == "error" {
				response["error"] = resp["error"]
			}
		}
		r.mu.Lock()
		ch := r.pending[id]
		r.mu.Unlock()
		if ch != nil {
			select {
			case ch <- response:
			default:
			}
			continue
		}
		method := str(d["method"])
		params := obj(d["params"])
		if d["id"] != nil && method != "" || str(d["type"]) == "control_request" {
			a := approval{ID: d["id"], Method: method, Params: params}
			if str(d["type"]) == "control_request" {
				a = approval{ID: d["request_id"], Method: str(obj(d["request"])["subtype"]), Params: obj(d["request"])}
				if a.Method != "can_use_tool" {
					_ = r.write(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "error", "request_id": a.ID, "error": "Unsupported host request"}})
					continue
				}
			}
			key := UUID()
			r.mu.Lock()
			r.approvals[key] = a
			r.mu.Unlock()
			r.emit(store.Event{Kind: "approval", Item: key, Text: Redact(jsonText(a.Params)), Mode: "snapshot", Status: "pending"})
			continue
		}
		if method == "serverRequest/resolved" || str(d["type"]) == "control_cancel_request" {
			keys := []string{}
			r.mu.Lock()
			for key, a := range r.approvals {
				if fmt.Sprint(a.ID) == fmt.Sprint(params["requestId"]) || fmt.Sprint(a.ID) == fmt.Sprint(d["request_id"]) {
					delete(r.approvals, key)
					keys = append(keys, key)
				}
			}
			r.mu.Unlock()
			for _, key := range keys {
				r.emit(store.Event{Kind: "approval", Item: key, Status: "resolved", Mode: "snapshot"})
			}
		}
		r.mu.Lock()
		if r.session.Agent == "claude" {
			native := nativeSettings(d, "claude")
			if native.Model != "" {
				r.settings.Model = native.Model
			}
			if native.Effort != "" {
				r.settings.Effort = native.Effort
			}
			if native.Speed != "" {
				r.settings.Speed = native.Speed
			}
		}
		events, err := r.manager.Normalizer.Parse(raw, r.session.Agent, "stream", &r.state)
		r.mu.Unlock()
		if err != nil {
			continue
		}
		// Release execution before publishing completion to the phone.
		if method == "turn/completed" || str(d["type"]) == "result" {
			keys := []string{}
			r.mu.Lock()
			if str(d["type"]) == "result" && r.claudeInputs > 0 {
				r.claudeInputs--
			}
			r.busy = r.claudeInputs > 0
			more := r.busy
			for key := range r.approvals {
				delete(r.approvals, key)
				keys = append(keys, key)
			}
			r.mu.Unlock()
			for _, key := range keys {
				r.emit(store.Event{Kind: "approval", Item: key, Mode: "snapshot", Status: "resolved"})
			}
			if more {
				for i := range events {
					if events[i].Kind == "status" && events[i].Status == "completed" {
						events[i].Status = "running"
					}
				}
			}
		}
		for _, ev := range events {
			r.emit(ev)
		}
		if method == "turn/started" {
			r.mu.Lock()
			r.turn = str(obj(params["turn"])["id"])
			r.mu.Unlock()
		}
		if str(d["type"]) == "result" && d["is_error"] == true {
			r.emit(store.Event{Kind: "notice", Text: Redact(jsonText(d["errors"]))})
		}
	}
}
