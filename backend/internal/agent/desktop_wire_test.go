package agent

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopWireTargetsOriginalWithoutPermissionOverrides(t *testing.T) {
	for _, rich := range []bool{false, true} {
		t.Run(fmt.Sprint(rich), func(t *testing.T) { testDesktopInputWire(t, rich) })
	}
}
func testDesktopInputWire(t *testing.T, rich bool) {
	dir, err := os.MkdirTemp("/tmp", "ad-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	os.Chmod(dir, 0700)
	path := filepath.Join(dir, "desktop.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	os.Chmod(path, 0600)
	done := make(chan error, 1)
	go func() {
		c, e := l.Accept()
		if e != nil {
			done <- e
			return
		}
		defer c.Close()
		for step := 0; step < 5; step++ {
			var h [4]byte
			if _, e = io.ReadFull(c, h[:]); e != nil {
				done <- e
				return
			}
			raw := make([]byte, binary.LittleEndian.Uint32(h[:]))
			if _, e = io.ReadFull(c, raw); e != nil {
				done <- e
				return
			}
			var v map[string]any
			json.Unmarshal(raw, &v)
			params := obj(v["params"])
			result := map[string]any{}
			owner := "owner"
			switch step {
			case 0:
				if v["method"] != "initialize" {
					t.Error("missing initialization")
				}
				result["clientId"] = "phone-client"
			case 1:
				if v["method"] != "thread-owner-discovery" || params["conversationId"] != "original" || params["hostId"] != "local" {
					t.Error("wrong owner discovery")
				}
			case 2:
				if v["method"] != "thread-stream-following-changed" || params["following"] != true {
					t.Error("missing input state preflight")
				}
				e = writeDesktopTest(c, map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"hostId": "local", "conversationId": "original", "change": map[string]any{"type": "snapshot", "conversationState": desktopTestState("original", "/authorized/project", "Fixture", "completed")}}})
				if e != nil {
					done <- e
					return
				}
				continue
			case 3:
				if v["method"] != "thread-stream-following-changed" || params["following"] != false {
					t.Error("missing input state unsubscribe")
				}
				continue
			case 4:
				if v["method"] != "thread-follower-start-turn" || v["version"] != float64(2) || v["targetClientId"] != "owner" {
					t.Error("not the original desktop executor")
				}
				turn := obj(params["turnStart"])
				req := obj(turn["request"])
				if params["conversationId"] != "original" || req["threadId"] != "original" || req["cwd"] != "/authorized/project" {
					t.Error("changed thread/project")
				}
				if req["approvalPolicy"] != nil || req["sandboxPolicy"] != nil || obj(turn["context"])["inheritThreadSettings"] != true {
					t.Error("overrode desktop settings")
				}
				if rich {
					if req["model"] != "fixture-model" || req["effort"] != "high" || len(arr(req["input"])) != 2 || obj(arr(req["input"])[1])["type"] != "image" {
						t.Error("rich input lost")
					}
				} else if req["model"] != nil || req["effort"] != nil {
					t.Error("unexpected settings")
				}
				if str(obj(arr(req["input"])[0])["text"]) != "fixture prompt" {
					t.Error("message lost")
				}
				if elements, ok := obj(arr(req["input"])[0])["text_elements"].([]any); !ok || len(elements) != 0 {
					t.Error("desktop text input is missing its explicit empty UI elements")
				}
			}
			out, _ := json.Marshal(map[string]any{"type": "response", "requestId": v["requestId"], "method": v["method"], "resultType": "success", "handledByClientId": owner, "result": result})
			frame := make([]byte, 4+len(out))
			binary.LittleEndian.PutUint32(frame, uint32(len(out)))
			copy(frame[4:], out)
			if _, e = c.Write(frame); e != nil {
				done <- e
				return
			}
		}
		done <- nil
	}()
	b := &DesktopBridge{Socket: path}
	if rich {
		err = b.SendInput(context.Background(), "original", "fixture prompt", "idempotent-request", "/authorized/project", TurnOptions{Model: "fixture-model", Effort: "high"}, []any{map[string]any{"type": "text", "text": "fixture prompt"}, map[string]any{"type": "image", "url": "data:image/png;base64,fixture"}})
	} else {
		err = b.Send(context.Background(), "original", "fixture prompt", "idempotent-request", "/authorized/project")
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	os.Chmod(path, 0660)
	if err = b.Validate(); err == nil {
		t.Fatal("non-private desktop socket accepted")
	}
}
