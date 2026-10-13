package agent

import (
	"agentdeck/internal/store"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var claudeCloudID = regexp.MustCompile(`^(session|cse)_[A-Za-z0-9_]+$`)

const claudeRemoteEnable = "电脑 Claude 原会话尚未开启 Remote Control；请在该会话启用远程连接后发送"
const claudeRemoteUnknown = "Claude 原会话投递结果尚未确认，请核对原会话；不会自动重复发送"
const claudeRemotePeerOnly = "Claude 官方 CLI 远程接口会将消息标记为其他会话请求，尚不能用于手机作为用户直发 Desktop 原会话；消息未发送"

type claudeRemoteReceipt struct {
	Fingerprint string `json:"fingerprint"`
	State       string `json:"state"`
}

// Native acceptance showed that --print --cloud is peer ingress, even though it
// returns ok=true and executes in the original process. Do not pass an owner's
// phone input through it or silently fall back to a queue/replacement executor.
func (m *Manager) sendClaudeRemote(ss store.Session, text, request string, options TurnOptions, files []InputAttachment) (bool, error) {
	if m.ClaudeDesktopDebugger {
		if handled, err := m.sendClaudeDesktopNative(ss, text, request, options, files); handled {
			return true, err
		}
	}
	owner, err := m.claudeHandoffOwner(ss.ID, m.claudeRuntimePID(ss.ID))
	if err != nil {
		return true, &InputNotSentError{Err: err}
	}
	if owner == nil {
		return false, nil
	}
	if owner.BridgeSessionID == "" {
		if owner.EntryPoint == "claude-desktop" {
			return true, &InputNotSentError{Err: errors.New(claudeRemoteEnable)}
		}
		return false, nil
	}
	return true, &InputNotSentError{Err: errors.New(claudeRemotePeerOnly)}
}

// Experimental peer transport, retained for protocol regression only. There
// are no production callers: a CLI acknowledgement is not human-input proof.
func (m *Manager) sendClaudeRemotePeer(ss store.Session, text, request string, options TurnOptions, files []InputAttachment) (bool, error) {
	lock := m.deliveryLock("claude-remote/" + ss.ID)
	lock.Lock()
	defer lock.Unlock()
	key := "claude-remote/" + ss.ID + "/" + request
	encoded, _ := json.Marshal(queuedInput{Text: text, Options: options, Files: files})
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded))
	var previous claudeRemoteReceipt
	if m.Store.Get("agentconfig", key, &previous) == nil {
		if previous.Fingerprint != fingerprint {
			return true, &InputNotSentError{Err: errors.New("消息编号已用于不同内容")}
		}
		if previous.State == "submitted" {
			return true, m.Store.Put("agentconfig", "native-input/"+ss.ID+"/"+request, map[string]string{"delivery": "submitted"})
		}
		if previous.State != "not_started" {
			return true, errors.New(claudeRemoteUnknown)
		}
	}
	owner, err := m.claudeHandoffOwner(ss.ID, m.claudeRuntimePID(ss.ID))
	if err != nil {
		return true, &InputNotSentError{Err: err}
	}
	if owner == nil {
		return false, nil
	}
	if owner.BridgeSessionID == "" {
		if owner.EntryPoint == "claude-desktop" {
			return true, &InputNotSentError{Err: errors.New(claudeRemoteEnable)}
		}
		return false, nil
	}
	notSent := func(message string) (bool, error) { return true, &InputNotSentError{Err: errors.New(message)} }
	if request == "" || len(request) > 128 || len(text) > 64<<10 || strings.TrimSpace(text) == "" && len(files) == 0 {
		return notSent("消息为空、编号无效或超过 64 KiB")
	}
	root, err := os.Lstat(m.claudeProcessRoot())
	if err != nil || !root.IsDir() || !desktopOwned(root) || root.Mode().Perm()&0077 != 0 || !claudeCloudID.MatchString(owner.BridgeSessionID) {
		return notSent("Claude 原会话的远程连接标识或目录权限无效")
	}
	project, err := filepath.EvalSymlinks(ss.Project)
	ownerProject, ownerErr := filepath.EvalSymlinks(owner.ProjectDir)
	if err != nil || ownerErr != nil || project != ownerProject {
		return notSent("Claude 原会话与授权项目不匹配")
	}
	settings, err := m.SessionSettings(ss.ID)
	if err != nil {
		return true, &InputNotSentError{Err: err}
	}
	if options.Model != "" && options.Model != settings.Model || options.Effort != "" && options.Effort != settings.Effort || options.Speed != "" && options.Speed != settings.Speed {
		return notSent("此 Claude 远程接口使用原会话的模型参数；请在电脑原会话修改")
	}
	for _, file := range files {
		if strings.HasPrefix(file.MIME, "image/") {
			return notSent("当前 Claude 远程发送接口尚不支持图片输入")
		}
	}
	_, input, err := m.prepareInput(ss.ID, text, request, files)
	if err != nil {
		return true, &InputNotSentError{Err: err}
	}
	// Recheck immediately before dispatch. A changed process or bridge must not
	// receive a message authorized for a different native session owner.
	latest, err := m.claudeHandoffOwner(ss.ID, m.claudeRuntimePID(ss.ID))
	if err != nil || latest == nil || latest.PID != owner.PID || latest.Start != owner.Start || latest.BridgeSessionID != owner.BridgeSessionID || latest.ProjectDir != owner.ProjectDir {
		return notSent("Claude 原会话连接已变化，消息未发送")
	}
	ctx := m.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "--print", "--cloud="+owner.BridgeSessionID, "--output-format=json")
	cmd.Dir = project
	cmd.Stdin = strings.NewReader(input)
	var output claudeRemoteOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	receipt := claudeRemoteReceipt{Fingerprint: fingerprint, State: "dispatching"}
	if err = m.Store.Put("agentconfig", key, receipt); err != nil {
		return true, &InputNotSentError{Err: err}
	}
	if err = cmd.Start(); err != nil {
		receipt.State = "not_started"
		_ = m.Store.Put("agentconfig", key, receipt)
		return notSent("无法启动 Claude 远程发送客户端，消息未发送")
	}
	err = cmd.Wait()
	var ack struct {
		OK      bool   `json:"ok"`
		Session string `json:"session_id"`
	}
	if err != nil || output.overflow || json.Unmarshal(output.Bytes(), &ack) != nil || !ack.OK || ack.Session != owner.BridgeSessionID {
		receipt.State = "unknown"
		_ = m.Store.Put("agentconfig", key, receipt)
		return true, errors.New(claudeRemoteUnknown)
	}
	receipt.State = "submitted"
	if err = m.Store.Put("agentconfig", key, receipt); err != nil {
		return true, errors.New(claudeRemoteUnknown)
	}
	return true, m.Store.Put("agentconfig", "native-input/"+ss.ID+"/"+request, map[string]string{"delivery": "submitted"})
}

// Drain stdout without allowing an unexpected CLI response to grow memory or
// leak its contents into logs. The success acknowledgement is only a few bytes.
type claudeRemoteOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *claudeRemoteOutput) Bytes() []byte { return b.buffer.Bytes() }

func (b *claudeRemoteOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (64 << 10) - b.buffer.Len()
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}
