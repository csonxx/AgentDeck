package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"
)

// Only presentation settings are persisted; raw CLI config, env and credentials never leave the workstation.
type SessionSettings struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
	Speed  string `json:"speed"`
	Source string `json:"source"`
}

func nativeSettings(d map[string]any, agent string) SessionSettings {
	v := SessionSettings{Source: "last_turn"}
	if agent == "codex" && str(d["type"]) == "turn_context" {
		p := obj(d["payload"])
		v.Model = str(p["model"])
		v.Effort = str(p["effort"])
		if v.Effort == "" {
			v.Effort = str(p["reasoning_effort"])
		}
		v.Speed = str(p["service_tier"])
	} else if agent == "claude" {
		v.Model = str(obj(d["message"])["model"])
		if str(d["type"]) == "system" && str(d["subtype"]) == "init" {
			v.Model = str(d["model"])
		}
		v.Effort = str(d["effort"])
		if v.Effort == "" {
			v.Effort = str(d["effortLevel"])
		}
		if fast, ok := d["fastMode"].(bool); ok {
			if fast {
				v.Speed = "fast"
			} else {
				v.Speed = "standard"
			}
		}
		if speed := str(obj(d["message"])["speed"]); speed != "" {
			v.Speed = speed
		}
	}
	if !modelPattern.MatchString(v.Model) {
		v.Model = ""
	}
	if !efforts[v.Effort] {
		v.Effort = ""
	}
	if v.Speed == "priority" {
		v.Speed = "fast"
	}
	if v.Speed == "default" {
		v.Speed = "standard"
	}
	if v.Speed != "fast" && v.Speed != "standard" {
		v.Speed = ""
	}
	return v
}
func (n *Normalizer) recordNativeSettings(d map[string]any, agent string, st *State) {
	if n.Store == nil || st.Session == "" {
		return
	}
	next := nativeSettings(d, agent)
	if next.Model == "" && next.Effort == "" && next.Speed == "" {
		return
	}
	n.nativeSettingsMu.Lock()
	defer n.nativeSettingsMu.Unlock()
	var old SessionSettings
	_ = n.Store.Get("native_settings", st.Session, &old)
	if next.Model == "" {
		next.Model = old.Model
	}
	if next.Effort == "" && (agent != "claude" || next.Model == old.Model) {
		next.Effort = old.Effort
	}
	if next.Speed == "" && (agent != "claude" || next.Model == old.Model) {
		next.Speed = old.Speed
	}
	if next != old {
		_ = n.Store.Put("native_settings", st.Session, next)
	}
}
func (m *Manager) SessionSettings(id string) (SessionSettings, error) {
	ss, e := m.sessionForInput(id)
	if e != nil {
		return SessionSettings{}, e
	}
	if ss.Agent == "claude" {
		m.mu.Lock()
		r := m.runs[id]
		m.mu.Unlock()
		if r != nil {
			select {
			case <-r.done:
			default:
				r.mu.Lock()
				current := r.settings
				r.mu.Unlock()
				if current.Model != "" {
					return current, nil
				}
			}
		}
	}
	var result SessionSettings
	_ = m.Store.Get("native_settings", id, &result)
	if m.ReadSettings != nil {
		if native, err := m.ReadSettings(id); err == nil && native.Source != "unknown" {
			result = native
		}
	}
	if ss.Agent == "codex" {
		if reader, ok := m.Desktop.(interface {
			CurrentSettings(context.Context, string, string) (SessionSettings, error)
		}); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if live, e := reader.CurrentSettings(ctx, id, ss.Project); e == nil && live.Model != "" {
				return live, nil
			}
		}
	}
	if ss.Agent == "opencode" {
		connector, _, err := m.externalSession(id)
		if api, ok := connector.(*openCode); ok && err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var messages []map[string]any
			if api.request(ctx, "GET", "/session/"+nativeID(ss)+"/message?limit=10", ss.Project, nil, &messages) == nil {
				for i := len(messages) - 1; i >= 0; i-- {
					info := obj(messages[i]["info"])
					model := obj(info["model"])
					if str(info["role"]) == "user" && str(model["modelID"]) != "" {
						return SessionSettings{Model: str(model["providerID"]) + "/" + str(model["modelID"]), Effort: str(info["variant"]), Source: "last_turn"}, nil
					}
				}
			}
		}
	}
	if ss.Agent == "qoder" || ss.Agent == "trae" {
		connector, _, err := m.externalSession(id)
		if api, ok := connector.(*acpConnector); ok && err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if runtime, e := api.get(ctx, ss); e == nil {
				runtime.mu.Lock()
				defer runtime.mu.Unlock()
				live := SessionSettings{Source: "live"}
				for _, option := range runtime.configOptions {
					value := str(option["currentValue"])
					switch configCategory(option) {
					case "model":
						live.Model = value
					case "thought_level":
						live.Effort = value
					case "speed":
						fast, standard := acpSpeedValues(option)
						if value == fast && fast != "" {
							live.Speed = "fast"
						} else if value == standard && standard != "" {
							live.Speed = "standard"
						}
					}
				}
				return live, nil
			}
		}
	}

	if result.Source == "" {
		result.Source = "unknown"
	}
	return result, nil
}
func (b *DesktopBridge) CurrentSettings(ctx context.Context, id, project string) (SessionSettings, error) {
	c, e := b.connect(ctx, id)
	if e != nil {
		return SessionSettings{}, e
	}
	defer c.conn.Close()
	stop := context.AfterFunc(ctx, func() { c.conn.Close() })
	defer stop()
	state, e := c.snapshot(id, project)
	if e != nil {
		return SessionSettings{}, e
	}
	return desktopSettings(state), nil
}
func desktopSettings(state map[string]any) SessionSettings {
	// Native snapshots expose configured values at the top level or conversation settings.
	v := SessionSettings{Source: "live"}
	for _, d := range []map[string]any{obj(obj(state["latestCollaborationMode"])["settings"]), obj(state["settings"]), state} {
		if model := str(d["model"]); modelPattern.MatchString(model) {
			v.Model = model
		}
		for _, k := range []string{"reasoningEffort", "reasoning_effort", "effort"} {
			if eff := str(d[k]); efforts[eff] {
				v.Effort = eff
			}
		}
		if speed := str(d["serviceTier"]); speed == "fast" || speed == "priority" {
			v.Speed = "fast"
		} else if speed == "default" {
			v.Speed = "standard"
		}
	}
	return v
}
func applySpeed(params map[string]any, o TurnOptions) {
	if o.Speed == "fast" {
		params["serviceTier"] = "priority"
	}
	if o.Speed == "standard" {
		params["serviceTier"] = "default"
	}
}

