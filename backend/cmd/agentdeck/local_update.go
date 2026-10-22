package main

import (
	"agentdeck/internal/localipc"
	"agentdeck/internal/localupdate"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

func localCommand(args []string) (bool, error) {
	cmd := args[0]
	if cmd != "version" && cmd != "local-install" && cmd != "local-run" && cmd != "local-update-status" && cmd != "local-release-keygen" && cmd != "local-release-sign" && cmd != "gateway-publish-local-release" {
		return false, nil
	}
	if cmd == "version" {
		fmt.Printf("AgentDeck %s build %s (%s/%s)\n", localupdate.Version, localupdate.Build, runtime.GOOS, runtime.GOARCH)
		return true, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return true, err
	}
	f := flag.NewFlagSet(cmd, flag.ContinueOnError)
	dir := f.String("data", filepath.Join(home, ".agentdeck"), "workstation data directory")
	gatewayURL := f.String("gateway-url", "", "HTTPS gateway including its path prefix")
	pub := f.String("publisher-key", "", "pinned Ed25519 key as key-id:base64-public-key")
	noStart := f.Bool("no-start", false, "stage install without registering or starting a login service")
	keyPath := f.String("key", "", "private publisher signing key path (never distribute)")
	keyID := f.String("key-id", "", "publisher key ID")
	out := f.String("out", "", "output manifest or new signing key path")
	bin := f.String("binary", "", "binary to publish")
	version := f.String("version", "", "release version")
	build := f.Int64("build", 0, "monotonic release build number")
	goos := f.String("os", runtime.GOOS, "target OS")
	arch := f.String("arch", runtime.GOARCH, "target architecture")
	url := f.String("url", "", "immutable HTTPS binary download URL")
	notes := f.String("notes-file", "", "UTF-8 release notes path")
	manifest := f.String("manifest", "", "signed workstation release manifest")
	if err := f.Parse(args[1:]); err != nil {
		return true, err
	}
	switch cmd {
	case "local-release-keygen":
		if *out == "" || *keyID == "" {
			return true, errors.New("--out and --key-id required")
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return true, err
		}
		if err = localipc.PrivateDir(filepath.Dir(*out)); err != nil {
			return true, err
		}
		file, err := os.OpenFile(*out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return true, err
		}
		_, err = file.WriteString(base64.StdEncoding.EncodeToString(priv))
		ce := file.Close()
		if err == nil {
			err = ce
		}
		if err == nil {
			fmt.Printf("Publisher public key: %s:%s\n", *keyID, base64.StdEncoding.EncodeToString(pub))
		}
		return true, err
	case "local-release-sign":
		raw, err := os.ReadFile(*keyPath)
		if err != nil {
			return true, err
		}
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil {
			return true, errors.New("invalid signing key")
		}
		file, err := os.Open(*bin)
		if err != nil {
			return true, err
		}
		defer file.Close()
		h := sha256.New()
		size, err := io.Copy(h, file)
		if err != nil {
			return true, err
		}
		text := ""
		if *notes != "" {
			b, err := os.ReadFile(*notes)
			if err != nil {
				return true, err
			}
			text = string(b)
		}
		r := localupdate.Release{Version: *version, Build: *build, OS: *goos, Arch: *arch, Bytes: size, SHA256: hex.EncodeToString(h.Sum(nil)), URL: *url, Notes: text, KeyID: *keyID, Protocol: localupdate.Protocol, StateSchema: localupdate.Schema}
		if err = r.Sign(ed25519.PrivateKey(key)); err != nil {
			return true, err
		}
		b, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return true, err
		}
		return true, os.WriteFile(*out, append(b, '\n'), 0600)
	case "gateway-publish-local-release":
		file, err := os.Open(*manifest)
		if err != nil {
			return true, err
		}
		defer file.Close()
		var r localupdate.Release
		dec := json.NewDecoder(io.LimitReader(file, 20<<10))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&r); err != nil {
			return true, err
		}
		if err = r.Validate(); err != nil {
			return true, err
		}
		_, err = adminRequest(*dir, "POST", "/releases/local", r)
		return true, err
	case "local-install":
		absolute, e := filepath.Abs(*dir)
		if e != nil {
			return true, e
		}
		*dir = absolute
		id, key, ok := strings.Cut(*pub, ":")
		if !ok {
			return true, errors.New("--publisher-key key-id:base64-public-key required")
		}
		// Installer options never override the authoritative data directory or
		// turn the child into a public Gateway. Native agent policy stays intact.
		serveArgs := f.Args()
		if len(serveArgs) == 0 {
			serveArgs = []string{"--relay", "--project-roots", home, "--media-roots", home}
			for _, source := range []struct{ flag, path string }{{"--codex-sessions", filepath.Join(home, ".codex", "sessions")}, {"--claude-sessions", filepath.Join(home, ".claude", "projects")}} {
				if st, e := os.Stat(source.path); e == nil && st.IsDir() {
					serveArgs = append(serveArgs, source.flag, source.path)
				}
			}
			if path := filepath.Join(*dir, "agents.json"); fileExists(path) {
				serveArgs = append(serveArgs, "--agents-config", path)
			}
			if runtime.GOOS != "windows" {
				p := filepath.Join(home, ".codex", "ipc", "ipc.sock")
				if _, err := os.Stat(p); err == nil {
					serveArgs = append(serveArgs, "--codex-desktop-socket", p)
				}
			}
		}
		for _, arg := range serveArgs {
			if strings.HasPrefix(arg, "--data") || strings.HasPrefix(arg, "-data") || strings.HasPrefix(arg, "--role") || strings.HasPrefix(arg, "-role") {
				return true, errors.New("installer serve arguments cannot override data or role")
			}
		}
		c := localipc.Client(*dir, "control")
		response, e := c.Get("http://unix/update/health")
		c.CloseIdleConnections()
		if e == nil {
			response.Body.Close()
			return true, errors.New("stop the existing workstation before first managed installation")
		}
		source, err := os.Executable()
		if err != nil {
			return true, err
		}
		if err = localupdate.Install(*dir, source, localupdate.Config{Gateway: *gatewayURL, Path: os.Getenv("PATH"), Keys: map[string]string{id: key}, Args: serveArgs, Auto: true}); err != nil {
			return true, err
		}
		if !*noStart {
			if err = localupdate.RegisterService(*dir); err != nil {
				return true, fmt.Errorf("files installed; login service setup failed: %w; run %s local-run --data %s", err, localupdate.Launcher(*dir), *dir)
			}
		}
		fmt.Printf("Installed S-LOCAL %s. Console: http://127.0.0.1:8787/\n", localupdate.Version)
		return true, nil
	case "local-run":
		d, err := filepath.Abs(*dir)
		if err != nil {
			return true, err
		}
		s, err := localupdate.Open(d)
		if err != nil {
			return true, err
		}
		if err = localipc.PrivateDir(filepath.Join(d, "logs")); err != nil {
			return true, err
		}
		stdout, err := os.OpenFile(filepath.Join(d, "logs", "local.stdout.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return true, err
		}
		defer stdout.Close()
		stderr, err := os.OpenFile(filepath.Join(d, "logs", "local.stderr.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return true, err
		}
		defer stderr.Close()
		oldOut, oldErr := os.Stdout, os.Stderr
		os.Stdout, os.Stderr = stdout, stderr
		defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
		log.SetOutput(stderr)
		slog.SetDefault(slog.New(slog.NewTextHandler(stderr, nil)))
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		err = s.Run(ctx)
		if err != nil {
			slog.Error("local_supervisor_stopped", "error", err)
		}
		return true, err
	case "local-update-status":
		c := localipc.Client(*dir, "updates")
		defer c.CloseIdleConnections()
		res, err := c.Get("http://unix/status")
		if err != nil {
			return true, err
		}
		defer res.Body.Close()
		_, err = io.Copy(os.Stdout, io.LimitReader(res.Body, 64<<10))
		return true, err
	}
	return true, exec.ErrNotFound
}

func fileExists(path string) bool { s, err := os.Stat(path); return err == nil && s.Mode().IsRegular() }
