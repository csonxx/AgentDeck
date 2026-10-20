# Agent 接入 / Agent integrations

首页右上角「设置 / Settings」控制 Codex、Claude Code、OpenCode、Qoder、TRAE、WorkBuddy 的显示。设置加密保存到手机；关闭显示不撤销授权，不删除历史。各 Agent 分别保留项目显示、顺序、折叠状态及「项目 / 最新会话」视图。

Use **Settings** at the upper right of Home to show or hide Codex, Claude Code, OpenCode, Qoder, TRAE and WorkBuddy. Preferences are stored in phone secure storage. Hiding does not revoke credentials or delete history. Each agent retains its own project visibility, ordering, folding and Projects/Recent views.

| Agent | 接入 / Interface | 能力与边界 / Capabilities and boundaries |
| --- | --- | --- |
| Codex | JSONL + CLI + 可选桌面原生实时 IPC / optional native desktop live IPC | 原桌面 ID 发送须显式配置 IPC，并且 Desktop 正常运行、协议兼容；macOS 可在明确无持有者时打开原 ID。手机创建会话由 CLI 执行。 / Original desktop IDs require explicit IPC and a running compatible Desktop. On macOS an explicitly unowned chat can open at its original ID. Mobile-created chats run through the CLI. |
| Claude Code | 桌面目录 + JSONL + 可选本机原生调试器 / opt-in local native debugger；自有 stream-json CLI | 真实 OnePlus 7 ↔ Desktop 原会话普通用户收发已通过，保持原 UUID/PID，无 peer 提示和分支。依赖所有者通过官方菜单开启的本机调试器；开关默认关闭，9229 禁止公网暴露。 / Real OnePlus 7 round trips to the original Desktop chat passed as ordinary human input, preserving UUID/PID without peer messages or forks. Requires the owner-enabled official loopback debugger; opt-in defaults off and 9229 must never be public. |
| OpenCode | REST + `/global/event` SSE | 列举、原 ID 发送、创建、停止、单次审批、消息/思考/工具/图片。首次或重连修复最近 200 条 provider 消息；首次列举每项目最近 100 个会话。 / List, send to original IDs, create, stop, one-time permission replies, messages/thoughts/tools/images. Initial/reconnect recovery fetches 200 recent provider messages and 100 recent sessions per project. |
| Qoder | 官方 CLI ACP，stdio / official CLI ACP over stdio | 在 Agent 广告 `session/list`、`session/load` 时列举及恢复原 ACP ID；不虚构 IDE 私有历史。 / List and restore native ACP IDs when the agent advertises `session/list` and `session/load`; private IDE histories are not fabricated. |
| TRAE | 官方 CLI ACP，stdio / official CLI ACP over stdio | 同上；创建、发送、停止、单次审批和已公开思考/工具/图片。 / Same ACP limits, with create, send, cancel, one-time approvals and exposed thoughts/tools/images. |
| WorkBuddy | 官方 OAuth + 本地助理 OpenAPI / official OAuth and Local Assistant OpenAPI | 每个授权账号一个「本地助理」，在线状态、发送及增量轮询。当前官方本地接口不提供项目切换、新会话、停止或通用工具审批；这些按钮不显示。 / One Local Assistant per authorized account, online status, sending and incremental polling. Its local API does not provide project switching, new chats, cancellation or general tool permissions; unsupported controls are hidden. |

Codex 原生会话使用单一写入者。手机创建的 CLI 会话完成一轮后，S-LOCAL 关闭自己的空闲执行进程；电脑可接管同一 ID，后续手机发送会跟随 Desktop。读取权限不恢复 CLI；空闲时修改权限会短暂恢复原 ID、确认原生设置后释放。正在执行、等待审批及 active Goal 保持原执行方；Goal 完成后自动复查释放。这里没有移除原生写锁，也不保证两个独立执行进程可以同时写入一个会话。

