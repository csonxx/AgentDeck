package presentation

import "testing"

func TestControlFiltering(t *testing.T) {
	for _, s := range []string{"<environment_context><cwd>/private</cwd></environment_context>", "# AGENTS.md instructions for /private\nsecret", "<system-reminder>hidden</system-reminder>"} {
		if Clean(s) != "" {
			t.Fatal("control context visible")
		}
	}
	s := "<environment_context>private</environment_context>请修复登录 <div>hello</div>\n```html\n<a href=\"x\">x</a>\n```"
	clean := Clean(s)
	if clean != "请修复登录 <div>hello</div>\n```html\n<a href=\"x\">x</a>\n```" {
		t.Fatal("human XML changed")
	}
	if Title("  标题\n 第二行 ") != "标题 第二行" {
		t.Fatal("multiline title")
	}
	if Clean(`<send_user_message_question_reply>[{"answer":"Codex + Claude Code"}]</send_user_message_question_reply>`) != "Codex + Claude Code" {
		t.Fatal("answer hidden")
	}
}
