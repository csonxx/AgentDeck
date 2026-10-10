package gateway

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"agentdeck/internal/relay"
	"agentdeck/internal/secure"
	"agentdeck/internal/server"
	"agentdeck/internal/store"
	"github.com/gorilla/websocket"
)

type fixture struct {
	g      *Server
	http   *httptest.Server
	local  *server.Server
	client *relay.Client
	link   relay.Link
}

func setup(t *testing.T) *fixture {
	return setupPrefix(t, "")
}
func setupPrefix(t *testing.T, prefix string) *fixture {
	t.Helper()
	db, e := store.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	g := New(db, "", "", "pin", time.Second)
	ts := httptest.NewTLSServer(g.Handler())
	g.URL = ts.URL + prefix
	g.Pin = relay.Hash(ts.Certificate().RawSubjectPublicKeyInfo)
	link, e := g.AddLocal("tenant-a", "workstation")
	if e != nil {
		t.Fatal(e)
	}
	var node Node
	_ = db.Get("gwnodes", link.Local, &node)
	node.Owner = "owner-a"
	_ = db.Put("gwnodes", node.ID, node)
	localDB, e := store.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	local := server.New(localDB, g.Pin, g.URL, time.Second)
	client, e := relay.NewClient(link)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); client.Run(ctx, local.Handler()) }()
	t.Cleanup(func() {
		cancel()
		g.Close()
		<-done
		ts.Close()
		localDB.Close()
		db.Close()
	})
	f := &fixture{g, ts, local, client, link}
	await(t, client.Online)
	return f
}
func await(t *testing.T, cond func() bool) {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(end) {
			t.Fatal("condition timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func request(t *testing.T, h *http.Client, origin, path, auth string, key []byte, scope string, body any, want int) ([]byte, http.Header) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	proof, e := relay.Auth(key, scope, "POST", path, raw, "")
	if e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequest("POST", origin+path, bytes.NewReader(raw))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Relay-Auth", proof)
	res, e := h.Do(req)
	if e != nil {
		t.Fatal("request failed")
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != want {
		t.Fatalf("%s got %d want %d", path, res.StatusCode, want)
	}
	return data, res.Header
}
func grantFor(t *testing.T, f *fixture) relay.Grant {
	t.Helper()
	g := relay.Grant{ID: secure.ID(), Key: secure.B64.EncodeToString(secure.Random(32)), Tenant: f.link.Tenant, Local: f.link.Local, Expires: time.Now().Add(5 * time.Minute).Unix()}
	if f.g.Store.Put("gwgrants", g.ID, g) != nil {
		t.Fatal("grant storage failed")
	}
	return g
}

type phone struct {
	grant          relay.Grant
	root, salt     []byte
	device, ticket string
	ws             *websocket.Conn
	sent           uint64
}

func pair(t *testing.T, f *fixture) *phone {
	t.Helper()
	g := grantFor(t, f)
	key, _ := secure.B64.DecodeString(g.Key)
	p, _ := ecdh.X25519().GenerateKey(rand.Reader)
	path := "/v2/relay/" + g.Local + "/offer"
	data, _ := request(t, f.http.Client(), f.g.URL, path, "Relay "+g.ID, key, "grant/"+g.ID, map[string]string{"public": secure.B64.EncodeToString(p.PublicKey().Bytes())}, 200)
	var offer struct{ Public, Token, Invite string }
	if json.Unmarshal(data, &offer) != nil {
		t.Fatal("offer invalid")
	}
	pub, _ := secure.B64.DecodeString(offer.Public)
	tok, _ := secure.B64.DecodeString(offer.Token)
	root, e := secure.PairRoot(p, pub, tok)
	if e != nil {
		t.Fatal(e)
	}
	body, _ := secure.SealJSON(root, "pair-request/"+offer.Invite, 0, 0, map[string]string{"token": offer.Token, "name": "test phone"})
	req := map[string]any{"invite": offer.Invite, "public": secure.B64.EncodeToString(p.PublicKey().Bytes()), "box": body}
	path = "/v2/relay/" + g.Local + "/pair"
	// The gateway knows routing secrets/public DH material, but neither root nor
	// the SAS. Pending approval cannot be bypassed with a valid gateway grant.
	bad, _ := secure.SealJSON(key, "pair-request/"+offer.Invite, 0, 0, map[string]string{"token": offer.Token, "name": "wrong root"})
	request(t, f.http.Client(), f.g.URL, path, "Relay "+g.ID, key, "grant/"+g.ID, map[string]any{"invite": offer.Invite, "public": req["public"], "box": bad}, 401)
	// An invalid encrypted proof cannot consume the owner's pending invitation.
	request(t, f.http.Client(), f.g.URL, path, "Relay "+g.ID, key, "grant/"+g.ID, req, 202)
	if f.local.Approve(g.ID, strings.Repeat("0", 12)) == nil {
		t.Fatal("wrong comparison code approved")
	}
	if f.local.Approve(g.ID, server.PairCode(root, offer.Invite)) != nil {
		t.Fatal("valid comparison code rejected")
	}
	data, _ = request(t, f.http.Client(), f.g.URL, path, "Relay "+g.ID, key, "grant/"+g.ID, req, 200)
	var box secure.Box
	_ = json.Unmarshal(data, &box)
	plain, e := secure.Open(root, "pair", box)
	if e != nil {
		t.Fatal(e)
	}
	var reply struct {
		Device string `json:"device"`
		Grant  string `json:"relay_grant"`
	}
	_ = json.Unmarshal(plain, &reply)
	if reply.Grant != g.ID {
		t.Fatal("peer binding mismatch")
	}
	request(t, f.http.Client(), f.g.URL, path, "Relay "+g.ID, key, "grant/"+g.ID, req, 401)
	return &phone{grant: g, root: root, device: reply.Device}
}
func (p *phone) connect(t *testing.T, f *fixture) {
	t.Helper()
	key, _ := secure.B64.DecodeString(p.grant.Key)
	hello, _ := secure.SealJSON(p.root, "hello/"+p.device, 0, 0, map[string]any{"time": time.Now().Unix(), "challenge": secure.ID()})
	path := "/v2/relay/" + p.grant.Local + "/session"
	data, _ := request(t, f.http.Client(), f.g.URL, path, "Relay "+p.grant.ID, key, "grant/"+p.grant.ID, map[string]any{"device": p.device, "box": hello}, 200)
	var box secure.Box
	_ = json.Unmarshal(data, &box)
	plain, e := secure.Open(p.root, "welcome/"+p.device, box)
	if e != nil {
		t.Fatal(e)
	}
	var v struct{ Salt, Ticket string }
	_ = json.Unmarshal(plain, &v)
	p.salt, _ = secure.B64.DecodeString(v.Salt)
	p.ticket = v.Ticket
	path = "/v2/relay/" + p.grant.Local + "/stream"
	proof, _ := relay.Auth(key, "grant/"+p.grant.ID, "GET", path, nil, "Bearer "+p.ticket)
	cfg, _ := relay.TLSConfig("pin", f.g.Pin)
	d := websocket.Dialer{TLSClientConfig: cfg}
	ws, _, e := d.Dial(strings.Replace(f.g.URL, "https://", "wss://", 1)+path, http.Header{"Authorization": []string{"Relay " + p.grant.ID}, "X-Relay-Auth": []string{proof}, "X-Local-Authorization": []string{"Bearer " + p.ticket}})
	if e != nil {
		t.Fatal("relay websocket upgrade failed", e)
	}
	p.ws = ws
	p.sent = 0
	t.Cleanup(func() { ws.Close() })
}
func (p *phone) send(t *testing.T, v any) {
	p.sent++
	ep := time.Now().UnixMilli() / 1000
	b, e := secure.SealJSON(secure.Traffic(p.root, p.salt, "c2s", ep), "c2s", ep, p.sent, v)
	if e != nil || p.ws.WriteJSON(b) != nil {
		t.Fatal("encrypted request failed")
	}
}
func (p *phone) read(t *testing.T, kind string) (map[string]any, secure.Box) {
	t.Helper()
	// Race instrumentation encrypts and relays a 2 MiB frame; slow hosts need
	// a bounded media allowance without weakening ordinary response deadlines.
	wait := 5 * time.Second
	if kind == "media" {
		wait = 15 * time.Second
	}
	_ = p.ws.SetReadDeadline(time.Now().Add(wait))
	for i := 0; i < 40; i++ {
		var box secure.Box
		if e := p.ws.ReadJSON(&box); e != nil {
			t.Fatal("encrypted response unavailable", e)
		}
		plain, e := secure.Open(secure.Traffic(p.root, p.salt, "s2c", box.Epoch), "s2c", box)
		if e != nil {
			t.Fatal("device decryption failed")
		}
		var v map[string]any
		_ = json.Unmarshal(plain, &v)
		if v["type"] == kind {
			return v, box
		}
	}
	t.Fatal("response missing")
	return nil, secure.Box{}
}
func TestRelayEncryptedPairLiveMediaReconnectionAndRevocation(t *testing.T) {
	for _, prefix := range []string{"", "/agentdeck"} {
		t.Run("prefix="+prefix, func(t *testing.T) { testRelayEncryptedPairLiveMediaReconnectionAndRevocation(t, prefix) })
	}
}
func testRelayEncryptedPairLiveMediaReconnectionAndRevocation(t *testing.T, prefix string) {
	f := setupPrefix(t, prefix)
	p := pair(t, f)
	p.connect(t, f)
	p.read(t, "sessions")
	p.send(t, server.Request{Method: "watch", Session: "live"})
	p.read(t, "watching")
	_, _, _ = f.local.Store.Append(store.Event{ID: "a", Session: "live", Agent: "codex", Kind: "message", Role: "assistant", Text: "PRIVATE_AGENT_CONTENT", Source: "stream"})
	v, box := p.read(t, "events")
	if v["cursor"] != float64(1) {
		t.Fatal("missing live event")
	}
	routeKey, _ := secure.B64.DecodeString(p.grant.Key)
	localKey, _ := secure.B64.DecodeString(f.link.Key)
	for _, wrong := range [][]byte{routeKey, localKey} {
		if _, e := secure.Open(secure.Traffic(wrong, p.salt, "s2c", box.Epoch), "s2c", box); e == nil {
			t.Fatal("gateway routing key decrypted agent content")
		}
	}
	// Exercise >8 window chunks: bounded credit flow must not drop large images.
	image := secure.Random(2 << 20)
	_ = f.local.Store.Put("media", "large", store.Media{MIME: "image/png", Data: image})
	p.send(t, server.Request{Method: "media", Media: "large"})
	v, _ = p.read(t, "media")
	decoded, _ := secure.B64.DecodeString(v["bytes"].(string))
	if !bytes.Equal(decoded, image) {
		t.Fatal("large media corrupted")
	}
	time.Sleep(1100 * time.Millisecond)
	p.send(t, server.Request{Method: "ping"})
	p.read(t, "pong")
	f.g.mu.Lock()
	peer := f.g.peers[f.link.Local]
	f.g.mu.Unlock()
	peer.Close()
	_ = p.ws.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, e := p.ws.ReadMessage(); e == nil {
		t.Fatal("tunnel failure left client alive")
	}
	await(t, func() bool {
		f.g.mu.Lock()
		defer f.g.mu.Unlock()
		return f.g.peers[f.link.Local] != nil && f.g.peers[f.link.Local] != peer
	})
	_, _, _ = f.local.Store.Append(store.Event{ID: "b", Session: "live", Agent: "claude", Kind: "thinking", Text: "summary", Source: "stream"})
	p.connect(t, f)
	p.read(t, "sessions")
	p.send(t, server.Request{Method: "watch", Session: "live", After: 1})
	p.read(t, "watching")
	v, _ = p.read(t, "events")
	if len(v["events"].([]any)) != 1 {
		t.Fatal("resume did not retain cursor")
	}
	for _, bucket := range []string{"events", "sessions", "media", "devices"} {
		rows, _ := f.g.Store.Values(bucket)
		if len(rows) != 0 {
			t.Fatal("gateway stored local content or device roots")
		}
	}
	q := pair(t, f)
	q.connect(t, f)
	q.read(t, "sessions")
	if bytes.Equal(q.root, p.root) {
		t.Fatal("device roots reused")
	}
	if _, e := secure.Open(secure.Traffic(q.root, p.salt, "s2c", box.Epoch), "s2c", box); e == nil {
		t.Fatal("other phone decrypted frame")
	}
	_ = f.g.RevokeGrant(p.grant.ID)
	_ = p.ws.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, e := p.ws.ReadMessage(); e == nil {
		t.Fatal("revoked grant stayed connected")
	}
	q.send(t, server.Request{Method: "ping"})
	q.read(t, "pong")
}
func TestTenantOwnerIsolationAndProofReplay(t *testing.T) {
	f := setup(t)
	a := Account{ID: secure.ID(), Key: secure.B64.EncodeToString(secure.Random(32)), Identity: Identity{Tenant: "tenant-a", Owner: "owner-a"}, Expires: time.Now().Add(time.Hour).Unix()}
	_ = f.g.Store.Put("gwaccounts", a.ID, a)
	key, _ := secure.B64.DecodeString(a.Key)
	sameTenant, _ := f.g.AddLocal("tenant-a", "other owner")
	otherTenant, _ := f.g.AddLocal("tenant-b", "other tenant")
	for _, link := range []relay.Link{sameTenant, otherTenant} {
		var n Node
		_ = f.g.Store.Get("gwnodes", link.Local, &n)
		n.Owner = "owner-b"
		if link.Tenant == "tenant-b" {
			n.Owner = "owner-a"
		}
		_ = f.g.Store.Put("gwnodes", n.ID, n)
		box, _ := secure.SealJSON(key, "account-request/"+a.ID, 0, 0, map[string]string{"local": n.ID})
		request(t, f.http.Client(), f.g.URL, "/v2/account/bind", "Account "+a.ID, key, "account/"+a.ID, box, 403)
	}
	data, _ := request(t, f.http.Client(), f.g.URL, "/v2/account/locals", "Account "+a.ID, key, "account/"+a.ID, nil, 200)
	var box secure.Box
	_ = json.Unmarshal(data, &box)
	plain, _ := secure.Open(key, "account-response/"+a.ID, box)
	var v struct{ Result []map[string]any }
	_ = json.Unmarshal(plain, &v)
	if len(v.Result) != 1 || v.Result[0]["id"] != f.link.Local {
		t.Fatal("tenant/owner list leaked")
	}
	g := grantFor(t, f)
	gkey, _ := secure.B64.DecodeString(g.Key)
	path := "/v2/relay/" + otherTenant.Local + "/offer"
	request(t, f.http.Client(), f.g.URL, path, "Relay "+g.ID, gkey, "grant/"+g.ID, map[string]string{"public": secure.B64.EncodeToString(secure.Random(32))}, 401)
	proof, _ := relay.Auth(key, "account/"+a.ID, "POST", "/v2/account/locals", nil, "")
	for i, want := range []int{200, 401} {
		req, _ := http.NewRequest("POST", f.g.URL+"/v2/account/locals", nil)
		req.Header.Set("Authorization", "Account "+a.ID)
		req.Header.Set("X-Relay-Auth", proof)
		res, e := f.http.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("proof attempt %d got %d", i, res.StatusCode)
		}
	}
}
func TestFeishuPKCECookieStateAndEncryptedEnrollment(t *testing.T) {
	for _, prefix := range []string{"", "/agentdeck"} {
		t.Run("prefix="+prefix, func(t *testing.T) { testFeishuPKCECookieStateAndEncryptedEnrollment(t, prefix) })
	}
}
func testFeishuPKCECookieStateAndEncryptedEnrollment(t *testing.T, prefix string) {
	f := setupPrefix(t, prefix)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["client_id"] != "synthetic-app" || req["client_secret"] != "synthetic-secret" || req["code_verifier"] == "" || req["redirect_uri"] != f.g.URL+"/v2/auth/callback" {
				t.Error("OAuth exchange fields missing")
				fail(w, 401)
				return
			}
			write(w, map[string]any{"code": 0, "access_token": "synthetic-user-token"})
		case "/user":
			if r.Header.Get("Authorization") != "Bearer synthetic-user-token" {
				fail(w, 401)
				return
			}
			write(w, map[string]any{"code": 0, "data": map[string]string{"tenant_key": "tenant-feishu", "open_id": "owner-feishu", "name": "test user"}})
		default:
			fail(w, 404)
		}
	}))
	defer fake.Close()
	provider := NewFeishu("synthetic-app", "synthetic-secret", f.g.URL+"/v2/auth/callback")
	provider.AuthorizeURL = fake.URL + "/authorize"
	provider.TokenURL = fake.URL + "/token"
	provider.UserURL = fake.URL + "/user"
	f.g.Feishu = provider
	flow, e := relay.BeginLogin(context.Background(), f.http.Client(), f.g.URL, "local", "SSO workstation")
	if e != nil {
		t.Fatal(e)
	}
	page, e := f.http.Client().Get(flow.URL)
	if e != nil {
		t.Fatal(e)
	}
	content, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if !bytes.Contains(content, []byte("data:image/png;base64,")) || bytes.Contains(content, []byte(secure.B64.EncodeToString(flow.Key))) {
		t.Fatal("login page missing QR or disclosed key")
	}
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Transport: f.http.Client().Transport, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := browser.Get(f.g.URL + "/v2/auth/authorize?flow=" + flow.ID)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	authURL, _ := url.Parse(res.Header.Get("Location"))
	if authURL.Query().Get("code_challenge") == "" || authURL.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("PKCE missing")
	}
	callback := f.g.URL + "/v2/auth/callback?state=" + authURL.Query().Get("state") + "&code=synthetic-code"
	// Knowing the QR flow/state alone is insufficient to complete the callback.
	attack, e := f.http.Client().Get(callback)
	if e != nil {
		t.Fatal(e)
	}
	attack.Body.Close()
	if attack.StatusCode != 401 {
		t.Fatal("callback without cookie accepted")
	}
	res, e = browser.Get(callback)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 303 {
		t.Fatal("Feishu callback rejected")
	}
	raw, ready, e := flow.Poll(context.Background())
	if e != nil || !ready {
		t.Fatal("encrypted login result missing")
	}
	var v struct {
		Link     relay.Link `json:"link"`
		Identity Identity   `json:"identity"`
	}
	_ = json.Unmarshal(raw, &v)
	if v.Link.Tenant != "tenant-feishu" || v.Identity.Owner != "owner-feishu" {
		t.Fatal("verified identity not used")
	}
	var n Node
	_ = f.g.Store.Get("gwnodes", v.Link.Local, &n)
	if n.Owner != "owner-feishu" {
		t.Fatal("local owner not recorded")
	}
	res, e = browser.Get(callback)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("OAuth state reused")
	}
	bad := *flow
	bad.Key = secure.Random(32)
	if _, _, e = bad.Poll(context.Background()); e == nil {
		t.Fatal("wrong flow key read enrollment")
	}
	// No identity credentials are accepted from Local/App request bodies.
	var saved Flow
	_ = f.g.Store.Get("gwflows", flow.ID, &saved)
	saved.Expires = time.Now().Unix() - 1
	_ = f.g.Store.Put("gwflows", flow.ID, saved)
	if _, _, e = flow.Poll(context.Background()); e == nil {
		t.Fatal("expired handoff accepted")
	}
}

