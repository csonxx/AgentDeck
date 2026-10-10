package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"agentdeck/internal/relay"
	"agentdeck/internal/secure"
)

// Association is controlled only through the owner Unix socket, never through
// the public or relayed phone handler. Its credentials stay in the encrypted store.
type Association struct {
	mu                             sync.Mutex
	ctx                            context.Context
	s                              *Server
	client                         *relay.Client
	phase, loginURL, problem, name string
	cancel                         context.CancelFunc
	wg                             sync.WaitGroup
	// Tests inject a client trusting their fixture CA; production always uses
	// system roots and never disables certificate verification.
	loginHTTP *http.Client
}

type AssociationStatus struct {
	Mode       string                  `json:"mode"`
	Linked     bool                    `json:"linked"`
	Online     bool                    `json:"online"`
	URL        string                  `json:"url"`
	Name       string                  `json:"name"`
	State      string                  `json:"state"`
	LoginURL   string                  `json:"login_url,omitempty"`
	Error      string                  `json:"error,omitempty"`
	Connection *relay.ConnectionStatus `json:"connection,omitempty"`
}

func (s *Server) EnableRelay(ctx context.Context) error {
	a := &Association{ctx: ctx, s: s, phase: "unlinked"}
	s.Association = a
	s.RelayClock = func() *secure.Clock {
		if c := a.current(); c != nil {
			return c.CurrentClock()
		}
		return nil
	}
	s.RelayBundle = func(b Bundle) (Bundle, error) {
		c := a.current()
		if c == nil || !c.Online() {
			return b, errors.New("gateway workstation is not online")
		}
		token, e := secure.B64.DecodeString(b.Token)
		if e != nil {
			return b, e
		}
		ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
		defer cancel()
		grant, e := c.Register(ctx, relay.Hash(token), b.Expires)
		if e != nil {
			return b, e
		}
		b.URL, b.Pin, b.TLSTrust = c.Link.URL, c.Link.Pin, c.Link.TLSTrust
		b.Expires = grant.Expires
		b.Relay = &relay.Binding{Tenant: c.Link.Tenant, Local: c.Link.Local, Grant: grant.ID, Key: grant.Key}
		return b, nil
	}
	var link relay.Link
	err := s.Store.Get("relayconfig", "gateway", &link)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := link.Validate(); err != nil {
		return err
	}
	_ = s.Store.Get("relayconfig", "name", &a.name)
	return a.attach(link)
}

func (a *Association) current() *relay.Client { a.mu.Lock(); defer a.mu.Unlock(); return a.client }
func (a *Association) attach(link relay.Link) error {
	c, e := relay.NewClient(link)
	if e != nil {
		return e
	}
	a.mu.Lock()
	if a.client != nil {
		a.mu.Unlock()
		return errors.New("workstation already linked")
	}
	a.client, a.phase, a.loginURL, a.problem = c, "connecting", "", ""
	a.wg.Add(1)
	a.mu.Unlock()
	go func() { defer a.wg.Done(); c.Run(a.ctx, a.s.Handler()) }()
	return nil
}

func (a *Association) Status() AssociationStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := AssociationStatus{Mode: "relay", Name: a.name, State: a.phase, LoginURL: a.loginURL, Error: a.problem}
	if a.client != nil {
		v.Linked, v.URL, v.Online = true, a.client.Link.URL, a.client.Online()
		status := a.client.Status()
		v.Connection = &status
		if v.Online {
			v.State = "online"
		} else {
			v.State = "reconnecting"
		}
	}
	return v
}

