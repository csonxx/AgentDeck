package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"agentdeck/internal/feedback"
	"agentdeck/internal/gateway"
	"agentdeck/internal/relay"
	"agentdeck/internal/server"
	"agentdeck/internal/store"
)

func adminRequest(dir, method, path string, v any) ([]byte, error) {
	var body io.Reader
	if v != nil {
		raw, _ := json.Marshal(v)
		body = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, "http://unix"+path, body)
	res, e := server.AdminClient(dir).Do(req)
	if e != nil {
		return nil, errors.New("owner admin socket unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("owner request rejected: %d", res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, 1<<20))
}
func gatewayManagement(cmd, dir, tenant, name, out, id, kind, code string) error {
	switch cmd {
	case "gateway-add-local":
		data, e := adminRequest(dir, "POST", "/locals", map[string]string{"tenant": tenant, "name": name})
		if e != nil {
			return e
		}
		if e = os.WriteFile(out+".json", data, 0600); e != nil {
			return e
		}
		if e = os.Chmod(out+".json", 0600); e != nil {
			return e
		}
		fmt.Printf("Owner-only local enrollment saved: %s.json\n", out)
		return nil
	case "gateway-locals", "gateway-accounts":
		path := "/locals"
		if cmd == "gateway-accounts" {
			path = "/accounts"
		}
		data, e := adminRequest(dir, "GET", path, nil)
		if e == nil {
			fmt.Println(string(data))
		}
		return e
	case "gateway-revoke":
		_, e := adminRequest(dir, "POST", "/revoke", map[string]string{"id": id, "kind": kind})
		return e
	case "pair-pending":
		data, e := adminRequest(dir, "GET", "/pair-pending", nil)
		if e == nil {
			fmt.Println(string(data))
		}
		return e
	case "pair-approve":
		_, e := adminRequest(dir, "POST", "/pair-approve", map[string]string{"grant": id, "code": strings.ReplaceAll(strings.ToLower(code), "-", "")})
		return e
	}
	return errors.New("unknown gateway command")
}
func localLogin(dir, url, pin, name string, noOpen bool) error {
	db, e := store.New(dir)
	if e != nil {
		return e
	}
	defer db.Close()
	var existing relay.Link
	if db.Get("relayconfig", "gateway", &existing) == nil {
		return errors.New("S-LOCAL is already linked; use its saved association or a separate --data directory")
	}
	trust := "system"
	if pin != "" {
		trust = "pin"
	}
	tlsConfig, e := relay.TLSConfig(trust, pin)
	if e != nil {
		return e
	}
	// Validate public origin without needing an already-enrolled Local ID/key.
	dummy := relay.Link{Version: 1, URL: url, Pin: pin, TLSTrust: trust, Tenant: "pending", Local: "pending", Key: strings.Repeat("A", 43) + "="}
	if dummy.Validate() != nil {
		return errors.New("--gateway-url must be an HTTPS URL with an optional canonical path prefix and valid trust configuration")
	}
	h := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	flow, e := relay.BeginLogin(ctx, h, url, "local", name)
	if e != nil {
		return e
	}
	fmt.Printf("Open this AgentDeck page and confirm with Feishu: %s\n", flow.URL)
	if !noOpen {
		var launch *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			launch = exec.Command("open", flow.URL)
		case "linux":
			launch = exec.Command("xdg-open", flow.URL)
		}
		if launch != nil {
			_ = launch.Run()
		}
	}
	for ctx.Err() == nil {
		raw, ready, e := flow.Poll(ctx)
		if e != nil {
			return e
		}
		if ready {
			var v struct {
				Link     relay.Link       `json:"link"`
				Identity gateway.Identity `json:"identity"`
			}
			if json.Unmarshal(raw, &v) != nil || v.Link.Validate() != nil || v.Link.URL != url || v.Link.Tenant != v.Identity.Tenant {
				return errors.New("invalid Feishu local enrollment")
			}
			// Preserve the trust explicitly selected on this workstation, rather than
			// accepting a different trust policy in the gateway enrollment response.
			v.Link.Pin, v.Link.TLSTrust = pin, trust
			if e = db.Put("relayconfig", "gateway", v.Link); e != nil {
				return e
			}
			_ = db.Put("relayconfig", "name", name)
			fmt.Printf("S-LOCAL linked to gateway; tenant=%s local=%s. Start with serve --relay.\n", v.Link.Tenant, v.Link.Local)
			return nil
		}
		tick := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			tick.Stop()
			return ctx.Err()
		case <-tick.C:
		}
	}
	return ctx.Err()
}
func configureGatewayFeishu(g *gateway.Server, public, id, secret string) error {
	// Explicit deployment placeholders must never create a usable OAuth flow.
	if id == "TODO" && secret == "TODO" {
		return nil
	}
	if id != "" || secret != "" {
		if id == "" || secret == "" || id == "TODO" || secret == "TODO" {
			return errors.New("FEISHU_APP_ID and FEISHU_APP_SECRET must be configured together")
		}
		g.Feishu = gateway.NewFeishu(id, secret, public+"/v2/auth/callback")
		g.Feishu.Scope = os.Getenv("FEISHU_OAUTH_SCOPE")
		allowed := os.Getenv("FEISHU_ALLOWED_TENANTS")
		if allowed != "" {
			g.Feishu.AllowedTenants = map[string]bool{}
			for _, t := range strings.Split(allowed, ",") {
				g.Feishu.AllowedTenants[strings.TrimSpace(t)] = true
			}
		}
	}
	return nil
}
func serveGateway(db *store.Store, dir, addr, public, pin, trust, logLevel string, cert tls.Certificate, behindProxy bool, rotation, connectionLogInterval time.Duration) error {
	g := gateway.New(db, public, pin, trust, rotation)
	if err := g.ConfigureDirect(os.Getenv("AGENTDECK_P2P_ENABLED"), os.Getenv("AGENTDECK_STUN_SERVERS")); err != nil {
		return err
	}
	if err := g.ConfigureProxyTrust(os.Getenv("AGENTDECK_TRUSTED_PROXY_CIDRS")); err != nil {
		return err
	}
	if e := g.ConfigureLogging(os.Stderr, logLevel); e != nil {
		return e
	}
	if e := configureGatewayFeishu(g, public, os.Getenv("FEISHU_APP_ID"), os.Getenv("FEISHU_APP_SECRET")); e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	feedbackConfig, err := feedback.FromEnv(os.Getenv)
	if err != nil {
		return err
	}
	feedbackStore, err := feedback.Open(ctx, feedbackConfig)
	if err != nil {
		return err
	}
	if feedbackStore != nil {
		defer feedbackStore.Close()
		g.Feedback = feedbackStore
		g.Logger.Info("feedback_storage_ready", "driver", feedbackConfig.Driver, "retention_hours", int(feedbackConfig.Retention.Hours()))
	}
	feedbackDone := make(chan struct{})
	go func() { defer close(feedbackDone); g.PruneFeedback(ctx) }()
	statsDone := make(chan struct{})
	go func() {
		defer close(statsDone)
		g.LogConnectionStats(ctx, connectionLogInterval)
	}()
	defer func() { stop(); <-statsDone; <-feedbackDone }()
	defer g.Close()
	srv := &http.Server{Addr: addr, Handler: g.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 90 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}}
	srv.ErrorLog = slog.NewLogLogger(g.Logger.Handler(), slog.LevelError)
	go func() {
		if e := g.Admin(ctx, dir); e != nil {
			g.Logger.Error("admin_socket_failed")
			stop()
		}
	}()
	go func() { <-ctx.Done(); g.Close(); _ = srv.Close() }()
	g.Logger.Info("gateway_listening", "listen", addr, "public_url", public, "feishu_configured", g.Feishu != nil)
	var e error
	if behindProxy {
		e = srv.ListenAndServe()
	} else {
		e = srv.ListenAndServeTLS("", "")
	}
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
