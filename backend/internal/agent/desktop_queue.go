package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type nativeQueueCache struct {
	raw     []byte
	updated time.Time
	owner   string
	serial  uint64
}

func (b *DesktopBridge) queueOwner(id, owner string) {
	b.queueMu.Lock()
	defer b.queueMu.Unlock()
	if cached, ok := b.queues[id]; ok && cached.owner != owner {
		delete(b.queues, id)
	}
}
func (b *DesktopBridge) queueStamp(id string) uint64 {
	b.queueMu.Lock()
	defer b.queueMu.Unlock()
	return b.queues[id].serial
}
func (b *DesktopBridge) cacheQueue(id, owner string, messages []any) {
	b.cacheQueueAt(id, owner, messages, nil)
}
func (b *DesktopBridge) cacheQueueAt(id, owner string, messages []any, expected *uint64) {
	raw, e := json.Marshal(messages)
	if e != nil || len(raw) > 32<<20 {
		return
	}
	b.queueMu.Lock()
	defer b.queueMu.Unlock()
	if expected != nil && b.queues[id].serial != *expected {
		return
	}
	if b.queues == nil {
		b.queues = map[string]nativeQueueCache{}
	}
	delete(b.queues, id)
	total := len(raw)
	for _, c := range b.queues {
		total += len(c.raw)
	}
	for len(b.queues) >= 32 || total > 64<<20 {
		var oldest string
		var stamp time.Time
		for key, c := range b.queues {
			if oldest == "" || c.updated.Before(stamp) {
				oldest = key
				stamp = c.updated
			}
		}
		if oldest == "" {
			break
		}
		total -= len(b.queues[oldest].raw)
		delete(b.queues, oldest)
	}
	b.queueSerial++
	b.queues[id] = nativeQueueCache{raw: raw, updated: time.Now(), owner: owner, serial: b.queueSerial}
}
func (c *desktopConnection) captureQueue(frame map[string]any) {
	if c.bridge == nil || c.owner == "" || frame["type"] != "broadcast" || frame["method"] != "thread-queued-followups-changed" || frame["version"] != float64(2) || frame["sourceClientId"] != c.owner {
		return
	}
	p := obj(frame["params"])
	if p["hostId"] != "local" || p["conversationId"] != c.thread {
		return
	}
	messages, ok := p["messages"].([]any)
	if !ok || len(messages) > 200 {
		return
	}
	c.queueRevision++
	c.bridge.cacheQueue(c.thread, c.owner, messages)
}

type nativeSteerUnknown struct{ removed map[string]any }

func (e *nativeSteerUnknown) Error() string {
	return "队列消息已取出，引导结果未确认，请先检查电脑会话，勿重复发送"
}

// The desktop's follower protocol replaces a conversation's LOCAL native queue.
// Preserve every opaque desktop field and every sibling entry. Never write its
// persistence file or use the S-LOCAL delivery worker for native queue entries.
func (b *DesktopBridge) nativeQueue(id string) ([]any, error) {
	b.queueMu.Lock()
	cached, ok := b.queues[id]
	b.queueMu.Unlock()
	if ok && time.Since(cached.updated) < 30*time.Second {
		var messages []any
		if json.Unmarshal(cached.raw, &messages) == nil {
			return messages, nil
		}
	}

	path := b.QueueStatePath
	if path == "" {
		path = filepath.Join(filepath.Dir(filepath.Dir(b.Socket)), ".codex-global-state.json")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("电脑队列读取失败，请更新或重新连接 Codex")
	}
	defer f.Close()
	info, err := f.Stat()
	link, le := os.Lstat(path)
	if err != nil || le != nil || !info.Mode().IsRegular() || !desktopOwned(info) || link.Mode()&os.ModeSymlink != 0 || info.Size() > 32<<20 {
		return nil, errors.New("电脑队列读取失败，请更新或重新连接 Codex")
	}
	var state map[string]json.RawMessage
	if json.NewDecoder(io.LimitReader(f, 32<<20)).Decode(&state) != nil {
		return nil, errors.New("电脑队列读取失败，请更新或重新连接 Codex")
	}
	var queues map[string][]any
	raw := state["queued-follow-ups"]
	if len(raw) == 0 {
		return []any{}, nil
	}
	if json.Unmarshal(raw, &queues) != nil {
		return nil, errors.New("电脑队列读取失败，请更新或重新连接 Codex")
	}
	queue := queues[id]
	if queue == nil {
		queue = []any{}
	}
	if len(queue) > 200 {
		return nil, errors.New("消息队列已满")
	}
	return queue, nil
}
func queueVersion(message any) string { raw, _ := json.Marshal(message); return digest(string(raw)) }
func queueDisplayText(text string) string {
	if len(text) > 64<<10 {
		return text[:64<<10] + "…"
	}
	return text
}
func queueRecord(raw any) QueuedMessage {
	v := obj(raw)
	state := "queued"
	if status := str(obj(v["submission"])["status"]); status == "sending" || status == "outcome-unknown" {
		state = "unknown"
		if status == "sending" {
			state = "dispatching"
		}
	}
	if state == "queued" && str(v["pausedReason"]) != "" {
		state = "paused"
	}
	return QueuedMessage{ID: str(v["id"]), Text: Redact(queueDisplayText(str(v["text"]))), State: state, Origin: "codex", Version: queueVersion(raw), Attachments: len(arr(obj(v["context"])["imageAttachments"])) + len(arr(obj(v["context"])["fileAttachments"]))}
}
func (b *DesktopBridge) NativeQueue(id string) ([]QueuedMessage, error) {
	raw, err := b.nativeQueue(id)
	if err != nil {
		return nil, err
	}
	out := []QueuedMessage{}
	for _, v := range raw {
		q := queueRecord(v)
		if q.ID != "" {
			out = append(out, q)
		}
	}
	return out, nil
}
func (b *DesktopBridge) nativeQueueSet(c *desktopConnection, id string, queue []any) error {
	params := map[string]any{"conversationId": id, "state": map[string]any{id: queue}, "discardedMessageIds": []any{}}
	raw, e := json.Marshal(params)
	if e != nil {
		return e
	}
	if len(raw) > 16<<20 {
		return errors.New("队列附件超过大小限制")
	}
	revision := c.queueRevision
	stamp := b.queueStamp(id)
	result, err := c.request("thread-follower-set-queued-follow-ups-state", 1, params, c.owner)
	if err == nil && obj(result["result"])["ok"] != true {
		return errors.New("电脑 Codex 未确认接收；请检查原会话，勿重复发送")
	}
	if err == nil && revision == c.queueRevision {
		b.cacheQueueAt(id, c.owner, queue, &stamp)
	}
	return err
}

