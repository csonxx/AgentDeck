// Synthetic-only integration harness. Fixture routes/identity provider are never
// compiled into the production cmd/agentdeck binary or enabled by runtime flags.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"agentdeck/internal/gateway"
	"agentdeck/internal/relay"
	"agentdeck/internal/secure"
	"agentdeck/internal/server"
	"agentdeck/internal/store"
)

type identityCode struct {
	Identity  gateway.Identity
	Challenge string
}

func main() {
	dir := flag.String("dir", "", "temporary state")
	listen := flag.String("listen", "", "loopback address")
	flag.Parse()
	if e := run(*dir, *listen); e != nil {
		log.Fatal("synthetic gateway harness failed")
	}
}
func run(dir, listen string) error {
	db, e := store.New(filepath.Join(dir, "state"))
	if e != nil {
		return e
	}
	defer db.Close()
	if e = os.MkdirAll(filepath.Join(dir, "tls"), 0700); e != nil {
		return e
	}
	cert, pin, e := server.Certificate(filepath.Join(dir, "tls"), "", "")
	if e != nil {
		return e
	}
	origin := "https://localhost:" + strings.Split(listen, ":")[1] + "/agentdeck"
	g := gateway.NewWithClock(db, origin, "", "system", time.Second, secure.NewClockAt(time.Now().Add(24*time.Hour).UnixMilli()))
	var mu sync.Mutex
	codes := map[string]identityCode{}
	tokens := map[string]gateway.Identity{}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			value, ok := codes[req["code"]]
			delete(codes, req["code"])
			mu.Unlock()
			hash := sha256.Sum256([]byte(req["code_verifier"]))
			if !ok || req["client_id"] != "synthetic-app" || req["client_secret"] != "synthetic-secret" || base64.RawURLEncoding.EncodeToString(hash[:]) != value.Challenge || req["redirect_uri"] != origin+"/v2/auth/callback" {
				http.Error(w, "denied", 401)
				return
			}
			token := secure.ID()
			mu.Lock()
			tokens[token] = value.Identity
			mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "access_token": token})
		case "/user":
			mu.Lock()
			identity, ok := tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
			mu.Unlock()
			if !ok {
				http.Error(w, "denied", 401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]string{"tenant_key": identity.Tenant, "open_id": identity.Owner, "name": identity.Name}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()
	g.Feishu = gateway.NewFeishu("synthetic-app", "synthetic-secret", origin+"/v2/auth/callback")
	g.Feishu.AuthorizeURL = fake.URL + "/authorize"
	g.Feishu.TokenURL = fake.URL + "/token"
	g.Feishu.UserURL = fake.URL + "/user"
	cfg, _ := relay.TLSConfig("pin", pin)
	complete := func(flowID, user string) error {
		jar, _ := cookiejar.New(nil)
		browser := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}, Jar: jar, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		res, e := browser.Get(origin + "/v2/auth/authorize?flow=" + flowID)
		if e != nil {
			return e
		}
		res.Body.Close()
		if res.StatusCode != 303 {
			return errors.New("authorize failed")
		}
		u, _ := url.Parse(res.Header.Get("Location"))
		state := u.Query().Get("state")
		identity := gateway.Identity{Tenant: "fixture-tenant-a", Owner: "fixture-owner-a", Name: "合成用户 A"}
		if user == "other-owner" {
			identity.Owner = "fixture-owner-b"
			identity.Name = "合成用户 B"
		}
		if user == "other-tenant" {
			identity.Tenant = "fixture-tenant-b"
		}
		code := secure.ID()
		mu.Lock()
		codes[code] = identityCode{identity, u.Query().Get("code_challenge")}
		mu.Unlock()
		res, e = browser.Get(origin + "/v2/auth/callback?state=" + state + "&code=" + code)
		if e != nil {
			return e
		}
		res.Body.Close()
		if res.StatusCode != 303 {
			return errors.New("callback failed")
		}
		return nil
	}
	mux := http.NewServeMux()
	mux.Handle("/", g.Handler())
	mux.HandleFunc("POST /agentdeck/fixture/complete", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Flow string `json:"flow"`
			User string `json:"user"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || complete(req.Flow, req.User) != nil {
			http.Error(w, "failed", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /agentdeck/fixture/approve", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 4<<10))
		res, e := server.AdminClient(filepath.Join(dir, "local-state")).Post("http://unix/pair-approve", "application/json", bytes.NewReader(raw))
		if e != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		defer res.Body.Close()
		w.WriteHeader(res.StatusCode)
		io.Copy(w, res.Body)
	})
	mux.HandleFunc("POST /agentdeck/fixture/disconnect", func(w http.ResponseWriter, r *http.Request) {
		g.Close()
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /agentdeck/fixture/revoke", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Grant string `json:"grant"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = g.RevokeGrant(req.Grant)
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /agentdeck/fixture/audit", func(w http.ResponseWriter, r *http.Request) {
		empty := true
		for _, b := range []string{"events", "sessions", "media", "devices"} {
			rows, e := db.Values(b)
			if e != nil || len(rows) != 0 {
				empty = false
			}
		}
		json.NewEncoder(w).Encode(map[string]bool{"gateway_has_no_local_content_or_device_roots": empty})
	})
	srv := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServeTLS("", "") }()
	defer func() { g.Close(); srv.Close() }()
	h := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	// Enroll S-LOCAL through the same PKCE/callback/encrypted-handoff flow.
	var flow *relay.LoginFlow
	for i := 0; i < 100; i++ {
		flow, e = relay.BeginLogin(ctx, h, origin, "local", "合成 S-LOCAL")
		if e == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if e != nil {
		return e
	}
	if e = complete(flow.ID, ""); e != nil {
		return e
	}
	raw, ready, e := flow.Poll(ctx)
	if e != nil || !ready {
		return errors.New("local handoff failed")
	}
	var v struct {
		Link relay.Link `json:"link"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return errors.New("local link invalid")
	}
	localDB, e := store.New(filepath.Join(dir, "local-state"))
	if e != nil {
		return e
	}
	if e = localDB.Put("relayconfig", "gateway", v.Link); e != nil {
		localDB.Close()
		return e
	}
	localDB.Close()
	// This fixture uses a local self-signed cert. Pin is inserted into the local
	// routing credential, while Dart system-trust tests inject its fixture root.
	localDB, e = store.New(filepath.Join(dir, "local-state"))
	if e != nil {
		return e
	}
	v.Link.Pin = pin
	v.Link.TLSTrust = "pin"
	e = localDB.Put("relayconfig", "gateway", v.Link)
	localDB.Close()
	if e != nil {
		return e
	}
	metadata, _ := json.Marshal(map[string]string{"url": origin, "local": v.Link.Local, "tenant": v.Link.Tenant})
	if e = os.WriteFile(filepath.Join(dir, "metadata.json"), metadata, 0600); e != nil {
		return e
	}
	select {
	case <-ctx.Done():
		return nil
	case e := <-errCh:
		if errors.Is(e, http.ErrServerClosed) {
			return nil
		}
		return e
	}
}
