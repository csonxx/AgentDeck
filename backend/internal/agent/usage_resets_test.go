package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

func resetFixture() map[string]any {
	return map[string]any{"accountId": "fake-account", "_resetUser": "fake@example.invalid", "rateLimitsByLimitId": map[string]any{"codex": map[string]any{"primary": map[string]any{"usedPercent": float64(95), "windowDurationMins": float64(300)}}}, "rateLimitResetCredits": map[string]any{"availableCount": float64(3), "credits": []any{map[string]any{"id": "credit-test", "status": "available", "resetType": "codexRateLimits", "title": "Example reset", "description": "Native description", "grantedAt": float64(1700000000), "expiresAt": float64(4102444800)}}}}
}
func TestCodexResetMissingIsUnknownAndOnlyCoreQuotaEligible(t *testing.T) {
	s := normalizeResets(resetFixture())
	if s.Available == nil || *s.Available != 3 || !s.DetailsKnown || !s.Eligible || len(s.Credits) != 1 {
		t.Fatal("native detail projection lost")
	}
	raw, _ := json.Marshal(s)
	if contains(string(raw), "fake-account") || contains(string(raw), "fake@example") {
		t.Fatal("account identity leaked")
	}
	missing := normalizeResets(map[string]any{})
	if missing.Available != nil || missing.DetailsKnown || missing.Eligible {
		t.Fatal("missing fields became zero/eligible")
	}
	v := resetFixture()
	v["rateLimitResetCredits"] = map[string]any{"availableCount": float64(5), "credits": nil}
	if normalizeResets(v).DetailsKnown {
		t.Fatal("count-only detail fabricated")
	}
	v["rateLimitsByLimitId"] = map[string]any{"other": map[string]any{"primary": map[string]any{"usedPercent": float64(100), "windowDurationMins": float64(300)}}}
	if _, yes := resetEligible(v); yes {
		t.Fatal("non-core quota permits consume")
	}
}
func TestCodexResetExplicitConfirmationDeviceAccountAndDedup(t *testing.T) {
	m := connectorManager(t)
	raw := resetFixture()
	calls := 0
	m.resetRPC = func(_ context.Context, method string, params map[string]any) (map[string]any, error) {
		if method == "account/rateLimits/read" {
			return raw, nil
		}
		calls++
		return map[string]any{"outcome": "reset"}, nil
	}
	state, e := m.CodexResetState()
	if e != nil {
		t.Fatal(e)
	}
	quote, e := m.PrepareCodexReset("phone-a", "credit-test", state.AccountVersion)
	if e != nil {
		t.Fatal(e)
	}
	if calls != 0 {
		t.Fatal("view/prepare consumed")
	}
	key := "00000000000000000000000000000001"
	if _, e = m.ConsumeCodexReset("phone-a", quote.Intent, key, false); e == nil {
		t.Fatal("unconfirmed consume accepted")
	}
	if _, e = m.ConsumeCodexReset("phone-b", quote.Intent, key, true); e == nil {
		t.Fatal("wrong device accepted")
	}
	raw["_resetUser"] = "other@example.invalid"
	if _, e = m.ConsumeCodexReset("phone-a", quote.Intent, key, true); e == nil {
		t.Fatal("changed user in same account accepted")
	}
	raw["_resetUser"] = "fake@example.invalid"
	if calls != 0 {
		t.Fatal("guard wrote native API")
	}
	result, e := m.ConsumeCodexReset("phone-a", quote.Intent, key, true)
	if e != nil || result.Outcome != "reset" || calls != 1 {
		t.Fatal("explicit consume failed", e)
	}
	result, e = m.ConsumeCodexReset("phone-a", quote.Intent, key, true)
	if e != nil || result.Outcome != "reset" || calls != 1 {
		t.Fatal("duplicate consumed twice")
	}
	if _, e = m.ConsumeCodexReset("phone-a", quote.Intent, key+"x", true); e == nil {
		t.Fatal("new key replay accepted")
	}
}
func TestCodexResetUnknownRetrySurvivesManagerRestartWithSameKey(t *testing.T) {
	m := connectorManager(t)
	raw := resetFixture()
	state := normalizeResets(raw)
	keys := []string{}
	f := func(_ context.Context, method string, params map[string]any) (map[string]any, error) {
		if method == "account/rateLimits/read" {
			return raw, nil
		}
		keys = append(keys, str(params["idempotencyKey"]))
		if len(keys) == 1 {
			return nil, errors.New("lost ack")
		}
		return map[string]any{"outcome": "alreadyRedeemed"}, nil
	}
	m.resetRPC = f
	q, e := m.PrepareCodexReset("phone", "credit-test", state.AccountVersion)
	if e != nil {
		t.Fatal(e)
	}
	key := UUID()
	r, e := m.ConsumeCodexReset("phone", q.Intent, key, true)
	if e != nil || r.Outcome != "unknown" {
		t.Fatal("unknown outcome falsely success")
	}
	if _, e = m.PrepareCodexReset("phone", "credit-test", state.AccountVersion); e == nil {
		t.Fatal("pending attempt replaced")
	}
	// Fresh quota may be low because the first request succeeded: do not discard
	// the original idempotency key or claim the reset was not applied.
	obj(obj(raw["rateLimitsByLimitId"])["codex"])["primary"] = map[string]any{"usedPercent": float64(0), "windowDurationMins": float64(300)}
	restart := &Manager{Store: m.Store, resetRPC: f}
	r, e = restart.ConsumeCodexReset("phone", q.Intent, key, true)
	if e != nil || r.Outcome != "alreadyRedeemed" || len(keys) != 2 || keys[0] != keys[1] {
		t.Fatal("restart lost attempt identity", e)
	}
}
func TestCodexResetExpiryAndQuotaRecoveryNeverConsume(t *testing.T) {
	m := connectorManager(t)
	raw := resetFixture()
	calls := 0
	m.resetRPC = func(_ context.Context, method string, _ map[string]any) (map[string]any, error) {
		if method != "account/rateLimits/read" {
			calls++
		}
		return raw, nil
	}
	s := normalizeResets(raw)
	q, e := m.PrepareCodexReset("p", "credit-test", s.AccountVersion)
	if e != nil {
		t.Fatal(e)
	}
	var intent resetIntent
	m.Store.Get("agentconfig", "reset-intent/p", &intent)
	intent.Expires = time.Now().Add(-time.Minute).Unix()
	m.Store.Put("agentconfig", "reset-intent/p", intent)
	if _, e = m.ConsumeCodexReset("p", q.Intent, UUID(), true); e == nil {
		t.Fatal("expired confirmation accepted")
	}
	q, e = m.PrepareCodexReset("p", "credit-test", s.AccountVersion)
	if e != nil {
		t.Fatal(e)
	}
	obj(obj(raw["rateLimitsByLimitId"])["codex"])["primary"] = map[string]any{"usedPercent": float64(10), "windowDurationMins": float64(300)}
	r, e := m.ConsumeCodexReset("p", q.Intent, UUID(), true)
	if e != nil || r.Outcome != "nothingToReset" || calls != 0 {
		t.Fatal("fresh quota recovery consumed")
	}
}
func TestCodexResetHistoryProjectionAndCursor(t *testing.T) {
	m := &Manager{}
	calls := 0
	m.resetRPC = func(_ context.Context, method string, _ map[string]any) (map[string]any, error) {
		if method != "account/rateLimits/read" {
			t.Fatal("read consumed")
		}
		return resetFixture(), nil
	}
	m.resetHistoryRead = func(_ context.Context, account, user, cursor string) (map[string]any, error) {
		calls++
		if account != "fake-account" || user != "fake@example.invalid" || cursor != "cursor-1" {
			t.Fatal("history scope/cursor lost")
		}
		return map[string]any{"events": []any{map[string]any{"id": "a", "kind": "granted", "occurred_at": "2026-10-10T08:00:00Z", "raw_secret": "must-not-leak"}, map[string]any{"id": "b", "kind": "future-kind", "occurred_at": "bad"}, map[string]any{"id": "c", "kind": "used", "occurred_at": "2026-10-11T08:00:00Z"}}, "next_cursor": "next-2"}, nil
	}
	p, e := m.CodexResetHistory("cursor-1")
	if e != nil || calls != 1 || len(p.Events) != 3 || p.Events[1].Kind != "unknown" || p.Events[2].Kind != "redeemed" || p.Next != "next-2" {
		t.Fatal("history projection incorrect", e)
	}
	b, _ := json.Marshal(p)
	if contains(string(b), "must-not-leak") {
		t.Fatal("raw history leaked")
	}
}
func TestRealCodexResetReadOnly(t *testing.T) {
	if os.Getenv("AGENTDECK_REAL_RESET_READS") != "1" {
		t.Skip("explicit read-only account acceptance")
	}
	m := &Manager{}
	s, e := m.CodexResetState()
	if e != nil {
		t.Fatal(e)
	}
	p, e := m.CodexResetHistory("")
	if e != nil {
		t.Fatal(e)
	}
	if s.Available == nil {
		t.Fatal("available count not returned")
	}
	used, unknown := 0, 0
	for _, event := range p.Events {
		if event.Kind == "redeemed" {
			used++
		}
		if event.Kind == "unknown" {
			unknown++
		}
	}
	t.Logf("Read-only: available=%v details=%d history=%d used=%d unknown=%d; no consume operation", *s.Available, len(s.Credits), len(p.Events), used, unknown)
}