// Refuse a visibly changing snapshot; the upstream follower protocol has no CAS.
// Like Codex's own follower windows, a simultaneous desktop edit after this check
// remains an upstream limitation. Do not conceal it with retries or blind resets.
func (b *DesktopBridge) stableQueue(id string) ([]any, error) {
	first, e := b.nativeQueue(id)
	if e != nil {
		return nil, e
	}
	second, e := b.nativeQueue(id)
	if e != nil {
		return nil, e
	}
	if queueVersion(first) != queueVersion(second) {
		return nil, errors.New("电脑队列已变化，请刷新后重试")
	}
	return second, nil
}
func (b *DesktopBridge) EnqueueNative(ctx context.Context, id, text, request, project string, o TurnOptions, inputs []any) error {
	c, e := b.connect(ctx, id)
	if e != nil {
		return e
	}
	defer c.conn.Close()
	state, e := c.snapshot(id, project)
	if e != nil {
		return e
	}
	queue, e := b.stableQueue(id)
	if e != nil {
		return e
	}
	for _, raw := range queue {
		if str(obj(raw)["id"]) == request {
			return nil
		}
	}
	if len(queue) >= 20 {
		return errors.New("消息队列已满")
	}
	prompt := ""
	images := []any{}
	for i, raw := range inputs {
		v := obj(raw)
		switch str(v["type"]) {
		case "text":
			prompt += str(v["text"])
		case "image":
			images = append(images, map[string]any{"id": request + "/image/" + string(rune('0'+i)), "src": v["url"]})
		default:
			return errors.New("电脑队列不支持此附件类型")
		}
	}
	context := map[string]any{"prompt": prompt, "addedFiles": []any{}, "fileAttachments": []any{}, "imageAttachments": images, "commentAttachments": []any{}, "mcpAppModelContextAttachments": []any{}, "ideContext": nil, "workspaceRoots": []string{project}}
	message := map[string]any{"id": request, "text": text, "images": []any{}, "context": context, "cwd": project, "createdAt": time.Now().UnixMilli(), "requiresExistingOwner": true, "submission": map[string]any{"hostId": "local", "status": "queued", "queueModeOverride": "queue"}, "submissionOptions": map[string]any{"executionHostId": "local"}}
	// Native queued/sending/outcome-unknown transitions remain owned by Codex.
	// Omit permissionProfileId: this uses the native local follow-up lane instead
	// of the separately versioned durable/server-queue lane. No permission values
	// or override flags are invented; the owner prepares current permissions.
	if o.Model != "" || o.Effort != "" {
		collaboration := obj(state["latestCollaborationMode"])
		settings := obj(collaboration["settings"])
		if len(collaboration) == 0 {
			return errors.New("电脑模型设置尚未就绪，请刷新后重试")
		}
		if o.Model != "" {
			settings["model"] = o.Model
		}
		if o.Effort != "" {
			settings["reasoning_effort"] = o.Effort
		}
		collaboration["settings"] = settings
		message["submissionOptions"] = map[string]any{"executionHostId": "local", "collaborationMode": collaboration}
	}
	if o.Speed != "" {
		submission := obj(message["submissionOptions"])
		applySpeed(submission, o)
		message["submissionOptions"] = submission
	}
	return b.nativeQueueSet(c, id, append(queue, message))
}

