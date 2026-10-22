package server

import (
	"strings"
	"testing"
)

func TestClientOpenTimingIsBoundedNumericMetadata(t *testing.T) {
	s, ts := setupServer(t)
	p := pairPhone(t, s, ts)
	p.connect(t, ts, s)
	p.read(t, "sessions")
	timing := &clientOpenTiming{Send: 50, RoundTrip: 150, ReceiveQueue: 50, Decode: 100, Apply: 20, Paint: 80, Total: 450, Bytes: 26000}
	p.send(t, Request{Method: "client_timing", ID: strings.Repeat("a", 32), Session: "session", Timing: timing})
	if result := p.read(t, "result"); result["result"].(map[string]any)["accepted"] != true {
		t.Fatal("valid diagnostic was rejected")
	}
	// Reporting must not create history, modify sessions or contain plaintext.
	if s.Store.LastSeq() != 0 {
		t.Fatal("diagnostic modified conversation history")
	}
	for _, change := range []func(*clientOpenTiming){
		func(v *clientOpenTiming) { v.Decode = -1 },
		func(v *clientOpenTiming) { v.Total++ },
		func(v *clientOpenTiming) { v.Bytes = 25 << 20 },
		func(v *clientOpenTiming) { v.Total = 120001 },
	} {
		invalid := *timing
		change(&invalid)
		if invalid.valid() {
			t.Fatal("unbounded/inconsistent diagnostic accepted")
		}
	}
	if logClientTiming(Request{ID: strings.Repeat("a", 32), Session: "message body\nsecret", Timing: timing}) {
		t.Fatal("arbitrary text allowed as a diagnostic session identifier")
	}
}
