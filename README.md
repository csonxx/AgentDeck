# AgentDeck · 会话甲板

在手机上查看和继续电脑里的 coding agent 会话。项目和 Agent 留在电脑上，手机负责收发消息、查看执行过程和处理审批。

View and continue your workstation's coding-agent chats from your phone. Projects and agents stay on the workstation; the phone handles messages, execution details and approvals.

## 有什么用 / What it does

- 按 Agent、项目或最近会话查看对话，接收文字、图片和附件。
- 发送消息、创建会话、停止执行，回答 Agent 的提问和权限请求。
- 查看会话实际使用的模型、effort 和权限，修改 Agent 支持的选项。
- 在 Codex 用量页查看重置卡详情和使用记录，手动确认后使用。
- 离开会话后继续发送；断线后恢复。工具过程默认折叠，点具体项再加载详情。

Browse chats by agent, project or recency. Read text and attachments, send messages, create or stop chats, answer questions and approve tools. Model and permission controls follow each agent's capabilities. Sending survives leaving a chat; folded execution details load when opened. Codex Usage also shows reset card details and history, with explicit confirmation before use.

| Agent | 接入情况 / Integration |
| --- | --- |
| Codex | 日志导入、CLI 执行；电脑原会话发送需要 Desktop IPC / Logs and CLI execution; original Desktop chats require IPC |
| Claude Code | CLI 会话；Desktop 原会话发送为默认关闭的实验功能 / CLI chats; original Desktop input is experimental and off by default |
| OpenCode | 通过本机 REST/SSE 服务操作原会话 / Original chats through its local REST/SSE server |
| Qoder / TRAE | ACP CLI，能力以 Agent 提供的接口为准 / ACP CLI, limited to advertised capabilities |
| WorkBuddy | 需开放平台应用和 OAuth 授权 / Requires a registered application and OAuth authorization |

主要测试环境是 macOS 工作站和 Android arm64 手机。各 Agent 的配置、限制和测试范围见 [接入说明](docs/AGENTS.md)。

The main tested platform is macOS with an Android arm64 phone. See [integrations](docs/AGENTS.md) for setup, limits and test coverage.

S-LOCAL 也支持通过网关自动更新，覆盖 macOS、Windows、Linux 的 arm64/amd64；首次安装、已有服务迁移和版本发布见 [工作站安装与自动更新](docs/LOCAL_UPDATES.md)。

S-LOCAL also supports gateway updates on macOS, Windows and Linux (arm64/amd64). See [installation and updates](docs/LOCAL_UPDATES.md) for setup, migration and publication.

支持后台协商点对点直连，失败时继续使用网关中继，界面显示实际连接方式。未配置 STUN 时可尝试局域网 / IPv6；公网 NAT 打洞需要另配 STUN。配置、加密及验证方式见 [直连与回退](docs/PEER_CONNECTIONS.md)。

Background peer-to-peer negotiation falls back to the gateway relay and shows the actual transport. Without STUN it can try LAN/IPv6; public NAT traversal needs a configured STUN server. See [direct connections and fallback](docs/PEER_CONNECTIONS.md).

## 怎么使用 / Getting started

### 1. 安装手机 App / Install the app

