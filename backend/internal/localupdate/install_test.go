package localupdate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestManagedInstallKeepsExistingVaultAndNeverReplacesInstall(t *testing.T) {
	oldVersion, oldBuild := Version, Build
	Version, Build = "0.4.47", "9064"
	defer func() { Version, Build = oldVersion, oldBuild }()
	dir := t.TempDir()
	sentinel := []byte("synthetic encrypted vault and pairings")
	if err := os.WriteFile(filepath.Join(dir, "vault.db"), sentinel, 0600); err != nil {
		t.Fatal(err)
	}
	_, keys := signed(t, "https://example.com/bin", []byte("fixture"), 1)
	source, _ := os.Executable()
	cfg := Config{Gateway: "https://example.com/agentdeck", Keys: keys, Path: "/owner/agent/bin", Args: []string{"--relay"}, Auto: true}
	if err := Install(dir, source, cfg); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "vault.db"))
	if err != nil || !bytes.Equal(after, sentinel) {
		t.Fatal("vault changed")
	}
	var state State
	if err = readJSON(filepath.Join(root(dir), "state.json"), &state); err != nil || !validInstalled(state.Active) {
		t.Fatal("invalid bootstrap state", err)
	}
	var saved Config
	if err = readJSON(filepath.Join(root(dir), "config.json"), &saved); err != nil || saved.Path != cfg.Path || !saved.Auto {
		t.Fatal("background agent path not preserved", err)
	}
	if err = verifyFile(binary(dir, state.Active), state.Active); err != nil {
		t.Fatal(err)
	}
	if Install(dir, source, cfg) == nil {
		t.Fatal("existing installation replaced")
	}
}
