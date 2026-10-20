package gateway

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"agentdeck/internal/relay"
	"agentdeck/internal/secure"
)

type diagnosticKey struct{}

var defaultDiagnosticLogger = slog.New(slog.NewTextHandler(os.Stderr, nil)).With("service", "agentdeck-gateway")

type requestDiagnostic struct {
	ID, Route, Reason string
	Status            int
	Logger            *slog.Logger
}

// ConfigureLogging must be called before serving requests. Diagnostics are text
// even at DEBUG; raising verbosity never includes request/provider payloads.
func (g *Server) ConfigureLogging(out io.Writer, level string) error {
	var threshold slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		threshold = slog.LevelInfo
	case "debug":
		threshold = slog.LevelDebug
	case "warn":
		threshold = slog.LevelWarn
	case "error":
		threshold = slog.LevelError
	default:
		return errors.New("gateway log level must be debug, info, warn or error")
	}
	g.Logger = slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: threshold})).With("service", "agentdeck-gateway")
	return nil
}

type connectionStats struct {
	Workstations, AppStreams, RelayRequests int
}

// Count authenticated logical connections, not idle HTTP keep-alives, registered
// accounts or persisted grants. A reserved/closed tunnel is not online.
func (g *Server) connectionStats() connectionStats {
	g.mu.Lock()
	defer g.mu.Unlock()
	stats := connectionStats{AppStreams: g.appStreams}
	for _, p := range g.peers {
		if p == nil {
			continue
		}
		select {
		case <-p.Done():
		default:
			stats.Workstations++
		}
	}
	for _, n := range g.active {
		stats.RelayRequests += n
	}
	stats.RelayRequests -= stats.AppStreams
	return stats
}

// LogConnectionStats logs an initial snapshot and then one per interval,
// including zero counts. It stops with the server context; zero disables it.
func (g *Server) LogConnectionStats(ctx context.Context, interval time.Duration) {
	if interval <= 0 || ctx.Err() != nil {
		return
	}
	logStats := func() {
		s := g.connectionStats()
		g.Logger.Info("connection_stats", "active_connections", s.Workstations+s.AppStreams,
			"online_workstations", s.Workstations, "app_streams", s.AppStreams, "relay_requests", s.RelayRequests)
	}
	logStats()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			logStats()
		}
	}
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
	case "/healthz", "/v2/time-sync", "/v2/auth/flows", "/v2/auth/poll", "/v2/auth/login", "/v2/auth/authorize", "/v2/auth/callback", "/v2/auth/done", "/v2/account/locals", "/v2/account/bind", "/v2/local/tunnel", "/v2/local/grants", "/v2/local/unlink", "/v2/feedback/create", "/v2/feedback/list", "/v2/feedback/get", "/v2/feedback/delete":
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
	level := slog.LevelInfo
	if event == "feishu_identity_failed" {
		level = slog.LevelWarn
		status, _ := fields["provider_status"].(int)
		if status == 0 || status >= 500 {
			level = slog.LevelError
		}
	}
	diagnosticAt(r, level, event, fields)
}
func diagnosticAt(r *http.Request, level slog.Level, event string, fields map[string]any) {
	logger := defaultDiagnosticLogger
	attrs := make([]any, 0, 4+2*len(fields))
	if d, ok := r.Context().Value(diagnosticKey{}).(*requestDiagnostic); ok {
		if d.Logger != nil {
			logger = d.Logger
		}
		attrs = append(attrs, "request_id", d.ID, "route", d.Route)
	}
	// Stable field order makes related records easy to compare in plain logs.
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		attrs = append(attrs, k, fields[k])
	}
	logger.Log(r.Context(), level, event, attrs...)
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
		d := &requestDiagnostic{ID: secure.ID(), Route: diagnosticRoute(path), Logger: g.Logger}
		r = r.WithContext(context.WithValue(r.Context(), diagnosticKey{}, d))
		w.Header().Set("X-Request-ID", d.ID)
		dw := &diagnosticWriter{ResponseWriter: w}
		next.ServeHTTP(dw, r)
		status := dw.status
		if status == 0 {
			status = 200
		}
		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case status >= 400:
			level = slog.LevelWarn
		case d.Route == "/healthz" || d.Route == "/v2/auth/poll" || d.Route == "/v2/time-sync":
			level = slog.LevelDebug
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
		diagnosticAt(r, level, "http_request", map[string]any{"method": method, "status": status, "duration_ms": time.Since(started).Milliseconds(), "reason": reason})
	})
}
