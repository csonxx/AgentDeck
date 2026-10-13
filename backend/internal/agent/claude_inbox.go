package agent

import (
	"agentdeck/internal/claudestdio"
	"agentdeck/internal/store"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const claudeInboxUnconfirmed = "已向电脑 Claude 原会话投递，但尚未确认接收；请检查电脑的消息接收设置或等待当前工具结束，勿重复发送"
const claudeInboxIdleUnconfirmed = "电脑 Claude 尚未确认接收；当前会话空闲，请检查 crossSessionInbound 收信设置。消息可能被暂存，勿重复发送"
const claudeInboxOptions = "此电脑 Claude 接口未提供模型参数修改，请在电脑会话中修改"
const claudeStdioRestart = "Claude 原生输入桥接已安装，请在当前任务结束后重新启动电脑 Claude 并打开原会话"
const claudeStdioInstall = "电脑 Claude 原会话需要原生输入桥接，请在本机安装后重新打开原会话"

type claudeInboxOwner struct {
	claudeProcessRecord
	CWD      string `json:"cwd"`
	Socket   string `json:"messagingSocketPath"`
	Protocol int    `json:"peerProtocol"`
}

type InputNotSentError struct{ Err error }

func (e *InputNotSentError) Error() string { return e.Err.Error() }
func (e *InputNotSentError) Unwrap() error { return e.Err }

func InputDefinitelyNotSent(err error) bool {
	var target *InputNotSentError
	return errors.As(err, &target)
}

// NativeSubmission returns transport acknowledgement, never inference completion.
// The encrypted command journal persists this small result for idempotent replay.
func (m *Manager) NativeSubmission(session, request string) any {
	var delivery store.Delivery
	if m.Store.Get("deliveries", request, &delivery) == nil && delivery.Session == session && delivery.State == "queued" {
		return map[string]string{"delivery": "queued"}
	}
	var receipt map[string]string
	if m.Store.Get("agentconfig", "native-input/"+session+"/"+request, &receipt) == nil {
		return receipt
	}
	return nil
}

func (m *Manager) claudeProcessRoot() string {
	if m.ClaudeProcesses != "" {
		return m.ClaudeProcesses
	}
	if m.ClaudeGoalRoot != "" {
		return filepath.Join(filepath.Dir(filepath.Clean(m.ClaudeGoalRoot)), "sessions")
	}
	return ""
}

// Discovery is server-side only. A phone cannot supply a PID, socket, path,
// permission class or arbitrary native frame. Match both PID birth time and
// original session/project, excluding runtimes this Manager already owns.
func (m *Manager) claudeInbox(ss store.Session) (claudeInboxOwner, bool, error) {
	var zero claudeInboxOwner
	if !m.ClaudeDesktopInput {
		return zero, false, nil
	}
	root := m.claudeProcessRoot()
	if root == "" {
		return zero, false, nil
	}
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return zero, false, nil
	}
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 || !desktopOwned(info) {
		return zero, false, errors.New("Claude 进程目录权限不安全，无法连接原会话")
	}
	files, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		return zero, false, err
	}
	snapshot := m.ClaudeProcessSnapshot
	if snapshot == nil {
		snapshot = claudeProcessSnapshot
	}
	processes, err := snapshot()
	if err != nil {
		return zero, false, errors.New("无法确认 Claude 会话占用状态，请在电脑检查后重试")
	}
	ownPID := 0
	m.mu.Lock()
	if r := m.runs[ss.ID]; r != nil {
		select {
		case <-r.done:
		default:
			if r.cmd != nil && r.cmd.Process != nil {
				ownPID = r.cmd.Process.Pid
			}
		}
	}
	m.mu.Unlock()
	var selected claudeInboxOwner
	found := false
	for _, p := range files {
		st, err := os.Lstat(p)
		if err != nil || !st.Mode().IsRegular() || st.Size() > 64<<10 || st.Mode().Perm()&0022 != 0 || !desktopOwned(st) {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var r claudeInboxOwner
		if json.Unmarshal(b, &r) != nil || r.Session != ss.ID || r.PID == ownPID || r.PID <= 0 || filepath.Base(p) != strconv.Itoa(r.PID)+".json" || r.Parked != "" {
			continue
		}
		if r.Start == "" || processes[r.PID] != strings.Join(strings.Fields(r.Start), " ") {
			continue
		}
		// An active owner without a compatible inbox must never trigger a second CLI.
		if found {
			return zero, true, errors.New("多个 Claude 进程持有同一会话，请先在电脑确认")
		}
		found = true
		selected = r
	}
	if !found {
		return zero, false, nil
	}
	cwd, err := filepath.EvalSymlinks(selected.CWD)
	project, e := filepath.EvalSymlinks(ss.Project)
	if err != nil || e != nil || cwd != project {
		return zero, true, errors.New("电脑会话与授权项目不匹配")
	}
	if _, err = m.allowed(cwd); err != nil {
		return zero, true, err
	}
	if selected.Protocol != 1 || selected.Socket == "" {
		return zero, true, errors.New("电脑 Claude 未提供兼容的原会话消息接口，请更新并重新打开该会话")
	}
	if err = validateClaudeInboxSocket(selected.Socket); err != nil {
		return zero, true, err
	}
	return selected, true, nil
}

