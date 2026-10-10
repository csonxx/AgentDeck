package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agentdeck/internal/server"
	"agentdeck/internal/store"
)

func TestGatewayDiagnosticsRedactUntrustedInputs(t *testing.T) {
	db, e := store.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	g := New(db, "https://example.com/agentdeck", "", "system", time.Second)
	var output bytes.Buffer
	if err := g.ConfigureLogging(&output, "debug"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/agentdeck/v2/auth/flows?code=DO_NOT_LOG_QUERY", "/agentdeck/DO_NOT_LOG_PATH"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{"secret":"DO_NOT_LOG_BODY"}`))
		r.Header.Set("Authorization", "Bearer DO_NOT_LOG_HEADER")
		r.Header.Set("Cookie", "DO_NOT_LOG_COOKIE")
		r.Header.Set("X-Request-ID", "DO_NOT_LOG_REQUEST_ID")
		w := httptest.NewRecorder()
		g.Handler().ServeHTTP(w, r)
		if w.Header().Get("X-Request-ID") == "" {
			t.Fatal("missing request ID")
		}
	}
	if strings.Contains(output.String(), "DO_NOT_LOG") {
		t.Fatal("gateway logged untrusted sensitive data")
	}
	if !strings.Contains(output.String(), "feishu_not_configured") || !strings.Contains(output.String(), "route=unmatched") {
		t.Fatal("missing useful rejection diagnostics")
	}
}

func TestGatewayTextLogLevels(t *testing.T) {
	for _, minimum := range []string{"", "debug", "info", "warn", "error"} {
		t.Run("minimum_"+minimum, func(t *testing.T) {
			g := New(nil, "https://example.com/agentdeck", "", "system", time.Second)
			var output bytes.Buffer
			if err := g.ConfigureLogging(&output, minimum); err != nil {
				t.Fatal(err)
			}
			cases := []struct {
				path   string
				status int
				level  slog.Level
			}{
				{"/healthz", 200, slog.LevelDebug},
				{"/v2/auth/poll", 200, slog.LevelDebug},
				{"/v2/time-sync", 200, slog.LevelDebug},
				{"/v2/auth/flows", 201, slog.LevelInfo},
				{"/v2/local/tunnel", 101, slog.LevelInfo},
				{"/healthz", 503, slog.LevelError},
				{"/v2/local/tunnel", 401, slog.LevelWarn},
				{"/v2/local/grants", 429, slog.LevelWarn},
				{"/unknown", 404, slog.LevelWarn},
				{"/v2/auth/flows", 500, slog.LevelError},
			}
			for _, c := range cases {
				output.Reset()
				w := httptest.NewRecorder()
				r := httptest.NewRequest("GET", "/agentdeck"+c.path, nil)
				g.observe(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(c.status)
				})).ServeHTTP(w, r)
				line := strings.TrimSpace(output.String())
				if !g.Logger.Enabled(context.Background(), c.level) {
					if line != "" {
						t.Fatal("log below configured minimum emitted")
					}
					continue
				}
				if !strings.HasPrefix(line, "time=") || !strings.Contains(line, "level="+c.level.String()) || !strings.Contains(line, "msg=http_request") || !strings.Contains(line, "request_id="+w.Header().Get("X-Request-ID")) || strings.Contains(line, "{") || strings.Count(line, "time=") != 1 || strings.Count(output.String(), "\n") != 1 {
					t.Fatalf("missing text log level/correlation or duplicate timestamps: %s", line)
				}
			}
		})
	}
}

func TestGatewayLifecycleAndProviderLevels(t *testing.T) {
	g := New(nil, "https://example.com", "", "system", time.Second)
	var output bytes.Buffer
	if err := g.ConfigureLogging(&output, "info"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		event  string
		fields map[string]any
		level  string
	}{
		{"local_online", map[string]any{"local_ref": "safe-hash"}, "INFO"},
		{"local_offline", nil, "INFO"},
		{"feishu_identity_failed", map[string]any{"provider_status": 400}, "WARN"},
		{"feishu_identity_failed", map[string]any{"provider_status": 503}, "ERROR"},
		{"feishu_identity_failed", map[string]any{"reason": "configuration_invalid"}, "ERROR"},
	} {
		output.Reset()
		r := httptest.NewRequest("GET", "/", nil)
		r = r.WithContext(context.WithValue(r.Context(), diagnosticKey{}, &requestDiagnostic{Logger: g.Logger}))
		diagnostic(r, c.event, c.fields)
		if !strings.Contains(output.String(), "level="+c.level) || !strings.Contains(output.String(), "msg="+c.event) {
			t.Fatal("incorrect lifecycle/provider severity")
		}
	}
	before := g.Logger
	if g.ConfigureLogging(&output, "DO_NOT_LOG_UNTRUSTED_LEVEL") == nil || g.Logger != before {
		t.Fatal("invalid level accepted or changed logger")
	}
}

func TestAssociationConsoleEnrollmentAndReconnect(t *testing.T) {
	f := setupPrefix(t, "/agentdeck")
	defer f.g.Close()
	// Reuse an existing, encrypted association to prove status and new manager
	// wiring do not expose keys and retain the outbound encrypted tunnel.
	db, e := store.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	link, e := f.g.AddLocal("tenant-a", "console workstation")
	if e != nil {
		t.Fatal(e)
	}
	_ = db.Put("relayconfig", "gateway", link)
	s := server.New(db, "", "https://localhost:8443", time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	if e := s.EnableRelay(ctx); e != nil {
		cancel()
		t.Fatal(e)
	}
	defer func() { cancel(); s.Association.Wait() }()
	await(t, func() bool { return s.Association.Status().Online })
	status := s.Association.Status()
	raw, _ := json.Marshal(status)
	if !status.Linked || status.URL != f.g.URL || strings.Contains(string(raw), link.Key) {
		t.Fatal("association status or credential redaction invalid")
	}
	invite, e := s.Invite()
	if e != nil || invite.URL != link.URL || invite.Relay == nil {
		t.Fatal("dynamic relay invitation failed")
	}
	if e := s.Association.Begin(f.g.URL, "overwrite"); e == nil {
		t.Fatal("existing association overwritten")
	}
	if e := f.g.RevokeLocal(link.Local); e != nil {
		t.Fatal(e)
	}
	await(t, func() bool { return !s.Association.Status().Online })
}

func TestFeishuProviderDiagnosticsExcludeResponse(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"code":20029,"error":"DO_NOT_LOG_PROVIDER","access_token":"DO_NOT_LOG_TOKEN"}`)
	}))
	defer provider.Close()
	f := NewFeishu("id", "DO_NOT_LOG_SECRET", "https://example.com/callback")
	f.TokenURL = provider.URL
	_, err := f.Resolve(context.Background(), "DO_NOT_LOG_CODE", "DO_NOT_LOG_VERIFIER")
	if err == nil {
		t.Fatal("bad provider accepted")
	}
	data, _ := json.Marshal(providerDiagnostic(err))
	if strings.Contains(string(data), "DO_NOT_LOG") || !strings.Contains(string(data), "20029") {
		t.Fatal("provider diagnostics leak or missing safe code")
	}
	unsafe := providerFailure("token", "rejected", 400, json.RawMessage(`"secret-token"`))
	data, _ = json.Marshal(providerDiagnostic(unsafe))
	if strings.Contains(string(data), "secret-token") {
		t.Fatal("provider code not sanitized")
	}
}
