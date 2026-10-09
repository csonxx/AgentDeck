package agent

import (
	"agentdeck/internal/store"
	"encoding/json"
	"strings"
	"testing"
)

func TestQuotedCredentialsDoNotLeakFromProviderToolOutput(t *testing.T) {
	m := connectorManager(t)
	ss := newExternalSession("fixture", "qoder", m.Roots[0], "acp")
	m.Store.Put("sessions", ss.ID, ss)
	raw := `{"password":"fixture secret with spaces","clientSecret":"fixture-client-value","apiKey":"fixture\\escaped","refresh_token":"fixture-refresh-value","safe":"visible"}`
	m.emitExternal(ss, store.Event{Kind: "tool", Item: "fixture-tool", Text: raw, Mode: "snapshot"})
	evs, e := m.Store.Events(ss.ID, 0, 10)
	if e != nil || len(evs) != 1 {
		t.Fatal("provider output missing")
	}
	for _, secret := range []string{"fixture secret", "fixture-client-value", "fixture\\escaped", "fixture-refresh-value"} {
		if strings.Contains(evs[0].Text, secret) {
			t.Fatal("quoted credential leaked")
		}
	}
	var value map[string]any
	if json.Unmarshal([]byte(evs[0].Text), &value) != nil {
		t.Fatal("redaction broke tool JSON formatting")
	}
	if value["safe"] != "visible" || value["password"] != "[REDACTED]" {
		t.Fatal("redaction changed unrelated presentation")
	}
}
