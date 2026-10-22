package server

import "log/slog"

type clientOpenTiming struct {
	Send         int64 `json:"send_ms"`
	RoundTrip    int64 `json:"round_trip_ms"`
	ReceiveQueue int64 `json:"receive_queue_ms"`
	Decode       int64 `json:"decode_ms"`
	Apply        int64 `json:"apply_ms"`
	Paint        int64 `json:"paint_ms"`
	Total        int64 `json:"total_ms"`
	Bytes        int64 `json:"wire_bytes"`
}

func (v *clientOpenTiming) valid() bool {
	if v == nil || v.Bytes < 0 || v.Bytes > 24<<20 {
		return false
	}
	for _, ms := range []int64{v.Send, v.RoundTrip, v.ReceiveQueue, v.Decode, v.Apply, v.Paint, v.Total} {
		if ms < 0 || ms > 120000 {
			return false
		}
	}
	return v.Total == v.Send+v.RoundTrip+v.ReceiveQueue+v.Decode+v.Apply+v.Paint
}

// Accept only numeric, bounded diagnostics over an already authenticated stream.
// Labels and fields are fixed; no client-provided text is logged as diagnostics.
func logClientTiming(req Request) bool {
	if !req.Timing.valid() || req.Session == "" || len(req.Session) > 128 || len(req.ID) != 32 {
		return false
	}
	for _, c := range req.Session {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '-' || c == '.' || c == '/' || c == ':') {
			return false
		}
	}
	for _, c := range req.ID {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	slog.Info("session_open_client", "session", req.Session, "request", req.ID,
		"send_ms", req.Timing.Send, "round_trip_ms", req.Timing.RoundTrip,
		"receive_queue_ms", req.Timing.ReceiveQueue, "decode_ms", req.Timing.Decode,
		"apply_ms", req.Timing.Apply, "paint_ms", req.Timing.Paint,
		"total_ms", req.Timing.Total, "wire_bytes", req.Timing.Bytes)
	return true
}
