package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Identity struct {
	Tenant string `json:"tenant"`
	Owner  string `json:"owner"`
	Name   string `json:"name"`
}

// Feishu tokens are used only to resolve verified identity, then discarded.
// Provider responses and credentials never appear in errors or logs.
type Feishu struct {
	AppID, Secret, Redirect, Scope  string
	AuthorizeURL, TokenURL, UserURL string
	HTTP                            *http.Client
	AllowedTenants                  map[string]bool
}

func NewFeishu(id, secret, redirect string) *Feishu {
	return &Feishu{AppID: id, Secret: secret, Redirect: redirect, AuthorizeURL: "https://accounts.feishu.cn/open-apis/authen/v1/authorize", TokenURL: "https://open.feishu.cn/open-apis/authen/v2/oauth/token", UserURL: "https://open.feishu.cn/open-apis/authen/v1/user_info", HTTP: &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (f *Feishu) Authorize(state, verifierChallenge string) string {
	u, _ := url.Parse(f.AuthorizeURL)
	q := u.Query()
	q.Set("client_id", f.AppID)
	q.Set("redirect_uri", f.Redirect)
	q.Set("response_type", "code")
	q.Set("state", state)
	q.Set("code_challenge", verifierChallenge)
	q.Set("code_challenge_method", "S256")
	if f.Scope != "" {
		q.Set("scope", f.Scope)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
func (f *Feishu) Resolve(ctx context.Context, code, verifier string) (Identity, error) {
	var identity Identity
	raw, _ := json.Marshal(map[string]string{"grant_type": "authorization_code", "client_id": f.AppID, "client_secret": f.Secret, "redirect_uri": f.Redirect, "code": code, "code_verifier": verifier})
	req, e := http.NewRequestWithContext(ctx, "POST", f.TokenURL, bytes.NewReader(raw))
	if e != nil {
		return identity, errors.New("invalid identity provider configuration")
	}
	req.Header.Set("Content-Type", "application/json")
	res, e := f.HTTP.Do(req)
	if e != nil {
		return identity, errors.New("identity provider unavailable")
	}
	defer res.Body.Close()
	var token struct {
		Code   json.RawMessage `json:"code"`
		Error  string          `json:"error"`
		Access string          `json:"access_token"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&token) != nil || token.Error != "" || !successCode(token.Code) || token.Access == "" {
		return identity, errors.New("Feishu authorization rejected")
	}
	user, e := http.NewRequestWithContext(ctx, "GET", f.UserURL, nil)
	if e != nil {
		return identity, errors.New("invalid identity provider configuration")
	}
	user.Header.Set("Authorization", "Bearer "+token.Access)
	result, e := f.HTTP.Do(user)
	if e != nil {
		return identity, errors.New("identity provider unavailable")
	}
	defer result.Body.Close()
	var info struct {
		Code json.RawMessage `json:"code"`
		Data struct {
			Tenant string `json:"tenant_key"`
			OpenID string `json:"open_id"`
			Name   string `json:"name"`
		} `json:"data"`
	}
	if result.StatusCode != 200 || json.NewDecoder(io.LimitReader(result.Body, 64<<10)).Decode(&info) != nil || (len(info.Code) == 0 || !successCode(info.Code)) || info.Data.Tenant == "" || info.Data.OpenID == "" {
		return identity, errors.New("Feishu identity missing tenant or user")
	}
	if len(f.AllowedTenants) > 0 && !f.AllowedTenants[info.Data.Tenant] {
		return identity, errors.New("Feishu tenant not permitted")
	}
	return Identity{info.Data.Tenant, info.Data.OpenID, info.Data.Name}, nil
}
func successCode(code json.RawMessage) bool {
	return len(code) == 0 || strings.Trim(string(code), "\"") == "0"
}
