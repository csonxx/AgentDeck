package server

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agentdeck/internal/agent"
	"agentdeck/internal/store"
)

// Only a synthetic native adapter is used: no real Desktop session receives input.
type slowDesktop struct {
	project string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func (d *slowDesktop) Read(_ context.Context, id string) (agent.DesktopThread, error) {
	return agent.DesktopThread{ID: id, Kind: "codex", Host: "local", Project: d.project}, nil
}

func (d *slowDesktop) Send(_ context.Context, _, _, _, _ string) error {
	d.calls.Add(1)
	d.once.Do(func() { close(d.entered) })
	<-d.release
	return nil
}

func TestSlowNativeSendDoesNotBlockWarmSessionSwitch(t *testing.T) {
	s, ts := setupServer(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d := &slowDesktop{project: root, entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	unblock := func() { release.Do(func() { close(d.release) }) }
	t.Cleanup(unblock)
	s.Manager = &agent.Manager{Context: context.Background(), Store: s.Store, Roots: []string{root}, Desktop: d}
	t.Cleanup(s.Manager.Close)
	for _, id := range []string{"sending", "reading"} {
		_, _, err = s.Store.Append(store.Event{ID: id, Session: id, Project: root, Agent: "codex", Source: "desktop", Kind: "message", Role: "assistant", Text: "Synthetic history"})
		if err != nil {
			t.Fatal(err)
		}
	}
	p := pairPhone(t, s, ts)
	p.connect(t, ts, s)
	p.read(t, "sessions")
	request := Request{ID: "slow-send", Method: "send", Session: "sending", Text: "Synthetic input"}
	p.send(t, request)
	select {
	case <-d.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("synthetic native send did not start")
	}
	// The connection is already live. Reading another chat and its heartbeat
	// must not wait for the native send acknowledgement from the previous chat.
	p.send(t, Request{ID: "switch", Method: "watch", Session: "reading", Recent: true, Compact: true})
	if history := p.read(t, "history"); history["session"] != "reading" {
		t.Fatal("warm switch returned another session")
	}
	p.read(t, "watching")
	p.send(t, Request{ID: "heartbeat", Method: "ping"})
	if pong := p.read(t, "pong"); pong["id"] != "heartbeat" {
		t.Fatal("heartbeat blocked behind native send")
	}
	// Retrying an in-flight request must still execute exactly once.
	p.send(t, request)
	next := request
	next.ID = "next-send"
	p.send(t, next)
	unblock()
	for _, id := range []string{request.ID, request.ID, next.ID} {
		if result := p.read(t, "result"); result["id"] != id {
			t.Fatal("native commands lost their order or request identity")
		}
	}
	if d.calls.Load() != 2 {
		t.Fatal("retried native send executed more than once")
	}
	request.Text = "Different synthetic input"
	p.send(t, request)
	if response := p.read(t, "error"); response["not_sent"] != true {
		t.Fatal("conflicting reuse of request ID was not rejected")
	}
	p.ws.Close()
	p.connect(t, ts, s)
	p.read(t, "sessions")
	p.send(t, next)
	p.read(t, "result")
	if d.calls.Load() != 2 {
		t.Fatal("reconnect retried a completed native send")
	}
}

func TestDisconnectedStreamFinishesStartedSendWithoutStartingQueuedSend(t *testing.T) {
	s, ts := setupServer(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d := &slowDesktop{project: root, entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	unblock := func() { release.Do(func() { close(d.release) }) }
	t.Cleanup(unblock)
	s.Manager = &agent.Manager{Context: context.Background(), Store: s.Store, Roots: []string{root}, Desktop: d}
	t.Cleanup(s.Manager.Close)
	if err := s.Store.Put("sessions", "sending", store.Session{ID: "sending", Project: root, Agent: "codex", Source: "desktop"}); err != nil {
		t.Fatal(err)
	}
	p := pairPhone(t, s, ts)
	p.connect(t, ts, s)
	p.read(t, "sessions")
	p.send(t, Request{ID: "started", Method: "send", Session: "sending", Text: "Synthetic input"})
	select {
	case <-d.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("synthetic native send did not start")
	}
	p.send(t, Request{ID: "not-started", Method: "send", Session: "sending", Text: "Synthetic input"})
	p.send(t, Request{ID: "barrier", Method: "ping"})
	p.read(t, "pong") // Confirms both commands were read before disconnect.
	p.ws.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		connections, err := s.appConnections()
		if err != nil {
			t.Fatal(err)
		}
		if connections.Online == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stream did not disconnect")
		}
		time.Sleep(time.Millisecond)
	}
	unblock()
	for {
		var outcome struct{ Error string }
		if s.Store.Get("commands", p.device+"/started", &outcome) == nil && outcome.Error == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("started operation lost its durable outcome after disconnect")
		}
		time.Sleep(time.Millisecond)
	}
	if d.calls.Load() != 1 {
		t.Fatal("disconnected stream started another native send")
	}
	var outcome any
	if s.Store.Get("commands", p.device+"/not-started", &outcome) == nil {
		t.Fatal("unstarted command was persisted as dispatched")
	}
}

func TestStreamCommandDoesNotStartAfterDisconnectOrRevocation(t *testing.T) {
	s, _ := setupServer(t)
	d := &slowDesktop{entered: make(chan struct{}), release: make(chan struct{})}
	s.Manager = &agent.Manager{Store: s.Store, Desktop: d}
	command := streamCommand{request: Request{ID: "not-started", Method: "send", Session: "synthetic", Text: "Synthetic input"}}
	stop := make(chan struct{})
	close(stop)
	if response := s.streamCommand("former-device", command, stop); response != nil {
		t.Fatal("disconnected stream still dispatched a command")
	}
	stop = make(chan struct{})
	if response := s.streamCommand("former-device", command, stop); response["type"] != "error" || response["not_sent"] != true {
		t.Fatal("revoked device did not fail before native dispatch")
	}
	if d.calls.Load() != 0 {
		t.Fatal("disconnected or revoked device reached native adapter")
	}
}
