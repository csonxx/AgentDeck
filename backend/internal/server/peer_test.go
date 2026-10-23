package server

import (
	"agentdeck/internal/agent"
	"agentdeck/internal/peerwire"
	"agentdeck/internal/relay"
	"agentdeck/internal/secure"
	"agentdeck/internal/store"
	"context"
	"encoding/binary"
	"encoding/json"
	"github.com/pion/webrtc/v4"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type directPhone struct {
	p      *phone
	pc     *webrtc.PeerConnection
	dc     *webrtc.DataChannel
	frames chan []byte
	closed chan struct{}
	wsDone chan struct{}
}

func upgradePhone(t *testing.T, s *Server, ts *httptest.Server, p *phone) *directPhone {
	t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	dc, err := pc.CreateDataChannel("agentdeck-v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	d := &directPhone{p: p, pc: pc, dc: dc, frames: make(chan []byte, 64), closed: make(chan struct{}), wsDone: make(chan struct{})}
	var mu sync.Mutex
	var once sync.Once
	a := peerwire.Assembler{}
	dc.OnMessage(func(m webrtc.DataChannelMessage) {
		mu.Lock()
		data, err := a.Push(m.Data)
		mu.Unlock()
		if err == nil && data != nil {
			select {
			case d.frames <- data:
			case <-d.closed:
			}
		}
	})
	dc.OnClose(func() { once.Do(func() { close(d.closed) }) })
	open := make(chan struct{})
	dc.OnOpen(func() { close(open) })
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err = pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gathered:
	case <-time.After(5 * time.Second):
		t.Fatal("host candidate gathering timeout")
	}
	p.send(t, Request{ID: "offer", Method: "peer_offer", SDP: pc.LocalDescription().SDP})
	answer := p.read(t, "result")["result"].(map[string]any)
	if err = pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer["sdp"].(string)}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-open:
	case <-time.After(5 * time.Second):
		t.Fatal("host ICE did not connect")
	}
	// A correct peer ID is necessary even inside an authenticated stream.
	p.send(t, Request{ID: "bad-commit", Method: "peer_commit", PeerID: "different"})
	p.read(t, "error")
	p.send(t, Request{ID: "commit", Method: "peer_commit", PeerID: answer["peer_id"].(string)})
	p.read(t, "result")
	p.ws.SetReadDeadline(time.Time{})
	go func() {
		defer close(d.wsDone)
		for {
			if _, _, err := p.ws.ReadMessage(); err != nil {
				return
			}
		}
	}()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		state, _ := s.appConnections()
		if len(state.Devices) > 0 && len(state.Devices[0].Transports) > 0 && state.Devices[0].Transports[0] == "webrtc" {
			return d
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("owner status did not report actual direct transport")
	return nil
}
func (d *directPhone) seal(t *testing.T, v any) []byte {
	t.Helper()
	d.p.sent++
	epoch := time.Now().UnixMilli() / 1000
	box, err := secure.SealJSON(secure.Traffic(d.p.root, d.p.salt, "c2s", epoch), "c2s", epoch, d.p.sent, v)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(box)
	return data
}
func (d *directPhone) sendRaw(t *testing.T, data []byte) {
	t.Helper()
	for offset := 0; offset < len(data); {
		end := min(len(data), offset+peerwire.FragmentSize-8)
		p := make([]byte, 8+end-offset)
		binary.BigEndian.PutUint32(p, uint32(len(data)))
		binary.BigEndian.PutUint32(p[4:], uint32(offset))
		copy(p[8:], data[offset:end])
		if err := d.dc.Send(p); err != nil {
			t.Fatal(err)
		}
		offset = end
	}
}
func (d *directPhone) read(t *testing.T, kind string) map[string]any {
	t.Helper()
	for i := 0; i < 60; i++ {
		select {
		case data := <-d.frames:
			var box secure.Box
			if json.Unmarshal(data, &box) != nil || box.Seq != d.p.received+1 {
				t.Fatal("direct ciphertext sequence gap")
			}
			d.p.received = box.Seq
			raw, err := secure.Open(secure.Traffic(d.p.root, d.p.salt, "s2c", box.Epoch), "s2c", box)
			if err != nil {
				t.Fatal(err)
			}
			var v map[string]any
			if json.Unmarshal(raw, &v) != nil {
				t.Fatal("direct payload malformed")
			}
			if v["type"] == kind {
				return v
			}
		case <-time.After(5 * time.Second):
			t.Fatal("direct response timeout")
		}
	}
	t.Fatal("direct response missing")
	return nil
}
func TestPeerEncryptedHandoverLargeHistoryAndRevocation(t *testing.T) {
	s, ts := setupServer(t)
	p := pairPhone(t, s, ts)
	p.connect(t, ts, s)
	p.read(t, "sessions")
	d := upgradePhone(t, s, ts, p)
	// Model a queued WebSocket request crossing the first direct request. Both
	// must be applied in the original encrypted sequence without duplicating work.
	first := d.seal(t, Request{Method: "ping", ID: "first"})
	second := d.seal(t, Request{Method: "ping", ID: "second"})
	if err := p.ws.WriteMessage(1, second); err != nil {
		t.Fatal(err)
	}
	d.sendRaw(t, first)
	if d.read(t, "pong")["id"] != "first" || d.read(t, "pong")["id"] != "second" {
		t.Fatal("handover reordered requests")
	}
	body := strings.Repeat("Synthetic image/history payload. ", 20000)
	_, _, err := s.Store.Append(store.Event{ID: "large", Session: "history", Kind: "message", Role: "assistant", Text: body})
	if err != nil {
		t.Fatal(err)
	}
	d.sendRaw(t, d.seal(t, Request{ID: "history", Method: "history", Session: "history"}))
	result := d.read(t, "result")
	raw, _ := json.Marshal(result)
	if len(raw) < peerwire.FragmentSize || !strings.Contains(string(raw), strings.TrimSpace(body)) {
		t.Fatalf("fragmented history mismatch: id=%v wire_json_bytes=%d expected_text_bytes=%d", result["id"], len(raw), len(body))
	}
	if err := s.revokeDevice(p.device); err != nil {
		t.Fatal(err)
	}
	select {
	case <-d.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("revoked peer stayed connected")
	}
}
func TestPeerFailureFallsBackWithCommandDedup(t *testing.T) {
	s, ts := setupServer(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	native := &slowDesktop{project: root, entered: make(chan struct{}), release: make(chan struct{})}
	close(native.release)
	s.Manager = &agent.Manager{Context: context.Background(), Store: s.Store, Roots: []string{root}, Desktop: native}
	t.Cleanup(s.Manager.Close)
	_, _, err = s.Store.Append(store.Event{ID: "one", Session: "chat", Agent: "codex", Kind: "message", Role: "assistant", Text: "Synthetic", Project: root, Source: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	p := pairPhone(t, s, ts)
	p.connect(t, ts, s)
	p.read(t, "sessions")
	d := upgradePhone(t, s, ts, p)
	request := Request{ID: "same-request", Method: "send", Session: "chat", Text: "Synthetic input"}
	d.sendRaw(t, d.seal(t, request))
	d.read(t, "result")
	d.pc.Close()
	select {
	case <-d.wsDone:
	case <-time.After(5 * time.Second):
		t.Fatal("failed peer did not close lifeline")
	}
	p.connect(t, ts, s)
	p.read(t, "sessions")
	p.send(t, request)
	p.read(t, "result")
	if native.calls.Load() != 1 {
		t.Fatal("fallback duplicated native send")
	}
}
func TestDirectPolicyDisabledKeepsExistingRelayStream(t *testing.T) {
	s, ts := setupServer(t)
	s.DirectConfig = func() relay.DirectConfig { return relay.DirectConfig{Version: 1, Enabled: false} }
	p := pairPhone(t, s, ts)
	p.connect(t, ts, s)
	p.read(t, "sessions")
	p.send(t, Request{ID: "offer", Method: "peer_offer", SDP: "invalid"})
	p.read(t, "error")
	p.send(t, Request{ID: "ping", Method: "ping"})
	p.read(t, "pong")
}
