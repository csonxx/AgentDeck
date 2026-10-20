package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"agentdeck/internal/store"
)

func desktopQuestionKey(id any) string { return desktopQuestionRequestKey(map[string]any{"id": id}) }
func desktopQuestionRequestKey(request map[string]any) string {
	params := obj(request["params"])
	return "desktop-question/" + digest(jsonText([]any{request["id"], params["turnId"], params["itemId"]}))
}
func desktopQuestions(state map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, raw := range arr(state["requests"]) {
		req := obj(raw)
		if req["method"] == "item/tool/requestUserInput" || req["method"] == "tool/requestUserInput" {
			out = append(out, req)
		}
	}
	return out
}
func (p *desktopProjection) publishQuestions(state map[string]any) error {
	// Missing request state is an incompatible snapshot, not evidence of resolution.
	if _, ok := state["requests"].([]any); !ok {
		return nil
	}
	stateKey := "codex-native-questions/" + p.session.ID
	if p.questions == nil {
		p.questions = map[string]string{}
		_ = p.m.Store.Get("agentconfig", stateKey, &p.questions)
		if p.questions == nil {
			p.questions = map[string]string{}
		}
	}
	active := map[string]string{}
	for _, request := range desktopQuestions(state) {
		params := obj(request["params"])
		if params["threadId"] != p.session.ID || request["id"] == nil || len(questionList(params)) == 0 {
			continue
		}
		key := desktopQuestionRequestKey(request)
		presentation := questionPresentation(params, "codex")
		presentation["source_item"] = str(params["itemId"])
		text := Redact(jsonText(presentation))
		position := p.questionPosition(state, str(params["itemId"]))
		active[key] = text
		if p.questions[key] == text {
			previous, e := p.m.Store.TranscriptItem(p.session.ID, key, "approval")
			if e == nil && previous.Status == "pending" && previous.Text == text && previous.Position == position {
				continue
			}
		}
		if err := p.append(store.Event{Session: p.session.ID, Agent: "codex", Project: p.session.Project, Source: "desktop-live", Kind: "approval", Item: key, Text: text, Position: position, Mode: "snapshot", Status: "pending"}); err != nil {
			return err
		}
	}
	for key, text := range p.questions {
		if _, ok := active[key]; ok {
			continue
		}
		if err := p.append(store.Event{Session: p.session.ID, Agent: "codex", Project: p.session.Project, Source: "desktop-live", Kind: "approval", Item: key, Text: text, Mode: "snapshot", Status: "resolved"}); err != nil {
			return err
		}
	}
	before, _ := json.Marshal(p.questions)
	after, _ := json.Marshal(active)
	if string(before) != string(after) {
		if err := p.m.Store.Put("agentconfig", stateKey, active); err != nil {
			return err
		}
	}
	p.questions = active
	return nil
}

// No turn start/resume, replacement CLI, or arbitrary public RPC. Read the owner's
// fresh request list on this connection, then reply through its native follower API.
func (b *DesktopBridge) AnswerQuestions(ctx context.Context, id, project, key, decision, answer string) error {
	ctx, cancel := context.WithTimeout(nonNil(ctx), 12*time.Second)
	defer cancel()
	c, err := b.connect(ctx, id)
	if err != nil {
		return err
	}
	defer c.conn.Close()
	stop := context.AfterFunc(ctx, func() { c.conn.Close() })
	defer stop()
	if err = c.following(id, true); err != nil {
		return err
	}
	defer func() { c.conn.SetWriteDeadline(time.Now().Add(time.Second)); _ = c.following(id, false) }()
	var request map[string]any
	for frames := 0; frames < 1000; frames++ {
		frame, e := readDesktopFrame(c.conn)
		if e != nil {
			return errors.New("电脑 Codex 提问状态读取失败，请刷新")
		}
		params := obj(frame["params"])
		if frame["type"] != "broadcast" || frame["method"] != "thread-stream-state-changed" || frame["sourceClientId"] != c.owner || frame["version"] != float64(11) || params["conversationId"] != id || params["hostId"] != "local" {
			continue
		}
		change := obj(params["change"])
		if change["type"] != "snapshot" {
			continue
		}
		state := obj(change["conversationState"])
		if state["id"] != id || state["cwd"] != project || (str(state["hostId"]) != "" && state["hostId"] != "local") {
			return errors.New("电脑 Codex 提问会话或项目不匹配")
		}
		for _, req := range desktopQuestions(state) {
			if req["id"] != nil && desktopQuestionRequestKey(req) == key && obj(req["params"])["threadId"] == id {
				request = req
				break
			}
		}
		break
	}
	if request == nil {
		return errors.New("提问已过期或已在其他设备处理，请刷新")
	}
	answers := questionAnswers{}
	if decision == "accept" {
		answers, err = parseQuestionAnswers(answer)
		if err != nil {
			return err
		}
		if err = validateQuestionAnswers(questionList(obj(request["params"])), answers, "codex"); err != nil {
			return err
		}
	}
	response, err := c.request("thread-follower-submit-user-input", 1, map[string]any{"conversationId": id, "requestId": request["id"], "response": codexQuestionResponse(answers)}, c.owner)
	if err != nil {
		return err
	}
	result := obj(response["result"])
	if result["method"] != "thread-follower-submit-user-input" || obj(result["result"])["ok"] != true {
		return errors.New("电脑 Codex 尚未确认回答，请刷新核对，勿重复提交")
	}
	return nil
}
