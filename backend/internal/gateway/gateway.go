// Package gateway owns tenant/routing authorization, not agent sessions or
// Local/App roots. All relayed content remains end-to-end ciphertext.
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"agentdeck/internal/relay"
	"agentdeck/internal/secure"
	"agentdeck/internal/store"
	"github.com/gorilla/websocket"
)

type Node struct {
	ID      string `json:"id"`
	Tenant  string `json:"tenant"`
	Name    string `json:"name"`
	Key     string `json:"key"`
	Created string `json:"created"`
	Owner   string `json:"owner,omitempty"`
}
type Server struct {
	Store              *store.Store
	URL, Pin, TLSTrust string
	Rotation           time.Duration
	Clock              *secure.Clock
	mu                 sync.Mutex
	peers              map[string]*relay.Peer
	active             map[string]int
	replay             relay.Replay
	window             time.Time
	attempts           int
	Feishu             *Feishu
}

func New(db *store.Store, url, pin, trust string, rotation time.Duration) *Server {
	return NewWithClock(db, url, pin, trust, rotation, secure.NewClock())
}

// NewWithClock supports an authoritative clock dependency. Keep the instance
// immutable for this Gateway lifetime; live connections capture its anchor.
func NewWithClock(db *store.Store, url, pin, trust string, rotation time.Duration, clock *secure.Clock) *Server {
	if clock == nil {
		clock = secure.NewClock()
	}
	return &Server{Clock: clock, replay: relay.Replay{Clock: clock}, Store: db, URL: url, Pin: pin, TLSTrust: trust, Rotation: rotation, peers: map[string]*relay.Peer{}, active: map[string]int{}}
}
func write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int) { http.Error(w, http.StatusText(code), code) }
func (g *Server) limit() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if now.Sub(g.window) > time.Minute {
		g.window = now
		g.attempts = 0
	}
	g.attempts++
	return g.attempts <= 2048
}
func (g *Server) AddLocal(tenant, name string) (relay.Link, error) {
	var l relay.Link
	if !relay.ValidID(tenant) || len(name) > 80 {
		return l, errors.New("invalid tenant/name")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	tenants, e := g.Store.Values("gwtenants")
	if e != nil {
		return l, e
	}
	var record struct {
		ID string `json:"id"`
	}
	if g.Store.Get("gwtenants", tenant, &record) != nil {
		if len(tenants) >= 128 {
			return l, errors.New("tenant capacity reached")
		}
		if e = g.Store.Put("gwtenants", tenant, map[string]string{"id": tenant}); e != nil {
			return l, e
		}
	}
	nodes, e := g.Store.Values("gwnodes")
	if e != nil {
		return l, e
	}
	count := 0
	for _, raw := range nodes {
		var n Node
		_ = json.Unmarshal(raw, &n)
		if n.Tenant == tenant {
			count++
		}
	}
	if count >= 16 {
		return l, errors.New("tenant local capacity reached")
	}
	n := Node{ID: secure.ID(), Tenant: tenant, Name: name, Key: secure.B64.EncodeToString(secure.Random(32)), Created: time.Now().UTC().Format(time.RFC3339)}
	if e = g.Store.Put("gwnodes", n.ID, n); e != nil {
		return l, e
	}
	l = relay.Link{Version: 1, URL: g.URL, Pin: g.Pin, TLSTrust: g.TLSTrust, Tenant: tenant, Local: n.ID, Key: n.Key}
	return l, nil
}
func (g *Server) RevokeLocal(id string) error {
	g.mu.Lock()
	p := g.peers[id]
	delete(g.peers, id)
	err := g.Store.Delete("gwnodes", id)
	g.mu.Unlock()
	if p != nil {
		p.Close()
	}
	return err
}
func (g *Server) RevokeGrant(id string) error {
	g.mu.Lock()
	var grant relay.Grant
	_ = g.Store.Get("gwgrants", id, &grant)
	p := g.peers[grant.Local]
	e := g.Store.Delete("gwgrants", id)
	g.mu.Unlock()
	if p != nil {
		p.CloseGrant(id)
	}
	return e
}

// RevokeAccount removes the login and every routing grant it issued, including
// active streams. Other phones and their independently paired roots stay valid.
func (g *Server) RevokeAccount(id string) error {
	g.mu.Lock()
	err := g.Store.Delete("gwaccounts", id)
	rows, readErr := g.Store.Values("gwgrants")
	var closed []struct {
		peer  *relay.Peer
		grant string
	}
	for _, row := range rows {
		var grant relay.Grant
		if json.Unmarshal(row, &grant) == nil && grant.Account == id {
			if e := g.Store.Delete("gwgrants", grant.ID); e != nil {
				err = e
			}
			closed = append(closed, struct {
				peer  *relay.Peer
				grant string
			}{g.peers[grant.Local], grant.ID})
		}
	}
	g.mu.Unlock()
	for _, item := range closed {
		if item.peer != nil {
			item.peer.CloseGrant(item.grant)
		}
	}
	if readErr != nil {
		return readErr
	}
	return err
}
func (g *Server) Close() {
	g.mu.Lock()
	list := []*relay.Peer{}
	for _, p := range g.peers {
		if p != nil {
			list = append(list, p)
		}
	}
	g.mu.Unlock()
	for _, p := range list {
		p.Close()
	}
}
func (g *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"service": "agentdeck-gateway", "version": 2})
	})
	m.HandleFunc("GET /v2/local/tunnel", g.tunnel)
	m.HandleFunc("POST /v2/local/grants", g.register)
	m.HandleFunc("POST /v2/time-sync", g.timeSync)
	m.HandleFunc("/v2/relay/", g.proxy)
	g.authRoutes(m)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Header.Get("Origin") != "" {
			fail(w, 403)
			return
		}
		if !g.limit() {
			fail(w, 429)
			return
		}
		m.ServeHTTP(w, r)
	})
}
func (g *Server) localAuth(r *http.Request, body []byte) (Node, bool) {
	id := strings.TrimPrefix(r.Header.Get("Authorization"), "Local ")
	var n Node
	if !relay.ValidID(id) || g.Store.Get("gwnodes", id, &n) != nil {
		return n, false
	}
	key, e := secure.B64.DecodeString(n.Key)
	return n, e == nil && g.replay.Verify(key, "local/"+id, r, body, "")
}
func (g *Server) tunnel(w http.ResponseWriter, r *http.Request) {
	n, ok := g.localAuth(r, nil)
	if !ok {
		fail(w, 401)
		return
	}
	g.mu.Lock()
	if _, busy := g.peers[n.ID]; busy {
		g.mu.Unlock()
		fail(w, 409)
		return
	}
	// Reserve this Local identity before upgrade to reject concurrent sessions.
	g.peers[n.ID] = nil
	g.mu.Unlock()
	defer func() { g.mu.Lock(); delete(g.peers, n.ID); g.mu.Unlock() }()
	up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }}
	ws, e := up.Upgrade(w, r, nil)
	if e != nil {
		return
	}
	defer ws.Close()
	key, _ := secure.B64.DecodeString(n.Key)
	salt := secure.Random(32)
	box, e := secure.SealJSON(key, "tunnel-welcome/"+n.ID, 0, 0, map[string]any{"salt": secure.B64.EncodeToString(salt), "rotation_ms": g.Rotation.Milliseconds(), "server_time_ms": g.Clock.Millis(), "challenge": relay.Hash([]byte(r.Header.Get("X-Relay-Auth")))})
	if e != nil {
		return
	}
	_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if ws.WriteJSON(box) != nil {
		return
	}
	p := relay.NewPeer(ws, key, salt, "g2l", g.Rotation)
	p.Clock = g.Clock
	g.mu.Lock()
	var current Node
	if g.Store.Get("gwnodes", n.ID, &current) != nil {
		g.mu.Unlock()
		return
	}
	g.peers[n.ID] = p
	g.mu.Unlock()
	log.Printf("S-GATEWAY local online tenant=%s local=%s", n.Tenant, n.ID)
	_ = p.Run()
	log.Printf("S-GATEWAY local offline tenant=%s local=%s", n.Tenant, n.ID)
}
func (g *Server) register(w http.ResponseWriter, r *http.Request) {
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
	if e != nil {
		fail(w, 400)
		return
	}
	n, ok := g.localAuth(r, raw)
	if !ok {
		fail(w, 401)
		return
	}
	key, _ := secure.B64.DecodeString(n.Key)
	var box secure.Box
	if json.Unmarshal(raw, &box) != nil {
		fail(w, 400)
		return
	}
	plain, e := secure.Open(key, "grant-request/"+n.ID, box)
	var req struct {
		Invite string `json:"invite"`
		TTL    int64  `json:"ttl_seconds"`
	}
	if e != nil || json.Unmarshal(plain, &req) != nil || len(req.Invite) != 64 || req.TTL <= 0 || req.TTL > 300 {
		fail(w, 400)
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	rows, e := g.Store.Values("gwgrants")
	if e != nil {
		fail(w, 500)
		return
	}
	count := 0
	for _, raw := range rows {
		var gr relay.Grant
		_ = json.Unmarshal(raw, &gr)
		if gr.Device == "" && gr.Expires < g.Clock.Unix() {
			_ = g.Store.Delete("gwgrants", gr.ID)
			continue
		}
		if gr.Local == n.ID {
			count++
		}
	}
	if count >= 32 {
		fail(w, 429)
		return
	}
	grant := relay.Grant{ID: secure.ID(), Key: secure.B64.EncodeToString(secure.Random(32)), Tenant: n.Tenant, Local: n.ID, Invite: req.Invite, Expires: g.Clock.Unix() + req.TTL}
	if g.Store.Put("gwgrants", grant.ID, grant) != nil {
		fail(w, 500)
		return
	}
	answer, e := secure.SealJSON(key, "grant-response/"+n.ID, 0, 0, map[string]any{"grant": grant, "challenge": relay.Hash(raw)})
	if e != nil {
		fail(w, 500)
		return
	}
	write(w, answer)
}

type routeContext struct{}
type route struct {
	grant relay.Grant
	peer  *relay.Peer
}

func (g *Server) proxy(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "v2" || parts[1] != "relay" || !relay.ValidID(parts[2]) || r.URL.RawQuery != "" {
		fail(w, 404)
		return
	}
	operation := parts[3]
	if operation != "pair" && operation != "session" && operation != "stream" && operation != "offer" && operation != "time-sync" {
		fail(w, 404)
		return
	}
	if operation == "stream" && r.Method != "GET" || operation != "stream" && r.Method != "POST" {
		fail(w, 405)
		return
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
	if e != nil {
		fail(w, 400)
		return
	}
	id := strings.TrimPrefix(r.Header.Get("Authorization"), "Relay ")
	var grant relay.Grant
	if !relay.ValidID(id) || g.Store.Get("gwgrants", id, &grant) != nil || grant.Local != parts[2] {
		fail(w, 401)
		return
	}
	var node Node
	if g.Store.Get("gwnodes", grant.Local, &node) != nil || node.Tenant != grant.Tenant {
		fail(w, 401)
		return
	}
	key, e := secure.B64.DecodeString(grant.Key)
	localAuth := r.Header.Get("X-Local-Authorization")
	if e != nil || !g.replay.Verify(key, "grant/"+id, r, raw, localAuth) {
		fail(w, 401)
		return
	}
	if operation == "pair" || operation == "offer" {
		var req struct {
			Invite string `json:"invite"`
		}
		if grant.Device != "" || grant.Expires < g.Clock.Unix() || (operation == "pair" && (json.Unmarshal(raw, &req) != nil || req.Invite != grant.Invite)) {
			fail(w, 401)
			return
		}
	} else {
		if grant.Device == "" {
			fail(w, 401)
			return
		}
		if operation == "session" || operation == "time-sync" {
			var req struct {
				Device string `json:"device"`
				ID     string `json:"id"`
			}
			if json.Unmarshal(raw, &req) != nil {
				fail(w, 401)
				return
			}
			if operation == "time-sync" {
				req.Device = req.ID
			}
			if req.Device != grant.Device {
				fail(w, 401)
				return
			}
		}
	}
	g.mu.Lock()
	p := g.peers[grant.Local]
	if p == nil || g.active[grant.Tenant] >= 64 {
		g.mu.Unlock()
		fail(w, 503)
		return
	}
	g.active[grant.Tenant]++
	g.mu.Unlock()
	defer func() { g.mu.Lock(); g.active[grant.Tenant]--; g.mu.Unlock() }()
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
	r = r.WithContext(context.WithValue(r.Context(), routeContext{}, route{grant, p}))
	target, _ := url.Parse("http://local")
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		req.URL.Path = "/v1/" + operation
		req.URL.RawPath = ""
		req.Host = "local"
		req.Header.Del("X-Relay-Auth")
		req.Header.Del("X-Local-Authorization")
		req.Header.Del("X-Forwarded-For")
		req.Header.Del("X-Forwarded-Host")
		req.Header.Del("X-Forwarded-Proto")
		req.Header.Set("Authorization", localAuth)
	}
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		rt := ctx.Value(routeContext{}).(route)
		g.mu.Lock()
		defer g.mu.Unlock()
		var current relay.Grant
		if g.Store.Get("gwgrants", rt.grant.ID, &current) != nil || g.peers[current.Local] != rt.peer {
			return nil, errors.New("routing grant revoked or tunnel changed")
		}
		return rt.peer.Open(rt.grant.ID)
	}}
	defer transport.CloseIdleConnections()
	proxy.Transport = transport
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) { fail(w, 502) }
	proxy.ModifyResponse = func(res *http.Response) error {
		if operation == "offer" && res.StatusCode == 200 {
			data, err := io.ReadAll(io.LimitReader(res.Body, 16<<10))
			res.Body.Close()
			var offer struct {
				Invite string `json:"invite"`
			}
			if err != nil || json.Unmarshal(data, &offer) != nil || len(offer.Invite) != 64 {
				return errors.New("invalid pairing offer")
			}
			g.mu.Lock()
			var saved relay.Grant
			if g.Store.Get("gwgrants", grant.ID, &saved) != nil || saved.Device != "" {
				g.mu.Unlock()
				return errors.New("binding already consumed")
			}
			saved.Invite = offer.Invite
			err = g.Store.Put("gwgrants", grant.ID, saved)
			g.mu.Unlock()
			if err != nil {
				return err
			}
			res.Body = io.NopCloser(bytes.NewReader(data))
			res.ContentLength = int64(len(data))
		}

		if operation == "pair" && res.StatusCode == 200 {
			device := res.Header.Get("X-AgentDeck-Bound-Device")
			if !relay.ValidID(device) {
				return errors.New("missing local device binding")
			}
			g.mu.Lock()
			defer g.mu.Unlock()
			var saved relay.Grant
			if g.Store.Get("gwgrants", grant.ID, &saved) != nil || saved.Device != "" {
				return errors.New("binding already consumed")
			}
			saved.Device = device
			if g.Store.Put("gwgrants", grant.ID, saved) != nil {
				return errors.New("binding persistence failed")
			}
		}
		res.Header.Del("X-AgentDeck-Bound-Device")
		return nil
	}
	proxy.ServeHTTP(w, r)
}