Codex enforces a single native writer. S-LOCAL retires its own idle mobile-created executor after a turn, allowing Desktop to take the same ID; later phone sends follow Desktop. Permission reads never resume a CLI; an idle permission edit briefly resumes the original ID, confirms native settings and releases it. Active turns, approvals and active Goals retain their executor, with release rechecked after Goal completion. Native locks remain enforced; two independent executors cannot write the same chat concurrently.

没有登录或未配置时显示真实错误；不生成示例对话。ACP 恢复失败不自动创建分支；Agent 不支持的能力不以“成功”返回。

Missing login/configuration reports a real error without sample conversations. ACP restore failures do not silently fork. Unsupported operations do not return success.

Claude 桌面目录和 Codex/Claude「用量」入口的启动参数、缓存与限制见 [README](../README.md#查看用量--view-usage)。

See [README](../README.md#查看用量--view-usage) for Claude desktop discovery, the Codex/Claude **Usage** entry, cache freshness and limitations.

## 工作站配置 / Workstation configuration

复制 [agents.example.json](config/agents.example.json)，只填写实际启用的连接；删除不用的块或将 URL 留空。文件必须为普通文件，最大 64 KiB，权限 `0600`；拒绝符号链接和不认识的字段。OpenCode 与 WorkBuddy 地址必须为 HTTPS，允许工作站回环 HTTP。配置文件中的 CLI 命令、参数由工作站所有者设置，手机不能修改或传入 shell 命令。

Copy [agents.example.json](config/agents.example.json), filling only enabled integrations; remove unused sections or leave their URLs empty. The file must be a regular file of at most 64 KiB with mode `0600`; symlinks and unknown fields are rejected. Provider URLs require HTTPS, with HTTP allowed on workstation loopback. Only the workstation owner configures CLI commands/arguments; the phone cannot change them or supply shell commands.

```sh
mkdir -p "$HOME/.agentdeck"
cp docs/config/agents.example.json "$HOME/.agentdeck/agents.json"
chmod 600 "$HOME/.agentdeck/agents.json"
# Edit actual paths, endpoints and application metadata before starting.
artifacts/agentdeck serve \
  --data "$HOME/.agentdeck" \
  --project-roots /absolute/workspace \
  --agents-config "$HOME/.agentdeck/agents.json" \
  --public-url https://agentdeck.example.com \
  --listen 127.0.0.1:8443
```

已有启动参数（日志目录、桌面 IPC、media-roots、代理/relay 等）继续保留，只增加 `--agents-config`。该参数只用于 S-LOCAL；S-GATEWAY 不连接 Agent、不持有 Agent 授权。

Keep existing source, desktop IPC, media, proxy/relay arguments and add `--agents-config`. This flag belongs to S-LOCAL; S-GATEWAY does not connect to agents or hold their credentials.

### OpenCode

使用已登录的 OpenCode，启动仅回环可达的服务。服务器与 AgentDeck 必须使用同一密码环境变量；不要把密码写入 JSON、命令行或手机。示例文件启用了 Basic authentication；未启用密码的现有回环服务可删除 `password_env`。

Start a loopback-only server from your signed-in OpenCode installation. OpenCode and AgentDeck must inherit the same password environment variable. Keep passwords out of JSON, command arguments and the phone. The example uses Basic authentication; omit `password_env` for an existing loopback service without authentication.

```sh
opencode serve --hostname 127.0.0.1 --port 4096
```

原会话发送使用 provider 的原 session ID + directory；相同请求携带稳定 message ID。断线期间 provider 已生成的旧历史受其接口分页范围限制，AgentDeck 已保存的历史仍保留。 / Sending preserves the provider session ID and directory, with stable message IDs for a request. Provider history recovery is bounded by the API fetch ranges; already imported AgentDeck history is retained.

官方文档 / Official documentation: [OpenCode Server](https://dev.opencode.ai/docs/server/).

### Qoder / TRAE

安装官方 CLI 并在后端运行用户下完成登录。在配置中填写可执行文件绝对路径可避免服务 PATH 差异。AgentDeck 使用 `qoder --acp` 和 `traecli acp serve`，沿用 Agent 的原生权限配置，不额外设置询问或自动允许模式。某些 CLI 登录需要用户在浏览器确认，该步骤不能用安装成功代替。

Install the official CLIs and sign in as the backend's operating-system user. Absolute executable paths avoid service PATH differences. AgentDeck uses `qoder --acp` and `traecli acp serve`, retaining default permission handling. Browser-confirmed CLI login is a separate step from installation.

```sh
qoder login
traecli login
```

ACP 单次工具审批只选择 `allow_once`，不接受 `allow_always` 替代。Agent 按自己的权限模式决定是否发出 `session/request_permission`；AgentDeck 转发真实请求，不为 `terminal/create` 再增加一次审批。Agent 发起的文件读写和终端工作目录限定在当前会话项目，解析符号链接并通过 Go 的目录作用域文件操作防止越界。终端输出最多 256 KiB，进程随会话连接关闭清理。

ACP permission replies select `allow_once`, never `allow_always`. The native agent decides whether its policy requires `session/request_permission`; AgentDeck forwards that request without adding another approval for `terminal/create`. File access and terminal working directories remain confined to the chat project, with symlink checks and Go directory-scoped operations. Terminals retain at most 256 KiB of output and are cleaned up when their ACP connection closes.

官方文档 / Official documentation: [Qoder ACP](https://docs.qoder.com/cli/acp), [TRAE CLI ACP](https://docs.trae.cn/cli_agent-client-protocol).

### WorkBuddy 配置与授权 / WorkBuddy configuration and authorization

当前版本已提供配置和授权入口；没有开放平台应用时无需填写虚假的 client ID。设置页的授权按钮会明确提示先配置应用。真实接入步骤如下。

Configuration and authorization entry points are implemented. Without an Open Platform application, leave the client ID blank. The authorization button explains the missing configuration. To enable an actual account:

1. 在 [WorkBuddy 开放平台](https://open.workbuddy.cn/docs/third-party-app) 创建第三方应用，取得 client ID；为其添加 `user.localassistant.readable user.localassistant.invokable` 权限。 / Create a third-party application and enable these two scopes.
2. 注册精确回调 `https://your-local-domain/v1/agents/workbuddy/callback`，填写配置中的 `client_id`、`redirect_uri`。 / Register the exact callback and fill the two fields.
3. 仅在 S-LOCAL 的运行环境注入 `AGENTDECK_WORKBUDDY_CLIENT_SECRET`；手机和网关不接收 client secret。重启 S-LOCAL 以加载配置。 / Inject the secret into S-LOCAL's process environment only and restart to load settings.
4. 手机连接后进入「Agent 显示设置 → 授权 WorkBuddy 本地助理」，浏览器登录并确认，返回页面提示结果，手机会同步「本地助理」。 / Open the authorization entry on the connected phone, confirm in the browser, and return to the synchronized Local Assistant.

回调必须路由到 **S-LOCAL 的 HTTP Handler**，可以通过独立 Envoy 公网路由到私网上游；仅把请求发给 S-GATEWAY 不会完成 WorkBuddy 授权。三角色配对的飞书身份与 WorkBuddy 的 OAuth 是独立授权。

The callback must route to the **S-LOCAL HTTP handler**, for example through a separate Envoy route to its private upstream. Sending it only to S-GATEWAY will not complete WorkBuddy authorization. Feishu pairing identity and WorkBuddy OAuth are independent grants.

授权 state 随机、有效 10 分钟、单次使用；code 不写日志、不返回手机。access/refresh token 加密保存在 Local vault，按实际 expires_in 自动刷新；刷新账号发生变化会拒绝继续发送。第一次按官方分页接口同步（上限 10,000 条，超过明确报错），随后用加密保存的消息游标增量轮询。账号切换拥有独立会话及游标。

OAuth state is random, expires after ten minutes and is consumed once. Codes are neither logged nor returned to the phone. Access/refresh tokens are encrypted in the Local vault and refreshed using `expires_in`; an unexpected account change rejects sending. Initial history uses official pagination with a 10,000-message cap and explicit overflow error. Subsequent polling uses an encrypted persisted cursor, isolated per account.

WorkBuddy 这一路经过官方服务，不能称为只在电脑和手机之间传输；Local → 手机的 AgentDeck 通道及缓存仍加密，网关仍只中继密文。 / This adapter communicates through WorkBuddy's official service. AgentDeck's Local-to-phone channel and caches remain encrypted, and its gateway relays ciphertext.

官方文档 / Official documentation: [WorkBuddy OpenAPI](https://open.workbuddy.cn/docs/openapi).


## 手机查看和修改权限 / Phone permission controls

会话输入框旁的盾牌按钮显示原生权限，点击查看标识、说明和可选模式。Codex 的 Auto review 显示 `auto_review`（兼容旧 `guardian_subagent`）；Claude 分别显示 `default`、`acceptEdits`、`auto`、`bypassPermissions`、`plan`、`dontAsk`，不套用 Codex 档位。新建 Codex / Claude 会话可保留工作站默认权限或显式选择。

The shield next to the composer shows native permissions and opens identifiers, descriptions and supported choices. Codex and Claude keep their own mode names. New chats can keep workstation defaults or explicitly select a mode.

OpenCode 显示当前会话规则，可设置 Ask / Allow / Deny；原生 API 会追加规则，最终通配规则按末项优先生效，旧规则保留。Qoder / TRAE 的 ACP 模式由 Agent 声明，其 Plan / Build 等会话模式并不等于 Bypass。WorkBuddy 没有提供权限修改接口时展示由 Agent 管理。修改需要在线原生确认；服务器策略拒绝会直接显示，用户可刷新核对，不必新建支线会话。

OpenCode exposes session rules with Ask / Allow / Deny; native updates append ordered rules and preserve earlier entries. ACP modes are agent-defined, and Plan / Build must not be mistaken for Bypass. WorkBuddy remains agent-managed when no permission API is available. Updates require native online acknowledgement, surface policy rejections, and do not create branches.

## Codex 未读状态 / Codex unread state

电脑端原生未读状态通过账号及本机连接身份匹配后，经加密连接同步到 App；电脑标记已读后，App 会清除对应蓝点。App 本地查看也会清除手机蓝点，回执保存在手机加密缓存中。**App 回写 Desktop 已读暂未打通**：当前 Desktop 拒绝外部客户端调用原生已读工具；没有直接改写 Desktop 状态文件、关闭校验或重启 Desktop。

Native Desktop unread state is matched to the current account and local execution host, then sent over the encrypted connection. Desktop marking a chat read clears its App dot. Reading on the phone stores a local receipt in its encrypted cache. **Phone-to-Desktop read receipts are not accepted yet**: the installed Desktop rejects external native read-tool clients. No native state file is modified, authorization check disabled, or Desktop restarted.


### 交互提问 / Interactive questions

Codex 标准 `requestUserInput`（包含 Desktop 原会话）、Claude `AskUserQuestion` 与 OpenCode `question` 的选项说明、原生单选/多选和自定义答案在独立提问卡片展示；ACP 许可卡片展示 Agent 实际提供的所有许可选项。回复仍经加密通道回到对应原会话，后台重新检查归属和有效期。独立 Option Picker、MCP 任意表单和未公开的 WorkBuddy 交互不属于上述接口，未宣称支持。[详细用法与边界](INPUTS.md#提问与原生选项--questions-and-native-choices)。

Codex standard `requestUserInput` (including original Desktop chats), Claude `AskUserQuestion` and OpenCode `question` render full descriptions, native single/multiple choices and custom answers in separate question cards. ACP permission cards show every option actually offered by the agent. Replies use the encrypted transport and original chat, with server-side ownership and expiry checks. Independent Option Picker, arbitrary MCP forms and unexposed WorkBuddy interactions are not claimed as supported. [Usage and boundaries](INPUTS.md#提问与原生选项--questions-and-native-choices).

### OpenCode 子会话 / Child sessions

OpenCode 原生 `parentID` 非空的会话归属于父会话，不再作为项目或最新会话中的独立聊天计数；不根据标题猜测。原生归档会话同样隐藏，取消归档后可恢复。历史仍按原 ID 加密保存，不删除子 Agent 输出；父会话的任务调用和结果继续展示。需要升级 S-LOCAL，App 重连后同步目录。

OpenCode sessions with a native `parentID` belong to their parent and do not inflate project/recent chat counts. Titles are not used for classification. Archived sessions are hidden and can reappear after unarchiving. Child history remains encrypted under its original ID; parent task calls/results remain visible. Upgrade S-LOCAL and reconnect the app to refresh the catalog.


## 异常状态与兼容范围 / Exceptional states and compatibility

从 0.4.45 起，执行状态与结构化异常原因分别同步；会话页不再把失败或未知状态统一显示成就绪。需同步升级 App 与 S-LOCAL，网关只转发密文，无需为此更新网关服务代码。

From 0.4.45, execution state and structured issues travel separately. Failed/unknown states no longer appear ready. Upgrade App and S-LOCAL together; Gateway only relays ciphertext and needs no service code change.

| 原生信号 / Native signal | 行为 / Behavior |
| --- | --- |
| Codex `misalignmentPolicyViolation` | 显示平台安全终止或需电脑复核；不继续发送、排队或自动创建分支。复核仅在原生 Desktop 完成，App 不复制开发者级续行指令。/ Show safety termination or required desktop review; no sends, queue insertion or automatic forks. Review stays in the official desktop. |
| Codex context / usage / auth / network structured errors | 分别提示上下文上限、额度/速率、授权、上游连接问题。未知错误使用明确的通用失败提示，不靠正文关键词判断。/ Distinguish context, limits, auth and connection errors; unknown codes use a generic failure, never message-text heuristics. |
| Codex Desktop pending requests | 原生已支持的提问继续使用手机提问卡；尚未映射的请求提示电脑处理，不默默忽略。/ Supported questions retain native answer cards; unmapped requests explicitly require desktop handling. |
| OpenCode `session.error` / `session.status` | 根据已公开的错误类型区分授权、上下文、输出和限额；retry 显示原生重试，error 后的 idle 清理不能覆盖失败，新 busy 回合清除旧异常。/ Structured error types retain their causes; native retries are visible, cleanup idle does not erase failures and a new busy turn clears them. |
| Claude CLI stream-json | 公开的 API 错误及 result 失败显示原因；未识别错误保留通用失败，不采集私有错误正文。真实 Desktop 仍暂停验收。/ Exposed API/result failures retain safe causes; unknown errors stay generic. Real Desktop acceptance remains paused. |
| ACP (Qoder/TRAE) | 区分正常结束、取消、输出/回合上限、单次拒绝与未知 stop reason；单次拒绝不封锁整条会话。/ Distinguish completion, cancellation, output/turn limits, per-request refusal and unknown stop reasons; a refusal does not permanently lock the chat. |
| 新增或未公开状态 / New or private state | 明确显示尚未兼容/需电脑处理；不宣称任意 MCP 表单、独立 Option Picker、私有 WorkBuddy 交互均已兼容。/ Display unsupported state/action explicitly; arbitrary MCP forms, independent Option Picker and private WorkBuddy interactions are not claimed supported. |

旧日志、目录元数据和去重不能抹去安全限制；只有新的权威原生状态才能解除。发送前的原生检查避免用过期缓存执行。发件箱遇到尚未提交的受限会话会保留失败记录；已发出但丢确认的消息仍沿原编号核对，不能换编号盲重发。

Old logs, catalog metadata and deduplication cannot erase a safety restriction; a fresh authoritative state can clear it. Native send preflight avoids relying on stale caches. The outbox retains unsent blocked items; lost acknowledgements still check the original command ID without blindly resubmitting a new turn.
