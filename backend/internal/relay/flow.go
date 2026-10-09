package relay

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"agentdeck/internal/secure"
)

type LoginFlow struct {
	ID, URL string
	Key     []byte
	Expires int64
	Gateway string
	HTTP    *http.Client
	Clock   *secure.Clock
}

func BeginLogin(ctx context.Context, h *http.Client, gateway, kind, name string) (*LoginFlow, error) {
	p, e := ecdh.X25519().GenerateKey(rand.Reader)
	if e != nil {
		return nil, e
	}
	raw, _ := json.Marshal(map[string]string{"kind": kind, "name": name, "public": secure.B64.EncodeToString(p.PublicKey().Bytes())})
	req, e := http.NewRequestWithContext(ctx, "POST", gateway+"/v2/auth/flows", bytes.NewReader(raw))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	res, e := h.Do(req)
	if e != nil {
		return nil, errors.New("gateway login unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, errors.New("gateway Feishu login not configured or unavailable")
	}
	var v struct {
		ID      string `json:"flow"`
		URL     string `json:"url"`
		Public  string `json:"public"`
		Salt    string `json:"salt"`
		Expires int64  `json:"expires"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 16<<10)).Decode(&v) != nil || !ValidID(v.ID) || v.URL != gateway+"/v2/auth/login?flow="+v.ID {
		return nil, errors.New("invalid login handoff")
	}
	pub, e := secure.B64.DecodeString(v.Public)
	if e != nil {
		return nil, e
	}
	remote, e := ecdh.X25519().NewPublicKey(pub)
	if e != nil {
		return nil, e
	}
	shared, e := p.ECDH(remote)
	if e != nil {
		return nil, e
	}
	salt, e := secure.B64.DecodeString(v.Salt)
	if e != nil || len(salt) != 32 {
		return nil, errors.New("invalid login salt")
	}
	key := secure.Derive(shared, salt, "agentdeck/auth-flow/v1/"+v.ID)
	clock, e := SyncClock(ctx, h, gateway, "flow", v.ID, key)
	if e != nil {
		return nil, e
	}
	return &LoginFlow{ID: v.ID, URL: v.URL, Key: key, Expires: v.Expires, Gateway: gateway, HTTP: h, Clock: clock}, nil
}
func (f *LoginFlow) Poll(ctx context.Context) (json.RawMessage, bool, error) {
	if f.Expires < f.Clock.Unix() {
		return nil, false, errors.New("Feishu login expired")
	}
	path := "/v2/auth/poll"
	proof, e := AuthAt(f.Key, "flow/"+f.ID, "POST", path, nil, "", f.Clock.Unix())
	if e != nil {
		return nil, false, e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", f.Gateway+path, nil)
	if e != nil {
		return nil, false, e
	}
	req.Header.Set("Authorization", "Flow "+f.ID)
	req.Header.Set("X-Relay-Auth", proof)
	res, e := f.HTTP.Do(req)
	if e != nil {
		return nil, false, errors.New("login polling unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, false, errors.New("login polling rejected")
	}
	var box secure.Box
	if json.NewDecoder(io.LimitReader(res.Body, 32<<10)).Decode(&box) != nil {
		return nil, false, errors.New("invalid login response")
	}
	plain, e := secure.Open(f.Key, "flow-result/"+f.ID, box)
	if e != nil {
		return nil, false, e
	}
	var v struct {
		Status    string          `json:"status"`
		Result    json.RawMessage `json:"result"`
		Challenge string          `json:"challenge"`
	}
	if json.Unmarshal(plain, &v) != nil || v.Challenge != Hash([]byte(proof)) {
		return nil, false, errors.New("login challenge rejected")
	}
	if v.Status == "failed" {
		return nil, false, errors.New("Feishu authorization failed")
	}
	return v.Result, v.Status == "ready", nil
}
