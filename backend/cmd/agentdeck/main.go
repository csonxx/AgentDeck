package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"agentdeck/internal/agent"
	"agentdeck/internal/relay"
	"agentdeck/internal/secure"
	"agentdeck/internal/server"
	"agentdeck/internal/store"
	"github.com/skip2/go-qrcode"
)

func main() {
	if e := run(); e != nil {
		slog.New(slog.NewTextHandler(os.Stderr, nil)).Error("command_failed", "error", e)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: agentdeck serve | console | local-login | pair | pair-pending | pair-approve | devices | revoke | bridge | gateway-locals | gateway-accounts | gateway-add-local | gateway-revoke")
	}
	home, _ := os.UserHomeDir()
	cmd := os.Args[1]
	f := flag.NewFlagSet(cmd, flag.ContinueOnError)
	voiceFile := f.String("voice-command-file", "", "private JSON argv for workstation offline voice transcription")
	inputSession := f.String("session", "", "session owning the uploaded attachment")
	inputUpload := f.String("upload", "", "uploaded attachment ID")
	inputFormat := f.String("format", "text", "attachment output: text, raw or base64")
	inputOffset := f.Int("offset", 0, "attachment byte offset")
	inputLimit := f.Int("limit", 0, "attachment byte limit (text default 64 KiB)")
	dir := f.String("data", filepath.Join(home, ".agentdeck"), "owner-only encrypted state directory")
	addr := f.String("listen", "127.0.0.1:8443", "listener address (HTTP only with --behind-proxy)")
	public := f.String("public-url", "https://localhost:8443", "phone-accessible HTTPS URL")
	cert := f.String("cert", "", "PEM certificate chain")
	key := f.String("key", "", "PEM TLS private key")
	behindProxy := f.Bool("behind-proxy", false, "serve HTTP/WS on a private listener behind a TLS-terminating proxy")
	publicPin := f.String("public-tls-pin", "", "optional external proxy SPKI SHA-256 hex pin (proxy mode only; default system CA trust)")
	rotation := f.Duration("rotation", 15*time.Minute, "traffic key rotation interval")
	codex := f.String("codex-sessions", "", "opt-in Codex session root (empty = disabled)")
	claude := f.String("claude-sessions", "", "opt-in Claude project log root (empty = disabled)")
	lookback := f.Duration("lookback", 72*time.Hour, "only import logs modified within this window (0 = all)")
	historyBytes := f.Int64("history-bytes", 8<<20, "recent bytes on first import (0 = full log)")
	projects := f.String("project-roots", "", "comma-separated directories allowed for mobile agent sessions")
	agentsConfig := f.String("agents-config", "", "owner-only JSON connection settings for OpenCode/Qoder/TRAE/WorkBuddy")
	desktopSocket := f.String("codex-desktop-socket", "", "owner-only Codex desktop coordination socket; enables original-thread sending")
	media := f.String("media-roots", "", "comma-separated allowed image and linked attachment roots")
	out := f.String("out", "pairing", "pairing bundle/QR prefix")
	device := f.String("device", "", "device ID to revoke")
	role := f.String("role", "local", "serve role: local or gateway")
	logLevel := f.String("log-level", os.Getenv("AGENTDECK_LOG_LEVEL"), "gateway text log level: debug, info (default), warn or error; overrides AGENTDECK_LOG_LEVEL")
	relayMode := f.Bool("relay", false, "S-LOCAL outbound relay mode; no public TCP listener")
	gatewayLink := f.String("gateway-link", "", "import owner-only Local/Gateway enrollment JSON")
	gatewayURL := f.String("gateway-url", "", "HTTPS gateway origin for local-login")
	tenant := f.String("tenant", "", "gateway administrative tenant ID")
	name := f.String("name", "Workstation", "Local display name")
	requestID := f.String("request", "", "pairing/grant/local/account ID")
	revokeKind := f.String("kind", "local", "gateway revoke: local, grant, account")
	pairCode := f.String("code", "", "Local/App comparison code to approve")
	noOpen := f.Bool("no-open", false, "print login page without opening browser")
	consoleListen := f.String("console-listen", "127.0.0.1:8787", "local workstation controls: IPv4 loopback address")
	noConsole := f.Bool("no-console", false, "disable local workstation web controls (S-LOCAL only)")
	kind := f.String("agent", "codex", "bridge agent: codex or claude")
	if e := f.Parse(os.Args[2:]); e != nil {
		return e
	}
	if cmd == "input-file" {
		return readInputFile(*dir, *inputSession, *inputUpload, *inputFormat, *inputOffset, *inputLimit)
	}
	if cmd == "local-login" {
		return localLogin(*dir, *gatewayURL, *publicPin, *name, *noOpen)
	}
	if cmd == "console" {
		return runConsole(*dir, *consoleListen, *noOpen)
	}
	if strings.HasPrefix(cmd, "gateway-") || cmd == "pair-pending" || cmd == "pair-approve" {
		return gatewayManagement(cmd, *dir, *tenant, *name, *out, *requestID, *revokeKind, *pairCode)
	}
	if cmd == "pair" || cmd == "devices" || cmd == "revoke" {
		client := server.AdminClient(*dir)
		path := "/devices"
		method := "GET"
		var body io.Reader
		if cmd == "pair" {
			path = "/invite"
			method = "POST"
		}
		if cmd == "revoke" {
			if *device == "" {
				return errors.New("--device required")
			}
			path = "/revoke"
			method = "POST"
			b, _ := json.Marshal(map[string]string{"id": *device})
			body = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, "http://unix"+path, body)
		resp, e := client.Do(req)
		if e != nil {
			return fmt.Errorf("backend admin socket unavailable: %w", e)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("admin request rejected: %d", resp.StatusCode)
		}
		data, e := io.ReadAll(resp.Body)
		if e != nil {
			return e
		}
		if cmd != "pair" {
			fmt.Println(string(data))
			return nil
		}
		var bundle server.Bundle
		if e = json.Unmarshal(data, &bundle); e != nil {
			return e
		}
		raw, _ := json.Marshal(bundle)
		if e = os.WriteFile(*out+".json", raw, 0600); e != nil {
			return e
		}
		if e = os.Chmod(*out+".json", 0600); e != nil {
			return e
		}
		encoded := "agentdeck://pair/" + secure.B64.EncodeToString(raw)
		qr, e := qrcode.Encode(encoded, qrcode.Medium, 512)
		if e != nil {
			return e
		}
		if e = os.WriteFile(*out+".png", qr, 0600); e != nil {
			return e
		}
		_ = os.Chmod(*out+".png", 0600)
		fmt.Printf("Pairing bundle saved: %s.json; QR: %s.png (single-use, expires in 5 minutes)\n", *out, *out)
		return nil
	}
	if cmd == "bridge" {
		if *kind != "codex" && *kind != "claude" {
			return errors.New("unsupported agent")
		}
		args := f.Args()
		if len(args) == 0 {
			return errors.New("bridge requires -- command [args...]")
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		proc := exec.CommandContext(ctx, args[0], args[1:]...)
		proc.Stdin = os.Stdin
		proc.Stderr = os.Stderr
		stdout, e := proc.StdoutPipe()
		if e != nil {
			return e
		}
		if e = proc.Start(); e != nil {
			return e
		}
		client := server.AdminClient(*dir)
		stream := secure.ID()
		var seq uint64
		reader := bufio.NewReader(stdout)
		for {
			line, e := reader.ReadBytes('\n')
			if len(line) > 0 {
				if _, we := os.Stdout.Write(line); we != nil {
					return we
				}
				if json.Valid(bytes.TrimSpace(line)) {
					seq++
					b, _ := json.Marshal(map[string]any{"agent": *kind, "stream": stream, "seq": seq, "record": json.RawMessage(bytes.TrimSpace(line))})
					for attempt := 0; attempt < 3; attempt++ {
						resp, err := client.Post("http://unix/ingest", "application/json", bytes.NewReader(b))
						if err == nil {
							_ = resp.Body.Close()
							if resp.StatusCode == 200 {
								break
							}
						}
						if attempt == 2 {
							log.Print("mirror unavailable; agent stdout continues; check backend")
						}
					}
				}
			}
			if e == io.EOF {
				break
			}
			if e != nil {
				return e
			}
		}
		return proc.Wait()
	}
	if *role != "local" && *role != "gateway" {
		return errors.New("--role must be local or gateway")
	}
	if (*relayMode || *gatewayLink != "") && (*role != "local" || !*relayMode) {
		return errors.New("--gateway-link requires serve --role local --relay")
	}
	if cmd != "serve" {
		return errors.New("unknown command")
	}
	if *relayMode && (*behindProxy || *cert != "" || *key != "" || *publicPin != "") {
		return errors.New("--relay has no public listener; cannot be combined with proxy or certificate flags")
	}
	if !relay.ValidGatewayURL(*public) || (*role != "gateway" && strings.Contains(strings.TrimPrefix(*public, "https://"), "/")) {
		return errors.New("--public-url must be an HTTPS origin; gateway mode also allows a canonical path prefix")
	}
	if *rotation < time.Second {
		return errors.New("--rotation must be >= 1s")
	}
	host, _, e := net.SplitHostPort(*addr)
	if e != nil {
		return e
	}
	if host != "127.0.0.1" && host != "::1" && *cert == "" && !*behindProxy && !*relayMode {
		return errors.New("public listeners require --cert and --key; localhost self-signed TLS is for local testing")
	}
	if (*cert == "") != (*key == "") {
		return errors.New("--cert and --key must be supplied together")
	}
	if *behindProxy && (*cert != "" || *key != "") {
		return errors.New("--behind-proxy cannot be combined with local --cert/--key")
	}
	if *publicPin != "" {
		decoded, err := hex.DecodeString(*publicPin)
		if !*behindProxy || err != nil || len(decoded) != 32 {
			return errors.New("--public-tls-pin requires --behind-proxy and a 64-character SPKI SHA-256 hex digest")
		}
		*publicPin = strings.ToLower(*publicPin)
	}
	db, e := store.New(*dir)
	if e != nil {
		return e
	}
	defer db.Close()
	if e = db.RepairPresentation(); e != nil {
		return e
	}
	if e = db.RecoverManaged(); e != nil {
		return e
	}
	var tlsCert tls.Certificate
	pin := *publicPin
	if !*behindProxy && !*relayMode {
		tlsCert, pin, e = server.Certificate(*dir, *cert, *key)
		if e != nil {
			return e
		}
	}
	trust := "pin"
	if *behindProxy && pin == "" {
		trust = "system"
	}
	if *role == "gateway" {
		if *voiceFile != "" {
			return errors.New("voice transcription belongs to S-LOCAL")
		}
		if *codex != "" || *claude != "" || *media != "" || *agentsConfig != "" {
			return errors.New("S-GATEWAY never imports agent content; configure sources only on S-LOCAL")
		}
		return serveGateway(db, *dir, *addr, *public, pin, trust, *logLevel, tlsCert, *behindProxy, *rotation)
	}
	var link relay.Link
	if *relayMode {
		if *gatewayLink != "" {
			raw, e := os.ReadFile(*gatewayLink)
			if e != nil {
				return e
			}
			if json.Unmarshal(raw, &link) != nil || link.Validate() != nil {
				return errors.New("invalid owner-only gateway enrollment")
			}
			if e = db.Put("relayconfig", "gateway", link); e != nil {
				return e
			}
		} else if err := db.Get("relayconfig", "gateway", &link); err != nil && !os.IsNotExist(err) {
			return err
		}
		if link.URL != "" {
			if err := link.Validate(); err != nil {
				return err
			}
			pin, *public, trust = link.Pin, link.URL, link.TLSTrust
		}
	}
	s := server.New(db, pin, *public, *rotation)
	s.TLSTrust = trust
	if *behindProxy && pin == "" {
		s.TLSTrust = "system"
	}
	normal := &agent.Normalizer{Store: db}
	for _, p := range strings.Split(*media, ",") {
		if p != "" {
			abs, e := filepath.Abs(p)
			if e != nil {
				return e
			}
			normal.MediaRoots = append(normal.MediaRoots, abs)
		}
	}
	s.Normalizer = normal
	roots := []agent.Root{}
	if *codex != "" {
		roots = append(roots, agent.Root{Agent: "codex", Path: *codex, Source: "log"})
	}
	if *claude != "" {
		roots = append(roots, agent.Root{Agent: "claude", Path: *claude, Source: "log"})
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	voiceArgs, voiceErr := loadVoiceCommand(*voiceFile)
	if voiceErr != nil {
		return voiceErr
	}
	executable, _ := os.Executable()
	controlDir, _ := filepath.Abs(*dir)
	s.Manager = &agent.Manager{Store: db, Normalizer: normal, Context: ctx, VoiceCommand: voiceArgs, Binary: executable, ControlDir: controlDir}
	go func() {
		if e := s.Manager.WarmVoice(); e != nil {
			log.Printf("Local voice warmup unavailable; first recording can retry")
		}
	}()
	connectionConfig, err := agent.LoadConnections(*agentsConfig)
	if err != nil {
		return err
	}
	s.Manager.Configure(connectionConfig)
	if *desktopSocket != "" {
		desktop := &agent.DesktopBridge{Socket: *desktopSocket}
		if err := desktop.Validate(); err != nil {
			return err
		}
		s.Manager.Desktop = desktop
	}

	for _, p := range strings.Split(*projects, ",") {
		if p != "" {
			s.Manager.Roots = append(s.Manager.Roots, p)
		}
	}
	defer s.Manager.Close()
	if !*noConsole {
		controls, err := startConsole(*dir, *consoleListen)
		if err != nil {
			return err
		}
		defer controls.server.Close()
		log.Printf("AgentDeck workstation controls: %s", controls.URL)
		go func() {
			select {
			case <-ctx.Done():
			case err := <-controls.done:
				if err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Print("workstation controls stopped unexpectedly")
					stop()
				}
			}
		}()
	}
	if *relayMode {
		if err := s.EnableRelay(ctx); err != nil {
			return err
		}
		defer func() { stop(); s.Association.Wait() }()
	}
	s.Manager.RunDeliveries(ctx)
	s.Manager.RunConnectors(ctx)
	index := ""
	if *codex != "" {
		p, e := filepath.Abs(*codex)
		if e != nil {
			return e
		}
		for p != filepath.Dir(p) {
			if filepath.Base(p) == "sessions" {
				index = filepath.Join(filepath.Dir(p), "session_index.jsonl")
				break
			}
			p = filepath.Dir(p)
		}
	}
	follower := &agent.Follower{Normalizer: normal, Roots: roots, Lookback: *lookback, HistoryBytes: *historyBytes, Index: index}
	followDone := make(chan struct{})
	go func() { defer close(followDone); follower.Run(ctx) }()
	go func() {
		if e := s.Admin(ctx, *dir); e != nil {
			log.Printf("admin socket failed: %v", e)
			stop()
		}
	}()
	if *relayMode {
		log.Print("AgentDeck relay mode; encrypted outbound gateway connection enabled")
		<-ctx.Done()
		s.Association.Wait()
		stop()
		<-followDone
		return nil
	}
	srv := &http.Server{Addr: *addr, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 90 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{tlsCert}}}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if *behindProxy {
		log.Printf("AgentDeck HTTP behind TLS proxy listening %s; public origin %s; sources=%d; rotation=%s; keep upstream private", *addr, *public, len(roots), *rotation)
		e = srv.ListenAndServe()
	} else {
		log.Printf("AgentDeck HTTPS listening %s; public origin %s; sources=%d; rotation=%s", *addr, *public, len(roots), *rotation)
		e = srv.ListenAndServeTLS("", "")
	}
	stop()
	<-followDone
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