func validateClaudeInboxSocket(path string) error {
	failure := errors.New("电脑 Claude 原会话消息接口权限不安全或已断开")
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return failure
	}
	st, err := os.Lstat(path)
	if err != nil || st.Mode()&os.ModeSocket == 0 || st.Mode().Perm()&0077 != 0 || !desktopOwned(st) {
		return failure
	}
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil || !parent.IsDir() || parent.Mode().Perm()&0077 != 0 || !desktopOwned(parent) {
		return failure
	}
	return nil
}

type claudeReceiptFile struct {
	path   string
	offset int64
}

func (m *Manager) claudeReceiptFiles(id string) []claudeReceiptFile {
	if m.ClaudeGoalRoot == "" || !claudeNativeID.MatchString(id) {
		return nil
	}
	paths, _ := filepath.Glob(filepath.Join(m.ClaudeGoalRoot, "*", id+".jsonl"))
	out := []claudeReceiptFile{}
	for _, p := range paths {
		if st, e := os.Lstat(p); e == nil && st.Mode().IsRegular() {
			out = append(out, claudeReceiptFile{p, st.Size()})
		}
	}
	return out
}

// The inbox accepts newline JSON but sends no synchronous receipt. Confirm by
// the native transcript's exact input UUID, reading only newly appended bytes.
// A successful write alone must never be presented as confirmed delivery.
func (m *Manager) claudeInputRecorded(id, request string, files *[]claudeReceiptFile) bool {
	known := map[string]bool{}
	for _, f := range *files {
		known[f.path] = true
	}
	for _, f := range m.claudeReceiptFiles(id) {
		if !known[f.path] {
			f.offset = 0
			*files = append(*files, f)
		}
	}
	for i := range *files {
		f := &(*files)[i]
		in, e := os.Open(f.path)
		if e != nil {
			continue
		}
		st, e := in.Stat()
		if e != nil || st.Size() < f.offset {
			in.Close()
			continue
		}
		reader := bufio.NewReader(io.NewSectionReader(in, f.offset, 2<<20))
		for {
			line, e := reader.ReadBytes('\n')
			if e != nil {
				break
			}
			f.offset += int64(len(line))
			var r struct {
				Type       string `json:"type"`
				UUID       string `json:"uuid"`
				Session    string `json:"sessionId"`
				SessionAlt string `json:"session_id"`
			}
			if json.Unmarshal(line, &r) == nil && r.Type == "user" && r.UUID == request && (r.Session == id || r.SessionAlt == id) {
				in.Close()
				return true
			}
		}
		in.Close()
	}
	return false
}

