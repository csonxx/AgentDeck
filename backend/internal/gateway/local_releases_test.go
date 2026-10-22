package gateway

import (
	"agentdeck/internal/localupdate"
	"agentdeck/internal/store"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLocalReleasePlatformsImmutableAndOwnerOnly(t *testing.T) {
	db, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	g := New(db, "https://example.com/agentdeck", "", "system", time.Minute)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	for _, os := range []string{"darwin", "windows", "linux"} {
		for _, arch := range []string{"arm64", "amd64"} {
			r := localupdate.Release{Version: "0.4.47", Build: 9064, OS: os, Arch: arch, Bytes: 100, SHA256: strings.Repeat("a", 64), URL: "https://cdn.example/immutable/agentdeck", KeyID: "fixture", Protocol: 1, StateSchema: 1}
			if err = r.Sign(key); err != nil {
				t.Fatal(err)
			}
			if err = g.PublishLocalRelease(r); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			g.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/agentdeck/v2/local/releases/latest?os="+os+"&arch="+arch, nil))
			var got localupdate.Release
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got != r {
				t.Fatal("platform release mismatch")
			}
			for _, mutate := range []func(*localupdate.Release){func(r *localupdate.Release) { r.Build-- }, func(r *localupdate.Release) { r.Notes = "replaced" }, func(r *localupdate.Release) { r.KeyID = "replacement" }} {
				copy := r
				mutate(&copy)
				_ = copy.Sign(key)
				if g.PublishLocalRelease(copy) == nil {
					t.Fatal("downgrade/replacement accepted")
				}
			}
		}
	}
	for _, route := range []string{"/agentdeck/releases/local", "/agentdeck/v2/local/releases/latest"} {
		w := httptest.NewRecorder()
		g.Handler().ServeHTTP(w, httptest.NewRequest("POST", route, strings.NewReader(`{}`)))
		if w.Code == 200 {
			t.Fatal("public publishing permitted")
		}
	}
	w := httptest.NewRecorder()
	g.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/agentdeck/v2/local/releases/latest?os=windows&arch=mips", nil))
	if w.Code != 400 {
		t.Fatal("invalid architecture accepted")
	}
}
