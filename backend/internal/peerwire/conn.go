package peerwire

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

	"agentdeck/internal/secure"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

type Answer struct {
	ID  string `json:"peer_id"`
	SDP string `json:"sdp"`
}

type candidate struct {
	id        string
	pc        *webrtc.PeerConnection
	dc        *webrtc.DataChannel
	active    bool
	accepting bool
	assembler Assembler
	receiveMu sync.Mutex
}

// Conn preserves the original traffic salt, sequence counters, request IDs,
// subscriptions and download handles during a one-way upgrade. A failed direct
// transport closes this logical stream: clients reconnect with fresh crypto and
// retry only through the existing durable command/outbox deduplication.
type Conn struct {
	ws           *websocket.Conn
	mu           sync.Mutex
	writeMu      sync.Mutex
	peer         *candidate
	attempts     int
	deadline     time.Time
	input        chan []byte
	done         chan struct{}
	once         sync.Once
	startOnce    sync.Once
	next         uint64
	pending      map[uint64][]byte
	pendingBytes int
}

func New(ws *websocket.Conn) *Conn {
	c := &Conn{ws: ws, input: make(chan []byte, 32), done: make(chan struct{}), pending: map[uint64][]byte{}}
	ws.SetReadLimit(1 << 20)

	return c
}

func (c *Conn) start() {
	c.startOnce.Do(func() {
		go func() {
			defer c.Close()
			for {
				_, data, err := c.ws.ReadMessage()
				if err != nil {
					return
				}
				if !c.push(data) {
					return
				}
			}
		}()
	})
}

func (c *Conn) push(data []byte) bool {
	select {
	case c.input <- data:
		return true
	case <-c.done:
		return false
	default:
		_ = c.Close()
		return false
	}
}

func (c *Conn) ReadJSON(out any) error {
	c.start()
	for {
		if data, ok := c.pending[c.next+1]; ok {
			delete(c.pending, c.next+1)
			c.pendingBytes -= len(data)
			c.next++
			return json.Unmarshal(data, out)
		}
		select {
		case <-c.done:
			return net.ErrClosed
		case data := <-c.input:
			var envelope struct {
				Seq uint64 `json:"seq"`
			}
			if json.Unmarshal(data, &envelope) != nil || envelope.Seq <= c.next || envelope.Seq > c.next+32 || len(data) > 1<<20 {
				return errors.New("peer sequence rejected")
			}
			if _, duplicate := c.pending[envelope.Seq]; duplicate {
				return errors.New("peer replay rejected")
			}
			// Only the bounded handover window can arrive out of order. AEAD and
			// epoch validation still run in Server.stream before any request executes.
			c.pendingBytes += len(data)
			if c.pendingBytes > 4<<20 {
				return errors.New("peer reorder window exceeded")
			}
			c.pending[envelope.Seq] = data
		}
	}
}

