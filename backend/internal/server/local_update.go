package server

import (
	"agentdeck/internal/localipc"
	"agentdeck/internal/localupdate"
	"io"
	"net/http"
	"os"
	"time"
)

func (s *Server) updateRoutes(m *http.ServeMux, dir string) {
	m.HandleFunc("GET /update/health", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]string{"instance": os.Getenv("AGENTDECK_RUN_ID"), "version": localupdate.Version, "build": localupdate.Build})
	})
	m.HandleFunc("POST /update/prepare", func(w http.ResponseWriter, r *http.Request) {
		ready := false
		if s.commandMu.TryLock() {
			if s.Manager != nil {
				ready = s.Manager.PrepareUpdate(r.Context())
			} else {
				ready = true
			}
			s.updateDraining = ready
			s.commandMu.Unlock()
		}
		write(w, map[string]bool{"ready": ready})
	})
	m.HandleFunc("POST /update/resume", func(w http.ResponseWriter, r *http.Request) {
		s.commandMu.Lock()
		s.updateDraining = false
		if s.Manager != nil {
			s.Manager.ResumeAfterUpdate()
		}
		s.commandMu.Unlock()
		write(w, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /update/stop", func(w http.ResponseWriter, r *http.Request) {
		if s.Stop == nil {
			fail(w, 503)
			return
		}
		write(w, map[string]bool{"ok": true})
		// Flush the acknowledgement before graceful service shutdown.
		time.AfterFunc(100*time.Millisecond, s.Stop)
	})
	for _, entry := range []struct{ method, public, target string }{{"GET", "/updates/status", "/status"}, {"POST", "/updates/check", "/check"}, {"POST", "/updates/policy", "/policy"}} {
		m.HandleFunc(entry.method+" "+entry.public, func(w http.ResponseWriter, r *http.Request) {
			client := localipc.Client(dir, "updates")
			defer client.CloseIdleConnections()
			req, _ := http.NewRequestWithContext(r.Context(), entry.method, "http://unix"+entry.target, http.MaxBytesReader(w, r.Body, 1024))
			res, err := client.Do(req)
			if err != nil {
				if entry.method == "GET" {
					write(w, map[string]any{"managed": false, "version": localupdate.Version, "build": localupdate.Build, "phase": "manual_install"})
					return
				}
				fail(w, 503)
				return
			}
			defer res.Body.Close()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(res.StatusCode)
			_, _ = io.Copy(w, io.LimitReader(res.Body, 64<<10))
		})
	}
}

// HoldForUpdate must run before any public listener or delivery loop starts.
func (s *Server) HoldForUpdate() {
	s.commandMu.Lock()
	defer s.commandMu.Unlock()
	s.updateDraining = true
	if s.Manager != nil {
		s.Manager.HoldForUpdate()
	}
}
