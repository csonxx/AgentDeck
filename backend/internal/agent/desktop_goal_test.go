package agent

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDesktopGoalPauseTargetsNativeScheduler(t *testing.T) {
	for _, mode := range []string{"active", "pause-error", "wrong-project"} {
		t.Run(mode, func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "ad-steer-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			os.Chmod(dir, 0700)
			path := filepath.Join(dir, "ipc.sock")
			listener, err := net.Listen("unix", path)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			os.Chmod(path, 0600)
			done := make(chan error, 1)
			go func() {
				c, e := listener.Accept()
				if e != nil {
					done <- e
					return
				}
				defer c.Close()
				c.SetDeadline(time.Now().Add(5 * time.Second))
				for i := 0; i < 2; i++ {
					req, e := readDesktopFrame(c)
					if e != nil {
						done <- e
						return
					}
					result := map[string]any{}
					if i == 0 {
						result["clientId"] = "phone"
					}
					if e = writeDesktopTest(c, map[string]any{"type": "response", "requestId": req["requestId"], "resultType": "success", "handledByClientId": "owner", "result": result}); e != nil {
						done <- e
						return
					}
				}
				follow, e := readDesktopFrame(c)
				if e != nil || obj(follow["params"])["following"] != true {
					done <- fmt.Errorf("missing subscription: %v", e)
					return
				}
				status, project := "inProgress", "/authorized"
				if mode == "idle" {
					status = "completed"
				}
				if mode == "wrong-project" {
					project = "/another"
				}
				e = writeDesktopTest(c, map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"hostId": "local", "conversationId": "original", "change": map[string]any{"type": "snapshot", "conversationState": desktopTestState("original", project, "Fixture", status)}}})
				if e != nil {
					done <- e
					return
				}
				unfollow, e := readDesktopFrame(c)
				if e != nil || obj(unfollow["params"])["following"] != false {
					done <- fmt.Errorf("missing unsubscribe: %v", e)
					return
				}
				req, e := readDesktopFrame(c)
				if mode == "wrong-project" {
					if e == nil {
						done <- fmt.Errorf("inactive or wrong project received mutation")
						return
					}
					done <- nil
					return
				}
				if e != nil {
					done <- e
					return
				}
				p := obj(req["params"])
				if req["method"] != "thread-follower-interrupt-turn" || req["version"] != float64(3) || req["targetClientId"] != "owner" || p["conversationId"] != "original" || p["mode"] != "user-stop" || p["expectedTurnId"] != nil {
					done <- fmt.Errorf("wrong native goal pause request")
					return
				}
				result := map[string]any{"ok": true}
				if mode == "pause-error" {
					result["goalPauseError"] = "fixture failure"
				}
				e = writeDesktopTest(c, map[string]any{"type": "response", "requestId": req["requestId"], "resultType": "success", "handledByClientId": "owner", "result": map[string]any{"method": "thread-follower-interrupt-turn", "result": result}})
				done <- e
			}()
			e := (&DesktopBridge{Socket: path}).PauseGoal(context.Background(), "original", "/authorized")
			if (e == nil) != (mode == "active") {
				t.Fatalf("incorrect acceptance for %s: %v", mode, e)
			}
			if e = <-done; e != nil {
				t.Fatal(e)
			}
		})
	}
}
