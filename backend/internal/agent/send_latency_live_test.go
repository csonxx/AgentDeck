package agent

import (
	"agentdeck/internal/store"
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

// A non-existent saved UUID is a read-only owner probe, not a created chat.
// Use this to detect silent follower connections delaying an owner miss.
func TestDesktopOwnerMissReadOnlyLatency(t *testing.T) {
	socket, id := os.Getenv("AGENTDECK_PROFILE_SOCKET"), os.Getenv("AGENTDECK_PROFILE_UNOWNED")
	if socket == "" || id == "" {
		t.Skip("explicit read-only unowned Desktop probe")
	}
	b := &DesktopBridge{Socket: socket}
	if owned := os.Getenv("AGENTDECK_PROFILE_OWNED"); owned != "" {
		ctx, cancel := context.WithCancel(context.Background())
		var workers sync.WaitGroup
		defer workers.Wait()
		defer cancel()
		ready := make(chan struct{}, 2)
		for i := 0; i < 2; i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				var once sync.Once
				_ = b.Follow(ctx, owned, func(map[string]any) error { once.Do(func() { ready <- struct{}{} }); return nil })
			}()
		}
		for i := 0; i < 2; i++ {
			select {
			case <-ready:
			case <-time.After(5 * time.Second):
				t.Fatal("read-only followers did not attach")
			}
		}
		if ms, _ := strconv.Atoi(os.Getenv("AGENTDECK_PROFILE_IDLE_MS")); ms > 0 && ms <= 22000 {
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}
	}
	start := time.Now()
	var err error
	if os.Getenv("AGENTDECK_PROFILE_INPUT_PROBE") == "1" {
		_, err = b.connectWithDiscoveryBudget(context.Background(), id, time.Second)
	} else {
		_, err = b.Read(context.Background(), id)
	}
	t.Logf("read_only owner_miss_ms=%d missing=%t probe_timeout=%t", time.Since(start).Milliseconds(), errors.Is(err, errDesktopNoOwner), errors.Is(err, errDesktopDiscoveryTimeout))
	if !errors.Is(err, errDesktopNoOwner) && !(os.Getenv("AGENTDECK_PROFILE_INPUT_PROBE") == "1" && errors.Is(err, errDesktopDiscoveryTimeout)) {
		t.Fatal("expected an unowned probe; no input sent", err)
	}
}

// Read-only: never opens a chat, starts a turn or sends account mutations.
func TestDesktopSendPrerequisitesReadOnlyLatency(t *testing.T) {
	id, socket, project := os.Getenv("AGENTDECK_PROFILE_SESSION"), os.Getenv("AGENTDECK_PROFILE_SOCKET"), os.Getenv("AGENTDECK_PROFILE_PROJECT")
	if id == "" || socket == "" || project == "" {
		t.Skip("explicit read-only Desktop latency diagnostic")
	}
	b := &DesktopBridge{Socket: socket}
	db, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := &Manager{Store: db, Roots: []string{project}}
	if err = db.Put("sessions", id, store.Session{ID: id, Agent: "codex", Project: project}); err != nil {
		t.Fatal(err)
	}
	modelsAt := time.Now()
	choices, err := m.Models("codex", id)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("read_only cold_model_catalog_ms=%d choices=%d", time.Since(modelsAt).Milliseconds(), len(choices))
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		start := time.Now()
		c, err := b.connect(ctx, id)
		connected := time.Now()
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		_, err = c.snapshot(id, project)
		done := time.Now()
		c.conn.Close()
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("read_only round=%d discovery_ms=%d snapshot_ms=%d total_ms=%d", i+1, connected.Sub(start).Milliseconds(), done.Sub(connected).Milliseconds(), done.Sub(start).Milliseconds())
	}
}
