package agent

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// Read-only native schema acceptance. Never prompts, changes settings, resumes,
// takes a writer, or replies in a user's working chat.
func TestDesktopAsyncQuestionSchemaReadOnlyLive(t *testing.T) {
	socket, id := os.Getenv("AGENTDECK_DESKTOP_SOCKET"), os.Getenv("AGENTDECK_DESKTOP_THREAD")
	if socket == "" || id == "" {
		t.Skip("explicit read-only native question metadata acceptance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	found := false
	questions := 0
	pending, answers := 0, 0
	err := (&DesktopBridge{Socket: socket}).Follow(ctx, id, func(state map[string]any) error {
		for _, rawTurn := range desktopTurns(state) {
			for _, rawItem := range arr(obj(rawTurn)["items"]) {
				item := obj(rawItem)
				if item["type"] != "agentMessage" {
					continue
				}
				for _, raw := range arr(item["questions"]) {
					q := obj(raw)
					if str(q["title"]) == "" {
						return errors.New("native async question title schema mismatch")
					}
					for _, option := range arr(q["options"]) {
						if _, ok := option.(string); !ok {
							return errors.New("native async choice schema mismatch")
						}
					}
					questions++
				}
			}
		}
		cards, _ := nativeAsyncCards(state)
		pending = len(cards)
		answers = len(nativeAsyncReplies(state))
		found = true
		cancel()
		return nil
	})
	if !found || err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal("native async schema observation failed", err)
	}
	if questions == 0 {
		t.Fatal("native snapshot contained no async question metadata")
	}
	t.Logf("read-only native async schema confirmed; questions=%d pending_cards=%d accepted_reply_items=%d; no reply or CLI resume", questions, pending, answers)
}