func (c *Conn) WriteJSON(value any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	p, deadline := c.peer, c.deadline
	direct := p != nil && p.active
	c.mu.Unlock()
	if !direct {
		return c.ws.WriteJSON(value)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	err = fragments(data, func(fragment []byte) error {
		for p.dc.BufferedAmount() > 256<<10 {
			if !deadline.IsZero() && time.Now().After(deadline) {
				return context.DeadlineExceeded
			}
			select {
			case <-c.done:
				return net.ErrClosed
			case <-time.After(10 * time.Millisecond):
			}
		}
		return p.dc.Send(fragment)
	})
	if err != nil {
		_ = c.Close()
	}
	return err
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	return c.ws.SetWriteDeadline(t)
}
func (c *Conn) SetReadDeadline(t time.Time) error   { return c.ws.SetReadDeadline(t) }
func (c *Conn) SetPongHandler(f func(string) error) { c.ws.SetPongHandler(f) }
func (c *Conn) SetReadLimit(n int64)                { c.ws.SetReadLimit(n) }
func (c *Conn) WriteControl(kind int, data []byte, deadline time.Time) error {
	return c.ws.WriteControl(kind, data, deadline)
}
func (c *Conn) Close() error {
	c.once.Do(func() {
		close(c.done)
		_ = c.ws.Close()
		c.mu.Lock()
		p := c.peer
		c.peer = nil
		c.mu.Unlock()
		if p != nil {
			go func() { _ = p.pc.Close() }()
		}
	})
	return nil
}

// Offer is reached only through the already paired end-to-end encrypted
// WebSocket. Thus both SDP fingerprints are authenticated by the device root;
// Gateway can relay negotiation but cannot replace its peer identity.
func (c *Conn) Offer(ctx context.Context, sdp string, servers []string) (Answer, error) {
	if len(sdp) == 0 || len(sdp) > 64<<10 {
		return Answer{}, errors.New("invalid peer offer")
	}
	c.mu.Lock()
	if c.peer != nil || c.attempts >= 3 {
		c.mu.Unlock()
		return Answer{}, errors.New("peer negotiation already active")
	}
	c.attempts++
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{ICEServers: []webrtc.ICEServer{{URLs: servers}}})
	if err != nil {
		c.mu.Unlock()
		return Answer{}, err
	}
	p := &candidate{id: secure.ID(), pc: pc, assembler: Assembler{Limit: 1 << 20}}
	c.peer = p
	c.mu.Unlock()
	success := false
	defer func() {
		if !success {
			c.drop(p)
		}
	}()
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != "agentdeck-v1" || !dc.Ordered() || dc.MaxRetransmits() != nil || dc.MaxPacketLifeTime() != nil {
			_ = dc.Close()
			return
		}
		c.mu.Lock()
		if c.peer != p || p.dc != nil {
			c.mu.Unlock()
			_ = dc.Close()
			return
		}
		p.dc = dc
		c.mu.Unlock()
		dc.OnMessage(func(message webrtc.DataChannelMessage) {
			c.mu.Lock()
			active := c.peer == p && p.accepting
			c.mu.Unlock()
			if !active || message.IsString {
				_ = c.Close()
				return
			}
			p.receiveMu.Lock()
			data, err := p.assembler.Push(message.Data)
			p.receiveMu.Unlock()
			if err != nil {
				_ = c.Close()
				return
			}
			if data != nil {
				c.push(data)
			}
		})
		dc.OnClose(func() { c.drop(p) })
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateDisconnected {
			time.AfterFunc(3*time.Second, func() {
				if pc.ConnectionState() == webrtc.PeerConnectionStateDisconnected {
					c.drop(p)
				}
			})
		}
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			c.drop(p)
		}
	})
	if err = pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp}); err != nil {
		return Answer{}, err
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return Answer{}, err
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err = pc.SetLocalDescription(answer); err != nil {
		return Answer{}, err
	}
	select {
	case <-ctx.Done():
		return Answer{}, ctx.Err()
	case <-c.done:
		return Answer{}, net.ErrClosed
	case <-gathered:
	}
	success = true
	// A missing client or failed ICE attempt cannot keep native resources alive.
	time.AfterFunc(20*time.Second, func() {
		c.mu.Lock()
		expired := c.peer == p && !p.active
		c.mu.Unlock()
		if expired {
			c.drop(p)
		}
	})
	return Answer{ID: p.id, SDP: pc.LocalDescription().SDP}, nil
}

func (c *Conn) drop(p *candidate) {
	c.mu.Lock()
	if c.peer != p {
		c.mu.Unlock()
		return
	}
	active := p.active
	c.peer = nil
	c.mu.Unlock()
	go func() { _ = p.pc.Close() }()
	if active {
		_ = c.Close()
	}
}

func (c *Conn) Ready(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	ready := c.peer != nil && c.peer.id == id && !c.peer.active && c.peer.dc != nil && c.peer.dc.ReadyState() == webrtc.DataChannelStateOpen
	if ready {
		c.peer.accepting = true
	}
	return ready
}

// The commit acknowledgement must finish on WebSocket before Activate. Any
// already queued client frames on the two paths are merged by the AEAD sequence.
func (c *Conn) Activate(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.peer == nil || c.peer.id != id || c.peer.dc == nil || c.peer.dc.ReadyState() != webrtc.DataChannelStateOpen {
		return errors.New("peer not ready")
	}
	c.peer.active = true
	return nil
}

func (c *Conn) DirectIP() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.peer == nil || !c.peer.active {
		return ""
	}
	pair, err := c.peer.pc.SCTP().Transport().ICETransport().GetSelectedCandidatePair()
	if err != nil || pair == nil {
		return ""
	}
	return pair.Remote.Address
}

func (c *Conn) Direct() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.peer != nil && c.peer.active }
