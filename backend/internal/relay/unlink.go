package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"agentdeck/internal/secure"
)

// Unlink revokes only this authenticated workstation. TLS and a proof-bound
// encrypted receipt protect the operation; an old gateway cannot silently pass.
func (c *Client) Unlink(ctx context.Context) error {
	key, _ := secure.B64.DecodeString(c.Link.Key)
	clock, err := c.syncClock(ctx)
	if err != nil {
		// A validated gateway rejecting the identity also covers a lost successful
		// unlink receipt. There is no usable remote association left to preserve.
		var rejected *ClockRejection
		if errors.As(err, &rejected) && rejected.Status == 401 {
			return nil
		}
		return errors.New("无法联系网关，关联尚未解除，请检查网络后重试")
	}
	const path = "/v2/local/unlink"
	proof, err := AuthAt(key, "local/"+c.Link.Local, "POST", path, nil, "", clock.Unix())
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.Link.URL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Local "+c.Link.Local)
	req.Header.Set("X-Relay-Auth", proof)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return errors.New("网关解除请求未完成，请重试")
	}
	defer res.Body.Close()
	if res.StatusCode == 401 {
		return nil
	}
	if res.StatusCode == 404 || res.StatusCode == 405 {
		return errors.New("网关版本尚不支持解除关联，请先更新网关")
	}
	if res.StatusCode != 200 {
		return errors.New("网关未确认解除关联，请重试")
	}
	var box secure.Box
	if json.NewDecoder(io.LimitReader(res.Body, 16<<10)).Decode(&box) != nil {
		return errors.New("网关解除回执格式异常")
	}
	raw, err := secure.Open(key, "local-unlink-response/"+c.Link.Local, box)
	var reply struct {
		OK        bool   `json:"ok"`
		Challenge string `json:"challenge"`
	}
	if err != nil || json.Unmarshal(raw, &reply) != nil || !reply.OK || reply.Challenge != Hash([]byte(proof)) {
		return errors.New("网关解除回执校验失败，请重试")
	}
	return nil
}
