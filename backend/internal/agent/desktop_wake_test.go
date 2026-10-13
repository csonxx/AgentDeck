package agent

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestDesktopWakeOnlyForExplicitUnownedOriginalInput(t *testing.T) {
	for _, mode := range []string{"wake", "owned", "passive", "rejected", "lost-ack", "invalid-id"} {
		t.Run(mode, func(t *testing.T) {
			dir, e := os.MkdirTemp("/tmp", "ad-wake-")
			if e != nil {
				t.Fatal(e)
			}
			defer os.RemoveAll(dir)
			path := filepath.Join(dir, "ipc.sock")
			listener, e := net.Listen("unix", path)
			if e != nil {
				t.Fatal(e)
			}
			os.Chmod(path, 0600)
			var workers sync.WaitGroup
			var opened, turns, discoveries atomic.Int32
			var ready atomic.Bool
			ready.Store(mode == "owned" || mode == "lost-ack")
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				for {
					c, e := listener.Accept()
					if e != nil {
						return
					}
					workers.Add(1)
					go func() {
						defer workers.Done()
						defer c.Close()
						for {
							in, e := readDesktopFrame(c)
							if e != nil {
								return
							}
							out := map[string]any{"type": "response", "requestId": in["requestId"], "resultType": "success", "handledByClientId": "native-owner", "result": map[string]any{}}
							switch in["method"] {
							case "initialize":
								out["result"] = map[string]any{"clientId": "phone"}
							case "thread-owner-discovery":
								discoveries.Add(1)
								if !ready.Load() {
									out["resultType"] = "error"
									out["error"] = "no-client-found"
								}
								if mode == "rejected" {
									out["error"] = "unsupported-version"
								}
							case "thread-follower-start-turn":
								turns.Add(1)
								p := obj(in["params"])
								r := obj(obj(p["turnStart"])["request"])
								if p["conversationId"] != idForWakeTest || r["threadId"] != idForWakeTest || r["cwd"] != "/authorized/project" || in["targetClientId"] != "native-owner" {
									t.Error("wrong original owner/project")
								}
								if mode == "lost-ack" {
									return
								}
							default:
								t.Error("unexpected native request")
								return
							}
							raw, _ := json.Marshal(out)
							frame := make([]byte, 4+len(raw))
							binary.LittleEndian.PutUint32(frame, uint32(len(raw)))
							copy(frame[4:], raw)
							if _, e = c.Write(frame); e != nil {
								return
							}
						}
					}()
				}
			}()
			defer func() { listener.Close(); <-finished; workers.Wait() }()
			b := &DesktopBridge{Socket: path, OpenThread: func(ctx context.Context, id string) error {
				opened.Add(1)
				if id != idForWakeTest {
					t.Error("invalid wake ID")
				}
				ready.Store(true)
				return nil
			}}
			id := idForWakeTest
			if mode == "invalid-id" {
				id = "new?prompt=injected"
			}
			var err error
			if mode == "passive" {
				_, err = b.Read(context.Background(), id)
			} else {
				err = b.Send(context.Background(), id, "fixture", UUID(), "/authorized/project")
			}
			if mode == "wake" || mode == "owned" {
				if err != nil || turns.Load() != 1 {
					t.Fatal("original input failed", err)
				}
			} else if err == nil {
				t.Fatal("failure hidden")
			}
			if mode == "wake" {
				if opened.Load() != 1 || discoveries.Load() != 2 {
					t.Fatal("wake did not rediscover original owner")
				}
			} else if opened.Load() != 0 {
				t.Fatal("opened on passive read, rejection, known owner or unconfirmed send")
			}
			if mode == "passive" && !errors.Is(err, errDesktopNoOwner) {
				t.Fatal("missing owner confused with transport error")
			}
			if mode == "lost-ack" && turns.Load() != 1 {
				t.Fatal("unconfirmed send retried")
			}
		})
	}
}

const idForWakeTest = "01a1237b-2828-7c81-8754-25c06ab72ce0"
