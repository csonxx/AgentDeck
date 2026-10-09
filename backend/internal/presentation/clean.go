// Package presentation excludes known host-injected context while preserving
// ordinary HTML/XML and fenced code in human messages.
package presentation

import (
	"regexp"
	"strings"
)

var control = []string{"environment_context", "user_instructions", "permissions instructions", "collaboration_mode", "app-context", "skills_instructions", "send_user_message_question_reply", "external_codex_apps_open_page", "codex_apps_open_page_instructions", "system-reminder", "local-command-caveat", "local-command-stdout", "command-name", "command-message", "command-args", "task-notification"}
var answer = regexp.MustCompile(`"answer"\s*:\s*"([^"\\]*(?:\\.[^"\\]*)*)"`)

func Clean(s string) string {
	if strings.HasPrefix(strings.TrimSpace(s), "# AGENTS.md instructions for ") || strings.HasPrefix(strings.TrimSpace(s), "<turn_aborted>") {
		return ""
	}
	for _, tag := range control {
		start, end := "<"+tag+">", "</"+tag+">"
		for {
			i := strings.Index(s, start)
			if i < 0 {
				break
			}
			j := strings.Index(s[i+len(start):], end)
			if j < 0 {
				s = s[:i]
				break
			}
			j += i + len(start)
			replacement := ""
			if tag == "send_user_message_question_reply" {
				for _, m := range answer.FindAllStringSubmatch(s[i+len(start):j], -1) {
					replacement += m[1] + "\n"
				}
			}
			s = s[:i] + replacement + s[j+len(end):]
		}
	}
	return strings.TrimSpace(s)
}
func Title(s string) string {
	s = Clean(s)
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > 60 {
		s = string(r[:60]) + "…"
	}
	return s
}
