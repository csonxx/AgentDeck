package agent

import (
	"agentdeck/internal/store"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const asyncQuestionPrefix = "desktop-question/async/"
const asyncReplyStart = "<send_user_message_question_reply>"
const asyncReplyEnd = "</send_user_message_question_reply>"

type asyncReply struct {
	QuestionItemID string `json:"questionItemId"`
	Question       string `json:"question"`
	Answer         string `json:"answer"`
}
type asyncAnswerRecord struct{ State, Fingerprint string }
type asyncQuestionCard struct {
	Key, Source string
	Questions   []any
}

func asyncQuestionID(item string, index int) string {
	return jsonText([]any{"request_user_input_async", item, index})
}
func parseAsyncReplies(text string) []asyncReply {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, asyncReplyStart) || !strings.HasSuffix(text, asyncReplyEnd) {
		return nil
	}
	text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, asyncReplyStart), asyncReplyEnd))
	var replies []asyncReply
	if json.Unmarshal([]byte(text), &replies) != nil {
		var one asyncReply
		if json.Unmarshal([]byte(text), &one) != nil {
			return nil
		}
		replies = []asyncReply{one}
	}
	if len(replies) == 0 || len(replies) > 64 {
		return nil
	}
	for _, r := range replies {
		if r.QuestionItemID == "" || r.Question == "" || r.Answer == "" {
			return nil
		}
	}
	return replies
}

