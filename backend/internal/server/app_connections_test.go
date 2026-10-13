package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppConnectionCountsTrackAuthenticatedStreamsAndUniqueDevices(t *testing.T) {
	s, ts := setupServer(t)
	a, b := pairPhone(t, s, ts), pairPhone(t, s, ts)
	check := func(online, connections int) appConnectionStatus {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			v, err := s.appConnections()
			if err != nil {
				t.Fatal(err)
			}
			if v.Online == online && v.Connections == connections && v.Paired == 2 {
				return v
			}
			if time.Now().After(deadline) {
				t.Fatalf("counts online=%d connections=%d paired=%d", v.Online, v.Connections, v.Paired)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	check(0, 0)
	a.login(t, ts)
	check(0, 0) // An issued ticket is not a connected App.
	a.connect(t, ts, s)
	defer a.ws.Close()
	check(1, 1)
	duplicate := &phone{device: a.device, root: a.root}
	duplicate.connect(t, ts, s)
	defer duplicate.ws.Close()
	check(1, 2)
	b.connect(t, ts, s)
	defer b.ws.Close()
	check(2, 3)
	a.ws.Close()
	check(2, 2)
	duplicate.ws.Close()
	check(1, 1)
	b.ws.Close()
	v := check(0, 0)
	for _, d := range v.Devices {
		if d.LastConnected == "" || d.Online || d.ConnectedAt != "" {
			t.Fatal("offline connection history invalid")
		}
	}
	// Stored connection time survives a fresh Server; active streams do not.
	v, err := New(s.Store, "", "", time.Second).appConnections()
	if err != nil || v.Online != 0 || v.Devices[0].LastConnected == "" {
		t.Fatal("restart connection history invalid", err)
	}
	raw, _ := json.Marshal(v)
	for _, secret := range []string{`"root"`, `"relay_grant"`, `"token"`} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("connection summary exposed credential field")
		}
	}
	for _, path := range []string{"/app-connections", "/devices", "/api/apps"} {
		res, err := ts.Client().Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 404 {
			t.Fatal("owner connection metadata exposed publicly", path)
		}
	}
	dir, err := os.MkdirTemp("/tmp", "ad-apps-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Admin(ctx, dir) }()
	defer func() { cancel(); <-done }()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(filepath.Join(dir, "control.sock")); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	res, err := AdminClient(dir).Get("http://unix/app-connections")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || json.NewDecoder(res.Body).Decode(&v) != nil || v.Paired != 2 || v.Online != 0 {
		t.Fatal("owner summary unavailable")
	}
}
