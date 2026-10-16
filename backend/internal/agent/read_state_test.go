package agent

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agentdeck/internal/store"
)

func nativeReadFixture(t *testing.T) (string, map[string]any) {
	t.Helper()
	dir := t.TempDir()
	claims, _ := json.Marshal(map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "account", "user_id": "user"}})
	auth, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{"access_token": "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"}})
	if e := os.WriteFile(filepath.Join(dir, "auth.json"), auth, 0600); e != nil {
		t.Fatal(e)
	}
	a := sha256.Sum256([]byte(`["chatgpt","account","user"]`))
	h := sha256.Sum256([]byte(`["local","local",null]`))
	v := map[string]any{"version": 1, "unreadByIdentity": map[string]any{hex.EncodeToString(a[:]): map[string]any{"local:" + hex.EncodeToString(h[:]): []string{"original", "other-private"}}, "other-account": map[string]any{"local:other-host": []string{"foreign"}}}}
	writeReadLedger(t, dir, v)
	return dir, v
}
func writeReadLedger(t *testing.T, dir string, v map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(v)
	if e := os.WriteFile(filepath.Join(dir, ".codex-global-state.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestNativeUnreadScopesIdentityHostAndAuthorizedCatalog(t *testing.T) {
	dir, _ := nativeReadFixture(t)
	m := &Manager{CodexReadHome: dir}
	ss := []store.Session{{ID: "original", Agent: "codex", Source: "log"}, {ID: "read", Agent: "codex", Source: "desktop", Managed: true}, {ID: "phone", Agent: "codex", Source: "stream", Managed: true}, {ID: "claude", Agent: "claude"}}
	v := m.CodexReadState(ss)
	if !v.Available || v.Writable || len(v.States) != 2 || !v.States["original"] || v.States["read"] {
		t.Fatal("wrong native read projection")
	}
	before, e := os.ReadFile(filepath.Join(dir, ".codex-global-state.json"))
	if e != nil {
		t.Fatal(e)
	}
	v2 := m.CodexReadState(ss)
	v2.States["original"] = false
	if !m.CodexReadState(ss).States["original"] {
		t.Fatal("caller mutated shared state")
	}
	after, _ := os.ReadFile(filepath.Join(dir, ".codex-global-state.json"))
	if string(before) != string(after) {
		t.Fatal("native ledger modified")
	}
	// A native mark-read update is visible without any new conversation event.
	writeReadLedger(t, dir, map[string]any{"version": 1, "unreadByIdentity": map[string]any{}})
	m.readStateChecked = time.Time{}
	if m.CodexReadState(ss).Available {
		t.Fatal("absent current identity presented as read")
	}
}
func TestNativeUnreadRejectsMalformedForeignAndSymlinkedInputs(t *testing.T) {
	for _, kind := range []string{"identity", "host", "version", "auth", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir, v := nativeReadFixture(t)
			switch kind {
			case "identity":
				v["unreadByIdentity"] = map[string]any{"foreign": map[string]any{}}
			case "host":
				for k := range v["unreadByIdentity"].(map[string]any) {
					v["unreadByIdentity"].(map[string]any)[k] = map[string]any{"remote:host": []string{"original"}}
				}
			case "version":
				v["version"] = 2
			case "auth":
				os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"auth_mode":"apikey"}`), 0600)
			case "symlink":
				os.Rename(filepath.Join(dir, "auth.json"), filepath.Join(dir, "actual"))
				os.Symlink("actual", filepath.Join(dir, "auth.json"))
			}
			writeReadLedger(t, dir, v)
			if _, ok := readCodexUnread(dir); ok {
				t.Fatal("unconfirmed native state accepted")
			}
		})
	}
}
