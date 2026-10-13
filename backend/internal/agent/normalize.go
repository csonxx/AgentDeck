// Package agent accepts only presentation records. System prompts, raw reasoning
// ciphertext, authentication/account records and internal context are excluded.
package agent

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"agentdeck/internal/presentation"
	"agentdeck/internal/store"
)

type State struct {
	Offset        int64  `json:"offset"`
	Session       string `json:"session"`
	CWD           string `json:"cwd"`
	Message       string `json:"message"`
	MediaVersion  int    `json:"media_version,omitempty"`
	DesktopSync   int    `json:"desktop_sync,omitempty"`
	SourceVersion int    `json:"source_version,omitempty"`
	Excluded      bool   `json:"excluded,omitempty"`
}
type Normalizer struct {
	goalRecordMu     sync.Mutex
	nativeSettingsMu sync.Mutex
	Store            *store.Store
	MediaRoots       []string
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
func str(v any) string { s, _ := v.(string); return s }
func arr(v any) []any  { a, _ := v.([]any); return a }
func jsonText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

var secrets = regexp.MustCompile(`(?i)(sk-[a-z0-9_-]{12,}|eyJ[a-z0-9_-]{15,}\.[a-z0-9_-]{10,}\.[a-z0-9_-]+|bearer\s+[a-z0-9._/-]{8,}|(?:api[_-]?key|password|secret|access[_-]?token|authorization)\s*[=:]\s*["']?[^\s,"'}]+)`)

// Mask quoted JSON/tool fields as complete values, including spaces and
// escaped characters. Preserve valid JSON and the field name for presentation.
var quotedSecrets = regexp.MustCompile(`(?i)(["'][a-z0-9_-]*(?:api[_-]?key|password|secret|access[_-]?token|refresh[_-]?token|authorization|private[_-]?key)[a-z0-9_-]*["']\s*:\s*)(?:"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|[^\s,"'}]+)`)

var signed = regexp.MustCompile(`(?i)https?://[^\s)"<>]*(?:signature|x-amz-credential|x-amz-security-token|token)=[^\s)"<>]+`)

func Redact(s string) string {
	s = quotedSecrets.ReplaceAllString(s, `${1}"[REDACTED]"`)
	s = secrets.ReplaceAllString(s, "[REDACTED]")
	return signed.ReplaceAllString(s, "[REDACTED URL]")
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func (n *Normalizer) media(src, cwd string) (string, error) {
	var data []byte
	if strings.HasPrefix(src, "data:") {
		i := strings.Index(src, ",")
		if i < 0 || !strings.Contains(src[:i], ";base64") {
			return "", fmt.Errorf("unsupported image")
		}
		if len(src) > 12<<20 {
			return "", fmt.Errorf("image too large")
		}
		var e error
		data, e = base64.StdEncoding.DecodeString(src[i+1:])
		if e != nil {
			return "", e
		}
	} else {
		p, e := n.imageSource(src, cwd)
		if e != nil {
			return "", e
		}
		f, e := n.openAllowed(p)
		if e != nil {
			return "", e
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil || !st.Mode().IsRegular() || st.Size() > 8<<20 {
			return "", fmt.Errorf("invalid image")
		}
		data, e = io.ReadAll(io.LimitReader(f, (8<<20)+1))
		if e != nil {
			return "", e
		}
	}
	if len(data) > 8<<20 {
		return "", fmt.Errorf("image too large")
	}
	mime := http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return "", fmt.Errorf("unsupported image MIME")
	}
	id := digest(string(data))
	return id, n.Store.Put("media", id, store.Media{MIME: mime, Data: data})
}

func (n *Normalizer) Parse(raw []byte, agent, source string, st *State) ([]store.Event, error) {
	var d map[string]any
	if e := json.Unmarshal(raw, &d); e != nil {
		return nil, e
	}
	defer n.recordNativeSettings(d, agent, st)
	p := obj(d["payload"])
	t := str(d["type"])
	if agent == "codex" && t == "session_meta" {
		st.Session = str(p["id"])
		st.CWD = str(p["cwd"])
		st.Excluded = codexInternalSource(p["source"])
		return nil, nil
	}
	if agent == "codex" && st.Excluded {
		return nil, nil
	}
	out := []store.Event{}
	emit := func(kind, role, text, item, mode, status string) {
		if kind == "message" && mode != "delta" {
			text = presentation.Clean(text)
			if strings.TrimSpace(text) == "" {
				return
			}
		}
		if mode != "delta" {
			text = markdownDestination.ReplaceAllStringFunc(text, func(match string) string {
				if !strings.HasPrefix(match, "![") {
					return match
				}
				parts := markdownDestination.FindStringSubmatch(match)
				src := parts[1]
				if src == "" {
					src = parts[2]
				}
				if !strings.HasPrefix(src, "data:") {
					return match
				}
				id, err := n.media(src, st.CWD)
				if err != nil {
					return match
				}
				return match[:strings.Index(match, "](")+2] + "agentdeck-media:" + id + ")"
			})
		}
		text = Redact(text)
		if len(text) > 128<<10 {
			text = text[:128<<10] + "\n[输出过长，已截断]"
		}
		out = append(out, store.Event{Project: st.CWD, Session: st.Session, Agent: agent, Kind: kind, Role: role, Text: text, Item: item, Mode: mode, Status: status, Time: str(d["timestamp"]), Source: source})
	}
	image := func(src, item string) {
		if u, e := url.Parse(src); e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil {
			if Redact(src) == src {
				out = append(out, store.Event{Project: st.CWD, Session: st.Session, Agent: agent, Kind: "image", Item: item + ":remote:" + digest(src), Text: src, Source: source, Time: str(d["timestamp"])})
			} else {
				emit("notice", "", "图片链接已隐藏敏感凭据", item, "snapshot", "")
			}
			return
		}
		id, e := n.media(src, st.CWD)
		if e != nil {
			emit("notice", "", "图片暂不可用："+e.Error(), item, "snapshot", "")
			return
		}
		out = append(out, store.Event{Project: st.CWD, Session: st.Session, Agent: agent, Kind: "image", Item: item + ":" + id, Media: id, Source: source, Time: str(d["timestamp"])})
	}
	var resultText func(any, string) string
	resultText = func(v any, item string) string {
		if text, ok := v.(string); ok {
			var decoded any
			if json.Unmarshal([]byte(text), &decoded) == nil {
				switch decoded.(type) {
				case map[string]any, []any:
					return resultText(decoded, item)
				}
			}
			return text
		}
		if list, ok := v.([]any); ok {
			parts := []string{}
			for i, block := range list {
				m := obj(block)
				switch str(m["type"]) {
				case "image":
					image(imageURL(m), item+fmt.Sprint(i))
				case "input_image", "output_image":
					image(imageURL(m), item+fmt.Sprint(i))
				case "text", "output_text", "input_text":
					parts = append(parts, str(m["text"]))
				default:
					parts = append(parts, jsonText(block))
				}
			}
			return strings.Join(parts, "\n")
		}
		if m, ok := v.(map[string]any); ok {
			if content, ok := m["content"]; ok {
				return resultText(content, item)
			}
			if str(m["type"]) == "image" || str(m["type"]) == "input_image" || str(m["type"]) == "output_image" || m["image_url"] != nil {
				image(imageURL(m), item)
				return ""
			}
		}
		return jsonText(v)
	}
	message := func(m map[string]any) {
		role := str(m["role"])
		if role != "user" && role != "assistant" {
			return
		}
		id := str(m["id"])
		if id == "" {
			id = str(d["uuid"])
		}
		if id == "" {
			id = digest(string(raw))
		}
		content := m["content"]
		if s, ok := content.(string); ok {
			emit("message", role, s, id, "snapshot", "")
			return
		}
		for i, c := range arr(content) {
			v := obj(c)
			item := id + ":" + strconv.Itoa(i)
			switch str(v["type"]) {
			case "text", "input_text", "output_text":
				s := str(v["text"])
				status := ""
				if str(m["phase"]) == "commentary" {
					status = "running"
				}
				if str(m["phase"]) == "final" {
					status = "completed"
				}
				emit("message", role, s, item, "snapshot", status)
			case "thinking":
				emit("thinking", "assistant", str(v["thinking"]), item, "snapshot", "")
			case "tool_use":
				emit("tool_call", "", str(v["name"])+"\n"+jsonText(v["input"]), item, "snapshot", "running")
			case "tool_result":
				emit("tool_result", "", resultText(v["content"], item), str(v["tool_use_id"])+":result", "snapshot", "")
			case "image", "input_image", "output_image", "image_url":
				src := imageURL(v)
				image(src, item)
			}
		}
	}
	if agent == "claude" {
		if s := str(d["sessionId"]); s != "" {
			st.Session = s
		}
		if s := str(d["session_id"]); s != "" {
			st.Session = s
		}
		if s := str(d["cwd"]); s != "" {
			st.CWD = s
		}
		n.captureClaudeGoal(d, st)
		switch t {
		case "user", "assistant":
			m := obj(d["message"])
			if t == "user" && claudeHumanInput(d) && (source != "log" || str(d["timestamp"]) != "") {
				status := "running"
				if claudeInterruptedInput(d) {
					status = "interrupted"
				}
				emit("status", "", "", "", "", status)
			}
			message(m)
			stop := str(m["stop_reason"])
			if d["isApiErrorMessage"] == true {
				emit("status", "", "", "", "", "failed")
			} else if stop == "end_turn" || stop == "stop_sequence" {
				emit("status", "", "", "", "", "completed")
			}

		case "stream_event":
			e := obj(d["event"])
			switch str(e["type"]) {
			case "message_start":
				st.Message = str(obj(e["message"])["id"])
				emit("status", "", "", st.Message, "", "running")
			case "content_block_start":
				v := obj(e["content_block"])
				item := st.Message + ":" + fmt.Sprint(e["index"])
				if str(v["type"]) == "tool_use" {
					emit("tool_call", "", str(v["name"]), item, "snapshot", "running")
				}
			case "content_block_delta":
				v := obj(e["delta"])
				item := st.Message + ":" + fmt.Sprint(e["index"])
				switch str(v["type"]) {
				case "text_delta":
					emit("message", "assistant", str(v["text"]), item, "delta", "")
				case "thinking_delta":
					emit("thinking", "assistant", str(v["thinking"]), item, "delta", "")
				case "input_json_delta":
					emit("tool_call", "", str(v["partial_json"]), item, "delta", "")
				}
			}
		case "result":
			status := "completed"
			if d["is_error"] == true {
				status = "failed"
			}
			emit("status", "", status, "", "", status)
		}
		return out, nil
	}
	// The app-server bridge records JSON-RPC notifications without response bodies.
	if method := str(d["method"]); method != "" {
		p = obj(d["params"])
		if s := str(p["threadId"]); s != "" {
			st.Session = s
		}
		item := str(p["itemId"])
		switch method {
		case "thread/started":
			th := obj(p["thread"])
			st.Session = str(th["id"])
			st.CWD = str(th["cwd"])
		case "turn/started":
			emit("status", "", "", "", "", "running")
		case "turn/completed":
			status := str(obj(p["turn"])["status"])
			emit("status", "", status, "", "", status)
		case "item/agentMessage/delta":
			emit("message", "assistant", str(p["delta"]), item, "delta", "")
		case "item/reasoning/summaryTextDelta":
			emit("thinking", "assistant", str(p["delta"]), item+":summary:"+fmt.Sprint(p["summaryIndex"]), "delta", "")
		case "item/commandExecution/outputDelta":
			emit("tool_result", "", str(p["delta"]), item+":result", "delta", "")
		case "item/started", "item/completed":
			v := obj(p["item"])
			id := str(v["id"])
			switch str(v["type"]) {
			case "agentMessage":
				if method == "item/completed" {
					s := str(v["text"])
					emit("message", "assistant", s, id, "snapshot", "")
				}
			case "userMessage":
				message(map[string]any{"role": "user", "id": id, "content": v["content"]})
			case "commandExecution":
				emit("tool_call", "", str(v["command"]), id, "snapshot", "")
				if method == "item/completed" {
					emit("tool_result", "", str(v["aggregatedOutput"]), id+":result", "snapshot", "")
				}
			case "fileChange":
				emit("file_change", "", jsonText(v["changes"]), id, "snapshot", "")
			case "mcpToolCall":
				emit("tool_call", "", str(v["server"])+"/"+str(v["tool"])+"\n"+jsonText(v["arguments"]), id, "snapshot", "")
				if method == "item/completed" {
					emit("tool_result", "", resultText(v["result"], id), id+":result", "snapshot", "")
				}
			case "reasoning":
				for i, s := range arr(v["summary"]) {
					text := str(s)
					if text == "" {
						text = str(obj(s)["text"])
					}
					if text != "" {
						emit("thinking", "assistant", text, id+":summary:"+strconv.Itoa(i), "snapshot", "")
					}
				}
			case "imageGeneration":
				if s := str(v["imagePath"]); s != "" {
					image(s, id)
				} else if s := str(v["result"]); s != "" {
					image("data:image/png;base64,"+s, id)
				}
			}
		}
		return out, nil
	}
	if t == "event_msg" {
		switch str(p["type"]) {
		case "task_started":
			emit("status", "", "", "", "", "running")
		case "task_complete":
			emit("status", "", "", "", "", "completed")
		case "turn_aborted":
			emit("status", "", "", "", "", "interrupted")
		}
		return out, nil
	}
	if t == "response_item" {
		switch str(p["type"]) {
		case "message":
			message(p)
		case "reasoning":
			for i, v := range arr(p["summary"]) {
				emit("thinking", "assistant", str(obj(v)["text"]), str(p["id"])+":summary:"+strconv.Itoa(i), "snapshot", "")
			}
		case "function_call", "custom_tool_call":
			s := str(p["arguments"])
			if s == "" {
				s = str(p["input"])
			}
			emit("tool_call", "", str(p["name"])+"\n"+s, str(p["call_id"]), "snapshot", "")
		case "function_call_output", "custom_tool_call_output":
			s := resultText(p["output"], str(p["call_id"]))
			emit("tool_result", "", s, str(p["call_id"])+":result", "snapshot", "")
		}
		return out, nil
	}
	// codex exec --json item lifecycle.
	if s := str(d["thread_id"]); s != "" {
		st.Session = s
	}
	switch t {
	case "turn.started":
		emit("status", "", "", "", "", "running")
	case "turn.completed":
		emit("status", "", "", "", "", "completed")
	case "turn.failed":
		emit("status", "", "失败", "", "", "failed")
	case "item.completed", "item.started":
		v := obj(d["item"])
		id := str(v["id"])
		switch str(v["type"]) {
		case "agent_message":
			emit("message", "assistant", str(v["text"]), id, "snapshot", "")
		case "reasoning":
			emit("thinking", "assistant", str(v["text"]), id, "snapshot", "")
		case "command_execution":
			emit("tool_call", "", str(v["command"]), id, "snapshot", "")
			if t == "item.completed" {
				emit("tool_result", "", str(v["aggregated_output"]), id+":result", "snapshot", "")
			}
		case "file_change":
			emit("file_change", "", jsonText(v["changes"]), id, "snapshot", "")
		}
	}
	return out, nil
}

func imageURL(v map[string]any) string {
	if s := str(v["image_url"]); s != "" {
		return s
	}
	if s := str(obj(v["image_url"])["url"]); s != "" {
		return s
	}
	source := obj(v["source"])
	if str(source["type"]) == "url" {
		return str(source["url"])
	}
	if data := str(source["data"]); data != "" {
		return "data:" + str(source["media_type"]) + ";base64," + data
	}
	mime := str(v["mimeType"])
	if mime == "" {
		mime = str(v["mime_type"])
	}
	return "data:" + mime + ";base64," + str(v["data"])
}
