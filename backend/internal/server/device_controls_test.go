package server

import (
	"agentdeck/internal/relay"
	"agentdeck/internal/secure"
	"github.com/gorilla/websocket"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDeviceRenamePushAndImmediateRevocation(t *testing.T) {
	s, ts := setupServer(t)
	p := pairPhone(t, s, ts)
	p.connect(t, ts, s)
	p.read(t, "sessions")
	p.send(t, map[string]any{"method": "rename_device", "id": "rename", "text": "我的 OnePlus 7"})
	r := p.read(t, "result")
	if r["result"].(map[string]any)["name"] != "我的 OnePlus 7" {
		t.Fatal("encrypted rename failed")
	}
	if _, err := s.Store.RenameDevice(p.device, "电脑端名称"); err != nil {
		t.Fatal(err)
	}
	r = p.read(t, "device")
	if r["name"] != "电脑端名称" {
		r = p.read(t, "device")
		if r["name"] != "电脑端名称" {
			t.Fatal("owner rename not pushed")
		}
	}
	if _, err := s.Store.RenameDevice(p.device, "name\nspoof"); err == nil {
		t.Fatal("control characters accepted")
	}
	unused := &phone{device: p.device, root: p.root}
	unused.login(t, ts) // A valid, unconsumed connection ticket.
	if err := s.revokeDevice(p.device); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	_, ticketRemains := s.tickets[unused.ticket]
	s.mu.Unlock()
	if ticketRemains {
		t.Fatal("unused ticket survived revocation")
	}
	box, err := secure.SealJSON(p.root, "hello/"+p.device, 0, 0, map[string]any{"time": time.Now().Unix(), "challenge": secure.ID()})
	if err != nil {
		t.Fatal(err)
	}
	post(t, ts, "/v1/session", map[string]any{"device": p.device, "box": box}, 401)
	p.ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	closed := false
	for i := 0; i < 5; i++ {
		_, _, err := p.ws.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, 4003) {
				t.Fatal("revocation close code", err)
			}
			closed = true
			break
		}
	}
	if !closed {
		t.Fatal("revoked stream remained open")
	}
	v, err := s.appConnections()
	if err != nil || v.Paired != 0 || v.Online != 0 {
		t.Fatal("revoked device remains in roster")
	}
	if _, err = s.trackAppConnection(p.device, "203.0.113.1", func() {}); err == nil {
		t.Fatal("revoked stream could register")
	}
}

func TestConnectionIPHeaderRequiresAuthenticatedRelay(t *testing.T) {
	r := httptest.NewRequest("GET", "https://local/v1/stream", nil)
	r.RemoteAddr = "192.0.2.8:9000"
	r.Header.Set("X-AgentDeck-Client-IP", "203.0.113.9")
	if connectionIP(r) != "192.0.2.8" {
		t.Fatal("direct client spoofed connection IP")
	}
	r = r.WithContext(relay.WithGrant(r.Context(), "authenticated-grant"))
	if connectionIP(r) != "203.0.113.9" {
		t.Fatal("gateway-observed IP not projected")
	}
	r.Header.Set("X-AgentDeck-Client-IP", "injected\nvalue")
	if connectionIP(r) != "" {
		t.Fatal("invalid IP accepted")
	}
}
