package server

import (
	"agentdeck/internal/localipc"
	"context"
	"crypto/ecdh"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"agentdeck/internal/agent"
	"agentdeck/internal/relay"
	"agentdeck/internal/secure"
	"agentdeck/internal/store"
	"github.com/gorilla/websocket"
)

type Invite struct {
	Token     []byte
	Private   []byte
	Expires   int64
	Grant     string
	AppPublic []byte
	Approved  bool
}
type Bundle struct {
	Version  int            `json:"version"`
	URL      string         `json:"url"`
	Pin      string         `json:"pin"`
	Token    string         `json:"token"`
	Public   string         `json:"public"`
	Expires  int64          `json:"expires"`
	TLSTrust string         `json:"tls_trust,omitempty"`
	Relay    *relay.Binding `json:"relay,omitempty"`
}
type ticket struct {
	Device  string
	Salt    []byte
	Expires time.Time
	Clock   *secure.Clock
}
type Server struct {
	Stop           func()
	updateDraining bool
	Store          *store.Store
	Pin            string
	PublicURL      string
	TLSTrust       string
	Rotation       time.Duration
	Clock          *secure.Clock
	RelayClock     func() *secure.Clock
	mu             sync.Mutex
	tickets        map[string]ticket
	replay         map[string]time.Time
	window         time.Time
	attempts       int
	active         int
	appStreams     map[string]map[uint64]appStream
	appStreamID    uint64
	Manager        *agent.Manager
	Normalizer     *agent.Normalizer
	commandMu      sync.Mutex
	uploadMu       sync.Mutex
	ingestMu       sync.Mutex
	pairMu         sync.Mutex
	RelayBundle    func(Bundle) (Bundle, error)
	Association    *Association
}

func New(s *store.Store, pin, url string, rotation time.Duration) *Server {
	return &Server{Store: s, Pin: pin, PublicURL: url, Rotation: rotation, Clock: secure.NewClock(), tickets: map[string]ticket{}, replay: map[string]time.Time{}}
}
func (s *Server) Invite() (Bundle, error) {
	clock := s.protocolClock(nil)
	if clock == nil {
		return Bundle{}, errors.New("gateway clock unavailable")
	}
	p, e := ecdh.X25519().GenerateKey(nilReader{})
	if e != nil {
		return Bundle{}, e
	}
	token := secure.Random(32)
	exp := clock.Unix() + 300
	iv := Invite{Token: token, Private: p.Bytes(), Expires: exp}
	h := sha256.Sum256(token)
	bundle := Bundle{Version: 1, URL: s.PublicURL, Pin: s.Pin, Token: secure.B64.EncodeToString(token), Public: secure.B64.EncodeToString(p.PublicKey().Bytes()), Expires: exp, TLSTrust: s.TLSTrust}
	if s.RelayBundle != nil {
		bundle, e = s.RelayBundle(bundle)
		if e != nil {
			return Bundle{}, e
		}
		iv.Grant = bundle.Relay.Grant
		iv.Expires = bundle.Expires
	}
	if e = s.Store.Put("invites", hex.EncodeToString(h[:]), iv); e != nil {
		return Bundle{}, e
	}
	return bundle, nil
}

// crypto/ecdh requires an io.Reader; secure.Random uses crypto/rand directly.
type nilReader struct{}

