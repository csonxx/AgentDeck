# 本机管理与排障 / Workstation controls and diagnostics

## 关联网关 / Link a gateway

1. 启动 S-LOCAL。保留已有来源目录、项目、附件、Agent 配置和桌面 IPC 等参数，使用 `serve --relay --data <原数据目录>`。未关联时也能启动；手机通过网关连接，本机管理页固定监听回环地址。 / Start S-LOCAL with `serve --relay --data <existing-directory>`, retaining source, project, attachment, agent and desktop IPC flags. An unlinked workstation starts successfully; phones connect through the gateway while local controls listen on loopback.
2. 浏览器打开 **http://127.0.0.1:8787/**，可直接收藏。页面随后端启动，不需要另起控制台、启动文件或复制凭据；刷新和新标签页都可直接访问。 / Open **http://127.0.0.1:8787/** and bookmark it. Controls start with S-LOCAL. No second console, launcher or copied credential is required; reloads and fresh tabs work directly.
3. 在「网关连接」输入完整 HTTPS 地址和工作站名称，点击「通过飞书关联工作站」，再点击「打开飞书授权页面」。测试地址为 `https://web-test-omnivibe.ttyuyin.com/agentdeck`，保留路径前缀。 / Enter the full HTTPS gateway address and workstation name, start enrollment, then open the Feishu authorization page. Preserve `/agentdeck` for the testing address above.
4. 飞书确认后返回管理页，关联会加密保存并自动建立出站 WSS 隧道。页面每两秒刷新，显示在线、重连、最近连接时间、尝试次数及失败类别。重启后沿用原关联，不会自动覆盖已有账号。 / Return after Feishu authorization. Enrollment is saved encrypted and the outbound WSS tunnel starts automatically. Status refreshes every two seconds, including online/reconnecting state, last connection, attempts and error category. Restart reuses the saved association; an existing association is never silently overwritten.

旧终端流程 `local-login` 仍可用；该命令单独打开数据库，所以运行它前仍需停止使用同一数据目录的后端。新网页流程通过运行中的后端操作，无此要求。 / The terminal `local-login` flow remains available and requires stopping the backend using that directory. Browser enrollment operates through the running backend and has no such requirement.

## 确认手机 / Approve a phone

1. App 选择「通过网关 · 飞书登录」，填写相同网关地址，以**同一个飞书账号**授权，选择在线工作站。 / In the app, choose gateway/Feishu sign-in, use the same gateway and **same Feishu account**, then select the online workstation.
2. 电脑管理页出现「手机配对请求」。把**手机显示的 12 位十六进制校验码**填入电脑页面；可带连字符。点击「校验并批准配对」。错误码和过期请求会拒绝。 / A pairing request appears on the computer. Enter the phone's 12-character hexadecimal comparison code, with optional hyphens, and approve. Wrong codes and expired requests are rejected.
3. 电脑显示「校验一致，已批准」后，在手机点击「两边一致，等待工作站确认」。手机完成加密配对并连接。若手机已点击等待，批准后自动继续。 / Once the computer confirms approval, tap the matching-code confirmation on the phone. If the phone is already waiting, it proceeds automatically.
4. 请求通常五分钟内有效。过期后在手机返回工作站列表，重新选择本机；无需重新关联电脑。 / Requests normally expire after five minutes. Return to the workstation list and select the computer again; workstation enrollment is retained.

多个请求分别输入各自手机显示的码。页面不会自动批准，也不会把电脑端预期码回填给用户。重置 App 后重新配对，电脑绑定不受影响。 / Approve each request using the corresponding phone code. The page never approves automatically or fills the expected code for you. An app reset requires a new pairing while retaining workstation enrollment.

## 解除和重新关联 / Unlink and link again

