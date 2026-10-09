package relay

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"agentdeck/internal/secure"
	"github.com/gorilla/websocket"
)

type Message struct {
	Kind  string `json:"kind"`
	ID    string `json:"id,omitempty"`
	Grant string `json:"grant,omitempty"`
	Data  []byte `json:"data,omitempty"`
}

// Peer multiplexes bounded virtual TCP streams. Every frame has directional
// AEAD, a connection salt, epoch key rotation and strict sequence validation.
// Payload HTTP/WS bytes already contain Local/App ciphertext.
type Peer struct {
	ws             *websocket.Conn
	root, salt     []byte
	direction      string
	rotation       time.Duration
	Clock          *secure.Clock
	sent, received uint64
	writeMu        sync.Mutex
	mu             sync.Mutex
	streams        map[string]*Conn
	closed         chan struct{}
	once           sync.Once
	OnOpen         func(*Conn) error
}

func NewPeer(ws *websocket.Conn, root, salt []byte, direction string, rotation time.Duration) *Peer {
	return &Peer{Clock: secure.NewClock(), ws: ws, root: root, salt: salt, direction: direction, rotation: rotation, streams: map[string]*Conn{}, closed: make(chan struct{})}
}
func (p *Peer) Done() <-chan struct{} { return p.closed }

func (p *Peer) CloseGrant(grant string) {
	p.mu.Lock()
	list := []*Conn{}
	for _, c := range p.streams {
		if c.Grant == grant {
			list = append(list, c)
		}
	}
	p.mu.Unlock()
	for _, c := range list {
		c.Close()
	}
}
func (p *Peer) Send(m Message) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	select {
	case <-p.closed:
		return net.ErrClosed
	default:
	}
	p.sent++
	ep := p.Clock.Epoch(p.rotation)
	b, e := secure.SealJSON(secure.Traffic(p.root, p.salt, "tunnel-"+p.direction, ep), "tunnel/"+p.direction, ep, p.sent, m)
	if e != nil {
		return e
	}
	_ = p.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	e = p.ws.WriteJSON(b)
	if e != nil {
		go p.Close()
	}
	return e
}
func (p *Peer) Close() {
	p.once.Do(func() {
		close(p.closed)
		_ = p.ws.Close()
		p.mu.Lock()
		list := make([]*Conn, 0, len(p.streams))
		for _, c := range p.streams {
			list = append(list, c)
		}
		p.streams = map[string]*Conn{}
		p.mu.Unlock()
		for _, c := range list {
			c.stop(false)
		}
	})
}
func (p *Peer) Open(grant string) (*Conn, error) {
	id := secure.ID()
	c := newConn(p, id, grant)
	p.mu.Lock()
	select {
	case <-p.closed:
		p.mu.Unlock()
		return nil, net.ErrClosed
	default:
	}
	if len(p.streams) >= MaxStreams {
		p.mu.Unlock()
		return nil, errors.New("local stream capacity reached")
	}
	p.streams[id] = c
	p.mu.Unlock()
	if e := p.Send(Message{Kind: "open", ID: id, Grant: grant}); e != nil {
		c.stop(false)
		return nil, e
	}
	return c, nil
}
func (p *Peer) forget(id string) { p.mu.Lock(); delete(p.streams, id); p.mu.Unlock() }
func (p *Peer) Run() error {
	defer p.Close()
	p.ws.SetReadLimit(1 << 20)
	_ = p.ws.SetReadDeadline(time.Now().Add(90 * time.Second))
	go func() {
		tick := time.NewTicker(20 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-p.closed:
				return
			case <-tick.C:
				if p.Send(Message{Kind: "ping"}) != nil {
					return
				}
			}
		}
	}()
	remote := "g2l"
	if p.direction == "g2l" {
		remote = "l2g"
	}
	for {
		var b secure.Box
		if e := p.ws.ReadJSON(&b); e != nil {
			return e
		}
		ep := p.Clock.Epoch(p.rotation)
		if b.Seq != p.received+1 || b.Epoch < ep-1 || b.Epoch > ep+1 {
			return errors.New("tunnel replay or epoch rejected")
		}
		plain, e := secure.Open(secure.Traffic(p.root, p.salt, "tunnel-"+remote, b.Epoch), "tunnel/"+remote, b)
		if e != nil {
			return errors.New("tunnel authentication rejected")
		}
		var m Message
		if json.Unmarshal(plain, &m) != nil || len(m.Data) > ChunkSize {
			return errors.New("invalid tunnel frame")
		}
		p.received = b.Seq
		_ = p.ws.SetReadDeadline(time.Now().Add(90 * time.Second))
		if m.Kind == "ping" {
			continue
		}
		if !ValidID(m.ID) {
			return errors.New("invalid tunnel stream ID")
		}
		p.mu.Lock()
		c := p.streams[m.ID]
		if m.Kind == "open" {
			if c != nil || p.OnOpen == nil || !ValidID(m.Grant) || len(p.streams) >= MaxStreams {
				p.mu.Unlock()
				return errors.New("invalid tunnel open")
			}
			c = newConn(p, m.ID, m.Grant)
			p.streams[m.ID] = c
			p.mu.Unlock()
			if p.OnOpen(c) != nil {
				c.Close()
			}
			continue
		}
		p.mu.Unlock()
		if c == nil {
			continue
		} // Late data after local cancellation is discarded.
		switch m.Kind {
		case "data":
			select {
			case c.incoming <- m.Data:
			case <-c.closed:
			default:
				c.Close()
			}
		case "credit":
			select {
			case c.credits <- struct{}{}:
			default:
				return errors.New("invalid stream credit")
			}
		case "close":
			c.stop(false)
		default:
			return errors.New("unknown tunnel frame")
		}
	}
}

