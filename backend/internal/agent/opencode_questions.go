package agent

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"agentdeck/internal/store"
)

const openCodeQuestionPrefix = "question/"

func openCodeQuestionParams(p map[string]any) map[string]any {
	v := questionPresentation(p, "opencode")
	questions := []any{}
	for i, raw := range arr(p["questions"]) {
		q := map[string]any{}
		for k, value := range obj(raw) {
			q[k] = value
		}
		// OpenCode replies are positional; stable IDs avoid collisions in repeated text.
		q["id"] = questionID(map[string]any{}, i)
		questions = append(questions, q)
	}
	v["questions"] = questions
	return v
}
func (o *openCode) emitQuestion(ss store.Session, key, text, status string) {
	previous, err := o.m.Store.TranscriptItem(ss.ID, openCodeQuestionPrefix+key, "approval")
	if err == nil {
		if text == "" {
			text = previous.Text
		}
		if previous.Status == status && previous.Text == text {
			return
		}
	}
	ev := store.Event{Kind: "approval", Item: openCodeQuestionPrefix + key, Text: text, Mode: "snapshot", Status: status}
	ev.ID = "opencode-question/" + UUID()
	o.m.emitExternal(ss, ev)
}
func (o *openCode) questionEvent(ss store.Session, key, text, status string) {
	if key == "" {
		return
	}
	o.questionMu.Lock()
	defer o.questionMu.Unlock()
	stateKey := "opencode-questions/" + digest(ss.Project)
	active := map[string]string{}
	_ = o.m.Store.Get("agentconfig", stateKey, &active)
	if active == nil {
		active = map[string]string{}
	}
	id := ss.ID + "\x00" + key
	if status == "pending" {
		active[id] = text
	} else {
		if text == "" {
			text = active[id]
		}
		delete(active, id)
	}
	o.emitQuestion(ss, key, text, status)
	_ = o.m.Store.Put("agentconfig", stateKey, active)
}
func (o *openCode) answerQuestion(ctx context.Context, ss store.Session, key, decision, answer string) error {
	var pending []map[string]any
	if e := o.request(ctx, "GET", "/question", ss.Project, nil, &pending); e != nil {
		return e
	}
	var found map[string]any
	for _, p := range pending {
		if str(p["id"]) == key && str(p["sessionID"]) == nativeID(ss) {
			found = p
			break
		}
	}
	if found == nil {
		return errors.New("提问已过期或不属于当前会话")
	}
	path := "/question/" + url.PathEscape(key) + "/reject"
	var body any = map[string]any{}
	if decision == "accept" {
		answers, e := parseQuestionAnswers(answer)
		if e != nil {
			return e
		}
		qs := questionList(openCodeQuestionParams(found))
		if e = validateQuestionAnswers(qs, answers, "opencode"); e != nil {
			return e
		}
		ordered := [][]string{}
		for i, raw := range qs {
			ordered = append(ordered, answers[questionID(obj(raw), i)])
		}
		body = map[string]any{"answers": ordered}
		path = "/question/" + url.PathEscape(key) + "/reply"
	}
	if e := o.request(ctx, "POST", path, ss.Project, body, nil); e != nil {
		return e
	}
	o.questionEvent(ss, key, jsonText(openCodeQuestionParams(found)), "resolved")
	return nil
}

// Called on initial sync and reconnect, not just while receiving SSE events.
func (o *openCode) syncQuestions(ctx context.Context, project string) error {
	o.questionMu.Lock()
	defer o.questionMu.Unlock()
	var pending []map[string]any
	if e := o.request(ctx, "GET", "/question", project, nil, &pending); e != nil {
		var status openCodeHTTPError
		if errors.As(e, &status) && status.status == 404 {
			return nil
		} // older server without questions
		return e
	}
	active := map[string]string{}
	for _, p := range pending {
		var ss store.Session
		if o.m.Store.Get("sessions", sessionID("opencode", str(p["sessionID"])), &ss) != nil || ss.Project != project {
			continue
		}
		key := str(p["id"])
		if key == "" {
			continue
		}
		text := jsonText(openCodeQuestionParams(p))
		active[ss.ID+"\x00"+key] = text
		o.emitQuestion(ss, key, text, "pending")
	}
	stateKey := "opencode-questions/" + digest(project)
	previous := map[string]string{}
	_ = o.m.Store.Get("agentconfig", stateKey, &previous)
	for id, text := range previous {
		if _, ok := active[id]; ok {
			continue
		}
		pieces := strings.SplitN(id, "\x00", 2)
		if len(pieces) != 2 {
			continue
		}
		var ss store.Session
		if o.m.Store.Get("sessions", pieces[0], &ss) == nil && ss.Project == project {
			o.emitQuestion(ss, pieces[1], text, "resolved")
		}
	}
	return o.m.Store.Put("agentconfig", stateKey, active)
}

// Once the SSE response is open, recover current questions while subsequent
// events are buffered by the server. This closes the list/subscribe race.
func (o *openCode) recoverQuestions(ctx context.Context) error {
	sessions, err := o.m.Store.Sessions()
	if err != nil {
		return err
	}
	projects := map[string]bool{}
	for _, ss := range sessions {
		if ss.Agent != "opencode" || projects[ss.Project] {
			continue
		}
		if _, e := o.m.allowed(ss.Project); e != nil {
			continue
		}
		projects[ss.Project] = true
		if e := o.syncQuestions(ctx, ss.Project); e != nil {
			return e
		}
	}
	return nil
}
