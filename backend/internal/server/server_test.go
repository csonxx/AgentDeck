package server

import (
	"agentdeck/internal/agent"
	"agentdeck/internal/secure"
	"agentdeck/internal/store"
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type phone struct {
	device         string
	root, salt     []byte
	ticket         string
	sent, received uint64
	ws             *websocket.Conn
}

func setupServer(t *testing.T) (*Server, *httptest.Server) {
	db, e := store.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	s := New(db, "", "", time.Second)
	ts := httptest.NewTLSServer(s.Handler())
	h := sha256.Sum256(ts.Certificate().RawSubjectPublicKeyInfo)
	s.Pin = hex.EncodeToString(h[:])
	s.PublicURL = ts.URL
	t.Cleanup(func() { ts.Close(); db.Close() })
	return s, ts
}
func post(t *testing.T, ts *httptest.Server, path string, v any, want int) []byte {
	t.Helper()
	b, _ := json.Marshal(v)
	resp, e := ts.Client().Post(ts.URL+path, "application/json", bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		t.Fatalf("%s: got HTTP %d want %d", path, resp.StatusCode, want)
	}
	var raw json.RawMessage
	if want == 200 {
		if e = json.NewDecoder(resp.Body).Decode(&raw); e != nil {
			t.Fatal(e)
		}
	}
	return raw
}
func pairPhone(t *testing.T, s *Server, ts *httptest.Server) *phone {
	t.Helper()
	b, e := s.Invite()
	if e != nil {
		t.Fatal(e)
	}
	p, _ := ecdh.X25519().GenerateKey(rand.Reader)
	pub, _ := secure.B64.DecodeString(b.Public)
	tok, _ := secure.B64.DecodeString(b.Token)
	root, e := secure.PairRoot(p, pub, tok)
	if e != nil {
		t.Fatal(e)
	}
	req := map[string]any{"token": b.Token, "public": secure.B64.EncodeToString(p.PublicKey().Bytes()), "name": "test phone"}
	raw := post(t, ts, "/v1/pair", req, 200)
	var box secure.Box
	_ = json.Unmarshal(raw, &box)
	plain, e := secure.Open(root, "pair", box)
	if e != nil {
		t.Fatal(e)
	}
	var response struct {
		Device string `json:"device"`
	}
	_ = json.Unmarshal(plain, &response)
	post(t, ts, "/v1/pair", req, 401)
	return &phone{device: response.Device, root: root}
}
func (p *phone) login(t *testing.T, ts *httptest.Server) map[string]any {
	t.Helper()
	challenge := secure.ID()
	b, _ := secure.SealJSON(p.root, "hello/"+p.device, 0, 0, map[string]any{"time": time.Now().Unix(), "challenge": challenge})
	req := map[string]any{"device": p.device, "box": b}
	raw := post(t, ts, "/v1/session", req, 200)
	post(t, ts, "/v1/session", req, 401)
	var box secure.Box
	_ = json.Unmarshal(raw, &box)
	plain, e := secure.Open(p.root, "welcome/"+p.device, box)
	if e != nil {
		t.Fatal(e)
	}
	var v map[string]any
	_ = json.Unmarshal(plain, &v)
	if v["challenge"] != challenge {
		t.Fatal("challenge mismatch")
	}
	p.ticket = v["ticket"].(string)
	p.salt, _ = secure.B64.DecodeString(v["salt"].(string))
	return req
}
func (p *phone) connect(t *testing.T, ts *httptest.Server, s *Server) {
	t.Helper()
	p.login(t, ts)
	d := websocket.Dialer{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, InsecureSkipVerify: true, VerifyConnection: func(cs tls.ConnectionState) error {
		h := sha256.Sum256(cs.PeerCertificates[0].RawSubjectPublicKeyInfo)
		if hex.EncodeToString(h[:]) != s.Pin {
			return http.ErrAbortHandler
		}
		return nil
	}}}
	ws, _, e := d.Dial(strings.Replace(ts.URL, "https://", "wss://", 1)+"/v1/stream", http.Header{"Authorization": []string{"Bearer " + p.ticket}})
	if e != nil {
		t.Fatal(e)
	}
	p.ws = ws
	p.sent = 0
	p.received = 0
	t.Cleanup(func() { ws.Close() })
	if _, resp, e := d.Dial(strings.Replace(ts.URL, "https://", "wss://", 1)+"/v1/stream", http.Header{"Authorization": []string{"Bearer " + p.ticket}}); e == nil || resp.StatusCode != 401 {
		t.Fatal("ticket reused")
	}
}
func (p *phone) send(t *testing.T, v any) secure.Box {
	t.Helper()
	p.sent++
	epoch := time.Now().UnixMilli() / 1000
	b, e := secure.SealJSON(secure.Traffic(p.root, p.salt, "c2s", epoch), "c2s", epoch, p.sent, v)
	if e != nil {
		t.Fatal(e)
	}
	if e = p.ws.WriteJSON(b); e != nil {
		t.Fatal(e)
	}
	return b
}
func (p *phone) read(t *testing.T, kind string) map[string]any {
	t.Helper()
	_ = p.ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	for i := 0; i < 30; i++ {
		var b secure.Box
		if e := p.ws.ReadJSON(&b); e != nil {
			t.Fatal(e)
		}
		if b.Seq != p.received+1 {
			t.Fatal("sequence gap")
		}
		p.received = b.Seq
		plain, e := secure.Open(secure.Traffic(p.root, p.salt, "s2c", b.Epoch), "s2c", b)
		if e != nil {
			t.Fatal(e)
		}
		var m map[string]any
		_ = json.Unmarshal(plain, &m)
		if m["type"] == kind {
			return m
		}
	}
	t.Fatal("message missing", kind)
	return nil
}
func TestEncryptedLiveReconnectRotationMediaAndRevocation(t *testing.T) {
	s, ts := setupServer(t)
	p := pairPhone(t, s, ts)
	p.connect(t, ts, s)
	p.read(t, "sessions")
	p.send(t, Request{ID: "w", Method: "watch", Session: "live"})
	p.read(t, "watching")
	a, _, e := s.Store.Append(store.Event{ID: "one", Session: "live", Agent: "codex", Kind: "message", Role: "assistant", Text: "实时 hello", Source: "stream"})
	if e != nil {
		t.Fatal(e)
	}
	v := p.read(t, "events")
	if v["cursor"] != float64(a.Seq) {
		t.Fatal(v)
	}
	p.ws.Close()
	_, _, _ = s.Store.Append(store.Event{ID: "two", Session: "live", Agent: "codex", Kind: "thinking", Text: "摘要", Source: "stream"})
	p.connect(t, ts, s)
	p.read(t, "sessions")
	p.send(t, Request{Method: "watch", Session: "live", After: a.Seq})
	p.read(t, "watching")
	v = p.read(t, "events")
	if len(v["events"].([]any)) != 1 {
		t.Fatal("reconnect did not resume", v)
	}
	firstEpoch := time.Now().UnixMilli() / 1000
	time.Sleep(1100 * time.Millisecond)
	if time.Now().UnixMilli()/1000 == firstEpoch {
		t.Fatal("test failed to cross epoch")
	}
	p.send(t, Request{ID: "new-key", Method: "ping"})
	p.read(t, "pong")
	media := store.Media{MIME: "image/png", Data: []byte{1, 2, 3}}
	_ = s.Store.Put("media", "image-id", media)
	p.send(t, Request{ID: "media", Method: "media", Media: "image-id"})
	v = p.read(t, "media")
	if v["bytes"] != secure.B64.EncodeToString(media.Data) {
		t.Fatal("media mismatch")
	}
	_ = s.Store.Delete("devices", p.device)
	_ = p.ws.SetReadDeadline(time.Now().Add(time.Second))
	var box secure.Box
	if e = p.ws.ReadJSON(&box); e == nil {
		t.Fatal("revoked connection stayed alive")
	}
	b, _ := secure.SealJSON(p.root, "hello/"+p.device, 0, 0, map[string]any{"time": time.Now().Unix(), "challenge": secure.ID()})
	post(t, ts, "/v1/session", map[string]any{"device": p.device, "box": b}, 401)
}
func TestReplayFrameClosesSocket(t *testing.T) {
	s, ts := setupServer(t)
	p := pairPhone(t, s, ts)
	p.connect(t, ts, s)
	p.read(t, "sessions")
	b := p.send(t, Request{Method: "ping"})
	p.read(t, "pong")
	_ = p.ws.WriteJSON(b)
	_ = p.ws.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, e := p.ws.ReadMessage(); e == nil {
		t.Fatal("replayed frame accepted")
	}
}
func TestExpiredInviteAndUnknownDevices(t *testing.T) {
	s, ts := setupServer(t)
	tok := secure.Random(32)
	h := sha256.Sum256(tok)
	p, _ := ecdh.X25519().GenerateKey(rand.Reader)
	_ = s.Store.Put("invites", hex.EncodeToString(h[:]), Invite{Token: tok, Private: p.Bytes(), Expires: time.Now().Unix() - 1})
	post(t, ts, "/v1/pair", map[string]any{"token": secure.B64.EncodeToString(tok), "public": secure.B64.EncodeToString(p.PublicKey().Bytes()), "name": "test"}, 401)
	post(t, ts, "/v1/session", map[string]any{"device": "unknown", "box": secure.Box{}}, 401)
	resp, e := ts.Client().Get(ts.URL + "/devices")
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatal("public admin endpoint exposed")
	}
}

