package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"agentdeck/internal/store"
)

type openCode struct {
	m          *Manager
	cfg        HTTPAgentConfig
	http       *http.Client
	mu         sync.Mutex
	sendMu     sync.Mutex
	questionMu sync.Mutex
	detail     string
	connected  bool
}

func agentHTTP() *http.Client {
	return &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func newOpenCode(m *Manager, c HTTPAgentConfig) *openCode {
	detail := "请配置 OpenCode server 地址"
	if c.URL != "" {
		detail = "已配置 · 正在连接 OpenCode server"
	}
	return &openCode{m: m, cfg: c, http: agentHTTP(), detail: detail}
}
func (o *openCode) Info() AgentInfo {
	o.mu.Lock()
	defer o.mu.Unlock()
	d := o.detail
	if o.connected {
		d = "已连接 · 原会话 · 实时事件"
	}
	return AgentInfo{ID: "opencode", Name: "OpenCode", Configured: o.cfg.URL != "", Create: true, Send: true, Stop: true, Approve: true, Detail: d}
}

type openCodeHTTPError struct{ status int }

func (e openCodeHTTPError) Error() string {
	return fmt.Sprintf("OpenCode 请求失败（HTTP %d），请检查登录、版本与会话状态", e.status)
}
func (o *openCode) request(ctx context.Context, method, path, project string, in, out any) error {
	if o.cfg.URL == "" {
		return errors.New("请在工作站配置 OpenCode server 地址")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	u, e := url.Parse(strings.TrimRight(o.cfg.URL, "/") + path)
	if e != nil {
		return errors.New("OpenCode 地址无效")
	}
	if project != "" {
		q := u.Query()
		q.Set("directory", project)
		u.RawQuery = q.Encode()
	}
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	r, e := http.NewRequestWithContext(ctx, method, u.String(), body)
	if e != nil {
		return e
	}
	r.Header.Set("Content-Type", "application/json")
	if o.cfg.PasswordEnv != "" {
		password := os.Getenv(o.cfg.PasswordEnv)
		if password == "" {
			return errors.New("OpenCode 密码环境变量未配置")
		}
		user := o.cfg.Username
		if user == "" {
			user = "opencode"
		}
		r.SetBasicAuth(user, password)
	}
	res, e := o.http.Do(r)
	if e != nil {
		return errors.New("OpenCode 未连接，请检查 server 地址或登录状态")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return openCodeHTTPError{res.StatusCode}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out)
}
func (o *openCode) Create(ctx context.Context, path, parent string) (store.Session, error) {
	if parent != "" {
		_, ss, e := o.m.externalSession(parent)
		if e != nil {
			return ss, e
		}
		var v map[string]any
		e = o.request(ctx, "GET", "/session/"+url.PathEscape(nativeID(ss)), path, nil, &v)
		if e != nil {
			return ss, e
		}
		return o.importSession(v)
	}
	var v map[string]any
	e := o.request(ctx, "POST", "/session", path, map[string]any{"title": "新会话"}, &v)
	if e != nil {
		return store.Session{}, e
	}
	return o.importSession(v)
}
func (o *openCode) importSession(v map[string]any) (store.Session, error) {
	path, e := o.m.allowed(str(v["directory"]))
	if e != nil {
		return store.Session{}, e
	}
	id := str(v["id"])
	if id == "" {
		return store.Session{}, errors.New("OpenCode 未返回会话 ID")
	}
	ss := newExternalSession(id, "opencode", path, "opencode-api")
	_ = o.m.Store.Get("sessions", ss.ID, &ss)
	ss.Project = path
	ss.Managed = true
	ss.Agent = "opencode"
	ss.Source = "opencode-api"
	// Native child sessions are execution details of their parent, not extra
	// user chats. Keep their identity/history for routing without inflating the
	// project/recent catalog (including rows imported by older Local versions).
	ss.Parent = ""
	if parent := str(v["parentID"]); parent != "" {
		ss.Parent = sessionID("opencode", parent)
	}
	ss.CatalogHidden = ss.Parent != "" || int64Number(obj(v["time"])["archived"]) > 0
	if title := str(v["title"]); title != "" {
		ss.Title = Redact(title)
	}
	if t, ok := obj(v["time"])["updated"].(float64); ok {
		ss.Updated = time.UnixMilli(int64(t)).UTC().Format(time.RFC3339Nano)
	}
	return ss, o.m.Store.PutCatalogSession(ss)
}
func (o *openCode) Send(ctx context.Context, ss store.Session, text, request string) error {
	return o.SendInput(ctx, ss, text, request, TurnOptions{}, nil)
}
func (o *openCode) SendInput(ctx context.Context, ss store.Session, text, request string, options TurnOptions, files []InputAttachment) error {
	o.sendMu.Lock()
	defer o.sendMu.Unlock()
	parts := []any{map[string]any{"type": "text", "text": text}}
	if len(files) > 0 {
		_, display, e := o.m.prepareInput(ss.ID, text, request, files)
		if e != nil {
			return e
		}
		parts[0] = map[string]any{"type": "text", "text": display}
		for _, file := range files {
			if strings.HasPrefix(file.MIME, "image/") {
				parts = append(parts, map[string]any{"type": "file", "mime": file.MIME, "filename": file.Name, "url": "data:" + file.MIME + ";base64," + base64.StdEncoding.EncodeToString(file.Data)})
			}
		}
	}
	body := map[string]any{"messageID": "msg_" + digest(ss.ID + request)[:32], "parts": parts}
	if options.Model != "" {
		ids := strings.SplitN(options.Model, "/", 2)
		if len(ids) != 2 {
			return errors.New("OpenCode 模型标识无效")
		}
		body["model"] = map[string]string{"providerID": ids[0], "modelID": ids[1]}
	}
	if options.Effort != "" {
		body["variant"] = options.Effort
	}
	e := o.request(ctx, "POST", "/session/"+url.PathEscape(nativeID(ss))+"/prompt_async", ss.Project, body, nil)
	if e == nil {
		o.m.emitExternal(ss, store.Event{Kind: "status", Status: "running"})
	}
	return e
}
func (o *openCode) Interrupt(ctx context.Context, ss store.Session) error {
	return o.request(ctx, "POST", "/session/"+url.PathEscape(nativeID(ss))+"/abort", ss.Project, map[string]any{}, nil)
}
func (o *openCode) Approve(ctx context.Context, ss store.Session, key, decision, answer string) error {
	if strings.HasPrefix(key, openCodeQuestionPrefix) {
		return o.answerQuestion(ctx, ss, strings.TrimPrefix(key, openCodeQuestionPrefix), decision, answer)
	}
	var pending []map[string]any
	if e := o.request(ctx, "GET", "/permission", ss.Project, nil, &pending); e != nil {
		return e
	}
	found := false
	for _, p := range pending {
		if str(p["id"]) == key && str(p["sessionID"]) == nativeID(ss) {
			found = true
			break
		}
	}
	if !found {
		return errors.New("审批已失效或不属于当前会话")
	}
	reply := "reject"
	if decision == "accept" {
		reply = "once"
	}
	if e := o.request(ctx, "POST", "/permission/"+url.PathEscape(key)+"/reply", ss.Project, map[string]any{"reply": reply}, nil); e != nil {
		return e
	}
	o.m.emitExternal(ss, store.Event{Kind: "approval", Item: key, Mode: "snapshot", Status: "resolved"})
	return nil
}
func (o *openCode) sync(ctx context.Context) error {
	var projects []map[string]any
	if e := o.request(ctx, "GET", "/project", "", nil, &projects); e != nil {
		return e
	}
	paths := map[string]bool{}
	for _, root := range o.m.Roots {
		if path, e := o.m.allowed(root); e == nil {
			paths[path] = true
		}
	}
	for _, p := range projects {
		if path, e := o.m.allowed(str(p["worktree"])); e == nil {
			paths[path] = true
		}
		for _, d := range arr(p["sandboxes"]) {
			if path, e := o.m.allowed(str(d)); e == nil {
				paths[path] = true
			}
		}
	}
	seen := map[string]bool{}
	for path := range paths {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var sessions []map[string]any
		if e := o.request(ctx, "GET", "/session?limit=100", path, nil, &sessions); e != nil {
			return e
		}
		for _, v := range sessions {
			ss, e := o.importSession(v)
			if e != nil {
				continue
			}
			if seen[ss.ID] {
				continue
			}
			seen[ss.ID] = true
			var messages []map[string]any
			if e = o.request(ctx, "GET", "/session/"+url.PathEscape(nativeID(ss))+"/message?limit=200", ss.Project, nil, &messages); e != nil {
				return e
			}
			for _, message := range messages {
				role := str(obj(message["info"])["role"])
				if role != "user" && role != "assistant" {
					continue
				}
				for _, v := range arr(message["parts"]) {
					o.part(ss, obj(v), role)
				}
			}
		}
		if e := o.syncQuestions(ctx, path); e != nil {
			return e
		}
	}
	return nil
}
func (o *openCode) part(ss store.Session, p map[string]any, role string) {
	ev := store.Event{Item: str(p["id"]), Mode: "snapshot", Role: role}
	if ev.Item == "" {
		return
	}
	switch str(p["type"]) {
	case "text":
		if p["synthetic"] == true || p["ignored"] == true {
			return
		}
		ev.Kind = "message"
		ev.Text = str(p["text"])
	case "reasoning":
		ev.Kind = "thinking"
		ev.Text = str(p["text"])
	case "tool":
		ev.Kind = "tool"
		st := obj(p["state"])
		ev.Text = str(p["tool"]) + "\n" + jsonText(st["input"]) + "\n" + str(st["output"])
		ev.Status = str(st["status"])
	case "file":
		if !strings.HasPrefix(str(p["mime"]), "image/") {
			return
		}
		src := str(p["url"])
		id, e := o.m.Normalizer.media(src, ss.Project)
		if e == nil {
			ev.Kind = "image"
			ev.Media = id
		} else if strings.HasPrefix(src, "https://") && Redact(src) == src {
			ev.Kind = "image"
			ev.Text = src
		} else {
			return
		}
	default:
		return
	}
	b, _ := json.Marshal(ev)
	ev.ID = "opencode/" + digest(ss.ID+string(b))
	o.m.emitExternal(ss, ev)
}
func (o *openCode) event(ctx context.Context, d map[string]any) {
	if p := obj(d["payload"]); str(p["type"]) != "" {
		d = p
	}
	p := obj(d["properties"])
	typ := str(d["type"])
	if typ == "session.deleted" {
		id := str(obj(p["info"])["id"])
		if id == "" {
			id = str(p["sessionID"])
		}
		var ss store.Session
		if o.m.Store.Get("sessions", sessionID("opencode", id), &ss) == nil && ss.Agent == "opencode" {
			ss.CatalogHidden = true
			_ = o.m.Store.PutCatalogSession(ss)
		}
		return
	}
	if typ == "session.created" || typ == "session.updated" {
		_, _ = o.importSession(obj(p["info"]))
		return
	}
	id := str(p["sessionID"])
	if id == "" {
		id = str(obj(p["part"])["sessionID"])
	}
	var ss store.Session
	if o.m.Store.Get("sessions", sessionID("opencode", id), &ss) != nil || ss.Agent != "opencode" {
		return
	}
	if _, e := o.m.allowed(ss.Project); e != nil {
		return
	}
	switch typ {
	case "message.part.updated":
		part := obj(p["part"])
		role := "assistant"
		var msg map[string]any
		if o.request(ctx, "GET", "/session/"+url.PathEscape(id)+"/message/"+url.PathEscape(str(part["messageID"])), ss.Project, nil, &msg) == nil {
			role = str(obj(msg["info"])["role"])
		}
		o.part(ss, part, role)
	case "message.part.delta": // Full part updates repair deltas after reconnect.
		if str(p["field"]) != "text" {
			return
		}
		kind := "message"
		h, _ := o.m.Store.History(ss.ID, 0, 40)
		for _, v := range h.Events {
			if v.Item == str(p["partID"]) && v.Kind == "thinking" {
				kind = "thinking"
			}
		}
		ev := store.Event{ID: str(d["id"]), Kind: kind, Role: "assistant", Text: str(p["delta"]), Item: str(p["partID"]), Mode: "delta"}
		o.m.emitExternal(ss, ev)
	case "session.status":
		status := "completed"
		t := str(obj(p["status"])["type"])
		// OpenCode emits idle after session.error as cleanup, not success.
		// Preserve the failure until a new busy turn proves execution resumed.
		if t == "idle" && ss.Status == "failed" {
			return
		}

		if t == "busy" || t == "retry" {
			status = "running"
		}
		issue := store.SessionIssue{}
		if t == "retry" {
			issue = nativeIssue("retrying")
		}
		if t != "idle" && t != "busy" && t != "retry" {
			status = "unknown"
			issue = nativeIssue("unsupported_state")
		}
		o.m.emitExternal(ss, store.Event{Kind: "status", Status: status, Issue: issue})
	case "session.error":
		o.m.emitExternal(ss, store.Event{Kind: "notice", Text: "OpenCode 执行失败，请检查工作站的模型或登录配置"})
		o.m.emitExternal(ss, store.Event{Kind: "status", Status: "failed", Issue: openCodeIssue(obj(p["error"]))})
	case "question.asked":
		o.questionEvent(ss, str(p["id"]), jsonText(openCodeQuestionParams(p)), "pending")
	case "question.replied", "question.rejected":
		o.questionEvent(ss, str(p["requestID"]), "", "resolved")
	case "permission.asked":
		o.m.emitExternal(ss, store.Event{Kind: "approval", Item: str(p["id"]), Text: jsonText(p), Mode: "snapshot", Status: "pending"})
	case "permission.replied":
		o.m.emitExternal(ss, store.Event{Kind: "approval", Item: str(p["requestID"]), Mode: "snapshot", Status: "resolved"})
	}
}
func (o *openCode) stream(ctx context.Context) error {
	r, e := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(o.cfg.URL, "/")+"/global/event", nil)
	if e != nil {
		return e
	}
	if o.cfg.PasswordEnv != "" {
		user := o.cfg.Username
		if user == "" {
			user = "opencode"
		}
		r.SetBasicAuth(user, os.Getenv(o.cfg.PasswordEnv))
	}
	h := *o.http
	h.Timeout = 0
	res, e := h.Do(r)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return errors.New("OpenCode 实时事件接口不可用")
	}
	if e := o.recoverQuestions(ctx); e != nil {
		return e
	}
	s := bufio.NewScanner(res.Body)
	s.Buffer(make([]byte, 4096), 8<<20)
	for s.Scan() {
		line := s.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var d map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(line[5:])), &d) == nil {
			o.event(ctx, d)
		}
	}
	return s.Err()
}
func (o *openCode) Run(ctx context.Context) {
	if o.cfg.URL == "" {
		return
	}
	for ctx.Err() == nil {
		e := o.sync(ctx)
		o.mu.Lock()
		o.connected = e == nil
		if e != nil {
			o.detail = e.Error()
		}
		o.mu.Unlock()
		if e == nil {
			_ = o.stream(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}
func (o *openCode) Close() {}