// Match an optimistic ACK, accepted steering item, admitted user message and
// rollout record by native interaction identity, never by generic message text.
func asyncReplyItemID(text string) string {
	replies := parseAsyncReplies(text)
	if replies == nil {
		return ""
	}
	return "codex-async-reply/" + digest(jsonText(replies))
}
func asyncReplyDisplay(text string) string {
	replies := parseAsyncReplies(text)
	if replies == nil {
		return text
	}
	var lines []string
	for _, r := range replies {
		lines = append(lines, r.Question+"\n"+r.Answer)
	}
	return strings.Join(lines, "\n\n")
}
func nativeAsyncReplies(state map[string]any) map[string]asyncReply {
	out := map[string]asyncReply{}
	for _, rawTurn := range desktopTurns(state) {
		for _, rawItem := range arr(obj(rawTurn)["items"]) {
			item := obj(rawItem)
			var content []any
			switch item["type"] {
			case "userMessage":
				content = arr(item["content"])
			case "steeringUserMessage":
				if item["status"] != "accepted" {
					continue
				}
				content = arr(item["input"])
			default:
				continue
			}
			if len(content) != 1 || obj(content[0])["type"] != "text" {
				continue
			}
			for _, r := range parseAsyncReplies(str(obj(content[0])["text"])) {
				out[r.QuestionItemID] = r
			}
		}
	}
	return out
}
func nativeAsyncCards(state map[string]any) ([]asyncQuestionCard, map[string]bool) {
	answered := nativeAsyncReplies(state)
	observed := map[string]bool{}
	cards := []asyncQuestionCard{}
	for _, rawTurn := range desktopTurns(state) {
		turn := obj(rawTurn)
		for _, rawItem := range arr(turn["items"]) {
			item := obj(rawItem)
			source := str(item["id"])
			if item["type"] != "agentMessage" || source == "" {
				continue
			}
			rawQuestions, known := item["questions"].([]any)
			if !known {
				continue
			}
			observed[source] = true
			if len(rawQuestions) > 64 {
				continue
			}
			questions := []any{}
			valid := true
			for i, raw := range rawQuestions {
				q := obj(raw)
				title := str(q["title"])
				qid := asyncQuestionID(source, i)
				if _, ok := answered[qid]; ok {
					continue
				}
				if title == "" || len(title) > 16<<10 {
					valid = false
					break
				}
				options := []any{}
				rawOptions, ok := q["options"].([]any)
				if q["options"] != nil && !ok {
					valid = false
					break
				}
				if len(rawOptions) > 100 {
					valid = false
					break
				}
				for _, option := range rawOptions {
					label, ok := option.(string)
					if !ok || label == "" || len(label) > 4<<10 {
						valid = false
						break
					}
					options = append(options, map[string]any{"label": label})
				}
				questions = append(questions, map[string]any{"id": qid, "question": title, "options": options, "isOther": true})
			}
			if !valid || len(questions) == 0 {
				continue
			}
			key := asyncQuestionPrefix + digest(jsonText([]any{turn["turnId"], source, questions}))
			cards = append(cards, asyncQuestionCard{Key: key, Source: source, Questions: questions})
		}
	}
	return cards, observed
}
func asyncAnswerRequestID(key, payload string) string {
	h := digest(key + payload)
	return h[:8] + "-" + h[8:12] + "-4" + h[13:16] + "-8" + h[17:20] + "-" + h[20:32]
}
func asyncRecordKey(session, key string) string {
	return "codex-async-answer/" + session + "/" + strings.TrimPrefix(key, asyncQuestionPrefix)
}
func (p *desktopProjection) publishAsyncQuestions(state map[string]any) error {
	stateKey := "codex-native-async-questions/" + p.session.ID
	if p.asyncQuestions == nil {
		p.asyncQuestions = map[string]string{}
		_ = p.m.Store.Get("agentconfig", stateKey, &p.asyncQuestions)
		if p.asyncQuestions == nil {
			p.asyncQuestions = map[string]string{}
		}
	}
	// Scan full loaded history on attach/history expansion or a new accepted
	// answer. Token deltas otherwise inspect only the latest two turns.
	turns := desktopTurns(state)
	recent := turns
	if len(recent) > 2 {
		recent = recent[len(recent)-2:]
	}
	scanState := map[string]any{"turns": recent}
	boundary := []any{len(turns)}
	if len(turns) > 0 {
		boundary = append(boundary, obj(turns[0])["turnId"], len(arr(obj(turns[0])["items"])), obj(turns[len(turns)-1])["turnId"])
	}
	stamp := digest(jsonText([]any{boundary, nativeAsyncReplies(scanState)}))
	if p.asyncScanStamp != stamp {
		scanState = state
	}
	for _, turn := range desktopTurns(scanState) {
		for _, raw := range arr(obj(turn)["items"]) {
			item := obj(raw)
			if len(arr(item["questions"])) > 0 {
				p.questionPosition(scanState, str(item["id"]))
			}
		}
	}
	cards, observed := nativeAsyncCards(scanState)
	answered := nativeAsyncReplies(scanState)
	active := map[string]string{}
	for _, card := range cards {
		var receipt asyncAnswerRecord
		_ = p.m.Store.Get("agentconfig", asyncRecordKey(p.session.ID, card.Key), &receipt)
		if receipt.State == "accepted" || receipt.State == "dismissed" {
			continue
		}
		text := Redact(jsonText(map[string]any{"question_kind": "codex_async", "source_item": card.Source, "questions": card.Questions}))
		position := p.questionPosition(scanState, card.Source)
		active[card.Key] = text
		if p.asyncQuestions[card.Key] == text {
			old, e := p.m.Store.TranscriptItem(p.session.ID, card.Key, "approval")
			if e == nil && old.Status == "pending" && old.Text == text && old.Position == position {
				continue
			}
		}
		if e := p.append(store.Event{Session: p.session.ID, Agent: "codex", Project: p.session.Project, Source: "desktop-live", Kind: "approval", Item: card.Key, Text: text, Position: position, Mode: "snapshot", Status: "pending"}); e != nil {
			return e
		}
	}
	for key, text := range p.asyncQuestions {
		if _, ok := active[key]; ok {
			continue
		}
		var params map[string]any
		_ = json.Unmarshal([]byte(text), &params)
		known := observed[str(params["source_item"])]
		for _, raw := range questionList(params) {
			if _, ok := answered[str(obj(raw)["id"])]; ok {
				known = true
			}
		}
		var receipt asyncAnswerRecord
		_ = p.m.Store.Get("agentconfig", asyncRecordKey(p.session.ID, key), &receipt)
		if receipt.State == "dispatching" || receipt.State == "unknown" {
			values := map[string]string{}
			for _, raw := range questionList(params) {
				q := obj(raw)
				r, ok := answered[str(q["id"])]
				if ok && r.Question == str(q["question"]) {
					values[r.QuestionItemID] = r.Answer
				}
			}
			if len(values) == len(questionList(params)) {
				payload, e := asyncQuestionPayload(params, jsonText(values))
				if e == nil && digest(payload) == receipt.Fingerprint {
					receipt.State = "accepted"
					if e = p.m.Store.Put("agentconfig", asyncRecordKey(p.session.ID, key), receipt); e != nil {
						return e
					}
				}
			}
		}
		known = known || receipt.State == "accepted" || receipt.State == "dismissed"
		// Partial history is not proof that an offscreen question was answered.
		if !known {
			active[key] = text
			continue
		}
		if e := p.append(store.Event{Session: p.session.ID, Agent: "codex", Project: p.session.Project, Source: "desktop-live", Kind: "approval", Item: key, Text: text, Mode: "snapshot", Status: "resolved"}); e != nil {
			return e
		}
	}
	if jsonText(active) != jsonText(p.asyncQuestions) {
		if e := p.m.Store.Put("agentconfig", stateKey, active); e != nil {
			return e
		}
	}
	p.asyncQuestions = active
	p.asyncScanStamp = stamp
	return nil
}
func asyncQuestionPayload(params map[string]any, answer string) (string, error) {
	answers, e := parseQuestionAnswers(answer)
	if e != nil {
		return "", e
	}
	questions := questionList(params)
	if e = validateQuestionAnswers(questions, answers, "codex_async"); e != nil {
		return "", e
	}
	replies := []asyncReply{}
	for i, raw := range questions {
		q := obj(raw)
		key := questionID(q, i)
		replies = append(replies, asyncReply{QuestionItemID: key, Question: str(q["question"]), Answer: answers[key][0]})
	}
	payload := asyncReplyStart + "\n" + jsonText(replies) + "\n" + asyncReplyEnd
	if len(payload) > 64<<10 {
		return "", errors.New("消息为空或超过 64 KiB")
	}
	return payload, nil
}