type Conn struct {
	credits                     chan struct{}
	writeChanged                chan struct{}
	peer                        *Peer
	ID, Grant                   string
	incoming                    chan []byte
	buffer                      *bytes.Reader
	closed                      chan struct{}
	once                        sync.Once
	readMu                      sync.Mutex
	deadlineMu                  sync.Mutex
	readDeadline, writeDeadline time.Time
	changed                     chan struct{}
}

func newConn(p *Peer, id, grant string) *Conn {
	c := &Conn{peer: p, ID: id, Grant: grant, incoming: make(chan []byte, 8), closed: make(chan struct{}), changed: make(chan struct{}, 1), credits: make(chan struct{}, 8), writeChanged: make(chan struct{}, 1)}
	for i := 0; i < 8; i++ {
		c.credits <- struct{}{}
	}
	return c
}
func (c *Conn) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for {
		if c.buffer != nil && c.buffer.Len() > 0 {
			n, err := c.buffer.Read(b)
			if c.buffer.Len() == 0 {
				_ = c.peer.Send(Message{Kind: "credit", ID: c.ID})
			}
			return n, err
		}
		// Drain already-received data before EOF so a final HTTP response is retained.
		select {
		case d := <-c.incoming:
			c.buffer = bytes.NewReader(d)
			continue
		default:
		}
		c.deadlineMu.Lock()
		deadline := c.readDeadline
		c.deadlineMu.Unlock()
		var timer *time.Timer
		var timeout <-chan time.Time
		if !deadline.IsZero() {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return 0, &timeoutError{}
			}
			timer = time.NewTimer(remaining)
			timeout = timer.C
		}
		select {
		case d := <-c.incoming:
			if timer != nil {
				timer.Stop()
			}
			c.buffer = bytes.NewReader(d)
		case <-c.closed:
			if timer != nil {
				timer.Stop()
			}
			select {
			case d := <-c.incoming:
				c.buffer = bytes.NewReader(d)
				continue
			default:
				return 0, io.EOF
			}
		case <-c.changed:
			if timer != nil {
				timer.Stop()
			}
		case <-timeout:
			return 0, &timeoutError{}
		}
	}
}
func (c *Conn) credit() error {
	for {
		c.deadlineMu.Lock()
		deadline := c.writeDeadline
		c.deadlineMu.Unlock()
		var timer *time.Timer
		var timeout <-chan time.Time
		if !deadline.IsZero() {
			d := time.Until(deadline)
			if d <= 0 {
				return &timeoutError{}
			}
			timer = time.NewTimer(d)
			timeout = timer.C
		}
		select {
		case <-c.credits:
			if timer != nil {
				timer.Stop()
			}
			return nil
		case <-c.closed:
			if timer != nil {
				timer.Stop()
			}
			return net.ErrClosed
		case <-c.writeChanged:
			if timer != nil {
				timer.Stop()
			}
		case <-timeout:
			return &timeoutError{}
		}
	}
}
func (c *Conn) Write(b []byte) (int, error) {
	sent := 0
	for len(b) > 0 {
		select {
		case <-c.closed:
			return sent, net.ErrClosed
		default:
		}
		c.deadlineMu.Lock()
		deadline := c.writeDeadline
		c.deadlineMu.Unlock()
		if !deadline.IsZero() && time.Now().After(deadline) {
			return sent, &timeoutError{}
		}
		if e := c.credit(); e != nil {
			return sent, e
		}
		n := len(b)
		if n > ChunkSize {
			n = ChunkSize
		}
		if e := c.peer.Send(Message{Kind: "data", ID: c.ID, Data: b[:n]}); e != nil {
			return sent, e
		}
		sent += n
		b = b[n:]
	}
	return sent, nil
}
func (c *Conn) stop(notify bool) {
	c.once.Do(func() {
		close(c.closed)
		c.peer.forget(c.ID)
		if notify {
			_ = c.peer.Send(Message{Kind: "close", ID: c.ID})
		}
	})
}
func (c *Conn) Close() error         { c.stop(true); return nil }
func (c *Conn) LocalAddr() net.Addr  { return relayAddr("relay") }
func (c *Conn) RemoteAddr() net.Addr { return relayAddr("peer") }
func (c *Conn) SetDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	c.readDeadline = t
	c.writeDeadline = t
	c.deadlineMu.Unlock()
	c.signal()
	c.signalWrite()
	return nil
}
func (c *Conn) SetReadDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	c.readDeadline = t
	c.deadlineMu.Unlock()
	c.signal()
	return nil
}
func (c *Conn) SetWriteDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	c.writeDeadline = t
	c.deadlineMu.Unlock()
	c.signalWrite()
	return nil
}
func (c *Conn) signalWrite() {
	select {
	case c.writeChanged <- struct{}{}:
	default:
	}
}
func (c *Conn) signal() {
	select {
	case c.changed <- struct{}{}:
	default:
	}
}

type relayAddr string

func (a relayAddr) Network() string { return "agentdeck-relay" }
func (a relayAddr) String() string  { return string(a) }

type timeoutError struct{}

func (*timeoutError) Error() string   { return "relay deadline exceeded" }
func (*timeoutError) Timeout() bool   { return true }
func (*timeoutError) Temporary() bool { return true }

type Listener struct {
	in     chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func NewListener() *Listener {
	return &Listener{in: make(chan net.Conn, MaxStreams), closed: make(chan struct{})}
}
func (l *Listener) Push(c net.Conn) error {
	select {
	case <-l.closed:
		return net.ErrClosed
	default:
	}
	select {
	case l.in <- c:
		return nil
	case <-l.closed:
		return net.ErrClosed
	default:
		return errors.New("relay listener full")
	}
}
func (l *Listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.in:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}
func (l *Listener) Close() error   { l.once.Do(func() { close(l.closed) }); return nil }
func (l *Listener) Addr() net.Addr { return relayAddr("relay") }
