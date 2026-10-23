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

func TestSendTimingRejectsUnboundedAndInconsistentValues(t *testing.T) {
	r := Request{ID: strings.Repeat("a", 32), Session: "original", SendTiming: &clientSendTiming{Prepare: 40, Request: 150, Total: 190}}
	if !logClientSendTiming(r) {
		t.Fatal("valid send diagnostic rejected")
	}
	for _, v := range []*clientSendTiming{nil, {Prepare: -1, Request: 2, Total: 1}, {Prepare: 1, Request: 2, Total: 4}, {Prepare: 120001, Request: 0, Total: 120001}, {Prepare: 9223372036854775807, Request: 1, Total: -9223372036854775808}} {
		r.SendTiming = v
		if logClientSendTiming(r) {
			t.Fatal("invalid diagnostic accepted")
		}
	}
}

func TestSendAndRunningTimingCanBeReportedWithoutChangingHistory(t *testing.T) {
	s, ts := setupServer(t)
	p := pairPhone(t, s, ts)
	p.connect(t, ts, s)
	p.read(t, "sessions")
	for _, running := range []*int64{nil, new(int64)} {
		if running != nil {
			*running = 8500
		}
		p.send(t, Request{Method: "client_timing", Kind: "send", ID: strings.Repeat("a", 32), Session: "original", SendTiming: &clientSendTiming{Prepare: 30, Request: 250, Total: 280, Running: running}})
		if r := p.read(t, "result"); r["result"].(map[string]any)["accepted"] != true {
			t.Fatal("send milestone dropped")
		}
	}
	if s.Store.LastSeq() != 0 {
		t.Fatal("diagnostics changed conversation history")
	}
}