// Async choices are ordinary human input on the existing owner, not permission
// grants or a new CLI. Confirmation-only retries never resend an unknown ACK.
func (b *DesktopBridge) AnswerAsyncQuestions(ctx context.Context, id, project, key, payload string, confirmOnly bool) error {
	budget := 12 * time.Second
	if !confirmOnly {
		budget = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(nonNil(ctx), budget)
	defer cancel()
	notSent := func(e error) error { return &InputNotSentError{Err: e} }
	var c *desktopConnection
	var e error
	if confirmOnly {
		c, e = b.connect(ctx, id)
	} else {
		c, e = b.connectForInput(ctx, id)
	}
	if e != nil {
		return notSent(e)
	}
	defer c.conn.Close()
	stop := context.AfterFunc(ctx, func() { c.conn.Close() })
	defer stop()
	state, e := c.snapshot(id, project)
	if e != nil {
		return notSent(e)
	}
	expected := parseAsyncReplies(payload)
	if expected == nil {
		return notSent(errors.New("提问回答格式无效"))
	}
	accepted := nativeAsyncReplies(state)
	confirmed := true
	for _, r := range expected {
		if accepted[r.QuestionItemID] != r {
			confirmed = false
			break
		}
	}
	if confirmed {
		return nil
	}
	if confirmOnly {
		return errors.New("电脑 Codex 尚未确认回答，请刷新核对，勿重复提交")
	}
	cards, _ := nativeAsyncCards(state)
	var current *asyncQuestionCard
	for _, card := range cards {
		if card.Key == key {
			copy := card
			current = &copy
			break
		}
	}
	if current == nil {
		return notSent(errors.New("提问已过期或已在其他设备处理，请刷新"))
	}
	if len(current.Questions) != len(expected) {
		return notSent(errors.New("提问已过期或已在其他设备处理，请刷新"))
	}
	for i, r := range expected {
		q := obj(current.Questions[i])
		if q["id"] != r.QuestionItemID || q["question"] != r.Question {
			return notSent(errors.New("提问已过期或已在其他设备处理，请刷新"))
		}
	}
	if e = checkSessionInput(store.Session{Issue: desktopSessionIssue(state)}); e != nil {
		return notSent(e)
	}
	request := asyncAnswerRequestID(key, payload)
	inputs := []any{map[string]any{"type": "text", "text": payload, "text_elements": []any{}}}
	if desktopTurnStatus(state) == "running" {
		restore := map[string]any{"id": request, "text": payload, "images": []any{}, "context": map[string]any{"commentAttachments": []any{}}}
		_, e = c.request("thread-follower-steer-turn", 1, map[string]any{"conversationId": id, "input": inputs, "clientUserMessageId": request, "restoreMessage": restore, "attachments": []any{}}, c.owner)
	} else {
		req := map[string]any{"threadId": id, "cwd": project, "input": inputs, "clientUserMessageId": request}
		_, e = c.request("thread-follower-start-turn", 2, map[string]any{"conversationId": id, "turnStart": map[string]any{"request": req, "context": map[string]any{"inheritThreadSettings": true}}}, c.owner)
	}
	return e
}
func (m *Manager) answerDesktopAsync(ss store.Session, key, decision, answer string) error {
	if _, e := m.allowed(ss.Project); e != nil {
		return e
	}
	previous, e := m.Store.TranscriptItem(ss.ID, key, "approval")
	if e != nil {
		return errors.New("提问已过期或已在其他设备处理，请刷新")
	}
	var params map[string]any
	if json.Unmarshal([]byte(previous.Text), &params) != nil || params["question_kind"] != "codex_async" {
		return errors.New("提问回答格式无效")
	}
	recordKey := asyncRecordKey(ss.ID, key)
	var old asyncAnswerRecord
	_ = m.Store.Get("agentconfig", recordKey, &old)
	if decision == "decline" {
		if old.State != "" && old.State != "dismissed" {
			return errors.New("提问已过期或已在其他设备处理，请刷新")
		}
		// Match native async Skip: dismiss the App card without answering or stopping.
		if e = m.Store.Put("agentconfig", recordKey, asyncAnswerRecord{State: "dismissed"}); e != nil {
			return e
		}
	} else {
		payload, err := asyncQuestionPayload(params, answer)
		if err != nil {
			return err
		}
		fingerprint := digest(payload)
		if old.Fingerprint != "" && old.Fingerprint != fingerprint {
			return errors.New("提问已过期或已在其他设备处理，请刷新")
		}
		if old.State == "dismissed" {
			return errors.New("提问已过期或已在其他设备处理，请刷新")
		}
		if old.State != "accepted" {
			bridge, ok := m.Desktop.(interface {
				AnswerAsyncQuestions(context.Context, string, string, string, string, bool) error
			})
			if !ok {
				return errors.New("当前电脑 Codex 接口不支持提问回答")
			}
			confirmOnly := old.State == "dispatching" || old.State == "unknown"
			if !confirmOnly {
				if e = m.Store.Put("agentconfig", recordKey, asyncAnswerRecord{State: "dispatching", Fingerprint: fingerprint}); e != nil {
					return e
				}
			}
			if e = bridge.AnswerAsyncQuestions(m.Context, ss.ID, ss.Project, key, payload, confirmOnly); e != nil {
				var notSent *InputNotSentError
				if !confirmOnly && errors.As(e, &notSent) {
					_ = m.Store.Delete("agentconfig", recordKey)
				} else {
					_ = m.Store.Put("agentconfig", recordKey, asyncAnswerRecord{State: "unknown", Fingerprint: fingerprint})
				}
				return e
			}
			if e = m.Store.Put("agentconfig", recordKey, asyncAnswerRecord{State: "accepted", Fingerprint: fingerprint}); e != nil {
				return e
			}
		}
		request := asyncAnswerRequestID(key, payload)
		if _, _, e = m.Store.AppendDesktop(store.Event{ID: "desktop-async-answer/" + request, Session: ss.ID, Agent: "codex", Project: ss.Project, Source: "desktop-live", Kind: "message", Role: "user", Item: asyncReplyItemID(payload) + ":0", Text: Redact(asyncReplyDisplay(payload)), Mode: "snapshot"}, false); e != nil {
			return e
		}
		m.keepDesktopSend(ss.ID)
	}
	_, _, e = m.Store.Append(store.Event{ID: "desktop-async-resolved/" + UUID(), Session: ss.ID, Agent: "codex", Project: ss.Project, Source: "desktop-live", Kind: "approval", Item: key, Text: previous.Text, Status: "resolved", Mode: "snapshot"})
	return e
}
