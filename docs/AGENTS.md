# Agent 接入 / Agent integrations

首页右上角「设置 / Settings」控制 Codex、Claude Code、OpenCode、Qoder、TRAE、WorkBuddy 的显示。设置加密保存到手机；关闭显示不撤销授权，不删除历史。各 Agent 分别保留项目显示、顺序、折叠状态及「项目 / 最新会话」视图。

Use **Settings** at the upper right of Home to show or hide Codex, Claude Code, OpenCode, Qoder, TRAE and WorkBuddy. Preferences are stored in phone secure storage. Hiding does not revoke credentials or delete history. Each agent retains its own project visibility, ordering, folding and Projects/Recent views.

| Agent | 接入 / Interface | 能力与边界 / Capabilities and boundaries |
| --- | --- | --- |
| Codex | JSONL + CLI + 可选桌面原生实时 IPC / optional native desktop live IPC | 原桌面 ID 发送须显式配置 IPC，并且电脑已打开会话。手机创建会话由 CLI 执行。 / Sending to the original desktop ID requires explicit IPC configuration and an open desktop chat. Mobile-created chats run through the CLI. |
| Claude Code | JSONL + stream-json CLI | 原日志旁观；手机续聊分支与原电脑会话区分显示。 / Observe original logs; mobile continuation branches are labeled separately. |
| OpenCode | REST + `/global/event` SSE | 列举、原 ID 发送、创建、停止、单次审批、消息/思考/工具/图片。首次或重连修复最近 200 条 provider 消息；首次列举每项目最近 100 个会话。 / List, send to original IDs, create, stop, one-time permission replies, messages/thoughts/tools/images. Initial/reconnect recovery fetches 200 recent provider messages and 100 recent sessions per project. |
| Qoder | 官方 CLI ACP，stdio / official CLI ACP over stdio | 在 Agent 广告 `session/list`、`session/load` 时列举及恢复原 ACP ID；不虚构 IDE 私有历史。 / List and restore native ACP IDs when the agent advertises `session/list` and `session/load`; private IDE histories are not fabricated. |
| TRAE | 官方 CLI ACP，stdio / official CLI ACP over stdio | 同上；创建、发送、停止、单次审批和已公开思考/工具/图片。 / Same ACP limits, with create, send, cancel, one-time approvals and exposed thoughts/tools/images. |
| WorkBuddy | 官方 OAuth + 本地助理 OpenAPI / official OAuth and Local Assistant OpenAPI | 每个授权账号一个「本地助理」，在线状态、发送及增量轮询。当前官方本地接口不提供项目切换、新会话、停止或通用工具审批；这些按钮不显示。 / One Local Assistant per authorized account, online status, sending and incremental polling. Its local API does not provide project switching, new chats, cancellation or general tool permissions; unsupported controls are hidden. |

没有登录或未配置时显示真实错误；不生成示例对话。ACP 恢复失败不自动创建分支；Agent 不支持的能力不以“成功”返回。

Missing login/configuration reports a real error without sample conversations. ACP restore failures do not silently fork. Unsupported operations do not return success.

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

安装官方 CLI 并在后端运行用户下完成登录。在配置中填写可执行文件绝对路径可避免服务 PATH 差异。AgentDeck 使用 `qoder --acp` 和 `traecli acp serve`；默认权限保持开启。某些 CLI 登录需要用户在浏览器确认，该步骤不能用安装成功代替。

Install the official CLIs and sign in as the backend's operating-system user. Absolute executable paths avoid service PATH differences. AgentDeck uses `qoder --acp` and `traecli acp serve`, retaining default permission handling. Browser-confirmed CLI login is a separate step from installation.

```sh
qoder login
traecli login
```

ACP 单次工具审批只选择 `allow_once`，不接受 `allow_always` 替代。Agent 发起的文件读写限定在当前会话项目，解析符号链接并通过 Go 的目录作用域文件操作防止越界。Agent 请求由客户端执行终端时必须额外经过手机单次确认，输出最多 256 KiB，进程随会话连接关闭清理。

ACP permission replies select `allow_once`, never `allow_always`. Agent-initiated file access is confined to the current chat project, with symlink checks and Go directory-scoped operations. Client-executed terminal requests require an additional explicit phone approval, retain at most 256 KiB of output, and are cleaned up when their ACP connection closes.

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