// Read only a bounded recent window, using the already-discovered source path.
func (f *Follower) Settings(id string) (SessionSettings, error) {
	f.mu.Lock()
	path, agent := "", ""
	for source, state := range f.states {
		if state.Session == id {
			path = source
			break
		}
	}
	for _, root := range f.Roots {
		if strings.HasPrefix(path, root.Path+string(os.PathSeparator)) {
			agent = root.Agent
			break
		}
	}
	f.mu.Unlock()
	result := SessionSettings{Source: "last_turn"}
	if path == "" {
		return result, os.ErrNotExist
	}
	file, e := os.Open(path)
	if e != nil {
		return result, e
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil {
		return result, e
	}
	start := max(int64(0), info.Size()-(8<<20))
	raw := make([]byte, info.Size()-start)
	count, e := file.ReadAt(raw, start)
	if e != nil && e != io.EOF {
		return result, e
	}
	raw = raw[:count]
	if start > 0 {
		if cut := bytes.IndexByte(raw, '\n'); cut >= 0 {
			raw = raw[cut+1:]
		}
	}
	scan := bufio.NewScanner(bytes.NewReader(raw))
	scan.Buffer(make([]byte, 64<<10), 8<<20)
	for scan.Scan() {
		var d map[string]any
		if json.Unmarshal(scan.Bytes(), &d) != nil {
			continue
		}
		v := nativeSettings(d, agent)
		if v.Model != "" {
			if agent == "claude" && v.Model != result.Model {
				result.Effort = ""
				result.Speed = ""
			}
			result.Model = v.Model
		}
		if v.Effort != "" {
			result.Effort = v.Effort
		}
		if v.Speed != "" {
			result.Speed = v.Speed
		}
	}
	if result.Model == "" && result.Effort == "" {
		result.Source = "unknown"
	}
	return result, nil
}
