package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"regexp"
	"time"
)

type ModelChoice struct {
	FastTier      string   `json:"fast_tier,omitempty"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Efforts       []string `json:"efforts"`
	DefaultEffort string   `json:"default_effort,omitempty"`
	Default       bool     `json:"default"`
	Images        bool     `json:"images"`
}
type TurnOptions struct {
	Model  string `json:"model,omitempty"`
	Speed  string `json:"speed,omitempty"`
	Effort string `json:"effort,omitempty"`
}
type modelEntry struct {
	at      time.Time
	choices []ModelChoice
}

var modelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/\[\]-]{0,127}$`)
var efforts = map[string]bool{"none": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true, "ultra": true}

func (m *Manager) CheckSession(id string) (string, error) {
	s, e := m.sessionForInput(id)
	if e != nil {
		return "", e
	}
	return s.Project, nil
}

// Models are discovered from the installed CLI, scoped to the selected project.
// This initializes a control channel only; it never starts an inference turn.
func (m *Manager) Models(agent, session string) ([]ModelChoice, error) {
	if agent != "codex" && agent != "claude" {
		return m.externalModels(agent, session)
	}
	path := ""
	if session != "" {
		var e error
		path, e = m.CheckSession(session)
		if e != nil {
			return nil, e
		}
	} else {
		ps := m.Projects()
		if len(ps) == 0 {
			return nil, errors.New("工作站未授权项目")
		}
		path = ps[0].Path
	}
	key := agent + "\n" + path
	m.catalogMu.Lock()
	defer m.catalogMu.Unlock()
	if c, ok := m.catalogs[key]; ok && time.Since(c.at) < 5*time.Minute {
		return c.choices, nil
	}
	ctx := m.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	args := []string{"app-server"}
	if agent == "claude" {
		args = []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--no-session-persistence", "--permission-mode", "default"}
	}
	cmd := exec.CommandContext(ctx, agent, args...)
	cmd.Dir = path
	cmd.Stderr = io.Discard
	in, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		return nil, errors.New("请在工作站安装并登录 Agent CLI")
	}
	defer func() {
		in.Close()
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		cmd.Wait()
	}()
	scan := bufio.NewScanner(out)
	scan.Buffer(make([]byte, 64<<10), 8<<20)
	call := func(method string, params map[string]any) (map[string]any, error) {
		id := UUID()
		v := map[string]any{"id": id, "method": method, "params": params}
		if agent == "claude" {
			v = map[string]any{"type": "control_request", "request_id": id, "request": params}
		}
		if e := json.NewEncoder(in).Encode(v); e != nil {
			return nil, e
		}
		for scan.Scan() {
			var d map[string]any
			if json.Unmarshal(scan.Bytes(), &d) != nil {
				continue
			}
			if agent == "codex" && str(d["id"]) == id {
				if d["error"] != nil {
					return nil, errors.New("模型目录读取失败")
				}
				return obj(d["result"]), nil
			}
			if agent == "claude" && str(d["type"]) == "control_response" {
				r := obj(d["response"])
				if str(r["request_id"]) == id {
					if r["subtype"] == "error" {
						return nil, errors.New("模型目录读取失败")
					}
					return obj(r["response"]), nil
				}
			}
		}
		return nil, errors.New("模型目录读取失败，请检查 CLI 登录状态")
	}
	init := map[string]any{"clientInfo": map[string]any{"name": "agentdeck", "version": "0.4.1"}}
	if agent == "claude" {
		init = map[string]any{"subtype": "initialize", "hooks": nil}
	}
	response, e := call("initialize", init)
	if e != nil {
		return nil, e
	}
	var choices []ModelChoice
	if agent == "codex" {
		json.NewEncoder(in).Encode(map[string]any{"method": "initialized"})
		cursor := ""
		for page := 0; page < 10; page++ {
			params := map[string]any{"limit": 100, "includeHidden": false}
			if cursor != "" {
				params["cursor"] = cursor
			}
			response, e = call("model/list", params)
			if e != nil {
				return nil, e
			}
			for _, raw := range arr(response["data"]) {
				v := obj(raw)
				if v["hidden"] == true {
					continue
				}
				c := ModelChoice{ID: str(v["model"]), Name: str(v["displayName"]), Default: v["isDefault"] == true, DefaultEffort: str(v["defaultReasoningEffort"]), Images: true, Efforts: []string{}}
				for _, raw := range arr(v["supportedReasoningEfforts"]) {
					eff := str(obj(raw)["reasoningEffort"])
					if efforts[eff] {
						c.Efforts = append(c.Efforts, eff)
					}
				}
				if modalities, ok := v["inputModalities"]; ok {
					c.Images = false
					for _, raw := range arr(modalities) {
						if str(raw) == "image" {
							c.Images = true
						}
					}
				}
				for _, tier := range arr(v["serviceTiers"]) {
					if str(obj(tier)["id"]) == "priority" {
						c.FastTier = "priority"
					}
				}
				if modelPattern.MatchString(c.ID) {
					choices = append(choices, c)
				}
			}
			cursor = str(response["nextCursor"])
			if cursor == "" {
				break
			}
		}
	} else {
		for _, raw := range arr(response["models"]) {
			v := obj(raw)
			c := ModelChoice{ID: str(v["value"]), Name: str(v["displayName"]), Default: str(v["value"]) == "default", Images: true, Efforts: []string{}}
			if v["supportsFastMode"] == true {
				c.FastTier = "fast"
			}
			// Claude advertises its own effort scale; never copy Codex levels.
			for _, raw := range arr(v["supportedEffortLevels"]) {
				eff := str(raw)
				if eff == "low" || eff == "medium" || eff == "high" || eff == "xhigh" || eff == "max" {
					c.Efforts = append(c.Efforts, eff)
				}
			}
			if modelPattern.MatchString(c.ID) {
				choices = append(choices, c)
			}
		}
	}
	if len(choices) == 0 {
		return nil, errors.New("Agent 未返回模型目录，请更新 CLI 后重试")
	}
	if m.catalogs == nil {
		m.catalogs = map[string]modelEntry{}
	}
	m.catalogs[key] = modelEntry{time.Now(), choices}
	return choices, nil
}
func (m *Manager) validateOptions(ssID, agent string, o TurnOptions) error {
	if o.Speed != "" && o.Speed != "fast" && o.Speed != "standard" {
		return errors.New("无效速度选项")
	}
	if o.Model == "" && o.Effort == "" && o.Speed == "" {
		return nil
	}
	if !modelPattern.MatchString(o.Model) {
		return errors.New("请选择模型后设置 effort")
	}
	cs, e := m.Models(agent, ssID)
	if e != nil {
		return e
	}
	for _, c := range cs {
		if c.ID != o.Model {
			continue
		}
		if o.Speed == "fast" && c.FastTier == "" {
			return errors.New("所选模型不支持快速模式")
		}
		if o.Speed != "" && (agent != "codex" && agent != "claude") && c.FastTier == "" {
			return errors.New("当前 Agent 没有独立快速模式，请选择原生 Variant")
		}
		if o.Effort == "" {
			return nil
		}
		for _, eff := range c.Efforts {
			if eff == o.Effort {
				return nil
			}
		}
		return errors.New("所选模型不支持该 effort")
	}
	return errors.New("模型不在工作站目录中，请刷新后重新选择")
}