func (nilReader) Read(p []byte) (int, error) { copy(p, secure.Random(len(p))); return len(p), nil }
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"service": "agentdeck", "version": 1})
	})
	m.HandleFunc("POST /v1/pair", s.pair)
	m.HandleFunc("POST /v1/offer", s.offer)
	m.HandleFunc("POST /v1/session", s.session)
	m.HandleFunc("POST /v1/time-sync", s.timeSync)
	m.HandleFunc("GET /v1/stream", s.stream)
	m.HandleFunc("GET /v1/agents/workbuddy/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if s.Manager == nil || len(r.URL.RawQuery) > 8192 {
			fail(w, 400)
			return
		}
		if e := s.Manager.WorkBuddyCallback(r.Context(), r.URL.Query().Get("state"), r.URL.Query().Get("code")); e != nil {
			w.WriteHeader(400)
			_, _ = w.Write([]byte("WorkBuddy 授权未完成，请返回 AgentDeck 重试。"))
			return
		}
		_, _ = w.Write([]byte("WorkBuddy 授权完成，请返回 AgentDeck。"))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if s.protocolClock(r) == nil {
			fail(w, 503)
			return
		}
		m.ServeHTTP(w, r)
	})
}
func write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int) { http.Error(w, http.StatusText(code), code) }
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
func (s *Server) limit() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if now.Sub(s.window) > time.Minute {
		s.window = now
		s.attempts = 0
	}
	s.attempts++
	return s.attempts <= 120
}
func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	if !s.limit() {
		fail(w, 429)
		return
	}
	var req struct {
		Token  string      `json:"token"`
		Public string      `json:"public"`
		Name   string      `json:"name"`
		Invite string      `json:"invite"`
		Box    *secure.Box `json:"box"`
	}
	if decode(w, r, &req) != nil {
		fail(w, 400)
		return
	}
	pub, e := secure.B64.DecodeString(req.Public)
	if e != nil || len(pub) != 32 {
		fail(w, 400)
		return
	}
	// The invitation token never crosses a gateway in plaintext. Validate the
	// sealed proof before atomically consuming an invitation, so a relay cannot
	// burn an invitation merely by observing its non-secret hash.
	s.pairMu.Lock()
	defer s.pairMu.Unlock()
	id := req.Invite
	var tok []byte
	if req.Box == nil {
		if relay.ContextGrant(r.Context()) != "" {
			fail(w, 401)
			return
		}
		tok, e = secure.B64.DecodeString(req.Token)
		if e != nil || len(tok) != 32 {
			fail(w, 401)
			return
		}
		hash := sha256.Sum256(tok)
		id = hex.EncodeToString(hash[:])
	}
	var iv Invite
	if s.Store.Get("invites", id, &iv) != nil || iv.Expires < s.protocolClock(r).Unix() || iv.Grant != relay.ContextGrant(r.Context()) {
		fail(w, 401)
		return
	}
	p, e := ecdh.X25519().NewPrivateKey(iv.Private)
	if e != nil {
		fail(w, 500)
		return
	}
	root, e := secure.PairRoot(p, pub, iv.Token)
	if e != nil {
		fail(w, 400)
		return
	}
	if req.Box != nil {
		plain, err := secure.Open(root, "pair-request/"+id, *req.Box)
		var proof struct {
			Token string `json:"token"`
			Name  string `json:"name"`
		}
		if err != nil || json.Unmarshal(plain, &proof) != nil || req.Box.Epoch != 0 || req.Box.Seq != 0 {
			fail(w, 401)
			return
		}
		tok, e = secure.B64.DecodeString(proof.Token)
		req.Name = proof.Name
	}
	if e != nil || subtle.ConstantTimeCompare(tok, iv.Token) != 1 {
		fail(w, 401)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = "AgentDeck"
	}
	if req.Name, e = store.CleanDeviceName(req.Name); e != nil {
		fail(w, 400)
		return
	}
	if len(iv.AppPublic) > 0 {
		if subtle.ConstantTimeCompare(pub, iv.AppPublic) != 1 {
			fail(w, 401)
			return
		}
		if !iv.Approved {
			fail(w, 202)
			return
		}
	}
	if s.Store.Take("invites", id, &iv) != nil {
		fail(w, 401)
		return
	}
	if iv.Grant != "" {
		_ = s.Store.Delete("relaypending", iv.Grant)
	}
	dev := store.Device{ID: secure.ID(), Name: req.Name, Root: root, Created: time.Now().UTC().Format(time.RFC3339), RelayGrant: iv.Grant}
	if e = s.Store.Put("devices", dev.ID, dev); e != nil {
		fail(w, 500)
		return
	}
	box, e := secure.SealJSON(root, "pair", 0, 0, map[string]any{"device": dev.ID, "relay_grant": dev.RelayGrant})
	if e != nil {
		fail(w, 500)
		return
	}
	if dev.RelayGrant != "" {
		w.Header().Set("X-AgentDeck-Bound-Device", dev.ID)
	}
	write(w, box)
}
func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	clock := s.protocolClock(r)
	if !s.limit() {
		fail(w, 429)
		return
	}
	var req struct {
		Device string     `json:"device"`
		Box    secure.Box `json:"box"`
	}
	if decode(w, r, &req) != nil {
		fail(w, 400)
		return
	}
	var dev store.Device
	if s.Store.Get("devices", req.Device, &dev) != nil || dev.RelayGrant != relay.ContextGrant(r.Context()) {
		fail(w, 401)
		return
	}
	plain, e := secure.Open(dev.Root, "hello/"+dev.ID, req.Box)
	var hello struct {
		Time      int64  `json:"time"`
		Challenge string `json:"challenge"`
	}
	if e != nil || json.Unmarshal(plain, &hello) != nil || hello.Time < clock.Unix()-60 || hello.Time > clock.Unix()+60 || len(hello.Challenge) < 16 {
		fail(w, 401)
		return
	}
	s.mu.Lock()
	now := time.Now()
	for k, v := range s.replay {
		if now.Sub(v) > 2*time.Minute {
			delete(s.replay, k)
		}
	}
	rk := dev.ID + ":" + req.Box.Nonce
	if _, ok := s.replay[rk]; ok || len(s.replay) >= 4096 {
		s.mu.Unlock()
		fail(w, 401)
		return
	}
	s.replay[rk] = now
	for k, v := range s.tickets {
		if now.After(v.Expires) {
			delete(s.tickets, k)
		}
	}
	if len(s.tickets) >= 256 {
		s.mu.Unlock()
		fail(w, 429)
		return
	}
	id := secure.ID()
	salt := secure.Random(32)
	s.tickets[id] = ticket{dev.ID, salt, now.Add(30 * time.Second), clock}
	s.mu.Unlock()
	box, e := secure.SealJSON(dev.Root, "welcome/"+dev.ID, 0, 0, map[string]any{"device_name": dev.Name, "ticket": id, "salt": secure.B64.EncodeToString(salt), "rotation_ms": s.Rotation.Milliseconds(), "server_time_ms": clock.Millis(), "clock_version": 1, "challenge": hello.Challenge})
	if e != nil {
		fail(w, 500)
		return
	}
	write(w, box)
}