1. 在「网关连接」点击「解除网关关联」，核对影响后点击「确认解除关联」。网关先验证工作站签名，只撤销调用者自己的身份及手机路由，返回绑定本次请求的加密回执。 / Choose Unlink gateway, review the effect, and confirm. The gateway authenticates the workstation signature, revokes only that workstation and its grants, and returns an encrypted receipt bound to the request.
2. 回执验证通过后，本机停止隧道及自动重连，原子清除关联凭据和未完成的配对；会话历史、已保存设备密钥及 Agent 配置保留。网关不可用或版本不支持时保留本机关联，并显示错误供重试。 / After validating the receipt, S-LOCAL stops the tunnel/retries and atomically clears enrollment and unfinished pairings. History, saved device keys and agent settings remain. An unreachable or unsupported gateway leaves local enrollment intact with a retryable error.
3. 页面立即显示「尚未关联」，并恢复网关地址和工作站名称输入框。点击「连接网关 · 飞书授权」，完成飞书授权后自动连接。新工作站身份及密钥与旧关联不同，手机需要重新选择工作站和配对。 / The page switches to Unlinked and restores the URL/name form. Start Feishu authorization and the tunnel connects automatically. The new identity/key differs from the revoked one, so phones must select and pair again.
4. 默认数据目录为 `~/.agentdeck`，页面展示实际目录。迁移已有安装时先正常停止后端，保留原数据库和 `storage.key` 配套迁移，不生成新密钥覆盖旧数据库；已有 `tools` 目录保持不变。 / The default directory is `~/.agentdeck`, with the effective path shown in controls. Stop an existing backend cleanly before moving its database and matching storage.key together. Never replace the old key with a new key; preserve existing tools.

## 本机访问边界 / Local access boundary

- S-LOCAL 默认同时启动本机管理页面，固定监听 `127.0.0.1:8787`。`--console-listen 127.0.0.1:PORT` 修改端口，拒绝非回环地址；端口冲突明确报错，不自动跳到其他端口。`--no-console` 可禁用。S-GATEWAY 不启动该页面。 / S-LOCAL starts controls at fixed `127.0.0.1:8787`. A loopback-only port override is supported. Port conflicts fail explicitly instead of silently changing the URL. `--no-console` disables controls; S-GATEWAY never starts them.
- 浏览器同源脚本通过带专用 Header 的 POST 自动获取进程级随机凭据，仅保留在页面内存中；拒绝跨源 Origin、Fetch Metadata 和 CORS 预检。无需 Cookie、URL fragment、启动文件或浏览器持久存储。重启后刷新页面自动获取新凭据，已打开页面会自动恢复。 / A same-origin script uses a custom-header POST to bootstrap an in-memory process capability. Cross-origin requests and CORS preflights are rejected. No cookie, URL fragment, launcher or browser storage is needed. Reloading after a restart acquires the new capability; an open page also recovers automatically.
- 直接打开回环地址的使用方式信任本机用户及进程；Host/Origin 校验用于阻止其他网站跨源访问，不提供操作系统用户之间的隔离。请在可信工作站账号运行，公网和局域网不能访问。 / Direct loopback access trusts local users and processes. Host/Origin checks block other websites, rather than isolate OS users. Run under a trusted workstation account; public and LAN clients cannot access controls.
- 后端内置页面与后端共用生命周期。独立 `console --data <相同目录>` 命令保留；若后端已启动内置页面，应选用不同端口，或先用 `--no-console` 关闭内置页面。多实例也需分别设置端口。 / Built-in controls share the backend lifecycle. The standalone console remains available with a different port, or with built-in controls disabled. Multiple instances also require distinct ports.
- 校验 Host、Origin、Fetch Metadata，并设置 CSP、禁止嵌入、禁缓存。页面只允许关联状态、发起/取消关联、待配对列表、批准及解除关联；不代理设备 root、会话内容、shell 或通用 owner API。 / Host, Origin and Fetch Metadata checks, CSP, frame denial and no-store headers restrict access. Only association status/enrollment/cancellation and pairing list/approval plus self-unlink are exposed; device roots, sessions, shell execution and generic owner operations are excluded.

## 网关日志 / Gateway diagnostics

网关默认向标准错误输出可读文本，每行只有一个时间戳，并明确标注 `DEBUG` / `INFO` / `WARN` / `ERROR`；不再是标准日志时间后拼接 JSON。默认最低级别为 `INFO`。 / The gateway writes readable text to stderr with one timestamp and an explicit severity; it no longer appends JSON after a standard logger timestamp. The default minimum level is `INFO`.

