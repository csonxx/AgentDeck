package server

import (
	"agentdeck/internal/secure"
	"agentdeck/internal/store"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDirectClockCalibrationWithoutCallerTimestamp(t *testing.T) {
	s, ts := setupServer(t)
	s.Clock = secure.NewClockAt(time.Now().Add(24 * time.Hour).UnixMilli())
	p := pairPhone(t, s, ts) // authoritative invitation expiry works with a skewed Local.
	scope := "device/" + p.device
	challenge := secure.ID()
	box, _ := secure.SealJSON(p.root, "clock-request/"+scope, 0, 0, map[string]string{"challenge": challenge})
	req := secure.ClockRequest{Kind: "device", ID: p.device, Box: box}
	raw := post(t, ts, "/v1/time-sync", req, 200)
	post(t, ts, "/v1/time-sync", req, 401)
	var response secure.Box
	_ = json.Unmarshal(raw, &response)
	plain, e := secure.Open(p.root, "clock-reply/"+scope, response)
	var sample secure.ClockSample
	if e != nil || json.Unmarshal(plain, &sample) != nil || sample.Challenge != challenge || sample.Version != 1 || sample.ServerTime < s.Clock.Millis()-10000 {
		t.Fatal("wrong authenticated time source")
	}
	bad, _ := secure.SealJSON(secure.Random(32), "clock-request/"+scope, 0, 0, map[string]string{"challenge": secure.ID()})
	post(t, ts, "/v1/time-sync", secure.ClockRequest{Kind: "device", ID: p.device, Box: bad}, 401)
	hello, _ := secure.SealJSON(p.root, "hello/"+p.device, 0, 0, map[string]any{"time": s.Clock.Unix(), "challenge": secure.ID()})
	post(t, ts, "/v1/session", map[string]any{"device": p.device, "box": hello}, 200)
	var dev store.Device
	_ = s.Store.Get("devices", p.device, &dev)
	dev.RelayGrant = "different-route"
	_ = s.Store.Put("devices", p.device, dev)
	fresh, _ := secure.SealJSON(p.root, "clock-request/"+scope, 0, 0, map[string]string{"challenge": secure.ID()})
	post(t, ts, "/v1/time-sync", secure.ClockRequest{Kind: "device", ID: p.device, Box: fresh}, 401)
	_ = s.Store.Delete("devices", p.device)
	post(t, ts, "/v1/time-sync", secure.ClockRequest{Kind: "device", ID: p.device, Box: fresh}, 401)
}

func TestAdminPendingUsesGatewayTime(t *testing.T) {
	s, _ := setupServer(t)
	clock := secure.NewClockAt(time.Now().Add(24 * time.Hour).UnixMilli())
	s.RelayClock = func() *secure.Clock { return clock }
	_ = s.Store.Put("relaypending", "expired", PendingPair{Grant: "expired", Expires: clock.Unix() - 1})
	_ = s.Store.Put("relaypending", "valid", PendingPair{Grant: "valid", Expires: clock.Unix() + 300})
	dir, e := os.MkdirTemp("/tmp", "ad-clock-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = s.Admin(ctx, dir) }()
	defer func() { cancel(); <-done }()
	for i := 0; i < 100; i++ {
		if _, e := os.Stat(filepath.Join(dir, "control.sock")); e == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	h := AdminClient(dir)
	res, e := h.Get("http://unix/pair-pending")
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var pending []PendingPair
	if json.NewDecoder(res.Body).Decode(&pending) != nil || len(pending) != 1 || pending[0].Grant != "valid" {
		t.Fatal("admin list uses wrong clock")
	}
}
