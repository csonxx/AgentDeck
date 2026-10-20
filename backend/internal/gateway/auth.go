package gateway

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"

	"agentdeck/internal/relay"
	"agentdeck/internal/secure"
	"github.com/skip2/go-qrcode"
)

type Flow struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Name       string          `json:"name"`
	Key        string          `json:"key"`
	State      string          `json:"state"`
	Verifier   string          `json:"verifier"`
	CookieHash string          `json:"cookie_hash"`
	Expires    int64           `json:"expires"`
	Used       bool            `json:"used"`
	Status     string          `json:"status"`
	Result     json.RawMessage `json:"result,omitempty"`
}
type Account struct {
	ID       string   `json:"id"`
	Key      string   `json:"key"`
	Identity Identity `json:"identity"`
	Expires  int64    `json:"expires"`
}

func (g *Server) authRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /v2/auth/flows", g.beginFlow)
	m.HandleFunc("POST /v2/auth/poll", g.pollFlow)
	m.HandleFunc("GET /v2/auth/login", g.loginPage)
	m.HandleFunc("GET /v2/auth/authorize", g.authorize)
	m.HandleFunc("GET /v2/auth/callback", g.callback)
	m.HandleFunc("GET /v2/auth/done", func(w http.ResponseWriter, r *http.Request) {
		pageHeaders(w)
		fmt.Fprint(w, "<!doctype html><meta charset=utf-8><title>AgentDeck</title><p>飞书授权已完成，请返回 AgentDeck。首次配对还需核对双方校验码。</p>")
	})
	m.HandleFunc("POST /v2/account/locals", g.accountLocals)
	m.HandleFunc("POST /v2/account/bind", g.accountBind)
}
func (g *Server) beginFlow(w http.ResponseWriter, r *http.Request) {
	if g.Feishu == nil {
		reject(w, r, 503, "feishu_not_configured")
		return
	}
	var req struct {
		Kind   string `json:"kind"`
		Name   string `json:"name"`
		Public string `json:"public"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req) != nil || (req.Kind != "local" && req.Kind != "app") || len(req.Name) > 80 {
		fail(w, 400)
		return
	}
	pub, e := secure.B64.DecodeString(req.Public)
	if e != nil || len(pub) != 32 {
		fail(w, 400)
		return
	}
	remote, e := ecdh.X25519().NewPublicKey(pub)
	if e != nil {
		fail(w, 400)
		return
	}
	p, e := ecdh.X25519().GenerateKey(rand.Reader)
	if e != nil {
		fail(w, 500)
		return
	}
	shared, e := p.ECDH(remote)
	if e != nil {
		fail(w, 400)
		return
	}
	id := secure.ID()
	salt := secure.Random(32)
	key := secure.Derive(shared, salt, "agentdeck/auth-flow/v1/"+id)
	flow := Flow{ID: id, Kind: req.Kind, Name: req.Name, Key: secure.B64.EncodeToString(key), State: secure.ID(), Verifier: base64.RawURLEncoding.EncodeToString(secure.Random(32)), Expires: g.Clock.Unix() + 600, Status: "waiting"}
	g.mu.Lock()
	defer g.mu.Unlock()
	rows, e := g.Store.Values("gwflows")
	if e != nil {
		fail(w, 500)
		return
	}
	count := 0
	for _, row := range rows {
		var old Flow
		_ = json.Unmarshal(row, &old)
		if old.Expires < g.Clock.Unix() {
			_ = g.Store.Delete("gwflows", old.ID)
			_ = g.Store.Delete("gwoauth", old.State)
		} else {
			count++
		}
	}
	if count >= 1024 {
		fail(w, 429)
		return
	}
	if g.Store.Put("gwflows", flow.ID, flow) != nil || g.Store.Put("gwoauth", flow.State, flow.ID) != nil {
		fail(w, 500)
		return
	}
	write(w, map[string]any{"flow": id, "public": secure.B64.EncodeToString(p.PublicKey().Bytes()), "salt": secure.B64.EncodeToString(salt), "url": g.URL + "/v2/auth/login?flow=" + id, "expires": flow.Expires})
	diagnostic(r, "login_flow_created", map[string]any{"kind": flow.Kind, "flow_ref": ref(id)})
}
func (g *Server) pollFlow(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.Header.Get("Authorization"), "Flow ")
	var flow Flow
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<10))
	if e != nil {
		fail(w, 400)
		return
	}
	if !relay.ValidID(id) || g.Store.Get("gwflows", id, &flow) != nil || flow.Expires < g.Clock.Unix() {
		reject(w, r, 401, "login_flow_missing_or_expired")
		return
	}
	key, _ := secure.B64.DecodeString(flow.Key)
	if !g.replay.Verify(key, "flow/"+id, r, raw, "") {
		reject(w, r, 401, "login_proof_rejected")
		return
	}
	box, e := secure.SealJSON(key, "flow-result/"+id, 0, 0, map[string]any{"status": flow.Status, "result": flow.Result, "challenge": relay.Hash([]byte(r.Header.Get("X-Relay-Auth")))})
	if e != nil {
		fail(w, 500)
		return
	}
	write(w, box)
}
func pageHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src data:; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
}
func (g *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	var flow Flow
	id := r.URL.Query().Get("flow")
	if g.Feishu == nil || !relay.ValidID(id) || g.Store.Get("gwflows", id, &flow) != nil || flow.Expires < g.Clock.Unix() || flow.Used {
		fail(w, 410)
		return
	}
	link := g.URL + "/v2/auth/authorize?flow=" + id
	qr, e := qrcode.Encode(link, qrcode.Medium, 320)
	if e != nil {
		fail(w, 500)
		return
	}
	purpose := "登录手机，查找你已关联的工作站"
	if flow.Kind == "local" {
		purpose = "关联工作站：" + flow.Name
	}
	pageHeaders(w)
	fmt.Fprintf(w, `<!doctype html><html lang="zh"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>AgentDeck · 飞书关联</title><style>body{font:16px system-ui;background:#f3f3f0;padding:32px;max-width:460px;margin:auto;color:#171817}h1{font-size:32px}img{width:256px;max-width:100%%}a{display:block;background:#171817;color:white;padding:16px;text-decoration:none;margin-top:24px}p{line-height:1.6}</style><h1>AgentDeck</h1><p>%s</p><p>用飞书扫一扫并确认身份。仅关联当前设备，首个手机的会话密钥还需双方校验码确认。</p><img alt="飞书扫码关联" src="data:image/png;base64,%s"><a href="%s">在飞书授权</a><p>10 分钟内有效。请核对域名和关联用途。</p></html>`, html.EscapeString(purpose), base64.StdEncoding.EncodeToString(qr), html.EscapeString(link))
}
func (g *Server) authorize(w http.ResponseWriter, r *http.Request) {
	if g.Feishu == nil {
		fail(w, 503)
		return
	}
	var flow Flow
	id := r.URL.Query().Get("flow")
	g.mu.Lock()
	defer g.mu.Unlock()
	if !relay.ValidID(id) || g.Store.Get("gwflows", id, &flow) != nil || flow.Expires < g.Clock.Unix() || flow.Used {
		fail(w, 410)
		return
	}
	cookieName := "__Host-agentdeck-oauth-" + flow.ID
	cookie := secure.ID()
	if flow.CookieHash != "" {
		old, e := r.Cookie(cookieName)
		if e != nil || relay.Hash([]byte(old.Value)) != flow.CookieHash {
			fail(w, 409)
			return
		}
		cookie = old.Value
	} else {
		flow.CookieHash = relay.Hash([]byte(cookie))
		if g.Store.Put("gwflows", id, flow) != nil {
			fail(w, 500)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: cookie, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	h := sha256.Sum256([]byte(flow.Verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])
	http.Redirect(w, r, g.Feishu.Authorize(flow.State, challenge), http.StatusSeeOther)
}
func (g *Server) callback(w http.ResponseWriter, r *http.Request) {
	if g.Feishu == nil {
		fail(w, 503)
		return
	}
	state := r.URL.Query().Get("state")
	var id string
	var flow Flow
	g.mu.Lock()
	if !relay.ValidID(state) || g.Store.Get("gwoauth", state, &id) != nil || g.Store.Get("gwflows", id, &flow) != nil || flow.Used || flow.Expires < g.Clock.Unix() {
		g.mu.Unlock()
		reject(w, r, 401, "oauth_state_missing_expired_or_used")
		return
	}
	cookie, e := r.Cookie("__Host-agentdeck-oauth-" + flow.ID)
	if e != nil || flow.CookieHash == "" || relay.Hash([]byte(cookie.Value)) != flow.CookieHash {
		g.mu.Unlock()
		reject(w, r, 401, "oauth_browser_cookie_mismatch")
		return
	}
	flow.Used = true
	flow.Status = "failed"
	_ = g.Store.Delete("gwoauth", state)
	if g.Store.Put("gwflows", flow.ID, flow) != nil {
		g.mu.Unlock()
		fail(w, 500)
		return
	}
	g.mu.Unlock()
	code := r.URL.Query().Get("code")
	if code == "" || len(code) > 4096 || r.URL.Query().Get("error") != "" {
		reject(w, r, 401, "oauth_denied_or_code_missing")
		return
	}
	identity, e := g.Feishu.Resolve(r.Context(), code, flow.Verifier)
	if e != nil {
		diagnostic(r, "feishu_identity_failed", providerDiagnostic(e))
		reject(w, r, 401, "feishu_identity_resolution_failed")
		return
	}
	if !relay.ValidID(identity.Tenant) || identity.Owner == "" {
		reject(w, r, 401, "feishu_identity_invalid")
		return
	}
	var result any
	if flow.Kind == "local" {
		link, e := g.AddLocal(identity.Tenant, flow.Name)
		if e != nil {
			fail(w, 429)
			return
		}
		var node Node
		if g.Store.Get("gwnodes", link.Local, &node) != nil {
			fail(w, 500)
			return
		}
		node.Owner = identity.Owner
		if g.Store.Put("gwnodes", node.ID, node) != nil {
			fail(w, 500)
			return
		}
		result = map[string]any{"link": link, "identity": identity}
	} else {
		// Account grants are routing credentials; they are not Local/App roots.
		account := Account{secure.ID(), secure.B64.EncodeToString(secure.Random(32)), identity, g.Clock.Unix() + 30*24*3600}
		g.mu.Lock()
		rows, e := g.Store.Values("gwaccounts")
		if e != nil {
			g.mu.Unlock()
			fail(w, 500)
			return
		}
		count := 0
		for _, row := range rows {
			var a Account
			_ = json.Unmarshal(row, &a)
			if a.Expires < g.Clock.Unix() {
				_ = g.Store.Delete("gwaccounts", a.ID)
			} else {
				count++
			}
		}
		if count >= 4096 || g.Store.Put("gwaccounts", account.ID, account) != nil {
			g.mu.Unlock()
			fail(w, 429)
			return
		}
		g.mu.Unlock()
		result = map[string]any{"account": account, "identity": identity}
	}
	flow.Result, _ = json.Marshal(result)
	flow.Status = "ready"
	if g.Store.Put("gwflows", flow.ID, flow) != nil {
		fail(w, 500)
		return
	}
	diagnostic(r, "login_authorized", map[string]any{"kind": flow.Kind, "flow_ref": ref(flow.ID), "tenant_ref": ref(identity.Tenant), "owner_ref": ref(identity.Owner)})
	http.SetCookie(w, &http.Cookie{Name: "__Host-agentdeck-oauth-" + flow.ID, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.Redirect(w, r, g.URL+"/v2/auth/done", http.StatusSeeOther)
}
func (g *Server) accountAuth(w http.ResponseWriter, r *http.Request) (Account, []byte, bool) {
	return g.accountAuthLimit(w, r, 16<<10)
}
func (g *Server) accountAuthLimit(w http.ResponseWriter, r *http.Request, limit int64) (Account, []byte, bool) {
	var a Account
	id := strings.TrimPrefix(r.Header.Get("Authorization"), "Account ")
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if e != nil || !relay.ValidID(id) || g.Store.Get("gwaccounts", id, &a) != nil || a.Expires < g.Clock.Unix() {
		fail(w, 401)
		return a, nil, false
	}
	key, _ := secure.B64.DecodeString(a.Key)
	if !g.replay.Verify(key, "account/"+id, r, raw, "") {
		fail(w, 401)
		return a, nil, false
	}
	return a, raw, true
}
func (g *Server) accountWrite(w http.ResponseWriter, a Account, r *http.Request, result any) {
	key, _ := secure.B64.DecodeString(a.Key)
	box, e := secure.SealJSON(key, "account-response/"+a.ID, 0, 0, map[string]any{"result": result, "challenge": relay.Hash([]byte(r.Header.Get("X-Relay-Auth")))})
	if e != nil {
		fail(w, 500)
		return
	}
	write(w, box)
}
func (g *Server) accountLocals(w http.ResponseWriter, r *http.Request) {
	a, _, ok := g.accountAuth(w, r)
	if !ok {
		return
	}
	rows, e := g.Store.Values("gwnodes")
	if e != nil {
		fail(w, 500)
		return
	}
	result := []map[string]any{}
	g.mu.Lock()
	for _, raw := range rows {
		var n Node
		_ = json.Unmarshal(raw, &n)
		if n.Tenant == a.Identity.Tenant && n.Owner == a.Identity.Owner {
			result = append(result, map[string]any{"id": n.ID, "name": n.Name, "online": g.peers[n.ID] != nil})
		}
	}
	g.mu.Unlock()
	g.accountWrite(w, a, r, result)
}
func (g *Server) accountBind(w http.ResponseWriter, r *http.Request) {
	a, raw, ok := g.accountAuth(w, r)
	if !ok {
		return
	}
	key, _ := secure.B64.DecodeString(a.Key)
	var box secure.Box
	var req struct {
		Local string `json:"local"`
	}
	if json.Unmarshal(raw, &box) != nil {
		fail(w, 400)
		return
	}
	plain, e := secure.Open(key, "account-request/"+a.ID, box)
	if e != nil || json.Unmarshal(plain, &req) != nil {
		fail(w, 400)
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var current Account
	if g.Store.Get("gwaccounts", a.ID, &current) != nil || current.Expires < g.Clock.Unix() {
		fail(w, 401)
		return
	}
	var n Node
	if g.Store.Get("gwnodes", req.Local, &n) != nil || n.Tenant != a.Identity.Tenant || n.Owner != a.Identity.Owner {
		reject(w, r, 403, "workstation_ownership_mismatch")
		return
	}
	if g.peers[n.ID] == nil {
		reject(w, r, 503, "local_offline")
		return
	}
	rows, e := g.Store.Values("gwgrants")
	if e != nil {
		fail(w, 500)
		return
	}
	count := 0
	for _, raw := range rows {
		var old relay.Grant
		_ = json.Unmarshal(raw, &old)
		if old.Device == "" && old.Expires < g.Clock.Unix() {
			_ = g.Store.Delete("gwgrants", old.ID)
		} else if old.Local == n.ID {
			count++
		}
	}
	if count >= 32 {
		fail(w, 429)
		return
	}
	grant := relay.Grant{Account: a.ID, ID: secure.ID(), Key: secure.B64.EncodeToString(secure.Random(32)), Tenant: n.Tenant, Local: n.ID, Expires: g.Clock.Unix() + 300}
	if g.Store.Put("gwgrants", grant.ID, grant) != nil {
		fail(w, 500)
		return
	}
	g.accountWrite(w, a, r, grant)
	diagnostic(r, "phone_binding_requested", map[string]any{"local_ref": ref(n.ID), "grant_ref": ref(grant.ID), "owner_ref": ref(a.Identity.Owner)})
}
