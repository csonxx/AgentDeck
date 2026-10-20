package agent

import (
	"agentdeck/internal/store"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

const desktopFrameLimit = 32 << 20

func readDesktopFrame(c net.Conn) (map[string]any, error) {
	var h [4]byte
	if _, err := io.ReadFull(c, h[:]); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint32(h[:])
	if size == 0 || size > desktopFrameLimit {
		return nil, errors.New("desktop frame exceeds bound")
	}
	raw := make([]byte, size)
	if _, err := io.ReadFull(c, raw); err != nil {
		return nil, err
	}
	var v map[string]any
	err := json.Unmarshal(raw, &v)
	return v, err
}
func (c *desktopConnection) following(id string, following bool) error {
	raw, err := json.Marshal(map[string]any{"type": "broadcast", "method": "thread-stream-following-changed", "version": 1, "sourceClientId": c.client, "targetClientIds": []string{c.owner}, "params": map[string]any{"hostId": "local", "conversationId": id, "following": following}})
	if err != nil {
		return err
	}
	frame := make([]byte, 4+len(raw))
	binary.LittleEndian.PutUint32(frame, uint32(len(raw)))
	copy(frame[4:], raw)
	_, err = io.Copy(c.conn, bytes.NewReader(frame))
	return err
}

// Follow only subscribes to the discovered owner. It never starts/resumes a turn
// or changes permissions. Raw desktop state remains in bounded process memory.
func (b *DesktopBridge) Follow(ctx context.Context, id string, consume func(map[string]any) error) error {
	c, err := b.connect(ctx, id)
	if err != nil {
		return err
	}
	defer c.conn.Close()
	stop := context.AfterFunc(ctx, func() { c.conn.Close() })
	defer stop()
	if err = c.following(id, true); err != nil {
		return err
	}
	defer func() { c.conn.SetWriteDeadline(time.Now().Add(time.Second)); _ = c.following(id, false) }()
	var state any
	var revision float64 = -1
	var budget int
	type result struct {
		frame map[string]any
		err   error
	}
	frames := make(chan result, 8)
	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	go func() {
		for {
			c.conn.SetReadDeadline(time.Now().Add(45 * time.Second))
			frame, err := readDesktopFrame(c.conn)
			select {
			case frames <- result{frame, err}:
			case <-readCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()
	dirty := false
	publish := func() error {
		if state == nil || !dirty {
			return nil
		}
		dirty = false
		return consume(obj(state))
	}
	for {
		var frame map[string]any
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if err := publish(); err != nil {
				return err
			}
			continue
		case result := <-frames:
			if result.err != nil {
				_ = publish()
				return result.err
			}
			frame = result.frame
		}

		c.captureQueue(frame)
		if frame["type"] != "broadcast" || frame["method"] != "thread-stream-state-changed" {
			continue
		}
		if frame["sourceClientId"] != c.owner || frame["version"] != float64(11) {
			continue
		}
		p := obj(frame["params"])
		if p["hostId"] != "local" || p["conversationId"] != id {
			continue
		}
		change := obj(p["change"])
		next, ok := change["revision"].(float64)
		if !ok || next < 0 || next != float64(int64(next)) {
			return errors.New("invalid desktop revision")
		}
		switch change["type"] {
		case "snapshot":
			state = change["conversationState"]
			budget = 0
			if obj(state)["id"] != id || (str(obj(state)["hostId"]) != "" && obj(state)["hostId"] != "local") {
				return errors.New("desktop snapshot identity mismatch")
			}
		case "patches":
			if state == nil || change["baseRevision"] != revision || next <= revision {
				return errors.New("desktop revision gap")
			}
			patches := arr(change["patches"])
			if len(patches) > 10000 {
				return errors.New("too many desktop patches")
			}
			for _, raw := range patches {
				patch := obj(raw)
				value, _ := json.Marshal(patch["value"])
				budget += len(value)
				if budget > 64<<20 {
					return errors.New("desktop patch budget exceeded")
				}
				state, err = applyDesktopPatch(state, arr(patch["path"]), str(patch["op"]), patch["value"])
				if err != nil {
					return err
				}
			}
		default:
			continue
		}
		if obj(state)["id"] != id || (str(obj(state)["hostId"]) != "" && obj(state)["hostId"] != "local") {
			return errors.New("desktop patched identity mismatch")
		}
		revision = next
		dirty = true
		if change["type"] == "snapshot" || desktopTurnStatus(obj(state)) != "running" {
			if err = publish(); err != nil {
				return err
			}
		}
	}
}

// Immer patches use arrays of typed path components, not JSON Pointer strings.
func applyDesktopPatch(node any, path []any, op string, value any) (any, error) {
	if len(path) > 64 {
		return nil, errors.New("desktop patch path too deep")
	}
	if len(path) == 0 {
		if op == "add" || op == "replace" {
			return value, nil
		}
		return nil, errors.New("invalid desktop root patch")
	}
	switch n := node.(type) {
	case map[string]any:
		key, ok := path[0].(string)
		if !ok {
			return nil, errors.New("invalid desktop object path")
		}
		if len(path) == 1 {
			switch op {
			case "add", "replace":
				n[key] = value
			case "remove":
				delete(n, key)
			default:
				return nil, errors.New("invalid desktop patch operation")
			}
			return n, nil
		}
		child, exists := n[key]
		if !exists {
			return nil, errors.New("missing desktop patch parent")
		}
		updated, err := applyDesktopPatch(child, path[1:], op, value)
		if err != nil {
			return nil, err
		}
		n[key] = updated
		return n, nil
	case []any:
		if path[0] == "length" && len(path) == 1 && op == "replace" {
			v, ok := value.(float64)
			if !ok || v < 0 || v > float64(len(n)) || v != float64(int(v)) {
				return nil, errors.New("invalid desktop array length")
			}
			return n[:int(v)], nil
		}
		f, ok := path[0].(float64)
		if !ok || f < 0 || f != float64(int(f)) || f > float64(len(n)) {
			return nil, errors.New("invalid desktop array index")
		}
		i := int(f)
		if len(path) == 1 {
			switch op {
			case "add":
				n = append(n, nil)
				copy(n[i+1:], n[i:])
				n[i] = value
			case "replace":
				if i == len(n) {
					return nil, errors.New("desktop replace out of bounds")
				}
				n[i] = value
			case "remove":
				if i == len(n) {
					return nil, errors.New("desktop remove out of bounds")
				}
				n = append(n[:i], n[i+1:]...)
			default:
				return nil, errors.New("invalid desktop array operation")
			}
			return n, nil
		}
		if i == len(n) {
			return nil, errors.New("missing desktop array parent")
		}
		updated, err := applyDesktopPatch(n[i], path[1:], op, value)
		if err != nil {
			return nil, err
		}
		n[i] = updated
		return n, nil
	default:
		return nil, errors.New("invalid desktop patch parent")
	}
}
func desktopTurns(state map[string]any) []any {
	history := obj(obj(state["turnHistory"])["history"])
	entities := obj(history["entitiesByKey"])
	turns := []any{}
	for _, island := range arr(history["islands"]) {
		for _, entry := range arr(obj(island)["entries"]) {
			if turn := entities[str(obj(entry)["value"])]; turn != nil {
				turns = append(turns, turn)
			}
		}
	}
	if len(turns) == 0 {
		return arr(state["turns"])
	}
	return turns
}
func desktopTurnStatus(state map[string]any) string {
	turns := desktopTurns(state)
	if len(turns) == 0 {
		return ""
	}
	switch str(obj(turns[len(turns)-1])["status"]) {
	case "inProgress":
		return "running"
	case "completed":
		return "completed"
	case "interrupted":
		return "interrupted"
	case "failed":
		return "failed"
	}
	if str(obj(turns[len(turns)-1])["status"]) != "" {
		return "unknown"
	}
	return ""
}

type desktopProjection struct {
	m              *Manager
	session        store.Session
	seen           map[string]string
	itemSeen       map[string]string
	questions      map[string]string
	asyncQuestions map[string]string
	asyncScanStamp string
}

func (p *desktopProjection) publish(state map[string]any) error {
	if cwd := str(state["cwd"]); cwd != "" && cwd != p.session.Project {
		return errors.New("desktop live project mismatch")
	}
	if p.itemSeen == nil {
		p.itemSeen = map[string]string{}
	}
	var pending []store.Event
	pendingBytes := 0
	hashes := map[string]string{}
	flush := func() error {
		if err := p.appendBatch(pending); err != nil {
			return err
		}
		for id, h := range hashes {
			p.itemSeen[id] = h
		}
		pending = nil
		pendingBytes = 0
		hashes = map[string]string{}
		return nil
	}
	turns := desktopTurns(state)
	if len(turns) > 2 {
		turns = turns[len(turns)-2:]
	}
	for _, rawTurn := range turns {
		turn := obj(rawTurn)
		for _, rawItem := range arr(turn["items"]) {
			item := obj(rawItem)
			if item["type"] == "steeringUserMessage" && item["status"] == "accepted" {
				input := arr(item["input"])
				if len(input) == 1 && obj(input[0])["type"] == "text" && parseAsyncReplies(str(obj(input[0])["text"])) != nil {
					item = map[string]any{"id": item["id"], "type": "userMessage", "content": input}
				}
			}
			if item["type"] == "userMessage" && str(item["clientId"]) != "" {
				copy := map[string]any{}
				for k, v := range item {
					copy[k] = v
				}
				copy["id"] = item["clientId"]
				item = copy
			}
			id := str(item["id"])
			if id == "" {
				continue
			}
			switch str(item["type"]) {
			case "agentMessage", "userMessage", "commandExecution", "fileChange", "mcpToolCall", "reasoning", "imageGeneration":
			default:
				continue
			}
			// Never persist raw state/settings/context or encrypted reasoning.
			allowed := map[string]any{}
			for _, key := range []string{"id", "type", "text", "content", "summary", "command", "aggregatedOutput", "changes", "server", "tool", "arguments", "result", "imagePath"} {
				if value, ok := item[key]; ok {
					allowed[key] = value
				}
			}
			raw, _ := json.Marshal(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": p.session.ID, "item": allowed}})
			itemHash := digest(string(raw))
			if p.itemSeen[id] == itemHash {
				continue
			}
			st := State{Session: p.session.ID, CWD: p.session.Project}
			events, err := p.m.Normalizer.Parse(raw, "codex", "desktop-live", &st)
			if err != nil {
				return err
			}
			for _, event := range events {
				if event.Kind == "tool_result" && event.Text == "" && event.Media == "" {
					continue
				}
				event.Status = ""
				pending = append(pending, event)
				pendingBytes += len(event.Text) + len(event.Media)
			}
			hashes[id] = itemHash
			if len(pending) >= 128 || pendingBytes >= 512<<10 {
				if err = flush(); err != nil {
					return err
				}
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	if err := p.publishQuestions(state); err != nil {
		return err
	}
	if err := p.publishAsyncQuestions(state); err != nil {
		return err
	}
	status := desktopTurnStatus(state)
	if status != "" {
		turn := obj(turns[len(turns)-1])
		event := store.Event{Session: p.session.ID, Agent: "codex", Project: p.session.Project, Source: "desktop-live", Kind: "status", Item: "desktop-turn/" + str(turn["turnId"]), Status: status, Issue: desktopSessionIssue(state)}
		if err := p.append(event); err != nil {
			return err
		}
	}
	return nil
}
func (p *desktopProjection) append(event store.Event) error {
	return p.appendBatch([]store.Event{event})
}
func (p *desktopProjection) appendBatch(events []store.Event) error {
	pending := []store.Event{}
	fingerprints := map[string]string{}
	for _, event := range events {
		key := event.Item + "/" + event.Kind
		body, _ := json.Marshal([]any{event.Text, event.Media, event.Status, event.Issue})
		fingerprint := digest(string(body))
		if p.seen[key] == fingerprint {
			continue
		}
		event.ID = "desktop-live/" + UUID()
		pending = append(pending, event)
		fingerprints[key] = fingerprint
	}
	if len(pending) == 0 {
		return nil
	}
	if _, err := p.m.Store.AppendImported(pending, false); err != nil {
		return err
	}
	for key, value := range fingerprints {
		p.seen[key] = value
	}
	return nil
}

type desktopLease struct {
	refs   int
	cancel context.CancelFunc
	done   chan struct{}
}

// One owner connection per watched thread, shared across phones and send leases.
func (m *Manager) WatchDesktop(ctx context.Context, id string) func() {
	var session store.Session
	if m.Normalizer == nil || m.Store.Get("sessions", id, &session) != nil {
		return func() {}
	}
	follow, ok := m.Desktop.(interface {
		Follow(context.Context, string, func(map[string]any) error) error
	})
	claude := session.Agent == "claude" && session.Source == "claude-desktop" && m.ClaudeDesktopDebugger
	if !claude && (!ok || session.Agent != "codex" || (session.Managed && session.Source != "desktop")) {
		return func() {}
	}
	if _, err := m.allowed(session.Project); err != nil {
		return func() {}
	}
	m.desktopMu.Lock()
	if m.desktopClosed {
		m.desktopMu.Unlock()
		return func() {}
	}
	if m.desktopLeases == nil {
		m.desktopLeases = map[string]*desktopLease{}
	}
	lease := m.desktopLeases[id]
	if lease == nil {
		if len(m.desktopLeases) >= 32 {
			m.desktopMu.Unlock()
			return func() {}
		}
		if m.desktopContext == nil {
			base := m.Context
			if base == nil {
				base = context.Background()
			}
			m.desktopContext, m.desktopStop = context.WithCancel(base)
		}
		life, cancel := context.WithCancel(m.desktopContext)
		lease = &desktopLease{cancel: cancel, done: make(chan struct{})}
		m.desktopLeases[id] = lease
		m.desktopWG.Add(1)
		go func() {
			defer m.desktopWG.Done()
			defer close(lease.done)
			p := &desktopProjection{m: m, session: session, seen: map[string]string{}}
			for life.Err() == nil {
				p.seen = map[string]string{}
				p.itemSeen = map[string]string{}
				if claude {
					_ = m.followClaudeDesktop(life, session)
				} else {
					err := follow.Follow(life, id, p.publish)
					if err != nil && life.Err() == nil {
						// Relay connectivity is not proof that the native owner
						// can still be observed. Keep history, stop stale spinners.
						_ = m.Store.MarkDesktopUnobserved(id)
					}
				}
				select {
				case <-life.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
		}()
	}
	lease.refs++
	m.desktopMu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			m.desktopMu.Lock()
			defer m.desktopMu.Unlock()
			lease.refs--
			if lease.refs == 0 && m.desktopLeases[id] == lease {
				delete(m.desktopLeases, id)
				lease.cancel()
			}
		})
	}
	if ctx != nil {
		stop := context.AfterFunc(ctx, release)
		return func() { stop(); release() }
	}
	return release
}
func (m *Manager) keepDesktopSend(id string) {
	if m.Normalizer == nil {
		return
	}
	var session store.Session
	_ = m.Store.Get("sessions", id, &session)
	if _, ok := m.Desktop.(interface {
		Follow(context.Context, string, func(map[string]any) error) error
	}); !ok && !(m.ClaudeDesktopDebugger && session.Agent == "claude" && session.Source == "claude-desktop") {
		return
	}
	release := m.WatchDesktop(nil, id)
	m.desktopMu.Lock()
	if m.desktopClosed || m.desktopContext == nil {
		m.desktopMu.Unlock()
		release()
		return
	}
	ctx, cancel := context.WithTimeout(m.desktopContext, 30*time.Minute)
	m.desktopWG.Add(1)
	m.desktopMu.Unlock()
	go func() {
		defer m.desktopWG.Done()
		defer cancel()
		defer release()
		t := time.NewTicker(time.Second)
		defer t.Stop()
		// A confirmed Claude human record may finish before the first observer
		// tick. Do not retain an otherwise idle send lease for thirty minutes.
		running := session.Agent == "claude"
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				var s store.Session
				if m.Store.Get("sessions", id, &s) != nil {
					return
				}
				if s.Status == "running" {
					running = true
				}
				if running && (s.Status == "completed" || s.Status == "failed" || s.Status == "interrupted" || s.Status == "idle") {
					return
				}
			}
		}
	}()
}
func (m *Manager) closeDesktop() {
	m.desktopMu.Lock()
	m.desktopClosed = true
	if m.desktopStop != nil {
		m.desktopStop()
	}
	for _, l := range m.desktopLeases {
		l.cancel()
	}
	m.desktopMu.Unlock()
	m.desktopWG.Wait()
}
