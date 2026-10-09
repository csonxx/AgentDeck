package agent

import (
	"agentdeck/internal/store"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func queueFixture(t *testing.T, queue []any, action string, handle func(net.Conn, map[string]any) error) (*DesktopBridge, <-chan error) {
	t.Helper()
	dir, e := os.MkdirTemp("/tmp", "ad-q-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "ipc.sock")
	l, e := net.Listen("unix", path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { l.Close() })
	os.Chmod(path, 0600)
	statePath := filepath.Join(dir, "state.json")
	b, _ := json.Marshal(map[string]any{"queued-follow-ups": map[string]any{"original": queue}, "unrelated-private-setting": "must-not-be-returned"})
	os.WriteFile(statePath, b, 0600)
	done := make(chan error, 1)
	go func() {
		c, e := l.Accept()
		if e != nil {
			done <- e
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(4 * time.Second))
		for i := 0; i < 2; i++ {
			v, e := readDesktopFrame(c)
			if e != nil {
				done <- e
				return
			}
			result := map[string]any{}
			if i == 0 {
				result["clientId"] = "phone"
			}
			if e = writeDesktopTest(c, map[string]any{"type": "response", "requestId": v["requestId"], "resultType": "success", "handledByClientId": "owner", "result": result}); e != nil {
				done <- e
				return
			}
		}
		if _, e = readDesktopFrame(c); e != nil {
			done <- e
			return
		}
		project := "/authorized"
		if action == "wrong-project" {
			project = "/other"
		}
		if e = writeDesktopTest(c, map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"hostId": "local", "conversationId": "original", "change": map[string]any{"type": "snapshot", "conversationState": desktopTestState("original", project, "Fixture", "inProgress")}}}); e != nil {
			done <- e
			return
		}
		if _, e = readDesktopFrame(c); e != nil {
			done <- e
			return
		}
		req, e := readDesktopFrame(c)
		if handle == nil {
			if e == nil {
				e = fmt.Errorf("unexpected queue mutation")
			} else {
				e = nil
			}
			done <- e
			return
		}
		if e == nil {
			e = handle(c, req)
		}
		done <- e
	}()
	return &DesktopBridge{Socket: path, QueueStatePath: statePath}, done
}
func queueAck(c net.Conn, req map[string]any, result any) error {
	return writeDesktopTest(c, map[string]any{"type": "response", "requestId": req["requestId"], "resultType": "success", "handledByClientId": "owner", "result": result})
}
func nativeMessage() map[string]any {
	return map[string]any{"id": "existing", "text": "Existing", "context": map[string]any{"prompt": "Existing\nAttachment notes", "imageAttachments": []any{map[string]any{"id": "image", "src": "data:image/png;base64,AA=="}}, "opaque": "preserve"}, "desktopOnly": map[string]any{"keep": true}}
}
func TestNativeQueueEnqueuesImmediatelyPreservingDesktopEntries(t *testing.T) {
	sibling := nativeMessage()
	bridge, done := queueFixture(t, []any{sibling}, "add", func(c net.Conn, req map[string]any) error {
		p := obj(req["params"])
		queue := arr(obj(p["state"])["original"])
		if req["method"] != "thread-follower-set-queued-follow-ups-state" || req["version"] != float64(1) || req["targetClientId"] != "owner" || p["conversationId"] != "original" || len(queue) != 2 || queueVersion(queue[0]) != queueVersion(sibling) {
			return errors.New("native queue identity or existing entry lost")
		}
		message := obj(queue[1])
		ctx := obj(message["context"])
		if message["id"] != "new" || message["text"] != "Next" || obj(message["submission"])["status"] != "queued" || obj(message["submission"])["queueModeOverride"] != "queue" || obj(message["submissionOptions"])["executionHostId"] != "local" || obj(message["submissionOptions"])["permissionProfileId"] != nil || message["requiresExistingOwner"] != true || ctx["prompt"] != "Next with file notes" || len(arr(ctx["imageAttachments"])) != 1 {
			return errors.New("native message context malformed")
		}
		return queueAck(c, req, map[string]any{"ok": true})
	})
	e := bridge.EnqueueNative(context.Background(), "original", "Next", "new", "/authorized", TurnOptions{}, []any{map[string]any{"type": "text", "text": "Next with file notes"}, map[string]any{"type": "image", "url": "data:image/png;base64,AA=="}})
	if e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	// The immediate acknowledged snapshot wins over delayed desktop persistence.
	list, e := bridge.NativeQueue("original")
	if e != nil || len(list) != 2 {
		t.Fatal("lost acknowledged queue before disk flush")
	}
	bridge.queueMu.Lock()
	bridge.queues = nil
	bridge.queueMu.Unlock()
	// The bridge never writes the desktop persistence file itself.
	list, e = bridge.NativeQueue("original")
	if e != nil || len(list) != 1 || list[0].Origin != "codex" {
		t.Fatal("native read shape or persistence changed")
	}
}
func TestNativeQueueEditingPreservesAttachmentsAndOpaqueContext(t *testing.T) {
	sibling := nativeMessage()
	bridge, done := queueFixture(t, []any{sibling}, "edit", func(c net.Conn, req map[string]any) error {
		q := arr(obj(obj(req["params"])["state"])["original"])
		v := obj(q[0])
		ctx := obj(v["context"])
		if req["method"] != "thread-follower-set-queued-follow-ups-state" || v["id"] != "existing" || v["text"] != "Edited" || ctx["prompt"] != "Edited\nAttachment notes" || ctx["opaque"] != "preserve" || len(arr(ctx["imageAttachments"])) != 1 || obj(v["desktopOnly"])["keep"] != true {
			return errors.New("edit lost context")
		}
		return queueAck(c, req, map[string]any{"ok": true})
	})
	e := bridge.ChangeNativeQueue(context.Background(), "original", "/authorized", "existing", queueVersion(sibling), "edit", "Edited", "edit-id")
	if e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
}
func TestNativeQueueRejectsStaleOrUnauthorizedMutation(t *testing.T) {
	for _, mode := range []string{"stale", "wrong-project"} {
		t.Run(mode, func(t *testing.T) {
			message := nativeMessage()
			bridge, done := queueFixture(t, []any{message}, mode, nil)
			version := queueVersion(message)
			if mode == "stale" {
				version = "stale"
			}
			if bridge.ChangeNativeQueue(context.Background(), "original", "/authorized", "existing", version, "edit", "Edited", "edit-id") == nil {
				t.Fatal("accepted stale or wrong project")
			}
			if e := <-done; e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestNativeSteerRemovesOnceBeforeSteeringAndDoesNotStartTurn(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			message := nativeMessage()
			bridge, done := queueFixture(t, []any{message}, "steer", func(c net.Conn, req map[string]any) error {
				if req["method"] != "thread-follower-remove-queued-message" || obj(req["params"])["messageId"] != "existing" {
					return errors.New("steer did not remove native entry first")
				}
				if e := queueAck(c, req, map[string]any{"removed": map[string]any{"index": 0, "message": message}}); e != nil {
					return e
				}
				next, e := readDesktopFrame(c)
				if e != nil {
					return e
				}
				p := obj(next["params"])
				if next["method"] != "thread-follower-steer-turn" || next["version"] != float64(1) || p["clientUserMessageId"] != "steer-id" || len(arr(p["input"])) != 2 || str(obj(arr(p["input"])[0])["text"]) != "Existing\nAttachment notes" {
					return errors.New("steer context mismatch")
				}
				if fail {
					return nil
				}
				return queueAck(c, next, map[string]any{"result": map[string]any{"turnId": "active"}})
			})
			err := bridge.ChangeNativeQueue(context.Background(), "original", "/authorized", "existing", queueVersion(message), "steer", "", "steer-id")
			if (err != nil) != fail {
				t.Fatalf("wrong outcome: %v", err)
			}
			if fail {
				var unknown *nativeSteerUnknown
				if !errors.As(err, &unknown) || unknown.removed["id"] != "existing" {
					t.Fatal("lost uncertain input")
				}
			}
			if e := <-done; e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestLocalQueueEditingVersionAndDispatchExclusion(t *testing.T) {
	n := setup(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	n.Store.Put("sessions", "s", store.Session{ID: "s", Agent: "claude", Managed: true, Project: root})
	m := &Manager{Store: n.Store, Roots: []string{root}}
	defer m.Close()
	if e := m.SendDelivery("s", "First", "q", TurnOptions{}, nil, "queue"); e != nil {
		t.Fatal(e)
	}
	list, e := m.Queue("s")
	if e != nil || len(list) != 1 {
		t.Fatal(e)
	}
	old := list[0].Version
	if e = m.ChangeQueue("s", "q", old, "edit", "Edited", "edit"); e != nil {
		t.Fatal(e)
	}
	list, _ = m.Queue("s")
	if list[0].Text != "Edited" || list[0].Version == old {
		t.Fatal("edit not persisted")
	}
	if m.ChangeQueue("s", "q", old, "delete", "", "stale-delete") == nil {
		t.Fatal("stale delete accepted")
	}
	if ok, e := n.Store.TransitionDelivery("q", "s", "queued", "dispatching"); e != nil || !ok {
		t.Fatal(e)
	}
	if m.ChangeQueue("s", "q", list[0].Version, "edit", "Too late", "late") == nil {
		t.Fatal("edited after dispatch")
	}
}

func TestNativeQueueUnknownAndRichContextAreNotResubmitted(t *testing.T) {
	for _, mode := range []string{"unknown", "context"} {
		t.Run(mode, func(t *testing.T) {
			message := nativeMessage()
			if mode == "unknown" {
				message["submission"] = map[string]any{"status": "outcome-unknown"}
				message["pausedReason"] = "submission-outcome-unknown"
				if queueRecord(message).State != "unknown" {
					t.Fatal("uncertain submission made editable")
				}
			} else {
				obj(message["context"])["mcpAppModelContextAttachments"] = []any{map[string]any{"kind": "message"}}
			}
			bridge, done := queueFixture(t, []any{message}, mode, nil)
			if bridge.ChangeNativeQueue(context.Background(), "original", "/authorized", "existing", queueVersion(message), "steer", "", "steer") == nil {
				t.Fatal("removed unsafe input")
			}
			if e := <-done; e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestNativeQueueBroadcastIsAuthoritativeAndScoped(t *testing.T) {
	bridge := &DesktopBridge{}
	c := &desktopConnection{bridge: bridge, owner: "owner", thread: "s"}
	frame := map[string]any{"type": "broadcast", "method": "thread-queued-followups-changed", "version": float64(2), "sourceClientId": "owner", "params": map[string]any{"hostId": "local", "conversationId": "s", "messages": []any{map[string]any{"id": "native", "text": "Fresh"}}}}
	c.captureQueue(frame)
	q, e := bridge.NativeQueue("s")
	if e != nil || len(q) != 1 || q[0].ID != "native" {
		t.Fatal("live snapshot not used before disk flush")
	}
	old := bridge.queueStamp("s")
	bridge.cacheQueue("s", "owner", []any{map[string]any{"id": "newer", "text": "Newer"}})
	bridge.cacheQueueAt("s", "owner", []any{map[string]any{"id": "old-ack"}}, &old)
	q, _ = bridge.NativeQueue("s")
	if q[0].ID != "newer" {
		t.Fatal("late acknowledgment overwrote native broadcast")
	}
	frame["sourceClientId"] = "wrong-owner"
	c.captureQueue(frame)
	frame["sourceClientId"] = "owner"
	obj(frame["params"])["conversationId"] = "another"
	c.captureQueue(frame)
	q, _ = bridge.NativeQueue("s")
	if q[0].ID != "newer" {
		t.Fatal("untrusted queue broadcast accepted")
	}
	bridge.queueOwner("s", "replacement-owner")
	if bridge.queueStamp("s") != 0 {
		t.Fatal("former owner's queue survived ownership change")
	}
}
