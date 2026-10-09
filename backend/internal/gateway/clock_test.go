package gateway

import (
	"agentdeck/internal/relay"
	"agentdeck/internal/secure"
	"agentdeck/internal/store"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGatewayClockScopesReplayAndRevocation(t *testing.T) {
	db, e := store.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	clock := secure.NewClockAt(time.Now().Add(24 * time.Hour).UnixMilli())
	g := NewWithClock(db, "", "", "pin", time.Second, clock)
	ts := httptest.NewTLSServer(g.Handler())
	defer ts.Close()
	g.URL = ts.URL
	g.Pin = relay.Hash(ts.Certificate().RawSubjectPublicKeyInfo)
	link, e := g.AddLocal("tenant-clock", "clock-local")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, e := relay.NewClient(link)
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		client.Run(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := relay.ContextClock(r.Context())
			if c == nil || abs(c.Millis()-clock.Millis()) > 10000 {
				http.Error(w, "wrong clock", 500)
				return
			}
			w.WriteHeader(200)
		}))
	}()
	await(t, client.Online)
	if abs(client.CurrentClock().Millis()-clock.Millis()) > 10000 {
		t.Fatal("Local did not adopt Gateway clock")
	}
	// Unlinked Local clock starts a day behind Gateway; enrolment translates duration.
	grant, e := client.Register(ctx, relay.Hash(secure.Random(32)), time.Now().Unix()+240)
	if e == nil {
		t.Fatal("Gateway-basis expiry must not accept Local wall-clock expiry after linking")
	}
	grant, e = client.Register(ctx, relay.Hash(secure.Random(32)), client.CurrentClock().Unix()+240)
	if e != nil || grant.Expires < clock.Unix()+230 {
		t.Fatal("Gateway grant expiry failed")
	}
	var node Node
	_ = db.Get("gwnodes", link.Local, &node)
	account := Account{ID: secure.ID(), Key: secure.B64.EncodeToString(secure.Random(32)), Expires: clock.Unix() + 300}
	flow := Flow{ID: secure.ID(), Key: secure.B64.EncodeToString(secure.Random(32)), Expires: clock.Unix() + 300, Used: true, Status: "ready"}
	_ = db.Put("gwaccounts", account.ID, account)
	_ = db.Put("gwflows", flow.ID, flow)
	for _, v := range []struct{ kind, id, key, bucket string }{{"local", node.ID, node.Key, "gwnodes"}, {"grant", grant.ID, grant.Key, "gwgrants"}, {"account", account.ID, account.Key, "gwaccounts"}, {"flow", flow.ID, flow.Key, "gwflows"}} {
		t.Run(v.kind, func(t *testing.T) {
			key, _ := secure.B64.DecodeString(v.key)
			c, e := relay.SyncClock(context.Background(), ts.Client(), ts.URL, v.kind, v.id, key)
			if e != nil || abs(c.Millis()-clock.Millis()) > 10000 {
				t.Fatal("valid scope rejected")
			}
			scope := v.kind + "/" + v.id
			box, _ := secure.SealJSON(key, "clock-request/"+scope, 0, 0, map[string]string{"challenge": secure.ID()})
			body, _ := json.Marshal(secure.ClockRequest{Kind: v.kind, ID: v.id, Box: box})
			for _, want := range []int{200, 401} {
				res, e := ts.Client().Post(ts.URL+"/v2/time-sync", "application/json", bytes.NewReader(body))
				if e != nil {
					t.Fatal(e)
				}
				res.Body.Close()
				if res.StatusCode != want {
					t.Fatal("clock request replay accepted")
				}
			}
			if _, e := relay.SyncClock(context.Background(), ts.Client(), ts.URL, v.kind, v.id, secure.Random(32)); e == nil {
				t.Fatal("wrong secret accepted")
			}
			if v.kind != "local" {
				_ = db.Delete(v.bucket, v.id)
				if _, e := relay.SyncClock(context.Background(), ts.Client(), ts.URL, v.kind, v.id, key); e == nil {
					t.Fatal("revoked identity accepted")
				}
			}
		})
	}
	cancel()
	g.Close()
	<-done
}
func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
