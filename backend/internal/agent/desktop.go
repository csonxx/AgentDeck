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
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Desktop coordination is a separate, opt-in owner-only Unix interface. The
// encrypted public protocol exposes project-scoped sends, never arbitrary RPCs.
// Wire versions match the inspected desktop coordination protocol; incompatible
// or unavailable owners fail explicitly and never fall back to a fork.
type DesktopController interface {
	Read(context.Context, string) (DesktopThread, error)
	Send(context.Context, string, string, string, string) error
}
type DesktopThread struct {
	ID      string
	Kind    string
	Host    string
	Project string
}
type DesktopBridge struct {
	Socket         string
	QueueStatePath string
	OpenThread     func(context.Context, string) error
	wakeLocks      sync.Map
	queueMu        sync.Mutex
	queues         map[string]nativeQueueCache
	queueSerial    uint64
}

var errDesktopNoOwner = errors.New("电脑 Codex 尚未加载原会话")

// Explicit input may wake the saved native chat. Passive reads/follows never
// navigate the user's desktop, and a sent turn is never retried here.
func (b *DesktopBridge) connectForInput(ctx context.Context, id string) (*desktopConnection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	guard, _ := b.wakeLocks.LoadOrStore(id, &sync.Mutex{})
	lock := guard.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	c, err := b.connect(ctx, id)
	if !errors.Is(err, errDesktopNoOwner) {
		return c, err
	}
	if !claudeNativeID.MatchString(id) {
		return nil, errors.New("Codex 原会话 ID 无效，无法自动唤起")
	}
	open := b.OpenThread
	if open == nil {
		open = openCodexThread
	}
	if err = open(ctx, id); err != nil {
		return nil, errors.New("无法自动唤起电脑 Codex 原会话，请检查桌面端是否正常运行")
	}
	life, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for life.Err() == nil {
		c, err = b.connect(life, id)
		if !errors.Is(err, errDesktopNoOwner) {
			return c, err
		}
		select {
		case <-life.Done():
		case <-time.After(200 * time.Millisecond):
		}
	}
	return nil, errors.New("电脑 Codex 已尝试加载原会话，但执行端尚未就绪；消息未发送，请稍后重试")
}

func (b *DesktopBridge) ReadForInput(ctx context.Context, id string) (DesktopThread, error) {
	c, err := b.connectForInput(ctx, id)
	if err != nil {
		return DesktopThread{}, err
	}
	c.conn.Close()
	return DesktopThread{ID: id, Kind: "codex", Host: "local"}, nil
}

func readDesktopForInput(d DesktopController, ctx context.Context, id string) (DesktopThread, error) {
	if reader, ok := d.(interface {
		ReadForInput(context.Context, string) (DesktopThread, error)
	}); ok {
		return reader.ReadForInput(ctx, id)
	}
	return d.Read(ctx, id)
}

func (b *DesktopBridge) Validate() error {
	if !filepath.IsAbs(b.Socket) {
		return errors.New("desktop socket must be absolute")
	}
	s, err := os.Lstat(b.Socket)
	if err != nil || s.Mode()&os.ModeSocket == 0 || s.Mode().Perm()&0077 != 0 || !desktopOwned(s) {
		return errors.New("desktop bridge requires an owner-only Unix socket")
	}
	p, err := os.Lstat(filepath.Dir(b.Socket))
	if err != nil || !p.IsDir() || p.Mode().Perm()&0077 != 0 || !desktopOwned(p) {
		return errors.New("desktop bridge requires an owner-only socket directory")
	}
	return nil
}

type desktopConnection struct {
	conn          net.Conn
	client, owner string
	bridge        *DesktopBridge
	thread        string
	queueRevision uint64
}

