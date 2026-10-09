package secure

import (
	"encoding/json"
	"testing"
	"time"
)

func TestClockRotationUsesMonotonicTime(t *testing.T) {
	const rotation = 15 * time.Minute
	base := int64(1770000000000)
	base = base/rotation.Milliseconds()*rotation.Milliseconds() + rotation.Milliseconds() - 1
	var elapsed time.Duration
	c := &Clock{base: base, elapsed: func() time.Duration { return elapsed }}
	epoch := c.Epoch(rotation)
	elapsed = time.Millisecond
	if c.Epoch(rotation) != epoch+1 {
		t.Fatal("rotation boundary not crossed")
	}
	elapsed = 2 * rotation
	if c.Millis() != base+elapsed.Milliseconds() {
		t.Fatal("monotonic anchor changed")
	}
	for _, rtt := range []time.Duration{-1, MaxClockRTT + time.Nanosecond} {
		if _, e := CalibratedClock(base, rtt); e == nil {
			t.Fatal("invalid RTT accepted")
		}
	}
	if _, e := CalibratedClock(0, time.Second); e == nil {
		t.Fatal("invalid server time accepted")
	}
	sample, e := CalibratedClock(base, 200*time.Millisecond)
	if e != nil || sample.Millis() < base+100 || sample.Millis() > base+110 {
		t.Fatal("RTT midpoint incorrect")
	}
}
func TestClockEnvelopeIdentity(t *testing.T) {
	key := Random(32)
	scope := "device/test"
	challenge := ID()
	box, _ := SealJSON(key, "clock-request/"+scope, 0, 0, map[string]string{"challenge": challenge})
	got, e := ClockChallenge(key, scope, box)
	if e != nil || got != challenge {
		t.Fatal("valid challenge rejected")
	}
	if _, e = ClockChallenge(key, "grant/test", box); e == nil {
		t.Fatal("wrong scope accepted")
	}
	if _, e = ClockChallenge(Random(32), scope, box); e == nil {
		t.Fatal("wrong key accepted")
	}
	box.Seq = 1
	if _, e = ClockChallenge(key, scope, box); e == nil {
		t.Fatal("nonzero sequence accepted")
	}
	reply, _ := ClockReply(key, scope, challenge, NewClockAt(1770000000000))
	plain, e := Open(key, "clock-reply/"+scope, reply)
	var sample ClockSample
	if e != nil || json.Unmarshal(plain, &sample) != nil || sample.Challenge != challenge || sample.Version != 1 {
		t.Fatal("invalid authenticated sample")
	}
}
