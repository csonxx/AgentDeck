package gateway

import (
	"agentdeck/internal/localupdate"
	"encoding/json"
	"errors"
	"net/http"
	"os"
)

func localReleaseKey(os, arch string) (string, bool) {
	return "local-release-" + os + "-" + arch, (os == "darwin" || os == "windows" || os == "linux") && (arch == "arm64" || arch == "amd64")
}
func (g *Server) PublishLocalRelease(release localupdate.Release) error {
	if err := release.Validate(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	k, _ := localReleaseKey(release.OS, release.Arch)
	var old localupdate.Release
	if err := g.Store.Get("agentconfig", k, &old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// An immutable build includes its trust identity and all signed metadata.
	if old.Build > release.Build || old.Build == release.Build && old.Signature != "" && old != release {
		return errors.New("workstation release cannot downgrade or replace a build")
	}
	return g.Store.Put("agentconfig", k, release)
}
func (g *Server) localReleaseRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /v2/local/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		k, ok := localReleaseKey(r.URL.Query().Get("os"), r.URL.Query().Get("arch"))
		if !ok {
			fail(w, 400)
			return
		}
		var release localupdate.Release
		err := g.Store.Get("agentconfig", k, &release)
		if errors.Is(err, os.ErrNotExist) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err != nil || release.Validate() != nil {
			fail(w, 503)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		write(w, release)
	})
}
func (g *Server) localReleaseAdminRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /releases/local", func(w http.ResponseWriter, r *http.Request) {
		var release localupdate.Release
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 20<<10))
		d.DisallowUnknownFields()
		if d.Decode(&release) != nil || g.PublishLocalRelease(release) != nil {
			fail(w, 400)
			return
		}
		g.Logger.Info("local_release_published", "version", release.Version, "build", release.Build, "os", release.OS, "arch", release.Arch)
		write(w, map[string]bool{"ok": true})
	})
}
