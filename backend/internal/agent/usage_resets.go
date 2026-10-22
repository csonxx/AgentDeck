package agent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type ResetCredit struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	ResetType   string `json:"reset_type"`
	GrantedAt   string `json:"granted_at,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}
type CodexResets struct {
	Available        *int64        `json:"available_count"`
	DetailsKnown     bool          `json:"details_known"`
	Credits          []ResetCredit `json:"credits"`
	AccountVersion   string        `json:"account_version"`
	Eligible         bool          `json:"eligible"`
	EligibilityKnown bool          `json:"eligibility_known"`
	AsOf             string        `json:"as_of"`
}
type ResetHistoryEvent struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	OccurredAt string `json:"occurred_at"`
}
type ResetHistory struct {
	Events      []ResetHistoryEvent `json:"events"`
	Next        string              `json:"next"`
	WindowStart string              `json:"window_start,omitempty"`
	AsOf        string              `json:"as_of,omitempty"`
}
type resetIntent struct {
	ID      string `json:"id"`
	Owner   string `json:"owner"`
	Account string `json:"account"`
	Credit  string `json:"credit"`
	Key     string `json:"key,omitempty"`
	Expires int64  `json:"expires"`
	Outcome string `json:"outcome,omitempty"`
}
type ResetQuote struct {
	Intent  string      `json:"intent"`
	Expires string      `json:"expires_at"`
	Credit  string      `json:"credit"`
	State   CodexResets `json:"state"`
}
type ResetResult struct {
	Outcome string `json:"outcome"`
}

func resetAccount(v map[string]any) string {
	id := str(v["accountId"])
	if id == "" || str(v["_resetUser"]) == "" {
		return ""
	}
	h := sha256.Sum256([]byte("agentdeck/codex-reset/account/v1/" + id + "/" + strings.ToLower(str(v["_resetUser"]))))
	return hex.EncodeToString(h[:])
}
func resetEligible(v map[string]any) (known, eligible bool) {
	b := obj(obj(v["rateLimitsByLimitId"])["codex"])
	if len(b) == 0 {
		b = obj(v["rateLimits"])
	}
	for _, k := range []string{"primary", "secondary"} {
		w := obj(b[k])
		n, ok := w["usedPercent"].(float64)
		mins, _ := w["windowDurationMins"].(float64)
		if ok && n >= 0 && n <= 100 && (mins == 300 || mins == 10080) {
			known = true
			eligible = eligible || n >= 90
		}
	}
	return
}
func resetTime(v any) string {
	if n, ok := v.(float64); ok && n > 0 {
		return time.Unix(int64(n), 0).UTC().Format(time.RFC3339)
	}
	if s, ok := v.(string); ok {
		if t, e := time.Parse(time.RFC3339Nano, s); e == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return ""
}
func resetText(v any) string {
	s := []rune(Redact(str(v)))
	if len(s) > 2000 {
		s = s[:2000]
	}
	return string(s)
}
func normalizeResets(v map[string]any) CodexResets {
	out := CodexResets{AccountVersion: resetAccount(v), AsOf: time.Now().UTC().Format(time.RFC3339), Credits: []ResetCredit{}}
	summary := obj(v["rateLimitResetCredits"])
	if n, ok := summary["availableCount"].(float64); ok && n >= 0 && n <= 1000000 && n == float64(int64(n)) {
		count := int64(n)
		out.Available = &count
	}
	rows, ok := summary["credits"].([]any)
	out.DetailsKnown = ok
	for _, row := range rows {
		c := obj(row)
		id := str(c["id"])
		if id == "" || len(id) > 512 || len(out.Credits) >= 500 {
			continue
		}
		status := str(c["status"])
		switch status {
		case "available", "redeeming", "redeemed":
		default:
			status = "unknown"
		}
		kind := str(c["resetType"])
		if kind != "codexRateLimits" {
			kind = "unknown"
		}
		out.Credits = append(out.Credits, ResetCredit{ID: id, Title: resetText(c["title"]), Description: resetText(c["description"]), Status: status, ResetType: kind, GrantedAt: resetTime(c["grantedAt"]), ExpiresAt: resetTime(c["expiresAt"])})
	}
	out.EligibilityKnown, out.Eligible = resetEligible(v)
	return out
}
func (m *Manager) resetCall(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	if m.resetRPC != nil {
		return m.resetRPC(ctx, method, params)
	}
	return nativeResetCall(ctx, method, params)
}
func (m *Manager) resetContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(nonNil(m.Context), 25*time.Second)
}
func (m *Manager) CodexResetState() (CodexResets, error) {
	ctx, cancel := m.resetContext()
	defer cancel()
	v, e := m.resetCall(ctx, "account/rateLimits/read", map[string]any{})
	if e != nil {
		return CodexResets{}, errors.New("暂时无法读取 Codex 重置卡，请检查电脑登录和 CLI 版本")
	}
	out := normalizeResets(v)
	if out.AccountVersion == "" {
		return out, errors.New("Codex 未返回可核对的账号，请更新电脑 CLI")
	}
	return out, nil
}
func (m *Manager) PrepareCodexReset(owner, credit, account string) (ResetQuote, error) {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	if m.updateDraining {
		return ResetQuote{}, errors.New("工作站正在更新，请稍后重试")
	}
	m.resetMu.Lock()
	defer m.resetMu.Unlock()
	if m.Store == nil || owner == "" || len(credit) > 512 {
		return ResetQuote{}, errors.New("无效的重置卡请求")
	}
	state, e := m.CodexResetState()
	if e != nil {
		return ResetQuote{}, e
	}
	if account == "" || account != state.AccountVersion {
		return ResetQuote{}, errors.New("电脑 Codex 账号已变化，请刷新后重新确认")
	}
	if state.Available == nil || *state.Available == 0 {
		return ResetQuote{}, errors.New("当前没有可用重置卡")
	}
	if !state.EligibilityKnown || !state.Eligible {
		return ResetQuote{}, errors.New("当前没有可重置的核心额度，五小时或每周额度需剩余不超过 10%")
	}
	if credit != "" {
		valid := false
		for _, c := range state.Credits {
			if c.ID == credit && c.Status == "available" && c.ResetType == "codexRateLimits" && (c.ExpiresAt == "" || c.ExpiresAt > time.Now().UTC().Format(time.RFC3339)) {
				valid = true
			}
		}
		if !valid {
			return ResetQuote{}, errors.New("这张重置卡已不可用，请刷新")
		}
	}
	var previous resetIntent
	if m.Store.Get("agentconfig", "reset-intent/"+owner, &previous) == nil && previous.Key != "" && previous.Outcome == "unknown" && previous.Expires >= time.Now().Unix() {
		return ResetQuote{}, errors.New("上次重置结果尚未确认，请先核对原请求")
	}
	id := UUID()
	intent := resetIntent{ID: id, Owner: owner, Credit: credit, Account: account, Expires: time.Now().Add(3 * time.Minute).Unix()}
	if e = m.Store.Put("agentconfig", "reset-intent/"+owner, intent); e != nil {
		return ResetQuote{}, errors.New("重置确认保存失败，尚未使用重置卡")
	}
	return ResetQuote{Intent: id, Credit: credit, Expires: time.Unix(intent.Expires, 0).UTC().Format(time.RFC3339), State: state}, nil
}

// Only an explicitly confirmed, device/account-bound intent can consume a card.
// Unknown outcomes retain the same upstream key; reads never invoke consume.
func (m *Manager) ConsumeCodexReset(owner, intentID, key string, confirmed bool) (ResetResult, error) {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	if m.updateDraining {
		return ResetResult{}, errors.New("工作站正在更新，请稍后重试")
	}
	m.resetMu.Lock()
	defer m.resetMu.Unlock()
	if !confirmed || m.Store == nil || owner == "" || len(intentID) > 100 || len(key) < 16 || len(key) > 100 {
		return ResetResult{}, errors.New("请先明确确认使用一张重置卡")
	}
	var intent resetIntent
	if m.Store.Get("agentconfig", "reset-intent/"+owner, &intent) != nil || intent.Owner != owner || intent.ID != intentID {
		return ResetResult{}, errors.New("重置确认不属于当前手机或已失效")
	}
	if intent.Key != "" && intent.Key != key {
		return ResetResult{}, errors.New("请使用原重置请求核对结果，勿新建请求")
	}
	if intent.Outcome != "" && intent.Outcome != "unknown" {
		return ResetResult{intent.Outcome}, nil
	}
	retry := intent.Key != ""
	if intent.Expires < time.Now().Unix() {
		return ResetResult{}, errors.New("确认已过期，请重新查看额度后确认")
	}
	state, e := m.CodexResetState()
	if e != nil {
		return ResetResult{}, e
	}
	if state.AccountVersion != intent.Account {
		return ResetResult{}, errors.New("电脑 Codex 账号已变化，未使用重置卡")
	}
	if !retry && (!state.EligibilityKnown || !state.Eligible) {
		return ResetResult{"nothingToReset"}, nil
	}
	intent.Key = key
	intent.Outcome = "unknown"
	if e = m.Store.Put("agentconfig", "reset-intent/"+owner, intent); e != nil {
		return ResetResult{}, errors.New("重置回执保存失败，未提交")
	}
	ctx, cancel := m.resetContext()
	defer cancel()
	params := map[string]any{"idempotencyKey": key, "_accountVersion": intent.Account, "_retry": retry}
	if intent.Credit != "" {
		params["creditId"] = intent.Credit
	}
	result, e := m.resetCall(ctx, "account/rateLimitResetCredit/consume", params)
	if e != nil {
		return ResetResult{"unknown"}, nil
	}
	outcome := str(result["outcome"])
	switch outcome {
	case "reset", "alreadyRedeemed", "noCredit", "nothingToReset":
	default:
		outcome = "unknown"
	}
	intent.Outcome = outcome
	if e = m.Store.Put("agentconfig", "reset-intent/"+owner, intent); e != nil {
		return ResetResult{"unknown"}, nil
	}
	if outcome == "reset" || outcome == "alreadyRedeemed" {
		m.usageMu.Lock()
		delete(m.usageCache, "codex")
		m.usageMu.Unlock()
	}
	return ResetResult{outcome}, nil
}

func nativeResetCall(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	if method != "account/rateLimits/read" && method != "account/rateLimitResetCredit/consume" {
		return nil, errors.New("unsupported account operation")
	}
	cmd := exec.CommandContext(ctx, "codex", "app-server")
	cmd.Dir, _ = os.UserHomeDir()
	cmd.Stderr = io.Discard
	in, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		return nil, e
	}
	defer func() { in.Close(); cmd.Process.Kill(); cmd.Wait() }()
	scan := bufio.NewScanner(out)
	scan.Buffer(make([]byte, 64<<10), 4<<20)
	call := func(name string, args map[string]any) (map[string]any, error) {
		id := UUID()
		if e := json.NewEncoder(in).Encode(map[string]any{"id": id, "method": name, "params": args}); e != nil {
			return nil, e
		}
		for scan.Scan() {
			var v map[string]any
			if json.Unmarshal(scan.Bytes(), &v) != nil {
				continue
			}
			if str(v["id"]) == id {
				if v["error"] != nil {
					return nil, errors.New("Codex account operation unconfirmed")
				}
				return obj(v["result"]), nil
			}
		}
		return nil, errors.New("Codex account connection closed")
	}
	if _, e = call("initialize", map[string]any{"clientInfo": map[string]any{"name": "agentdeck", "version": "0.4.48"}}); e != nil {
		return nil, e
	}
	if e = json.NewEncoder(in).Encode(map[string]any{"method": "initialized"}); e != nil {
		return nil, e
	}
	identity, e := call("account/read", map[string]any{"refreshToken": false})
	if e != nil {
		return nil, e
	}
	user := str(obj(identity["account"])["email"])
	if method == "account/rateLimits/read" {
		result, e := call(method, map[string]any{})
		if e == nil {
			result["_resetUser"] = user
		}
		return result, e
	}
	fresh, e := call("account/rateLimits/read", map[string]any{})
	if e != nil {
		return nil, e
	}
	fresh["_resetUser"] = user
	if resetAccount(fresh) == "" || resetAccount(fresh) != str(params["_accountVersion"]) {
		return nil, errors.New("Codex account changed")
	}
	if params["_retry"] != true {
		known, eligible := resetEligible(fresh)
		if !known || !eligible {
			return map[string]any{"outcome": "nothingToReset"}, nil
		}
	}
	native := map[string]any{"idempotencyKey": params["idempotencyKey"]}
	if id := str(params["creditId"]); id != "" {
		native["creditId"] = id
	}
	return call(method, native)
}

func (m *Manager) CodexResetHistory(cursor string) (ResetHistory, error) {
	if len(cursor) > 2048 {
		return ResetHistory{}, errors.New("历史游标无效")
	}
	ctx, cancel := m.resetContext()
	defer cancel()
	raw, e := m.resetCall(ctx, "account/rateLimits/read", map[string]any{})
	if e != nil {
		return ResetHistory{}, errors.New("无法核对 Codex 历史账号")
	}
	account := str(raw["accountId"])
	if account == "" {
		return ResetHistory{}, errors.New("Codex 历史账号暂不可用")
	}
	read := m.resetHistoryRead
	if read == nil {
		read = readResetHistory
	}
	v, e := read(ctx, account, str(raw["_resetUser"]), cursor)
	if e != nil {
		return ResetHistory{}, errors.New("重置卡历史暂不可用，请检查电脑登录或稍后重试")
	}
	rows, ok := v["events"].([]any)
	if !ok {
		return ResetHistory{}, errors.New("重置卡历史格式暂不支持")
	}
	page := ResetHistory{Events: []ResetHistoryEvent{}, Next: str(v["next_cursor"]), WindowStart: resetTime(v["window_start"]), AsOf: resetTime(v["as_of"])}
	if len(page.Next) > 2048 {
		return ResetHistory{}, errors.New("重置卡历史游标无效")
	}
	for _, row := range rows {
		x := obj(row)
		if len(page.Events) >= 500 {
			break
		}
		id := str(x["id"])
		if id == "" || len(id) > 512 {
			continue
		}
		kind := str(x["kind"])
		// Official history uses "used"; present the same used-card state as
		// the native consume result without losing unknown future kinds.
		if kind == "used" {
			kind = "redeemed"
		}
		if kind != "granted" && kind != "redeemed" {
			kind = "unknown"
		}
		page.Events = append(page.Events, ResetHistoryEvent{id, kind, resetTime(x["occurred_at"])})
	}
	return page, nil
}

// Fixed read-only origin/path. Tokens never leave this process or enter logs.
func readResetHistory(ctx context.Context, account, user, cursor string) (map[string]any, error) {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		h, _ := os.UserHomeDir()
		home = filepath.Join(h, ".codex")
	}
	f, e := os.Open(filepath.Join(home, "auth.json"))
	if e != nil {
		return nil, e
	}
	defer f.Close()
	var auth struct {
		Tokens struct {
			ID      string `json:"id_token"`
			Access  string `json:"access_token"`
			Account string `json:"account_id"`
		} `json:"tokens"`
	}
	if e = json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&auth); e != nil {
		return nil, errors.New("Codex credentials unavailable")
	}
	parts := strings.Split(auth.Tokens.ID, ".")
	var claims struct {
		Email string `json:"email"`
	}
	if len(parts) != 3 {
		return nil, errors.New("Codex history identity unavailable")
	}
	payload, decodeErr := base64.RawURLEncoding.DecodeString(parts[1])
	if decodeErr != nil || json.Unmarshal(payload, &claims) != nil || !strings.EqualFold(claims.Email, user) || user == "" {
		return nil, errors.New("Codex history identity changed")
	}
	if auth.Tokens.Account != account || auth.Tokens.Access == "" {
		return nil, errors.New("Codex history account changed")
	}
	target := "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/history"
	if cursor != "" {
		target += "?" + url.Values{"cursor": []string{cursor}}.Encode()
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+auth.Tokens.Access)
	req.Header.Set("ChatGPT-Account-Id", account)
	req.Header.Set("User-Agent", "AgentDeck/0.4.48")
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := client.Do(req)
	if e != nil {
		return nil, errors.New("Codex history read failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, errors.New("Codex history unavailable")
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if e != nil || len(data) > 2<<20 {
		return nil, errors.New("Codex history exceeds limit")
	}
	var v map[string]any
	if json.Unmarshal(data, &v) != nil {
		return nil, errors.New("Codex history invalid")
	}
	return v, nil
}
