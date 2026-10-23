# 发送延迟 / Send latency

发送速度分为本地保存、传输、本机处理和 Agent 开始执行。收到发送回执不等于模型已开始输出。

Delivery has separate stages: durable local save, transport, workstation processing, and native execution. An acknowledgement does not mean the model has started responding.

## 排查 / Diagnostics

本机日志在 `~/.agentdeck/logs/local.stderr.log`。按同一个 `request` 对照以下记录；日志只记标识、状态与耗时，不记消息、附件或凭据。

Use `~/.agentdeck/logs/local.stderr.log` and correlate the same `request`. Diagnostics contain identifiers, status and timings, never message bodies, attachments or credentials.

| 记录 / Record | 含义 / Meaning |
|---|---|
| `agent_send_client` | `prepare_ms`：手机保存及准备；`request_ms`：发出到回执；`total_ms`：两者合计。 / Phone preparation, request-to-ack and their total. |
| `agent_send` | `queue_ms`：本机收包到获得命令执行权；`execute_ms`：本机执行及持久化回执。 / Workstation queue and command processing. |
| `codex_send` | `connect_ms`：连接及必要的原会话唤起；`snapshot_ms`：项目、安全状态校验；`submit_ms`：提交到原会话并等待确认。 / Native discovery/wake, validation, and submission. |
| `agent_send_running_client` | `total_ms`：手机观察到本次消息且会话运行中；`after_ack_ms`：回执之后的等待。仅在观察到真实状态时记录。 / Observed native running state, with time since acknowledgement. |

客户端使用单调时钟，本机各阶段也独立计时；不要直接相减两台设备的墙上时钟。断线重试继续使用原请求 ID，不会为了测速度重复发送。

Client and server durations use monotonic clocks independently. Do not subtract wall-clock timestamps across devices. Retries retain their original request IDs and diagnostics never resend input.

## 已验证的优化 / Verified changes

支持原子发送的本机后端会公布 `codex_send_direct`。App 直接发送到已有 Codex 会话，后端在同一个原生连接上校验项目、安全状态并投递，省去单独的关联请求和重复发现。旧后端继续走原流程。本地草稿仍在发送前加密持久化，去掉的是未改变草稿的重复写入。

Workstations advertising `codex_send_direct` validate and send to the existing Codex conversation in one operation. This removes the extra association round trip and repeated discovery. Older workstations retain the previous flow. Drafts remain encrypted and durable before sending; only an unchanged, duplicate write is removed.

只读探测原生前置步骤（不发送消息、不打开会话）：

Read-only native preflight diagnostics (no message sending or opening conversations):

```sh
cd backend
AGENTDECK_PROFILE_SESSION='<existing-open-session-id>' \
AGENTDECK_PROFILE_SOCKET="$HOME/.codex/ipc/ipc.sock" \
AGENTDECK_PROFILE_PROJECT='<exact-authorized-project-path>' \
GOWORK=off go test ./internal/agent \
  -run '^TestDesktopSendPrerequisitesReadOnlyLatency$' -count=1 -v
```

前置探测不能代表手机端到端发送速度。完整结论需要同一次真实发送的客户端与本机记录。

Preflight measurements do not establish phone-to-agent latency. An end-to-end conclusion needs correlated client and workstation records from the same real send.