func (a *Association) Begin(url, name string) error {
	url = strings.TrimSpace(url)
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return errors.New("请输入工作站名称，最多 80 字节")
	}
	check := relay.Link{Version: 1, URL: url, TLSTrust: "system", Tenant: "pending", Local: "pending", Key: strings.Repeat("A", 43) + "="}
	if check.Validate() != nil {
		return errors.New("请输入 HTTPS 网关地址，可含 /agentdeck 前缀，不含末尾斜杠")
	}
	a.mu.Lock()
	if a.ctx.Err() != nil {
		a.mu.Unlock()
		return errors.New("后端正在关闭")
	}
	if a.client != nil || a.cancel != nil {
		a.mu.Unlock()
		return errors.New("已关联或正在授权，请勿重复发起")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Minute)
	a.cancel, a.phase, a.problem, a.name = cancel, "preparing", "", name
	a.wg.Add(1)
	a.mu.Unlock()
	go func() {
		defer a.wg.Done()
		defer cancel()
		err := a.login(ctx, url, name)
		a.mu.Lock()
		defer a.mu.Unlock()
		a.cancel = nil
		if err != nil && a.client == nil {
			a.phase, a.loginURL, a.problem = "failed", "", associationError(err)
		}
	}()
	return nil
}

func associationError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "授权已超时，请重新关联"
	}
	if errors.Is(err, context.Canceled) {
		return "授权已取消，可重新关联"
	}
	// Relay errors are deliberately bounded messages, but never forward a URL,
	// provider body or arbitrary transport error into the owner page.
	switch err.Error() {
	case "gateway Feishu login not configured or unavailable":
		return "网关飞书登录服务不可用，请检查网关配置"
	case "Feishu authorization failed":
		return "飞书授权失败，请重新关联"
	case "Feishu login expired":
		return "飞书授权已过期，请重新关联"
	case "invalid Feishu local enrollment":
		return "关联结果校验失败，请检查网关地址和版本"
	case "save enrollment failed":
		return "本机保存关联失败，请检查数据目录"
	default:
		return "无法完成网关关联，请检查网络、证书及网关日志后重试"
	}
}

func (a *Association) login(ctx context.Context, url, name string) error {
	tls, e := relay.TLSConfig("system", "")
	if e != nil {
		return e
	}
	h := &http.Client{Transport: &http.Transport{TLSClientConfig: tls}, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if a.loginHTTP != nil {
		h = a.loginHTTP
	}
	defer h.CloseIdleConnections()
	f, e := relay.BeginLogin(ctx, h, url, "local", name)
	if e != nil {
		return e
	}
	a.mu.Lock()
	a.phase, a.loginURL = "authorizing", f.URL
	a.mu.Unlock()
	for {
		raw, ready, e := f.Poll(ctx)
		if e != nil {
			return e
		}
		if ready {
			var v struct {
				Link     relay.Link `json:"link"`
				Identity struct {
					Tenant string `json:"tenant"`
				} `json:"identity"`
			}
			if json.Unmarshal(raw, &v) != nil || v.Link.Validate() != nil || v.Link.URL != url || v.Link.Tenant != v.Identity.Tenant {
				return errors.New("invalid Feishu local enrollment")
			}
			v.Link.Pin, v.Link.TLSTrust = "", "system"
			if a.s.Store.Put("relayconfig", "gateway", v.Link) != nil {
				return errors.New("save enrollment failed")
			}
			_ = a.s.Store.Put("relayconfig", "name", name)
			return a.attach(v.Link)
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (a *Association) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}
func (a *Association) Wait() { a.wg.Wait() }

func (s *Server) associationRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /gateway/status", func(w http.ResponseWriter, r *http.Request) {
		if s.Association == nil {
			write(w, AssociationStatus{Mode: "direct", State: "direct"})
			return
		}
		write(w, s.Association.Status())
	})
	m.HandleFunc("POST /gateway/login", func(w http.ResponseWriter, r *http.Request) {
		if s.Association == nil {
			fail(w, 409)
			return
		}
		var req struct {
			URL  string `json:"url"`
			Name string `json:"name"`
		}
		if decode(w, r, &req) != nil {
			fail(w, 400)
			return
		}
		if e := s.Association.Begin(req.URL, req.Name); e != nil {
			w.WriteHeader(400)
			write(w, map[string]string{"error": e.Error()})
			return
		}
		write(w, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /gateway/cancel", func(w http.ResponseWriter, r *http.Request) {
		if s.Association != nil {
			s.Association.Cancel()
		}
		write(w, map[string]bool{"ok": true})
	})
}