| 0.4.48 · Android arm64 | 下载 / Download | 大小 / Size |
| --- | --- | --- |
| 轻量版 / Lite | [APK](https://xcdn.ukilive.com/mcp_file/custom_file/agentdeck/0.4.48/99c1c4837b15c440b3387be8f68becd25e378b9780f1409c421a2850a252c8fd/agentdeck-0.4.48-arm64-v8a-lite.apk) | 33.3 MB |
| 带语音版 / With voice | [APK](https://xcdn.ukilive.com/mcp_file/custom_file/agentdeck/0.4.48/9c64099f512fc52ad62ee9e1b49a92aadf1583bb159269a4f2a79ce67fdcef45/agentdeck-0.4.48-arm64-v8a-voice.apk) | 273.3 MB |

两版的会话功能相同，语音版增加手机离线识别。可覆盖切换并保留配对，不能同时安装。后续在 **设置 → 应用更新** 选择版本、下载和安装。

Both editions have the same chat features; Voice adds offline speech recognition. They replace one another while retaining pairing. Use **Settings → App updates** for later downloads and edition changes.

### 2. 启动电脑后端 / Start the workstation backend

直接使用 [macOS / Windows / Linux 安装包](docs/LOCAL_UPDATES.md#安装包--downloads)，无需安装 Go；它会配置登录后启动和网关自动更新。以下源码方式适合开发或手动管理。

Use a [workstation installer](docs/LOCAL_UPDATES.md#安装包--downloads) for background startup and gateway updates; no Go installation is needed. The source setup below is for development or manual operation.

需要 Go 1.25+，并先让所需 Agent 在电脑上独立登录、正常运行。后端使用同一个系统用户。已有安装就在原目录更新，保留 `~/.agentdeck`。

Install Go 1.25+ and sign in to your agents first. Run the backend as the same OS user. Upgrade existing installations in place, keeping `~/.agentdeck`.

```sh
git clone https://gitlab.ttyuyin.com/azeroth/agentdesk.git AgentDeck
cd AgentDeck
mkdir -p artifacts
(cd backend && GOWORK=off go build -o ../artifacts/agentdeck.next ./cmd/agentdeck)
mv artifacts/agentdeck.next artifacts/agentdeck
```

下面使用已有网关。把 `PROJECTS_ROOT` 改为实际项目父目录，删除未安装 Agent 的参数。后端保持运行。

The following uses an existing gateway. Set the real project root and remove flags for agents you do not use. Keep the backend running.

```sh
PROJECTS_ROOT="$HOME/workspace"
test -d "$PROJECTS_ROOT" || exit 1
./artifacts/agentdeck serve --relay \
  --data "$HOME/.agentdeck" \
  --codex-sessions "$HOME/.codex/sessions" \
  --claude-sessions "$HOME/.claude/projects" \
  --project-roots "$PROJECTS_ROOT" \
  --media-roots "$PROJECTS_ROOT"
```

操作 Codex Desktop 原会话时，确认 socket 存在，再给启动命令添加 `--codex-desktop-socket "$HOME/.codex/ipc/ipc.sock"`。OpenCode 等 Agent 的配置见 [使用手册](docs/USAGE.md#各-agent-怎么接--connect-each-agent)。

For original Codex Desktop chats, add that socket flag only after checking the path exists. See the [manual](docs/USAGE.md#各-agent-怎么接--connect-each-agent) for OpenCode and other agents.

### 3. 关联并配对 / Link and pair

1. 电脑浏览器打开 **http://127.0.0.1:8787/**，填入网关 HTTPS 地址和工作站名称，完成飞书授权，等待在线。
2. 手机选择 **通过网关 · 飞书登录**，填写相同地址，用同一飞书账号登录，选择这台工作站。
3. 在电脑管理页输入手机显示的 **12 位十六进制校验码**，批准配对；手机确认两边一致。
4. 首页显示「实时连接」后，选择 Agent 和会话即可使用。

Open the local console, enter your gateway URL and workstation name, and authorize with Feishu. Sign in on the phone with the same account and URL, select the workstation, then enter the phone's 12-character comparison code in the console and approve. Confirm the matching code on the phone and wait for Live connection.

网关地址有 `/agentdeck` 等前缀时，两端都要填完整。8787 是电脑管理页，不是手机连接地址。没有网关时，可先用 [Android USB 直连](docs/USAGE.md#方式-bandroid-usb-直连--android-usb-direct)；需要公网访问则按 [自建网关](docs/USAGE.md#自建公网网关--host-a-public-gateway) 部署。

Keep any gateway URL prefix on both devices. Port 8787 is local administration. Without a gateway, use [Android USB direct](docs/USAGE.md#方式-bandroid-usb-直连--android-usb-direct), or [host a gateway](docs/USAGE.md#自建公网网关--host-a-public-gateway) for remote access.

### 日常操作 / Daily use

首页可切换项目和最近会话，设置项目顺序、显示范围并下拉刷新。会话右上角目录可筛选发送或收到的消息并跳转；历史随滚动加载。审批可展开完整命令后再确认。发送状态和失败重试见发件箱；附件可后台下载或用其他应用打开。

Switch between Projects and Recent, reorder or hide projects, and pull to refresh. The chat outline filters sent/received messages and jumps through history. Expand commands before approving. Use Outbox for delivery status and retries; attachments support background downloads and external apps.

### 查看用量 / View usage

在项目页点击「用量」查看 Codex 或 Claude 返回的额度。数据依赖原生登录和接口，不可用时会显示原因。设置中还可调整界面大小、Agent 显示和四位启动密码。

Tap Usage on the project page for Codex or Claude limits. Data depends on native authentication and APIs; unavailable results include a reason. Settings also contains interface size, agent visibility and a four-digit app passcode.

## 原理如何 / How it works

```text
电脑 S-LOCAL ──出站 WSS── 网关 S-GATEWAY ──HTTPS/WSS── 手机 C-APP
Workstation              Gateway                     Phone
```

Go 后端读取 Agent 的日志或原生接口，将消息、思考、工具和审批转为统一事件。Flutter App 展示这些事件；发送和审批交给对应 Agent 执行。原会话能否继续，取决于 Agent 的接口及当前持有者，不通过复制历史新建分支来替代。

The Go backend reads native logs or APIs and normalizes messages, reasoning, tools and approvals. Flutter renders these events and returns input to the corresponding agent. Continuing an original chat depends on its native interface and owner; copying history into a new branch is not a substitute.

网关处理飞书身份、工作站发现和密文转发。首次配对时，手机与工作站核对校验码并建立独立密钥；网关不持有会话解密密钥。流量密钥默认每 15 分钟轮换，后端数据和手机缓存加密保存。也支持手机通过 HTTPS/WSS 直连工作站。

The gateway handles Feishu identity, discovery and encrypted relay. Pairing establishes a separate phone/workstation key after code comparison; the gateway has no chat decryption key. Traffic keys rotate every 15 minutes by default, and stored data and phone caches are encrypted. Direct HTTPS/WSS connections are also supported.

TLS 可由 Envoy 等代理终止；本机管理页和 Agent 调试端口只监听回环。当前网关使用单实例 bbolt，不能直接扩为多副本。存储迁移要同时保留 `vault.db` 和 `storage.key`。协议和信任边界见 [安全说明](docs/SECURITY.md)。

Envoy can terminate TLS. Administration and agent debugging stay on loopback. The gateway currently uses single-instance bbolt; multiple replicas require further work. Migrate `vault.db` with its matching `storage.key`. See [security](docs/SECURITY.md) for protocol and trust boundaries.

## 更多文档 / Documentation

| 文档 / Document | 内容 / Contents |
| --- | --- |
| [使用手册 / Manual](docs/USAGE.md) | USB、网关部署、后台运行、更新发布、自行构建和排障 / USB, hosting, services, releases, builds and troubleshooting |
| [Agent 接入 / Integrations](docs/AGENTS.md) | OpenCode、ACP、WorkBuddy 配置及能力限制 / Provider setup and limits |
| [本机管理 / Workstation](docs/WORKSTATION.md) | 工作站关联、手机管理、日志、实验性 Desktop 接入 / Linking, devices, logs and experimental Desktop input |
| [网关 / Gateway](docs/GATEWAY.md) | 飞书应用与配对协议 / Feishu application and pairing |
| [输入与附件 / Input](docs/INPUTS.md) | 模型、effort、权限、提问、附件和语音 / Models, permissions, questions, attachments and speech |
| [验收记录 / Verification](docs/ACCEPTANCE.md) | 各版本测试和实机记录 / Version-specific tests and device results |

源码 / Source: [GitLab](https://gitlab.ttyuyin.com/azeroth/agentdesk) · [GitHub](https://github.com/csonxx/AgentDeck) · 语音许可 / Speech licenses: [说明 / Notices](docs/CLIENT_VOICE_LICENSE.md)
