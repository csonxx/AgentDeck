package agent

import (
	"agentdeck/internal/store"
	"errors"
	"strings"
)

// Never classify model-generated prose or copy provider errors/context into UI.
// Only structured, native error discriminants cross this presentation boundary.
func nativeIssue(code string) store.SessionIssue {
	i := store.SessionIssue{Code: code, Action: "desktop"}
	switch code {
	case "safety_ended":
		i.Message = "平台已因安全检查结束此会话，请在电脑端查看；原会话不能继续发送"
		i.BlocksInput = true
	case "safety_review":
		i.Message = "平台已暂停此会话，请在电脑端完成安全复核后刷新"
		i.BlocksInput = true
	case "safety_refusal":
		i.Message = "Agent 拒绝了本次请求，请查看原会话说明"
		i.Action = "review"
	case "context_limit":
		i.Message = "会话上下文已达上限，请在电脑端压缩上下文或新建会话"
		// Context compaction/recovery remains the native executor's decision.
		// A failed turn is not a permanently closed conversation.
	case "usage_limit":
		i.Message = "Agent 额度或速率已达上限，请查看账号用量后再试"
		i.Action = "wait"
	case "authentication":
		i.Message = "Agent 登录或授权已失效，请在电脑端重新授权后再试"
		i.Action = "login"
	case "network":
		i.Message = "Agent 上游连接失败，请检查工作站网络后再试"
		i.Action = "retry"
	case "retrying":
		i.Message = "Agent 正在自动重试，请等待原会话结果"
		i.Action = "wait"
	case "output_limit":
		i.Message = "本次输出达到上限，请核对已有回复后决定是否继续"
		i.Action = "review"
	case "desktop_action":
		i.Message = "Agent 正在等待手机暂不支持的操作，请在电脑端处理后刷新"
		i.BlocksInput = true
	case "unsupported_state":
		i.Message = "Agent 返回了尚未兼容的会话状态，请在电脑端核对后刷新"
	default:
		i.Code = "execution_error"
		i.Message = "Agent 执行失败，请在电脑端查看具体原因后再试"
		i.Action = "review"
	}
	return i
}
func codexSessionIssue(err map[string]any) store.SessionIssue {
	code := str(err["codexErrorInfo"])
	info := obj(err["codexErrorInfo"])
	if code == "" {
		for key := range info {
			code = key
			break
		}
	}
	switch code {
	case "misalignmentPolicyViolation":
		// Native review capabilities are not reproduced remotely. The official
		// desktop decides whether a flagged turn can be reviewed or must end.
		m := obj(err["misalignment"])
		if str(m["detailedExplanation"]) != "" && str(obj(m["steer"])["message"]) != "" {
			return nativeIssue("safety_review")
		}
		return nativeIssue("safety_ended")
	case "contextWindowExceeded":
		return nativeIssue("context_limit")
	case "usageLimitExceeded", "rateLimitExceeded":
		return nativeIssue("usage_limit")
	case "unauthorized", "authenticationError":
		return nativeIssue("authentication")
	case "httpConnectionFailed", "responseStreamConnectionFailed", "responseStreamDisconnected", "responseTooManyFailedAttempts":
		return nativeIssue("network")
	case "responseTooLarge":
		return nativeIssue("output_limit")
	}
	return nativeIssue("execution_error")
}
func desktopSessionIssue(state map[string]any) store.SessionIssue {
	turns := desktopTurns(state)
	if len(turns) == 0 {
		return store.SessionIssue{}
	}
	last := obj(turns[len(turns)-1])
	status := str(last["status"])
	if status == "failed" {
		return codexSessionIssue(obj(last["error"]))
	}
	if status != "" && status != "inProgress" && status != "completed" && status != "interrupted" {
		return nativeIssue("unsupported_state")
	}
	if status == "inProgress" {
		for _, raw := range arr(state["requests"]) {
			method := str(obj(raw)["method"])
			if method != "" && method != "item/tool/requestUserInput" && method != "tool/requestUserInput" {
				return nativeIssue("desktop_action")
			}
		}
	}
	return store.SessionIssue{}
}
func checkSessionInput(s store.Session) error {
	if s.Issue.BlocksInput {
		return &InputNotSentError{Err: errors.New(s.Issue.Message)}
	}
	return nil
}
func openCodeIssue(err map[string]any) store.SessionIssue {
	switch str(err["name"]) {
	case "ProviderAuthError", "AuthError":
		return nativeIssue("authentication")
	case "ContextOverflowError":
		return nativeIssue("context_limit")
	case "MessageOutputLengthError":
		return nativeIssue("output_limit")
	case "APIError":
		status := obj(err["data"])["statusCode"]
		if status == float64(401) || status == float64(403) {
			return nativeIssue("authentication")
		}
		if status == float64(429) {
			return nativeIssue("usage_limit")
		}
		if status == float64(502) || status == float64(503) || status == float64(504) {
			return nativeIssue("network")
		}
	}
	return nativeIssue("execution_error")
}
func claudeSessionIssue(d map[string]any) store.SessionIssue {
	switch strings.ToLower(str(d["error"])) {
	case "authentication_failed":
		return nativeIssue("authentication")
	case "rate_limit":
		return nativeIssue("usage_limit")
	case "billing_error":
		return nativeIssue("usage_limit")
	}
	if str(d["subtype"]) == "error_max_turns" {
		return nativeIssue("output_limit")
	}
	return nativeIssue("execution_error")
}
