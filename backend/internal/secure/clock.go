package secure

import (
	"encoding/json"
	"errors"
	"time"
)

const MaxClockRTT = 10 * time.Second

// Clock anchors protocol time once, then advances with Go's monotonic clock.
// Subsequent changes to the host wall clock cannot move a connection's epochs.
// Instances are immutable; recalibration creates a new clock for a new connection.
type Clock struct {
	base    int64
	elapsed func() time.Duration
}

func NewClock() *Clock { return NewClockAt(time.Now().UnixMilli()) }
func NewClockAt(ms int64) *Clock {
	start := time.Now()
	return &Clock{ms, func() time.Duration { return time.Since(start) }}
}
func (c *Clock) Millis() int64                      { return c.base + c.elapsed().Milliseconds() }
func (c *Clock) Unix() int64                        { return c.Millis() / 1000 }
func (c *Clock) Epoch(rotation time.Duration) int64 { return c.Millis() / rotation.Milliseconds() }
func CalibratedClock(ms int64, rtt time.Duration) (*Clock, error) {
	if ms <= 0 || rtt < 0 || rtt > MaxClockRTT {
		return nil, errors.New("invalid clock sample")
	}
	return NewClockAt(ms + rtt.Milliseconds()/2), nil
}

type ClockRequest struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Box  Box    `json:"box"`
}
type ClockSample struct {
	Challenge  string `json:"challenge"`
	ServerTime int64  `json:"server_time_ms"`
	Version    int    `json:"version"`
}

// Sync requests authenticate possession of a known key without trusting the
// caller's wall time. Replay rejection is performed by the endpoint before reply.
func ClockChallenge(key []byte, scope string, box Box) (string, error) {
	if box.Epoch != 0 || box.Seq != 0 {
		return "", errors.New("invalid clock envelope")
	}
	plain, err := Open(key, "clock-request/"+scope, box)
	if err != nil {
		return "", err
	}
	var v struct {
		Challenge string `json:"challenge"`
	}
	if json.Unmarshal(plain, &v) != nil || len(v.Challenge) < 16 || len(v.Challenge) > 128 {
		return "", errors.New("invalid clock challenge")
	}
	return v.Challenge, nil
}
func ClockReply(key []byte, scope, challenge string, clock *Clock) (Box, error) {
	return SealJSON(key, "clock-reply/"+scope, 0, 0, ClockSample{challenge, clock.Millis(), 1})
}
