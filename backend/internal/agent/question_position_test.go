package agent

import (
	"agentdeck/internal/store"
	"testing"
)

func TestAsyncQuestionWithoutTextAnchorsBeforeLaterReplies(t *testing.T) {
	for _, answered := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "already-answered"}[answered], func(t *testing.T) {
			m := connectorManager(t)
			ss := store.Session{ID: "original", Agent: "codex", Project: m.Roots[0], Source: "desktop"}
			m.Store.Put("sessions", ss.ID, ss)
			state := asyncFixtureState(ss.ID, ss.Project, "completed")
			_, _, payload := asyncFixtureCard(t, state)
			turn := obj(desktopTurns(state)[0])
			ask := obj(arr(turn["items"])[0])
			ask["text"] = ""
			turn["items"] = []any{map[string]any{"id": "before", "type": "userMessage", "content": []any{map[string]any{"type": "text", "text": "synthetic question"}}}, ask, map[string]any{"id": "later", "type": "agentMessage", "text": "synthetic later reply"}}
			if answered {
				addAsyncReply(state, payload, "accepted")
			}
			p := &desktopProjection{m: m, session: ss, seen: map[string]string{}}
			if e := p.publish(state); e != nil {
				t.Fatal(e)
			}
			before, e := m.Store.TranscriptItem(ss.ID, "before:0", "message")
			if e != nil {
				t.Fatal(e)
			}
			if position := m.Store.QuestionPosition(ss.ID, "ask-1"); position != before.First {
				t.Fatal("empty or answered question has no causal position", position, before.First)
			}
			if !answered {
				cards, _ := nativeAsyncCards(state)
				card, e := m.Store.TranscriptItem(ss.ID, cards[0].Key, "approval")
				if e != nil || card.Position != before.First {
					t.Fatal("card not anchored", e)
				}
			}
		})
	}
}
