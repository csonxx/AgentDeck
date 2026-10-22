package localupdate

import (
	"agentdeck/internal/localipc"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func signed(t *testing.T, url string, b []byte, build int64) (Release, map[string]string) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	h := sha256.Sum256(b)
	r := Release{Version: "1.0." + strconv.FormatInt(build, 10), Build: build, OS: runtime.GOOS, Arch: runtime.GOARCH, Bytes: int64(len(b)), SHA256: hex.EncodeToString(h[:]), URL: url, KeyID: "test", Protocol: Protocol, StateSchema: Schema}
	if err := r.Sign(priv); err != nil {
		t.Fatal(err)
	}
	return r, map[string]string{"test": base64.StdEncoding.EncodeToString(pub)}
}
func TestSignedReleaseAuthenticatesEveryField(t *testing.T) {
	r, keys := signed(t, "https://example.com/immutable/bin", []byte("binary"), 2)
	if err := r.Verify(keys); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Release){func(r *Release) { r.URL += "evil" }, func(r *Release) { r.Build++ }, func(r *Release) { r.Arch = "mips" }, func(r *Release) { r.Notes = "changed" }, func(r *Release) { r.StateSchema++ }, func(r *Release) { r.KeyID = "other" }, func(r *Release) { r.Bytes++ }, func(r *Release) { r.SHA256 = strings.Repeat("0", 64) }} {
		copy := r
		mutate(&copy)
		if copy.Verify(keys) == nil {
			t.Fatal("changed release passed verification")
		}
	}
	if r.Verify(map[string]string{}) == nil {
		t.Fatal("unpinned publisher accepted")
	}
}
func TestDownloadResumesAndRejectsCorruption(t *testing.T) {
	data := []byte(strings.Repeat("verified-binary", 4000))
	offset := 12345
	requests := 0
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Range") != fmt.Sprintf("bytes=%d-", offset) {
			t.Error("missing resume range")
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, len(data)-1, len(data)))
		w.WriteHeader(206)
		_, _ = w.Write(data[offset:])
	}))
	defer ts.Close()
	r, keys := signed(t, ts.URL+"/bin", data, 2)
	if r.Verify(keys) != nil {
		t.Fatal("signature")
	}
	dir := t.TempDir()
	target := binary(dir, r)
	_ = os.MkdirAll(filepath.Dir(target), 0700)
	_ = os.WriteFile(target+".part", data[:offset], 0600)
	if err := download(context.Background(), ts.Client(), dir, r, func(int64) {}); err != nil {
		t.Fatal(err)
	}
	if verifyFile(target, r) != nil || requests != 1 {
		t.Fatal("resume did not finish")
	}
	if err := download(context.Background(), ts.Client(), dir, r, func(int64) {}); err != nil || requests != 1 {
		t.Fatal("verified download repeated")
	}
	_ = os.Remove(target)
	_ = os.WriteFile(target+".part", make([]byte, len(data)), 0600)
	if download(context.Background(), ts.Client(), dir, r, func(int64) {}) == nil {
		t.Fatal("corruption accepted")
	}
	if _, err := os.Stat(target + ".part"); !os.IsNotExist(err) {
		t.Fatal("bad partial retained")
	}
}
func TestHTTPRejectsInsecureRedirect(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://localhost/bin", 307) }))
	defer ts.Close()
	c := HTTPClient()
	c.Transport = ts.Client().Transport
	if _, err := c.Get(ts.URL); err == nil {
		t.Fatal("insecure redirect accepted")
	}
}

