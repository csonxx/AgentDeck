package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

func claudeHumanInput(d map[string]any) bool {
	if d["isMeta"] == true {
		return false
	}
	m := obj(d["message"])
	if text, ok := m["content"].(string); ok {
		return strings.TrimSpace(text) != ""
	}
	for _, block := range arr(m["content"]) {
		v := obj(block)
		if str(v["type"]) == "text" && strings.TrimSpace(str(v["text"])) != "" {
			return true
		}
	}
	return false
}

func claudeInterruptedInput(d map[string]any) bool {
	m := obj(d["message"])
	text := str(m["content"])
	for _, block := range arr(m["content"]) {
		if str(obj(block)["type"]) == "text" {
			text += str(obj(block)["text"])
		}
	}
	text = strings.TrimSpace(text)
	return text == "[Request interrupted by user]" || text == "[Request interrupted by user for tool use]"
}

func claudeLogStatus(d map[string]any) (string, string) {
	at := str(d["timestamp"])
	switch str(d["type"]) {
	case "user":
		if claudeInterruptedInput(d) {
			return "interrupted", at
		}
		if claudeHumanInput(d) {
			return "running", at
		}
	case "assistant":
		if d["isApiErrorMessage"] == true {
			return "failed", at
		}
		switch str(obj(d["message"])["stop_reason"]) {
		case "end_turn", "stop_sequence":
			return "completed", at
		default:
			return "running", at
		}
	case "result":
		if d["is_error"] == true {
			return "failed", at
		}
		return "completed", at
	case "system":
		if str(d["subtype"]) == "turn_duration" {
			return "completed", at
		}
	}
	return "", ""
}

// Only status/timestamps leave this bounded reader. No transcript, tool payload,
// context or credentials are stored or sent by sidebar status observation.
func readClaudeTailStatus(path string, size int64) (string, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	for _, budget := range []int64{256 << 10, 2 << 20} {
		start := max(int64(0), size-budget)
		data := make([]byte, size-start)
		n, e := f.ReadAt(data, start)
		if e != nil && e != io.EOF {
			return "", "", e
		}
		data = data[:n]
		if start > 0 {
			if i := bytes.IndexByte(data, '\n'); i >= 0 {
				data = data[i+1:]
			} else {
				continue
			}
		}
		// Unterminated records may still be in the middle of a write.
		end := bytes.LastIndexByte(data, '\n')
		if end < 0 {
			continue
		}
		data = data[:end]
		lines := bytes.Split(data, []byte{'\n'})
		for i := len(lines) - 1; i >= 0; i-- {
			var d map[string]any
			if json.Unmarshal(lines[i], &d) != nil {
				continue
			}
			if status, at := claudeLogStatus(d); status != "" && at != "" {
				return status, at, nil
			}
		}
		if start == 0 {
			return "", "", nil
		}
	}
	return "", "", nil
}

func (f *Follower) refreshClaudeStatuses() {
	if f.claude == nil || !f.claude.ready {
		return
	}
	c := f.claude
	if c.statusStamps == nil {
		c.statusStamps = map[string]string{}
	}
	for id, entry := range c.entries {
		if entry.CatalogHidden {
			continue
		}
		path := c.paths[id]
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		stamp := fmt.Sprintf("%d/%d", info.Size(), info.ModTime().UnixNano())
		if c.statusStamps[path] == stamp {
			continue
		}
		status, at, err := readClaudeTailStatus(path, info.Size())
		if err != nil {
			continue
		}
		if status != "" {
			if err = f.Normalizer.Store.ObserveClaudeStatus(id, status, at); err != nil {
				continue
			}
		}
		c.statusStamps[path] = stamp
	}
}
