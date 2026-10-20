package agent

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestDesktopQuestionsReadOnlyLive(t *testing.T) {
	socket, id := os.Getenv("AGENTDECK_DESKTOP_SOCKET"), os.Getenv("AGENTDECK_DESKTOP_THREAD")
	if socket == "" || id == "" {
		t.Skip("explicit read-only native Desktop schema acceptance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	seen := false
	err := (&DesktopBridge{Socket: socket}).Follow(ctx, id, func(state map[string]any) error {
		if state["id"] != id {
			return errors.New("native conversation identity mismatch")
		}
		requests, ok := state["requests"].([]any)
		if !ok {
			return errors.New("native Desktop stream does not expose a request array")
		}
		seen = true
		t.Logf("native Desktop request array confirmed; pending count %d", len(requests))
		cancel()
		return nil
	})
	if !seen || err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal("read-only native question schema not confirmed", err)
	}
}
