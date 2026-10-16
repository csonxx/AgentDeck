package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"agentdeck/internal/store"
	acp "github.com/coder/acp-go-sdk"
)

type acpConnector struct {
	m      *Manager
	id     string
	cfg    ACPConfig
	mu     sync.Mutex
	runs   map[string]*acpRuntime
	detail string
}
type acpApproval struct {
	options []acp.PermissionOption
	reply   chan acp.RequestPermissionOutcome
}
type acpRuntime struct {
	c                       *acpConnector
	s                       store.Session
	cmd                     *exec.Cmd
	input                   io.WriteCloser
	conn                    *acp.ClientSideConnection
	caps                    acp.AgentCapabilities
	configOptions           []map[string]any
	modes                   *acp.SessionModeState
	mu                      sync.Mutex
	busy, loading, suppress bool
	turn, lastKind          string
	seq                     int
	texts                   map[string]string
	tools                   map[string]map[string]any
	approvals               map[string]acpApproval
	terminals               map[string]*acpTerminal
}

func newACP(m *Manager, id string, c ACPConfig) *acpConnector {
	return &acpConnector{m: m, id: id, cfg: c, runs: map[string]*acpRuntime{}}
}
func (c *acpConnector) Info() AgentInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, e := exec.LookPath(c.cfg.Command)
	ok := e == nil && c.cfg.Command != ""
	d := "请在工作站安装并登录 " + AgentNames[c.id] + " CLI"
	if ok {
		d = "已安装 · ACP · 首次连接校验登录"
	}
	if c.detail != "" {
		d = c.detail
	}
	return AgentInfo{ID: c.id, Name: AgentNames[c.id], Configured: ok, Create: true, Send: true, Stop: true, Approve: true, Detail: d}
}
func (c *acpConnector) launch(ctx context.Context, s store.Session) (*acpRuntime, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, e := exec.LookPath(c.cfg.Command); e != nil {
		return nil, errors.New("请在工作站安装并登录 " + AgentNames[c.id] + " CLI")
	}
	cmd := exec.CommandContext(ctx, c.cfg.Command, c.cfg.Args...)
	cmd.Dir = s.Project
	terminalProcessGroup(cmd)
	cmd.Stderr = io.Discard
	in, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		return nil, errors.New("ACP 进程启动失败")
	}
	r := &acpRuntime{c: c, s: s, cmd: cmd, input: in, texts: map[string]string{}, tools: map[string]map[string]any{}, approvals: map[string]acpApproval{}, terminals: map[string]*acpTerminal{}}
	r.conn = acp.NewClientSideConnection(r, in, out)
	r.conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	go func() {
		_ = cmd.Wait()
		killTerminalProcess(cmd)
		r.mu.Lock()
		busy := r.busy
		session := r.s
		r.busy = false
		r.cancelApprovalsLocked()
		r.mu.Unlock()
		if busy {
			c.m.emitExternal(session, store.Event{Kind: "notice", Text: AgentNames[c.id] + " 连接已断开，请检查 CLI 登录状态"})
			c.m.emitExternal(session, store.Event{Kind: "status", Status: "failed"})
		}
		r.killTerminals()
	}()
	initCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	v, e := r.conn.Initialize(initCtx, acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersionNumber, ClientInfo: &acp.Implementation{Name: "agentdeck", Version: "0.4.0"}, ClientCapabilities: acp.ClientCapabilities{Fs: acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true}, Terminal: true}})
	if e != nil {
		r.close()
		return nil, errors.New(AgentNames[c.id] + " ACP 初始化失败，请检查版本或登录")
	}
	r.caps = v.AgentCapabilities
	return r, nil
}
func (c *acpConnector) get(ctx context.Context, s store.Session) (*acpRuntime, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r := c.runs[s.ID]; r != nil {
		select {
		case <-r.conn.Done():
			delete(c.runs, s.ID)
		default:
			return r, nil
		}
	}
	c.pruneLocked()
	if len(c.runs) >= 16 {
		return nil, errors.New("已达到同时打开 ACP 会话上限")
	}
	r, e := c.launch(ctx, s)
	if e != nil {
		return nil, e
	}
	if !r.caps.LoadSession {
		r.close()
		return nil, errors.New("当前 Agent 版本不支持恢复原会话")
	}
	r.mu.Lock()
	r.loading = true
	h, _ := c.m.Store.History(s.ID, 0, 1)
	r.suppress = len(h.Events) > 0
	r.mu.Unlock()
	loadCtx, cancel := context.WithTimeout(nonNil(ctx), 60*time.Second)
	defer cancel()
	loaded, e := r.conn.LoadSession(loadCtx, acp.LoadSessionRequest{SessionId: acp.SessionId(nativeID(s)), Cwd: s.Project, McpServers: []acp.McpServer{}})
	r.mu.Lock()
	r.configOptions = configMaps(loaded.ConfigOptions)
	r.modes = loaded.Modes
	r.loading = false
	r.suppress = false
	r.lastKind = ""
	r.mu.Unlock()
	if e != nil {
		r.close()
		return nil, errors.New("原会话恢复失败，请检查 Agent 登录与会话 ID")
	}
	c.runs[s.ID] = r
	return r, nil
}
func (c *acpConnector) pruneLocked() {
	for id, r := range c.runs {
		select {
		case <-r.conn.Done():
			delete(c.runs, id)
		default:
		}
	}
}
func (c *acpConnector) Hydrate(ctx context.Context, s store.Session) error {
	_, e := c.get(ctx, s)
	return e
}
func nonNil(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
func (c *acpConnector) Create(ctx context.Context, path, parent string) (store.Session, error) {
	if parent != "" {
		_, s, e := c.m.externalSession(parent)
		if e != nil {
			return s, e
		}
		_, e = c.get(ctx, s)
		return s, e
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked()
	if len(c.runs) >= 16 {
		return store.Session{}, errors.New("已达到同时打开 ACP 会话上限")
	}
	s := newExternalSession("pending", c.id, path, "acp")
	r, e := c.launch(ctx, s)
	if e != nil {
		return s, e
	}
	newCtx, cancel := context.WithTimeout(nonNil(ctx), 30*time.Second)
	defer cancel()
	v, e := r.conn.NewSession(newCtx, acp.NewSessionRequest{Cwd: path, McpServers: []acp.McpServer{}})
	if e != nil || v.SessionId == "" {
		c.detail = "请在电脑完成 " + AgentNames[c.id] + " CLI 登录并检查模型配置"
		r.close()
		return s, errors.New("创建会话失败，请检查 CLI 登录状态")
	}
	s.ID = sessionID(c.id, string(v.SessionId))
	r.mu.Lock()
	r.s = s
	r.configOptions = configMaps(v.ConfigOptions)
	r.modes = v.Modes
	r.mu.Unlock()
	if e = c.m.Store.Put("sessions", s.ID, s); e != nil {
		r.close()
		return s, e
	}
	c.runs[s.ID] = r
	return s, nil
}
func (c *acpConnector) Send(ctx context.Context, s store.Session, text, request string) error {
	return c.SendInput(ctx, s, text, request, TurnOptions{}, nil)
}
func (c *acpConnector) SendInput(ctx context.Context, s store.Session, text, request string, options TurnOptions, files []InputAttachment) error {
	r, e := c.get(ctx, s)
	if e != nil {
		return e
	}
	r.mu.Lock()
	if r.busy {
		r.mu.Unlock()
		return errors.New("正在执行，请等待或停止当前任务")
	}
	if imageFiles(files) && !r.caps.PromptCapabilities.Image {
		r.mu.Unlock()
		return errors.New("当前 ACP Agent 未声明图片输入支持")
	}
	r.busy = true
	r.mu.Unlock()
	if e = r.applyOptions(ctx, options); e != nil {
		r.mu.Lock()
		r.busy = false
		r.mu.Unlock()
		return e
	}
	prompt := []acp.ContentBlock{acp.TextBlock(text)}
	display := text
	if len(files) > 0 {
		_, display, e = c.m.prepareInput(s.ID, text, request, files)
		if e != nil {
			r.mu.Lock()
			r.busy = false
			r.mu.Unlock()
			return e
		}
		prompt[0] = acp.TextBlock(display)
		for _, file := range files {
			if strings.HasPrefix(file.MIME, "image/") {
				prompt = append(prompt, acp.ImageBlock(base64.StdEncoding.EncodeToString(file.Data), file.MIME))
			}
		}
	}
	r.mu.Lock()
	r.turn = request
	r.seq = 0
	r.lastKind = ""
	r.texts = map[string]string{}
	r.tools = map[string]map[string]any{}
	r.mu.Unlock()
	c.m.emitExternal(s, store.Event{ID: "acp-input/" + s.ID + "/" + request, Kind: "message", Role: "user", Item: "turn/" + request + "/user", Text: display, Mode: "snapshot"})
	c.m.emitExternal(s, store.Event{Kind: "status", Status: "running"})
	go func() {
		result, err := r.conn.Prompt(nonNil(ctx), acp.PromptRequest{SessionId: acp.SessionId(nativeID(s)), Prompt: prompt})
		r.mu.Lock()
		r.busy = false
		r.cancelApprovalsLocked()
		r.mu.Unlock()
		status := "completed"
		if result.StopReason == acp.StopReasonCancelled {
			status = "interrupted"
		}
		if err != nil {
			status = "failed"
			c.m.emitExternal(s, store.Event{Kind: "notice", Text: AgentNames[c.id] + " 执行失败，请检查登录、网络或模型配置"})
		}
		c.m.emitExternal(s, store.Event{Kind: "status", Status: status})
	}()
	return nil
}
func (c *acpConnector) Interrupt(ctx context.Context, s store.Session) error {
	c.mu.Lock()
	r := c.runs[s.ID]
	c.mu.Unlock()
	if r == nil {
		return errors.New("会话当前没有活动执行")
	}
	r.mu.Lock()
	r.cancelApprovalsLocked()
	r.mu.Unlock()
	return r.conn.Cancel(nonNil(ctx), acp.CancelNotification{SessionId: acp.SessionId(nativeID(s))})
}
func (c *acpConnector) Approve(ctx context.Context, s store.Session, key, decision, answer string) error {
	c.mu.Lock()
	r := c.runs[s.ID]
	c.mu.Unlock()
	if r == nil {
		return errors.New("审批会话已断开")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.approvals[key]
	if !ok {
		return errors.New("审批已失效或不属于当前会话")
	}
	out := acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{}}
	kind := acp.PermissionOptionKindRejectOnce
	if decision == "accept" {
		kind = acp.PermissionOptionKindAllowOnce
	}
	for _, v := range p.options {
		if v.Kind == kind {
			out = acp.RequestPermissionOutcome{Selected: &acp.RequestPermissionOutcomeSelected{OptionId: v.OptionId}}
			break
		}
	}
	if decision == "accept" && out.Selected == nil {
		return errors.New("Agent 没有提供单次允许选项")
	}
	p.reply <- out
	delete(r.approvals, key)
	c.m.emitExternal(s, store.Event{Kind: "approval", Item: key, Mode: "snapshot", Status: "resolved"})
	return nil
}
func (c *acpConnector) Run(ctx context.Context) {
	// Listing is optional in ACP v1. Never manufacture IDE sessions if absent.
	if c.cfg.Command == "" {
		return
	}
	_, e := exec.LookPath(c.cfg.Command)
	if e != nil {
		return
	}
	for ctx.Err() == nil {
		projects := c.m.Projects()
		if len(projects) > 0 {
			s := newExternalSession("discovery", c.id, projects[0].Path, "acp")
			r, e := c.launch(ctx, s)
			if e == nil {
				if r.caps.SessionCapabilities.List != nil {
					var cursor *string
					for page := 0; page < 100; page++ {
						q, cancel := context.WithTimeout(ctx, 20*time.Second)
						v, e := r.conn.ListSessions(q, acp.ListSessionsRequest{Cursor: cursor})
						cancel()
						if e != nil {
							c.mu.Lock()
							c.detail = "请在电脑完成 " + AgentNames[c.id] + " CLI 登录，会话同步尚未成功"
							c.mu.Unlock()
							break
						}
						c.mu.Lock()
						c.detail = "已连接 · ACP · 原会话"
						c.mu.Unlock()
						for _, info := range v.Sessions {
							path, e := c.m.allowed(info.Cwd)
							if e != nil {
								continue
							}
							ss := newExternalSession(string(info.SessionId), c.id, path, "acp")
							_ = c.m.Store.Get("sessions", ss.ID, &ss)
							if info.Title != nil {
								ss.Title = Redact(*info.Title)
							}
							if info.UpdatedAt != nil {
								ss.Updated = *info.UpdatedAt
							}
							_ = c.m.Store.Put("sessions", ss.ID, ss)
						}
						if v.NextCursor == nil {
							break
						}
						if cursor != nil && *cursor == *v.NextCursor {
							break
						}
						cursor = v.NextCursor
					}
				}
				r.close()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}
func (c *acpConnector) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.runs {
		r.close()
	}
}
func (r *acpRuntime) close() {
	_ = r.input.Close()
	killTerminalProcess(r.cmd)
	r.killTerminals()
}
func (r *acpRuntime) valid(id acp.SessionId) bool { return string(id) == nativeID(r.s) }
func (r *acpRuntime) cancelApprovalsLocked() {
	for key, p := range r.approvals {
		select {
		case p.reply <- acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{}}:
		default:
		}
		delete(r.approvals, key)
		r.c.m.emitExternal(r.s, store.Event{Kind: "approval", Item: key, Mode: "snapshot", Status: "resolved"})
	}
}
func (r *acpRuntime) RequestPermission(ctx context.Context, p acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	r.mu.Lock()
	if !r.valid(p.SessionId) {
		r.mu.Unlock()
		return acp.RequestPermissionResponse{}, errors.New("wrong session")
	}
	key := UUID()
	ch := make(chan acp.RequestPermissionOutcome, 1)
	r.approvals[key] = acpApproval{p.Options, ch}
	session := r.s
	r.mu.Unlock()
	r.c.m.emitExternal(session, store.Event{Kind: "approval", Item: key, Text: jsonText(p.ToolCall), Mode: "snapshot", Status: "pending"})
	defer func() { r.mu.Lock(); delete(r.approvals, key); r.mu.Unlock() }()
	select {
	case outcome := <-ch:
		return acp.RequestPermissionResponse{Outcome: outcome}, nil
	case <-ctx.Done():
		return acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{}}}, nil
	case <-r.conn.Done():
		return acp.RequestPermissionResponse{}, errors.New("agent disconnected")
	}
}
func (r *acpRuntime) SessionUpdate(ctx context.Context, n acp.SessionNotification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.valid(n.SessionId) {
		return errors.New("wrong session")
	}
	if n.Update.CurrentModeUpdate != nil && r.modes != nil {
		r.modes.CurrentModeId = n.Update.CurrentModeUpdate.CurrentModeId
		return nil
	}
	if n.Update.ConfigOptionUpdate != nil {
		r.configOptions = configMaps(n.Update.ConfigOptionUpdate.ConfigOptions)
		return nil
	}
	if r.suppress {
		return nil
	}
	b, _ := json.Marshal(n.Update)
	var u map[string]any
	_ = json.Unmarshal(b, &u)
	kind := str(u["sessionUpdate"])
	if kind == "session_info_update" {
		if title := str(u["title"]); title != "" {
			r.s.Title = Redact(title)
			return r.c.m.Store.Put("sessions", r.s.ID, r.s)
		}
		return nil
	}
	if kind == "agent_message_chunk" || kind == "user_message_chunk" || kind == "agent_thought_chunk" {
		if kind == "user_message_chunk" && !r.loading {
			return nil
		}
		role := "assistant"
		ek := "message"
		if kind == "user_message_chunk" {
			role = "user"
		}
		if kind == "agent_thought_chunk" {
			ek = "thinking"
		}
		if r.lastKind != kind {
			r.seq++
			r.lastKind = kind
		}
		prefix := "turn/" + r.turn
		if r.loading {
			prefix = "history"
		}
		item := fmt.Sprintf("%s/%d/%s", prefix, r.seq, ek)
		content := obj(u["content"])
		if str(content["type"]) == "text" {
			r.texts[item] += str(content["text"])
			if len(r.texts[item]) > 128<<10 {
				r.texts[item] = r.texts[item][:128<<10]
			}
			ev := store.Event{Kind: ek, Role: role, Item: item, Text: r.texts[item], Mode: "snapshot"}
			ev.ID = "acp/" + digest(r.s.ID+item+ev.Text)
			r.c.m.emitExternal(r.s, ev)
		} else {
			r.content(content, item)
		}
		return nil
	}
	if kind == "tool_call" || kind == "tool_call_update" {
		r.lastKind = ""
		key := str(u["toolCallId"])
		merged := r.tools[key]
		if merged == nil {
			merged = map[string]any{}
			r.tools[key] = merged
		}
		for k, v := range u {
			if v != nil {
				merged[k] = v
			}
		}
		u = merged
		text := str(u["title"])
		if u["rawInput"] != nil {
			text += "\n" + jsonText(u["rawInput"])
		}
		if u["rawOutput"] != nil {
			text += "\n" + jsonText(u["rawOutput"])
		}
		for _, v := range arr(u["content"]) {
			p := obj(v)
			if str(p["type"]) == "content" {
				if c := obj(p["content"]); str(c["type"]) == "text" {
					text += "\n" + str(c["text"])
				} else {
					r.content(c, str(u["toolCallId"])+"/image")
				}
			} else if str(p["type"]) == "diff" {
				text += "\n" + str(p["path"]) + "\n" + str(p["newText"])
			}
		}
		ev := store.Event{Kind: "tool", Item: str(u["toolCallId"]), Text: text, Mode: "snapshot", Status: str(u["status"])}
		ev.ID = "acp-tool/" + digest(r.s.ID+string(b))
		r.c.m.emitExternal(r.s, ev)
	}
	if kind == "plan" {
		ev := store.Event{Kind: "tool", Item: "plan", Text: jsonText(u["entries"]), Mode: "snapshot"}
		ev.ID = "acp-plan/" + digest(r.s.ID+ev.Text)
		r.c.m.emitExternal(r.s, ev)
	}
	return nil
}
func (r *acpRuntime) content(c map[string]any, item string) {
	if str(c["type"]) == "image" {
		src := str(c["uri"])
		if data := str(c["data"]); data != "" {
			src = "data:" + str(c["mimeType"]) + ";base64," + data
		}
		id, e := r.c.m.Normalizer.media(src, r.s.Project)
		if e == nil {
			r.c.m.emitExternal(r.s, store.Event{ID: "acp-image/" + r.s.ID + "/" + item + "/" + id, Kind: "image", Item: item + "/" + id, Media: id})
		} else if strings.HasPrefix(src, "https://") && Redact(src) == src {
			r.c.m.emitExternal(r.s, store.Event{Kind: "image", Item: item, Text: src})
		}
	}
	if str(c["type"]) == "resource_link" {
		r.c.m.emitExternal(r.s, store.Event{Kind: "message", Role: "assistant", Item: item + "/link", Text: "[" + str(c["name"]) + "](" + str(c["uri"]) + ")", Mode: "snapshot"})
	}
}

// Agent-initiated client operations stay within this exact session's project.
func (r *acpRuntime) file(id acp.SessionId, path string, write bool) (string, error) {
	r.mu.Lock()
	valid, project := r.valid(id), r.s.Project
	r.mu.Unlock()
	if !valid || !filepath.IsAbs(path) {
		return "", errors.New("invalid session or path")
	}
	p, e := filepath.EvalSymlinks(path)
	if os.IsNotExist(e) && write {
		parent, e2 := filepath.EvalSymlinks(filepath.Dir(path))
		if e2 != nil {
			return "", e2
		}
		p = filepath.Join(parent, filepath.Base(path))
		e = nil
	}
	if e != nil {
		return "", errors.New("file unavailable")
	}
	rel, e := filepath.Rel(project, p)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("file outside session project")
	}
	return p, nil
}
func (r *acpRuntime) ReadTextFile(ctx context.Context, p acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	path, e := r.file(p.SessionId, p.Path, false)
	if e != nil {
		return acp.ReadTextFileResponse{}, e
	}
	r.mu.Lock()
	project := r.s.Project
	r.mu.Unlock()
	root, e := os.OpenRoot(project)
	if e != nil {
		return acp.ReadTextFileResponse{}, e
	}
	defer root.Close()
	relative, e := filepath.Rel(project, path)
	if e != nil {
		return acp.ReadTextFileResponse{}, e
	}
	f, e := root.Open(relative)
	if e != nil {
		return acp.ReadTextFileResponse{}, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > 2<<20 {
		return acp.ReadTextFileResponse{}, errors.New("text file exceeds limit")
	}
	b, e := io.ReadAll(io.LimitReader(f, 2<<20))
	if e != nil || !utf8.Valid(b) {
		return acp.ReadTextFileResponse{}, errors.New("invalid text file")
	}
	lines := strings.Split(string(b), "\n")
	start := 0
	if p.Line != nil && *p.Line > 0 {
		start = *p.Line - 1
	}
	if start > len(lines) {
		start = len(lines)
	}
	end := len(lines)
	if p.Limit != nil && *p.Limit > 0 && start+*p.Limit < end {
		end = start + *p.Limit
	}
	return acp.ReadTextFileResponse{Content: strings.Join(lines[start:end], "\n")}, nil
}
func (r *acpRuntime) WriteTextFile(ctx context.Context, p acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	path, e := r.file(p.SessionId, p.Path, true)
	if e != nil {
		return acp.WriteTextFileResponse{}, e
	}
	if len(p.Content) > 2<<20 {
		return acp.WriteTextFileResponse{}, errors.New("text file exceeds limit")
	}
	r.mu.Lock()
	project := r.s.Project
	r.mu.Unlock()
	root, e := os.OpenRoot(project)
	if e != nil {
		return acp.WriteTextFileResponse{}, e
	}
	defer root.Close()
	relative, e := filepath.Rel(project, path)
	if e != nil {
		return acp.WriteTextFileResponse{}, e
	}
	f, e := root.OpenFile(relative, os.O_WRONLY|os.O_CREATE, 0600)
	if e != nil {
		return acp.WriteTextFileResponse{}, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return acp.WriteTextFileResponse{}, errors.New("not a regular text file")
	}
	if e = f.Truncate(0); e != nil {
		return acp.WriteTextFileResponse{}, e
	}
	_, e = f.WriteString(p.Content)
	return acp.WriteTextFileResponse{}, e
}
