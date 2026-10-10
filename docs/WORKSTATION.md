# 本机管理与排障 / Workstation controls and diagnostics

## 关联网关 / Link a gateway

1. 启动 S-LOCAL。保留已有来源目录、项目、附件、Agent 配置和桌面 IPC 等参数，使用 `serve --relay --data <原数据目录>`。未关联时也能启动，不开手机可访问的 TCP 端口。 / Start S-LOCAL with `serve --relay --data <existing-directory>`, retaining source, project, attachment, agent and desktop IPC flags. An unlinked workstation starts successfully without a phone-facing TCP listener.
2. 在同一操作系统用户下另开终端运行 `artifacts/agentdeck console --data <相同目录>`。浏览器自动打开本机管理页面，保持两个进程运行。 / Run `artifacts/agentdeck console --data <same-directory>` as the same OS user in another terminal. The browser opens local controls; keep both processes running.
3. 在「网关连接」输入完整 HTTPS 地址和工作站名称，点击「通过飞书关联工作站」，再点击「打开飞书授权页面」。测试地址为 `https://web-test-omnivibe.ttyuyin.com/agentdeck`，保留路径前缀。 / Enter the full HTTPS gateway address and workstation name, start enrollment, then open the Feishu authorization page. Preserve `/agentdeck` for the testing address above.
4. 飞书确认后返回管理页，关联会加密保存并自动建立出站 WSS 隧道。页面每两秒刷新，显示在线、重连、最近连接时间、尝试次数及失败类别。重启后沿用原关联，不会自动覆盖已有账号。 / Return after Feishu authorization. Enrollment is saved encrypted and the outbound WSS tunnel starts automatically. Status refreshes every two seconds, including online/reconnecting state, last connection, attempts and error category. Restart reuses the saved association; an existing association is never silently overwritten.

旧终端流程 `local-login` 仍可用；该命令单独打开数据库，所以运行它前仍需停止使用同一数据目录的后端。新网页流程通过运行中的后端操作，无此要求。 / The terminal `local-login` flow remains available and requires stopping the backend using that directory. Browser enrollment operates through the running backend and has no such requirement.

## 确认手机 / Approve a phone

1. App 选择「通过网关 · 飞书登录」，填写相同网关地址，以**同一个飞书账号**授权，选择在线工作站。 / In the app, choose gateway/Feishu sign-in, use the same gateway and **same Feishu account**, then select the online workstation.
2. 电脑管理页出现「手机配对请求」。把**手机显示的 12 位十六进制校验码**填入电脑页面；可带连字符。点击「校验并批准配对」。错误码和过期请求会拒绝。 / A pairing request appears on the computer. Enter the phone's 12-character hexadecimal comparison code, with optional hyphens, and approve. Wrong codes and expired requests are rejected.
3. 电脑显示「校验一致，已批准」后，在手机点击「两边一致，等待工作站确认」。手机完成加密配对并连接。若手机已点击等待，批准后自动继续。 / Once the computer confirms approval, tap the matching-code confirmation on the phone. If the phone is already waiting, it proceeds automatically.
4. 请求通常五分钟内有效。过期后在手机返回工作站列表，重新选择本机；无需重新关联电脑。 / Requests normally expire after five minutes. Return to the workstation list and select the computer again; workstation enrollment is retained.

多个请求分别输入各自手机显示的码。页面不会自动批准，也不会把电脑端预期码回填给用户。重置 App 后重新配对，电脑绑定不受影响。 / Approve each request using the corresponding phone code. The page never approves automatically or fills the expected code for you. An app reset requires a new pairing while retaining workstation enrollment.

## 本机访问边界 / Local access boundary

