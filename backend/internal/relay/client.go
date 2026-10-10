package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"agentdeck/internal/secure"
	"github.com/gorilla/websocket"
)

type Client struct {
	Link   Link
	HTTP   *http.Client
	Dialer websocket.Dialer
	mu     sync.Mutex
	timeMu sync.Mutex
	peer   *Peer
	status ConnectionStatus
}

type ConnectionStatus struct {
	ConnectedAt    string `json:"connected_at,omitempty"`
	DisconnectedAt string `json:"disconnected_at,omitempty"`
	Attempts       uint64 `json:"attempts"`
	LastError      string `json:"last_error,omitempty"`
}

func (c *Client) Status() ConnectionStatus { c.mu.Lock(); defer c.mu.Unlock(); return c.status }

func NewClient(link Link) (*Client, error) {
	if e := link.Validate(); e != nil {
		return nil, e
	}
	tlsConfig, e := TLSConfig(link.TLSTrust, link.Pin)
	if e != nil {
		return nil, e
	}
	return &Client{Link: link, HTTP: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsConfig}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Dialer: websocket.Dialer{TLSClientConfig: tlsConfig, HandshakeTimeout: 15 * time.Second}}, nil
}
func (c *Client) Online() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.peer != nil }
func (c *Client) Register(ctx context.Context, invite string, expires int64) (Grant, error) {
	var g Grant
	key, _ := secure.B64.DecodeString(c.Link.Key)
	clock, e := c.syncClock(ctx)
	if e != nil {
		return g, e
	}
	// Calculate lifetime against the Gateway anchor used to create the invite.
	// Gateway issues its own expiry, avoiding off-by-one rejection from RTT skew.
	source := c.CurrentClock()
	if source == nil {
		source = clock
	}
	remaining := expires - source.Unix()
	if remaining <= 0 || remaining > 300 {
		return g, errors.New("invalid invitation lifetime")
	}
	body, e := secure.SealJSON(key, "grant-request/"+c.Link.Local, 0, 0, map[string]any{"invite": invite, "ttl_seconds": remaining})
	if e != nil {
		return g, e
	}
	raw, _ := json.Marshal(body)
	path := "/v2/local/grants"
	proof, e := AuthAt(key, "local/"+c.Link.Local, "POST", path, raw, "", clock.Unix())
	if e != nil {
		return g, e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", c.Link.URL+path, bytes.NewReader(raw))
	if e != nil {
		return g, e
	}
	req.Header.Set("Authorization", "Local "+c.Link.Local)
	req.Header.Set("X-Relay-Auth", proof)
	req.Header.Set("Content-Type", "application/json")
	res, e := c.HTTP.Do(req)
	if e != nil {
		return g, errors.New("gateway unavailable for binding")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return g, errors.New("gateway rejected binding")
	}
	var box secure.Box
	if json.NewDecoder(io.LimitReader(res.Body, 16<<10)).Decode(&box) != nil {
		return g, errors.New("invalid gateway binding response")
	}
	plain, e := secure.Open(key, "grant-response/"+c.Link.Local, box)
	if e != nil {
		return g, e
	}
	var reply struct {
		Grant     Grant  `json:"grant"`
		Challenge string `json:"challenge"`
	}
	if json.Unmarshal(plain, &reply) != nil || reply.Challenge != Hash(raw) || reply.Grant.Local != c.Link.Local || reply.Grant.Tenant != c.Link.Tenant || reply.Grant.Invite != invite {
		return g, errors.New("gateway binding identity mismatch")
	}
	return reply.Grant, nil
}
func (c *Client) connect(ctx context.Context, handler http.Handler) error {
	key, _ := secure.B64.DecodeString(c.Link.Key)
	clock, e := c.syncClock(ctx)
	if e != nil {
		return e
	}
	path := "/v2/local/tunnel"
	proof, e := AuthAt(key, "local/"+c.Link.Local, "GET", path, nil, "", clock.Unix())
	if e != nil {
		return e
	}
	ws, res, e := c.Dialer.DialContext(ctx, strings.Replace(c.Link.URL, "https://", "wss://", 1)+path, http.Header{"Authorization": []string{"Local " + c.Link.Local}, "X-Relay-Auth": []string{proof}})
	if e != nil {
		if res != nil {
			res.Body.Close()
			return fmt.Errorf("gateway tunnel handshake HTTP %d", res.StatusCode)
		}
		return errors.New("gateway tunnel handshake failed")
	}
	defer ws.Close()
	ws.SetReadLimit(1 << 20)
	_ = ws.SetReadDeadline(time.Now().Add(15 * time.Second))
	var box secure.Box
	if e = ws.ReadJSON(&box); e != nil {
		return errors.New("gateway tunnel welcome unavailable")
	}
	plain, e := secure.Open(key, "tunnel-welcome/"+c.Link.Local, box)
	if e != nil {
		return errors.New("gateway tunnel welcome rejected")
	}
	var welcome struct {
		Salt      string `json:"salt"`
		Rotation  int64  `json:"rotation_ms"`
		Challenge string `json:"challenge"`
	}
	if json.Unmarshal(plain, &welcome) != nil || welcome.Challenge != Hash([]byte(proof)) || welcome.Rotation < 1000 {
		return errors.New("gateway tunnel identity mismatch")
	}
	salt, e := secure.B64.DecodeString(welcome.Salt)
	if e != nil || len(salt) != 32 {
		return errors.New("invalid tunnel salt")
	}
	peer := NewPeer(ws, key, salt, "l2g", time.Duration(welcome.Rotation)*time.Millisecond)
	peer.Clock = clock
	ln := NewListener()
	peer.OnOpen = func(conn *Conn) error { return ln.Push(conn) }
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 90 * time.Second, ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
		return WithClock(WithGrant(ctx, conn.(*Conn).Grant), clock)
	}}
	go func() { _ = srv.Serve(ln) }()
	c.mu.Lock()
	c.peer = peer
	c.status.ConnectedAt = time.Now().UTC().Format(time.RFC3339)
	c.status.LastError = ""
	c.mu.Unlock()
	log.Printf("S-LOCAL linked tenant=%s local=%s", c.Link.Tenant, c.Link.Local)
	defer func() {
		c.mu.Lock()
		if c.peer == peer {
			c.peer = nil
		}
		c.mu.Unlock()
		ln.Close()
		srv.Close()
		peer.Close()
	}()
	go func() {
		select {
		case <-ctx.Done():
			peer.Close()
		case <-peer.Done():
		}
	}()
	return peer.Run()
}
func (c *Client) Run(ctx context.Context, handler http.Handler) {
	delay := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		c.mu.Lock()
		c.status.Attempts++
		c.mu.Unlock()
		err := c.connect(ctx, handler)
		if ctx.Err() != nil {
			return
		}
		reason := connectionReason(err)
		c.mu.Lock()
		c.status.DisconnectedAt, c.status.LastError = time.Now().UTC().Format(time.RFC3339), reason
		c.mu.Unlock()
		log.Printf("S-LOCAL tunnel disconnected local=%s reason=%s retry_ms=%d; reconnecting", c.Link.Local, reason, delay.Milliseconds())
		if time.Since(started) > time.Minute {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < 30*time.Second {
			delay *= 2
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
	}
}

func connectionReason(err error) string {
	if err == nil {
		return "tunnel_closed"
	}
	switch err.Error() {
	case "gateway clock unavailable":
		return "clock_unavailable"
	case "gateway clock rejected":
		return "clock_authentication_rejected"
	case "gateway tunnel handshake HTTP 401":
		return "tunnel_authentication_rejected"
	case "gateway tunnel handshake HTTP 409":
		return "workstation_already_connected"
	case "gateway tunnel handshake HTTP 503":
		return "gateway_unavailable"
	case "gateway tunnel handshake failed":
		return "tunnel_network_or_tls_failed"
	case "gateway tunnel welcome unavailable":
		return "tunnel_welcome_unavailable"
	case "gateway tunnel welcome rejected", "gateway tunnel identity mismatch":
		return "tunnel_identity_rejected"
	default:
		return "tunnel_disconnected"
	}
}

func (c *Client) syncClock(ctx context.Context) (*secure.Clock, error) {
	c.timeMu.Lock()
	defer c.timeMu.Unlock()
	key, _ := secure.B64.DecodeString(c.Link.Key)
	return SyncClock(ctx, c.HTTP, c.Link.URL, "local", c.Link.Local, key)
}

func (c *Client) CurrentClock() *secure.Clock {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.peer == nil {
		return nil
	}
	return c.peer.Clock
}
