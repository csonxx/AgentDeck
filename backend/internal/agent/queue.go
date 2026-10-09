package agent

import (
	"agentdeck/internal/store"
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

type QueuedMessage struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	State       string `json:"state"`
	Origin      string `json:"origin"`
	Version     string `json:"version"`
	Attachments int    `json:"attachments"`
}
type nativeQueueController interface {
	NativeQueue(string) ([]QueuedMessage, error)
	EnqueueNative(context.Context, string, string, string, string, TurnOptions, []any) error
	ChangeNativeQueue(context.Context, string, string, string, string, string, string, string) error
}

func (m *Manager) Queue(id string) ([]QueuedMessage, error) {
	ss, e := m.sessionForInput(id)
	if e != nil {
		return nil, e
	}
	out := []QueuedMessage{}
	if ss.Agent == "codex" && m.Desktop != nil && (!ss.Managed || ss.Source == "desktop") {
		if native, ok := m.Desktop.(nativeQueueController); ok {
			out, e = native.NativeQueue(id)
			if e != nil {
				return nil, e
			}
		}
	}
	list, e := m.Store.Deliveries("queued", "dispatching", "unknown")
	if e != nil {
		return nil, e
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Created < list[j].Created })
	for _, d := range list {
		if d.Session != id {
			continue
		}
		var q queuedInput
		if m.Store.Get("deliverypayloads", d.ID, &q) != nil {
			continue
		}
		origin := "local"
		if q.NativeRemoved != nil {
			origin = "codex"
		}
		out = append(out, QueuedMessage{ID: d.ID, Text: Redact(q.Text), State: d.State, Origin: origin, Version: d.Hash, Attachments: len(q.Files)})
	}
	return out, nil
}
func (m *Manager) ChangeQueue(id, message, version, action, text, request string) error {
	ss, e := m.sessionForInput(id)
	if e != nil {
		return e
	}
	if action != "edit" && action != "delete" && action != "steer" {
		return errors.New("无效的消息发送方式")
	}
	if action == "edit" && (len(text) > 64<<10 || strings.TrimSpace(text) == "") {
		return errors.New("消息为空或超过 64 KiB")
	}
	// Legacy AgentDeck records keep their own identity when upgrading a server.
	var d store.Delivery
	if m.Store.Get("deliveries", message, &d) != nil || d.Session != id {
		if ss.Agent == "codex" && m.Desktop != nil && (!ss.Managed || ss.Source == "desktop") {
			if native, ok := m.Desktop.(nativeQueueController); ok {
				lock := m.deliveryLock(id)
				lock.Lock()
				defer lock.Unlock()
				err := native.ChangeNativeQueue(m.Context, id, ss.Project, message, version, action, text, request)
				var unknown *nativeSteerUnknown
				if errors.As(err, &unknown) {
					backup := queuedInput{Text: str(unknown.removed["text"]), NativeRemoved: unknown.removed}
					_ = m.Store.EnqueueDelivery(store.Delivery{ID: message, Session: id, State: "unknown", Created: time.Now().UTC().Format(time.RFC3339Nano)}, backup)
				}
				return err
			}
		}
		return errors.New("待发消息已不可用")
	}
	lock := m.deliveryLock(id)
	lock.Lock()
	if m.Store.Get("deliveries", message, &d) != nil || d.Session != id || version == "" || d.Hash != version {
		lock.Unlock()
		return errors.New("电脑队列已变化，请刷新后重试")
	}
	var q queuedInput
	if m.Store.Get("deliverypayloads", message, &q) != nil {
		lock.Unlock()
		return errors.New("待发消息已不可用")
	}
	if action == "delete" {
		e = m.CancelDelivery(id, message)
		lock.Unlock()
		return e
	}
	if d.State != "queued" {
		lock.Unlock()
		return errors.New("消息已提交，请先检查会话")
	}
	if action == "edit" {
		q.Text = text
		e = m.Store.EditDelivery(message, id, version, q)
		lock.Unlock()
		if e == nil {
			d.State = "queued"
			e = m.deliveryEvent(d, text)
		}
		return e
	}
	changed, e := m.Store.TransitionDelivery(message, id, "queued", "dispatching")
	lock.Unlock()
	if e != nil {
		return e
	}
	if !changed {
		return errors.New("消息已提交，请先检查会话")
	}
	e = m.steerInput(ss, q.Text, request, q.Options, q.Files)
	state := "sent"
	if e != nil {
		state = "unknown"
	}
	if ok, _ := m.Store.TransitionDelivery(message, id, "dispatching", state); ok {
		d.State = state
		_ = m.deliveryEvent(d, q.Text)
	}
	return e
}
