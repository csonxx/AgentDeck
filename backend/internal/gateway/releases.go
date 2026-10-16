package gateway

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
)

type AndroidRelease struct {
	Edition     string `json:"edition,omitempty"`
	Version     string `json:"version"`
	VersionCode int64  `json:"version_code"`
	Package     string `json:"package"`
	ABI         string `json:"abi"`
	MinSDK      int    `json:"min_sdk"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
	URL         string `json:"url"`
	Notes       string `json:"notes"`
}

func (r AndroidRelease) Validate() error {
	if r.Edition != "" && r.Edition != "voice" && r.Edition != "lite" {
		return errors.New("invalid Android edition")
	}
	u, e := url.Parse(r.URL)
	h, he := hex.DecodeString(r.SHA256)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		he != nil || len(h) != 32 || r.SHA256 != strings.ToLower(r.SHA256) || r.VersionCode < 1 || r.Bytes < 1 || r.Bytes > 2<<30 || r.MinSDK < 21 ||
		r.Package != "dev.agentdeck.agentdeck" || r.ABI != "arm64-v8a" || !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(r.Version) || len(r.Notes) > 12000 {
		return errors.New("invalid Android release")
	}
	return nil
}

func releaseKey(edition string) (string, error) {
	switch edition {
	case "", "voice":
		return "android-release", nil
	case "lite":
		return "android-release-lite", nil
	}
	return "", errors.New("invalid Android edition")
}

func (g *Server) PublishAndroidRelease(r AndroidRelease) error {
	if err := r.Validate(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var old AndroidRelease
	key, _ := releaseKey(r.Edition)
	if err := g.Store.Get("agentconfig", key, &old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if old.VersionCode > r.VersionCode || old.VersionCode == r.VersionCode && old.SHA256 != "" && old.SHA256 != r.SHA256 {
		return errors.New("release cannot downgrade or replace an existing build")
	}
	return g.Store.Put("agentconfig", key, r)
}

func (g *Server) releaseRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /v2/app/releases/android", func(w http.ResponseWriter, r *http.Request) {
		key, e := releaseKey(r.URL.Query().Get("edition"))
		if e != nil {
			fail(w, 400)
			return
		}
		var release AndroidRelease
		if err := g.Store.Get("agentconfig", key, &release); errors.Is(err, os.ErrNotExist) {
			w.WriteHeader(http.StatusNoContent)
			return
		} else if err != nil || release.Validate() != nil {
			fail(w, 503)
			return
		}
		if abi := r.URL.Query().Get("abi"); abi != "" && abi != release.ABI {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		write(w, release)
	})
	m.HandleFunc("GET /v2/app/releases/android/apk", func(w http.ResponseWriter, r *http.Request) {
		key, e := releaseKey(r.URL.Query().Get("edition"))
		if e != nil {
			fail(w, 400)
			return
		}
		var release AndroidRelease
		if g.Store.Get("agentconfig", key, &release) != nil || release.Validate() != nil {
			fail(w, 404)
			return
		}
		http.Redirect(w, r, release.URL, http.StatusTemporaryRedirect)
	})
}

func (g *Server) releaseAdminRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /releases/android", func(w http.ResponseWriter, r *http.Request) {
		var release AndroidRelease
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		dec.DisallowUnknownFields()
		if dec.Decode(&release) != nil || g.PublishAndroidRelease(release) != nil {
			fail(w, 400)
			return
		}
		g.Logger.Info("android_release_published", "version", release.Version, "version_code", release.VersionCode)
		write(w, map[string]bool{"ok": true})
	})
}
