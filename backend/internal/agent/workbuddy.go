package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"agentdeck/internal/secure"
	"agentdeck/internal/store"
)

type workBuddyToken struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	OpenID  string `json:"open_id"`
	Scope   string `json:"scope"`
	Expires int64  `json:"expires"`
}
type workBuddy struct {
	m       *Manager
	cfg     WorkBuddyConfig
	http    *http.Client
	mu      sync.Mutex
	tokenMu sync.Mutex
	flows   map[string]time.Time
	detail  string
	cursor  string
	account string
	wake    chan struct{}
}

func newWorkBuddy(m *Manager, c WorkBuddyConfig) *workBuddy {
	if c.API == "" {
		c.API = "https://www.workbuddy.cn/openapi/v2"
	}
	return &workBuddy{m: m, cfg: c, http: agentHTTP(), flows: map[string]time.Time{}, wake: make(chan struct{}, 1)}
}
func (w *workBuddy) Info() AgentInfo {
	w.mu.Lock()
	defer w.mu.Unlock()
	configured := w.cfg.ClientID != "" && w.cfg.RedirectURI != "" && os.Getenv(w.cfg.ClientSecretEnv) != ""
	d := "请配置开放平台应用并授权本地助理"
	if configured {
		d = "应用已配置 · 待用户授权"
	}
	var token workBuddyToken
	if w.m.Store.Get("agentconfig", "workbuddy/oauth", &token) == nil && token.Access != "" {
		d = "已授权 · 电脑本地助理 · 增量同步"
	}
	if w.detail != "" {
		d = w.detail
	}
	return AgentInfo{ID: "workbuddy", Name: "WorkBuddy", Configured: configured, Create: false, Send: true, Stop: false, Approve: false, Detail: d}
}
func (w *workBuddy) StartLogin() (map[string]any, error) {
	if w.cfg.ClientID == "" || w.cfg.RedirectURI == "" || os.Getenv(w.cfg.ClientSecretEnv) == "" {
		return nil, errors.New("请在工作站配置 WorkBuddy 开放平台应用、密钥环境变量及 HTTPS 回调地址")
	}
	u, e := url.Parse(w.cfg.RedirectURI)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/v1/agents/workbuddy/callback" {
		return nil, errors.New("WorkBuddy 回调须为 HTTPS /v1/agents/workbuddy/callback")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for key, t := range w.flows {
		if time.Since(t) > 10*time.Minute {
			delete(w.flows, key)
		}
	}
	if len(w.flows) >= 16 {
		return nil, errors.New("授权请求过多，请稍后重试")
	}
	state := secure.ID()
	w.flows[state] = time.Now()
	q := url.Values{"response_type": {"code"}, "client_id": {w.cfg.ClientID}, "redirect_uri": {w.cfg.RedirectURI}, "scope": {"user.localassistant.readable user.localassistant.invokable"}, "state": {state}}
	return map[string]any{"url": strings.TrimRight(w.cfg.API, "/") + "/authorize?" + q.Encode(), "expires_in": 600}, nil
}
func (w *workBuddy) tokenRequest(ctx context.Context, form url.Values) (workBuddyToken, error) {
	form.Set("client_id", w.cfg.ClientID)
	form.Set("client_secret", os.Getenv(w.cfg.ClientSecretEnv))
	r, e := http.NewRequestWithContext(nonNil(ctx), "POST", strings.TrimRight(w.cfg.API, "/")+"/token", strings.NewReader(form.Encode()))
	if e != nil {
		return workBuddyToken{}, e
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, e := w.http.Do(r)
	if e != nil {
		return workBuddyToken{}, errors.New("WorkBuddy 授权服务未连接")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return workBuddyToken{}, errors.New("WorkBuddy 授权失败，请检查应用与授权范围")
	}
	var v struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		OpenID  string `json:"open_id"`
		Scope   string `json:"scope"`
		Expires int64  `json:"expires_in"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&v) != nil || v.Access == "" || v.Expires <= 0 {
		return workBuddyToken{}, errors.New("WorkBuddy 授权响应无效")
	}
	return workBuddyToken{Access: v.Access, Refresh: v.Refresh, OpenID: v.OpenID, Scope: v.Scope, Expires: time.Now().Unix() + v.Expires}, nil
}
func (w *workBuddy) CompleteLogin(ctx context.Context, state, code string) error {
	w.mu.Lock()
	t, ok := w.flows[state]
	delete(w.flows, state)
	w.mu.Unlock()
	if !ok || time.Since(t) > 10*time.Minute || len(code) == 0 || len(code) > 4096 {
		return errors.New("授权状态无效、过期或已使用")
	}
	w.tokenMu.Lock()
	defer w.tokenMu.Unlock()
	token, e := w.tokenRequest(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {w.cfg.RedirectURI}})
	if e != nil {
		return e
	}
	if token.OpenID == "" || !strings.Contains(" "+token.Scope+" ", " user.localassistant.readable ") || !strings.Contains(" "+token.Scope+" ", " user.localassistant.invokable ") {
		return errors.New("WorkBuddy 未授予本地助理读写权限")
	}
	if e = w.m.Store.Put("agentconfig", "workbuddy/oauth", token); e != nil {
		return e
	}
	w.mu.Lock()
	w.detail = ""
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return nil
}
func (w *workBuddy) access(ctx context.Context) (workBuddyToken, error) {
	w.tokenMu.Lock()
	defer w.tokenMu.Unlock()
	var token workBuddyToken
	if w.m.Store.Get("agentconfig", "workbuddy/oauth", &token) != nil || token.Access == "" {
		return token, errors.New("请先授权 WorkBuddy 本地助理")
	}
	if time.Now().Unix() < token.Expires-60 {
		return token, nil
	}
	if token.Refresh == "" {
		return token, errors.New("WorkBuddy 授权已到期，请重新授权")
	}
	fresh, e := w.tokenRequest(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {token.Refresh}})
	if e != nil {
		return token, e
	}
	if fresh.OpenID == "" {
		fresh.OpenID = token.OpenID
	}
	if fresh.OpenID != token.OpenID {
		return token, errors.New("WorkBuddy 授权账号已变更，请重新授权")
	}
	if fresh.Refresh == "" {
		fresh.Refresh = token.Refresh
	}
	if fresh.Scope == "" {
		fresh.Scope = token.Scope
	}
	return fresh, w.m.Store.Put("agentconfig", "workbuddy/oauth", fresh)
}
func (w *workBuddy) request(ctx context.Context, method, path string, in any) (map[string]any, workBuddyToken, error) {
	token, e := w.access(ctx)
	if e != nil {
		return nil, token, e
	}
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	r, e := http.NewRequestWithContext(nonNil(ctx), method, strings.TrimRight(w.cfg.API, "/")+path, body)
	if e != nil {
		return nil, token, e
	}
	r.Header.Set("Authorization", "Bearer "+token.Access)
	r.Header.Set("Content-Type", "application/json")
	res, e := w.http.Do(r)
	if e != nil {
		return nil, token, errors.New("WorkBuddy 未确认接收，请检查电脑后再操作，避免重复发送")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, token, fmt.Errorf("WorkBuddy 请求失败（HTTP %d），请检查授权或电脑在线状态", res.StatusCode)
	}
	var v map[string]any
	if json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&v) != nil {
		return nil, token, errors.New("WorkBuddy 响应格式不支持")
	}
	if code, ok := v["code"].(float64); ok && code != 0 {
		return nil, token, errors.New("WorkBuddy 未完成请求，请检查电脑在线状态或授权范围")
	}
	if data, ok := v["data"].(map[string]any); ok {
		v = data
	}
	return v, token, nil
}
func workBuddySession(t workBuddyToken) store.Session {
	s := newExternalSession("local-"+digest(t.OpenID)[:24], "workbuddy", "", "workbuddy-local")
	s.Title = "本地助理"
	return s
}
func (w *workBuddy) Create(context.Context, string, string) (store.Session, error) {
	return store.Session{}, errors.New("WorkBuddy 官方本地接口对应账号的一个本地助理，请打开已有本地助理会话")
}
func (w *workBuddy) Send(ctx context.Context, s store.Session, text, request string) error {
	online, token, e := w.request(ctx, "GET", "/localassistant", nil)
	if e != nil {
		return e
	}
	if workBuddySession(token).ID != s.ID {
		return errors.New("该会话不属于当前授权的 WorkBuddy 账号")
	}
	if online["online"] != true {
		return errors.New("电脑 WorkBuddy 本地助理离线")
	}
	v, _, e := w.request(ctx, "POST", "/localassistant/message", map[string]any{"content": text, "msg_type": "text"})
	if e != nil {
		return e
	}
	id := str(v["message_id"])
	if id == "" {
		return errors.New("WorkBuddy 未确认消息 ID，请检查电脑后再发送")
	}
	w.m.emitExternal(s, store.Event{ID: "workbuddy-input/" + id, Item: id, Kind: "message", Role: "user", Mode: "snapshot", Text: text})
	return nil
}
func (w *workBuddy) Interrupt(context.Context, store.Session) error {
	return errors.New("WorkBuddy 本地助理接口未提供停止操作，请在电脑处理")
}
func (w *workBuddy) Approve(context.Context, store.Session, string, string, string) error {
	return errors.New("WorkBuddy 本地助理的工具审批请在电脑处理")
}
func (w *workBuddy) sync(ctx context.Context) error {
	status, token, e := w.request(ctx, "GET", "/localassistant", nil)
	if e != nil {
		return e
	}
	if status["online"] != true {
		return errors.New("电脑 WorkBuddy 本地助理离线")
	}
	s := workBuddySession(token)
	_ = w.m.Store.Get("sessions", s.ID, &s)
	if e = w.m.Store.Put("sessions", s.ID, s); e != nil {
		return e
	}
	if w.account != token.OpenID {
		w.cursor = ""
		w.account = token.OpenID
		_ = w.m.Store.Get("agentconfig", "workbuddy/cursor/"+digest(token.OpenID), &w.cursor)
	}
	messages := []any{}
	if w.cursor != "" {
		v, current, err := w.request(ctx, "GET", "/localassistant/message?message_id="+url.QueryEscape(w.cursor), nil)
		if err != nil {
			return err
		}
		if current.OpenID != token.OpenID {
			return errors.New("授权账号已变化，请重新连接")
		}
		messages = arr(v["messages"])
	} else {
		for page := 0; page < 100; page++ {
			v, current, err := w.request(ctx, "GET", fmt.Sprintf("/localassistant/message?limit=100&offset=%d", page*100), nil)
			if err != nil {
				return err
			}
			if current.OpenID != token.OpenID {
				return errors.New("授权账号已变化，请重新连接")
			}
			rows := arr(v["messages"])
			messages = append(messages, rows...)
			if len(rows) < 100 {
				break
			}
			if page == 99 {
				return errors.New("WorkBuddy 历史超过首次同步上限，请联系管理员")
			}
		}
	}
	// Pagination ordering is provider-defined; preserve chronological presentation.
	sort.SliceStable(messages, func(i, j int) bool { return str(obj(messages[i])["created_at"]) < str(obj(messages[j])["created_at"]) })
	for _, raw := range messages {
		p := obj(raw)
		id := str(p["message_id"])
		role := str(p["role"])
		if id == "" || role != "user" && role != "assistant" {
			continue
		}
		text := []string{}
		for i, c := range arr(p["content"]) {
			if str(c) != "" {
				text = append(text, str(c))
				continue
			}
			b := obj(c)
			if str(b["type"]) == "text" {
				text = append(text, str(b["text"]))
			}
			if str(b["type"]) == "image" || str(b["type"]) == "image_url" {
				w.image(s, b, id, i)
			}
		}
		if len(text) > 0 {
			value := strings.Join(text, "\n")
			w.m.emitExternal(s, store.Event{ID: "workbuddy-msg/" + digest(s.ID+id+value), Item: id, Kind: "message", Role: role, Mode: "snapshot", Text: value, Time: str(p["created_at"])})
		}
		for i, b := range arr(p["attachments"]) {
			w.image(s, obj(b), id, i)
		}
		w.cursor = id
	}
	return w.m.Store.Put("agentconfig", "workbuddy/cursor/"+digest(token.OpenID), w.cursor)
}
func (w *workBuddy) image(s store.Session, b map[string]any, id string, index int) {
	src := str(b["url"])
	if src == "" {
		src = str(obj(b["image_url"])["url"])
	}
	mime := str(b["mime_type"])
	if mime == "" {
		mime = str(b["mimeType"])
	}
	if !strings.HasPrefix(mime, "image/") && str(b["type"]) != "image" && str(b["type"]) != "image_url" {
		return
	}
	media, e := w.m.Normalizer.media(src, "")
	item := fmt.Sprintf("%s/image/%d", id, index)
	ev := store.Event{ID: "workbuddy-image/" + digest(s.ID+item+src), Kind: "image", Item: item}
	if e == nil {
		ev.Media = media
	} else if strings.HasPrefix(src, "https://") && Redact(src) == src {
		ev.Text = src
	} else {
		return
	}
	w.m.emitExternal(s, ev)
}
func (w *workBuddy) Run(ctx context.Context) {
	delay := 2 * time.Second
	for ctx.Err() == nil {
		e := w.sync(ctx)
		w.mu.Lock()
		if e != nil {
			w.detail = e.Error()
		} else {
			w.detail = "已连接 · 本地助理 · 增量同步"
		}
		w.mu.Unlock()
		if e != nil {
			delay *= 2
			if delay > time.Minute {
				delay = time.Minute
			}
		} else {
			delay = 2 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
			delay = 2 * time.Second
		case <-time.After(delay):
		}
	}
}
func (w *workBuddy) Close() {}
func (m *Manager) AgentLogin(id string) (map[string]any, error) {
	w, ok := m.Connectors[id].(*workBuddy)
	if !ok {
		return nil, errors.New("此 Agent 使用工作站 CLI 登录")
	}
	return w.StartLogin()
}
func (m *Manager) WorkBuddyCallback(ctx context.Context, state, code string) error {
	w, ok := m.Connectors["workbuddy"].(*workBuddy)
	if !ok {
		return errors.New("WorkBuddy 未配置")
	}
	return w.CompleteLogin(ctx, state, code)
}
func (m *Manager) Hydrate(id string) error {
	var s store.Session
	if m.Store.Get("sessions", id, &s) != nil {
		return nil
	}
	if s.Source != "acp" {
		return nil
	}
	if _, e := m.allowed(s.Project); e != nil {
		return e
	}
	c, ok := m.Connectors[s.Agent].(*acpConnector)
	if !ok {
		return errors.New("ACP Agent 未配置")
	}
	return c.Hydrate(m.Context, s)
}
