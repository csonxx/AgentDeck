package gateway

import (
	"agentdeck/internal/store"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAndroidReleaseValidationPublicationAndPrefix(t *testing.T) {
	db, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	g := New(db, "https://gateway.example/agentdeck", "", "system", time.Minute)
	h := g.Handler()
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	if get("/agentdeck/v2/app/releases/android").Code != 204 {
		t.Fatal("unpublished release not empty")
	}
	r := AndroidRelease{Version: "0.4.33", VersionCode: 7050, Package: "dev.agentdeck.agentdeck", ABI: "arm64-v8a", MinSDK: 24, Bytes: 123, SHA256: strings.Repeat("a", 64), URL: "https://cdn.example/release.apk", Notes: "fixture"}
	for _, change := range []func(*AndroidRelease){func(v *AndroidRelease) { v.URL = "http://cdn.example/a.apk" }, func(v *AndroidRelease) { v.URL += "?token=private" }, func(v *AndroidRelease) { v.Package = "other.app" }, func(v *AndroidRelease) { v.SHA256 = "bad" }, func(v *AndroidRelease) { v.SHA256 = strings.Repeat("A", 64) }, func(v *AndroidRelease) { v.Bytes = 0 }} {
		bad := r
		change(&bad)
		if g.PublishAndroidRelease(bad) == nil {
			t.Fatal("invalid release accepted")
		}
	}
	if err = g.PublishAndroidRelease(r); err != nil {
		t.Fatal(err)
	}
	w := get("/agentdeck/v2/app/releases/android?abi=arm64-v8a")
	var got AndroidRelease
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got != r {
		t.Fatal("release manifest mismatch")
	}
	if get("/v2/app/releases/android").Code != 404 || get("/agentdeck/v2/app/releases/android?abi=x86").Code != 204 {
		t.Fatal("prefix or ABI guard")
	}
	w = get("/agentdeck/v2/app/releases/android/apk")
	if w.Code != 307 || w.Header().Get("Location") != r.URL {
		t.Fatal("APK delivery redirect mismatch")
	}
	old := r
	old.VersionCode--
	if g.PublishAndroidRelease(old) == nil {
		t.Fatal("release downgraded")
	}
	old = r
	old.SHA256 = strings.Repeat("b", 64)
	if g.PublishAndroidRelease(old) == nil {
		t.Fatal("existing build replaced")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/agentdeck/releases/android", nil))
	if w.Code == 200 {
		t.Fatal("public publishing allowed")
	}
	other := New(db, g.URL, "", "system", time.Minute)
	w = httptest.NewRecorder()
	other.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/agentdeck/v2/app/releases/android", nil))
	if w.Code != 200 {
		t.Fatal("published release not persistent")
	}
}

func TestGatewayClientIPOnlyTrustsConfiguredProxyChain(t *testing.T) {
	g := &Server{}
	r := httptest.NewRequest("GET", "https://gateway.example/", nil)
	r.RemoteAddr = "203.0.113.9:4321"
	r.Header.Set("X-Forwarded-For", "192.0.2.66")
	if g.clientIP(r) != "203.0.113.9" {
		t.Fatal("client supplied IP trusted")
	}
	if g.ConfigureProxyTrust("127.0.0.0/8,10.4.0.0/16") != nil {
		t.Fatal("proxy config")
	}
	r.RemoteAddr = "127.0.0.6:8080"
	r.Header.Set("X-Forwarded-For", "192.0.2.66,203.0.113.9,10.4.1.2")
	if g.clientIP(r) != "203.0.113.9" {
		t.Fatal("trusted chain did not select observed client")
	}
	r.Header.Set("X-Forwarded-For", "bad,not-an-ip")
	if g.clientIP(r) != "127.0.0.6" {
		t.Fatal("malformed chain accepted")
	}
}
