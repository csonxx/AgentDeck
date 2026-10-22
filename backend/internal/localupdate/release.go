// Package localupdate distributes workstation executables, never agent data.
// A gateway can advertise a release but cannot authorize code: Local verifies
// an Ed25519 publisher key installed through the owner's initial installer.
package localupdate

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

const Protocol = 1
const Schema = 1

// These are set by the reproducible release build; source builds do not update
// themselves until installed with an explicit publisher key and build number.
var Version = "dev"
var Build = "0"

type Release struct {
	Version     string `json:"version"`
	Build       int64  `json:"build"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
	URL         string `json:"url"`
	Notes       string `json:"notes"`
	Protocol    int    `json:"protocol"`
	StateSchema int    `json:"state_schema"`
	KeyID       string `json:"key_id"`
	Signature   string `json:"signature"`
}

var versionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
var keyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func (r Release) Validate() error {
	u, err := url.Parse(r.URL)
	h, he := hex.DecodeString(r.SHA256)
	s, se := base64.StdEncoding.DecodeString(r.Signature)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" ||
		he != nil || len(h) != 32 || r.SHA256 != strings.ToLower(r.SHA256) || se != nil || len(s) != ed25519.SignatureSize ||
		!versionPattern.MatchString(r.Version) || r.Build < 1 || r.Bytes < 1 || r.Bytes > 512<<20 || len(r.Notes) > 12000 ||
		!keyPattern.MatchString(r.KeyID) || r.Protocol != Protocol || r.StateSchema != Schema ||
		(r.OS != "darwin" && r.OS != "windows" && r.OS != "linux") || (r.Arch != "amd64" && r.Arch != "arm64") {
		return errors.New("invalid workstation release")
	}
	return nil
}

// The fixed Go struct is the canonical signed payload. Include every field
// except the signature and domain-separate it from any other signed object.
func (r Release) payload() []byte {
	r.Signature = ""
	b, _ := json.Marshal(r)
	return append([]byte("agentdeck-local-release-v1\n"), b...)
}
func (r *Release) Sign(key ed25519.PrivateKey) error {
	if len(key) != ed25519.PrivateKeySize {
		return errors.New("invalid signing key")
	}
	r.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, r.payload()))
	return r.Validate()
}
func (r Release) Verify(keys map[string]string) error {
	if err := r.Validate(); err != nil {
		return err
	}
	k, err := base64.StdEncoding.DecodeString(keys[r.KeyID])
	s, _ := base64.StdEncoding.DecodeString(r.Signature)
	if err != nil || len(k) != ed25519.PublicKeySize || !ed25519.Verify(k, r.payload(), s) {
		return errors.New("workstation release signature rejected")
	}
	return nil
}

func GatewayURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && !strings.Contains(u.Path, "..")
}
