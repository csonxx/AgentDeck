package agent

import (
	"agentdeck/internal/store"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

type InputAttachment struct {
	ID, Name, MIME string
	Data           []byte
}

func (m *Manager) sessionForInput(id string) (store.Session, error) {
	var ss store.Session
	if m.Store.Get("sessions", id, &ss) != nil {
		return ss, errors.New("会话不存在")
	}
	// External adapters enforce their own session boundary. WorkBuddy's
	// account-scoped Local Assistant has no filesystem project directory.
	if ss.Agent != "codex" && ss.Agent != "claude" {
		_, external, e := m.externalSession(id)
		return external, e
	}
	_, e := m.allowed(ss.Project)
	return ss, e
}
func (m *Manager) Options(id string) TurnOptions {
	var o TurnOptions
	_ = m.Store.Get("generation", id, &o)
	return o
}
func (m *Manager) setOptions(ss store.Session, o TurnOptions) (TurnOptions, error) {
	if o.Model == "" && o.Effort == "" && o.Speed == "" {
		return m.Options(ss.ID), nil
	}
	if ss.Agent == "claude" {
		_, native, err := m.claudeInbox(ss)
		if err != nil {
			return o, err
		}
		if native {
			if (o.Model != "" && o.Model != "default") || o.Effort != "" {
				return o, errors.New(claudeInboxOptions)
			}
			return TurnOptions{}, nil
		}
	}
	if o.Model != "" && o.Effort == "" && ss.Agent == "codex" {
		choices, e := m.Models(ss.Agent, ss.ID)
		if e != nil {
			return o, e
		}
		for _, c := range choices {
			if c.ID == o.Model {
				o.Effort = c.DefaultEffort
				break
			}
		}
	}
	if e := m.validateOptions(ss.ID, ss.Agent, o); e != nil {
		return o, e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.runs[ss.ID]; r != nil {
		r.mu.Lock()
		busy := r.busy
		r.mu.Unlock()
		if busy && ss.Agent == "claude" && r.options == o {
			return o, nil
		}
		if busy {
			return o, &InputNotSentError{Err: errors.New("当前任务正在执行，完成后可修改模型参数")}
		}
		if ss.Agent == "claude" && r.options != o {
			r.close()
			select {
			case <-r.done:
			case <-time.After(5 * time.Second):
				return o, errors.New("旧会话尚未退出，请重试")
			}
			delete(m.runs, ss.ID)
		}
	}
	return o, m.Store.Put("generation", ss.ID, o)
}

// All files stay encrypted in the store. The trusted owner CLI streams their
// bytes over a private Unix socket; no plaintext working copy is created here.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func (m *Manager) prepareInput(id, text, request string, files []InputAttachment) ([]any, string, error) {
	if len(files) > 6 {
		return nil, "", errors.New("一次最多发送 6 个附件")
	}
	inputs := []any{}
	notes := []string{}
	total := 0
	for _, f := range files {
		total += len(f.Data)
		if len(f.Data) == 0 || len(f.Data) > 8<<20 || total > 24<<20 {
			return nil, "", errors.New("附件为空或超过大小限制")
		}
		label := strings.NewReplacer("[", "\\[", "]", "\\]").Replace(f.Name)
		href := "agentdeck-file:" + f.ID
		if strings.HasPrefix(f.MIME, "image/") {
			inputs = append(inputs, map[string]any{"type": "image", "url": "data:" + f.MIME + ";base64," + base64.StdEncoding.EncodeToString(f.Data)})
			notes = append(notes, "!["+label+"]("+href+")")
		} else {
			if m.Binary == "" || m.ControlDir == "" {
				return nil, "", errors.New("工作站未配置附件读取工具")
			}
			command := shellQuote(m.Binary) + " input-file --data " + shellQuote(m.ControlDir) + " --session " + shellQuote(id) + " --upload " + shellQuote(f.ID)
			notes = append(notes, fmt.Sprintf("[%s](%s) (%s, %d bytes)\n只读附件命令：%s --format text\n二进制使用 --format raw 通过管道读取；文本默认前 64 KiB，可用 --offset / --limit 分页。", label, href, f.MIME, len(f.Data), command))
		}
	}
	if len(notes) > 0 {
		text += "\n\n" + strings.Join(notes, "\n\n")
	}
	if strings.TrimSpace(text) != "" {
		inputs = append([]any{map[string]any{"type": "text", "text": text}}, inputs...)
	}
	return inputs, text, nil
}

func claudeInput(inputs []any) any {
	out := []any{}
	for _, raw := range inputs {
		v := obj(raw)
		switch str(v["type"]) {
		case "text":
			out = append(out, map[string]any{"type": "text", "text": v["text"]})
		case "image":
			url := str(v["url"])
			pieces := strings.SplitN(url, ",", 2)
			if len(pieces) == 2 {
				mime := strings.TrimSuffix(strings.TrimPrefix(pieces[0], "data:"), ";base64")
				out = append(out, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": mime, "data": pieces[1]}})
			}
		}
	}
	if len(out) == 1 && str(obj(out[0])["type"]) == "text" {
		return obj(out[0])["text"]
	}
	return out
}
