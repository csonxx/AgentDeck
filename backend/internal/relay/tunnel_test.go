package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agentdeck/internal/secure"
	"github.com/gorilla/websocket"
)

func TestTunnelRejectsReplayTamperingAndWrongDirection(t *testing.T) {
	for _, attack := range []string{"replay", "tamper", "wrong-direction"} {
		t.Run(attack, func(t *testing.T) {
			root, salt := secure.Random(32), secure.Random(32)
			done := make(chan error, 1)
			ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				up := websocket.Upgrader{}
				ws, e := up.Upgrade(w, r, nil)
				if e != nil {
					return
				}
				peer := NewPeer(ws, root, salt, "g2l", time.Second)
				done <- peer.Run()
			}))
			defer ts.Close()
			d := websocket.Dialer{TLSClientConfig: ts.Client().Transport.(*http.Transport).TLSClientConfig}
			ws, _, e := d.Dial(strings.Replace(ts.URL, "https://", "wss://", 1), nil)
			if e != nil {
				t.Fatal(e)
			}
			defer ws.Close()
			ep := time.Now().UnixMilli() / 1000
			direction := "l2g"
			if attack == "wrong-direction" {
				direction = "g2l"
			}
			box, _ := secure.SealJSON(secure.Traffic(root, salt, "tunnel-"+direction, ep), "tunnel/"+direction, ep, 1, Message{Kind: "ping"})
			if attack == "tamper" {
				data, _ := secure.B64.DecodeString(box.Data)
				data[len(data)-1] ^= 1
				box.Data = secure.B64.EncodeToString(data)
			}
			if e = ws.WriteJSON(box); e != nil {
				t.Fatal(e)
			}
			if attack == "replay" {
				if e = ws.WriteJSON(box); e != nil {
					t.Fatal(e)
				}
			}
			select {
			case e := <-done:
				if e == nil {
					t.Fatal("invalid encrypted frame accepted")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("invalid frame did not terminate tunnel")
			}
		})
	}
}
func TestBlockedCreditAndReadRespectChangedDeadlines(t *testing.T) {
	c := newConn(nil, "stream", "grant")
	for i := 0; i < 8; i++ {
		<-c.credits
	}
	done := make(chan error, 1)
	go func() { done <- c.credit() }()
	_ = c.SetWriteDeadline(time.Now().Add(20 * time.Millisecond))
	select {
	case e := <-done:
		if _, ok := e.(*timeoutError); !ok {
			t.Fatal("write deadline not honored")
		}
	case <-time.After(time.Second):
		t.Fatal("credit wait stuck")
	}
	go func() { _, e := c.Read(make([]byte, 10)); done <- e }()
	_ = c.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	select {
	case e := <-done:
		if _, ok := e.(*timeoutError); !ok {
			t.Fatal("read deadline not honored")
		}
	case <-time.After(time.Second):
		t.Fatal("read wait stuck")
	}
}