type Request struct {
	ResetCredit       string            `json:"reset_credit,omitempty"`
	ResetAccount      string            `json:"reset_account,omitempty"`
	ResetIntent       string            `json:"reset_intent,omitempty"`
	ResetCursor       string            `json:"reset_cursor,omitempty"`
	Confirmed         bool              `json:"confirmed,omitempty"`
	Timing            *clientOpenTiming `json:"timing,omitempty"`
	Compact           bool              `json:"compact,omitempty"`
	Item              string            `json:"item,omitempty"`
	PermissionMode    string            `json:"permission_mode,omitempty"`
	PermissionVersion string            `json:"permission_version,omitempty"`
	Version           string            `json:"version,omitempty"`
	Speed             string            `json:"speed,omitempty"`
	Download          string            `json:"download,omitempty"`
	GoalVersion       string            `json:"goal_version,omitempty"`
	GoalAction        string            `json:"goal_action,omitempty"`
	Delivery          string            `json:"delivery,omitempty"`
	QueueID           string            `json:"queue_id,omitempty"`
	QueueVersion      string            `json:"queue_version,omitempty"`
	Model             string            `json:"model,omitempty"`
	Effort            string            `json:"effort,omitempty"`
	Attachments       []string          `json:"attachments,omitempty"`
	Upload            string            `json:"upload,omitempty"`
	Name              string            `json:"name,omitempty"`
	MIME              string            `json:"mime,omitempty"`
	Kind              string            `json:"kind,omitempty"`
	Bytes             string            `json:"bytes,omitempty"`
	Size              int               `json:"size,omitempty"`
	Offset            int               `json:"offset,omitempty"`
	Limit             int               `json:"limit,omitempty"`
	Project           string            `json:"project,omitempty"`
	Agent             string            `json:"agent,omitempty"`
	Text              string            `json:"text,omitempty"`
	Approval          string            `json:"approval,omitempty"`
	Decision          string            `json:"decision,omitempty"`
	ID                string            `json:"id"`
	Method            string            `json:"method"`
	Session           string            `json:"session,omitempty"`
	After             uint64            `json:"after,omitempty"`
	Before            uint64            `json:"before,omitempty"`
	First             uint64            `json:"first,omitempty"`
	Recent            bool              `json:"recent,omitempty"`
	Media             string            `json:"media,omitempty"`
	Href              string            `json:"href,omitempty"`
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.mu.Lock()
	tk, ok := s.tickets[token]
	delete(s.tickets, token)
	if !ok || time.Now().After(tk.Expires) || s.active >= 32 {
		s.mu.Unlock()
		fail(w, 401)
		return
	}
	s.active++
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.active--; s.mu.Unlock() }()
	var dev store.Device
	if s.Store.Get("devices", tk.Device, &dev) != nil || dev.RelayGrant != relay.ContextGrant(r.Context()) {
		fail(w, 401)
		return
	}
	up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }}
	conn, e := up.Upgrade(w, r, nil)
	if e != nil {
		return
	}
	defer conn.Close()
	disconnect, err := s.trackAppConnection(tk.Device, connectionIP(r), func() {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4003, "pairing revoked"), time.Now().Add(time.Second))
		_ = conn.Close()
	})
	if err != nil {
		return
	}
	defer disconnect()
	lastDeviceName := dev.Name
	downloads := downloads{}
	defer downloads.prune(true)
	conn.SetReadLimit(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(90 * time.Second)) })
	type streamRequest struct {
		box     secure.Box
		arrived time.Time
	}
	requests := make(chan streamRequest, 8)
	done := make(chan struct{})
	stop := make(chan struct{})
	defer close(stop)
	commandContext, stopCommands := context.WithCancel(r.Context())
	defer stopCommands()
	go func() {
		defer close(done)
		defer stopCommands()
		for {
			var b secure.Box
			if e := conn.ReadJSON(&b); e != nil {
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			select {
			case requests <- streamRequest{box: b, arrived: time.Now()}:
			case <-stop:
				return
			}
		}
	}()
	commands := make(chan streamCommand, 8)
	commandResults := make(chan map[string]any, 8)
	go s.runStreamCommands(tk.Device, commands, commandResults, commandContext.Done())
	asyncResults := make(chan map[string]any, 8)
	asyncSlots := make(chan struct{}, 2)
	var sent, received, cursor uint64
	stopDesktop := func() {}
	defer func() { stopDesktop() }()
	var watched string
	var recentMode bool
	var compactMode bool
	var clientTimingCheck time.Time
	last := uint64(0)
	send := func(v any) error {
		sent++
		epoch := tk.Clock.Epoch(s.Rotation)
		key := secure.Traffic(dev.Root, tk.Salt, "s2c", epoch)
		b, e := secure.SealJSON(key, "s2c", epoch, sent, v)
		if e != nil {
			return e
		}
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(b)
	}
	queueState := ""
	queueCheck := time.Time{}
	pushQueue := func(force bool) error {
		if watched == "" || s.Manager == nil {
			return nil
		}
		queueCheck = time.Now()
		var ss store.Session
		if s.Store.Get("sessions", watched, &ss) != nil || (ss.Agent != "codex" && ss.Agent != "claude") {
			return nil
		}
		messages, err := s.Manager.Queue(watched)
		packet := map[string]any{"type": "queue", "session": watched, "messages": messages}
		if err != nil {
			packet["message"] = err.Error()
		}
		raw, _ := json.Marshal(packet)
		sum := sha256.Sum256(raw)
		fingerprint := hex.EncodeToString(sum[:])
		if !force && fingerprint == queueState {
			return nil
		}
		queueState = fingerprint
		return send(packet)
	}
	catalogState := ""
	sessionRevision := s.Store.SessionCatalogRevision()
	catalogCheck := time.Now()
	var nativeReadSessions []store.Session
	nativeReadState := ""
	nativeReadCheck := time.Time{}
	pushNativeReads := func(force bool) error {
		if s.Manager == nil {
			return nil
		}
		nativeReadCheck = time.Now()
		state := s.Manager.CodexReadState(nativeReadSessions)
		raw, _ := json.Marshal(state)
		if !force && string(raw) == nativeReadState {
			return nil
		}
		nativeReadState = string(raw)
		return send(map[string]any{"type": "native_reads", "agent": "codex", "state": state})
	}
	sessions := func(id string) error {
		ss, e := s.Store.Sessions()
		if e != nil {
			return e
		}
		nativeReadSessions = ss
		state := agent.NativeReadState{States: map[string]bool{}}
		if s.Manager != nil {
			state = s.Manager.CodexReadState(ss)
		}
		raw, _ := json.Marshal(state)
		nativeReadState = string(raw)
		nativeReadCheck = time.Now()
		return send(map[string]any{"type": "sessions", "id": id, "sessions": ss, "native_reads": state, "codex_desktop": s.Manager != nil && s.Manager.Desktop != nil, "agents": func() any {
			if s.Manager != nil {
				info := s.Manager.Catalog()
				encoded, _ := json.Marshal(info)
				catalogState = string(encoded)
				return info
			}
			return []any{}
		}(), "projects": func() any {
			if s.Manager != nil {
				return s.Manager.Projects()
			}
			return []any{}
		}()})
	}
	if sessions("") != nil {
		return
	}
	last = s.Store.LastSeq()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case response := <-commandResults:
			if send(response) != nil {
				return
			}
			if pushQueue(true) != nil {
				return
			}
			if response["type"] == "result" && sessions("") != nil {
				return
			}
		case response := <-asyncResults:
			if send(response) != nil {
				return
			}
		case <-done:
			return
		case incoming := <-requests:
			b := incoming.box
			started := time.Now()
			epoch := tk.Clock.Epoch(s.Rotation)
			if b.Seq != received+1 || b.Epoch < epoch-1 || b.Epoch > epoch+1 {
				return
			}
			plain, e := secure.Open(secure.Traffic(dev.Root, tk.Salt, "c2s", b.Epoch), "c2s", b)
			if e != nil {
				return
			}
			received = b.Seq
			var req Request
			if json.Unmarshal(plain, &req) != nil {
				return
			}
			switch req.Method {
			case "client_timing":
				accepted := false
				if time.Since(clientTimingCheck) >= time.Second {
					accepted = logClientTiming(req)
					if accepted {
						clientTimingCheck = time.Now()
					}
				}
				if send(map[string]any{"type": "result", "id": req.ID, "result": map[string]bool{"accepted": accepted}}) != nil {
					return
				}
			case "usage_resets", "usage_reset_history", "usage_reset_prepare", "usage_reset_consume", "goal_list", "usage", "models", "session_settings", "session_permissions", "input_capabilities", "transcribe", "transcribe_preview", "upload_preview":
				select {
				case asyncSlots <- struct{}{}:
					go func(req Request, owner string) {
						defer func() { <-asyncSlots }()
						result, e := s.inputRequest(owner, req)
						response := map[string]any{"type": "result", "id": req.ID, "result": result}
						if e != nil {
							response = map[string]any{"type": "error", "id": req.ID, "message": e.Error()}
						}
						select {
						case asyncResults <- response:
						case <-stop:
						}
					}(req, tk.Device)
				default:
					if send(map[string]any{"type": "error", "id": req.ID, "message": "输入服务正在处理，请稍后重试"}) != nil {
						return
					}
				}
			case "upload_begin", "upload_chunk", "upload_drop", "upload_read":
				result, err := s.inputRequest(tk.Device, req)
				if err != nil {
					if send(map[string]any{"type": "error", "id": req.ID, "message": err.Error()}) != nil {
						return
					}
				} else {
					if send(map[string]any{"type": "result", "id": req.ID, "result": result}) != nil {
						return
					}
				}
			case "queue_list":
				var result any
				err := errors.New("工作站未启用项目操作")
				if s.Manager != nil {
					result, err = s.Manager.Queue(req.Session)
				}
				response := map[string]any{"type": "result", "id": req.ID, "result": result}
				if err != nil {
					response = map[string]any{"type": "error", "id": req.ID, "message": err.Error()}
				}
				if send(response) != nil {
					return
				}
			case "session_permissions_change", "goal_change", "create", "send", "interrupt", "approve", "queue_cancel", "queue_edit", "queue_steer", "queue_delete":
				select {
				case commands <- streamCommand{request: req, hash: sha256.Sum256(plain)}:
				default:
					if send(map[string]any{"type": "error", "id": req.ID, "message": "输入服务正在处理，请稍后重试", "not_sent": true}) != nil {
						return
					}
				}
			case "agent_login":
				if s.Manager == nil {
					return
				}
				result, err := s.Manager.AgentLogin(req.Agent)
				if err != nil {
					if send(map[string]any{"type": "error", "id": req.ID, "message": err.Error()}) != nil {
						return
					}
				} else if send(map[string]any{"type": "result", "id": req.ID, "result": result}) != nil {
					return
				}
			case "rename_device":
				name, err := s.Store.RenameDevice(tk.Device, req.Text)
				if err != nil {
					if send(map[string]any{"type": "error", "id": req.ID, "message": "设备名称无效，请输入 1–80 个字符"}) != nil {
						return
					}
				} else if send(map[string]any{"type": "result", "id": req.ID, "result": map[string]string{"name": name}}) != nil {
					return
				}
			case "sessions":
				if sessions(req.ID) != nil {
					return
				}
			case "watch":
				stopDesktop()
				stopDesktop = func() {}
				watched = req.Session
				queueState = ""
				recentMode = req.Recent || req.Compact
				compactMode = req.Compact
				cursor = req.After
				if recentMode && watched != "" {
					if s.Manager != nil {
						// ACP history loading can take longer than a heartbeat. Continue
						// serving the encrypted stream while the adapter imports it.
						id := watched
						go func() {
							if err := s.Manager.Hydrate(id); err != nil {
								var session store.Session
								if s.Store.Get("sessions", id, &session) == nil {
									event := store.Event{ID: "hydrate-error/" + id, Session: id, Agent: session.Agent, Kind: "notice", Text: err.Error()}
									if session.Source == "claude-desktop" {
										_, _, _ = s.Store.AppendCatalogHistory(event)
									} else {
										_, _, _ = s.Store.Append(event)
									}
								}
							}
						}()
					}
					readStarted := time.Now()
					page, err := s.Store.History(watched, 0, 40)
					if err != nil {
						return
					}
					cursor = page.Cursor
					projected := s.projectHistory(page, compactMode)
					writeStarted := time.Now()
					if send(map[string]any{"type": "history", "id": req.ID, "session": watched, "page": projected}) != nil {
						return
					}
					// Durations and record counts only: no keys, message bodies or
					// attachment paths. Separate Local work from phone/network delay.
					slog.Info("session_open", "session", watched,
						"request", req.ID,
						"wait_ms", started.Sub(incoming.arrived).Milliseconds(),
						"history_ms", writeStarted.Sub(readStarted).Milliseconds(),
						"write_ms", time.Since(writeStarted).Milliseconds(),
						"total_ms", time.Since(incoming.arrived).Milliseconds(), "records", len(page.Events))
				}
				if send(map[string]any{"type": "watching", "id": req.ID, "session": watched}) != nil {
					return
				}
				// Cached history is the first-screen dependency. Queue inspection
				// and native subscription setup must not delay showing it.
				if s.Manager != nil && watched != "" {
					stopDesktop = s.Manager.WatchDesktop(r.Context(), watched)
				}
				if pushQueue(true) != nil {
					return
				}
			case "item_detail":
				if !executionKind(req.Kind) || req.Item == "" || len(req.Item) > 512 {
					if send(map[string]any{"type": "error", "id": req.ID, "message": "该记录已不可用"}) != nil {
						return
					}
					continue
				}
				event, err := s.Store.TranscriptItem(req.Session, req.Item, req.Kind)
				if err != nil {
					if send(map[string]any{"type": "error", "id": req.ID, "message": "该记录已不可用"}) != nil {
						return
					}
				} else if send(map[string]any{"type": "result", "id": req.ID, "result": event}) != nil {
					return
				}
			case "outline":
				page, err := s.Store.Outline(req.Session, req.Before, 200)
				if err != nil {
					if send(map[string]any{"type": "error", "id": req.ID, "message": "目录读取失败"}) != nil {
						return
					}
				} else if send(map[string]any{"type": "result", "id": req.ID, "result": page}) != nil {
					return
				}
			case "history_at":
				page, err := s.Store.HistoryAt(req.Session, req.First)
				if err != nil {
					if send(map[string]any{"type": "error", "id": req.ID, "message": "该记录已不可用"}) != nil {
						return
					}
				} else if send(map[string]any{"type": "result", "id": req.ID, "result": s.projectHistory(page, req.Compact)}) != nil {
					return
				}
			case "history_after":
				page, err := s.Store.HistoryAfter(req.Session, req.After)
				if err != nil {
					if send(map[string]any{"type": "error", "id": req.ID, "message": "历史读取失败"}) != nil {
						return
					}
				} else if send(map[string]any{"type": "result", "id": req.ID, "result": s.projectHistory(page, req.Compact)}) != nil {
					return
				}
			case "history":
				page, err := s.Store.History(req.Session, req.Before, 40)
				if err != nil {
					if send(map[string]any{"type": "error", "id": req.ID, "message": "历史读取失败"}) != nil {
						return
					}
				} else if send(map[string]any{"type": "result", "id": req.ID, "result": s.projectHistory(page, req.Compact)}) != nil {
					return
				}
			case "attachment_open", "attachment_read", "attachment_close":
				result, err := downloads.request(s.Normalizer, req)
				response := map[string]any{"type": "result", "id": req.ID, "result": result}
				if err != nil {
					response = map[string]any{"type": "error", "id": req.ID, "message": err.Error()}
				}
				if send(response) != nil {
					return
				}
			case "attachment":
				var result agent.Attachment
				var err error
				if s.Normalizer == nil {
					err = errors.New("工作站未启用附件读取")
				} else {
					result, err = s.Normalizer.ReadAttachment(req.Session, req.Href)
				}
				if err != nil {
					if send(map[string]any{"type": "error", "id": req.ID, "message": err.Error()}) != nil {
						return
					}
				} else if send(map[string]any{"type": "result", "id": req.ID, "result": result}) != nil {
					return
				}
			case "media":
				var media store.Media
				if !s.mayReadMedia(tk.Device, req.Media) {
					e = errors.New("图片不可用")
				} else {
					e = s.Store.Get("media", req.Media, &media)
				}
				if e != nil {
					if send(map[string]any{"type": "error", "id": req.ID, "message": "图片不可用"}) != nil {
						return
					}
				} else if send(map[string]any{"type": "media", "id": req.ID, "media": req.Media, "mime": media.MIME, "bytes": secure.B64.EncodeToString(media.Data)}) != nil {
					return
				}
			case "ping":
				if send(map[string]any{"type": "pong", "id": req.ID}) != nil {
					return
				}
			default:
				if send(map[string]any{"type": "error", "id": req.ID, "message": "未知请求"}) != nil {
					return
				}
			}
		case <-ticker.C:
			downloads.prune(false)
			if time.Since(nativeReadCheck) >= 2*time.Second {
				if pushNativeReads(false) != nil {
					return
				}
			}
			if time.Since(queueCheck) >= time.Second {
				if pushQueue(false) != nil {
					return
				}
			}
			if s.Store.Get("devices", tk.Device, &dev) != nil {
				return
			}
			if dev.Name != lastDeviceName {
				lastDeviceName = dev.Name
				if send(map[string]any{"type": "device", "name": dev.Name}) != nil {
					return
				}
			}
			if s.Manager != nil && time.Since(catalogCheck) >= 2*time.Second {
				catalogCheck = time.Now()
				info := s.Manager.Catalog()
				encoded, _ := json.Marshal(info)
				if string(encoded) != catalogState {
					catalogState = string(encoded)
					if send(map[string]any{"type": "agents", "agents": info}) != nil {
						return
					}
				}
			}
			seq := s.Store.LastSeq()
			revision := s.Store.SessionCatalogRevision()
			if seq != last || revision != sessionRevision {
				if sessions("") != nil {
					return
				}
				last = seq
				sessionRevision = revision
			}
			if watched != "" {
				var events []store.Event
				var e error
				next := cursor
				if recentMode {
					events, next, e = s.Store.LiveEvents(watched, cursor, 100)
				} else {
					events, e = s.Store.Events(watched, cursor, 100)
					if len(events) > 0 {
						next = events[len(events)-1].Seq
					}
				}
				if e != nil {
					return
				}
				cursor = next
				if len(events) > 0 {
					events = s.transcriptEvents(events, compactMode)
					if send(map[string]any{"type": "events", "session": watched, "events": events, "cursor": cursor}) != nil {
						return
					}
				}
			}
		case <-ping.C:
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
				return
			}
		}
	}
}

