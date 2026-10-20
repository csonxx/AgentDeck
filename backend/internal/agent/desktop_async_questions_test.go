package agent

import (
	"agentdeck/internal/store"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func asyncFixtureState(id, project, status string) map[string]any {
	return map[string]any{"id": id, "cwd": project, "requests": []any{}, "turns": []any{map[string]any{"turnId": "turn-1", "status": status, "items": []any{map[string]any{"id": "ask-1", "type": "agentMessage", "text": "Please choose", "questions": []any{map[string]any{"title": "Choose agent", "options": []any{"A", "B"}}, map[string]any{"title": "Choose project", "options": []any{}}}}}}}}
}
func asyncFixtureCard(t *testing.T, state map[string]any) (asyncQuestionCard, map[string]any, string) {
	t.Helper()
	cards, _ := nativeAsyncCards(state)
	if len(cards) != 1 {
		t.Fatal("missing async native card")
	}
	card := cards[0]
	params := map[string]any{"question_kind": "codex_async", "source_item": card.Source, "questions": card.Questions}
	answer := jsonText(map[string]string{asyncQuestionID("ask-1", 0): "B", asyncQuestionID("ask-1", 1): "custom project"})
	payload, e := asyncQuestionPayload(params, answer)
	if e != nil {
		t.Fatal(e)
	}
	return card, params, payload
}
func addAsyncReply(state map[string]any, payload, status string) {
	turn := obj(desktopTurns(state)[0])
	turn["items"] = append(arr(turn["items"]), map[string]any{"id": "answer-1", "type": "steeringUserMessage", "status": status, "input": []any{map[string]any{"type": "text", "text": payload}}})
}
func TestDesktopAsyncQuestionProjectionAndNativeAnswerSync(t *testing.T) {
	m := connectorManager(t)
	ss := store.Session{ID: "original", Agent: "codex", Project: m.Roots[0], Source: "desktop"}
	m.Store.Put("sessions", ss.ID, ss)
	state := asyncFixtureState(ss.ID, ss.Project, "inProgress")
	card, _, payload := asyncFixtureCard(t, state)
	if asyncQuestionID("ask-1", 0) != `["request_user_input_async","ask-1",0]` {
		t.Fatal("native question ID changed")
	}
	publish := func() {
		t.Helper()
		p := &desktopProjection{m: m, session: ss, seen: map[string]string{}}
		if e := p.publish(state); e != nil {
			t.Fatal(e)
		}
	}
	publish()
	previous := m.Store.LastSeq()
	publish()
	if m.Store.LastSeq() != previous {
		t.Fatal("reconnect duplicated async card")
	}
	ev, e := m.Store.TranscriptItem(ss.ID, card.Key, "approval")
	if e != nil || ev.Status != "pending" || !strings.Contains(ev.Text, "Choose project") {
		t.Fatal("async choices discarded", e)
	}
	saved := state["turns"]
	state["turns"] = []any{}
	publish()
	ev, _ = m.Store.TranscriptItem(ss.ID, card.Key, "approval")
	if ev.Status != "pending" {
		t.Fatal("partial history falsely resolved card")
	}
	state["turns"] = saved
	addAsyncReply(state, payload, "pending")
	publish()
	ev, _ = m.Store.TranscriptItem(ss.ID, card.Key, "approval")
	if ev.Status != "pending" {
		t.Fatal("unaccepted steer resolved card")
	}
	items := arr(obj(desktopTurns(state)[0])["items"])
	obj(items[len(items)-1])["status"] = "accepted"
	publish()
	ev, _ = m.Store.TranscriptItem(ss.ID, card.Key, "approval")
	if ev.Status != "resolved" {
		t.Fatal("desktop accepted answer did not resolve App card")
	}
}

type asyncAnswerFixture struct {
	desktopFixture
	calls     int
	confirms  []bool
	payload   string
	fail      bool
	confirmed bool
}

func (b *asyncAnswerFixture) AnswerAsyncQuestions(_ context.Context, _, _, _, payload string, confirm bool) error {
	b.calls++
	b.confirms = append(b.confirms, confirm)
	b.payload = payload
	if b.fail {
		return errors.New("fixture ACK lost")
	}
	if confirm && !b.confirmed {
		return errors.New("fixture still unknown")
	}
	return nil
}
func TestDesktopAsyncAnswerDedupUnknownAckAndSkip(t *testing.T) {
	for _, scenario := range []string{"normal", "unknown", "skip"} {
		t.Run(scenario, func(t *testing.T) {
			m := connectorManager(t)
			ss := store.Session{ID: "original", Agent: "codex", Project: m.Roots[0], Source: "desktop"}
			m.Store.Put("sessions", ss.ID, ss)
			state := asyncFixtureState(ss.ID, ss.Project, "completed")
			card, _, payload := asyncFixtureCard(t, state)
			p := &desktopProjection{m: m, session: ss, seen: map[string]string{}}
			if e := p.publish(state); e != nil {
				t.Fatal(e)
			}
			bridge := &asyncAnswerFixture{}
			m.Desktop = bridge
			answer := jsonText(map[string]string{asyncQuestionID("ask-1", 0): "B", asyncQuestionID("ask-1", 1): "custom project"})
			if scenario == "skip" {
				if e := m.Approve(ss.ID, card.Key, "decline", "{}"); e != nil {
					t.Fatal(e)
				}
				if bridge.calls != 0 {
					t.Fatal("skip sent model input")
				}
			} else {
				if scenario == "unknown" {
					bridge.fail = true
					if e := m.Approve(ss.ID, card.Key, "accept", answer); e == nil {
						t.Fatal("lost ACK pretended success")
					}
					bridge.fail = false
					if e := m.Approve(ss.ID, card.Key, "accept", answer); e == nil {
						t.Fatal("unknown answer was resent")
					}
					if !bridge.confirms[1] {
						t.Fatal("unknown ACK retry allowed a second send")
					}
					bridge.confirmed = true
				}
				if e := m.Approve(ss.ID, card.Key, "accept", answer); e != nil {
					t.Fatal(e)
				}
				before := bridge.calls
				if e := m.Approve(ss.ID, card.Key, "accept", answer); e != nil || bridge.calls != before {
					t.Fatal("duplicate repeated native send", e)
				}
				if bridge.payload != payload {
					t.Fatal("native reply ID/title/answers changed")
				}
				h, _ := m.Store.History(ss.ID, 0, 40)
				count := 0
				for _, ev := range h.Events {
					if ev.Kind == "message" && ev.Role == "user" {
						count++
						if strings.Contains(ev.Text, asyncReplyStart) || !strings.Contains(ev.Text, "custom project") {
							t.Fatal("raw protocol marker displayed")
						}
					}
				}
				if count != 1 {
					t.Fatal("answer duplicated in transcript", count)
				}
			}
			p = &desktopProjection{m: m, session: ss, seen: map[string]string{}}
			p.publish(state)
			ev, _ := m.Store.TranscriptItem(ss.ID, card.Key, "approval")
			if ev.Status != "resolved" {
				t.Fatal("stale native snapshot revived handled card")
			}
			if m.liveRuntime(ss.ID) != nil {
				t.Fatal("answer acquired a competing CLI writer")
			}
		})
	}
}
func TestDesktopAsyncReplyNativeWire(t *testing.T) {
	for _, scenario := range []string{"running", "completed", "foreign", "expired", "confirm-pending", "confirm-accepted"} {
		t.Run(scenario, func(t *testing.T) {
			dir, e := os.MkdirTemp("/tmp", "ad-async-")
			if e != nil {
				t.Fatal(e)
			}
			defer os.RemoveAll(dir)
			os.Chmod(dir, 0700)
			path := filepath.Join(dir, "ipc.sock")
			ln, e := net.Listen("unix", path)
			if e != nil {
				t.Fatal(e)
			}
			defer ln.Close()
			os.Chmod(path, 0600)
			status := "inProgress"
			if scenario == "completed" {
				status = "completed"
			}
			state := asyncFixtureState("original", "/authorized", status)
			card, _, payload := asyncFixtureCard(t, state)
			if scenario == "confirm-accepted" {
				addAsyncReply(state, payload, "accepted")
			}
			project, key := "/authorized", card.Key
			if scenario == "foreign" {
				project = "/foreign"
			}
			if scenario == "expired" {
				key += "stale"
			}
			mutate := scenario == "running" || scenario == "completed"
			done := make(chan error, 1)
			go func() {
				c, err := ln.Accept()
				if err != nil {
					done <- err
					return
				}
				defer c.Close()
				c.SetDeadline(time.Now().Add(4 * time.Second))
				for step := 0; step < 2; step++ {
					v, err := readDesktopFrame(c)
					if err != nil {
						done <- err
						return
					}
					result := map[string]any{}
					if step == 0 {
						result["clientId"] = "phone"
					}
					writeDesktopTest(c, map[string]any{"type": "response", "requestId": v["requestId"], "resultType": "success", "handledByClientId": "owner", "result": result})
				}
				if _, err = readDesktopFrame(c); err != nil {
					done <- err
					return
				}
				writeDesktopTest(c, map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"hostId": "local", "conversationId": "original", "change": map[string]any{"type": "snapshot", "conversationState": state}}})
				if _, err = readDesktopFrame(c); err != nil {
					done <- err
					return
				} // unsubscribe
				v, err := readDesktopFrame(c)
				if !mutate {
					if err == nil {
						t.Error("read-only or stale answer mutated native state")
					}
					done <- nil
					return
				}
				if err != nil {
					done <- err
					return
				}
				params := obj(v["params"])
				input := arr(params["input"])
				if scenario == "running" {
					if v["method"] != "thread-follower-steer-turn" || v["version"] != float64(1) {
						t.Error("running answer did not steer original")
					}
				} else {
					if v["method"] != "thread-follower-start-turn" || v["version"] != float64(2) {
						t.Error("completed answer did not continue original")
					}
					turn := obj(params["turnStart"])
					req := obj(turn["request"])
					input = arr(req["input"])
					if req["cwd"] != "/authorized" || req["threadId"] != "original" || obj(turn["context"])["inheritThreadSettings"] != true || req["sandboxPolicy"] != nil || req["approvalPolicy"] != nil {
						t.Error("answer changed native identity/permissions")
					}
				}
				if v["targetClientId"] != "owner" || params["conversationId"] != "original" || len(input) != 1 || obj(input[0])["text"] != payload {
					t.Error("answer lost original owner or structured question payload")
				}
				if _, ok := obj(input[0])["text_elements"].([]any); !ok {
					t.Error("native renderer input array missing")
				}
				writeDesktopTest(c, map[string]any{"type": "response", "requestId": v["requestId"], "resultType": "success", "handledByClientId": "owner", "result": map[string]any{}})
				done <- nil
			}()
			e = (&DesktopBridge{Socket: path}).AnswerAsyncQuestions(context.Background(), "original", project, key, payload, strings.HasPrefix(scenario, "confirm-"))
			shouldFail := scenario == "foreign" || scenario == "expired" || scenario == "confirm-pending"
			if (e != nil) != shouldFail {
				t.Fatal("wrong reply outcome", e)
			}
			if e = <-done; e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestCodexAsyncReplyPresentationOnly(t *testing.T) {
	m := connectorManager(t)
	state := asyncFixtureState("original", m.Roots[0], "completed")
	_, _, payload := asyncFixtureCard(t, state)
	raw, _ := json.Marshal(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "original", "item": map[string]any{"id": "human", "type": "userMessage", "content": []any{map[string]any{"type": "text", "text": payload}}}}})
	events, e := m.Normalizer.Parse(raw, "codex", "desktop-live", &State{Session: "original", CWD: m.Roots[0]})
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, ev := range events {
		if ev.Kind == "message" && ev.Role == "user" {
			found = true
			if strings.Contains(ev.Text, asyncReplyStart) || ev.Text != "Choose agent\nB\n\nChoose project\ncustom project" {
				t.Fatal("native markers leaked into display")
			}
		}
	}
	if !found {
		t.Fatal("answer presentation lost")
	}
	if asyncReplyDisplay(asyncReplyStart+"not valid"+asyncReplyEnd) != asyncReplyStart+"not valid"+asyncReplyEnd {
		t.Fatal("arbitrary text was treated as a protocol reply")
	}
}

func TestDesktopAsyncOlderPartialAnswerAndRestart(t *testing.T) {
	m := connectorManager(t)
	ss := store.Session{ID: "original", Agent: "codex", Project: m.Roots[0], Source: "desktop"}
	m.Store.Put("sessions", ss.ID, ss)
	state := asyncFixtureState(ss.ID, ss.Project, "completed")
	old, _, payload := asyncFixtureCard(t, state)
	state["turns"] = append(arr(state["turns"]), map[string]any{"turnId": "turn-2", "status": "completed", "items": []any{}}, map[string]any{"turnId": "turn-3", "status": "inProgress", "items": []any{}})
	p := &desktopProjection{m: m, session: ss, seen: map[string]string{}}
	if e := p.publish(state); e != nil {
		t.Fatal(e)
	}
	replies := parseAsyncReplies(payload)
	partial := asyncReplyStart + "\n" + jsonText(replies[:1]) + "\n" + asyncReplyEnd
	last := obj(desktopTurns(state)[2])
	last["items"] = []any{map[string]any{"id": "reply-1", "type": "userMessage", "content": []any{map[string]any{"type": "text", "text": partial}}}}
	if e := p.publish(state); e != nil {
		t.Fatal(e)
	}
	previous, _ := m.Store.TranscriptItem(ss.ID, old.Key, "approval")
	if previous.Status != "resolved" {
		t.Fatal("obsolete two-question card remained interactive")
	}
	cards, _ := nativeAsyncCards(state)
	if len(cards) != 1 || len(cards[0].Questions) != 1 || obj(cards[0].Questions[0])["id"] != asyncQuestionID("ask-1", 1) {
		t.Fatal("partial native answer hid the remaining original question")
	}
	current, e := m.Store.TranscriptItem(ss.ID, cards[0].Key, "approval")
	if e != nil || current.Status != "pending" {
		t.Fatal("remaining question not projected", e)
	}
	if e = m.Store.RecoverManaged(); e != nil {
		t.Fatal(e)
	}
	p = &desktopProjection{m: m, session: ss, seen: map[string]string{}}
	if e = p.publish(state); e != nil {
		t.Fatal(e)
	}
	current, e = m.Store.TranscriptItem(ss.ID, cards[0].Key, "approval")
	if e != nil || current.Status != "pending" {
		t.Fatal("restart lost live async question", e)
	}
}

func TestDesktopAsyncAnswerAdmissionAndRolloutDoNotDuplicate(t *testing.T) {
	m := connectorManager(t)
	ss := store.Session{ID: "original", Agent: "codex", Project: m.Roots[0], Source: "desktop"}
	m.Store.Put("sessions", ss.ID, ss)
	state := asyncFixtureState(ss.ID, ss.Project, "inProgress")
	card, _, payload := asyncFixtureCard(t, state)
	p := &desktopProjection{m: m, session: ss, seen: map[string]string{}}
	if e := p.publish(state); e != nil {
		t.Fatal(e)
	}
	m.Desktop = &asyncAnswerFixture{}
	answer := jsonText(map[string]string{asyncQuestionID("ask-1", 0): "B", asyncQuestionID("ask-1", 1): "custom project"})
	if e := m.Approve(ss.ID, card.Key, "accept", answer); e != nil {
		t.Fatal(e)
	}
	addAsyncReply(state, payload, "accepted")
	if e := p.publish(state); e != nil {
		t.Fatal(e)
	}
	turn := obj(desktopTurns(state)[0])
	turn["items"] = append(arr(turn["items"]), map[string]any{"id": "native-admitted-id", "type": "userMessage", "content": []any{map[string]any{"type": "text", "text": payload}}})
	if e := p.publish(state); e != nil {
		t.Fatal(e)
	}
	for _, content := range []any{payload, []any{map[string]any{"type": "input_text", "text": payload}}} {
		raw, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "user", "id": "rollout-id", "content": content}})
		evs, e := m.Normalizer.Parse(raw, "codex", "log", &State{Session: ss.ID, CWD: ss.Project})
		if e != nil {
			t.Fatal(e)
		}
		for _, ev := range evs {
			if _, _, e = m.Store.AppendDesktop(ev, true); e != nil {
				t.Fatal(e)
			}
		}
	}
	h, _ := m.Store.History(ss.ID, 0, 50)
	count := 0
	for _, ev := range h.Events {
		if ev.Kind == "message" && ev.Role == "user" {
			count++
			if ev.Item != asyncReplyItemID(payload)+":0" {
				t.Fatal("reply interaction identity changed")
			}
		}
	}
	if count != 1 {
		t.Fatal("ACK/steer/admission/rollout duplicated answer", count)
	}
}

