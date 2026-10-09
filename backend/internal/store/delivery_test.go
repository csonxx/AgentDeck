package store

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestDeliveryEncryptedPersistenceIdempotencyAndClaim(t *testing.T) {
	dir := t.TempDir()
	s, e := New(dir)
	if e != nil {
		t.Fatal(e)
	}
	d := Delivery{ID: "request", Session: "s", State: "queued"}
	payload := map[string]string{"Text": "PRIVATE_QUEUED_TEXT"}
	if e = s.EnqueueDelivery(d, payload); e != nil {
		t.Fatal(e)
	}
	if e = s.EnqueueDelivery(d, payload); e != nil {
		t.Fatal("same request failed", e)
	}
	if e = s.EnqueueDelivery(d, map[string]string{"Text": "different"}); e == nil {
		t.Fatal("conflicting payload replaced queue")
	}
	if _, e = s.TransitionDelivery("request", "other", "queued", "cancelled"); e == nil {
		t.Fatal("cross-session cancellation allowed")
	}
	var wg sync.WaitGroup
	var claims atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			changed, _ := s.TransitionDelivery("request", "s", "queued", "dispatching")
			if changed {
				claims.Add(1)
			}
		}()
	}
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatal("queue item dispatched more than once")
	}
	s.Close()
	raw, _ := os.ReadFile(filepath.Join(dir, "vault.db"))
	if bytes.Contains(raw, []byte("PRIVATE_QUEUED_TEXT")) {
		t.Fatal("plaintext queue persisted")
	}
	s, e = New(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	pending, e := s.Deliveries("dispatching")
	if e != nil || len(pending) != 1 {
		t.Fatal("restart lost dispatch uncertainty")
	}
	var recovered map[string]string
	if e = s.Get("deliverypayloads", "request", &recovered); e != nil || recovered["Text"] != "PRIVATE_QUEUED_TEXT" {
		t.Fatal("encrypted payload recovery failed")
	}
}