// This executable is an isolated fixture at the front of PATH. It has no
// credentials or network calls, including when testing the mutation RPC.
func TestCodexResetNativeProtocolGuardsAndProjection(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Unix fixture executable")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.jsonl")
	fixture := `#!/usr/bin/env python3
import json,os,sys
for line in sys.stdin:
 v=json.loads(line)
 with open(os.environ['RESET_FIXTURE_LOG'],'a') as f:f.write(json.dumps(v)+'\n')
 method=v['method']
 if 'id' not in v:continue
 if method=='account/read':result={'account':{'email':'fake@example.invalid'}}
 elif method=='account/rateLimits/read':result={'accountId':'fake-account','rateLimitsByLimitId':{'codex':{'primary':{'usedPercent':int(os.environ['RESET_FIXTURE_USED']),'windowDurationMins':300}}}}
 elif method=='account/rateLimitResetCredit/consume':result={'outcome':'reset'}
 else:result={}
 print(json.dumps({'id':v['id'],'result':result}),flush=True)
`
	if e := os.WriteFile(filepath.Join(dir, "codex"), []byte(fixture), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("RESET_FIXTURE_LOG", log)
	t.Setenv("RESET_FIXTURE_USED", "95")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	state, e := nativeResetCall(ctx, "account/rateLimits/read", nil)
	if e != nil {
		t.Fatal(e)
	}
	account := resetAccount(state)
	if account == "" {
		t.Fatal("missing account binding")
	}
	if _, e = nativeResetCall(ctx, "account/rateLimitResetCredit/consume", map[string]any{"_accountVersion": "different"}); e == nil {
		t.Fatal("changed identity consumed")
	}
	t.Setenv("RESET_FIXTURE_USED", "20")
	params := map[string]any{"_accountVersion": account, "idempotencyKey": "fixture-original-id", "creditId": "fixture-card"}
	v, e := nativeResetCall(ctx, "account/rateLimitResetCredit/consume", params)
	if e != nil || str(v["outcome"]) != "nothingToReset" {
		t.Fatal("fresh core guard failed", e)
	}
	params["_retry"] = true
	v, e = nativeResetCall(ctx, "account/rateLimitResetCredit/consume", params)
	if e != nil || str(v["outcome"]) != "reset" {
		t.Fatal("same-key retry failed", e)
	}
	data, e := os.ReadFile(log)
	if e != nil {
		t.Fatal(e)
	}
	writes := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var v map[string]any
		if e = json.Unmarshal([]byte(line), &v); e != nil {
			t.Fatal(e)
		}
		if v["method"] == "account/rateLimitResetCredit/consume" {
			writes++
			p := obj(v["params"])
			if len(p) != 2 || p["idempotencyKey"] != "fixture-original-id" || p["creditId"] != "fixture-card" {
				t.Fatal("native parameters changed or internal metadata leaked")
			}
		}
	}
	if writes != 1 {
		t.Fatal("read/guard invoked mutation", writes)
	}
}
