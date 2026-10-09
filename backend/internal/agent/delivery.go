package agent

import (
	"agentdeck/internal/store"
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

type queuedInput struct {
	NativeRemoved map[string]any
	Text          string
	Options       TurnOptions
	Files         []InputAttachment
}

func (m *Manager) SendDelivery(id, text, request string, o TurnOptions, files []InputAttachment, mode string) error {
	if mode == "" || mode == "send" {
		return m.SendInput(id, text, request, o, files)
	}
	if mode != "queue" && mode != "steer" {
		return errors.New("无效的消息发送方式")
	}
	ss, e := m.sessionForInput(id)
	if e != nil {
		return e
	}
	if ss.Agent != "codex" && ss.Agent != "claude" {
		return errors.New("此 Agent 使用自身的发送方式")
	}
	if len(text) > 64<<10 || strings.TrimSpace(text) == "" && len(files) == 0 {
		return errors.New("消息为空或超过 64 KiB")
	}
	if mode == "steer" {
		return m.steerInput(ss, text, request, o, files)
	}
	if e = m.validateOptions(id, ss.Agent, o); e != nil {
		return e
	}
	if ss.Source == "desktop" {
		if native, ok := m.Desktop.(nativeQueueController); ok {
			lock := m.deliveryLock(id)
			lock.Lock()
			defer lock.Unlock()
			inputs, _, err := m.prepareInput(id, text, request, files)
			if err != nil {
				return err
			}
			if err = native.EnqueueNative(m.Context, id, text, request, ss.Project, o, inputs); err != nil {
				return err
			}
			m.keepDesktopSend(id)
			return nil
		}
	}
	d := store.Delivery{ID: request, Session: id, State: "queued", Created: time.Now().UTC().Format(time.RFC3339Nano)}
	if e = m.Store.EnqueueDelivery(d, queuedInput{Text: text, Options: o, Files: files}); e != nil {
		return e
	}
	if e = m.deliveryEvent(d, text); e != nil {
		return e
	}
	if ss.Source == "desktop" {
		m.keepDesktopSend(id)
	}
	return nil
}
func (m *Manager) deliveryEvent(d store.Delivery, text string) error {
	if text == "" {
		var q queuedInput
		if m.Store.Get("deliverypayloads", d.ID, &q) == nil {
			text = q.Text
		}
	}
	var ss store.Session
	if m.Store.Get("sessions", d.Session, &ss) != nil {
		return errors.New("会话不存在")
	}
	_, _, err := m.Store.Append(store.Event{ID: "queue-state/" + d.ID + "/" + d.State, Session: d.Session, Agent: ss.Agent, Project: ss.Project, Kind: "queued_message", Role: d.State, Item: "queue/" + d.ID, Mode: "snapshot", Text: Redact(text), Source: "delivery"})
	return err
}
func (m *Manager) CancelDelivery(id, request string) error {
	if _, e := m.sessionForInput(id); e != nil {
		return e
	}
	var record store.Delivery
	if m.Store.Get("deliveries", request, &record) != nil || record.Session != id {
		return errors.New("待发消息已不可用")
	}
	if record.State != "queued" && record.State != "unknown" {
		return errors.New("消息已提交，请先检查会话")
	}
	var q queuedInput
	if e := m.Store.Get("deliverypayloads", request, &q); e != nil {
		return errors.New("待发消息已不可用")
	}
	changed, e := m.Store.TransitionDelivery(request, id, record.State, "cancelled")
	if e != nil {
		return e
	}
	if !changed {
		return errors.New("消息已提交，请先检查会话")
	}
	m.deliveryEvent(store.Delivery{ID: request, Session: id, State: "cancelled"}, q.Text)
	return nil
}
func (m *Manager) RunDeliveries(ctx context.Context) {
	m.deliveryMu.Lock()
	defer m.deliveryMu.Unlock()
	if m.deliveryCancel != nil {
		return
	}
	stale, _ := m.Store.Deliveries("dispatching")
	for _, d := range stale {
		if changed, _ := m.Store.TransitionDelivery(d.ID, d.Session, "dispatching", "unknown"); changed {
			d.State = "unknown"
			m.deliveryEvent(d, "")
		}
	}
	life, cancel := context.WithCancel(ctx)
	m.deliveryCancel = cancel
	m.deliveryWG.Add(1)
	go func() {
		defer m.deliveryWG.Done()
		leases := map[string]func(){}
		defer func() {
			for _, release := range leases {
				release()
			}
		}()
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-life.Done():
				return
			case <-t.C:
				list, _ := m.Store.Deliveries()
				active := map[string]bool{}
				for _, d := range list {
					active[d.Session] = true
					if leases[d.Session] == nil {
						leases[d.Session] = m.WatchDesktop(life, d.Session)
					}
				}
				for id, release := range leases {
					if !active[id] {
						release()
						delete(leases, id)
					}
				}
				m.dispatchDeliveries(life)
			}
		}
	}()
}
func (m *Manager) closeDeliveries() {
	m.deliveryMu.Lock()
	if m.deliveryCancel != nil {
		m.deliveryCancel()
	}
	m.deliveryMu.Unlock()
	m.deliveryWG.Wait()
}
func (m *Manager) dispatchDeliveries(ctx context.Context) {
	list, e := m.Store.Deliveries()
	if e != nil {
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Created < list[j].Created })
	seen := map[string]bool{}
	unconfirmed, _ := m.Store.Deliveries("unknown", "dispatching")
	for _, d := range unconfirmed {
		seen[d.Session] = true
	}
	for _, d := range list {
		if ctx.Err() != nil {
			return
		}
		if seen[d.Session] {
			continue
		}
		seen[d.Session] = true
		m.dispatchDelivery(ctx, d)
	}
}
func (m *Manager) deliveryLock(id string) *sync.Mutex {
	m.deliveryMu.Lock()
	defer m.deliveryMu.Unlock()
	if m.deliveryLocks == nil {
		m.deliveryLocks = map[string]*sync.Mutex{}
	}
	if m.deliveryLocks[id] == nil {
		m.deliveryLocks[id] = &sync.Mutex{}
	}
	return m.deliveryLocks[id]
}
func (m *Manager) dispatchDelivery(ctx context.Context, d store.Delivery) {
	lock := m.deliveryLock(d.Session)
	lock.Lock()
	defer lock.Unlock()
	if ctx.Err() != nil {
		return
	}
	var ss store.Session
	if m.Store.Get("sessions", d.Session, &ss) != nil {
		return
	}
	if ss.Status == "running" || ss.Status == "pending" {
		return
	}
	if when, e := time.Parse(time.RFC3339Nano, d.Created); e == nil && time.Since(when) < time.Second {
		return
	}
	if ss.Source == "desktop" {
		if native, ok := m.Desktop.(interface {
			CurrentStatus(context.Context, string, string) (string, error)
		}); ok {
			state, e := native.CurrentStatus(ctx, ss.ID, ss.Project)
			if e != nil || state == "" || state == "running" {
				return
			}
		} else {
			return
		}
	}
	var q queuedInput
	if m.Store.Get("deliverypayloads", d.ID, &q) != nil {
		return
	}
	changed, e := m.Store.TransitionDelivery(d.ID, d.Session, "queued", "dispatching")
	if e != nil || !changed {
		return
	}
	// Never automatically retry after dispatch: an acknowledgement can be lost.
	e = m.SendInput(d.Session, q.Text, d.ID, q.Options, q.Files)
	next := "sent"
	if e != nil {
		next = "unknown"
	}
	if changed, _ = m.Store.TransitionDelivery(d.ID, d.Session, "dispatching", next); changed {
		d.State = next
		m.deliveryEvent(d, q.Text)
	}
}