func TestDesktopAsyncWakeOnlyForExplicitAnswer(t *testing.T) {
	for _, confirmOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "send", true: "confirm-only"}[confirmOnly], func(t *testing.T) {
			dir, e := os.MkdirTemp("/tmp", "ad-async-wake-")
			if e != nil {
				t.Fatal(e)
			}
			defer os.RemoveAll(dir)
			os.Chmod(dir, 0700)
			path := filepath.Join(dir, "ipc.sock")
			ln, e := net.Listen("unix", path)
			if e != nil {
				t.Fatal(e)
			}
			os.Chmod(path, 0600)
			var ready atomic.Bool
			var opened, sends atomic.Int32
			var workers sync.WaitGroup
			done := make(chan struct{})
			state := asyncFixtureState(idForWakeTest, "/authorized", "completed")
			card, _, payload := asyncFixtureCard(t, state)
			go func() {
				defer close(done)
				for {
					c, e := ln.Accept()
					if e != nil {
						return
					}
					workers.Add(1)
					go func() {
						defer workers.Done()
						defer c.Close()
						c.SetDeadline(time.Now().Add(5 * time.Second))
						for {
							v, e := readDesktopFrame(c)
							if e != nil {
								return
							}
							result := map[string]any{"type": "response", "requestId": v["requestId"], "resultType": "success", "handledByClientId": "owner", "result": map[string]any{}}
							switch v["method"] {
							case "initialize":
								result["result"] = map[string]any{"clientId": "phone"}
							case "thread-owner-discovery":
								if !ready.Load() {
									result["resultType"] = "error"
									result["error"] = "no-client-found"
								}
							case "thread-stream-following-changed":
								if obj(v["params"])["following"] == true {
									writeDesktopTest(c, map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"hostId": "local", "conversationId": idForWakeTest, "change": map[string]any{"type": "snapshot", "conversationState": state}}})
								}
								continue
							case "thread-follower-start-turn":
								sends.Add(1)
							default:
								t.Error("unexpected native method")
							}
							if e = writeDesktopTest(c, result); e != nil {
								return
							}
						}
					}()
				}
			}()
			defer func() { ln.Close(); <-done; workers.Wait() }()
			bridge := &DesktopBridge{Socket: path, OpenThread: func(_ context.Context, id string) error {
				if id != idForWakeTest {
					t.Error("opened wrong native ID")
				}
				opened.Add(1)
				ready.Store(true)
				return nil
			}}
			e = bridge.AnswerAsyncQuestions(context.Background(), idForWakeTest, "/authorized", card.Key, payload, confirmOnly)
			if confirmOnly {
				if e == nil || opened.Load() != 0 || sends.Load() != 0 {
					t.Fatal("unknown ACK confirmation opened or resent a thread")
				}
			} else {
				if e != nil || opened.Load() != 1 || sends.Load() != 1 {
					t.Fatal("explicit original answer could not wake owner", e)
				}
			}
		})
	}
}