// The helper is a real isolated child process with the same owner IPC and
// readiness protocol. It never opens a real vault or invokes an agent.
func TestMain(m *testing.M) {
	if os.Getenv("AGENTDECK_UPDATER_FIXTURE") == "1" && len(os.Args) > 3 && os.Args[1] == "serve" {
		fixture()
		return
	}
	os.Exit(m.Run())
}
func fixture() {
	dir := os.Args[3]
	var state State
	if readJSON(filepath.Join(root(dir), "state.json"), &state) != nil {
		os.Exit(3)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "crash")); strings.TrimSpace(string(b)) == fmt.Sprint(state.Active.Build) {
		os.Exit(4)
	}
	ln, err := localipc.Listen(dir, "control")
	if err != nil {
		os.Exit(5)
	}
	defer ln.Close()
	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}
	mux.HandleFunc("GET /gateway/status", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"linked":false}`) })
	mux.HandleFunc("GET /update/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"instance": os.Getenv("AGENTDECK_RUN_ID"), "version": state.Active.Version, "build": fmt.Sprint(state.Active.Build)})
	})
	mux.HandleFunc("POST /update/prepare", func(w http.ResponseWriter, r *http.Request) {
		_, err := os.Stat(filepath.Join(dir, "busy"))
		_ = json.NewEncoder(w).Encode(map[string]bool{"ready": os.IsNotExist(err)})
	})
	mux.HandleFunc("POST /update/resume", func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("AGENTDECK_UPDATE_TRIAL") != "1" {
			http.Error(w, "missing trial hold", 500)
			return
		}
		if err := os.WriteFile(filepath.Join(dir, "activated"), []byte(fmt.Sprint(state.Active.Build)), 0600); err != nil {
			http.Error(w, "save failed", 500)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	mux.HandleFunc("POST /update/stop", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
		time.AfterFunc(10*time.Millisecond, func() { srv.Close() })
	})
	_ = srv.Serve(ln)
}
func eventually(t *testing.T, test func() bool) {
	t.Helper()
	end := time.Now().Add(20 * time.Second)
	for time.Now().Before(end) {
		if test() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
func TestSupervisorUpdatesWhenIdleAndRollsBackFailedBuild(t *testing.T) {
	t.Setenv("AGENTDECK_UPDATER_FIXTURE", "1")
	dir, err := os.MkdirTemp("", "ad-update-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	source, _ := os.Executable()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var latest Release
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bin" {
			_, _ = w.Write(data)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(latest)
	}))
	defer ts.Close()
	r, keys := signed(t, ts.URL+"/bin", data, 2)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keys["test"] = base64.StdEncoding.EncodeToString(pub)
	if err = r.Sign(priv); err != nil {
		t.Fatal(err)
	}
	initial := r
	initial.Version = "1.0.1"
	initial.Build = 1
	initial.Signature = ""
	if err = copyBinary(source, binary(dir, initial)); err != nil {
		t.Fatal(err)
	}
	if err = saveJSON(filepath.Join(root(dir), "state.json"), State{Active: initial, Highest: 1}); err != nil {
		t.Fatal(err)
	}
	if err = saveJSON(filepath.Join(root(dir), "config.json"), Config{Gateway: ts.URL, Keys: keys, Auto: true}); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "busy"), []byte("task"), 0600)
	mu.Lock()
	latest = r
	mu.Unlock()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.HTTP = ts.Client()
	s.PollInterval = 20 * time.Millisecond
	s.CheckInterval = time.Hour
	s.Soak = 50 * time.Millisecond
	s.IdleRetry = 30 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("supervisor did not stop")
		}
	}()
	eventually(t, func() bool { return s.snapshot().Phase == "waiting_idle" })
	if s.snapshot().Build != 1 {
		t.Fatal("interrupted busy child")
	}
	_ = os.Remove(filepath.Join(dir, "busy"))
	// Avoid the production thirty-second idle retry in this short fixture by
	// restarting only its timer policy, never skipping the readiness handshake.
	s.mu.Lock()
	s.cfg.Auto = false
	s.mu.Unlock()
	time.Sleep(30 * time.Millisecond)
	s.mu.Lock()
	s.cfg.Auto = true
	s.mu.Unlock()
	// Polling uses a configurable interval for tests; wait through the bounded
	// retry rather than introducing a production force-update path.
	eventually(t, func() bool { return s.snapshot().Build == 2 && s.snapshot().Phase == "current" })
	if b, err := os.ReadFile(filepath.Join(dir, "activated")); err != nil || string(b) != "2" {
		t.Fatal("trial worker not activated", err)
	}
	// Exercise an actual failed child process and automatic restart of build 2.
	bad := r
	bad.Build = 3
	bad.Version = "1.0.3"
	if err = bad.Sign(priv); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "crash"), []byte("3"), 0600)
	mu.Lock()
	latest = bad
	mu.Unlock()
	s.notify()
	eventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.state.Active.Build == 2 && s.state.Rejected == 3 && s.status.Phase == "current"
	})
	// A failed candidate is rejected persistently; restart retains the original
	// high-water mark so an old gateway cannot replay a downgraded release.
	s.mu.Lock()
	previous := initial
	s.state.Previous = &previous
	s.state.Active = r
	s.state.Trial = true
	s.state.Highest = 3
	err = s.save()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	<-done
	done = make(chan error, 1)
	done <- nil
	recovered, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.state.Active.Build != 1 || recovered.state.Rejected != 2 || recovered.state.Highest != 3 {
		t.Fatal("unconfirmed upgrade was not rolled back")
	}
}
