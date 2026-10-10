package main

import (
	"agentdeck/internal/gateway"
	"agentdeck/internal/relay"
	"agentdeck/internal/store"
	"os"
	"strings"
	"testing"
)

func TestRejectConflictingOrInsecureTransportConfiguration(t *testing.T) {
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"relay with proxy", []string{"--relay", "--behind-proxy"}, "no public listener"},
		{"relay with certificate", []string{"--relay", "--cert", "cert.pem", "--key", "key.pem"}, "no public listener"},
		{"gateway outbound mode", []string{"--role", "gateway", "--relay"}, "requires serve"},
		{"unknown role", []string{"--role", "unknown"}, "local or gateway"},
		{"negative connection log interval", []string{"--role", "gateway", "--behind-proxy", "--connection-log-interval", "-1s"}, "--connection-log-interval must be >= 0"},
		{"public plaintext URL", []string{"--behind-proxy", "--public-url", "http://agent.example.com"}, "HTTPS origin"},
		{"unprotected public listener", []string{"--listen", "0.0.0.0:8080"}, "public listeners require"},
		{"local and proxy certificates", []string{"--behind-proxy", "--cert", "cert.pem", "--key", "key.pem"}, "cannot be combined"},
		{"malformed external pin", []string{"--behind-proxy", "--public-tls-pin", "broken"}, "64-character"},
		{"external pin without proxy", []string{"--public-tls-pin", strings.Repeat("a", 64)}, "requires --behind-proxy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			os.Args = append([]string{"agentdeck", "serve", "--data", t.TempDir()}, tc.args...)
			if err := run(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q, got %v", tc.want, err)
			}
		})
	}
}

func TestLocalLoginPreservesExistingEncryptedAssociation(t *testing.T) {
	dir := t.TempDir()
	db, e := store.New(dir)
	if e != nil {
		t.Fatal(e)
	}
	original := relay.Link{Version: 1, URL: "https://original.example.com", TLSTrust: "system", Local: "original", Tenant: "tenant", Key: strings.Repeat("A", 43) + "="}
	if e := db.Put("relayconfig", "gateway", original); e != nil {
		t.Fatal(e)
	}
	db.Close()
	if e := localLogin(dir, "https://replacement.example.com", "", "workstation", true); e == nil || !strings.Contains(e.Error(), "already linked") {
		t.Fatal("existing association was not protected")
	}
	db, e = store.New(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var saved relay.Link
	if e := db.Get("relayconfig", "gateway", &saved); e != nil || saved != original {
		t.Fatal("existing association changed")
	}
}

func TestGatewayFeishuTODOStaysDisabled(t *testing.T) {
	for _, tc := range []struct {
		id, secret          string
		configured, invalid bool
	}{
		{"", "", false, false}, {"TODO", "TODO", false, false},
		{"synthetic-app", "synthetic-secret", true, false},
		{"TODO", "synthetic-secret", false, true},
		{"synthetic-app", "TODO", false, true},
		{"synthetic-app", "", false, true},
	} {
		g := &gateway.Server{}
		err := configureGatewayFeishu(g, "https://example.com/agentdeck", tc.id, tc.secret)
		if (err != nil) != tc.invalid || (g.Feishu != nil) != tc.configured {
			t.Fatal("invalid Feishu placeholder handling")
		}
		if g.Feishu != nil && g.Feishu.Redirect != "https://example.com/agentdeck/v2/auth/callback" {
			t.Fatal("callback prefix lost")
		}
	}
}
