package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"agentdeck/internal/relay"
	"agentdeck/internal/secure"
)

type diagnosticKey struct{}
type requestDiagnostic struct {
	ID, Route, Reason string
	Status            int
}

func gatewayPrefix(raw string) string {
	u, e := url.Parse(raw)
	if e != nil {
		return ""
	}
	return u.Path
}

// Routes are a bounded vocabulary. Query strings, path IDs, headers, bodies,
// provider replies, comparison codes and cryptographic material are excluded.
func diagnosticRoute(path string) string {
	if strings.HasPrefix(path, "/v2/relay/") {
		return "/v2/relay/:binding"
	}
	switch path {
	case "/healthz", "/v2/time-sync", "/v2/auth/flows", "/v2/auth/poll", "/v2/auth/login", "/v2/auth/authorize", "/v2/auth/callback", "/v2/auth/done", "/v2/account/locals", "/v2/account/bind", "/v2/local/tunnel", "/v2/local/grants", "/v2/local/unlink":
		return path
	default:
		return "unmatched"
	}
}
func ref(id string) string {
	if id == "" {
		return ""
	}
	return relay.Hash([]byte(id))[:12]
}
func diagnostic(r *http.Request, event string, fields map[string]any) {
	v := map[string]any{"service": "agentdeck-gateway", "event": event}
	if d, ok := r.Context().Value(diagnosticKey{}).(*requestDiagnostic); ok {
		v["request_id"], v["route"] = d.ID, d.Route
	}
	for k, value := range fields {
		v[k] = value
	}
	raw, _ := json.Marshal(v)
	log.Print(string(raw))
}
func reject(w http.ResponseWriter, r *http.Request, status int, reason string) {
	if d, ok := r.Context().Value(diagnosticKey{}).(*requestDiagnostic); ok {
		d.Reason = reason
	}
	fail(w, status)
}

type diagnosticWriter struct {
	http.ResponseWriter
	status int
}

func (w *diagnosticWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *diagnosticWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(p)
}
func (w *diagnosticWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *diagnosticWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *diagnosticWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, b, e := http.NewResponseController(w.ResponseWriter).Hijack()
	if e == nil {
		w.status = 101
	}
	return c, b, e
}

func (g *Server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		// Prefix stripping occurs inside the gateway; normalize for logs without
		// retaining potentially sensitive unmatched paths.
		path := r.URL.Path
		if prefix := gatewayPrefix(g.URL); prefix != "" && strings.HasPrefix(path, prefix+"/") {
			path = strings.TrimPrefix(path, prefix)
		}
		d := &requestDiagnostic{ID: secure.ID(), Route: diagnosticRoute(path)}
		r = r.WithContext(context.WithValue(r.Context(), diagnosticKey{}, d))
		w.Header().Set("X-Request-ID", d.ID)
		dw := &diagnosticWriter{ResponseWriter: w}
		next.ServeHTTP(dw, r)
		status := dw.status
		if status == 0 {
			status = 200
		}
		if status < 400 && (d.Route == "/healthz" || d.Route == "/v2/auth/poll" || d.Route == "/v2/time-sync") {
			return
		}
		reason := d.Reason
		if reason == "" && status >= 400 {
			reason = http.StatusText(status)
		}
		method := r.Method
		switch method {
		case "GET", "POST", "OPTIONS", "HEAD":
		default:
			method = "OTHER"
		}
		diagnostic(r, "http_request", map[string]any{"method": method, "status": status, "duration_ms": time.Since(started).Milliseconds(), "reason": reason})
	})
}
