package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func usageJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestUsageNormalizationMissingIsNotZeroAndBucketsStaySeparate(t *testing.T) {
	r := usageJSON(t, `{"accountId":"private-account","rateLimits":{"primary":{"usedPercent":99}},"rateLimitsByLimitId":{"codex":{"limitName":"Codex","primary":{"usedPercent":34,"windowDurationMins":10080,"resetsAt":1791949865}},"other":{"primary":null,"secondary":{"usedPercent":0,"windowDurationMins":300}}}}`)
	u, err := normalizeUsage("codex", r, time.Now())
	if err != nil || len(u.Windows) != 2 || u.Windows[0].Used != 34 || u.Windows[0].Resets == "" || u.Windows[1].Used != 0 {
		t.Fatal("usage normalization lost window semantics", err)
	}
	b, _ := json.Marshal(u)
	if contains(string(b), "private-account") || contains(string(b), "rateLimits") {
		t.Fatal("raw account leaked")
	}
	for _, s := range []string{`{"rateLimits":{"primary":null}}`, `{"rateLimits":{"primary":{"usedPercent":null}}}`, `{"rateLimits":{"primary":{"usedPercent":-1}}}`} {
		if _, err := normalizeUsage("codex", usageJSON(t, s), time.Now()); err == nil {
			t.Fatal("unavailable usage became zero")
		}
	}
}

func TestClaudeUsageAndStaleCache(t *testing.T) {
	u, err := normalizeUsage("claude", usageJSON(t, `{"rate_limits_available":true,"rate_limits":{"five_hour":{"utilization":3,"resets_at":"2026-10-10T12:00:00Z"},"seven_day":null,"model_scoped":[{"display_name":"Fable","utilization":7,"resets_at":null}]}}`), time.Now())
	if err != nil || len(u.Windows) != 2 || u.Windows[1].Label != "Fable" || u.Stale {
		t.Fatal("Claude model window missing", err)
	}
	path := filepath.Join(t.TempDir(), "usage.json")
	os.WriteFile(path, []byte(`{"samples":[{"t":1000,"org":"one","u":{"fh":3,"sd":96}}]}`), 0600)
	u, err = readClaudeUsageCache(path)
	if err != nil || !u.Stale || u.Source != "claude-desktop-cache" || u.AsOf != "1970-01-01T00:00:01Z" || len(u.Windows) != 2 || u.Windows[0].Resets != "" {
		t.Fatal("cache freshness or unknown reset misrepresented", err)
	}
	os.WriteFile(path, []byte(`{"samples":[{"t":1000,"org":"one","u":{"fh":3}},{"t":2000,"org":"two","u":{"fh":4}}]}`), 0600)
	if _, err = readClaudeUsageCache(path); err == nil {
		t.Fatal("mixed account cache accepted")
	}
	os.WriteFile(path, []byte(`{"samples":[{"t":1000,"org":"one","u":{"fh":0}}]} trailing`), 0600)
	if _, err = readClaudeUsageCache(path); err == nil {
		t.Fatal("invalid cache accepted")
	}
}

func TestNativeUsageControlNoInference(t *testing.T) {
	bin := t.TempDir()
	fixture, err := os.ReadFile("../../../scripts/fixture_runtime.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"codex", "claude"} {
		os.WriteFile(filepath.Join(bin, a), fixture, 0700)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := &Manager{}
	for _, a := range []string{"codex", "claude"} {
		u, err := m.AccountUsage(a)
		if err != nil || len(u.Windows) != 1 || u.Windows[0].Used != 25 {
			t.Fatalf("%s control failed: %v", a, err)
		}
		again, err := m.AccountUsage(a)
		if err != nil || again.AsOf != u.AsOf {
			t.Fatal("usage cache miss")
		}
	}
	if _, err = m.AccountUsage("other"); err == nil {
		t.Fatal("unsupported agent accepted")
	}
}
