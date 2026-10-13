package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"agentdeck/internal/store"
	acp "github.com/coder/acp-go-sdk"
)

// Only presentation fields are returned. Provider credentials, headers and
// endpoint options never cross the paired app protocol.
func (m *Manager) externalModels(name, id string) ([]ModelChoice, error) {
	c, s, e := m.externalSession(id)
	if e != nil {
		return nil, e
	}
	if s.Agent != name {
		return nil, errors.New("Agent 与会话不匹配")
	}
	switch v := c.(type) {
	case *openCode:
		return v.Models(m.Context, s)
	case *acpConnector:
		r, e := v.get(m.Context, s)
		if e != nil {
			return nil, e
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.modelChoicesLocked(), nil
	case *workBuddy:
		return []ModelChoice{}, nil
	}
	return []ModelChoice{}, nil
}
func (o *openCode) Models(ctx context.Context, s store.Session) ([]ModelChoice, error) {
	var response map[string]any
	if e := o.request(ctx, "GET", "/provider", s.Project, nil, &response); e != nil {
		return nil, e
	}
	connected := map[string]bool{}
	for _, v := range arr(response["connected"]) {
		connected[str(v)] = true
	}
	out := []ModelChoice{}
	defaults := obj(response["default"])
	for _, raw := range arr(response["all"]) {
		provider := obj(raw)
		pid := str(provider["id"])
		if !connected[pid] {
			continue
		}
		for key, rawModel := range obj(provider["models"]) {
			model := obj(rawModel)
			mid := str(model["id"])
			if mid == "" {
				mid = key
			}
			id := pid + "/" + mid
			if !modelPattern.MatchString(id) || model["status"] == "deprecated" {
				continue
			}
			choice := ModelChoice{ID: id, Name: Redact(str(model["name"])), Default: str(defaults[pid]) == mid, Images: obj(obj(model["capabilities"])["input"])["image"] == true, Efforts: []string{}}
			if choice.Name == "" {
				choice.Name = id
			}
			for variant := range obj(model["variants"]) {
				if modelPattern.MatchString(variant) {
					choice.Efforts = append(choice.Efforts, variant)
				}
			}
			sort.Strings(choice.Efforts)
			out = append(out, choice)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Manager) InputCapabilities(name, id string, media bool) (map[string]any, error) {
	if !KnownAgent(name) {
		return nil, errors.New("未知 Agent")
	}
	s, e := m.sessionForInput(id)
	if e != nil {
		return nil, e
	}
	if s.Agent != name {
		return nil, errors.New("Agent 与会话不匹配")
	}
	result := map[string]any{"voice": len(m.VoiceCommand) > 0, "files": m.Binary != "" && m.ControlDir != "", "images": false, "model_switch": false, "effort_label": "Effort", "detail": ""}
	if !media {
		return result, nil
	}
	switch name {
	case "codex", "claude":
		result["images"] = true
		result["model_switch"] = true
	case "opencode":
		choices, e := m.externalModels(name, id)
		if e != nil {
			result["detail"] = e.Error()
			break
		}
		result["model_switch"] = len(choices) > 0
		result["effort_label"] = "Variant"
		for _, choice := range choices {
			if choice.Images {
				result["images"] = true
			}
		}
	case "qoder", "trae":
		c, ok := m.Connectors[name].(*acpConnector)
		if !ok {
			return nil, errors.New("ACP Agent 未配置")
		}
		r, e := c.get(m.Context, s)
		if e != nil {
			result["detail"] = e.Error()
			break
		}
		r.mu.Lock()
		result["images"] = r.caps.PromptCapabilities.Image
		result["model_switch"] = len(r.modelChoicesLocked()) > 0
		r.mu.Unlock()
		if result["model_switch"] == false {
			result["detail"] = "当前 ACP 版本未返回模型选择项，沿用电脑会话设置"
		}
	case "workbuddy":
		result["detail"] = "WorkBuddy 本地助理接口未提供模型切换或图片输入；文件使用本机只读附件引用"
	}
	return result, nil
}

func configMaps(v []acp.SessionConfigOption) []map[string]any {
	b, _ := json.Marshal(v)
	var out []map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}
func configCategory(v map[string]any) string {
	if category := str(v["category"]); category != "" {
		return category
	}
	switch str(v["id"]) {
	case "model":
		return "model"
	case "speed", "service_tier", "fast_mode":
		return "speed"
	case "effort", "reasoning_effort", "thought_level", "variant":
		return "thought_level"
	}
	return ""
}
func configValues(v map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, raw := range arr(v["options"]) {
		option := obj(raw)
		if option["group"] != nil {
			for _, raw := range arr(option["options"]) {
				out = append(out, obj(raw))
			}
		} else {
			out = append(out, option)
		}
	}
	return out
}
func (r *acpRuntime) modelChoicesLocked() []ModelChoice {
	var model, thought, speed map[string]any
	for _, option := range r.configOptions {
		if option["type"] != "select" {
			continue
		}
		switch configCategory(option) {
		case "model":
			if model == nil {
				model = option
			}
		case "speed":
			speed = option
		case "thought_level":
			if thought == nil {
				thought = option
			}
		}
	}
	out := []ModelChoice{}
	for _, option := range configValues(model) {
		id := str(option["value"])
		if !modelPattern.MatchString(id) {
			continue
		}
		c := ModelChoice{ID: id, Name: Redact(str(option["name"])), Default: id == str(model["currentValue"]), Images: r.caps.PromptCapabilities.Image, Efforts: []string{}}
		for _, eff := range configValues(thought) {
			if value := str(eff["value"]); modelPattern.MatchString(value) {
				c.Efforts = append(c.Efforts, value)
			}
		}
		fast, standard := acpSpeedValues(speed)
		if fast != "" && standard != "" {
			c.FastTier = fast
		}
		if c.Default {
			c.DefaultEffort = str(thought["currentValue"])
		}
		out = append(out, c)
	}
	return out
}

// Configure only model/reasoning selectors. Permission/mode selectors are never
// touched. Refresh after model changes because reasoning options may depend on it.
func (r *acpRuntime) applyOptions(ctx context.Context, o TurnOptions) error {
	for _, selection := range []struct{ category, value string }{{"model", o.Model}, {"thought_level", o.Effort}, {"speed", o.Speed}} {
		if selection.value == "" {
			continue
		}
		r.mu.Lock()
		var chosen map[string]any
		for _, option := range r.configOptions {
			if option["type"] == "select" && configCategory(option) == selection.category {
				chosen = option
				break
			}
		}
		if selection.category == "speed" {
			fast, standard := acpSpeedValues(chosen)
			if selection.value == "fast" {
				selection.value = fast
			} else {
				selection.value = standard
			}
		}
		valid := selection.value != ""
		found := false
		for _, option := range configValues(chosen) {
			if str(option["value"]) == selection.value {
				found = true
			}
		}
		current := str(chosen["currentValue"])
		id := str(chosen["id"])
		r.mu.Unlock()
		if !valid || !found {
			return errors.New("当前模型不支持该选择，请刷新模型与强度选项")
		}
		if current == selection.value {
			continue
		}
		timeout, cancel := context.WithTimeout(nonNil(ctx), 20*time.Second)
		response, e := r.conn.SetSessionConfigOption(timeout, acp.SetSessionConfigOptionRequest{ValueId: &acp.SetSessionConfigOptionValueId{SessionId: acp.SessionId(nativeID(r.s)), ConfigId: acp.SessionConfigId(id), Value: acp.SessionConfigValueId(selection.value)}})
		cancel()
		if e != nil {
			return errors.New("Agent 未应用模型或强度设置，请检查 ACP 版本")
		}
		r.mu.Lock()
		r.configOptions = configMaps(response.ConfigOptions)
		r.mu.Unlock()
	}
	return nil
}

func imageFiles(files []InputAttachment) bool {
	for _, f := range files {
		if strings.HasPrefix(f.MIME, "image/") {
			return true
		}
	}
	return false
}

// Only explicit native speed selectors are touched; no permission/mode selectors.
func acpSpeedValues(option map[string]any) (fast, standard string) {
	for _, v := range configValues(option) {
		value := str(v["value"])
		switch strings.ToLower(value) {
		case "fast", "priority", "on", "true":
			fast = value
		case "standard", "default", "normal", "off", "false":
			standard = value
		}
	}
	return
}
