package agent

import (
	"agentdeck/internal/claudedesktop"
	"agentdeck/internal/store"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const claudeDebuggerUnavailable = "Claude 本机直发连接未开启；请在电脑 Claude 的 Developer 菜单开启 Enable Main Process Debugger，消息未发送"
const claudeDebuggerUnknown = "Claude 原会话尚未确认收到这条消息；不会重复发送，请稍后刷新核对"

type claudeDesktopClient interface {
	Snapshot(context.Context, claudedesktop.Target, string) (claudedesktop.Snapshot, error)
	Send(context.Context, claudedesktop.Target, string, string) error
	Close()
}

// The phone supplies only a catalog session ID. Resolve the Desktop host ID
// locally and require a unique owner-owned file and the same allowed project.
func (m *Manager) claudeDesktopTarget(ss store.Session) (claudedesktop.Target, bool, error) {
	var target claudedesktop.Target
	if m.ClaudeDesktopSessions == "" {
		return target, false, nil
	}
	root, err := os.Lstat(m.ClaudeDesktopSessions)
	if err != nil || !root.IsDir() || !desktopOwned(root) || root.Mode().Perm()&0022 != 0 {
		return target, true, errors.New("Claude Desktop 会话目录无效或权限不安全")
	}
	count := 0
	err = filepath.WalkDir(m.ClaudeDesktopSessions, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasPrefix(d.Name(), "local_") || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 || !desktopOwned(info) || info.Mode().Perm()&0022 != 0 {
			return errors.New("unsafe catalog file")
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		var meta claudeDesktopRecord
		if json.Unmarshal(b, &meta) != nil {
			return errors.New("invalid catalog file")
		}
		if meta.CLI != ss.ID {
			return nil
		}
		if !claudeNativeID.MatchString(meta.SessionID) || meta.SessionID+".json" != d.Name() || meta.Archived {
			return errors.New("invalid Desktop session")
		}
		project := meta.Origin
		if project == "" {
			project = meta.CWD
		}
		p, e := m.allowed(project)
		expected, expectedErr := m.allowed(ss.Project)
		if e != nil || expectedErr != nil || p != expected || !filepath.IsAbs(meta.CWD) {
			return errors.New("project mismatch")
		}
		count++
		target = claudedesktop.Target{Host: meta.SessionID, Native: ss.ID, CWD: meta.CWD, Project: project}
		return nil
	})
	if err != nil || count > 1 {
		return target, true, errors.New("无法唯一确认 Claude Desktop 原会话和项目，消息未发送")
	}
	if count == 0 && ss.Source == "claude-desktop" {
		return target, true, errors.New("Claude Desktop 原会话记录不存在，消息未发送")
	}
	return target, count == 1, nil
}

func claudeDesktopMessageUUID(session, request string) string {
	h := sha256.Sum256([]byte("agentdeck/claude-desktop/user/v1\x00" + session + "\x00" + request))
	h[6] = (h[6] & 15) | 0x50
	h[8] = (h[8] & 63) | 0x80
	b := fmt.Sprintf("%x", h[:16])
	return b[:8] + "-" + b[8:12] + "-" + b[12:16] + "-" + b[16:20] + "-" + b[20:]
}

func (m *Manager) sendClaudeDesktopNative(ss store.Session, text, request string, options TurnOptions, files []InputAttachment) (bool, error) {
	lock := m.deliveryLock("claude-native/" + ss.ID)
	lock.Lock()
	defer lock.Unlock()
	notSent := func(err error) (bool, error) { return true, &InputNotSentError{Err: err} }
	key := "claude-native/" + ss.ID + "/" + request
	encoded, _ := json.Marshal(queuedInput{Text: text, Options: options, Files: files})
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded))
	var receipt claudeRemoteReceipt
	exists := m.Store.Get("agentconfig", key, &receipt) == nil
	if exists {
		if receipt.Fingerprint != fingerprint {
			return notSent(errors.New("消息编号已用于不同内容"))
		}
		if receipt.State == "submitted" {
			return true, m.Store.Put("agentconfig", "native-input/"+ss.ID+"/"+request, map[string]string{"delivery": "submitted", "message_id": claudeDesktopMessageUUID(ss.ID, request)})
		}
	}
	// A previous ambiguous dispatch remains ambiguous even if its owner or
	// debugger disappeared; never turn it into a retryable not-sent error.
	unknown := exists && receipt.State != "not_started"
	target, handled, err := m.claudeDesktopTarget(ss)
	if !handled && err == nil {
		if unknown {
			return true, errors.New(claudeDebuggerUnknown)
		}
		return false, nil
	}
	if err != nil {
		if unknown {
			return true, errors.New(claudeDebuggerUnknown)
		}
		return notSent(err)
	}
	if request == "" || len(request) > 128 || len(text) > 64<<10 || strings.TrimSpace(text) == "" && len(files) == 0 {
		return notSent(errors.New("消息为空、编号无效或超过 64 KiB"))
	}
	var images []claudedesktop.Image
	for _, file := range files {
		if strings.HasPrefix(file.MIME, "image/") {
			switch file.MIME {
			case "image/png", "image/jpeg", "image/gif", "image/webp":
			default:
				return notSent(errors.New("Claude 不支持此图片格式，消息未发送"))
			}
			images = append(images, claudedesktop.Image{Base64: base64.StdEncoding.EncodeToString(file.Data), MIME: file.MIME, Name: file.Name})
		}
	}
	_, input, err := m.prepareInput(ss.ID, text, request, files)
	if err != nil {
		return notSent(err)
	}
	ctx := m.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	open := m.openClaudeDesktop
	if open == nil {
		open = func(ctx context.Context) (claudeDesktopClient, error) { return claudedesktop.Open(ctx) }
	}
	bridge, err := open(ctx)
	if err != nil {
		if unknown {
			return true, errors.New(claudeDebuggerUnknown)
		}
		return notSent(errors.New(claudeDebuggerUnavailable))
	}
	defer bridge.Close()
	imageBridge, imageSupported := bridge.(interface {
		SendImages(context.Context, claudedesktop.Target, string, string, []claudedesktop.Image) error
	})
	if len(images) > 0 && !imageSupported {
		return notSent(errors.New("当前 Claude Desktop 不支持图片直发，消息未发送"))
	}
	uuid := claudeDesktopMessageUUID(ss.ID, request)
	before, err := bridge.Snapshot(ctx, target, uuid)
	if err != nil || !before.Found {
		if unknown {
			return true, errors.New(claudeDebuggerUnknown)
		}
		return notSent(errors.New("无法确认 Claude Desktop 原会话，消息未发送"))
	}
	accepted := before.Accepted
	if !accepted && unknown {
		return true, errors.New(claudeDebuggerUnknown)
	}
	if !accepted {
		owner, ownerErr := m.claudeHandoffOwner(ss.ID, 0)
		if ownerErr != nil {
			return notSent(ownerErr)
		}
		if owner != nil && owner.EntryPoint != "claude-desktop" {
			return notSent(errors.New("此原会话由另一执行端持有，消息未发送；不会新建并行执行端"))
		}
		effort := before.Effort
		if effort == "extra" {
			effort = "xhigh"
		}
		if options.Model != "" && options.Model != before.Model || options.Effort != "" && options.Effort != effort || options.Speed != "" && (options.Speed == "fast") != before.Fast {
			return notSent(errors.New("请在电脑 Claude 原会话中修改模型参数后再发送"))
		}
		latest, found, e := m.claudeDesktopTarget(ss)
		if e != nil || !found || latest != target {
			return notSent(errors.New("Claude Desktop 原会话身份已变化，消息未发送"))
		}
		latestOwner, ownerErr := m.claudeHandoffOwner(ss.ID, 0)
		if ownerErr != nil || owner == nil && latestOwner != nil || owner != nil && (latestOwner == nil || latestOwner.PID != owner.PID || latestOwner.Start != owner.Start) {
			return notSent(errors.New("Claude 原会话执行端已变化，消息未发送"))
		}
		receipt = claudeRemoteReceipt{Fingerprint: fingerprint, State: "dispatching"}
		if err = m.Store.Put("agentconfig", key, receipt); err != nil {
			return notSent(err)
		}
		// Do not retry on any send error. Even a thrown IPC exception can follow
		// a successful write to the original process's stdin.
		if len(images) > 0 {
			_ = imageBridge.SendImages(ctx, target, input, uuid, images)
		} else {
			_ = bridge.Send(ctx, target, input, uuid)
		}
		// Desktop dispatch is asynchronous. A fixed 2.4-second polling window
		// can expire before its human record is flushed even though inference
		// is already starting. Use the bounded request deadline instead.
		for ctx.Err() == nil {
			snapshot, e := bridge.Snapshot(ctx, target, uuid)
			if e != nil {
				break
			}
			if snapshot.Accepted {
				accepted = true
				break
			}
			select {
			case <-ctx.Done():
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if !accepted {
		return true, errors.New(claudeDebuggerUnknown)
	}
	receipt = claudeRemoteReceipt{Fingerprint: fingerprint, State: "submitted"}
	if err = m.Store.Put("agentconfig", key, receipt); err != nil {
		return true, errors.New(claudeDebuggerUnknown)
	}
	if m.LoadHistory != nil {
		_ = m.LoadHistory(ss.ID)
	}
	m.keepDesktopSend(ss.ID)
	// The follower owns real native status/replies, including quota errors.
	// Receipt means human input landed, never inference success or running.
	return true, m.Store.Put("agentconfig", "native-input/"+ss.ID+"/"+request, map[string]string{"delivery": "submitted", "message_id": claudeDesktopMessageUUID(ss.ID, request)})
}
