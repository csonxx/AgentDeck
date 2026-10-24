package agent

import (
	"agentdeck/internal/store"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func desktopTestState(id, project, text, status string) map[string]any {
	return map[string]any{"id": id, "hostId": "local", "cwd": project, "latestThreadSettings": map[string]any{"developer_instructions": "PRIVATE_CONTEXT"}, "turns": []any{map[string]any{"turnId": "turn", "status": status, "items": []any{map[string]any{"id": "m", "type": "agentMessage", "text": text}, map[string]any{"id": "r", "type": "reasoning", "encrypted_content": "PRIVATE_REASONING", "summary": []any{map[string]any{"text": "Public summary"}}}, map[string]any{"id": "private", "type": "systemMessage", "text": "PRIVATE_SYSTEM"}}}}}
}
func writeDesktopTest(c net.Conn, v any) error {
	b, _ := json.Marshal(v)
	frame := make([]byte, len(b)+4)
	binary.LittleEndian.PutUint32(frame, uint32(len(b)))
	copy(frame[4:], b)
	_, e := io.Copy(c, strings.NewReader(string(frame)))
	return e
}
func TestDesktopStreamPublishesBeforeToolsAndFlushesTrailingPatch(t *testing.T) {
	dir, e := os.MkdirTemp("/tmp", "ad-stream-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	os.Chmod(dir, 0700)
	path := filepath.Join(dir, "ipc.sock")
	listener, e := net.Listen("unix", path)
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	os.Chmod(path, 0600)
	fixtureDone := make(chan error, 1)
	finish := make(chan struct{})
	go func() {
		c, e := listener.Accept()
		if e != nil {
			fixtureDone <- e
			return
		}
		defer c.Close()
		for step := 0; step < 2; step++ {
			v, e := readDesktopFrame(c)
			if e != nil {
				fixtureDone <- e
				return
			}
			result := map[string]any{}
			if step == 0 {
				result["clientId"] = "follower"
			}
			if e = writeDesktopTest(c, map[string]any{"type": "response", "requestId": v["requestId"], "resultType": "success", "handledByClientId": "owner", "result": result}); e != nil {
				fixtureDone <- e
				return
			}
		}
		follow, e := readDesktopFrame(c)
		if e != nil {
			fixtureDone <- e
			return
		}
		if follow["method"] != "thread-stream-following-changed" || obj(follow["params"])["following"] != true || str(arr(follow["targetClientIds"])[0]) != "owner" {
			fixtureDone <- errors.New("incorrect read-only subscription")
			return
		}
		broadcast := func(owner, thread string, change any) error {
			return writeDesktopTest(c, map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": owner, "params": map[string]any{"conversationId": thread, "hostId": "local", "change": change}})
		}
		for _, bad := range [][2]string{{"other-owner", "original"}, {"owner", "another-thread"}} {
			broadcast(bad[0], bad[1], map[string]any{"type": "snapshot", "revision": 0, "conversationState": desktopTestState(bad[1], "/fixture", "WRONG_THREAD", "inProgress")})
		}
		broadcast("owner", "original", map[string]any{"type": "snapshot", "revision": 1, "conversationState": desktopTestState("original", "/fixture", "H", "inProgress")})
		for i, text := range []string{"Hello", "Hello world"} {
			broadcast("owner", "original", map[string]any{"type": "patches", "baseRevision": i + 1, "revision": i + 2, "patches": []any{map[string]any{"op": "replace", "path": []any{"turns", 0, "items", 0, "text"}, "value": text}}})
		}
		// No tool event and no later frame: the 80 ms tick must flush the last text.
		<-finish
		broadcast("owner", "original", map[string]any{"type": "patches", "baseRevision": 3, "revision": 4, "patches": []any{map[string]any{"op": "replace", "path": []any{"turns", 0, "status"}, "value": "completed"}}})
		_, e = readDesktopFrame(c)
		fixtureDone <- e
	}()
	n := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Store.Put("sessions", "original", store.Session{ID: "original", Agent: "codex", Project: "/fixture", Source: "desktop", Managed: true})
	p := &desktopProjection{m: &Manager{Store: n.Store, Normalizer: n}, session: store.Session{ID: "original", Project: "/fixture"}, seen: map[string]string{}}
	observations := make(chan string, 8)
	done := make(chan error, 1)
	go func() {
		done <- (&DesktopBridge{Socket: path}).Follow(ctx, "original", func(state map[string]any) error {
			e := p.publish(state)
			observations <- str(obj(arr(obj(desktopTurns(state)[0])["items"])[0])["text"]) + ":" + desktopTurnStatus(state)
			return e
		})
	}()
	wait := func(wanted string) {
		t.Helper()
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		for {
			select {
			case got := <-observations:
				if got == wanted {
					return
				}
			case <-timer.C:
				t.Fatal("native update was not received before tool activity")
			}
		}
	}
	wait("Hello world:running")
	page, _ := n.Store.History("original", 0, 40)
	if len(page.Events) != 2 {
		t.Fatal("public message/summary missing or private state leaked")
	}
	for _, event := range page.Events {
		if strings.Contains(event.Text, "PRIVATE") || strings.Contains(event.Text, "WRONG_THREAD") || strings.Contains(event.Text, "\"text\"") {
			t.Fatal("unfiltered desktop state leaked")
		}
	}
	close(finish)
	wait("Hello world:completed")
	cancel()
	<-done
	select {
	case <-fixtureDone:
	case <-time.After(time.Second):
		t.Fatal("subscription did not stop")
	}
}
func TestDesktopCanonicalProjectionAndReceiptIdentity(t *testing.T) {
	n := setup(t)
	ss := store.Session{ID: "s", Agent: "codex", Source: "desktop", Project: "/fixture", Managed: true}
	n.Store.Put("sessions", "s", ss)
	n.Store.AppendDesktop(store.Event{ID: "receipt", Session: "s", Agent: "codex", Source: "desktop-live", Kind: "message", Role: "user", Item: "request:0", Text: "Same accepted input", Mode: "snapshot"}, false)
	turn := map[string]any{"turnId": "t", "status": "inProgress", "items": []any{map[string]any{"id": "native-user", "clientId": "request", "type": "userMessage", "content": []any{map[string]any{"type": "text", "text": "Same accepted input"}}}}}
	state := map[string]any{"id": "s", "cwd": "/fixture", "turns": []any{}, "turnHistory": map[string]any{"kind": "canonical", "history": map[string]any{"entitiesByKey": map[string]any{"entity": turn}, "islands": []any{map[string]any{"entries": []any{map[string]any{"value": "entity"}}}}}}}
	p := &desktopProjection{m: &Manager{Store: n.Store, Normalizer: n}, session: ss, seen: map[string]string{}}
	if e := p.publish(state); e != nil {
		t.Fatal(e)
	}
	page, _ := n.Store.History("s", 0, 40)
	if len(page.Events) != 1 {
		t.Fatal("receipt duplicated native user message")
	}
}
func TestDesktopPatchBounds(t *testing.T) {
	node := map[string]any{"a": []any{"one", "two"}}
	out, e := applyDesktopPatch(node, []any{"a", float64(1)}, "add", "middle")
	if e != nil || len(arr(obj(out)["a"])) != 3 {
		t.Fatal("array insertion failed")
	}
	if _, e = applyDesktopPatch(node, []any{"a", float64(99)}, "replace", "bad"); e == nil {
		t.Fatal("out of bounds patch accepted")
	}
	if _, e = applyDesktopPatch(node, []any{"missing", "child"}, "replace", "bad"); e == nil {
		t.Fatal("missing path accepted")
	}
}

type followingFixture struct {
	desktopFixture
	calls   atomic.Int32
	started chan struct{}
	stopped chan struct{}
}

type lateOwnerFixture struct {
	desktopFixture
	calls atomic.Int32
	ready chan struct{}
}

func (f *lateOwnerFixture) Follow(ctx context.Context, _ string, _ func(map[string]any) error) error {
	if f.calls.Add(1) == 1 {
		return errDesktopDiscoveryTimeout
	}
	close(f.ready)
	<-ctx.Done()
	return ctx.Err()
}
func TestDesktopWatchPromptlyRetriesAnOwnerOpenedByInput(t *testing.T) {
	n := setup(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	n.Store.Put("sessions", "s", store.Session{ID: "s", Agent: "codex", Source: "desktop", Project: root, Managed: true})
	f := &lateOwnerFixture{ready: make(chan struct{})}
	m := &Manager{Store: n.Store, Normalizer: n, Roots: []string{root}, Desktop: f}
	defer m.Close()
	release := m.WatchDesktop(nil, "s")
	defer release()
	select {
	case <-f.ready:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("watch kept the old two-second retry gap after owner discovery timed out")
	}
}

func (f *followingFixture) Follow(ctx context.Context, id string, consume func(map[string]any) error) error {
	f.calls.Add(1)
	close(f.started)
	<-ctx.Done()
	close(f.stopped)
	return ctx.Err()
}
func TestDesktopWatchLeaseSharedAndShutdownWaits(t *testing.T) {
	n := setup(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	n.Store.Put("sessions", "s", store.Session{ID: "s", Agent: "codex", Source: "desktop", Project: root, Managed: true})
	fixture := &followingFixture{started: make(chan struct{}), stopped: make(chan struct{})}
	m := &Manager{Store: n.Store, Normalizer: n, Roots: []string{root}, Desktop: fixture}
	first := m.WatchDesktop(nil, "s")
	second := m.WatchDesktop(nil, "s")
	<-fixture.started
	first()
	select {
	case <-fixture.stopped:
		t.Fatal("one phone stopped another phone's subscription")
	default:
	}
	m.Close()
	select {
	case <-fixture.stopped:
	default:
		t.Fatal("shutdown did not wait for follower")
	}
	second()
	if fixture.calls.Load() != 1 {
		t.Fatal("duplicate owner connections")
	}
}

func TestDesktopStreamRejectsRevisionGap(t *testing.T) {
	dir, e := os.MkdirTemp("/tmp", "ad-gap-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	os.Chmod(dir, 0700)
	path := filepath.Join(dir, "ipc.sock")
	l, e := net.Listen("unix", path)
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	os.Chmod(path, 0600)
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, e := l.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		for step := 0; step < 2; step++ {
			v, e := readDesktopFrame(c)
			if e != nil {
				return
			}
			r := map[string]any{}
			if step == 0 {
				r["clientId"] = "follower"
			}
			if writeDesktopTest(c, map[string]any{"type": "response", "requestId": v["requestId"], "resultType": "success", "handledByClientId": "owner", "result": r}) != nil {
				return
			}
		}
		if _, e = readDesktopFrame(c); e != nil {
			return
		}
		for _, change := range []any{
			map[string]any{"type": "snapshot", "revision": 1, "conversationState": desktopTestState("s", "/fixture", "Good", "inProgress")},
			map[string]any{"type": "patches", "baseRevision": 9, "revision": 10, "patches": []any{map[string]any{"op": "replace", "path": []any{"turns", 0, "items", 0, "text"}, "value": "INVALID_GAP"}}},
		} {
			if writeDesktopTest(c, map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"hostId": "local", "conversationId": "s", "change": change}}) != nil {
				return
			}
		}
		readDesktopFrame(c)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	calls := 0
	e = (&DesktopBridge{Socket: path}).Follow(ctx, "s", func(state map[string]any) error { calls++; return nil })
	if e == nil || !strings.Contains(e.Error(), "revision gap") || calls != 1 {
		t.Fatal("gap was applied instead of forcing a fresh snapshot")
	}
	<-done
}
