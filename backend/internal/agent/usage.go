package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type UsageWindow struct {
	Label   string  `json:"label"`
	Used    float64 `json:"used_percent"`
	Minutes int64   `json:"window_minutes,omitempty"`
	Resets  string  `json:"resets_at,omitempty"`
}
type AccountUsage struct {
	Agent   string        `json:"agent"`
	Source  string        `json:"source"`
	AsOf    string        `json:"as_of"`
	Stale   bool          `json:"stale"`
	Windows []UsageWindow `json:"windows"`
	readAt  time.Time
}

// AccountUsage queries native control channels only. No prompt/inference is
// submitted and no credentials or raw account metadata cross the wire.
func (m *Manager) AccountUsage(agent string) (AccountUsage, error) {
	if agent != "codex" && agent != "claude" {
		return AccountUsage{}, errors.New("此 Agent 暂不提供用量查询")
	}
	m.usageMu.Lock()
	defer m.usageMu.Unlock()
	if u, ok := m.usageCache[agent]; ok && time.Since(u.readAt) < 30*time.Second {
		return u, nil
	}
	u, err := m.readAccountUsage(agent)
	if agent == "claude" && err != nil && m.ClaudeUsageFile != "" {
		u, err = readClaudeUsageCache(m.ClaudeUsageFile)
	}
	if err != nil {
		return AccountUsage{}, errors.New("暂时无法获取账号额度，请检查电脑端登录状态或稍后重试")
	}
	u.readAt = time.Now()
	if m.usageCache == nil {
		m.usageCache = map[string]AccountUsage{}
	}
	m.usageCache[agent] = u
	return u, nil
}

