package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Keep answers as arrays until the native boundary. Older clients send strings.
type questionAnswers map[string][]string

func parseQuestionAnswers(answer string) (questionAnswers, error) {
	var raw map[string]json.RawMessage
	if len(answer) > 128<<10 || json.Unmarshal([]byte(answer), &raw) != nil || raw == nil {
		return nil, errors.New("提问回答格式无效")
	}
	out := questionAnswers{}
	for key, data := range raw {
		if strings.TrimSpace(string(data)) == "null" {
			return nil, errors.New("提问回答格式无效")
		}
		var one string
		var many []string
		if json.Unmarshal(data, &one) == nil {
			many = []string{one}
		} else if string(data) == "null" || json.Unmarshal(data, &many) != nil {
			return nil, errors.New("提问回答格式无效")
		}
		for _, value := range many {
			if strings.TrimSpace(value) == "" {
				continue
			}
			out[key] = append(out[key], value)
		}
	}
	return out, nil
}

func questionID(q map[string]any, index int) string {
	if id := str(q["id"]); id != "" {
		return id
	}
	if id := str(q["question"]); id != "" {
		return id
	}
	return fmt.Sprint(index)
}
func questionList(params map[string]any) []any {
	if questions, ok := params["questions"].([]any); ok {
		return questions
	}
	return arr(obj(params["input"])["questions"])
}
func questionMultiple(q map[string]any) bool {
	return q["multiSelect"] == true || q["multiple"] == true
}
func validateQuestionAnswers(questions []any, answers questionAnswers, kind string) error {
	if len(questions) == 0 {
		return errors.New("提问已过期")
	}
	keys := map[string]bool{}
	for i, raw := range questions {
		q := obj(raw)
		key := questionID(q, i)
		if kind == "claude" && str(q["question"]) != "" {
			key = str(q["question"])
		}
		if keys[key] {
			return errors.New("提问题目标识重复")
		}
		keys[key] = true
		values := answers[key]
		if len(values) == 0 || (!questionMultiple(q) && len(values) != 1) {
			return errors.New("请回答每个问题，并核对单选或多选")
		}
		options := arr(q["options"])
		custom := len(options) == 0 || q["isOther"] == true || (kind != "codex" && q["custom"] != false && q["isOther"] != false)
		seen := map[string]bool{}
		for _, value := range values {
			if seen[value] {
				return errors.New("回答中有重复选项")
			}
			seen[value] = true
			found := false
			for _, option := range options {
				if str(obj(option)["label"]) == value {
					found = true
					break
				}
			}
			if !found && !custom {
				return errors.New("请选择提问提供的选项")
			}
		}
	}
	for key := range answers {
		if !keys[key] {
			return errors.New("回答包含不属于此提问的问题")
		}
	}
	return nil
}
func codexQuestionResponse(answers questionAnswers) map[string]any {
	out := map[string]any{}
	for key, values := range answers {
		out[key] = map[string]any{"answers": values}
	}
	return map[string]any{"answers": out}
}
func claudeQuestionResponse(answers questionAnswers) map[string]string {
	out := map[string]string{}
	// AskUserQuestion's native SDK answer map is string-valued, including multi-select.
	for key, values := range answers {
		out[key] = strings.Join(values, ", ")
	}
	return out
}
func questionPresentation(params map[string]any, kind string) map[string]any {
	out := map[string]any{}
	for k, v := range params {
		if kind == "codex" {
			switch k {
			case "threadId", "turnId", "itemId", "questions", "isBlocking":
			default:
				continue
			}
		}
		out[k] = v
	}
	out["question_kind"] = kind
	return out
}
