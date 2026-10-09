package relay

import (
	"agentdeck/internal/secure"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSyncClockAuthenticatesChallenge(t *testing.T) {
	key := secure.Random(32)
	clock := secure.NewClockAt(time.Now().Add(24 * time.Hour).UnixMilli())
	for _, mode := range []string{"valid", "challenge", "key", "envelope"} {
		t.Run(mode, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req secure.ClockRequest
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					http.Error(w, "bad", 400)
					return
				}
				challenge, e := secure.ClockChallenge(key, "local/test", req.Box)
				if e != nil {
					http.Error(w, "bad", 401)
					return
				}
				if mode == "challenge" {
					challenge = secure.ID()
				}
				secret := key
				if mode == "key" {
					secret = secure.Random(32)
				}
				box, _ := secure.ClockReply(secret, "local/test", challenge, clock)
				if mode == "envelope" {
					box.Epoch = 1
				}
				_ = json.NewEncoder(w).Encode(box)
			}))
			defer ts.Close()
			calibrated, e := SyncClock(context.Background(), ts.Client(), ts.URL, "local", "test", key)
			if mode == "valid" {
				if e != nil || calibrated.Millis() < clock.Millis()-100 || calibrated.Millis() > clock.Millis()+100 {
					t.Fatal("valid remote clock rejected")
				}
			} else if e == nil {
				t.Fatal("forged response accepted")
			}
		})
	}
}
