package agent

import (
	"agentdeck/internal/store"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeGenerationWhitelistsAndSeparatesSpeedFromEffort(t *testing.T) {
	for _, tt := range []struct{ agent, record, model, effort, speed string }{
		{"codex", `{"type":"turn_context","payload":{"model":"gpt-test","effort":"high","service_tier":"priority","developer_instructions":"private"}}`, "gpt-test", "high", "fast"},
		{"claude", `{"type":"assistant","effort":"max","fastMode":true,"message":{"model":"claude-opus-test","content":"private"}}`, "claude-opus-test", "max", "fast"},
		{"claude", `{"type":"assistant","message":{"model":"<synthetic>"}}`, "", "", ""},
		{"claude", `{"type":"system","subtype":"init","model":"claude-native-init"}`, "claude-native-init", "", ""},
	} {
		var d map[string]any
		json.Unmarshal([]byte(tt.record), &d)
		v := nativeSettings(d, tt.agent)
		if v.Model != tt.model || v.Effort != tt.effort || v.Speed != tt.speed {
			t.Fatal(v)
		}
		raw, _ := json.Marshal(v)
		if string(raw) == "" {
			t.Fatal("empty schema")
		}
	}
	p := map[string]any{"effort": "max"}
	applySpeed(p, TurnOptions{Speed: "fast"})
	if p["effort"] != "max" || p["serviceTier"] != "priority" {
		t.Fatal(p)
	}
	applySpeed(p, TurnOptions{Speed: "standard"})
	if p["serviceTier"] != "default" {
		t.Fatal(p)
	}
	v := desktopSettings(map[string]any{"latestCollaborationMode": map[string]any{"settings": map[string]any{"model": "gpt-native", "reasoning_effort": "ultra"}}, "serviceTier": "priority"})
	if v.Model != "gpt-native" || v.Effort != "ultra" || v.Speed != "fast" || v.Source != "live" {
		t.Fatal(v)
	}
}
func TestFollowerSettingsReadsExistingChatAndIgnoresPartialTail(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "native.jsonl")
	os.WriteFile(path, []byte("{\"type\":\"assistant\",\"effort\":\"high\",\"message\":{\"model\":\"claude-first\"}}\n{\"type\":\"assistant\",\"message\":{\"model\":\"claude-current\"}}\n{\"type\":"), 0600)
	f := &Follower{Roots: []Root{{Path: root, Agent: "claude"}}, states: map[string]*State{path: {Session: "native"}}}
	got, e := f.Settings("native")
	if e != nil || got.Model != "claude-current" || got.Effort != "" || got.Source != "last_turn" {
		t.Fatal(got, e)
	}
	if _, e = f.Settings("other"); e == nil {
		t.Fatal("unrelated chat settings")
	}
}
func TestFastValidationRequiresAdvertisedModelCapability(t *testing.T) {
	// Reuse an actual encrypted test store; no CLI/inference is started.
	root := t.TempDir()
	s, e := store.New(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	m := &Manager{Store: s, Roots: []string{root}, catalogs: map[string]modelEntry{}}
	s.Put("sessions", "s", store.Session{ID: "s", Agent: "claude", Project: root})
	m.catalogs["claude\n"+root] = modelEntry{at: time.Now(), choices: []ModelChoice{{ID: "opus", FastTier: "fast", Efforts: []string{"max"}}, {ID: "sonnet", Efforts: []string{"high"}}}}
	if e = m.validateOptions("s", "claude", TurnOptions{Model: "opus", Effort: "max", Speed: "fast"}); e != nil {
		t.Fatal(e)
	}
	if e = m.validateOptions("s", "claude", TurnOptions{Model: "sonnet", Effort: "high", Speed: "fast"}); e == nil {
		t.Fatal("unsupported fast enabled")
	}
	if e = m.validateOptions("s", "claude", TurnOptions{Model: "opus", Speed: "invalid"}); e == nil {
		t.Fatal("invalid tier enabled")
	}
}
