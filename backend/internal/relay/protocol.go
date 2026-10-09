// Package relay carries opaque HTTP/WebSocket bytes over an authenticated,
// encrypted outbound tunnel. Its keys are independent of Local/App device roots.
package relay

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"agentdeck/internal/secure"
)

const MaxStreams = 32
const ChunkSize = 32 << 10

type Link struct {
	Version  int    `json:"version"`
	URL      string `json:"url"`
	Pin      string `json:"pin"`
	TLSTrust string `json:"tls_trust"`
	Tenant   string `json:"tenant"`
	Local    string `json:"local"`
	Key      string `json:"key"`
}
type Binding struct {
	Tenant string `json:"tenant"`
	Local  string `json:"local"`
	Grant  string `json:"grant"`
	Key    string `json:"key"`
}
type Grant struct {
	Account string `json:"account,omitempty"`
	ID      string `json:"id"`
	Key     string `json:"key"`
	Tenant  string `json:"tenant"`
	Local   string `json:"local"`
	Invite  string `json:"invite"`
	Expires int64  `json:"expires"`
	Device  string `json:"device,omitempty"`
}

func (l Link) Validate() error {
	u, e := url.Parse(l.URL)
	k, ke := secure.B64.DecodeString(l.Key)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || l.Version != 1 || ke != nil || len(k) != 32 || !ValidID(l.Local) || !ValidID(l.Tenant) {
		return errors.New("invalid gateway link")
	}
	if l.TLSTrust == "system" && l.Pin == "" {
		return nil
	}
	p, e := hex.DecodeString(l.Pin)
	if l.TLSTrust != "pin" || e != nil || len(p) != 32 {
		return errors.New("invalid gateway TLS trust")
	}
	return nil
}
func ValidID(s string) bool {
	if len(s) < 1 || len(s) > 80 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
func Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func TLSConfig(trust, pin string) (*tls.Config, error) {
	c := &tls.Config{MinVersion: tls.VersionTLS13}
	if trust == "system" && pin == "" {
		return c, nil
	}
	p, e := hex.DecodeString(pin)
	if trust != "pin" || e != nil || len(p) != 32 {
		return nil, errors.New("invalid gateway TLS trust")
	}
	// Pin trust is explicitly authorized by an owner-only enrollment file. Always
	// check validity too; system mode retains ordinary CA/hostname verification.
	c.InsecureSkipVerify = true
	c.VerifyConnection = func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return errors.New("missing certificate")
		}
		leaf := cs.PeerCertificates[0]
		now := time.Now()
		if Hash(leaf.RawSubjectPublicKeyInfo) != strings.ToLower(pin) || now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
			return errors.New("gateway certificate rejected")
		}
		return nil
	}
	return c, nil
}

// Test callers may augment the normal root store, never disable verification.
func WithRoots(c *tls.Config, roots *x509.CertPool) *tls.Config {
	c = c.Clone()
	c.RootCAs = roots
	return c
}

type Proof struct {
	Time          int64  `json:"time"`
	Method        string `json:"method"`
	Path          string `json:"path"`
	Body          string `json:"body"`
	Authorization string `json:"authorization"`
}

func Auth(key []byte, scope, method, path string, body []byte, auth string) (string, error) {
	return AuthAt(key, scope, method, path, body, auth, time.Now().Unix())
}
func AuthAt(key []byte, scope, method, path string, body []byte, auth string, unix int64) (string, error) {
	b, e := secure.SealJSON(key, scope, 0, 0, Proof{unix, method, path, Hash(body), auth})
	if e != nil {
		return "", e
	}
	d, e := json.Marshal(b)
	return secure.B64.EncodeToString(d), e
}

type Replay struct {
	mu    sync.Mutex
	seen  map[string]time.Time
	Clock *secure.Clock
}

func (p *Replay) Verify(key []byte, scope string, r *http.Request, body []byte, auth string) bool {
	raw, e := secure.B64.DecodeString(r.Header.Get("X-Relay-Auth"))
	if e != nil || len(raw) > 4096 {
		return false
	}
	var box secure.Box
	if json.Unmarshal(raw, &box) != nil || box.Epoch != 0 || box.Seq != 0 {
		return false
	}
	plain, e := secure.Open(key, scope, box)
	var v Proof
	now := time.Now().Unix()
	if p.Clock != nil {
		now = p.Clock.Unix()
	}
	if e != nil || json.Unmarshal(plain, &v) != nil || v.Time < now-60 || v.Time > now+60 || v.Method != r.Method || v.Path != r.URL.Path || v.Body != Hash(body) || v.Authorization != auth {
		return false
	}
	return p.Accept(scope + ":" + box.Nonce)
}
func (p *Replay) Accept(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if p.seen == nil {
		p.seen = map[string]time.Time{}
	}
	for key, at := range p.seen {
		if now.Sub(at) > 2*time.Minute {
			delete(p.seen, key)
		}
	}
	if _, ok := p.seen[id]; ok || len(p.seen) >= 8192 {
		return false
	}
	p.seen[id] = now
	return true
}

type grantContext struct{}

func WithGrant(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, grantContext{}, id)
}
func ContextGrant(ctx context.Context) string { s, _ := ctx.Value(grantContext{}).(string); return s }

// Gateway-calibrated clock is scoped to the authenticated tunnel, never a
// mutable global offset shared with an existing connection.
type clockContext struct{}

func WithClock(ctx context.Context, clock *secure.Clock) context.Context {
	return context.WithValue(ctx, clockContext{}, clock)
}
func ContextClock(ctx context.Context) *secure.Clock {
	c, _ := ctx.Value(clockContext{}).(*secure.Clock)
	return c
}
