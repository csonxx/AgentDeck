package agent

import (
	"agentdeck/internal/store"
	"context"
	"os"
	"testing"
	"time"
)

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
