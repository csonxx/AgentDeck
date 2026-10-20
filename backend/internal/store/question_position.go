package store

// Display anchors do not alter the durable first-appearance index or cursors.
// Native assistant messages use the exact ID; user text blocks use :0.
func (s *Store) QuestionPosition(session, source string) uint64 {
	if source == "" {
		return 0
	}
	for _, id := range []string{source, source + ":0", source + ":summary:0"} {
		for _, kind := range []string{"message", "tool_call", "thinking", "file_change"} {
			if event, e := s.TranscriptItem(session, id, kind); e == nil {
				return event.First
			}
		}
	}
	var position uint64
	_ = s.Get("agentconfig", "question-position/"+session+"/"+source, &position)
	if position > 0 {
		return position
	}
	return 0
}
