package server

import (
	"agentdeck/internal/agent"
	"context"
	"testing"
)

func TestCodexResetWireRequiresPairedOwnerAndExplicitConfirmation(t *testing.T) {
	s, ts := setupServer(t)
	s.Manager = &agent.Manager{Context: context.Background(), Store: s.Store}
	// No native account operation is needed to reject an unpaired caller.
	for _, method := range []string{"usage_resets", "usage_reset_history", "usage_reset_prepare", "usage_reset_consume"} {
		if _, err := s.inputRequest("unpaired", Request{Method: method, Agent: "codex"}); err == nil {
			t.Fatal("unpaired reset operation accepted", method)
		}
	}
	p := pairPhone(t, s, ts)
	p.connect(t, ts, s)
	p.read(t, "sessions")
	// These rejection paths cannot invoke the real account API or use a card.
	for _, req := range []Request{
		{ID: "reset-missing-confirm", Method: "usage_reset_consume", Agent: "codex", ResetIntent: "nonexistent"},
		{ID: "reset-wrong-agent", Method: "usage_resets", Agent: "claude"},
		{ID: "reset-no-intent", Method: "usage_reset_consume", Agent: "codex", Confirmed: true, ResetIntent: "nonexistent"},
	} {
		p.send(t, req)
		result := p.read(t, "error")
		if result["id"] != req.ID {
			t.Fatal("encrypted response lost request identity")
		}
	}
}
