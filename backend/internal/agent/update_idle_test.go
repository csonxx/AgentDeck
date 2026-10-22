package agent

import (
	"agentdeck/internal/store"
	"context"
	"testing"
)

func TestUpdateWaitsForWorkAndPendingApprovals(t *testing.T) {
	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := &Manager{Store: s}
	if !m.PrepareUpdate(context.Background()) {
		t.Fatal("empty workstation not idle")
	}
	m.ResumeAfterUpdate()
	_, _, err = s.Append(store.Event{ID: "approval", Session: "chat", Agent: "codex", Kind: "approval", Item: "q", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if m.PrepareUpdate(context.Background()) {
		t.Fatal("pending approval interrupted")
	}
	_, _, _ = s.Append(store.Event{ID: "resolved", Session: "chat", Agent: "codex", Kind: "approval", Item: "q", Status: "resolved"})
	if !m.PrepareUpdate(context.Background()) {
		t.Fatal("resolved approval still blocks")
	}
	m.ResumeAfterUpdate()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if m.PrepareUpdate(ctx) {
		t.Fatal("expired prepare request froze delivery")
	}
	_, _, _ = s.Append(store.Event{ID: "running", Session: "chat", Agent: "codex", Kind: "status", Status: "running"})
	if m.PrepareUpdate(context.Background()) {
		t.Fatal("running work interrupted")
	}
}

func TestUpdateWaitsForResetReceiptAndRejectsNewReset(t *testing.T) {
	m := connectorManager(t)
	raw := resetFixture()
	entered, release := make(chan struct{}), make(chan struct{})
	m.resetRPC = func(_ context.Context, method string, _ map[string]any) (map[string]any, error) {
		if method == "account/rateLimits/read" {
			return raw, nil
		}
		close(entered)
		<-release
		return map[string]any{"outcome": "reset"}, nil
	}
	q, err := m.PrepareCodexReset("phone", "credit-test", normalizeResets(raw).AccountVersion)
	if err != nil {
		t.Fatal(err)
	}
	key := UUID()
	done := make(chan error, 1)
	go func() { _, err := m.ConsumeCodexReset("phone", q.Intent, key, true); done <- err }()
	<-entered
	if m.PrepareUpdate(context.Background()) {
		close(release)
		<-done
		t.Fatal("update interrupted reset receipt")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !m.PrepareUpdate(context.Background()) {
		t.Fatal("completed reset still blocks update")
	}
	m.resetRPC = func(context.Context, string, map[string]any) (map[string]any, error) {
		t.Error("draining reset reached RPC")
		return nil, nil
	}
	if _, err := m.PrepareCodexReset("phone", "credit-test", normalizeResets(raw).AccountVersion); err == nil {
		t.Fatal("draining prepare accepted")
	}
	if _, err := m.ConsumeCodexReset("phone", q.Intent, key, true); err == nil {
		t.Fatal("draining consume accepted")
	}
	m.ResumeAfterUpdate()
	if result, err := m.ConsumeCodexReset("phone", q.Intent, key, true); err != nil || result.Outcome != "reset" {
		t.Fatal("completed receipt lost", err)
	}
}
