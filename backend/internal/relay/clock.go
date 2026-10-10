package relay

import (
	"agentdeck/internal/secure"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

type ClockRejection struct{ Status int }

func (e *ClockRejection) Error() string { return "gateway clock rejected" }

func SyncClock(ctx context.Context, h *http.Client, origin, kind, id string, key []byte) (*secure.Clock, error) {
	scope := kind + "/" + id
	challenge := secure.ID()
	box, err := secure.SealJSON(key, "clock-request/"+scope, 0, 0, map[string]string{"challenge": challenge})
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(secure.ClockRequest{Kind: kind, ID: id, Box: box})
	req, err := http.NewRequestWithContext(ctx, "POST", origin+"/v2/time-sync", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	started := time.Now()
	res, err := h.Do(req)
	if err != nil {
		return nil, errors.New("gateway clock unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, &ClockRejection{Status: res.StatusCode}
	}
	if json.NewDecoder(io.LimitReader(res.Body, 8192)).Decode(&box) != nil {
		return nil, errors.New("invalid clock response")
	}
	elapsed := time.Since(started)
	if box.Epoch != 0 || box.Seq != 0 {
		return nil, errors.New("invalid clock envelope")
	}
	plain, err := secure.Open(key, "clock-reply/"+scope, box)
	if err != nil {
		return nil, err
	}
	var sample secure.ClockSample
	if json.Unmarshal(plain, &sample) != nil || sample.Challenge != challenge || sample.Version != 1 {
		return nil, errors.New("clock challenge mismatch")
	}
	return secure.CalibratedClock(sample.ServerTime, elapsed)
}
