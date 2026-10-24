package agent

import (
	"context"
	"net"
	"testing"
	"time"
)

// Reproduce the router's actual protocol: discovery cannot finish until every
// follower says it cannot own the thread. A silent follower used to stall the
// unowned-thread path for the router's full discovery timeout.
func TestDesktopFollowerAnswersRouterDiscoveryWhileAwaitingResponse(t *testing.T) {
	for _, waiting := range []string{"request", "snapshot"} {
		t.Run(waiting, func(t *testing.T) {
			client, router := net.Pipe()
			defer client.Close()
			defer router.Close()
			_ = client.SetDeadline(time.Now().Add(2 * time.Second))
			_ = router.SetDeadline(time.Now().Add(2 * time.Second))
			done := make(chan error, 1)
			go func() {
				in, err := readDesktopWireFrame(router)
				if err != nil {
					done <- err
					return
				}
				err = writeDesktopTest(router, map[string]any{"type": "client-discovery-request", "requestId": "router-probe", "request": map[string]any{"method": "thread-owner-discovery"}})
				if err != nil {
					done <- err
					return
				}
				answer, err := readDesktopWireFrame(router)
				if err != nil {
					done <- err
					return
				}
				if answer["type"] != "client-discovery-response" || answer["requestId"] != "router-probe" || obj(answer["response"])["canHandle"] != false {
					t.Error("follower did not explicitly decline ownership")
				}
				if waiting == "request" {
					err = writeDesktopTest(router, map[string]any{"type": "response", "requestId": in["requestId"], "resultType": "success", "handledByClientId": "owner", "result": map[string]any{}})
				} else {
					err = writeDesktopTest(router, map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"hostId": "local", "conversationId": "thread", "change": map[string]any{"type": "snapshot", "conversationState": map[string]any{"id": "thread", "cwd": "/project"}}}})
					if err == nil {
						_, err = readDesktopWireFrame(router)
					}
				}
				done <- err
			}()
			c := &desktopConnection{conn: client, client: "follower", owner: "owner"}
			var err error
			if waiting == "request" {
				_, err = c.request("thread-owner-discovery", 1, map[string]any{}, "")
			} else {
				_, err = c.snapshot("thread", "/project")
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDesktopFollowerAnswersDiscoveryDuringIdleFollow(t *testing.T) {
	// Follow uses the same frame reader as requests/snapshots. An idle stream
	// must answer probes even when no conversation data is arriving.
	c, r := net.Pipe()
	defer c.Close()
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	context.AfterFunc(ctx, func() { c.Close(); r.Close() })
	done := make(chan error, 1)
	go func() {
		if e := writeDesktopTest(r, map[string]any{"type": "client-discovery-request", "requestId": "idle-probe"}); e != nil {
			done <- e
			return
		}
		v, e := readDesktopWireFrame(r)
		if e == nil && obj(v["response"])["canHandle"] != false {
			t.Error("follower claimed ownership")
		}
		if e == nil {
			e = writeDesktopTest(r, map[string]any{"type": "broadcast", "method": "fixture"})
		}
		done <- e
	}()
	if _, e := readDesktopFrame(c); e != nil {
		t.Fatal(e)
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}