func TestDesktopAsyncExistingCachedAnswerKeepsItsRow(t *testing.T) {
	m := connectorManager(t)
	ss := store.Session{ID: "original", Agent: "codex", Project: m.Roots[0], Source: "desktop"}
	m.Store.Put("sessions", ss.ID, ss)
	state := asyncFixtureState(ss.ID, ss.Project, "completed")
	_, _, payload := asyncFixtureCard(t, state)
	_, _, e := m.Store.AppendDesktop(store.Event{ID: "old-native", Session: ss.ID, Agent: "codex", Project: ss.Project, Source: "desktop-live", Kind: "message", Role: "user", Item: "old-native:0", Text: payload, Mode: "snapshot"}, false)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": ss.ID, "item": map[string]any{"id": "old-native", "type": "userMessage", "content": []any{map[string]any{"type": "text", "text": payload}}}}})
	events, e := m.Normalizer.Parse(raw, "codex", "desktop-live", &State{Session: ss.ID, CWD: ss.Project})
	if e != nil {
		t.Fatal(e)
	}
	for _, event := range events {
		event.ID = "snapshot/" + event.Item
		if _, _, e = m.Store.AppendDesktop(event, false); e != nil {
			t.Fatal(e)
		}
	}
	history, _ := m.Store.History(ss.ID, 0, 40)
	if len(history.Events) != 1 || history.Events[0].Item != "old-native:0" || strings.Contains(history.Events[0].Text, asyncReplyStart) {
		t.Fatal("upgrading duplicated or retained raw cached reply", len(history.Events))
	}
}
