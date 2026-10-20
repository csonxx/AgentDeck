package agent

// A native question may have no separate text item. Anchor it after the nearest
// preceding visible native item, including when importing an already answered
// question. Persist only its ordinal, so older cached cards can be placed too.
func (p *desktopProjection) questionPosition(state map[string]any, source string) uint64 {
	if source == "" {
		return 0
	}
	if position := p.m.Store.QuestionPosition(p.session.ID, source); position > 0 {
		return position
	}
	var previous []string
	for _, rawTurn := range desktopTurns(state) {
		for _, rawItem := range arr(obj(rawTurn)["items"]) {
			id := str(obj(rawItem)["id"])
			if id == source {
				for i := len(previous) - 1; i >= 0 && i >= len(previous)-128; i-- {
					if position := p.m.Store.QuestionPosition(p.session.ID, previous[i]); position > 0 {
						_ = p.m.Store.Put("agentconfig", "question-position/"+p.session.ID+"/"+source, position)
						return position
					}
				}
				return 0
			}
			if id != "" {
				previous = append(previous, id)
			}
		}
	}
	return 0
}
