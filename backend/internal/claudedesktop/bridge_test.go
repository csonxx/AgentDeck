package claudedesktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestDebuggerURLIsStrictlyLoopback(t *testing.T) {
	for _, raw := range []string{"ws://127.0.0.1:9229/uuid", "ws://127.0.0.1:9229/hello"} {
		if !validDebuggerURL(raw) {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"ws://evil.test:9229/uuid", "ws://127.0.0.1.evil.test:9229/uuid", "ws://localhost:9229/uuid", "ws://127.0.0.1:9228/uuid", "wss://127.0.0.1:9229/uuid", "ws://user:secret@127.0.0.1:9229/uuid", "ws://127.0.0.1:9229/uuid?x=1", "ws://127.0.0.1:9229/../uuid"} {
		if validDebuggerURL(raw) {
			t.Fatal("unsafe URL", raw)
		}
	}
}

func rpcFixture(t *testing.T, response map[string]any) (*Bridge, <-chan string) {
	t.Helper()
	expression := make(chan string, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer c.Close()
		var frame struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Expression string `json:"expression"`
				Await      bool   `json:"awaitPromise"`
			} `json:"params"`
		}
		if c.ReadJSON(&frame) != nil {
			return
		}
		expression <- frame.Params.Expression
		if frame.Method != "Runtime.evaluate" || !frame.Params.Await {
			t.Error("wrong RPC method")
		}
		c.WriteJSON(map[string]any{"id": frame.ID, "result": response})
	}))
	t.Cleanup(s.Close)
	c, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http"), nil)
	if e != nil {
		t.Fatal(e)
	}
	b := &Bridge{conn: c, wc: 2}
	t.Cleanup(b.Close)
	return b, expression
}

func TestNativeSendQuotesUntrustedInputAndKeepsOwnerAPI(t *testing.T) {
	b, expressions := rpcFixture(t, map[string]any{"result": map[string]any{"value": true}})
	text := "');process.exit();` ${danger}\n中文"
	target := Target{Host: "local_original", Native: "native-id", CWD: "/project", Project: "/project"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e := b.Send(ctx, target, text, "message-uuid"); e != nil {
		t.Fatal(e)
	}
	expr := <-expressions
	start := strings.Index(expr, "executeJavaScript(") + len("executeJavaScript(")
	var renderer string
	if e := json.Unmarshal([]byte(strings.TrimSuffix(expr[start:], ")})()")), &renderer); e != nil {
		t.Fatal("renderer expression not encoded as JSON", e)
	}
	if !strings.Contains(renderer, "const pending=l.sendMessage(s.sessionId,"+js(text)+",undefined,undefined,undefined,undefined,undefined,\"message-uuid\")") {
		t.Fatal("wrong owner API arguments")
	}
	for _, forbidden := range []string{"origin:", "initiator:", "start(", "forkSession(", "setPermissionMode(", "cloud="} {
		if strings.Contains(renderer, forbidden) {
			t.Fatal("provenance or ownership changed", forbidden)
		}
	}
}

func TestInspectorExceptionsNeverExposeSessionOrCredentialText(t *testing.T) {
	b, _ := rpcFixture(t, map[string]any{"exceptionDetails": map[string]any{"text": "private-session-and-secret"}, "result": map[string]any{"value": false}})
	var result bool
	e := b.evaluate(context.Background(), "true", &result)
	if e == nil || strings.Contains(e.Error(), "private-session") {
		t.Fatal("exception leaked", e)
	}
}