// Admin listens on an owner-only Unix socket. No public endpoint can mint invites
// or revoke phones; this is a separate boundary from the HTTPS viewing service.
func (s *Server) Admin(ctx context.Context, dir string) error {
	path := filepath.Join(dir, "control.sock")
	ln, e := localipc.Listen(dir, "control")
	if e != nil {
		return e
	}
	m := http.NewServeMux()
	s.updateRoutes(m, dir)
	m.HandleFunc("POST /ingest", func(w http.ResponseWriter, r *http.Request) {
		if s.Normalizer == nil {
			fail(w, 503)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
		var req struct {
			Agent  string          `json:"agent"`
			Stream string          `json:"stream"`
			Seq    uint64          `json:"seq"`
			Record json.RawMessage `json:"record"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || (req.Agent != "codex" && req.Agent != "claude") || req.Stream == "" {
			fail(w, 400)
			return
		}
		s.ingestMu.Lock()
		defer s.ingestMu.Unlock()
		k := "bridge/" + req.Stream
		st := agent.State{Session: req.Stream}
		e := s.Store.Get("checkpoints", k, &st)
		if e != nil && !os.IsNotExist(e) {
			fail(w, 500)
			return
		}
		events, e := s.Normalizer.Parse(req.Record, req.Agent, "stream", &st)
		if e != nil {
			fail(w, 400)
			return
		}
		for i, ev := range events {
			if ev.Session == "" {
				ev.Session = req.Stream
			}
			ev.ID = fmt.Sprintf("bridge/%s/%d/%d", req.Stream, req.Seq, i)
			if _, _, e = s.Store.Append(ev); e != nil {
				fail(w, 500)
				return
			}
		}
		if e = s.Store.Put("checkpoints", k, st); e != nil {
			fail(w, 500)
			return
		}
		write(w, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /invite", func(w http.ResponseWriter, r *http.Request) {
		b, e := s.Invite()
		if e != nil {
			fail(w, 500)
			return
		}
		write(w, b)
	})
	s.associationRoutes(m)
	m.HandleFunc("GET /pair-pending", func(w http.ResponseWriter, r *http.Request) {
		clock := s.protocolClock(nil)
		if clock == nil {
			fail(w, 503)
			return
		}
		rows, e := s.Store.Values("relaypending")
		if e != nil {
			fail(w, 500)
			return
		}
		out := []PendingPair{}
		for _, raw := range rows {
			var p PendingPair
			_ = json.Unmarshal(raw, &p)
			if p.Expires >= clock.Unix() {
				out = append(out, p)
			}
		}
		write(w, out)
	})
	m.HandleFunc("POST /pair-approve", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Grant string `json:"grant"`
			Code  string `json:"code"`
		}
		if decode(w, r, &req) != nil || s.Approve(req.Grant, req.Code) != nil {
			fail(w, 401)
			return
		}
		write(w, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /input-read", s.adminInputRead)
	m.HandleFunc("GET /devices", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Store.Devices()
		if e != nil {
			fail(w, 500)
			return
		}
		write(w, v)
	})
	m.HandleFunc("GET /app-connections", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.appConnections()
		if err != nil {
			fail(w, 500)
			return
		}
		write(w, v)
	})
	m.HandleFunc("POST /device-name", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if decode(w, r, &req) != nil {
			fail(w, 400)
			return
		}
		name, err := s.Store.RenameDevice(req.ID, req.Name)
		if err != nil {
			fail(w, 400)
			return
		}
		write(w, map[string]string{"name": name})
	})
	m.HandleFunc("POST /revoke", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		if decode(w, r, &req) != nil || req.ID == "" {
			fail(w, 400)
			return
		}
		if e := s.revokeDevice(req.ID); e != nil {
			fail(w, 500)
			return
		}
		write(w, map[string]bool{"ok": true})
	})
	srv := &http.Server{Handler: m, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); _ = srv.Close(); _ = os.Remove(path) }()
	e = srv.Serve(ln)
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
func AdminClient(dir string) *http.Client {
	return localipc.Client(dir, "control")
}
func (s *Server) String() string {
	return fmt.Sprintf("AgentDeck %s rotation=%s", s.PublicURL, s.Rotation)
}
