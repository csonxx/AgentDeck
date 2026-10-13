package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionClaudeNeverUsesDesktopOrPeerBridge(t *testing.T) {
	m, ss, _, _, _ := inboxFixture(t)
	m.ClaudeDesktopInput = false
	if _, found, err := m.claudeInbox(ss); err != nil || found {
		t.Fatal("production still selects unsupported desktop bridge")
	}
	if err := m.checkClaudeOwner(ss.ID, 0); err == nil || err.Error() != claudeOwnerBusy {
		t.Fatal("verified original owner not reported")
	}
	// PID reuse does not make an unrelated live process own this history.
	m.ClaudeProcessSnapshot = func() (map[int]string, error) { return map[int]string{1234: "different birth"}, nil }
	if err := m.checkClaudeOwner(ss.ID, 0); err != nil {
		t.Fatal("stale owner blocked input")
	}
}

func TestClaudeOwnerRegistryDetectsSessionWithoutArgvResume(t *testing.T) {
	m, ss, record, _, _ := inboxFixture(t)
	m.ClaudeDesktopInput = false
	raw, _ := os.ReadFile(record)
	var d map[string]any
	json.Unmarshal(raw, &d)
	d["parkedJobId"] = "parked"
	raw, _ = json.Marshal(d)
	os.WriteFile(record, raw, 0600)
	if err := m.checkClaudeOwner(ss.ID, 0); err == nil {
		t.Fatal("SDK claimed process without resume argv ignored")
	}
	if err := m.checkClaudeOwner(ss.ID, 1234); err != nil {
		t.Fatal("own process blocked")
	}
	if filepath.Base(record) != "1234.json" || !strings.Contains(string(raw), ss.ID) {
		t.Fatal("invalid fixture")
	}
}