```text
time=2026-10-10T08:30:00.000Z level=INFO msg=gateway_listening service=agentdeck-gateway listen=0.0.0.0:8080 public_url=https://example.com/agentdeck feishu_configured=true
time=2026-10-10T08:30:01.000Z level=WARN msg=http_request service=agentdeck-gateway request_id=example-request route=/v2/local/tunnel duration_ms=0 method=GET reason=local_authentication_rejected status=401
```

| 级别 / Level | 用途 / Usage |
| --- | --- |
| DEBUG | 成功的健康检查、时间校准、登录轮询。 / Successful health checks, clock synchronization and login polling. |
| INFO | 启动、登录、工作站上下线、配对、中继、解除关联及普通成功请求。 / Startup, login, workstation connections, pairing, relay, unlinking and ordinary successful requests. |
| WARN | HTTP 4xx 拒绝，包括认证、权限、冲突及限流；飞书拒绝授权或身份请求。 / HTTP 4xx rejections, including authentication, authorization, conflicts and rate limits; rejected Feishu identity requests. |
| ERROR | HTTP 5xx、管理 socket 失败、飞书配置/网络错误及上游 5xx。 / HTTP 5xx, admin socket failures, Feishu configuration/network errors and provider 5xx. |

启动时设置 `--log-level debug`，或环境变量 `AGENTDECK_LOG_LEVEL=debug`；支持 `debug`、`info`、`warn`、`error`，命令行优先。修改后需重启/重新部署；DEBUG 同样遵守脱敏规则。 / Set `--log-level debug` or `AGENTDECK_LOG_LEVEL=debug` at startup. Supported values are `debug`, `info`, `warn` and `error`; the CLI flag takes precedence. Restart/redeploy after changing it. DEBUG uses the same redaction rules.

代理原有访问日志需自行禁用敏感查询串；AgentDeck 的脱敏不会修改 Envoy/Ingress 的配置。 / Configure proxies separately to omit sensitive query strings; application redaction does not alter Envoy/Ingress access logging.

字段包括 `service`、`event`、`request_id`、固定模板 `route`、HTTP 状态、耗时及 `reason`。每次请求生成 `X-Request-ID` 响应头，可用它关联应用日志。工作站/租户/用户/流程/路由授权只记录不可逆短摘要 `*_ref`，用于关联事件。 / Fields include service, event, request ID, a fixed route template, HTTP status, duration and reason. The response `X-Request-ID` correlates logs. Stable short hashes correlate workstation, tenant, user, flow and grant identities without recording their original values.

记录登录流创建、飞书授权成功、token/user_info 失败阶段及安全数值错误码、工作站上下线、中继开始/结束、认证拒绝、离线/容量/代理失败。成功的健康检查、时间校准与授权轮询仅在 DEBUG 显示，失败仍按 WARN/ERROR 记录。 / Events cover flow creation, successful authorization, token/user_info failure stages and safe numeric provider codes, workstation online/offline, relay start/end and authentication/offline/capacity/proxy failures. Successful health, clock and login polling requests appear only at DEBUG; failures retain WARN/ERROR severity.

常见 `reason`：`feishu_not_configured`、`oauth_browser_cookie_mismatch`、`oauth_state_missing_expired_or_used`、`feishu_identity_resolution_failed`、`local_authentication_rejected`、`local_already_connected`、`local_offline`、`relay_proof_rejected`、`local_tunnel_proxy_failed`。 / These reason codes distinguish configuration, browser cookie/state, provider identity, workstation authentication/conflict/offline, relay authentication and tunnel failures.

日志不输出请求/响应正文、原始查询串/未知路径、认证 Header、Cookie、OAuth code/state、token、校验码、密钥或用户会话。网关诊断只有部署新二进制后才生效；本地源码通过测试不代表远端已发布。 / Request/response bodies, raw query strings/unknown paths, authorization headers, cookies, OAuth code/state, tokens, comparison codes, keys and user sessions are excluded. New diagnostics take effect only after deploying the updated gateway binary; local tests do not prove remote rollout.
