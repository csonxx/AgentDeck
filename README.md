# AgentDeck · 会话甲板

在手机上查看、继续电脑上的 coding agent 会话。Go 后端运行在工作站，Flutter App 负责加密连接、消息输入和展示。无需把项目迁移到 AgentDeck，也不要求项目使用 Git。

View and continue workstation coding-agent chats on your phone. A Go backend runs on the workstation; the Flutter app handles encrypted connections, input and presentation. Existing projects stay where they are and do not need Git.

**当前版本 / Current version: 0.4.41** · [GitHub](https://github.com/csonxx/AgentDeck) · [GitLab](https://gitlab.ttyuyin.com/azeroth/agentdesk)

| Android arm64-v8a | 下载 / Download | 大小 / Size | 区别 / Difference |
| --- | --- | --- | --- |
| 轻量版 / Lite | [0.4.41 APK](https://xcdn.ukilive.com/mcp_file/custom_file/agentdeck/0.4.41/a6b8b882fe3eb55dc1ad0c0ddb37903ec3eaa4e932e93d21d39101738a58b88a/agentdeck-0.4.41-arm64-v8a-lite.apk) | 33.0 MB | 不含语音模型、引擎和麦克风权限 / No speech models, engines or microphone permission |
| 带语音版 / With voice | [0.4.41 APK](https://xcdn.ukilive.com/mcp_file/custom_file/agentdeck/0.4.41/b2d1cb4ed33504c5b80846920ca49a086bdc50e47b06ce1bd08ea090c9dba561/agentdeck-0.4.41-arm64-v8a-voice.apk) | 273.1 MB | 增加离线语音输入 / Adds offline speech input |

两个包的文字、图片、附件及会话功能一致，使用相同应用 ID 和签名；不能同时安装，可覆盖切换并保留配对与设置。公网文件已完成全量下载及 SHA-256 校验。现有 APK 使用开发测试签名；自建安装包必须维护自己的固定签名，不能假定能覆盖这里下载的包。

Both editions share text, image, attachment and chat features, application ID and signing certificate. They replace one another while retaining pairing/settings; they cannot coexist. Public downloads were fully SHA-256 verified. Distributed APKs currently use a development signing key. Maintain a stable signing identity for your own builds; a locally generated debug key will not necessarily replace these APKs.

**发布状态（2026-10-16） / Release status:** 测试网关已部署双通道代码并发布两份 0.4.41 清单，两个公网更新查询及 APK 跳转已验证。App 可在设置 → 应用更新选择安装类型；各阶段实机证据见验收记录。/ The testing gateway now serves both 0.4.41 editions. Public manifests and APK redirects are verified; choose an edition in Settings → App updates. Device acceptance is recorded separately. [验证范围 / Acceptance scope](docs/ACCEPTANCE.md)

## 从哪里开始 / Start here

- **只用手机 / Phone user:** 安装上方 APK，向工作站所有者获取配对码或网关地址，再按[连接流程](#连接手机--connect-the-phone)操作。/ Install an APK, obtain pairing material or a gateway address from the workstation owner, then follow the connection steps.
- **已有网关 / Existing gateway:** 构建后端，使用[网关模式](#方式-a已有网关--existing-gateway)启动 S-LOCAL，浏览器配对；手机不需要 USB。/ Build the backend, start S-LOCAL in gateway mode and pair in the browser. USB is unnecessary.
- **先本机体验 / Local trial:** 使用 [USB 直连](#方式-bandroid-usb-直连--android-usb-direct)，不需要飞书应用或公网域名。/ Use USB direct mode; no Feishu application or public domain is needed.
- **从零搭建网关 / Host your own gateway:** 先完成[网关部署](#自建公网网关--host-a-public-gateway)，再启动 S-LOCAL。/ Deploy a gateway before linking the workstation.
- **交给 AI / AI-assisted setup:** 复制下一节的任务，并填写实际目录、设备和连接方式。/ Copy the following brief with your actual paths, device and connection mode.

## 直接交给 AI 的任务 / Copy this task to your AI

以下提示词不含凭据；把方括号替换成自己的信息。已有后端或配对时明确写“保留现有数据”。不要把 storage.key、飞书 Secret、Agent token 或配对 JSON 发给 AI 聊天。

Replace bracketed values below. Explicitly request preservation of existing data when applicable. Do not paste storage keys, Feishu secrets, agent tokens or pairing JSON into chat.

```text
请在这台电脑安装并配置 AgentDeck，按仓库 README.md 和 AGENTS.md 执行。
仓库：https://github.com/csonxx/AgentDeck
工作站系统：[macOS / Linux]；源码目录：[绝对路径]
允许手机操作的项目父目录：[绝对路径，可逗号分隔]
启用的 Agent：[Codex / Claude CLI / OpenCode / 其他]
连接模式：[已有网关 / 自建网关 / Android USB 直连]
网关 HTTPS 地址：[自有地址，含路径前缀；USB 模式不填]
手机：[adb devices 中的设备编号，或由我手动安装]
安装类型：[轻量版 / 带语音版]；数据目录：[默认 ~/.agentdeck]
已有安装：[无 / 有，必须保留数据、storage.key、配对和设置]

先检查环境、已有进程、版本、端口和目录，再构建；只启用我列出的 Agent。
读取本仓库 CLI --help，不猜参数。已有网关不重建；直连不申请飞书应用。
以登录 Agent 的同一个系统用户运行 S-LOCAL，保留 Agent 的原生默认权限。
网关 Secret 仅通过本机安全文件或部署平台注入，不写入源码或聊天。
启动后验证：本机管理页、网关状态（如适用）、手机加密配对、真实会话同步。
需要我登录、扫飞书码、核对配对码或批准系统安装时，明确提示我操作。
发送验收只使用专门的测试会话，不向现有工作会话发消息。
不要退出任何 Agent 的登录，不自动开启 Claude Desktop 调试器，不关闭 TLS 校验，
不要重置数据、覆盖存储密钥、放宽权限或把 8787/9229/control.sock 暴露到公网。
如需后台常驻，使用实际绝对路径配置本机服务管理器，说明启动/停止/日志位置。
最后报告：启动命令、管理页、版本、手机类型、数据目录、已验证项和未通过项。
没有我的明确要求，不提交推送代码、不发布公网服务。
```

English equivalent:

```text
Install AgentDeck using this repository's README.md and AGENTS.md.
Repository: https://github.com/csonxx/AgentDeck
OS: [macOS / Linux]; checkout: [absolute path]
Allowed project roots: [absolute paths, comma-separated]
Enabled agents: [Codex / Claude CLI / OpenCode / others]
Connection: [existing gateway / self-hosted gateway / Android USB direct]
Gateway URL: [my HTTPS URL including its prefix, or empty for USB]
Phone: [adb serial, or manual APK installation]; edition: [Lite / With voice]
State directory: [~/.agentdeck unless specified]
Existing installation: [none / preserve existing state, keys, pairing and settings]

Inspect prerequisites, existing processes, versions, ports and paths first.
Use CLI --help; enable only requested agents. Do not redeploy an existing gateway.
Run S-LOCAL as the same OS user who signed into the agents, with native default permissions.
Keep secrets in local protected files or deployment secret management, never source or chat.
Verify the local console, gateway status if applicable, encrypted phone pairing and real history.
Ask me to perform required sign-ins, Feishu authorization, code comparison or OS install approval.
Use a dedicated test chat for sending; never send into an existing work chat for acceptance.
Do not sign agents out, enable the Claude Desktop debugger automatically, disable TLS checks,
reset state, replace storage keys, widen permissions or expose 8787/9229/control.sock publicly.
If background service is needed, use the actual absolute paths and report start/stop/log commands.
Report versions, paths, connection mode, tested behavior and remaining failures separately.
Do not commit, push or publish services unless I explicitly request it.
```

## 架构与连接方式 / Architecture and connection modes

| 角色 / Role | 运行位置 / Where | 职责 / Responsibility |
| --- | --- | --- |
| S-LOCAL | 运行 Agent 的电脑 / Agent workstation | 导入原会话、执行授权操作、加密保存、与手机配对 / Import chats, execute authorized operations, encrypt storage and pair phones |
| S-GATEWAY | 公网 HTTPS/Envoy 后 / Behind public HTTPS/Envoy | 飞书身份、多租户归属、在线目录和密文中继；不持有会话解密 root / Feishu identity, tenant ownership, discovery and ciphertext relay; no chat decryption root |
| C-APP | Android 手机 / Android phone | 配对、解密、输入、会话与附件展示 / Pairing, decryption, input, chats and attachments |

直连：`C-APP → HTTPS/WSS → S-LOCAL`。网关：`S-LOCAL → 出站 WSS → S-GATEWAY ← HTTPS/WSS ← C-APP`。HTTP 用于认证、控制、分页和附件；WebSocket 用于实时事件；OpenCode 上游另用 REST/SSE。不是把终端画面录屏传给手机。

Direct: `C-APP → HTTPS/WSS → S-LOCAL`. Gateway: `S-LOCAL → outbound WSS → S-GATEWAY ← HTTPS/WSS ← C-APP`. HTTP handles authentication, control, paging and attachments; WebSocket carries live events. OpenCode additionally uses REST/SSE upstream. The phone receives structured events rather than a terminal screen recording.

Local/App 首次核对配对码，之后自动重连并轮换流量密钥（默认 15 分钟）。事件、设备、附件及手机缓存加密保存。网关多租户不等于分布式高可用：当前使用单实例 bbolt 和进程内路由，不可直接扩成多副本。/ Local/App compare a code once, then reconnect and rotate traffic keys automatically (15-minute default). Events, devices, attachments and phone caches are encrypted at rest. Multi-tenancy is authorization isolation; the current single-instance bbolt/in-memory router is not horizontally scalable HA. [安全说明 / Security](docs/SECURITY.md)

## 环境准备与后端构建 / Prerequisites and backend build

当前主要验收平台为 **macOS 工作站 + Android arm64-v8a 手机**。Go 服务可用于 Linux；桌面 IPC、macOS 自动打开原会话等能力受平台和原生版本限制。Flutter 使用 `dart:io`，不是 Web App；iOS/macOS 源码目录不代表已有同等实机验收。

The main accepted platform is **macOS workstation + Android arm64-v8a phone**. Go services also run on Linux; desktop IPC and macOS original-chat opening are platform/version dependent. This is a native `dart:io` app, not a web app. iOS/macOS source folders do not imply equivalent device acceptance.

| 用途 / Purpose | 需要 / Required |
| --- | --- |
| 后端 / Backend | Git、Go 1.25+；Agent 已在同一系统用户下安装、登录且能独立使用 / Git, Go 1.25+, agents installed/signed in and working as the same OS user |
| 安装现成 APK / Install existing APK | Android 7.0+、arm64-v8a；允许浏览器安装来源，或 Android SDK platform-tools / Allow the APK install source, or use platform-tools |
| 自建 APK / Build APK | Flutter 3.38+ / Dart 3.10+、Python 3、JDK 17+、Android SDK/build-tools；先通过 `flutter doctor -v` / Complete Flutter's Android toolchain setup |
| 网关 / Gateway | HTTPS 域名、反向代理、飞书网页登录应用；仅 USB 直连不需要 / HTTPS domain, reverse proxy and Feishu web-login application; unnecessary for USB direct |

```sh
git clone https://github.com/csonxx/AgentDeck.git
cd AgentDeck
mkdir -p artifacts
(cd backend && GOWORK=off go build -o ../artifacts/agentdeck.next ./cmd/agentdeck)
mv artifacts/agentdeck.next artifacts/agentdeck
./artifacts/agentdeck serve --help
```

已有 checkout 在原目录更新和构建即可，不必再次克隆。`artifacts/agentdeck` 是生成文件，不入 Git。后端默认数据在 `~/.agentdeck`；用其他目录时，启动、配对及管理命令的 `--data` 必须一致。/ Update an existing checkout in place. The binary is generated, not committed. State defaults to `~/.agentdeck`; every serve/pair/admin command must use the same `--data` if overridden.

## 启动工作站 / Start the workstation

### 方式 A：已有网关 / Existing gateway

以下从仓库根目录执行，先将 `PROJECTS_ROOT` 改成实际存在的项目父目录。示例启用 Codex 和 Claude 日志导入；未安装的 Agent 删除对应参数。`--codex-desktop-socket` 只在该路径确实存在且需操作原 Desktop 会话时添加，不能把示例路径当成所有平台通用路径。

Run from the repository root after setting a real existing project root. The example imports Codex and Claude logs; remove absent agents' flags. Add the optional Codex desktop socket only when it actually exists and native Desktop operations are required.

```sh
PROJECTS_ROOT="$HOME/workspace"
test -d "$PROJECTS_ROOT"
./artifacts/agentdeck serve --relay \
  --data "$HOME/.agentdeck" \
  --codex-sessions "$HOME/.codex/sessions" \
  --claude-sessions "$HOME/.claude/projects" \
  --project-roots "$PROJECTS_ROOT" \
  --media-roots "$PROJECTS_ROOT"
```

需要 Codex Desktop 原会话时，给上面启动命令增加 / For original Codex Desktop chats, add:

```sh
--codex-desktop-socket "$HOME/.codex/ipc/ipc.sock"
```

1. 保持后端运行，电脑浏览器打开 **http://127.0.0.1:8787/**。未关联时也能打开。/ Keep the process running and open the local console, even before enrollment.
2. 填自己的完整网关 HTTPS 地址和工作站名称，点击飞书关联，用飞书授权后返回，确认状态在线。地址有 `/agentdeck` 等前缀时必须保留。/ Enter your full gateway URL and workstation name, authorize with Feishu, return and verify Online. Preserve any path prefix.
3. 按下一节在手机登录和配对。网页可查看在线 App 数量、名称、IP、待配对请求及网关状态。/ Sign in and pair the phone below. The console shows app connections, names, IPs, pairing requests and gateway status.

`--relay` 不开放手机直连的公网 TCP 端口；8787 只是电脑本机管理页，手机不要填写它。后端需持续运行，关闭终端/进程即停止服务；后台常驻见[服务管理](#日常启动停止与数据--service-and-state-management)。/ Relay mode opens no public workstation listener. Port 8787 is local administration, not the phone connection address. The process must remain running; closing it stops service.

### 方式 B：Android USB 直连 / Android USB direct

无需网关或飞书。开启手机 USB 调试，在电脑运行 `adb devices -l`，解锁手机并允许调试；把 `DEVICE_SERIAL` 换成目标手机编号，多台手机时不要省略 `-s`。

No gateway or Feishu is required. Enable USB debugging, run `adb devices -l`, unlock the phone and approve debugging. Replace `DEVICE_SERIAL` with the intended device and retain `-s` when multiple phones are attached.

```sh
PROJECTS_ROOT="$HOME/workspace"
test -d "$PROJECTS_ROOT"
./artifacts/agentdeck serve \
  --data "$HOME/.agentdeck" \
  --listen 127.0.0.1:8443 --public-url https://localhost:8443 \
  --codex-sessions "$HOME/.codex/sessions" \
  --claude-sessions "$HOME/.claude/projects" \
  --project-roots "$PROJECTS_ROOT" --media-roots "$PROJECTS_ROOT"
```

另开终端，在相同仓库根目录执行 / In a second terminal at the same checkout:

```sh
adb -s DEVICE_SERIAL reverse tcp:8443 tcp:8443
./artifacts/agentdeck pair --data "$HOME/.agentdeck" --out /tmp/agentdeck-pair
```

App 选择扫码/粘贴配对，扫描 `/tmp/agentdeck-pair.png`，或私下复制 `.json` 到 App。邀请五分钟内有效且单次使用；成功后删除这两份一次性文件。这里只使用回环自签证书的公钥绑定，不能改成全局忽略 TLS 错误。USB 重连后再执行 `adb reverse`，保留的配对仍有效。

Choose QR/pasted pairing in the app and use the generated PNG/JSON privately. Invitations expire after five minutes and are single-use; delete both files after success. Loopback self-signed TLS uses public-key pinning, never a global certificate bypass. Repeat `adb reverse` after USB reconnection; existing pairing remains valid.

直连和 relay 选一种启动同一数据目录，不要同时运行两个进程。直连工作站经公网 HTTPS 使用时，见 [Envoy/TLS 部署](docs/deploy/README.md)。/ Run one mode/process per data directory. For direct public HTTPS, use the Envoy/TLS deployment guide.

## 连接手机 / Connect the phone

### 安装 / Install

手机下载上方对应 APK，按 Android 提示允许该安装来源并安装。USB 安装也可使用 / Download an edition on the phone and approve its Android install source, or install with ADB:

```sh
adb -s DEVICE_SERIAL install -r /absolute/path/to/agentdeck-0.4.41-arm64-v8a-lite.apk
```

签名不一致会拒绝覆盖；不要通过自动卸载来“修复”，卸载会丢失手机配对和设置。/ A signing mismatch prevents replacement. Do not automatically uninstall to work around it; uninstalling loses phone pairing and preferences.

### 网关配对 / Gateway pairing

1. 手机选「通过网关 · 飞书登录」，填写与电脑完全一致的 HTTPS 地址和前缀。/ Select gateway/Feishu sign-in and enter exactly the workstation's gateway URL, including its prefix.
2. 用**同一飞书账号**授权，选择在线工作站；同一手机可点授权链接，无需扫描自己屏幕的二维码。/ Authorize as the **same Feishu account**, then select the online workstation. On one phone, follow the authorization link rather than scanning its own QR.
3. 电脑打开 http://127.0.0.1:8787/，将**手机显示的 12 位十六进制校验码**填入待配对请求，点击校验并批准。/ Enter the **phone's 12-character hexadecimal code** in the local console and approve that request.
4. 手机确认两边一致；等待中的手机会在电脑批准后继续。确认首页显示「实时连接」，并能打开真实会话。/ Confirm the code matches on the phone; a waiting phone proceeds after approval. Verify Live connection and real chat history.

飞书登录只确认归属，不代替端到端配对；网关绑定完成不等于手机已配对。请求过期后重选工作站即可，不需要重置后端。/ Feishu establishes ownership, not the end-to-end pairing key. Gateway enrollment alone does not pair a phone. If a request expires, select the workstation again without resetting backend state. [详细流程 / Detailed workflow](docs/WORKSTATION.md)

## 各 Agent 怎么接 / Connect each agent

| Agent | 操作 / Setup | 当前边界 / Current boundary |
| --- | --- | --- |
| Codex | 先在电脑独立登录使用；配置 `--codex-sessions`，原 Desktop 会话另配置其 IPC / Sign in on the workstation; import logs and optionally configure Desktop IPC | 原会话发送依赖运行中的 Desktop 及兼容 IPC；macOS 在明确无持有者时可打开原 ID，不自动创建分支 / Original input requires a running compatible Desktop; macOS can open the original ID when explicitly unowned, without silently forking |
| Claude Code CLI | 先运行 `claude` 完成原生登录，配置 `--claude-sessions` / Sign in natively and enable the log root | Local 自己持有的 CLI 使用 stream-json；不等于接管 Desktop 进程 / Local-owned stream-json execution is separate from Desktop ownership |
| Claude Desktop | macOS 原生目录自动发现；直发为单独显式启用的实验功能 / macOS catalog auto-discovery; input is a separate opt-in experiment | 需要所有者经官方菜单开启本机调试入口及两个启动开关，默认关闭，不能保证任意版本兼容 / Requires owner-enabled local debugger and flags; defaults off, version dependent |
| OpenCode | 启动回环 server，配置 `agents.json` / Start a loopback server and configure agents.json | REST/SSE 原 ID 会话；已做真实链路验收 / Original IDs over REST/SSE; real integration exercised |
| Qoder / TRAE | 安装、原生登录，再配置 ACP CLI / Install, sign in and configure ACP | 只展示 CLI 实际支持的会话及能力，不承诺读取 IDE 私有历史；当前未做同等实机验收 / Only advertised CLI capabilities/history; no equivalent live acceptance |
| WorkBuddy | 注册开放平台应用、配置回调和环境密钥，再从 App 授权 / Register an application, callback and environment secret, then authorize in the app | 入口已实现；无应用凭据时不可用，尚无真实授权验收 / Implemented entry point; unavailable without credentials, real OAuth acceptance pending |

额外 Agent 配置放在工作站 `~/.agentdeck/agents.json`（0600），启动时加 `--agents-config "$HOME/.agentdeck/agents.json"`。只复制实际启用的块，不把未配置的 Agent 全部开启。OpenCode 最小示例 / Put only enabled integrations in a private workstation configuration and add the flag. Minimal OpenCode example:

```sh
opencode serve --hostname 127.0.0.1 --port 4096
```

```json
{"opencode":{"url":"http://127.0.0.1:4096"}}
```

没有认证的回环示例只供本机可信用户；有密码时使用 `password_env` 指向安全注入的环境变量，OpenCode 和 S-LOCAL 必须继承同一密码。/ The unauthenticated example assumes trusted local users. For authentication, use password_env and inject the same password into both processes. [完整配置、WorkBuddy 权限和授权 / Full integration instructions](docs/AGENTS.md)

Claude Desktop 直发曾在特定版本和设备完成原会话验收，但不代表当前所有环境均可用；目前这条排障工作暂停。不要为了安装 AgentDeck 自动退出登录、改隐私设置或开启调试器。需要这项能力时由所有者按[本机管理指南](docs/WORKSTATION.md#claude-app-原会话发送--original-claude-chats-in-the-app)明确启用并重新验收；9229 只允许回环，不能经过公网代理。

Claude Desktop input passed historical tests on a specific version/device, not every current environment; that troubleshooting work is currently paused. Installation must not sign the user out, change privacy settings or automatically enable debugging. Owners requiring it should explicitly follow the workstation guide and revalidate. Never publish the loopback debugger on port 9229.

## 在 App 里使用 / Use the app

| 位置 / Location | 使用方式 / How to use |
| --- | --- |
| 首页 / Home | 先选 Agent，再选「项目 / 最新会话」；项目可折叠、排序、选择显示；下拉刷新 / Choose an agent, then Projects/Recent; fold, order, hide projects and pull to refresh |
| 会话输入 / Composer | 输入文字发送；支持图片/文件。显示真实模型、effort 和支持的快速模式，读取不到不冒充默认 / Send text or attachments; show reported model/effort and supported fast mode without inventing unknown values |
| 权限盾牌 / Permission shield | 查看原生权限及完整说明，在线修改同一会话；未指定时沿用工作站默认 / Inspect native permissions/details and change the same chat online; unspecified policies follow workstation defaults |
| 执行过程 / Execution | 工具调用默认折叠，支持整体展开/收起；执行中转圈 / Tool runs default to folded and expand/collapse together; active tasks show a spinner |
| 审批 / Approval | 先展开完整命令、目录和参数，再允许或拒绝 / Expand full command, directory and arguments before allowing/denying |
| 历史与目录 / History and outline | 上滑自动获取更早、下滑补齐后续；折叠工具页自动翻过；目录筛选「我发送的 / 我收到的」并搜索跳转 / Automatic bidirectional paging past folded tools; filter sent/received outline entries and search/jump |
| 复制 / Copy | 每条文字消息下方复制完整原文，保留 Markdown / Copy the complete original text beneath each message |
| 附件 / Attachments | 图片加载有转圈、失败可重试；原文件下载前确认，下载任务支持后台/恢复；可选浏览器或其他应用 / Image loading/retry; confirm original-file downloads, resume/background tasks, open externally |
| 设置 / Settings | Agent 显示、设备名称、标准/小一号界面、语言及 4 位启动密码 / Agent visibility, device name, standard/smaller UI, languages and four-digit app passcode |

启动密码连续输错五次冷却十分钟，不递增；它是本机打开保护，不能替代系统锁屏和端到端配对。语音版语音在手机离线识别，先填入输入框，确认后手动发送；轻量版无麦克风入口。模型、effort、快速模式和权限可改能力由 Agent/会话决定，拒绝或不支持时显示真实结果。/ Five wrong passcodes cause a fixed ten-minute cooldown. It complements device security and pairing. Speech is recognized offline on the phone into an editable draft and sent manually; Lite has no microphone entry. Model/effort/fast-mode/policy changes depend on the agent and chat; rejection/unsupported status remains visible. [输入详情 / Inputs](docs/INPUTS.md)

### 查看用量 / View usage

项目统计行的「用量」可查看 Codex / Claude 原生可获取的账号限额、窗口和刷新时间；不是按聊天次数推算额度。登录、版本或服务不支持时明确报错，数据有采样/缓存时延。/ Tap Usage beside the project count for available native Codex/Claude account limits/windows/reset times. Chat counts are not token quotas; unavailable access is reported explicitly and cached samples can lag.

### 执行完成未读提示 / Unread completion indicator

项目和最近会话中的蓝点表示未查看的完成结果，打开会话后清除；运行中仍用转圈。Codex 优先同步当前电脑账号和本机的原生未读状态：电脑已读可清除手机蓝点，手机查看也保存加密本地已读。**手机已读回写电脑 Codex 尚未打通**，不能声称双向一致。无可读原生状态时使用本地完成回执。/ Dots indicate unseen completions and clear on opening; running chats use a spinner. Codex prefers native account/host unread state. Desktop reads clear phone dots; phone reads are encrypted local receipts. **Phone-to-Desktop read writeback is not yet supported.** Local completion receipts are the fallback.

目标入口只显示进行中的 Goal；暂停、删除和其他操作以原生 Agent 实际支持的能力为准，历史完成文本不是当前执行状态。/ Goals show ongoing work; pause/delete and other actions depend on native capabilities. Historical completion text is not live execution state.

## 更新 App 与发布两种包 / App updates and publishing

设置 → 应用更新可选择「轻量版 / 带语音版」，查看版本、大小和说明，手动下载和安装。自动检查通常每六小时一次；默认跟随安装类型，主动选择后使用所选类型。支持同版本切换，不允许降级；下载中/待安装时先取消任务才能改选。Android 后台负责下载，恢复后可继续；也可用浏览器。安装前核对字节数、SHA-256、包名、版本、目标类型和签名，最后由系统确认。

Settings → App updates offers edition selection, version/size/notes, downloads and installation. Automatic checks normally run at most once per six hours, initially following the installed edition and then an explicit selection. Same-version switches are allowed; downgrades are blocked. Cancel active/ready downloads before changing editions. Android owns background/resumable downloads, with browser fallback. Installation validates bytes, SHA-256, package, version, edition and signer before OS confirmation.

更新地址使用已配对/登录的网关；直连未设置网关时使用构建默认值。自建版本可在手动 Flutter 构建时设置 `--dart-define=AGENTDECK_GATEWAY_URL=https://your-gateway.example/agentdeck`。不要把仓库自带的测试网关当作其他用户的部署服务。/ Updates use the paired/signed-in gateway, falling back to the compiled default for direct-only installations. Override that default in a manual Flutter build using the shown dart-define. The repository's testing gateway is not a hosting service for other installations.

网关公开接口 / Public gateway endpoints (prepend your actual gateway base URL):

```text
GET /v2/app/releases/android?abi=arm64-v8a&edition=lite
GET /v2/app/releases/android?abi=arm64-v8a&edition=voice
GET /v2/app/releases/android/apk?edition=lite
GET /v2/app/releases/android/apk?edition=voice
```

未指定类型兼容旧版返回 voice。APK 位于你自己的 HTTPS 文件存储/CDN，网关仅在现有加密数据库保存清单，不新增 PVC。发布过程：先构建双包 → 上传至不可变 URL → 全量下载核验 SHA-256 → 生成清单 → 在运行中的网关机器通过 owner socket 发布。每个通道独立拒绝降级及同版本不同内容替换。/ Omitting edition retains voice for old clients. Store APKs in your HTTPS object store/CDN; the gateway stores only encrypted manifests, requiring no extra PVC. Build, upload immutable objects, fully download/hash-verify, generate manifests, then publish through the running gateway's owner socket. Downgrade/replacement guards are independent per channel.

```sh
# Run per edition, replacing VERSION and the verified receipt/notes paths.
python3 scripts/android_release_manifest.py --edition lite \
  --apk artifacts/agentdeck-VERSION-arm64-v8a-lite.apk \
  --receipt artifacts/apk-public-VERSION-lite.json \
  --notes-file release-notes.txt --output android-release-lite.json
# Run on the gateway host, using its actual binary and state directory:
agentdeck gateway-publish-release --data "$HOME/.agentdeck-gateway" \
  --android-manifest android-release-lite.json
```

receipt 为上传后实际公网全量核验记录，包含 `url`、`bytes`、`sha256`、`anonymous_https_full_download_verified: true`；生成器再与 APK 比对。voice 同样操作并使用 `--edition voice`。网关必须先部署支持双通道的代码；清单发布本身无需重启。/ The receipt records the actual full public download with these four fields; the generator checks it against the APK. Repeat for voice. Deploy dual-channel code before publishing; manifest publication alone needs no restart.

## 自建公网网关 / Host a public gateway

只部署一次 S-GATEWAY；它不能读取 Agent 日志、项目或媒体。准备 HTTPS 域名、Envoy/反向代理，以及你自己的飞书网页登录应用。证书留在代理；Go 监听受控私网 HTTP/WS，不向公网开放明文端口。/ Deploy S-GATEWAY separately without agent log/project/media sources. Provide HTTPS, a reverse proxy and your Feishu web-login application. Certificates remain on the proxy; private Go HTTP/WS is never public.

1. 在飞书开放平台配置网页登录、用户可用范围和**完整重定向地址**：`https://your-gateway.example/agentdeck/v2/auth/callback`。路径必须与 `--public-url` 一致。/ Configure web login, authorized users and the exact callback matching public-url.
2. 在网关安全环境文件中注入 `FEISHU_APP_ID` / `FEISHU_APP_SECRET`，不是在手机或 Local 填。当前登录只读取身份，不需要机器人/通讯录功能；scope 及应用类型要求见[飞书指南](docs/GATEWAY.md)。/ Inject the application credentials into the gateway's protected environment, never the phone/Local. Identity-only login does not use bots/address-book features; see the detailed guide for scopes/application availability.
3. 启动网关（环境变量须先由服务管理器或当前安全环境加载；示例值需替换） / Start with credentials already supplied securely:

```sh
./artifacts/agentdeck serve --role gateway \
  --data "$HOME/.agentdeck-gateway" \
  --behind-proxy --listen 127.0.0.1:8080 \
  --public-url https://your-gateway.example/agentdeck \
  --rotation 15m --log-level info --connection-log-interval 30s
```

4. 代理保留 `/agentdeck` 前缀转发；Go 网关负责剥离。支持 WebSocket upgrade，流无总超时，保留认证 Header，不对一次性登录/配对请求重试或缓存。容器跨主机时使用受控私网；跨不可信网络另用 mTLS/安全隧道。/ Preserve the prefix to the gateway, enable WebSocket upgrade, no total stream timeout, retain authentication headers, and avoid retries/caching for one-time operations. Protect cross-host upstream transport appropriately.
5. 先验证 `https://your-gateway.example/agentdeck/healthz`、真实飞书授权、本机隧道在线，再连接手机。`Ready` 或健康检查成功不能代替登录、配对和收发验收。/ Verify health, real Feishu authorization and an online Local tunnel before phone pairing. Ready/health alone is not end-to-end acceptance.

飞书应用必须自行申请，扫码不会凭空获得 App ID/Secret。仅同租户、同账号归属的工作站可发现；跨企业还需飞书应用类型/授权范围支持。网关是单实例，数据目录需持久化；数据库和 `storage.key` 成对备份，不能只保存数据库。/ You must register a Feishu application; scanning cannot create application credentials. Discovery is limited to the same tenant/account, with cross-enterprise access additionally constrained by Feishu. Persist the single-instance gateway's state and back up database/key together.

完整模板 / Templates: [Gateway](docs/GATEWAY.md) · [Envoy](docs/deploy/README.md) · [gateway.env.example](docs/deploy/gateway.env.example) · [systemd](docs/deploy/agentdeck-gateway.service)

## 日常启动停止与数据 / Service and state management

- **启动/停止 / Start/stop:** 示例前台进程使用 Ctrl+C 正常停止。常驻 Linux 可按实际路径安装 [Local systemd user service](docs/deploy/agentdeck-relay-local.service)；macOS 使用自己的 LaunchAgent，写实际绝对路径、Agent CLI PATH、日志位置，并以登录 Agent 的用户运行。不要把本仓库开发者的电脑路径复制到其他机器。/ Foreground examples stop with Ctrl+C. Use the supplied Linux user-service template or a workstation-specific macOS LaunchAgent with real absolute paths, CLI PATH and logs.
- **本机管理 / Console:** http://127.0.0.1:8787/；改端口用 `--console-listen 127.0.0.1:PORT`。只监听回环，不能交给公网代理。/ Loopback only; never expose this administration interface through a public proxy.
- **数据 / State:** Local 默认 `~/.agentdeck`，Gateway 单独 `~/.agentdeck-gateway`；同一目录只运行一个后端。升级不换目录；迁移前正常停止并成对备份 `vault.db` 和 `storage.key`。/ Separate Local/Gateway state; one process per directory. Preserve directories on upgrade and back up database/key together after a clean stop.
- **App 管理 / Devices:** 电脑管理页可改名、查看在线 IP、撤销配对；手机设置也可改名。撤销使旧手机密钥失效，需要重新配对，历史保留。/ Rename, inspect online IP and revoke from the console; the phone can rename itself. Revocation invalidates the device key, requiring a new pairing without deleting history.
- **网关解绑 / Unlink:** 在电脑页解除关联；网关验证撤销并回执后清除本机关联。重新关联获得新身份，手机需重新选工作站及配对。网关不可用时保留原关联供重试。/ Unlink waits for authenticated gateway revocation; re-enrollment requires phone selection/pairing again. Failed unlink keeps the original association.
- **保活 / Availability:** Mac 锁屏不等于休眠。电脑休眠、退出 Agent 或停止 Local 会影响执行/同步；不要用修改 Agent 登录和权限解决网络问题。/ Screen lock and sleep differ. Sleep, agent exit or stopping Local interrupts service; do not alter credentials/permissions to fix transport issues.

## 排障与验收 / Troubleshooting and acceptance

| 现象 / Symptom | 检查 / Check |
| --- | --- |
| 手机连不上 / Phone offline | 电脑管理页后端/网关状态 → 完整域名和前缀 → 代理 WS → 手机网络；USB 模式重做 adb reverse / Console state, exact URL/prefix, proxy WS and phone network; repeat adb reverse for USB |
| 网关登录失败 / Gateway login fails | HTTPS 健康检查、真实 App ID/Secret、完整飞书回调和可用范围；TODO 不可登录 / Health, real app credentials, exact callback and app availability; TODO is not a valid configuration |
| 等待工作站确认 / Waiting for approval | 电脑 8787 页面输入该手机校验码并批准；过期重选工作站 / Approve the phone's code in the console; reselect after expiry |
| 会话少/标题不同 / Missing chats | 同一系统用户、日志目录、lookback 默认 72h、首次每日志最近 8 MiB；`--lookback 0 --history-bytes 0` 可全量导入但启动更慢 / Check user/roots and import limits; zero values opt into slower full import |
| 无法发送原会话 / Cannot send to original chat | 检查 Agent 登录/额度/原生状态和 IPC 配置；不确定送达时先核对原会话，不自动重发或创建分支 / Check native login/quota/state and IPC; reconcile uncertain delivery before any resend |
| 附件不允许访问 / Attachment outside allowed roots | `--media-roots` 必须包含真实文件目录；符号链接按实际路径校验；不要放开整个文件系统 / Allow the actual directory, including resolved symlink target, rather than the entire filesystem |
| 工具次数增加但对话没动 / Tool-only paging | 折叠过程自动继续翻页；断线时先恢复连接。开启图片加载会显示进度，失败可重试 / Folded tools are paged through; reconnect if offline, use image loading/retry indicators |
| 更新页没有轻量版 / No Lite update | 网关须部署双通道代码并发布对应清单；旧网关只回 voice 时不会替换用户选择 / Deploy channel support and publish both manifests; a mismatched old voice response is ignored |
| 更新签名失败 / Signing mismatch | 保留原固定签名，检查来源；不自动卸载/清空来绕过 / Verify provenance and signer; do not uninstall/reset automatically |
| 启动后暂时无页面 / Slow initial startup | 大型已有 vault 的展示索引检查可延迟监听；查看启动进程/日志，避免反复强杀或重建数据 / Existing large vaults may delay serving during index checks; inspect progress rather than repeatedly killing/resetting |

网关默认文本 INFO 日志，连接变化立即打印；每 30 秒输出 `connection_stats`（在线工作站、App 流及请求数）。`--log-level debug` 可辅助排障，仍不应记录凭据或会话正文。代理后真实 IP 仅信任 `AGENTDECK_TRUSTED_PROXY_CIDRS` 指定的来源。/ Gateway INFO text logs include immediate connection changes and a 30-second connection_stats summary. Debug logging remains credential/content safe; forwarded IPs require explicitly configured trusted proxy CIDRs. [日志与本机排障 / Diagnostics](docs/WORKSTATION.md)

完整验收至少检查：手机看到真实历史 → 专用测试会话发送且在目标 Agent 原会话执行 → 输出/图片同步 → 后台恢复和断线补齐 → 需要时完整审批 → 已配对设备撤销。没有真实授权的 Agent 或没有现场执行的分支，标为未验收。/ Acceptance requires real history, execution in a dedicated intended chat, live output/images, background/reconnect recovery, applicable approval and device revocation. Mark unexercised agents/branches explicitly as unaccepted.

## 自行构建与开发 / Build and develop

```sh
flutter doctor -v
(cd app && flutter pub get)
export FLUTTER="$(command -v flutter)"
# Lite does not require downloading speech models.
python3 scripts/build_android_editions.py --edition lite
# With voice / both editions: download and verify public model weights first.
python3 scripts/setup_client_voice.py
python3 scripts/build_android_editions.py --edition all
```

Linux/自定义安装请设置实际 `JAVA_HOME`、`ANDROID_SDK_ROOT`；脚本默认路径针对 macOS，`FLUTTER` 应指向可执行文件。两包输出在 `artifacts/agentdeck-VERSION-arm64-v8a-{lite,voice}.apk`，同时生成原生版本、哈希、签名及冻结源码校验记录。原始 Flutter 文件名带 flavor：`app-arm64-v8a-lite-release.apk` / `app-arm64-v8a-voice-release.apk`。/ Set actual Java/SDK locations on Linux/custom setups; script defaults are macOS-oriented. Outputs include edition APKs and verified native-version/hash/signer/source metadata. Raw Flutter output filenames include the flavor.

独立 Flutter 构建示例（替换你的网关；在 `app/` 目录执行） / Manual Flutter build with your update fallback gateway:

```sh
flutter build apk --release --flavor lite --split-per-abi \
  --target-platform android-arm64 \
  --dart-define=AGENTDECK_GATEWAY_URL=https://your-gateway.example/agentdeck
```

提交前验证 / Verify before committing:

```sh
bash scripts/verify.sh
(cd app && flutter test --no-pub --flavor lite test/input_composer_lite_test.dart)
git diff --check
```

verify 使用隔离的合成会话及身份执行 Go race/vet、Flutter analysis、真实 HTTP/TLS/加密中继协议和 UI 回归，不会用真实聊天发任务。它会下载语音模型并构建本地后端；不替代真实飞书、手机或生产部署验收。APK、模型权重、运行目录、数据库、密钥及配对码不提交。/ verify uses isolated synthetic chats/identity for Go race/vet, Flutter analysis, HTTP/TLS/encrypted relay and UI regression. It downloads speech models and builds the backend, but does not replace real Feishu/device/deployment acceptance. Never commit APKs, weights, runtime state, databases, keys or pairing material.

| 文档 / Document | 内容 / Contents |
| --- | --- |
| [Agent 接入 / Integrations](docs/AGENTS.md) | OpenCode、ACP、WorkBuddy OAuth、权限、已读边界 |
| [网关 / Gateway](docs/GATEWAY.md) | 飞书应用、角色、配对和租户 |
| [本机管理 / Workstation](docs/WORKSTATION.md) | 网页、解绑、App 管理、日志、实验性 Desktop 接入 |
| [部署 / Deployment](docs/deploy/README.md) | Envoy、直接 TLS、systemd 模板 |
| [输入 / Inputs](docs/INPUTS.md) | 模型、effort、附件和语音 |
| [安全 / Security](docs/SECURITY.md) | 信任边界、轮换、持久化与授权 |
| [验收 / Acceptance](docs/ACCEPTANCE.md) | 按日期/版本区分代码、实机和公网证据 |

语音模型及第三方许可 / Speech and third-party licensing: [Client speech](docs/CLIENT_VOICE_LICENSE.md) · [licenses](docs/licenses/)