func (b *DesktopBridge) connect(ctx context.Context, id string) (*desktopConnection, error) {
	if err := b.Validate(); err != nil {
		return nil, errors.New("电脑 Codex 未连接，请启动桌面端并检查工作站桥接配置")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", b.Socket)
	if err != nil {
		return nil, errors.New("电脑 Codex 未连接")
	}
	deadline := time.Now().Add(20 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)
	c := &desktopConnection{conn: conn, client: "initializing-client", bridge: b, thread: id}
	v, err := c.request("initialize", 0, map[string]any{"clientType": "agentdeck"}, "")
	if err != nil {
		conn.Close()
		return nil, err
	}
	c.client = str(obj(v["result"])["clientId"])
	if c.client == "" {
		conn.Close()
		return nil, errors.New("电脑 Codex 接口版本不支持")
	}
	v, err = c.request("thread-owner-discovery", 1, map[string]any{"hostId": "local", "conversationId": id}, "")
	if err != nil {
		conn.Close()
		return nil, err
	}
	c.owner = str(v["handledByClientId"])
	if c.owner == "" {
		conn.Close()
		return nil, errors.New("电脑原会话不可用")
	}
	b.queueOwner(id, c.owner)
	return c, nil
}
func (c *desktopConnection) request(method string, version int, params any, target string) (map[string]any, error) {
	id := UUID()
	v := map[string]any{"type": "request", "requestId": id, "sourceClientId": c.client, "version": version, "method": method, "params": params, "timeoutMs": 15000}
	if target != "" {
		v["targetClientId"] = target
	}
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	frame := make([]byte, 4+len(body))
	binary.LittleEndian.PutUint32(frame, uint32(len(body)))
	copy(frame[4:], body)
	if _, err = io.Copy(c.conn, bytes.NewReader(frame)); err != nil {
		return nil, errors.New("电脑 Codex 请求未确认，请检查原会话后再发送")
	}
	for frames := 0; frames < 1000; frames++ {
		var head [4]byte
		if _, err = io.ReadFull(c.conn, head[:]); err != nil {
			break
		}
		size := binary.LittleEndian.Uint32(head[:])
		if size == 0 || size > 8<<20 {
			break
		}
		raw := make([]byte, size)
		if _, err = io.ReadFull(c.conn, raw); err != nil {
			break
		}
		var out map[string]any
		if json.Unmarshal(raw, &out) != nil {
			break
		}
		c.captureQueue(out)
		if out["type"] != "response" || out["requestId"] != id {
			continue
		}
		if out["resultType"] != "success" {
			if method == "thread-owner-discovery" && out["error"] == "no-client-found" {
				return nil, errDesktopNoOwner
			}
			return nil, errors.New("电脑 Codex 拒绝了操作；请检查原会话状态")
		}
		if target != "" && out["handledByClientId"] != target {
			return nil, errors.New("电脑会话执行端已变更，请重新连接")
		}
		return out, nil
	}
	return nil, errors.New("电脑 Codex 未确认接收；请检查原会话，勿重复发送")
}
func (b *DesktopBridge) Read(ctx context.Context, id string) (DesktopThread, error) {
	c, err := b.connect(ctx, id)
	if err != nil {
		return DesktopThread{}, err
	}
	defer c.conn.Close()
	// The source log supplies the authorized project; the owner discovery supplies
	// exact desktop identity. Sends include that authorized cwd explicitly.
	return DesktopThread{ID: id, Kind: "codex", Host: "local"}, nil
}
func (b *DesktopBridge) Send(ctx context.Context, id, text, request, project string) error {
	return b.SendInput(ctx, id, text, request, project, TurnOptions{}, []any{map[string]any{"type": "text", "text": text}})
}
func (b *DesktopBridge) SendInput(ctx context.Context, id, text, request, project string, o TurnOptions, inputs []any) error {
	c, err := b.connectForInput(ctx, id)
	if err != nil {
		return err
	}
	defer c.conn.Close()
	stop := context.AfterFunc(nonNil(ctx), func() { c.conn.Close() })
	defer stop()
	state, err := c.snapshot(id, project)
	if err != nil {
		return &InputNotSentError{Err: err}
	}
	if err = checkSessionInput(store.Session{Issue: desktopSessionIssue(state)}); err != nil {
		return err
	}
	// Desktop renders the optimistic input before app-server applies schema
	// defaults. Its text renderer expects this array to exist even when empty.
	normalized := make([]any, len(inputs))
	for i, raw := range inputs {
		v := obj(raw)
		copy := make(map[string]any, len(v)+1)
		for k, value := range v {
			copy[k] = value
		}
		if str(copy["type"]) == "text" && copy["text_elements"] == nil {
			copy["text_elements"] = []any{}
		}
		normalized[i] = copy
	}
	req := map[string]any{"threadId": id, "cwd": project, "input": normalized, "clientUserMessageId": request}
	if o.Model != "" {
		req["model"] = o.Model
	}
	if o.Effort != "" {
		req["effort"] = o.Effort
	}
	applySpeed(req, o)
	_, err = c.request("thread-follower-start-turn", 2, map[string]any{"conversationId": id, "turnStart": map[string]any{"request": req, "context": map[string]any{"inheritThreadSettings": true}}}, c.owner)
	return err
}

func (c *desktopConnection) snapshot(id, project string) (map[string]any, error) {
	if err := c.following(id, true); err != nil {
		return nil, err
	}
	defer c.following(id, false)
	for {
		frame, err := readDesktopFrame(c.conn)
		if err != nil {
			return nil, err
		}
		c.captureQueue(frame)
		p := obj(frame["params"])
		if frame["method"] != "thread-stream-state-changed" || frame["version"] != float64(11) || frame["sourceClientId"] != c.owner || p["hostId"] != "local" || p["conversationId"] != id {
			continue
		}
		change := obj(p["change"])
		if change["type"] != "snapshot" {
			continue
		}
		state := obj(change["conversationState"])
		if state["id"] != id || str(state["cwd"]) != project {
			return nil, errors.New("桌面会话项目已变更，请重新连接")
		}
		return state, nil
	}
}
func (b *DesktopBridge) CurrentStatus(ctx context.Context, id, project string) (string, error) {
	c, e := b.connect(ctx, id)
	if e != nil {
		return "", e
	}
	defer c.conn.Close()
	if ctx != nil {
		stop := context.AfterFunc(ctx, func() { c.conn.Close() })
		defer stop()
	}
	state, e := c.snapshot(id, project)
	if e != nil {
		return "", e
	}
	return desktopTurnStatus(state), nil
}
func (b *DesktopBridge) SteerInput(ctx context.Context, id, text, request, project string, inputs []any) error {
	c, e := b.connect(ctx, id)
	if e != nil {
		return e
	}
	defer c.conn.Close()
	if ctx != nil {
		stop := context.AfterFunc(ctx, func() { c.conn.Close() })
		defer stop()
	}
	state, e := c.snapshot(id, project)
	if e != nil {
		return e
	}
	if e = checkSessionInput(store.Session{Issue: desktopSessionIssue(state)}); e != nil {
		return e
	}
	if desktopTurnStatus(state) != "running" {
		return errors.New("没有正在执行的任务，请正常发送")
	}
	for _, raw := range inputs {
		v := obj(raw)
		if v["type"] == "text" && v["text_elements"] == nil {
			v["text_elements"] = []any{}
		}
	}
	restore := map[string]any{"id": request, "text": text, "images": []any{}, "context": map[string]any{"commentAttachments": []any{}}}
	_, e = c.request("thread-follower-steer-turn", 1, map[string]any{"conversationId": id, "input": inputs, "clientUserMessageId": request, "restoreMessage": restore, "attachments": []any{}}, c.owner)
	return e
}

// PauseGoal pauses the owner's scheduler before interrupting its original turn.
func (b *DesktopBridge) PauseGoal(ctx context.Context, id, project string) error {
	c, e := b.connect(ctx, id)
	if e != nil {
		return e
	}
	defer c.conn.Close()
	if ctx != nil {
		stop := context.AfterFunc(ctx, func() { c.conn.Close() })
		defer stop()
	}
	if _, e = c.snapshot(id, project); e != nil {
		return e
	}
	v, e := c.request("thread-follower-interrupt-turn", 3, map[string]any{"conversationId": id, "mode": "user-stop"}, c.owner)
	if e != nil {
		return e
	}
	r := obj(obj(v["result"])["result"])
	if str(r["goalPauseError"]) != "" {
		return errors.New("电脑目标暂停失败，请检查电脑会话")
	}
	if r["ok"] != true {
		return errors.New("电脑未确认暂停目标")
	}
	return nil
}