- 管理页面为独立 `console` 进程，默认监听系统分配的 `127.0.0.1` 端口；`--console-listen 127.0.0.1:PORT` 可指定端口，拒绝非回环地址。 / Controls use a separate console process and a dynamically allocated IPv4 loopback port. An explicit loopback port is supported; non-loopback addresses are rejected.
- API 使用进程级随机凭据，浏览器首次打开从 URL fragment 读取后立即移除；仅放在该标签页的临时 `sessionStorage`，便于刷新页面，不使用持久 `localStorage`、Cookie、查询串或日志。它不含手机 root 或会话数据，关闭标签页后清除，控制台进程结束即失效。 / The per-process capability is consumed from the initial URL fragment and immediately removed. Tab-scoped `sessionStorage` supports reloads; persistent localStorage, cookies, query strings and logs are not used. This capability contains no phone root or session data, clears with the tab and expires when the console process ends.
- 启动文件权限 `0600`，只存本次进程有效的控制台入口，不含工作站/手机加密密钥；正常退出删除，异常退出残留也不能在原进程结束后继续访问。 / The temporary launcher is mode `0600` and contains only the process-lifetime console capability, never workstation/phone cryptographic keys. It is removed on clean exit; a crash leftover is unusable after that process ends.
- 校验 Host、Origin、Fetch Metadata，并设置 CSP、禁止嵌入、禁缓存。页面只允许关联状态、发起/取消关联、待配对列表及批准；不代理设备 root、会话内容、shell 或通用 owner API。 / Host, Origin and Fetch Metadata checks, CSP, frame denial and no-store headers restrict access. Only association status/enrollment/cancellation and pairing list/approval are exposed; device roots, sessions, shell execution and generic owner operations are excluded.

## 网关日志 / Gateway diagnostics

新网关二进制默认向标准错误输出 JSON 诊断记录，无需 debug 开关。代理原有访问日志需自行禁用敏感查询串；AgentDeck 的脱敏不会修改 Envoy/Ingress 的配置。 / The updated gateway binary writes JSON diagnostics to stderr by default. Configure proxies separately to omit sensitive query strings; application redaction does not alter Envoy/Ingress access logging.

字段包括 `service`、`event`、`request_id`、固定模板 `route`、HTTP 状态、耗时及 `reason`。每次请求生成 `X-Request-ID` 响应头，可用它关联应用日志。工作站/租户/用户/流程/路由授权只记录不可逆短摘要 `*_ref`，用于关联事件。 / Fields include service, event, request ID, a fixed route template, HTTP status, duration and reason. The response `X-Request-ID` correlates logs. Stable short hashes correlate workstation, tenant, user, flow and grant identities without recording their original values.

记录登录流创建、飞书授权成功、token/user_info 失败阶段及安全数值错误码、工作站上下线、中继开始/结束、认证拒绝、离线/容量/代理失败。成功的健康检查、时间校准与授权轮询不逐次写访问日志，失败仍记录。 / Events cover flow creation, successful authorization, token/user_info failure stages and safe numeric provider codes, workstation online/offline, relay start/end and authentication/offline/capacity/proxy failures. Successful health, clock and login polling requests are suppressed; failures remain visible.

常见 `reason`：`feishu_not_configured`、`oauth_browser_cookie_mismatch`、`oauth_state_missing_expired_or_used`、`feishu_identity_resolution_failed`、`local_authentication_rejected`、`local_already_connected`、`local_offline`、`relay_proof_rejected`、`local_tunnel_proxy_failed`。 / These reason codes distinguish configuration, browser cookie/state, provider identity, workstation authentication/conflict/offline, relay authentication and tunnel failures.

日志不输出请求/响应正文、原始查询串/未知路径、认证 Header、Cookie、OAuth code/state、token、校验码、密钥或用户会话。网关诊断只有部署新二进制后才生效；本地源码通过测试不代表远端已发布。 / Request/response bodies, raw query strings/unknown paths, authorization headers, cookies, OAuth code/state, tokens, comparison codes, keys and user sessions are excluded. New diagnostics take effect only after deploying the updated gateway binary; local tests do not prove remote rollout.