func TestAccountRevocationClosesIssuedGrantsAndPreservesOtherPhone(t *testing.T) {
	f := setup(t)
	a := Account{ID: secure.ID(), Key: secure.B64.EncodeToString(secure.Random(32)), Identity: Identity{Tenant: "tenant-a", Owner: "owner-a"}, Expires: time.Now().Add(time.Hour).Unix()}
	if e := f.g.Store.Put("gwaccounts", a.ID, a); e != nil {
		t.Fatal(e)
	}
	key, _ := secure.B64.DecodeString(a.Key)
	bind, _ := secure.SealJSON(key, "account-request/"+a.ID, 0, 0, map[string]string{"local": f.link.Local})
	data, _ := request(t, f.http.Client(), f.g.URL, "/v2/account/bind", "Account "+a.ID, key, "account/"+a.ID, bind, 200)
	var box secure.Box
	_ = json.Unmarshal(data, &box)
	plain, _ := secure.Open(key, "account-response/"+a.ID, box)
	var result struct{ Result relay.Grant }
	_ = json.Unmarshal(plain, &result)
	if result.Result.Account != a.ID {
		t.Fatal("issued grant lost originating account")
	}
	p, q := pair(t, f), pair(t, f)
	if e := f.g.Store.Get("gwgrants", p.grant.ID, &p.grant); e != nil {
		t.Fatal(e)
	}
	p.grant.Account = a.ID
	if e := f.g.Store.Put("gwgrants", p.grant.ID, p.grant); e != nil {
		t.Fatal(e)
	}
	p.connect(t, f)
	q.connect(t, f)
	p.send(t, server.Request{Method: "ping"})
	p.read(t, "pong")
	if e := f.g.RevokeAccount(a.ID); e != nil {
		t.Fatal(e)
	}
	request(t, f.http.Client(), f.g.URL, "/v2/account/locals", "Account "+a.ID, key, "account/"+a.ID, nil, 401)
	var grant relay.Grant
	if f.g.Store.Get("gwgrants", result.Result.ID, &grant) == nil || f.g.Store.Get("gwgrants", p.grant.ID, &grant) == nil {
		t.Fatal("revoked account retained grants")
	}
	_ = p.ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, e := p.ws.ReadMessage(); e == nil {
		t.Fatal("revoked account stream stayed open")
	}
	q.send(t, server.Request{Method: "ping"})
	q.read(t, "pong")
}
