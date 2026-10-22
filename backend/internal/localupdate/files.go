package localupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"agentdeck/internal/localipc"
)

type Config struct {
	Gateway string            `json:"gateway"`
	Path    string            `json:"path,omitempty"`
	Keys    map[string]string `json:"publisher_keys"`
	Args    []string          `json:"serve_args"`
	Auto    bool              `json:"auto_update"`
}
type State struct {
	Active   Release  `json:"active"`
	Previous *Release `json:"previous,omitempty"`
	Pending  *Release `json:"pending,omitempty"`
	Trial    bool     `json:"trial"`
	Highest  int64    `json:"highest_build"`
	Rejected int64    `json:"rejected_build,omitempty"`
	Problem  string   `json:"problem,omitempty"`
}

func root(dir string) string { return filepath.Join(dir, "updates") }
func binary(dir string, r Release) string {
	name := "agentdeck"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(root(dir), "versions", r.SHA256, name)
}
func readJSON(path string, out any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 64<<10))
	d.DisallowUnknownFields()
	return d.Decode(out)
}
func saveJSON(path string, value any) error {
	if err := localipc.PrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	// os.Rename replaces an existing regular file on all supported Go targets.
	return os.Rename(name, path)
}
func digest(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}
func verifyFile(path string, r Release) error {
	s, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !s.Mode().IsRegular() || s.Size() != r.Bytes {
		return errors.New("workstation binary size/type rejected")
	}
	h, _, err := digest(path)
	if err != nil {
		return err
	}
	if h != r.SHA256 {
		return errors.New("workstation binary checksum rejected")
	}
	return nil
}

// Install only bootstraps an owner-trusted executable. Future bytes must pass
// the separately pinned publisher signature. Existing installs are preserved.
func Install(dir, source string, cfg Config) error {
	if !GatewayURL(cfg.Gateway) || len(cfg.Keys) == 0 {
		return errors.New("HTTPS gateway and publisher keys required")
	}
	for id, key := range cfg.Keys {
		if !keyPattern.MatchString(id) || !validPublicKey(key) {
			return errors.New("invalid publisher key")
		}
	}
	n, err := strconv.ParseInt(Build, 10, 64)
	if err != nil || n < 1 || !versionPattern.MatchString(Version) {
		return errors.New("install a versioned release build")
	}
	if _, err = os.Stat(filepath.Join(root(dir), "state.json")); !os.IsNotExist(err) {
		return errors.New("workstation already installed; use automatic updates")
	}
	if err = localipc.PrivateDir(dir); err != nil {
		return err
	}
	h, size, err := digest(source)
	if err != nil {
		return err
	}
	r := Release{Version: Version, Build: n, OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: h, Bytes: size, Protocol: Protocol, StateSchema: Schema}
	for _, path := range []string{binary(dir, r), Launcher(dir)} {
		if err = copyBinary(source, path); err != nil {
			return err
		}
	}
	if err = saveJSON(filepath.Join(root(dir), "config.json"), cfg); err != nil {
		return err
	}
	return saveJSON(filepath.Join(root(dir), "state.json"), State{Active: r, Highest: n})
}
func Launcher(dir string) string {
	name := "agentdeck"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, "bin", name)
}
func copyBinary(source, target string) error {
	if err := localipc.PrivateDir(filepath.Dir(target)); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	f, err := os.CreateTemp(filepath.Dir(target), ".binary-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = io.Copy(f, in)
	if err == nil {
		err = f.Chmod(0700)
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), target)
}