// Rich native app/tool/comment context needs Codex's private preparation
// pipeline. Refuse before removal instead of dropping part of the input.
func queueContextSupported(message map[string]any) bool {
	c := obj(message["context"])
	for _, key := range []string{"untrustedAppMessage", "appTaskInstruction", "restoredPromptContext", "additionalContext", "writingBlockAdditionalContext", "aeonReplyQuote", "messageSender", "mcpAppModelContextAttachments", "addedFiles", "fileAttachments", "pastedTextAttachments", "uploadedFileAttachments", "commentAttachments", "imageCommentDraft", "imageCommentDrafts"} {
		v := c[key]
		if v == nil {
			continue
		}
		switch x := v.(type) {
		case string:
			if x != "" {
				return false
			}
		case []any:
			if len(x) > 0 {
				return false
			}
		default:
			return false
		}
	}
	for _, raw := range arr(c["imageAttachments"]) {
		image := obj(raw)
		if !strings.HasPrefix(str(image["src"]), "data:image/") {
			return false
		}
	}
	return true
}
func (b *DesktopBridge) ChangeNativeQueue(ctx context.Context, id, project, messageID, version, action, text, request string) error {
	c, e := b.connect(ctx, id)
	if e != nil {
		return e
	}
	defer c.conn.Close()
	state, e := c.snapshot(id, project)
	if e != nil {
		return e
	}
	queue, e := b.stableQueue(id)
	if e != nil {
		return e
	}
	index := -1
	for i, raw := range queue {
		if str(obj(raw)["id"]) == messageID {
			index = i
			break
		}
	}
	if index < 0 {
		return errors.New("待发消息已不可用")
	}
	raw := obj(queue[index])
	record := queueRecord(raw)
	if version == "" || version != record.Version {
		return errors.New("电脑队列已变化，请刷新后重试")
	}
	if record.State == "dispatching" || record.State == "unknown" && action != "delete" {
		return errors.New("消息已提交，请先检查会话")
	}
	if action != "delete" && !queueContextSupported(raw) {
		return errors.New("此队列消息包含特殊上下文，请在电脑操作")
	}
	switch action {
	case "edit":
		if len(str(raw["text"])) > 64<<10 {
			return errors.New("此队列消息包含特殊上下文，请在电脑编辑")
		}
		if len(text) > 64<<10 || strings.TrimSpace(text) == "" {
			return errors.New("消息为空或超过 64 KiB")
		}
		// Only change the editable prompt; keep attachments, context and sibling data.
		context := obj(raw["context"])
		old := str(raw["text"])
		prompt := str(context["prompt"])
		if !strings.HasPrefix(prompt, old) {
			return errors.New("此队列消息包含特殊上下文，请在电脑编辑")
		}
		context["prompt"] = text + strings.TrimPrefix(prompt, old)
		raw["text"] = text
		raw["context"] = context
		delete(raw, "pausedReason")
		return b.nativeQueueSet(c, id, queue)
	case "delete", "steer":
		if action == "steer" && desktopTurnStatus(state) != "running" {
			return errors.New("没有正在执行的任务，请正常发送")
		}
		revision := c.queueRevision
		stamp := b.queueStamp(id)
		v, e := c.request("thread-follower-remove-queued-message", 1, map[string]any{"conversationId": id, "messageId": messageID}, c.owner)
		if e != nil {
			return e
		}
		removed := obj(obj(v["result"])["removed"])
		message := obj(removed["message"])
		if str(message["id"]) != messageID {
			return errors.New("待发消息已不可用")
		}
		if revision == c.queueRevision {
			remaining := append(queue[:index:index], queue[index+1:]...)
			b.cacheQueueAt(id, c.owner, remaining, &stamp)
		}
		if action == "delete" {
			return nil
		}
		// Native removal is acknowledged before steering: never let a second run
		// consume the same queued message. Ambiguous steering is never retried.
		context := obj(message["context"])
		input := []any{map[string]any{"type": "text", "text": str(context["prompt"]), "text_elements": []any{}}}
		for _, image := range arr(context["imageAttachments"]) {
			v := obj(image)
			input = append(input, map[string]any{"type": "image", "url": v["src"]})
		}
		_, e = c.request("thread-follower-steer-turn", 1, map[string]any{"conversationId": id, "input": input, "restoreMessage": message, "attachments": []any{}, "clientUserMessageId": request}, c.owner)
		if e != nil {
			return &nativeSteerUnknown{removed: message}
		}
		return nil
	}
	return errors.New("无效的消息发送方式")
}
