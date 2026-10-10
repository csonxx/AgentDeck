package gateway

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

type connectionLogBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *connectionLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *connectionLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestConnectionCountsTrackRealStreamsAndRevocation(t *testing.T) {
	f := setupPrefix(t, "/agentdeck")
	if s := f.g.connectionStats(); s != (connectionStats{Workstations: 1}) {
		t.Fatalf("online tunnel count: %+v", s)
	}
	f.g.mu.Lock()
	f.g.peers["reserved"] = nil
	f.g.mu.Unlock()
	if s := f.g.connectionStats(); s.Workstations != 1 {
		t.Fatal("pending tunnel counted as online")
	}
	p := pair(t, f)
	// Pairing/session HTTP requests finish without leaving active connections.
	await(t, func() bool { return f.g.connectionStats() == (connectionStats{Workstations: 1}) })
	p.connect(t, f)
	p.read(t, "sessions")
	await(t, func() bool { return f.g.connectionStats() == (connectionStats{Workstations: 1, AppStreams: 1}) })
	q := pair(t, f)
	q.connect(t, f)
	q.read(t, "sessions")
	await(t, func() bool { return f.g.connectionStats() == (connectionStats{Workstations: 1, AppStreams: 2}) })
	_ = p.ws.Close()
	await(t, func() bool { return f.g.connectionStats() == (connectionStats{Workstations: 1, AppStreams: 1}) })
	if err := f.g.RevokeGrant(q.grant.ID); err != nil {
		t.Fatal(err)
	}
	// Consume the server close like the app read loop, then close the client
	// half of the upgraded TCP connection before expecting its handler to exit.
	_ = q.ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := q.ws.ReadMessage(); err == nil {
		t.Fatal("revoked stream stayed readable")
	}
	_ = q.ws.Close()
	await(t, func() bool { return f.g.connectionStats() == (connectionStats{Workstations: 1}) })
	if err := f.g.RevokeLocal(f.link.Local); err != nil {
		t.Fatal(err)
	}
	await(t, func() bool { return f.g.connectionStats() == (connectionStats{}) })
}

func TestPeriodicConnectionLogsAndCancellation(t *testing.T) {
	g := New(nil, "https://example.com", "", "system", time.Second)
	var output connectionLogBuffer
	if err := g.ConfigureLogging(&output, "info"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); g.LogConnectionStats(ctx, 10*time.Millisecond) }()
	t.Cleanup(func() { cancel(); <-done })
	await(t, func() bool { return strings.Count(output.String(), "msg=connection_stats") >= 2 })
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("connection logger did not stop on cancellation")
	}
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		if !strings.Contains(line, "level=INFO msg=connection_stats") || !strings.Contains(line, "active_connections=0 online_workstations=0 app_streams=0 relay_requests=0") || strings.Contains(line, "request_id") || strings.Contains(line, "tenant") {
			t.Fatal("aggregate connection log missing counts or exposing identities")
		}
	}
}

func TestConnectionLogDisabledAndRequestBreakdown(t *testing.T) {
	g := New(nil, "https://example.com", "", "system", time.Second)
	var output bytes.Buffer
	_ = g.ConfigureLogging(&output, "info")
	g.LogConnectionStats(context.Background(), 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g.LogConnectionStats(ctx, time.Second)
	if output.Len() != 0 {
		t.Fatal("disabled/canceled connection logger emitted output")
	}
	g.active["tenant-a"] = 4
	g.active["tenant-b"] = 3
	g.appStreams = 2
	if s := g.connectionStats(); s != (connectionStats{AppStreams: 2, RelayRequests: 5}) {
		t.Fatalf("HTTP requests counted as persistent connections: %+v", s)
	}
}
