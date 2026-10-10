// Package console exposes a narrowly scoped owner UI on IPv4 loopback. Agent
// content, device roots, shells and the generic Unix admin API are never proxied.
package console

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"agentdeck/internal/secure"
)

//go:embed web/*
var assets embed.FS

type Console struct {
	Admin       *http.Client
	Token, Host string
}

func New(admin *http.Client, host string) *Console {
	return &Console{Admin: admin, Host: host, Token: secure.B64.EncodeToString(secure.Random(32))}
}
func (c *Console) URL() string { return "http://" + c.Host + "/" }

func (c *Console) allowedHost(host string) bool {
	_, port, e := net.SplitHostPort(c.Host)
	return host == c.Host || e == nil && host == "localhost:"+port
}

func (c *Console) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		host, _, e := net.SplitHostPort(r.RemoteAddr)
		if e != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || !c.allowedHost(r.Host) {
			http.Error(w, "Forbidden", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "Forbidden", 403)
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			http.Error(w, "Forbidden", 403)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			name := map[string]string{"/": "index.html", "/console.js": "console.js", "/console.css": "console.css"}[r.URL.Path]
			if name == "" || (r.Method != "GET" && r.Method != "HEAD") {
				http.NotFound(w, r)
				return
			}
			data, _ := assets.ReadFile("web/" + name)
			mime := map[string]string{"index.html": "text/html; charset=utf-8", "console.js": "text/javascript; charset=utf-8", "console.css": "text/css; charset=utf-8"}[name]
			w.Header().Set("Content-Type", mime)
			if r.Method == "HEAD" {
				w.WriteHeader(200)
				return
			}
			_, _ = w.Write(data)
			return
		}
		// A same-origin script bootstraps an in-memory capability. The custom
		// header prevents cross-origin forms/navigation; CORS preflights are
		// never accepted. Host/Origin/Fetch Metadata checks also apply here.
		if r.URL.Path == "/api/bootstrap" {
			if r.Method != "POST" || r.Header.Get("X-AgentDeck-Console") != "1" {
				http.Error(w, "Forbidden", 403)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"token": c.Token})
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+c.Token)) != 1 {
			http.Error(w, "Unauthorized", 401)
			return
		}
		allowed := map[string]string{"GET /api/status": "/gateway/status", "POST /api/login": "/gateway/login", "POST /api/cancel": "/gateway/cancel", "POST /api/unlink": "/gateway/unlink", "GET /api/pending": "/pair-pending", "POST /api/approve": "/pair-approve"}
		path := allowed[r.Method+" "+r.URL.Path]
		if path == "" {
			http.NotFound(w, r)
			return
		}
		if r.Method == "POST" && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "JSON required", 415)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		req, e := http.NewRequestWithContext(ctx, r.Method, "http://unix"+path, http.MaxBytesReader(w, r.Body, 4096))
		if e != nil {
			http.Error(w, "Bad request", 400)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		res, e := c.Admin.Do(req)
		if e != nil {
			writeProblem(w, 503, "本地后端未运行，请先启动相同数据目录的后端")
			return
		}
		defer res.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(res.Body, (64<<10)+1))
		if e != nil || len(raw) > 64<<10 {
			writeProblem(w, 502, "本地管理接口响应异常")
			return
		}
		// Pending invitations include a comparison code on the owner socket; the
		// browser accepts the phone code, so it only receives minimal request metadata.
		if path == "/pair-pending" && res.StatusCode == 200 {
			var rows []struct {
				Grant    string `json:"grant"`
				Expires  int64  `json:"expires"`
				Approved bool   `json:"approved"`
			}
			if json.Unmarshal(raw, &rows) != nil {
				writeProblem(w, 502, "配对列表格式异常")
				return
			}
			raw, _ = json.Marshal(rows)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(res.StatusCode)
		_, _ = w.Write(raw)
	})
}

func writeProblem(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