func (m *Manager) steerInput(ss store.Session, text, request string, o TurnOptions, files []InputAttachment) error {
	lock := m.deliveryLock(ss.ID)
	lock.Lock()
	defer lock.Unlock()
	if ss.Source == "desktop" {
		sender, ok := m.Desktop.(interface {
			SteerInput(context.Context, string, string, string, string, []any) error
		})
		if !ok {
			return errors.New("桌面引导接口不可用")
		}
		if _, e := m.allowed(ss.Project); e != nil {
			return e
		}
		inputs, display, e := m.prepareInput(ss.ID, text, request, files)
		if e != nil {
			return e
		}
		if e = sender.SteerInput(m.Context, ss.ID, text, request, ss.Project, inputs); e != nil {
			return e
		}
		_, _, e = m.Store.AppendDesktop(store.Event{ID: "desktop-input/" + request, Session: ss.ID, Agent: ss.Agent, Project: ss.Project, Kind: "message", Role: "user", Item: request + ":0", Mode: "snapshot", Text: Redact(display), Source: "desktop-live"}, false)
		if e == nil {
			m.keepDesktopSend(ss.ID)
		}
		return e
	}
	r, e := m.get(ss.ID)
	if e != nil {
		return e
	}
	r.mu.Lock()
	busy, turn := r.busy, r.turn
	r.mu.Unlock()
	if !busy {
		return errors.New("没有正在执行的任务，请正常发送")
	}
	if ss.Agent == "claude" {
		if _, e = r.call("interrupt", map[string]any{"subtype": "interrupt"}); e != nil {
			return e
		}
		deadline := time.NewTimer(8 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(30 * time.Millisecond)
		defer tick.Stop()
		for {
			r.mu.Lock()
			active := r.busy
			r.mu.Unlock()
			if !active {
				break
			}
			select {
			case <-deadline.C:
				return errors.New("尚未确认中断，消息未发送")
			case <-tick.C:
			}
		}
		return m.SendInput(ss.ID, text, request, o, files)
	}
	inputs, _, e := m.prepareInput(ss.ID, text, request, files)
	if e != nil {
		return e
	}
	_, e = r.call("turn/steer", map[string]any{"threadId": ss.ID, "expectedTurnId": turn, "input": inputs})
	return e
}
