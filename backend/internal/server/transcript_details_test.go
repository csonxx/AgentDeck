package server

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"agentdeck/internal/store"
)

func TestCompactEmptyHistoryRetainsArray(t *testing.T) {
	s, _ := setupServer(t)
	for _, compact := range []bool{false, true} {
		page := s.projectHistory(store.HistoryPage{Events: []store.Event{}}, compact)
		raw, _ := json.Marshal(page)
		if !bytes.Contains(raw, []byte(`"events":[]`)) {
			t.Fatal("empty history became null")
		}
	}
}

func TestCompactHistoryKeepsFullVaultAndQuestionAnchor(t *testing.T) {
	s, ts := setupServer(t)
	for _, event := range []store.Event{
		{ID: "ask", Session: "a", Item: "question", Kind: "message", Role: "assistant", Text: "Pick a choice"},
		{ID: "tool", Session: "a", Item: "call", Kind: "tool_call", Text: "shell\n" + strings.Repeat("PRIVATE_EXECUTION", 1000)},
		{ID: "later", Session: "a", Item: "later", Kind: "message", Role: "assistant", Text: "Later reply"},
		{ID: "choice", Session: "a", Item: "choices", Kind: "approval", Text: `{"question_kind":"codex_async","source_item":"question","questions":[]}`},
	} {
		if _, _, e := s.Store.Append(event); e != nil {
			t.Fatal(e)
		}
	}
	p := pairPhone(t, s, ts)
	p.connect(t, ts, s)
	p.send(t, Request{Method: "watch", Session: "a", Recent: true, Compact: true})
	var page store.HistoryPage
	m := p.read(t, "history")
	raw, _ := json.Marshal(m["page"])
	json.Unmarshal(raw, &page)
	if len(page.Events) != 4 || !page.Events[1].Detail || page.Events[1].Text != "shell" || page.Events[3].Position != page.Events[0].First || page.Before != page.Events[0].First {
		t.Fatal("compact page lost body boundary, question anchor or cursor")
	}
	p.read(t, "watching")
	p.send(t, Request{ID: "detail", Method: "item_detail", Session: "a", Item: "call", Kind: "tool_call"})
	m = p.read(t, "result")
	var detail store.Event
	raw, _ = json.Marshal(m["result"])
	json.Unmarshal(raw, &detail)
	if detail.Detail || !strings.Contains(detail.Text, "PRIVATE_EXECUTION") || detail.First != page.Events[1].First {
		t.Fatal("explicit detail lost original body")
	}
	// Live updates stay compact, and a stale resolved question retains its anchor.
	s.Store.Append(store.Event{ID: "tool-update", Session: "a", Item: "call", Kind: "tool_call", Text: "shell\nNEW_PRIVATE_BODY", Mode: "snapshot"})
	m = p.read(t, "events")
	raw, _ = json.Marshal(m["events"])
	var events []store.Event
	json.Unmarshal(raw, &events)
	for _, event := range events {
		if event.Kind == "tool_call" && (!event.Detail || strings.Contains(event.Text, "NEW_PRIVATE_BODY")) {
			t.Fatal("live stream downloaded collapsed details")
		}
	}
	p.send(t, Request{ID: "foreign", Method: "item_detail", Session: "b", Item: "call", Kind: "tool_call"})
	p.read(t, "error")
	p.send(t, Request{ID: "unsupported", Method: "item_detail", Session: "a", Item: "later", Kind: "message"})
	p.read(t, "error")
	full, e := s.Store.History("a", 0, 40)
	if e != nil || !strings.Contains(full.Events[1].Text, "NEW_PRIVATE_BODY") || full.Events[1].Detail {
		t.Fatal("wire projection modified stored content")
	}
}