func (m *Manager) sendClaudeInbox(ss store.Session, owner claudeInboxOwner, text, request string, o TurnOptions, files []InputAttachment) (retErr error) {
	wrote := false
	defer func() {
		if retErr != nil && !wrote {
			retErr = &InputNotSentError{Err: retErr}
		}
	}()
	if (o.Model != "" && o.Model != "default") || o.Effort != "" {
		return errors.New(claudeInboxOptions)
	}
	inputs, display, err := m.prepareInput(ss.ID, text, request, files)
	if err != nil {
		return err
	}
	// Serialize per-session sends; revalidate after waiting for the prior send.
	// Queue dispatch already owns the delivery lock for ss.ID.
	lock := m.deliveryLock("claude-inbox/" + ss.ID)
	lock.Lock()
	defer lock.Unlock()
	current, found, err := m.claudeInbox(ss)
	if err != nil {
		return err
	}
	if !found || current.PID != owner.PID || current.Start != owner.Start || current.Socket != owner.Socket {
		return errors.New("电脑会话执行端已变更，请重新连接")
	}
	observed := m.claudeReceiptFiles(ss.ID)
	ctx := m.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	socket := owner.Socket
	var bridge *claudestdio.Record
	if m.ControlDir != "" {
		root := filepath.Join(m.ControlDir, "cs")
		if r, e := claudestdio.ReadRecord(root, owner.PID); e == nil {
			project, e := filepath.EvalSymlinks(ss.Project)
			if e != nil || r.Start != strings.Join(strings.Fields(owner.Start), " ") || r.Project != project {
				return errors.New("电脑 Claude 原生输入桥接与会话不匹配")
			}
			if e = validateClaudeInboxSocket(r.Socket); e != nil {
				return e
			}
			bridge = &r
			socket = r.Socket
		} else if !os.IsNotExist(e) {
			return errors.New("电脑 Claude 原生输入桥接状态无效，请在电脑检查")
		} else if _, e = os.Stat(filepath.Join(root, "enabled")); e == nil {
			return errors.New(claudeStdioRestart)
		} else {
			return errors.New(claudeStdioInstall)
		}
	}
	if bridge == nil {
		for _, f := range files {
			if strings.HasPrefix(f.MIME, "image/") {
				return errors.New("电脑 Claude 原会话消息接口暂不支持图片输入；请在电脑发送图片")
			}
		}
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return errors.New("电脑 Claude 原会话消息接口已断开，请重试")
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	conn.SetWriteDeadline(deadline)
	// Deliberately assert no peer permission class and use no messaging token.
	// Native inbound hold/refuse rules and tool permissions remain authoritative.
	var frame any = map[string]any{"type": "user", "uuid": request, "session_id": ss.ID, "message": map[string]any{"role": "user", "content": display}}
	if bridge != nil {
		frame = claudestdio.Input{Session: ss.ID, UUID: request, Content: claudeInput(inputs)}
	}
	b, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	n, writeErr := conn.Write(b)
	wrote = n > 0
	if writeErr != nil || n != len(b) {
		return errors.New(claudeInboxUnconfirmed)
	}
	if bridge != nil {
		conn.SetReadDeadline(deadline)
		var reply claudestdio.Reply
		if json.NewDecoder(io.LimitReader(conn, 4096)).Decode(&reply) != nil || !reply.Written {
			return errors.New("电脑 Claude 原生输入未确认，请刷新核对，勿重复发送")
		}
		// The original CLI can defer logging input while a turn/tool is running.
		// A verified bridge write owns the input; waiting for a history UUID here
		// misclassifies legitimate queued input as an error and invites resends.
		if err = m.Store.Put("agentconfig", "native-input/"+ss.ID+"/"+request, map[string]string{"delivery": "submitted"}); err != nil {
			return err
		}
		status := "pending"
		if current.Status == "busy" {
			status = "running"
		}
		if err = m.Store.ObserveClaudeStatus(ss.ID, status, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		return nil
	}
	timer := time.NewTicker(50 * time.Millisecond)
	defer timer.Stop()
	for {
		if m.claudeInputRecorded(ss.ID, request, &observed) {
			if m.LoadHistory != nil {
				if e := m.LoadHistory(ss.ID); e != nil {
					return errors.New("电脑 Claude 已接收，历史同步暂未完成，请刷新会话，勿重复发送")
				}
			}
			return nil
		}
		select {
		case <-ctx.Done():
			if current, found, e := m.claudeInbox(ss); e == nil && found && current.PID == owner.PID && current.Start == owner.Start && current.Status == "idle" {
				return errors.New(claudeInboxIdleUnconfirmed)
			}
			return errors.New(claudeInboxUnconfirmed)
		case <-timer.C:
		}
	}
}