func (m *Manager) readAccountUsage(agent string) (AccountUsage, error) {
	ctx := m.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	args := []string{"app-server"}
	if agent == "claude" {
		args = []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--no-session-persistence", "--permission-mode", "default", "--settings", `{"disableAllHooks":true}`, "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`}
	}
	cmd := exec.CommandContext(ctx, agent, args...)
	// Use the owner's home, not a project: usage is account-wide and must not
	// execute project hooks or load a chat history.
	cmd.Dir, _ = os.UserHomeDir()
	cmd.Stderr = io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		return AccountUsage{}, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return AccountUsage{}, err
	}
	if err = cmd.Start(); err != nil {
		return AccountUsage{}, err
	}
	defer func() { in.Close(); cmd.Process.Kill(); cmd.Wait() }()
	scan := bufio.NewScanner(out)
	scan.Buffer(make([]byte, 64<<10), 8<<20)
	call := func(method string, params map[string]any) (map[string]any, error) {
		id := UUID()
		v := map[string]any{"id": id, "method": method, "params": params}
		if agent == "claude" {
			v = map[string]any{"type": "control_request", "request_id": id, "request": params}
		}
		if err := json.NewEncoder(in).Encode(v); err != nil {
			return nil, err
		}
		for scan.Scan() {
			var d map[string]any
			if json.Unmarshal(scan.Bytes(), &d) != nil {
				continue
			}
			if agent == "codex" && str(d["id"]) == id {
				if d["error"] != nil {
					return nil, errors.New("usage unavailable")
				}
				return obj(d["result"]), nil
			}
			if agent == "claude" && str(d["type"]) == "control_response" {
				r := obj(d["response"])
				if str(r["request_id"]) == id {
					if r["subtype"] == "error" {
						return nil, errors.New("usage unavailable")
					}
					return obj(r["response"]), nil
				}
			}
		}
		return nil, errors.New("usage unavailable")
	}
	init := map[string]any{"clientInfo": map[string]any{"name": "agentdeck", "version": "0.4.1"}}
	if agent == "claude" {
		init = map[string]any{"subtype": "initialize", "hooks": nil}
	}
	if _, err = call("initialize", init); err != nil {
		return AccountUsage{}, err
	}
	method, params := "account/rateLimits/read", map[string]any{}
	if agent == "codex" {
		if err = json.NewEncoder(in).Encode(map[string]any{"method": "initialized"}); err != nil {
			return AccountUsage{}, err
		}
	} else {
		method = ""
		params = map[string]any{"subtype": "get_usage", "skip_behaviors": true}
	}
	r, err := call(method, params)
	if err != nil {
		return AccountUsage{}, err
	}
	return normalizeUsage(agent, r, time.Now())
}

func normalizeUsage(agent string, r map[string]any, now time.Time) (AccountUsage, error) {
	u := AccountUsage{Agent: agent, Source: agent + "-cli", AsOf: now.UTC().Format(time.RFC3339), Windows: []UsageWindow{}}
	add := func(label string, used any, minutes int64, reset string) {
		v, ok := used.(float64)
		if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return
		}
		if reset != "" {
			if t, err := time.Parse(time.RFC3339Nano, reset); err == nil {
				reset = t.UTC().Format(time.RFC3339)
			} else {
				reset = ""
			}
		}
		label = strings.TrimSpace(Redact(label))
		if len([]rune(label)) > 100 {
			label = string([]rune(label)[:100])
		}
		u.Windows = append(u.Windows, UsageWindow{Label: label, Used: v, Minutes: minutes, Resets: reset})
	}
	if agent == "codex" {
		buckets := obj(r["rateLimitsByLimitId"])
		if len(buckets) == 0 {
			buckets = map[string]any{"codex": r["rateLimits"]}
		}
		keys := []string{}
		for k := range buckets {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b := obj(buckets[k])
			label := str(b["limitName"])
			if label == "" {
				label = k
			}
			for _, key := range []string{"primary", "secondary"} {
				w := obj(b[key])
				reset := ""
				if v, ok := w["resetsAt"].(float64); ok && v > 0 {
					reset = time.Unix(int64(v), 0).UTC().Format(time.RFC3339)
				}
				minutes, _ := w["windowDurationMins"].(float64)
				add(label, w["usedPercent"], int64(minutes), reset)
			}
		}
	} else {
		if r["rate_limits_available"] != true {
			return u, errors.New("plan usage unavailable")
		}
		limits := obj(r["rate_limits"])
		for _, row := range []struct {
			key, label string
			minutes    int64
		}{{"five_hour", "5 小时额度", 300}, {"seven_day", "每周额度", 10080}, {"seven_day_opus", "Opus", 10080}, {"seven_day_sonnet", "Sonnet", 10080}, {"seven_day_oauth_apps", "OAuth apps", 10080}} {
			w := obj(limits[row.key])
			add(row.label, w["utilization"], row.minutes, str(w["resets_at"]))
		}
		for _, raw := range arr(limits["model_scoped"]) {
			w := obj(raw)
			add(str(w["display_name"]), w["utilization"], 10080, str(w["resets_at"]))
		}
	}
	if len(u.Windows) == 0 {
		return u, errors.New("plan usage unavailable")
	}
	return u, nil
}

// Older Claude /usage implementations can omit live utilization. A desktop
// cache is useful only with its original timestamp and an explicit stale label.
// Refuse mixed organizations rather than guess which account the CLI uses.
func readClaudeUsageCache(path string) (AccountUsage, error) {
	f, err := os.Open(path)
	if err != nil {
		return AccountUsage{}, err
	}
	defer f.Close()
	var d struct {
		Samples []struct {
			T   int64  `json:"t"`
			Org string `json:"org"`
			U   struct {
				FH *float64 `json:"fh"`
				SD *float64 `json:"sd"`
			} `json:"u"`
		} `json:"samples"`
	}
	decoder := json.NewDecoder(io.LimitReader(f, 8<<20))
	if err = decoder.Decode(&d); err != nil {
		return AccountUsage{}, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return AccountUsage{}, errors.New("invalid usage cache")
	}
	if len(d.Samples) == 0 {
		return AccountUsage{}, errors.New("empty usage cache")
	}
	org := d.Samples[0].Org
	last := d.Samples[0]
	for _, s := range d.Samples {
		if org == "" || s.Org != org {
			return AccountUsage{}, errors.New("ambiguous usage account")
		}
		if s.T > last.T {
			last = s
		}
	}
	if last.T <= 0 || time.UnixMilli(last.T).After(time.Now().Add(time.Minute)) {
		return AccountUsage{}, errors.New("invalid cache timestamp")
	}
	limits := map[string]any{}
	if last.U.FH != nil {
		limits["five_hour"] = map[string]any{"utilization": *last.U.FH}
	}
	if last.U.SD != nil {
		limits["seven_day"] = map[string]any{"utilization": *last.U.SD}
	}
	u, err := normalizeUsage("claude", map[string]any{"rate_limits_available": true, "rate_limits": limits}, time.UnixMilli(last.T))
	u.Source = "claude-desktop-cache"
	u.Stale = true
	return u, err
}
