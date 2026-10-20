package agent

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Explicit opt-in: one disposable real-provider chat, never a user's work chat.
func TestRealOpenCodeQuestionRoundTrip(t *testing.T) {
	if os.Getenv("AGENTDECK_REAL_QUESTIONS") != "1" {
		t.Skip("explicit real-provider question acceptance only")
	}
	cfg, err := LoadConnections(filepath.Join(os.Getenv("HOME"), ".agentdeck", "agents.json"))
	if err != nil {
		t.Fatal("connection configuration unavailable")
	}
	m := connectorManager(t)
	o := newOpenCode(m, cfg.OpenCode)
	m.Connectors = map[string]Connector{"opencode": o}
	go o.Run(m.Context)
	ss, err := m.Create("opencode", m.Projects()[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if e := o.request(ctx, "DELETE", "/session/"+url.PathEscape(nativeID(ss)), ss.Project, nil, nil); e != nil {
			t.Error("owned acceptance chat cleanup failed")
		}
	}()
	if err = m.Send(ss.ID, "This is a disposable AgentDeck question-interface acceptance chat. Do not read or write files and do not use shell, network, or other tools. Use only the question tool to ask one question with options Alpha and Beta and descriptions. Wait for my answer. After receiving the answer, reply only AD_QUESTION_NATIVE_DONE.", UUID()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(90 * time.Second)
	answered := false
	for time.Now().Before(deadline) {
		history, e := m.Store.History(ss.ID, 0, 40)
		if e != nil {
			t.Fatal(e)
		}
		for _, ev := range history.Events {
			if !answered && ev.Kind == "approval" && ev.Status == "pending" {
				var params map[string]any
				if json.Unmarshal([]byte(ev.Text), &params) != nil {
					continue
				}
				if params["question_kind"] != "opencode" {
					continue
				}
				answers := questionAnswers{}
				for i, raw := range questionList(params) {
					q := obj(raw)
					options := arr(q["options"])
					if len(options) == 0 {
						t.Fatal("native agent did not offer choices")
					}
					answers[questionID(q, i)] = []string{str(obj(options[0])["label"])}
				}
				body, _ := json.Marshal(answers)
				if e = m.Approve(ss.ID, ev.Item, "accept", string(body)); e != nil {
					t.Fatal(e)
				}
				answered = true
			}
			if answered && ev.Kind == "message" && ev.Role == "assistant" && ev.Text == "AD_QUESTION_NATIVE_DONE" {
				t.Log("real OpenCode native question, original-session answer and completion passed")
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("real OpenCode question round trip not confirmed before deadline")
}
