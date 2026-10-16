package agent

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agentdeck/internal/store"
)

type NativeReadState struct {
	Available bool            `json:"available"`
	Writable  bool            `json:"writable"`
	States    map[string]bool `json:"states"`
}

// Read the account- and execution-host-scoped Desktop ledger. Never open chats
// or write native state. External native writers are currently unsupported.
func (m *Manager) CodexReadState(sessions []store.Session) NativeReadState {
	m.readStateMu.Lock()
	defer m.readStateMu.Unlock()
	if time.Since(m.readStateChecked) > time.Second {
		m.readStateChecked = time.Now()
		m.readStateIDs, m.readStateAvailable = readCodexUnread(m.CodexReadHome)
	}
	result := NativeReadState{Available: m.readStateAvailable, States: map[string]bool{}}
	if !result.Available {
		return result
	}
	for _, s := range sessions {
		if s.Agent == "codex" && (!s.Managed || s.Source == "desktop") {
			result.States[s.ID] = m.readStateIDs[s.ID]
		}
	}
	return result
}

func privateReadJSON(path string, limit int64, v any) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || !desktopOwned(info) || info.Size() > limit {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	return err == nil && int64(len(raw)) <= limit && json.Unmarshal(raw, v) == nil
}

func readCodexUnread(home string) (map[string]bool, bool) {
	if home == "" {
		return nil, false
	}
	var auth struct {
		Mode   string `json:"auth_mode"`
		Tokens struct {
			Access string `json:"access_token"`
		} `json:"tokens"`
	}
	if !privateReadJSON(filepath.Join(home, "auth.json"), 256<<10, &auth) || auth.Mode != "chatgpt" {
		return nil, false
	}
	// Identity claims stay inside the owner process. No token, account, or native
	// global state is sent to the phone; credential refresh is never performed.
	parts := strings.Split(auth.Tokens.Access, ".")
	if len(parts) != 3 {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var claims struct {
		Auth struct {
			Account         string `json:"chatgpt_account_id"`
			FallbackAccount string `json:"account_id"`
			User            string `json:"user_id"`
			FallbackUser    string `json:"chatgpt_user_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(raw, &claims) != nil {
		return nil, false
	}
	account, user := claims.Auth.Account, claims.Auth.User
	if account == "" {
		account = claims.Auth.FallbackAccount
	}
	if user == "" {
		user = claims.Auth.FallbackUser
	}
	if account == "" || user == "" {
		return nil, false
	}
	identity, _ := json.Marshal([]any{"chatgpt", account, user})
	sum := sha256.Sum256(identity)
	identityKey := hex.EncodeToString(sum[:])
	hostSum := sha256.Sum256([]byte(`["local","local",null]`))
	hostKey := "local:" + hex.EncodeToString(hostSum[:])
	var ledger struct {
		Version    int                            `json:"version"`
		Identities map[string]map[string][]string `json:"unreadByIdentity"`
	}
	if !privateReadJSON(filepath.Join(home, ".codex-global-state.json"), 16<<20, &ledger) || ledger.Version != 1 {
		return nil, false
	}
	hosts, ok := ledger.Identities[identityKey]
	if !ok {
		return nil, false
	}
	ids, ok := hosts[hostKey]
	if !ok {
		return nil, false
	}
	out := map[string]bool{}
	for _, id := range ids {
		if len(id) <= 128 {
			out[id] = true
		}
	}
	return out, true
}
