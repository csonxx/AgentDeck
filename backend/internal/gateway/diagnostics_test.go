package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
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
	original := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(original)
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
	if !strings.Contains(output.String(), "feishu_not_configured") || !strings.Contains(output.String(), `"route":"unmatched"`) {
		t.Fatal("missing useful rejection diagnostics")
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
