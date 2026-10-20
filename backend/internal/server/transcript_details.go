package server

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"agentdeck/internal/store"
)

func executionKind(kind string) bool {
	switch kind {
	case "thinking", "tool_call", "tool_result", "file_change":
		return true
	}
	return false
}

// Projection only: old clients retain the complete protocol and the encrypted
// vault retains all native content. Summaries never materialize as full details.
func compactEvents(events []store.Event) []store.Event {
	out := make([]store.Event, len(events))
	copy(out, events)
	for i := range out {
		if !executionKind(out[i].Kind) {
			continue
		}
		text, _, _ := strings.Cut(out[i].Text, "\n")
		if len(text) > 640 {
			text = text[:640]
			for !utf8.ValidString(text) {
				text = text[:len(text)-1]
			}
		}
		runes := []rune(text)
		if len(runes) > 160 {
			text = string(runes[:160])
		}
		out[i].Text = text
		out[i].Detail = true
		out[i].Mode = "snapshot"
	}
	return out
}
func (s *Server) transcriptEvents(events []store.Event, compact bool) []store.Event {
	if compact {
		events = compactEvents(events)
	} else {
		out := make([]store.Event, len(events))
		copy(out, events)
		events = out
	}
	for i := range events {
		if events[i].Kind != "approval" || events[i].Position != 0 {
			continue
		}
		var params struct {
			Source string `json:"source_item"`
		}
		if json.Unmarshal([]byte(events[i].Text), &params) == nil {
			events[i].Position = s.Store.QuestionPosition(events[i].Session, params.Source)
		}
	}
	return events
}
func (s *Server) projectHistory(page store.HistoryPage, compact bool) store.HistoryPage {
	page.Events = s.transcriptEvents(page.Events, compact)
	return page
}
