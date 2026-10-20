package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
)

func (g *Server) Admin(ctx context.Context, dir string) error {
	path := filepath.Join(dir, "control.sock")
	_ = os.Remove(path)
	ln, e := net.Listen("unix", path)
	if e != nil {
		return e
	}
	if e = os.Chmod(path, 0600); e != nil {
		ln.Close()
		return e
	}
	m := http.NewServeMux()
	g.releaseAdminRoutes(m)
	g.feedbackAdminRoutes(m)
	m.HandleFunc("POST /locals", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tenant string `json:"tenant"`
			Name   string `json:"name"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req) != nil {
			fail(w, 400)
			return
		}
		link, e := g.AddLocal(req.Tenant, req.Name)
		if e != nil {
			fail(w, 400)
			return
		}
		write(w, link)
	})
	m.HandleFunc("GET /locals", func(w http.ResponseWriter, r *http.Request) {
		rows, e := g.Store.Values("gwnodes")
		if e != nil {
			fail(w, 500)
			return
		}
		out := []map[string]any{}
		g.mu.Lock()
		defer g.mu.Unlock()
		for _, raw := range rows {
			var n Node
			_ = json.Unmarshal(raw, &n)
			out = append(out, map[string]any{"id": n.ID, "tenant": n.Tenant, "name": n.Name, "online": g.peers[n.ID] != nil})
		}
		write(w, out)
	})
	m.HandleFunc("GET /accounts", func(w http.ResponseWriter, r *http.Request) {
		rows, e := g.Store.Values("gwaccounts")
		if e != nil {
			fail(w, 500)
			return
		}
		out := []map[string]any{}
		for _, raw := range rows {
			var a Account
			if json.Unmarshal(raw, &a) == nil {
				out = append(out, map[string]any{"id": a.ID, "tenant": a.Identity.Tenant, "owner": a.Identity.Owner, "name": a.Identity.Name, "expires": a.Expires})
			}
		}
		write(w, out)
	})
	m.HandleFunc("POST /revoke", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req) != nil || req.ID == "" {
			fail(w, 400)
			return
		}
		var e error
		switch req.Kind {
		case "local":
			e = g.RevokeLocal(req.ID)
		case "grant":
			e = g.RevokeGrant(req.ID)
		case "account":
			e = g.RevokeAccount(req.ID)
		default:
			fail(w, 400)
			return
		}
		if e != nil {
			fail(w, 500)
			return
		}
		write(w, map[string]bool{"ok": true})
	})
	srv := &http.Server{Handler: m, ReadHeaderTimeout: 5e9}
	go func() { <-ctx.Done(); g.Close(); srv.Close(); os.Remove(path) }()
	e = srv.Serve(ln)
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
