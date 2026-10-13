package agent

import (
	"agentdeck/internal/store"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeAppBusyOriginalQueuesThenHandsOffSameID(t *testing.T) {
	m, ss, record, _, _ := inboxFixture(t)
	m.ClaudeDesktopInput = false
	m.Context = context.Background()
	defer m.Close()
	bin, history := t.TempDir(), t.TempDir()
	fixture, err := os.ReadFile("../../../scripts/fixture_runtime.py")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(bin, "claude"), fixture, 0700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AGENTDECK_FIXTURE_SESSION_DIR", history)
	os.WriteFile(filepath.Join(history, ss.ID), []byte("original history"), 0600)
	update := func(status string) {
		b, _ := json.Marshal(claudeProcessRecord{PID: 1234, Session: ss.ID, Start: "fixture birth", Status: status, At: time.Now().UnixMilli()})
		os.WriteFile(record, b, 0600)
	}
	update("busy")
	released := 0
	m.claudeRelease = func(owner claudeProcessRecord) error {
		if owner.PID != 1234 || owner.Session != ss.ID || owner.Status != "idle" {
			t.Fatal("wrong executor signaled")
		}
		released++
		return os.Remove(record)
	}
	selected, err := m.Create("claude", digest(ss.Project)[:24], ss.ID)
	if err != nil || selected.ID != ss.ID || selected.Parent != "" || released != 0 {
		t.Fatal("selection changed identity or interrupted", err)
	}
	request := UUID()
	if err = m.SendDelivery(ss.ID, "fixture app input", request, TurnOptions{}, nil, "send"); err != nil {
		t.Fatal(err)
	}
	ack, ok := m.NativeSubmission(ss.ID, request).(map[string]string)
	if !ok || ack["delivery"] != "queued" {
		t.Fatal("queued receipt missing")
	}
	var d store.Delivery
	m.Store.Get("deliveries", request, &d)
	d.Created = time.Now().Add(-2 * time.Second).UTC().Format(time.RFC3339Nano)
	m.Store.Put("deliveries", request, d)
	m.dispatchDeliveries(m.Context)
	if released != 0 || len(m.runs) != 0 {
		t.Fatal("busy executor was interrupted")
	}
	update("waiting")
	m.dispatchDeliveries(m.Context)
	if released != 0 {
		t.Fatal("pending approval was interrupted")
	}
	update("idle")
	m.dispatchDeliveries(m.Context)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var current store.Session
		m.Store.Get("sessions", ss.ID, &current)
		if current.Status == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.Store.Get("sessions", ss.ID, &selected)
	if selected.Status != "completed" || released != 1 {
		t.Fatal("queued original did not execute")
	}
	list, _ := m.Store.Sessions()
	if len(list) != 1 || list[0].ID != ss.ID || list[0].Parent != "" {
		t.Fatal("mobile branch created")
	}
	r := m.runs[ss.ID]
	args := strings.Join(r.cmd.Args, " ")
	if !strings.Contains(args, "--resume="+ss.ID) || strings.Contains(args, "--fork-session") {
		t.Fatal("native original was not resumed")
	}
	q, _ := m.Queue(ss.ID)
	if len(q) != 0 {
		t.Fatal("completed input remained queued")
	}
}

func TestClaudeHandoffRechecksIdleAndNeverSignalsParkedOwner(t *testing.T) {
	for _, changed := range []string{"busy", "parked", "different-birth"} {
		t.Run(changed, func(t *testing.T) {
			m, ss, record, _, _ := inboxFixture(t)
			m.ClaudeDesktopInput = false
			calls := 0
			m.claudeRelease = func(claudeProcessRecord) error { t.Fatal("unsafe signal"); return nil }
			m.ClaudeProcessSnapshot = func() (map[int]string, error) {
				calls++
				if calls == 2 {
					b, _ := os.ReadFile(record)
					var d map[string]any
					json.Unmarshal(b, &d)
					if changed == "parked" {
						d["parkedJobId"] = "job"
					} else if changed == "busy" {
						d["status"] = "busy"
					} else {
						d["procStart"] = "different-birth"
					}
					b, _ = json.Marshal(d)
					os.WriteFile(record, b, 0600)
				}
				return map[int]string{1234: "fixture birth"}, nil
			}
			_ = m.acquireClaude(ss.ID)
		})
	}
}
