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

本机桥接会对 Codex 路由器的 `client-discovery-request` 明确回复 `canHandle: false`。AgentDeck 是会话旁观者，不持有执行权；不能忽略这类询问，否则查找未加载的原会话可能等待所有静默连接超时。回应逻辑覆盖请求等待、快照读取及空闲实时订阅，不会发送消息、创建分支或获取写锁。短消息在已加载会话上的结果不能覆盖这个场景。

The bridge explicitly answers native `client-discovery-request` probes with `canHandle: false`. AgentDeck observes conversations and never owns execution. Ignoring probes makes discovery for an unloaded original wait for silent connections to time out. Requests, snapshots and idle live subscriptions all answer probes; doing so sends no input, creates no branch and acquires no writer lock. Fast sends to an already loaded thread do not cover this case.

其他本机客户端也可能静默。手机明确发送时，首次执行端查找最多探测 1 秒，然后才尝试唤起同一个已保存的会话 ID 并再次确认所有者；不会把超时当作已收到消息，也不会创建其他执行器。发送前连接或校验失败明确记为未发送，提交后丢失回执仍保留未知结果，禁止盲目重发。

Other local clients may also stay silent. Explicit phone input probes owner discovery for at most one second before waking the same saved ID and rediscovering its owner. A timeout is never an acknowledgement and never creates another executor. Pre-submit connection/validation failures are marked unsent; an acknowledgement lost after submission remains uncertain and is not blindly replayed.

App 的设置 → 问题反馈可附带本机加密诊断记录；发送中、超时和未确认的请求也有编号。用反馈中的 `request_id` 关联下方本机日志。详见 [反馈](FEEDBACK.md)。

Settings → Report a problem includes encrypted phone diagnostics, including in-flight and timed-out request IDs. Correlate their `request_id` with workstation logs below. See [Feedback](FEEDBACK.md).

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
