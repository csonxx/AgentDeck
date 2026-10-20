package store

import "testing"

func TestQuestionPositionSurvivesResolutionWithoutMovingCursor(t *testing.T) {
	s, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	first, _, e := s.Append(Event{ID: "source", Session: "a", Item: "ask", Kind: "message", Text: "choose"})
	if e != nil {
		t.Fatal(e)
	}
	card, _, e := s.Append(Event{ID: "card", Session: "a", Item: "q", Kind: "approval", Position: first.Seq, Text: "choices", Status: "pending"})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Append(Event{ID: "resolved", Session: "a", Item: "q", Kind: "approval", Text: "choices", Status: "resolved"}); e != nil {
		t.Fatal(e)
	}
	r, e := s.TranscriptItem("a", "q", "approval")
	if e != nil || r.Position != first.Seq || r.First != card.Seq || r.Status != "resolved" {
		t.Fatal("resolution moved or erased question anchor", e)
	}
}
