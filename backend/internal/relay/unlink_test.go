package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agentdeck/internal/secure"
)

func TestUnlinkDoesNotTreatUnavailableClockAsRevoked(t *testing.T) {
	for _, status := range []int{401, 404, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer ts.Close()
			c, err := NewClient(Link{Version: 1, URL: ts.URL, TLSTrust: "pin", Pin: Hash(ts.Certificate().RawSubjectPublicKeyInfo), Tenant: "test-tenant", Local: "test-local", Key: secure.B64.EncodeToString(secure.Random(32))})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err = c.Unlink(ctx)
			if status == 401 && err != nil {
				t.Fatal("revoked identity must be retryable")
			}
			if status != 401 && err == nil {
				t.Fatal("gateway failure incorrectly counted as revocation")
			}
		})
	}
}
