package agent

import (
	"agentdeck/internal/store"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type deliveryFixture struct {
	desktopFixture
	status  string
	count   int
	steered string
	fail    bool
}

func (f *deliveryFixture) CurrentStatus(context.Context, string, string) (string, error) {
	return f.status, nil
}
func (f *deliveryFixture) Send(_ context.Context, id, text, request, project string) error {
	f.count++
	f.sentID = id
	f.text = text
	if f.fail {
		return errors.New("uncertain transport")
	}
	return nil
}
func (f *deliveryFixture) SteerInput(_ context.Context, id, text, request, project string, input []any) error {
	f.steered = id
	return nil
}
func TestDeliveryQueueWaitsCancelsResumesAndDoesNotRetryUnknown(t *testing.T) {
	n := setup(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	ss := store.Session{ID: "original", Agent: "codex", Source: "desktop", Managed: true, Project: root, Status: "running"}
	n.Store.Put("sessions", ss.ID, ss)
	f := &deliveryFixture{desktopFixture: desktopFixture{thread: DesktopThread{ID: ss.ID, Kind: "codex", Host: "local", Project: root}}, status: "running"}
	m := &Manager{Store: n.Store, Roots: []string{root}, Desktop: f}
	defer m.Close()
	enqueue := func(id, text string) {
		t.Helper()
		if e := m.SendDelivery(ss.ID, text, id, TurnOptions{}, nil, "queue"); e != nil {
			t.Fatal(e)
		}
		var d store.Delivery
		n.Store.Get("deliveries", id, &d)
		d.Created = time.Now().Add(-time.Minute).Format(time.RFC3339Nano)
		n.Store.Put("deliveries", id, d)
	}
	enqueue("first", "Queue one")
	m.dispatchDeliveries(context.Background())
	if f.count != 0 {
		t.Fatal("sent during current turn")
	}
	ss.Status = "completed"
	n.Store.Put("sessions", ss.ID, ss)
	m.dispatchDeliveries(context.Background())
	if f.count != 0 {
		t.Fatal("trusted stale log state instead of native busy state")
	}
	f.status = "completed"
	m.dispatchDeliveries(context.Background())
	if f.count != 1 || f.sentID != ss.ID || f.text != "Queue one" {
		t.Fatal("original desktop delivery missing")
	}
	enqueue("cancel", "Never send")
	if e := m.CancelDelivery(ss.ID, "cancel"); e != nil {
		t.Fatal(e)
	}
	m.dispatchDeliveries(context.Background())
	if f.count != 1 {
		t.Fatal("cancelled message was delivered")
	}
	ss.Status = "completed"
	n.Store.Put("sessions", ss.ID, ss)
	f.fail = true
	enqueue("uncertain", "Unconfirmed")
	m.dispatchDeliveries(context.Background())
	m.dispatchDeliveries(context.Background())
	if f.count != 2 {
		t.Fatal("uncertain submission retried")
	}
	enqueue("next", "Wait behind uncertain")
	m.dispatchDeliveries(context.Background())
	if f.count != 2 {
		t.Fatal("queue continued after unconfirmed delivery")
	}
	if e := m.CancelDelivery(ss.ID, "uncertain"); e != nil {
		t.Fatal(e)
	}
	if e := m.SendDelivery(ss.ID, "Steer", "steer", TurnOptions{}, nil, "steer"); e != nil || f.steered != ss.ID {
		t.Fatal("steering did not target original ID")
	}
	page, _ := n.Store.History(ss.ID, 0, 40)
	found := false
	for _, ev := range page.Events {
		if ev.Kind == "queued_message" && ev.Item == "queue/cancel" && ev.Role == "cancelled" {
			found = true
		}
	}
	if !found {
		t.Fatal("cancel status did not replace queue card")
	}
}

func TestRestartedInFlightDeliveryBecomesUnknownWithoutRetry(t *testing.T) {
	n := setup(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	ss := store.Session{ID: "original", Agent: "codex", Source: "desktop", Managed: true, Project: root, Status: "completed"}
	n.Store.Put("sessions", ss.ID, ss)
	n.Store.EnqueueDelivery(store.Delivery{ID: "lost-ack", Session: ss.ID, State: "dispatching", Created: time.Now().Add(-time.Minute).Format(time.RFC3339Nano)}, queuedInput{Text: "Already possibly sent"})
	m := &Manager{Store: n.Store, Roots: []string{root}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.RunDeliveries(ctx)
	m.Close()
	var d store.Delivery
	if e := n.Store.Get("deliveries", "lost-ack", &d); e != nil || d.State != "unknown" {
		t.Fatal("in-flight delivery was not quarantined", e)
	}
}

func TestClaudeInterruptThenContinueKeepsQueuedInputSeparate(t *testing.T) {
	fixture, e := os.ReadFile("../../../scripts/fixture_runtime.py")
	if e != nil {
		t.Fatal(e)
	}
	bin := t.TempDir()
	if e = os.WriteFile(filepath.Join(bin, "claude"), fixture, 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	n := setup(t)
	root := t.TempDir()
	m := &Manager{Store: n.Store, Normalizer: n, Roots: []string{root}}
	defer m.Close()
	ss, e := m.Create("claude", m.Projects()[0].ID, "")
	if e != nil {
		t.Fatal(e)
	}
	if e = m.SendInput(ss.ID, "pause current fixture", UUID(), TurnOptions{}, nil); e != nil {
		t.Fatal(e)
	}
	queued := UUID()
	if e = m.SendDelivery(ss.ID, "queued fixture", queued, TurnOptions{}, nil, "queue"); e != nil {
		t.Fatal(e)
	}
	if e = m.SendDelivery(ss.ID, "continue fixture", UUID(), TurnOptions{}, nil, "steer"); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		events, _ := n.Store.Events(ss.ID, 0, 100)
		stopped, continued := false, false
		for _, ev := range events {
			if ev.Role == "assistant" && ev.Text == "STOPPED" {
				stopped = true
			}
			if stopped && ev.Role == "assistant" && ev.Text == "RUNTIME_OK" {
				continued = true
			}
		}
		if continued {
			var d store.Delivery
			if e = n.Store.Get("deliveries", queued, &d); e != nil || d.State != "queued" {
				t.Fatal("interrupt consumed queued input", e)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("interrupt did not complete before continuation")
}