func TestAttachmentUsesEncryptedSessionStream(t *testing.T) {
	s, ts := setupServer(t)
	root := t.TempDir()
	s.Normalizer = &agent.Normalizer{Store: s.Store, MediaRoots: []string{root}}
	_ = os.WriteFile(filepath.Join(root, "note.md"), []byte("encrypted attachment\n"), 0600)
	_, _, _ = s.Store.Append(store.Event{ID: "linked", Session: "attachments", Project: root, Agent: "codex", Kind: "message", Text: "[note](note.md:1)"})
	p := pairPhone(t, s, ts)
	p.connect(t, ts, s)
	p.read(t, "sessions")
	p.send(t, Request{ID: "file", Method: "attachment", Session: "attachments", Href: "note.md:1"})
	v := p.read(t, "result")
	result := v["result"].(map[string]any)
	if result["bytes"] != secure.B64.EncodeToString([]byte("encrypted attachment\n")) || result["line"] != float64(1) {
		t.Fatal("attachment content mismatch")
	}
	p.send(t, Request{ID: "wrong", Method: "attachment", Session: "other", Href: "note.md"})
	p.read(t, "error")
}

func TestEncryptedAgentCatalogAndAuthorizationEntry(t *testing.T) {
	s, ts := setupServer(t)
	s.Manager = &agent.Manager{Store: s.Store, Normalizer: &agent.Normalizer{Store: s.Store}}
	config, e := agent.LoadConnections("")
	if e != nil {
		t.Fatal(e)
	}
	s.Manager.Configure(config)
	t.Cleanup(s.Manager.Close)
	p := pairPhone(t, s, ts)
	p.connect(t, ts, s)
	catalog := p.read(t, "sessions")["agents"].([]any)
	if len(catalog) != 6 {
		t.Fatal("catalog omitted an adapter")
	}
	for _, entry := range catalog {
		v := entry.(map[string]any)
		for _, key := range []string{"client_secret", "url", "access_token", "password"} {
			if _, ok := v[key]; ok {
				t.Fatal("catalog contains private connection data")
			}
		}
	}
	p.send(t, Request{ID: "authorization", Method: "agent_login", Agent: "workbuddy"})
	response := p.read(t, "error")
	if response["id"] != "authorization" || !strings.Contains(response["message"].(string), "开放平台") {
		t.Fatal("unconfigured authorization has no actionable error")
	}
	res, e := ts.Client().Get(ts.URL + "/v1/agents/workbuddy/callback?state=foreign&code=fixture-code")
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	if res.StatusCode == 200 || res.Header.Get("Referrer-Policy") != "no-referrer" || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("unsafe OAuth callback")
	}
}
