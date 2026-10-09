# AgentDeck · 会话甲板

**把工作站上的 coding agent 会话带到手机。** Go 后端 + Flutter 原生客户端，支持 Codex、Claude Code、OpenCode、Qoder、TRAE、WorkBuddy，提供加密直连与飞书多租户网关。

**Bring your workstation’s coding-agent sessions to your phone.** A Go backend and native Flutter client for Codex, Claude Code, OpenCode, Qoder, TRAE and WorkBuddy, with encrypted direct connections and a Feishu-authenticated multi-tenant gateway.

版本 / Version: **0.4.14** · 源码仓库 / Source: [csonxx/AgentDeck](https://github.com/csonxx/AgentDeck)

AgentDeck 是可信工作站所有者的**移动 coding agent 工作台**：按 Agent、项目和最新会话查看 Codex、Claude Code、OpenCode、Qoder、TRAE、WorkBuddy。各接入按官方能力提供创建、发送、续聊、停止与工具审批，展示消息、思考、工具和图片；WorkBuddy 当前对应账号的一个本地助理。界面使用黑白灰、留白和克制的强调色，借鉴 ZEEKR App 的风格，使用自己的名称和图标。

AgentDeck is a **mobile coding agent workspace** for trusted workstation owners. Organize Codex, Claude Code, OpenCode, Qoder, TRAE and WorkBuddy by agent, project and recent chats. Each integration exposes supported creation, continuation, sending, interruption and approval operations, with messages, exposed reasoning, tools and images. WorkBuddy currently exposes one Local Assistant per account. Its own branding uses a restrained black-and-white interface inspired by the ZEEKR app.

## 真机界面 / On-device screenshots

GM1900 上运行的 **0.4.13 正式客户端**，使用已有加密配对；为避免展示私人会话，项目组已折叠。下图为原始真机截图，不是界面模型或示例会话。

The **0.4.13 production client** on GM1900, using its retained encrypted pairing. Project groups are collapsed to keep private chat contents out of the screenshots. These are original device captures, not mockups or demo sessions.

<p>
  <img src="docs/images/app-0.4.13-workspace.png" width="260" alt="AgentDeck 0.4.13 real Android workspace with collapsed project groups / 真机项目列表">
  <img src="docs/images/app-0.4.13-settings.png" width="260" alt="AgentDeck 0.4.13 real Android settings / 真机设置">
</p>

## 目录 / Contents

- [真机界面 / On-device screenshots](#真机界面--on-device-screenshots)
- [使用顺序 / How to use](#使用顺序--how-to-use)
- [功能与界面 / Features and screenshots](#功能与界面--features-and-screenshots)
- [架构与协议 / Architecture and protocols](#架构与协议--architecture-and-protocols)
- [环境与构建 / Requirements and builds](#环境与构建--requirements-and-builds)
- [连接并开始会话 / Connect and start a conversation](#连接并开始会话--connect-and-start-a-conversation)
- [直连工作站 / Connect directly](#直连工作站--connect-directly)
- [飞书网关模式 / Feishu gateway mode](#飞书网关模式--feishu-gateway-mode)
- [项目、发送与续聊 / Projects, sending and continuation](#项目发送与续聊--projects-sending-and-continuation)
- [接入 Codex 与 Claude / Connect Codex and Claude](#接入-codex-与-claude--connect-codex-and-claude)
- [多 Agent 接入与显示设置 / Multi-agent integration and visibility](#多-agent-接入与显示设置--multi-agent-integration-and-visibility)
- [升级、重启与数据保留 / Upgrade, restart and retain data](#升级重启与数据保留--upgrade-restart-and-retain-data)
- [加密与授权 / Encryption and authorization](#加密与授权--encryption-and-authorization)
- [命令与配置 / Commands and configuration](#命令与配置--commands-and-configuration)
- [测试与验收 / Tests and acceptance](#测试与验收--tests-and-acceptance)
- [排障 / Troubleshooting](#排障--troubleshooting)
- [项目结构与开发 / Project layout and development](#项目结构与开发--project-layout-and-development)
- [当前限制 / Current limitations](#当前限制--current-limitations)
- [相关文档 / Further documentation](#相关文档--further-documentation)
- [版本历史 / Version history](#版本历史--version-history)

## 使用顺序 / How to use

首次使用按以下顺序完成；安装 App 本身不会自动发现电脑，也不会生成示例会话。下文命令均从仓库根目录执行，`DEVICE_SERIAL`、示例域名、项目目录及应用 ID 必须换成自己的值。

For a first run, follow this order. Installing the app does not discover your computer automatically or generate sample chats. Run commands below from the repository root and replace device serials, example domains, project paths and application IDs.

1. **构建并安装** Go 后端和手机 App。 / **Build and install** the Go backend and phone app.
2. **配置 Agent**：在运行 S-LOCAL 的系统用户下安装、登录所需 CLI；OpenCode 启动服务，WorkBuddy 另外申请应用并授权。 / **Configure agents** under the OS user running S-LOCAL. OpenCode needs a server; WorkBuddy needs application registration and authorization.
3. **选一种连接方式**，按对应章节启动并配对。 / **Choose one connection mode**, then start and pair using its instructions.
4. **手机选择 Agent → 项目或最新会话 → 打开/新建 → 发送**，确认真实回复，再测试图片与历史跳转。 / **On the phone: Agent → Projects or Recent → open/create → send**. Verify a real reply, then test images and history navigation.

| 需求 / Need | 选择与前提 / Mode and prerequisites |
| --- | --- |
| Android 插电脑先体验 / Try Android over USB | [USB 直连](#本地开发与-usb-手机--local-development-and-a-usb-phone)：USB 调试、`adb reverse`，不需要飞书或公网域名。 / USB debugging and reverse forwarding; no Feishu app or public domain. |
| 手机随时通过公网连接自己的工作站 / Reach a workstation over the internet | [Envoy 直连](#公网-envoy-终止-tls--public-access-with-envoy-tls-termination)：HTTPS 域名、证书和到 Local 的受限上游。 / An HTTPS domain, certificate and restricted upstream to Local. |
| 多租户目录、工作站仅主动连出 / Multi-tenant discovery and outbound-only workstations | [飞书网关](#飞书网关模式--feishu-gateway-mode)：独立 Gateway、公网 HTTPS/WSS、真实飞书应用与可用范围。 / A separate Gateway, public HTTPS/WSS and an available registered Feishu application. |

直连配对不需要飞书；飞书登录和 WorkBuddy OAuth 是两套独立授权。已有 Envoy 可继续持有证书。`--relay` 与直连启动参数二选一，不要同时启动两个进程占用同一 `--data`。

Direct pairing needs no Feishu account. Feishu login and WorkBuddy OAuth are independent authorizations. Existing Envoy can keep its certificates. Choose relay or direct startup; do not run two processes against the same data directory.


## 0.4.13：旧 Office、文本与音频 / Legacy Office, text and audio

| 文件 / File | 手机预览 / Phone preview |
| --- | --- |
| `.doc`（Word 97–2003） | 正文段落、表格中的文字和域显示结果；不复刻页面排版。 / Body paragraphs, table text and field display results; page layout is not reproduced. |
| `.xls`（Excel 97–2003 / BIFF8） | 工作表切换、中文、原始行号、稀疏单元格、数值/布尔值/错误值和已保存的公式结果。没有缓存结果的公式保持空白。 / Sheet selection, Unicode text, original row numbers, sparse cells, numeric/boolean/error values and stored formula results. Uncached formulas remain blank. |
| `.txt/.log/.csv/.tsv`、代码与配置文本 / source and configuration text | UTF-8、带 BOM 的 UTF-16；明确文本扩展名另外支持 GB18030/GBK。自动识别后转为 UTF-8，敏感字段脱敏；大文件显示前 128 KiB 并提示。 / UTF-8 and BOM-marked UTF-16; explicit text extensions also support GB18030/GBK. Text is normalized to UTF-8 and sensitive fields are redacted. Large files show a labeled 128 KiB prefix. |
| MP3、WAV、M4A、AAC、FLAC、OGG/Opus、AMR、AIFF | 原生播放、暂停、拖动进度；离开前台自动暂停，关闭预览释放内存与回环服务。解码能力取决于手机系统和文件编码。 / Native play, pause and seeking; backgrounding pauses playback and closing releases memory and the loopback service. Decoder availability depends on the phone and codec. |

本地输出必须在当前会话中引用、位于允许目录且不超过 8 MiB；上传附件预览必须属于当前配对手机与会话。旧 Office 在 S-LOCAL 内存中解析成受限内容，再经现有加密通道发送；不安装 Office、不运行宏或公式、不改动供 agent 读取的原文件。`.doc` 最多 10,000 段、约 1 MiB 正文；`.xls` 最多 50 表、每表 2,000 行/100 列、单元格 4,096 字符，并有全局内容预算。加密或损坏的旧 Office 文件会显示明确错误。更早的 Word/BIFF 格式不在此范围内。

Local outputs must be referenced by the current chat, inside allowed roots and within 8 MiB. Uploaded previews are scoped to the paired phone and chat. S-LOCAL parses legacy Office files in RAM into bounded content sent through the existing encrypted channel. No Office installation, macro/formula execution or modification of the agent's original file is involved. DOC previews allow 10,000 paragraphs and roughly 1 MiB of body text; XLS allows 50 sheets, 2,000 rows/100 columns per sheet and 4,096 characters per cell, with global content budgets. Password-protected or damaged files report explicit errors. Earlier Word/BIFF variants are outside this scope.

音视频通过带随机令牌的手机回环内存服务交给原生播放器，不额外写出明文缓存。HTTPS 音视频直链也可在 App 预览，手机直接从来源下载，遵守 HTTPS、大小和 MIME 校验；这类公开外链不经过 S-LOCAL 端到端加密通道。网页仍由浏览器打开。

Audio/video bytes reach the native decoder through a token-protected phone loopback RAM server, without an additional plaintext cache. HTTPS media links can also preview in-app, fetched directly by the phone with TLS, size and MIME checks; these public external links do not use the Local-to-phone end-to-end encrypted channel. Ordinary web pages still open in the browser.

## 视频、现代 Office 与运行中发送 / Video, modern Office and sending during a turn

- **视频**：点击会话中的 MP4/WebM 本地链接或视频卡片，在 App 播放、暂停和拖动进度。附件仍须由当前会话引用，位于允许目录，单个不超过 8 MiB；格式能否解码取决于手机。传输使用原有端到端加密连接，解密后的播放器数据只在内存中，通过带随机令牌的手机回环地址交给原生解码器，不额外写出明文视频缓存。公开 HTTPS/WSS 与证书校验保持原样。
  **Video:** Open a session-referenced MP4/WebM local link or video card to play, pause and seek. Files must remain in an allowed directory and within 8 MiB; codec support depends on the phone. Existing end-to-end encrypted transport delivers the file. Decrypted bytes remain in memory and reach the native decoder through a token-protected phone loopback address, without an additional plaintext video cache. Public HTTPS/WSS certificate validation is unchanged.
- **文档**：Markdown 按格式显示；`.docx` 显示段落与表格内容；`.xlsx` 可切换工作表，显示列标、原始行号和缓存的公式结果，最多每表 2000 行、100 列。Office 解析在后台 isolate 中执行，不运行宏、公式或外部引用。当前是内容预览，未复刻 Word 排版或 Excel 图表、日期/数字格式；旧 `.doc`/`.xls` 支持见上文。相同的会话授权、目录和 8 MiB 边界适用于文档。
  **Documents:** Markdown renders formatting; `.docx` previews paragraph and table contents; `.xlsx` offers sheet selection, column labels, original row numbers and cached formula values, up to 2,000 rows and 100 columns per sheet. Office parsing runs in a background isolate without executing macros, formulas or external references. These are content previews; Word page layout, Excel charts and date/number formatting are not reproduced. See the section above for legacy `.doc`/`.xls` support. The same session authorization, directory and 8 MiB limits apply.
- **Codex**：执行中发送默认排队；输入框上方显示队列卡片，右侧 `…` 菜单提供“引导、编辑消息、删除消息”，不再在输入框预选引导。电脑原会话经 owner-only Unix IPC **立即交给 Codex 原生队列并等待确认**，由 Codex 决定何时开始下一轮；S-LOCAL 不再等当前任务结束才转发。原生队列优先使用已验证 owner 的广播及操作确认，落盘的 `queued-follow-ups` 用于初始读取和短期缓存到期后的恢复，通过现有加密连接独立推送，不依赖聊天历史分页。电脑端变化约每秒检查一次，手机操作后立即推送。
  **Codex:** Sending during a turn queues by default. Cards sit above the composer; their `…` menus offer Steer, Edit message and Delete message. Original desktop chats **immediately submit to Codex's native queue through owner-only Unix IPC and wait for acknowledgment**. Codex schedules the next turn; Local no longer waits for the current turn to finish before forwarding. Native snapshots prefer verified owner broadcasts and acknowledged mutations; persisted `queued-follow-ups` supplies initial reads and recovery after the short-lived memory snapshot expires. Updates are pushed over the existing encrypted connection, independently of paginated chat history. Desktop changes are checked about once a second; phone actions push an immediate update.
- **引导与边界**：引导先由 Codex 确认移除该队列项，再调用原生 `thread-follower-steer-turn`；保持当前任务模型与权限。删除不影响同组其他消息，编辑保留已支持的附件上下文；版本不一致则拒绝操作。特殊的 Codex App／工具／评论上下文须在电脑操作，避免丢失上下文。引导结果不明确时在 S-LOCAL 加密保留待确认记录，不自动重发。手机管理的 CLI 分支与升级前遗留待发消息仍标为“工作站队列”；OpenCode 保持原接口语义。
  **Steering and scope:** Codex first acknowledges removal of the queue item, then receives `thread-follower-steer-turn`, retaining the active turn's model and permissions. Delete leaves siblings intact; editing preserves supported attachment context. Stale versions are rejected. Rich Codex App/tool/comment context must be managed on the computer rather than reconstructed incompletely. An uncertain steer retains an encrypted Local record without automatic resending. Managed CLI continuations and pre-upgrade pending records remain labeled Workstation queue; OpenCode keeps its native submission semantics.
- **版本相关限制**：当前原生桌面桥接使用 queue setter v1、remove v1、steer v1。Codex 的 setter 是整组状态更新，没有原子版本校验；本实现读取并保留当前同组消息、拒绝可见冲突，但无法保证电脑在最后一次读取后同时修改队列时不发生覆盖。不要同时在电脑和手机编辑同一组队列；这不是无冲突多端队列的保证。该适配只处理原生本地 follow-up 队列，不宣称支持 Codex durable/server queue。新提交的队列使用本地原生 follow-up 格式，由桌面执行；S-LOCAL 自身的数据仍加密，Codex 自己的持久化由 Codex 管理。
  **Version-dependent limits:** The desktop bridge uses queue setter v1, remove v1 and steer v1. Codex's setter replaces the conversation queue without atomic version checks. This adapter reads and preserves current sibling messages and rejects visible conflicts, but cannot prevent an overwrite if the computer edits after the final read. Avoid simultaneous computer/phone edits to the same queue; this is not a conflict-free multi-client guarantee. This adapter covers native local follow-ups, not Codex durable/server queues. New entries use the local native follow-up format and execute in the desktop. AgentDeck's own persistent data remains encrypted; Codex manages its own persistence.
- **Claude Code / OpenCode**：Claude 的手机管理分支支持排队，以及明确的“中断并继续”；不会接管未管理的原终端任务。OpenCode 使用原会话的 `prompt_async` 接口，允许执行中继续提交，由 OpenCode 决定后续执行；停止仍是独立操作。
  **Claude Code / OpenCode:** Managed mobile Claude continuations support queueing and explicit Interrupt and continue, without taking control of an unmanaged terminal turn. OpenCode submits to the original session's `prompt_async` endpoint while running and lets OpenCode schedule execution; stopping remains a separate action.
- **异常与恢复**：工作站队列每会话最多 20 条、工作站最多 200 条活动消息；原生队列新增每会话最多 20 条，单次 IPC 状态限制 16 MiB。任务接收结果不明确，或工作站重启时存在发送中记录，会标记“接收待确认”并暂停该会话后续队列，不自动重发。请先检查电脑；移除待确认记录只移除手机队列标记，不能撤回已经收到的消息。
  **Recovery:** Workstation queues allow 20 active messages per chat and 200 per workstation. Native additions allow 20 per chat and a 16 MiB IPC-state bound. An uncertain acknowledgment, including an in-flight record found after restart, becomes Delivery unconfirmed and blocks subsequent queue entries without automatic resending. Check the computer first. Removing that record only clears the local queue marker; it cannot recall a message already received.

## 功能与界面 / Features and screenshots

| 功能 / Feature | 行为 / Behavior |
| --- | --- |
| 界面语言 / Interface language | 默认英文；点首页右上角设置图标 → Language，可选择 English、简体中文、繁體中文、Русский。立即生效并保存在本机，升级或忘记工作站后保留；会话正文、项目名和模型名保持原文。 / English by default. Open the top-right Settings icon → Language to choose English, 简体中文, 繁體中文 or Русский. Changes apply immediately and persist across restarts, upgrades and unpairing. Conversation content, project names and model IDs remain unchanged. |
| Codex + Claude Code | 读取已有 JSONL 日志，或旁观 stdout JSON 事件流。 / Read existing JSONL logs or mirror JSON events from stdout. |
| 项目与最新会话 / Projects and recent chats | 点击项目标题可折叠整组，每组会话默认显示 5 条；项目管理支持勾选显示/隐藏、全部显示/隐藏、拖动排序、上下移动和恢复默认排序，布局加密保存。隐藏只影响项目列表，最新会话仍可查看。首页先选择显示的 Agent，再看其项目或最新会话；每个 Agent 的项目布局分别保存。 / Tap a project header to collapse its group; groups show five initial chats. Project management supports visibility checkboxes, show/hide all, dragging, move-up/down and reset, with securely stored layout preferences. Hidden projects remain accessible through recent chats. Choose a visible agent first, then browse its projects or recent chats. Each agent has independent layout preferences. |
| 执行状态 / Running state | 列表与会话页按真实状态转圈，任务结束自动消失。 / Lists and chat pages animate while the actual session is running and stop when it finishes. |
| 实时展示 / Live presentation | 消息增量、公开思考摘要、工具调用/结果、文件变更与 Markdown。 / Message deltas, public reasoning summaries, tool calls/results, file changes, and Markdown. |
| 图片与链接 / Images and links | PNG/JPEG/GIF/WebP 在消息内展示、点击缩放，单图最多 8 MiB；网页链接打开手机浏览器，本地图片、音视频、文本、Markdown、Word/Excel 内容经加密连接预览，文本支持源码行号；格式边界见上文。 / PNG/JPEG/GIF/WebP render inline with tap-to-zoom, up to 8 MiB; web links open the phone browser, while local images, audio/video, text, Markdown and Word/Excel content preview over the encrypted connection. Text supports source-line links; format limits are listed above. |
| 重连与缓存 / Reconnection and cache | 打开/重连先取最近最多 40 个完整展示块（每页约 512 KiB 文本预算），旧消息按需分页，后续输出实时追加；手机仅缓存最近一次会话的 40 个展示块。 / Opening/reconnecting fetches at most 40 complete items (roughly 512 KiB text budget per page); older history is paged on demand and new output streams live. The encrypted phone cache retains 40 items from the most recently viewed chat. |
| 直连与代理 / Direct and proxy | HTTPS/WSS；可由 Go 或外层 Envoy 终止 TLS。 / HTTPS/WSS with TLS terminated by Go or an external Envoy. |
| 飞书网关 / Feishu gateway | 同租户、同飞书账号匹配工作站；S-LOCAL 主动连出，无需入站 TCP 端口。 / Match workstations by tenant and Feishu account; S-LOCAL connects outbound without an inbound TCP port. |
| 配对与密钥 / Pairing and keys | 每手机独立 root；默认 15 分钟轮换流量密钥；支持撤销。 / Independent roots per phone, traffic-key rotation every 15 minutes by default, and revocation. |


## 架构与协议 / Architecture and protocols

| 角色 / Role | 运行位置 / Location | 职责 / Responsibilities |
| --- | --- | --- |
| **S-LOCAL** — Go | Agent 所在的 Linux/macOS 工作站。 / The Linux/macOS workstation running agents. | 导入并加密保存会话，持有手机 root；直接服务 App 或主动连接网关。 / Import and encrypt sessions, hold phone roots, and serve the app directly or connect outbound to the gateway. |
| **S-GATEWAY** — Go | 公网域名后的服务器，通常位于 Envoy 后。 / A server behind a public domain, usually behind Envoy. | 飞书身份、租户/账号隔离、工作站目录、访问凭据和密文中继；无会话 root。 / Feishu identity, tenant/account isolation, workstation discovery, routing grants, and ciphertext relay; no session roots. |
| **C-APP** — Flutter | Android/iOS 手机；macOS 开发构建。 / Android/iOS phones; macOS development builds. | 配对、验证身份、解密展示；密钥放平台安全存储。 / Pair, verify identity, decrypt and display content, and keep keys in platform secure storage. |

```text
Direct / 直连
C-APP ── HTTPS/WSS ── [optional Envoy / 可选 Envoy] ── S-LOCAL

Gateway / 网关
S-LOCAL ── outbound WSS + AEAD / 主动加密隧道 ── S-GATEWAY
                                                    │
                                        Envoy / public HTTPS + WSS
                                                    │
                                                  C-APP
```

HTTP 用于配对、会话握手和飞书登录/发现；WebSocket 用于实时事件、图片和 Local 隧道。公网 origin 必须是 HTTPS，不能带子路径、查询串或用户信息。Envoy 可使用自己的证书/SDS，Go 通过 `--behind-proxy` 运行私网上游 HTTP/WS；应用内容仍在内层 AEAD 中。

HTTP handles pairing, session handshakes, and Feishu login/discovery. WebSocket carries live events, images, and the Local tunnel. Public origins must use HTTPS without a path prefix, query string, or user information. Envoy can retain its certificate/SDS configuration while Go uses private HTTP/WS with `--behind-proxy`; application content remains inside an inner AEAD layer.

## 环境与构建 / Requirements and builds

- 后端 / Backend: **Go 1.25+**，Linux/macOS；本机管理使用 owner-only Unix socket。 / Linux/macOS; local management uses an owner-only Unix socket.
- 资源准备 / Asset preparation: **Python 3**、首次下载语音模型所需网络与磁盘空间。 / Python 3, network access and disk space for the initial speech-model download.
- 客户端 / Client: **Flutter 3.38+ / Dart 3.10+**，Android SDK/JDK 或 Xcode/CocoaPods。 / Android SDK/JDK or Xcode/CocoaPods.
- Android: 项目当前最低 Android 7.0 / API 24。 / The project currently targets Android 7.0 / API 24 or later.
- iOS/macOS: 真机/安全存储运行需要合适的签名；本项目不是 Flutter Web 应用。 / Device and secure-storage operation require appropriate signing; this is not a Flutter Web app.

首次检出；已有仓库直接进入目录，不要重复 clone。SSH 克隆需 GitHub SSH 权限，也可用 HTTPS 地址。

For a fresh checkout; enter an existing repository instead of cloning again. SSH cloning needs GitHub SSH access; HTTPS cloning is also available.

```sh
git clone git@github.com:csonxx/AgentDeck.git
cd AgentDeck
# HTTPS alternative: git clone https://github.com/csonxx/AgentDeck.git
```

以下从仓库根目录执行。只部署后端的服务器只需 Go，不必安装 Flutter、Android SDK 或语音模型。Go 模块位于 `backend/`，Flutter 包位于 `app/`。

Run the following from the repository root. A backend-only server needs Go, without Flutter, Android SDK or speech models. The Go module is in `backend/`; Flutter is in `app/`.

```sh
mkdir -p artifacts
(cd backend && GOWORK=off go build -o ../artifacts/agentdeck.next ./cmd/agentdeck)
mv artifacts/agentdeck.next artifacts/agentdeck
```

手机端构建环境可按 [Flutter 官方安装指南](https://docs.flutter.dev/get-started/install) 准备，用 `flutter doctor` 检查。先安装依赖并下载资源，再选择自己的目标平台：

Prepare the client toolchain using the [official Flutter installation guide](https://docs.flutter.dev/get-started/install) and check it with `flutter doctor`. Install dependencies/download assets, then choose your target platform:

```sh
flutter doctor
(cd app && flutter pub get)
python3 scripts/setup_client_voice.py
```

Android：准备 Android SDK/JDK，连接手机前开启开发者选项与 USB 调试，并在手机确认电脑调试授权。

Android: prepare Android SDK/JDK, enable developer options and USB debugging, and accept the computer's debugging authorization on the phone.

```sh
(cd app && flutter build apk --release --split-per-abi)
```

iOS：在 macOS 安装 Xcode/CocoaPods；以下仅编译，真机需在 Xcode 打开 `app/ios/Runner.xcworkspace` 配置自己的 Team、Bundle ID、签名和设备，再安装。首次构建后才能打开生成的 workspace。

iOS: use macOS with Xcode/CocoaPods. This only compiles; open `app/ios/Runner.xcworkspace` after the initial build, configure your Team, Bundle ID, signing and device, then install.

```sh
(cd app && flutter build ios --release --no-codesign)
```

开发调试可选择已连接设备运行：

For development, select a connected device:

```sh
(cd app && flutter devices)
(cd app && flutter run -d YOUR_DEVICE_ID)
```

`setup_client_voice.py` 下载并校验固定版本的手机端语音模型，首次约需 300 MB 模型资源；后续复用已校验文件。模型随 App 打包，日常识别不需要工作站 Whisper 服务。Go 使用临时文件构建再替换，避免覆盖正在运行的 macOS 可执行文件；替换后仍需重启服务才能运行新版本。

`setup_client_voice.py` downloads and verifies pinned on-device speech models, requiring roughly 300 MB of model assets initially; validated files are reused. Models ship with the app, so normal recognition needs no workstation Whisper service. Build Go to a temporary file before replacement to avoid overwriting a running macOS executable; restart the service to run the new binary.

Android arm64 APK 输出在 `app/build/app/outputs/flutter-apk/app-arm64-v8a-release.apk`。当前 release 使用开发测试签名，不是商店发布签名。`artifacts/` 是本地交付目录，不纳入 Git；新检出需自行构建。

The Android arm64 APK is written to `app/build/app/outputs/flutter-apk/app-arm64-v8a-release.apk`. Current release builds use a development test signature, not a store release signature. `artifacts/` contains local deliverables and is excluded from Git; build it after a fresh checkout.

```sh
adb devices -l
adb -s DEVICE_SERIAL install -r app/build/app/outputs/flutter-apk/app-arm64-v8a-release.apk
```

## 连接并开始会话 / Connect and start a conversation

### 首页与项目 / Home and projects

配对成功后，首页选择 **Codex / Claude Code / OpenCode / Qoder / TRAE / WorkBuddy**。右上角 **Agent 显示设置** 勾选要显示的 Agent；隐藏只改变本手机入口，不停止服务、不撤销授权、不删除历史。

After pairing, select **Codex / Claude Code / OpenCode / Qoder / TRAE / WorkBuddy** on Home. Use **Agent 显示设置 (Agent display settings)** at the top right to choose visible agents. Hiding only changes this phone's navigation; it does not stop an integration, revoke access or delete history.

- **项目**：按当前 Agent 的工作目录分组；点击文件夹标题折叠/展开。每组先显示 5 条，点“展开显示”查看更多。 / **Projects:** group the selected agent's chats by working directory. Tap a folder header to fold/unfold; expand beyond the initial five chats.
- **最新会话**：当前 Agent 下跨项目按更新时间排列。隐藏的项目仍可从这里进入。 / **Recent chats:** sort the selected agent's chats across projects by update time, including hidden projects.
- **项目管理**：勾选显示/隐藏、全部显示/隐藏；拖动把手或点上下箭头排序，可恢复默认。每个 Agent 的排序、折叠和显示偏好分别加密保存。 / **Project management:** toggle visibility, show/hide all, drag or use arrows to reorder, and reset ordering. Each agent's order, folding and visibility are saved separately in encrypted storage.
- **新建会话**：项目右侧 **+**，或首页右上角新建入口，选择已配置且支持创建的 Agent 与项目。项目目录必须由 Local 的 `--project-roots` 提供；WorkBuddy 当前只有一个本地助理，没有项目新建入口。 / **New chat:** use a project's **+** or Home's create control, then choose a configured agent and allowed project. Local publishes projects through `--project-roots`; WorkBuddy has one Local Assistant without project creation.

### 发送、停止与审批 / Send, stop and approve

打开会话，在底部输入文字并点发送。输入框的转圈表示发送/连接阶段，列表或会话的执行圆圈表示 Agent 正在运行；“已发送”表示请求已接受，**不等于已经回复或逐 token 同步**。正常回复追加在当前会话。支持中断的 Local 任务可点停止；收到工具审批时查看内容后选允许或拒绝。电脑 Codex 原会话的停止和审批目前在电脑处理。

Open a chat, type in the bottom composer and tap Send. A composer spinner indicates sending/connecting; the chat/list activity indicator means the agent is running. Accepted **does not mean a reply has arrived or native token streaming is active**. Replies append to the chat. Stop supported Local-managed turns, and review tool requests before allowing or declining them. Original desktop Codex turns currently require desktop interruption and approval.

模型与思考强度只在接入实际提供能力时可选；电脑原会话沿用桌面设置。思考和工具块可展开查看；只展示 Agent 公开输出的内容。发送后连接中断时先查看会话结果，再决定是否重发，避免重复执行。

Model and reasoning controls follow the integration's actual capabilities; original desktop chats retain desktop settings. Expand reasoning/tool blocks to inspect exposed output. After a disconnect, inspect the result before resending to avoid duplicate execution.

### 长会话与对话目录 / Long chats and the conversation directory

进入会话先显示最近最多 40 个展示块，向上翻或点“加载更早消息”分页。右上角 **对话目录** 列出全部**已导入**用户消息、Agent 回复和图片摘要，包括尚未加载的正文；搜索摘要并点选可直接跳转、高亮目标，不必下载中间所有消息。目录内刷新可取最新摘要；浏览旧记录时新输出不强制滚到底部，点 **回到最新消息** 返回。默认源日志只导入最近 72 小时修改文件的尾部 8 MiB，目录不会包含未导入内容；全量导入方法见日志同步。

Chats initially show at most 40 recent items; scroll upward or load earlier messages for more. **对话目录 (Conversation directory)** lists all **imported** user messages, agent replies and image previews, including unloaded bodies. Search and tap to jump and highlight a record without downloading intervening messages. Refresh updates the directory; new output preserves historical reading, while **回到最新消息 (Jump to latest)** returns to recent output. Default source import covers the last 8 MiB of files modified within 72 hours; unimported source history is absent. See log synchronization for full import.

### 图片、链接与文件 / Images, links and files

消息内图片点开可缩放；右上角 **会话附件** 集中列出当前已加载历史中的图片、文件和链接。HTTP/HTTPS 网页在手机浏览器打开，本机图片与 UTF-8 文本/源码经加密连接预览并支持行号；PDF、SVG、音视频等尚不提供内置预览。工作站文件必须仍存在、被当前会话引用，且位于 `--media-roots` 允许目录内；文本最多 1 MiB，单图最多 8 MiB。

Tap images to zoom. **会话附件 (Chat attachments)** lists images, files and links in currently loaded history. Web links open in the phone browser; workstation images and UTF-8 text/source preview over the encrypted connection with line numbers. PDF, SVG and audio/video lack built-in previews. Workstation files must exist, be referenced by the current chat and lie within `--media-roots`; text is limited to 1 MiB and images to 8 MiB.

例如“清理明细”指向 `/private/tmp/cleanup-details.json`，应在现有 Local 启动命令中设置 `--media-roots "$HOME/workspace,/private/tmp"`，然后重启同一数据目录的服务。macOS 每用户临时目录（`$TMPDIR`）和 `/private/tmp` 不同；截图在哪个目录，就授权对应目录。不要把 `/` 当附件根目录；已删除的临时文件无法预览。

If a Cleanup details link points to `/private/tmp/cleanup-details.json`, add `--media-roots "$HOME/workspace,/private/tmp"` to the existing Local command and restart with the same data directory. macOS's per-user `$TMPDIR` differs from `/private/tmp`; allow the directory that actually contains the screenshot. Do not use `/` as an attachment root. Deleted temporary files cannot be previewed.

输入区 **＋ → 照片 / 拍照 / 文件** 在手机本地选择，发送前可移除附件。点击发送后才校验 Agent 的图片输入能力，不支持时保留草稿。普通文件上传至 Local 的加密附件库，Agent 通过本机只读引用读取；发给 Agent 的内容仍受该 Agent 自身服务与模型的数据策略约束。

Use composer **+ → Photo / Camera / File** to select locally and remove attachments before sending. Image capability is checked after Send; unsupported input preserves the draft. General files upload into Local's encrypted attachment store and are exposed to the agent through owner-only read references. Content supplied to an agent remains subject to that agent/model service's data handling.

### 手机语音输入 / On-device voice input

点麦克风切换 **按住说话**，首次允许麦克风权限；长按期间持续出字，停顿或松开后进行整句校正，上滑取消。识别结果进入文字草稿，可修改后**手动发送**。首次加载模型和校正会等待，技术名词可能识别错误。当前默认音频只在手机内存中处理、不上传，不需要配置工作站 Whisper；旧版工作站转写属于兼容路径，见 [输入能力说明](docs/INPUTS.md)。

Tap the microphone for **Hold to speak** and grant microphone access initially. Hold for live text, pause/release for utterance correction, or slide upward to cancel. Edit the text draft and **send explicitly**. Initial model loading and correction take time; technical terms may be wrong. The current default processes audio only in phone memory without upload and needs no workstation Whisper setup. Legacy workstation transcription is a compatibility path described in [input capabilities](docs/INPUTS.md).

当前 App 文案以中文为主；此 README 中英对照不表示 App 已有英文界面。

The app's current UI is primarily Chinese; this bilingual README does not imply an English app localization.

## 直连工作站 / Connect directly

### 本地开发与 USB 手机 / Local development and a USB phone

在工作站的终端 A 启动并保持运行。先将 `$HOME/workspace` 换成真实存在的项目父目录；来源目录必须显式配置，不传来源时不读取 Agent 日志。

Start the loopback TLS service in workstation terminal A and leave it running. Replace `$HOME/workspace` with an existing project parent directory. Source directories are opt-in: no agent logs are read unless explicitly configured.

```sh
./artifacts/agentdeck serve \
  --data "$HOME/.agentdeck" \
  --listen 127.0.0.1:8443 \
  --public-url https://localhost:8443 \
  --codex-sessions "$HOME/.codex/sessions" \
  --claude-sessions "$HOME/.claude/projects" \
  --project-roots "$HOME/workspace" \
  --media-roots "$HOME/workspace"
```

手机上的 `localhost` 是手机自己；Android USB 调试可用下列反向隧道访问工作站。普通 Wi-Fi/移动网络请使用下节公网方式。

On a phone, `localhost` refers to the phone. For Android USB debugging, use this reverse tunnel to reach the workstation. Use the public deployment below for normal Wi-Fi/mobile access.

```sh
adb devices -l
adb -s DEVICE_SERIAL reverse tcp:8443 tcp:8443
```

服务启动后，在终端 B 用同一系统用户、同一数据目录生成一次性配对码。手动打开电脑上的 PNG；手机在配对页选“扫描配对码”，或“粘贴配对串或 JSON”。`pair` 生成文件，不会自动展示图片。

After the service starts, generate a one-time pairing code in terminal B, using the same OS user and data directory. Display the PNG on the computer, then use the app's Scan pairing code or Paste pairing string/JSON option. The pair command creates files; it does not display the QR image automatically.

```sh
./artifacts/agentdeck pair --data "$HOME/.agentdeck" --out /tmp/agentdeck-pair
```

`/tmp/agentdeck-pair.json` 和 `.png` 为 0600 文件、5 分钟有效、单次使用。把它们当作访问授权，配对后删除，不上传仓库或发给其他人。完成开发体验后，可移除 USB 隧道：

The JSON and PNG have mode 0600, expire after five minutes, and are single-use. Treat them as access authorization: delete them after pairing and do not upload or share them. Remove the USB tunnel after testing:

```sh
adb -s DEVICE_SERIAL reverse --remove tcp:8443
```

成功标准：手机显示已连接，选择真实 Agent 后能看到其已导入会话，发送项目内请求能得到真实回复。拔 USB 或电脑休眠会断开；重连 USB 后重新执行 `adb reverse`，已保存的配对不需要重新生成。此 USB 方法只适用于 Android，iOS 使用公网 HTTPS 方式。

Success means the phone is connected, imported real chats are visible and an allowed-project prompt receives a real reply. USB removal or workstation sleep disconnects it. Reapply `adb reverse` after reconnecting USB; preserve the existing pairing. This USB method is Android-only; use public HTTPS for iOS.


### 公网 Envoy 终止 TLS / Public access with Envoy TLS termination

证书留在 Envoy；Go 不需要公网证书或私钥。替换示例域名，配置现有 Envoy 的 routes/cluster，使用 [参考配置](docs/deploy/envoy.yaml)。

Keep the certificate in Envoy; Go does not need the public certificate or private key. Replace the example domain and configure routes/clusters in the existing Envoy using the [reference configuration](docs/deploy/envoy.yaml).

```sh
./artifacts/agentdeck serve \
  --behind-proxy --listen 127.0.0.1:8080 \
  --public-url https://agent.example.com \
  --data "$HOME/.agentdeck" \
  --codex-sessions "$HOME/.codex/sessions" \
  --claude-sessions "$HOME/.claude/projects" \
  --project-roots "$HOME/workspace" \
  --media-roots "$HOME/workspace" --rotation 15m
```

默认 App 校验公网 CA、域名和有效期，Envoy 正常续证或换私钥无需重配。Go 上游只允许回环或受限私网；跨不可信网络需额外 mTLS/安全隧道。直连公开路径为 `/healthz`、`/v1/pair`、`/v1/session`、`/v1/time-sync` 和 `/v1/stream`，WebSocket route 关闭总时限。不要记录认证 header、Cookie、请求体或 OAuth callback 查询参数。

By default, the app validates the public CA chain, hostname, and certificate lifetime; normal Envoy certificate/key renewal does not require pairing again. Restrict the Go upstream to loopback or a controlled private network; use additional mTLS/a secure tunnel across untrusted networks. Direct public routes are `/healthz`, `/v1/pair`, `/v1/session`, `/v1/time-sync`, and `/v1/stream`. Disable the total timeout on WebSocket routes. Do not log authentication headers, cookies, request bodies, or OAuth callback query parameters.

先验证 `curl --fail https://agent.example.com/healthz`，再按 USB 章节的 `pair` 命令生成配对码并在手机扫描；此时不需要 `adb reverse`。健康检查成功只证明 HTTPS 路由可达，还需实际配对并发送验证。不要将 USB 的 `localhost` 配对串用于公网部署。

First check `curl --fail https://agent.example.com/healthz`, then run the same `pair` command described for USB and scan on the phone; no `adb reverse` is needed. A health response proves HTTPS routing only; verify pairing and a real prompt separately. Do not reuse a USB `localhost` invitation for public deployment.

也支持 Go 直接 TLS：传 `--cert`/`--key` 并使用 HTTPS 公网 origin。未指定证书时只允许回环自签测试。直接 TLS 默认绑定 SPKI；换私钥需重配。完整说明见 [部署指南](docs/deploy/README.md)。

Go can also terminate TLS directly using `--cert`/`--key` and a public HTTPS origin. Without a certificate, only loopback self-signed testing is allowed. Direct TLS pins the SPKI by default; changing the private key requires pairing again. See the [deployment guide](docs/deploy/README.md).

## 飞书网关模式 / Feishu gateway mode

### 0. 申请真实飞书应用 / Register a real Feishu application

**必须先申请并配置飞书应用。扫描任意二维码不会让网关自动取得用户身份。** 部署者在飞书开放平台创建应用，启用网页登录能力，配置主页/登录入口、应用可用范围及登录所需身份权限，完成对应的审核/发布流程。网页登录回调须精确设置为 `https://agent.example.com/v2/auth/callback`，取得 App ID 与 App Secret。普通使用者只需在已可用的应用中登录，不必每人申请应用。多企业使用还取决于应用类型及企业安装/授权，不能只靠网关支持多租户。参考 [飞书官方扫码登录方案](https://open.feishu.cn/solutions/detail/qrcode?lang=zh-CN)。

**A registered, configured Feishu application is required. Scanning an arbitrary QR code does not reveal identity to the gateway.** The operator creates an app, enables web login, configures entry URLs, availability and required identity permissions, and completes applicable review/publication. Register the exact callback `https://agent.example.com/v2/auth/callback` and obtain the App ID/Secret. Users log into the available application rather than registering individual apps. Cross-enterprise use also depends on app type and installation/authorization. See the [official Feishu QR-login solution](https://open.feishu.cn/solutions/detail/qrcode?lang=zh-CN).

实际流程为：Local 请求登录 → 网关返回授权页面/二维码 → 用户飞书扫码并确认 → 飞书回调带回 code/state → 网关用应用凭据换取用户令牌并读取用户信息 → Local 取得加密关联凭据。网关使用返回的 `tenant_key + open_id` 归属工作站；不会通过姓名猜测用户。未配置真实应用时，这条流程不能完成。

The actual flow is Local login request → Gateway authorization page/QR → user scans and consents → Feishu callback with code/state → Gateway exchanges the code with app credentials and retrieves user information → encrypted Local enrollment. Workstations belong to the returned `tenant_key + open_id`, not a guessed display name. This cannot complete without a real application.

### 1. 配置并启动网关 / Configure and start the gateway

工作站和 App 使用**同一个飞书应用**登录。配置网页登录回调为 `https://agent.example.com/v2/auth/callback`，设置应用可用范围与所需身份权限。跨企业可用性由飞书应用类型、安装和授权范围决定；网关租户隔离不会自动扩大应用范围。

The workstation and app authenticate through the **same Feishu application**. Configure the web-login callback as `https://agent.example.com/v2/auth/callback`, along with application availability and required identity permissions. Cross-enterprise availability depends on Feishu application type, installation, and authorization; gateway tenant isolation does not expand that availability.

| 环境变量 / Variable | 用途 / Purpose |
| --- | --- |
| `FEISHU_APP_ID` | 应用 ID，必须与 Secret 一起配置。 / Application ID; must be configured with the secret. |
| `FEISHU_APP_SECRET` | 仅放网关 secret 环境，不传 Local/App。 / Gateway-only secret; never sent to Local/App. |
| `FEISHU_OAUTH_SCOPE` | 可选，使用已获批范围。 / Optional; use approved scopes. |
| `FEISHU_ALLOWED_TENANTS` | 可选，逗号分隔 tenant_key 白名单。 / Optional comma-separated tenant-key allowlist. |

通过 service manager 的 EnvironmentFile/secret 注入配置。示例文件见 [gateway.env.example](docs/deploy/gateway.env.example)，真实配置权限 0600，不能提交 Git。没有配置 App ID/Secret 时，飞书登录返回 503。

Inject configuration through the service manager’s EnvironmentFile/secret mechanism. Use [gateway.env.example](docs/deploy/gateway.env.example) as a template, keep real configuration at mode 0600, and do not commit it. Feishu login returns 503 when the application ID/secret is missing.

首次准备环境文件；已有文件只编辑，不覆盖。填好真实值后，在启动网关的终端加载。文件内容使用 shell `KEY=value` 语法；密钥仅放文件/服务管理器，不写进下面的启动参数。

Prepare the environment file once; edit rather than overwrite existing credentials. Fill real values, then load it in the Gateway terminal. Use shell `KEY=value` syntax; keep secrets in the file/service manager rather than startup arguments.

```sh
mkdir -p "$HOME/.config/agentdeck"
test -e "$HOME/.config/agentdeck/gateway.env" || \
  install -m 600 docs/deploy/gateway.env.example "$HOME/.config/agentdeck/gateway.env"
# Edit gateway.env with your editor before continuing.
set -a
. "$HOME/.config/agentdeck/gateway.env"
set +a
```

```sh
./artifacts/agentdeck serve --role gateway \
  --behind-proxy --listen 127.0.0.1:8080 \
  --public-url https://agent.example.com \
  --data "$HOME/.agentdeck-gateway" --rotation 15m
```

Envoy 需要转发 `/v2/auth/*`、`/v2/account/*`、`/v2/local/tunnel`、`/v2/local/grants`、`/v2/time-sync` 和 `/v2/relay/*`。Local tunnel 和 relay 的 WebSocket upgrade/长连接配置见 [envoy.yaml](docs/deploy/envoy.yaml)。网关不导入任何 agent 来源；网关与 Local 必须使用不同数据目录。

Envoy must forward `/v2/auth/*`, `/v2/account/*`, `/v2/local/tunnel`, `/v2/local/grants`, `/v2/time-sync`, and `/v2/relay/*`. See [envoy.yaml](docs/deploy/envoy.yaml) for tunnel/relay WebSocket upgrade and long-connection settings. The gateway imports no agent sources; Gateway and Local must use separate data directories.

### 2. 工作站扫码关联 / Associate the workstation

先停止该 Local 数据目录的服务，再执行登录。命令打开浏览器页面，用飞书扫码或授权确认；`--no-open` 可只打印登录页地址。

Stop the service using that Local data directory before logging in. The command opens a browser page for Feishu scanning/authorization; `--no-open` prints the page URL without opening it.

```sh
./artifacts/agentdeck local-login \
  --gateway-url https://agent.example.com \
  --name 'My Workstation' --data "$HOME/.agentdeck"
```

关联凭据加密交付并存进 Local 加密库。已有关联不会被覆盖；重启使用原凭据。然后启动只主动连出的 Local：

Enrollment credentials are delivered encrypted and saved in the Local encrypted store. Existing associations are protected against overwriting and reused after restart. Then start the outbound-only Local:

```sh
./artifacts/agentdeck serve --relay --data "$HOME/.agentdeck" \
  --codex-sessions "$HOME/.codex/sessions" \
  --claude-sessions "$HOME/.claude/projects" \
  --project-roots "$HOME/workspace" \
  --media-roots "$HOME/workspace" --rotation 15m
```

### 3. App 登录并校验 / Log in and verify from the app

在 App 选择 **“通过网关 · 飞书登录”**，输入同一 HTTPS origin，用**同租户、同一飞书账号**授权。网关按 `tenant_key + open_id` 匹配，选择在线工作站。同一手机可点授权链接，不必扫描自己的屏幕。

Choose **“通过网关 · 飞书登录” (Connect through gateway · Feishu login)** in the app, enter the same HTTPS origin, and authorize with the **same Feishu account in the same tenant**. The gateway matches `tenant_key + open_id`; select an online workstation. On the same phone, use the authorization link instead of scanning its own screen.

首次配对时，App 显示 12 位十六进制校验码；在可信工作站终端查看待处理请求，**逐字核对一致后**才批准：

During first pairing, the app displays a 12-hex-digit comparison code. View pending requests on a trusted workstation terminal and approve **only after verifying an exact match**:

```sh
./artifacts/agentdeck pair-pending --data "$HOME/.agentdeck"
./artifacts/agentdeck pair-approve --data "$HOME/.agentdeck" \
  --request GRANT_ID --code xxxx-xxxx-xxxx
```

手机点“两边一致，等待工作站确认”。飞书解决身份归属，48-bit SAS 防止中继替换 DH 公钥；网关不能代替工作站确认。以后重启/重连沿用设备 root，不必每次扫码。账号凭据 30 天，用于发现/新绑定；已配对 grant 独立有效至撤销。

On the phone, select “两边一致，等待工作站确认” (Codes match; wait for workstation approval). Feishu establishes ownership; the 48-bit SAS prevents relay substitution of DH public keys. The gateway cannot approve on behalf of the workstation. Device roots survive restarts/reconnections, so scanning is not repeated for each connection. Account credentials last 30 days for discovery/new bindings; paired grants remain independently valid until revoked.

网关模式成功标准：Local 登录完成并保持隧道在线 → 手机同账号能列出该工作站 → 两边 SAS 一致并批准 → 手机读到真实会话并发送成功。仅有二维码、飞书成功页或在线工作站列表都不等于完成设备配对。当前仓库已实现并用合成服务测试登录协议；真实飞书应用、公网域名与租户的端到端验收仍需部署者实际完成，不能把模拟测试当真实扫码成功。

Gateway success requires Local enrollment and an online tunnel → same-account workstation discovery on the phone → matching and approved SAS → real chat reads and successful sending. A QR code, login success page or online listing alone does not establish pairing. The repository implements and tests the login protocol with synthetic services; real Feishu/domain/tenant end-to-end acceptance still requires operator verification.

详细流程与 Linux 用户级 systemd 模板： [网关指南](docs/GATEWAY.md)、[Gateway service](docs/deploy/agentdeck-gateway.service)、[Local service](docs/deploy/agentdeck-relay-local.service)。

Detailed flow and Linux user-level systemd templates: [gateway guide](docs/GATEWAY.md), [Gateway service](docs/deploy/agentdeck-gateway.service), and [Local service](docs/deploy/agentdeck-relay-local.service).

## 项目、发送与续聊 / Projects, sending and continuation

后端启动时添加 `--project-roots /absolute/workspace`，将该目录、一级子目录以及范围内已导入会话的工作目录提供给手机。路径经符号链接解析后检查范围；App 只能选择后端发布的项目 ID。先在工作站安装并登录 `codex`、`claude` CLI；运行账户使用已有 CLI 账号和模型配置。

Start S-LOCAL with `--project-roots /absolute/workspace`. It lists that directory, its immediate children, and imported conversation working directories inside the allowlist. Paths are checked after resolving symlinks; the app selects server-issued project IDs. Install and sign in to the `codex` and `claude` CLIs on the workstation. Sessions use the existing CLI account and model configuration.

```bash
./artifacts/agentdeck serve \
  --data ~/.agentdeck \
  --listen 127.0.0.1:8443 \
  --public-url https://localhost:8443 \
  --project-roots "$PWD" \
  --codex-sessions ~/.codex/sessions \
  --claude-sessions ~/.claude/projects
```

手机选择项目，点击 **+ → Codex / Claude → 创建**，发送真实消息。开启 codex-desktop-socket 后，电脑 Codex 原会话的发送交给桌面原执行端，保持同一个 ID；需在电脑保持原会话打开。未开启该接口的 Codex 和 Claude 通过分叉复制上下文，界面会明确说明，并将分支标记出来。手机创建的会话直接续聊，后端重启后按持久 ID 恢复。Local 管理的任务可在手机停止及允许/拒绝工具审批；桌面原会话目前在电脑处理这些操作。

Tap the **+** beside a project folder, choose **Codex / Claude → Create**, and send a message. With codex-desktop-socket enabled, sending to a desktop Codex chat delegates to its original executor with the same ID; keep that chat open on desktop. Codex without this interface and Claude copy context into an explicitly described and labeled mobile branch. Mobile-created chats continue directly and resume by persisted ID after backend restart. Local-managed tasks support phone interruption and explicit Allow/Decline approvals; original desktop chats currently handle those actions on the computer.

操作、审批、项目路径和事件全部在原有 Local/App AEAD 加密帧中传输，S-GATEWAY 仅转发。请求标识用于去重；连接中断或后端崩溃导致结果不明时，不会自动重放任务，先检查会话结果再发送。标题和正文过滤已知内部上下文标签，正常代码、HTML、Markdown 保留；升级会修复已加密保存的展示数据并保留配对根密钥。

Commands, approvals, project paths and events travel inside the existing Local/App AEAD frames; S-GATEWAY forwards them. Request IDs deduplicate operations. Ambiguous outcomes after a disconnect or backend crash are not automatically replayed; inspect the conversation before sending again. Known internal context tags are removed from titles and messages while ordinary code, HTML and Markdown remain. Upgrade repairs encrypted presentation data and preserves pairing roots.


## 接入 Codex 与 Claude / Connect Codex and Claude

### 安装与登录 / Install and sign in

先按各 Agent 的官方指南安装 CLI，在**运行 Local 的同一系统用户**下完成登录，并确认普通终端内能正常访问模型。桌面 App 已登录不代表服务进程一定能找到 CLI 或其凭据；服务管理器通常需要显式配置 PATH。下面只做登录与状态检查，不运行任务。

Install each CLI using its official guide, sign in as **the same OS user running Local**, and verify model access in a normal terminal. Desktop login does not guarantee that a service can find the CLI or credentials; service managers often need an explicit PATH. These commands handle login/status without starting a task.

```sh
command -v codex
codex login
codex login status
command -v claude
command claude auth login
command claude auth status
```

Codex/Claude 不需要在 `agents.json` 中配置块；Local 使用现有 CLI 配置。`--codex-sessions` 与 `--claude-sessions` 负责导入历史，`--project-roots` 负责允许手机发送/新建；这两项不能互相替代。`command claude` 避免当前交互 shell 的别名改变登录命令。

Codex/Claude require no `agents.json` sections; Local uses their existing CLI configuration. Source flags import history, while `--project-roots` permits mobile sending/creation; neither substitutes for the other. `command claude` avoids interactive-shell aliases changing the login command.

Claude 原生版升级到最新发布版可运行 `command claude install latest`，再用 `command claude --version` 核对。如果同时存在 npm 和原生安装，须确认 PATH 优先命中新版；旧 zsh 终端可先执行 `rehash`。Local 模型目录缓存最多 5 分钟，过期后关闭并重新打开手机模型选择页。Fable 等新增模型以 CLI 实际返回为准，列出模型不代表已验证账号的调用权限。

For native Claude upgrades, run `command claude install latest` and verify with `command claude --version`. With both npm and native installations, ensure PATH selects the new executable; run `rehash` in an existing zsh terminal if needed. Local caches model catalogs for up to five minutes; reopen the phone's model picker after expiry. New models such as Fable follow the CLI's actual catalog; listing does not prove account inference access.

### 电脑 Codex 同一会话发送 / Send to the same desktop Codex chat

S-LOCAL 可显式开启 `--codex-desktop-socket "$HOME/.codex/ipc/ipc.sock"`，连接本机运行中的 Codex 桌面端。该目录与 Unix socket 必须属于当前用户且只有该用户可访问。选中已在桌面打开的原会话，手机发送会保持原 ID，通过本机协作协议交给原执行端；不再复制历史或默认创建同标题副本。桌面未运行、原会话未打开或协议不兼容时明确报错，不自动退回分叉。手机显示“连接电脑原会话 / 发送到电脑原会话 / 已发送”的不同阶段；旧分支标为“手机分支”。未配置该接口时，界面明确提示会复制上下文到手机分支。手机新建会话和 Claude 继续使用 Local 管理的 CLI。

S-LOCAL can opt into `--codex-desktop-socket "$HOME/.codex/ipc/ipc.sock"` to connect to the running local Codex desktop app. Its directory and Unix socket must belong to the current user with owner-only access. Sending to an original chat already open on desktop preserves its ID and delegates to its existing executor through the local coordination protocol. It no longer copies history or silently creates a same-title fork. An unavailable desktop, unopened chat or incompatible protocol reports an error without falling back to a fork. The phone distinguishes connecting, sending and accepted stages; older forks show Mobile branch. Without this interface, the UI explicitly describes copying context into a mobile branch. New mobile chats and Claude still use Local-managed CLIs.

该接口仅暴露已授权项目中会话的发送，手机不能指定任意桌面 RPC。保留桌面会话的模型与权限设置，使用授权项目目录，不停启桌面进程。桌面会话的停止与工具审批目前仍在电脑处理。该协作协议来自当前安装版本的接口检查，并非公开稳定 API；升级桌面后需重新验证。公网与 S-GATEWAY 仍走既有内层加密通信，工作站 Unix socket 不暴露公网。

The phone exposes only sending within authorized projects, not arbitrary desktop RPCs. Desktop model/permission settings are inherited, the authorized project directory is used and the desktop process is preserved. Stopping desktop turns and approving their tools currently remain desktop actions. This coordination protocol was inspected in the installed app and is not a public stable API; revalidate after desktop upgrades. Public and S-GATEWAY connections retain their inner encryption, and the workstation Unix socket is never exposed publicly.

将下列参数加入**现有 Local 启动命令**，不要另起一个占用相同数据库的服务：

Add this flag to the **existing Local startup command**, rather than starting a second service against the same database:

`--codex-desktop-socket "$HOME/.codex/ipc/ipc.sock"`（socket 必须存在且属于当前 owner / the socket must exist and belong to the current owner）

### 日志同步 / Log synchronization

通过 `--codex-sessions` / `--claude-sessions` 读取现有 JSONL。日志目录只读，500ms 轮询，新文件自动发现；WebSocket 最长再等待约 250ms，负载和网络延迟另计。默认导入最近 72 小时修改过的文件，每个新文件先读尾部 8 MiB；`--lookback 0 --history-bytes 0` 明确选择全量。

Use `--codex-sessions` / `--claude-sessions` to read existing JSONL files. Sources are read-only, polled every 500ms, with new-file discovery; WebSocket delivery can add roughly 250ms, plus load/network latency. Defaults import files modified within 72 hours and the last 8 MiB of each newly discovered file; `--lookback 0 --history-bytes 0` explicitly imports full history.

**源 agent 将内容 flush 到日志之后才可同步，未写出的 token 无法显示。** 日志格式是兼容适配，不是稳定公共协议；不自动旁接 Codex PC 已运行的私有连接。存在同 profile 的 session_index.jsonl 时，只更新已导入线程的 PC 标题。

**Synchronization starts after the source agent flushes content to its log; unlogged tokens cannot be displayed.** Log formats are compatibility adapters rather than stable public protocols. AgentDeck does not automatically attach to private connections of an already-running Codex desktop app. A matching profile’s session_index.jsonl only updates titles of threads already imported.

### stdout 事件桥接 / stdout event bridge

`bridge` 保留 agent stdin/stdout，通过本机 owner Unix socket 旁观 stdout JSON，不创建明文镜像日志。用于需要原生增量事件的宿主/CLI：

`bridge` preserves agent stdin/stdout and mirrors stdout JSON through the local owner Unix socket without creating plaintext mirror logs. Use it with hosts/CLIs that emit native incremental events:

```sh
# Claude Code
./artifacts/agentdeck bridge --data "$HOME/.agentdeck" --agent claude -- \
  claude -p 'Explain this project' \
  --output-format stream-json --verbose --include-partial-messages

# Codex App Server: replace the spawn command in your existing host
./artifacts/agentdeck bridge --data "$HOME/.agentdeck" --agent codex -- \
  codex app-server --listen stdio://

# Codex item events; not every item is a token-level delta
./artifacts/agentdeck bridge --data "$HOME/.agentdeck" --agent codex -- \
  codex exec --json 'Explain this project'
```

Codex App Server 的原宿主仍负责 initialize、thread/turn 生命周期、权限与审批；包装命令本身不会自动发起回合。Claude/Codex 只同步实际发出的事件。镜像失败会重试 3 次并报告 stderr，原 stdout 保留；漏掉的镜像依赖源日志补齐。避免同会话同时接入两种来源造成不同 source ID 的重复记录。

The existing Codex App Server host remains responsible for initialization, thread/turn lifecycle, permissions, and approvals; the wrapper alone does not start a turn. Only emitted events are mirrored. Mirroring failures are retried three times and reported on stderr while preserving original stdout; source logs must fill any gaps. Avoid importing the same session through both sources, which can produce duplicate records with different source IDs.

## 多 Agent 接入与显示设置 / Multi-agent integration and visibility

### 能力与前提 / Capabilities and prerequisites

六种 Agent 可独立启用，在首页或设备页的 **Agent 显示设置** 中勾选入口。安装、登录、服务在线和 AgentDeck 配置是不同步骤；勾选显示不会替你安装或授权。

Enable each of the six agents independently and select its entry in **Agent 显示设置 (Agent display settings)** on Home or the device page. Installation, login, online service and AgentDeck configuration are separate steps; a visibility checkbox does not install or authorize an agent.

| Agent | 工作站前提 / Workstation prerequisite | 当前接入与操作 / Current adapter and operations |
| --- | --- | --- |
| Codex | 已安装并登录 CLI；导入历史需日志目录。 / Signed-in CLI; log source for imported history. | 日志或 stdout 桥接；Local 会话创建/发送/停止/审批。原桌面会话发送需显式 IPC，停止/审批留在桌面。 / Logs or stdout bridge; Local create/send/stop/approve. Original desktop sending requires opt-in IPC; stop/approve on desktop. |
| Claude Code | 已安装并登录 CLI；导入历史需日志目录。 / Signed-in CLI; log source for imported history. | 日志或 stdout 桥接；Local 创建/发送/停止/审批，旧电脑会话明确分支续聊。 / Logs or stdout bridge; Local create/send/stop/approve, with explicit branches for imported desktop chats. |
| OpenCode | 已配置可用模型的 OpenCode HTTP 服务。 / OpenCode HTTP server with a usable model provider. | REST/SSE；原 session ID 的查看、创建、发送、停止、审批，模型/Variant/原生图片。 / REST/SSE; native session view/create/send/stop/approve, models/variants/native images. |
| Qoder | 已登录官方 CLI，支持 ACP。 / Signed-in official CLI with ACP. | stdio ACP；创建、发送、停止和单次审批；历史恢复、模型/思考/图片取决于实际广告能力。 / stdio ACP; create/send/stop/one-time approvals, with history/models/thought/image support negotiated. |
| TRAE | 已登录官方 CLI，支持 ACP。 / Signed-in official CLI with ACP. | 同 Qoder；不自动读取 IDE 私有历史。 / Same ACP boundaries; no automatic private IDE history import. |
| WorkBuddy | 电脑本地助理已登录在线；已审核应用、可达回调及用户 OAuth。 / Online signed-in PC Local Assistant; approved app, reachable callback and user OAuth. | 官方 OpenAPI，一个授权账号的本地助理，发送与增量轮询；不提供项目新建、停止、通用审批、模型切换或图片输入。 / Official OpenAPI; one account's Local Assistant, send and incremental polling; no project creation, stop, general approvals, model switching or image input. |

语音先在手机转为可编辑文字，再按普通消息发送；普通文件使用加密附件只读引用。显示回复里的图片与**向模型发送图片**是不同能力。Qoder/TRAE 的 `session/list`、`session/load` 不可用时，不能据此声称能浏览并续聊 IDE 中全部旧会话。

Voice becomes an editable phone-side text draft before normal sending; general files use encrypted attachments and read references. Displaying an output image and **sending images to a model** are separate capabilities. Without ACP `session/list` and `session/load`, Qoder/TRAE cannot claim access to every historical IDE chat.

### 共享配置文件 / Shared configuration file

只为 OpenCode/Qoder/TRAE/WorkBuddy 使用 `agents.json`；Codex/Claude 走上一节的 CLI 配置。首次复制模板，已有文件保留并编辑；移除不用的块，填写真实路径/地址。配置须为普通文件、最大 64 KiB、权限 `0600`，不能是符号链接，不接受未知字段。密钥用环境变量引用，不放手机或 JSON。

Use `agents.json` for OpenCode/Qoder/TRAE/WorkBuddy; Codex/Claude use their CLI configuration above. Copy the template once, preserve/edit existing files, remove unused sections and fill actual paths/endpoints. It must be a regular non-symlink file of at most 64 KiB with mode `0600`; unknown fields are rejected. Reference secrets through environment variables rather than phone settings or JSON.

```sh
mkdir -p "$HOME/.agentdeck"
test -e "$HOME/.agentdeck/agents.json" || \
  install -m 600 docs/config/agents.example.json "$HOME/.agentdeck/agents.json"
chmod 600 "$HOME/.agentdeck/agents.json"
# Edit before use; then add the flag below to your existing Local serve command.
```

在现有直连或 relay Local 启动命令中加入 `--agents-config "$HOME/.agentdeck/agents.json"`，保留日志、项目、附件、代理和桌面 IPC 参数。修改文件/进程环境后重启 Local；同一数据库只运行一个 Local。此参数不用于 Gateway。配置模板中的 WorkBuddy 字段为空是待配置状态；OpenCode 模板启用了密码环境变量，需按下节处理后才能连接。

Add `--agents-config "$HOME/.agentdeck/agents.json"` to the existing direct or relay Local startup, retaining source/project/media/proxy/desktop IPC flags. Restart Local after configuration/environment changes; run only one Local per database. Gateway does not use this flag. Empty WorkBuddy fields mean unconfigured; the OpenCode template references a password environment variable that must be handled below.

### OpenCode：启动服务并接入 / Start and connect OpenCode

1. 按 [OpenCode 官方文档](https://dev.opencode.ai/docs/) 安装 CLI，在 Local 所用用户下执行 `opencode auth login` 配置模型服务凭据。确认 OpenCode 自己能调用模型；AgentDeck 不提供模型账号或额度。 / Install the CLI using the official documentation, then run `opencode auth login` as Local's OS user to configure a model provider. Verify OpenCode can use it; AgentDeck supplies no provider account or quota.
2. 启动工作站回环服务并保持运行： / Start the workstation loopback server and leave it running:

```sh
opencode serve --hostname 127.0.0.1 --port 4096
```

3. 最小回环配置如下，合并到 `agents.json`；如果仅启用 OpenCode，可以整个文件使用这一份： / Merge this minimal loopback configuration into `agents.json`, or use it as the whole file for OpenCode only:

```json
{
  "opencode": {
    "url": "http://127.0.0.1:4096"
  }
}
```

4. 如果服务启用 Basic 密码，JSON 添加 `"username": "opencode"`、`"password_env": "OPENCODE_SERVER_PASSWORD"`，并在 **OpenCode 和 Local 两个进程**的环境注入相同密码；自定义用户名也须一致。模板已包含这两项。无密码的现有回环服务应移除 `password_env`，不能留着一个未设置的变量。非回环 Provider URL 必须 HTTPS。 / For Basic authentication, add `username` and `password_env` as shown in the template and inject the same password into **both OpenCode and Local**; match customized usernames too. Remove `password_env` for an existing unauthenticated loopback service. Non-loopback provider URLs require HTTPS.
5. Local 加载配置后，在手机启用 OpenCode，选项目新建，发送请求并观察 SSE 输出、工具、完成状态；再发第二句确认续聊。OpenCode 原会话使用原 session ID 和 directory。若需电脑 TUI 同步操作同一服务，应将电脑客户端连接到这个服务；另起默认 TUI 可能使用另一个端口。 / Load the Local configuration, enable OpenCode on the phone, create in a project and verify SSE output/tools/completion, then send a second turn. Continuation preserves the original session ID and directory. Connect a desktop TUI to this same server when desired; a separately started default TUI may use another port.

服务器认证与 REST/SSE 行为参见 [OpenCode Server](https://dev.opencode.ai/docs/server/)。断线期间历史补取受 Provider 接口范围限制，Local 已保存的历史继续保留。

See [OpenCode Server](https://dev.opencode.ai/docs/server/) for server authentication and REST/SSE. Recovery of provider history after disconnection is bounded by its fetch API; history already stored in Local remains available.

### Qoder 与 TRAE：CLI 登录和 ACP / Qoder and TRAE CLI login and ACP

按 [Qoder CLI](https://docs.qoder.com/cli/acp) / [TRAE CLI](https://docs.trae.cn/cli_agent-client-protocol) 的官方安装说明安装，在 Local 所用系统用户下完成浏览器登录。电脑 IDE 登录与 CLI 登录不要混为一谈；没有可用账号、套餐或模型权限时应先在 CLI 解决。

Install the official CLIs using the [Qoder](https://docs.qoder.com/cli/acp) / [TRAE](https://docs.trae.cn/cli_agent-client-protocol) documentation and complete browser login as Local's OS user. IDE login and CLI login are distinct; resolve account, plan or model-access failures in the CLI first.

```sh
qoder login
traecli login
```

将已安装的可执行文件绝对路径写入配置，替换下面示例路径：

Configure the absolute paths to the installed executables, replacing these examples:

```json
{
  "qoder": {"command": "/absolute/path/to/qoder", "args": ["--acp"]},
  "trae": {"command": "/absolute/path/to/traecli", "args": ["acp", "serve"]}
}
```

Local 自动启动 stdio ACP 子进程，**不用另外运行一个 ACP TCP 服务**。保留默认权限，不加跳过审批参数。手机在相应 Agent 的项目下新建并发送；成功标准是收到真实模型回复，不是仅看到 CLI 版本或“已配置”。Agent 公布可恢复会话能力时才列举并加载原 ACP ID，恢复失败明确报错，不静默改成新会话。

Local spawns stdio ACP subprocesses automatically; **no separate ACP TCP server is needed**. Retain default permissions without bypass flags. Create/send in the agent's project on the phone; a real model reply establishes success rather than a CLI version or configured label. Native ACP sessions are listed/loaded only when advertised, and restore failures report errors without silently creating replacements.

工具请求由手机单次允许/拒绝；需要客户端执行终端时也须确认。AgentDeck 侧文件访问限定在当前项目，并检查符号链接；这不替代 Agent CLI 自身权限与沙箱。尚未完成真实账号登录和模型调用的接入应标为待验收。

Review one-time tool requests and client-executed terminal requests on the phone. AgentDeck-side file access is project-scoped with symlink checks; this does not replace CLI permissions/sandboxing. Integrations without a real account/model round trip remain unaccepted.

### WorkBuddy：申请应用、配置回调、用户授权 / Register, configure and authorize WorkBuddy

**只在电脑登录 WorkBuddy 不够，还需要开放平台应用和用户 OAuth。** 当前接入的是电脑的本地助理，不是 WorkBuddy 全部云任务或全部项目。飞书登录只用于 AgentDeck 工作站归属，不能代替 WorkBuddy 授权。

**Desktop WorkBuddy login alone is insufficient: an Open Platform application and user OAuth are also required.** This adapter exposes the PC Local Assistant rather than every WorkBuddy cloud task or project. Feishu establishes AgentDeck workstation ownership and does not authorize WorkBuddy.

1. 在电脑打开 WorkBuddy，用要授权的账号登录，并保持本地助理在线。 / Open WorkBuddy on the computer, sign into the intended account and keep Local Assistant online.
2. 在 [WorkBuddy 开放平台第三方应用](https://open.workbuddy.cn/docs/third-party-app) 创建应用，申请 `user.localassistant.readable` 与 `user.localassistant.invokable`，配置精确回调，完成审核并启用应用；仅创建应用/取得 ID 不代表已经可用。保存 client ID 与仅显示一次的 client secret。 / Register a third-party app, request the two Local Assistant scopes, register the exact callback and complete review/enablement. Obtaining an ID alone does not establish availability. Save the client ID and once-displayed client secret.
3. 回调示例为 `https://workstation.example.com/v1/agents/workbuddy/callback`。必须 HTTPS、无额外 query/fragment，并与平台注册值精确一致。Envoy 将此路径路由到 **S-LOCAL HTTP Handler**，不能发给 Gateway。 / Register an exact HTTPS callback such as the example, without query/fragment. Envoy must route this path to **S-LOCAL's HTTP handler**, rather than Gateway.
4. 合并以下配置，替换实际 client ID 和回调域名： / Merge this configuration and replace the client ID and callback domain:

```json
{
  "workbuddy": {
    "api": "https://www.workbuddy.cn/openapi/v2",
    "client_id": "YOUR_WORKBUDDY_CLIENT_ID",
    "client_secret_env": "AGENTDECK_WORKBUDDY_CLIENT_SECRET",
    "redirect_uri": "https://workstation.example.com/v1/agents/workbuddy/callback"
  }
}
```

5. 仅在 Local 的进程环境设置 `AGENTDECK_WORKBUDDY_CLIENT_SECRET`，推荐使用权限 `0600` 的环境文件或服务管理器。不要放进 JSON、手机或 Gateway。重启 Local 加载文件与环境，保留 `--agents-config` 及现有全部启动参数。 / Inject `AGENTDECK_WORKBUDDY_CLIENT_SECRET` into Local's process environment only, preferably through a mode-0600 environment file or service manager. Keep it out of JSON, phone and Gateway. Restart Local with its existing flags plus `--agents-config`.

首次创建 Local 的环境文件，在编辑器中填入 `AGENTDECK_WORKBUDDY_CLIENT_SECRET='实际 secret / actual secret'`（替换占位内容），然后在启动 Local 的同一个终端加载；已有文件只编辑。服务管理器则使用同一文件作为 EnvironmentFile。下面不启动第二个 Local：

Create a Local environment file once and set `AGENTDECK_WORKBUDDY_CLIENT_SECRET='actual secret'` in an editor, replacing the placeholder. Load it in the terminal that will start Local, or use it as the service manager's EnvironmentFile. Preserve existing files. This does not start a second Local:

```sh
mkdir -p "$HOME/.config/agentdeck"
test -e "$HOME/.config/agentdeck/local.env" || \
  install -m 600 /dev/null "$HOME/.config/agentdeck/local.env"
# Edit local.env before continuing.
set -a
. "$HOME/.config/agentdeck/local.env"
set +a
# Restart Local with your existing arguments and --agents-config.
```

6. 手机已连接后，进入 **Agent 显示设置 → 授权 WorkBuddy 本地助理**。浏览器跳转 WorkBuddy，使用电脑同一账号登录并同意权限；等回调页显示结果，再返回 App。 / On the connected phone, open **Agent display settings → Authorize WorkBuddy Local Assistant**. Sign in and consent in the browser with the PC's account; wait for the callback result, then return to the app.
7. 启用 WorkBuddy 入口，打开 **本地助理**，发送一条真实文字并确认电脑执行与手机回复。正常约每 2 秒增量轮询历史，**不是逐 token 流**；网络/官方服务错误时会退避。 / Enable WorkBuddy, open **Local Assistant**, send a real message and verify PC execution plus the phone reply. Normal history polling is approximately every two seconds, **not token streaming**, with backoff on failures.

**纯 relay 模式的回调限制：** `serve --relay` 不监听 TCP，当前 S-GATEWAY 也不代理 WorkBuddy OAuth callback。因此不能只填网关域名就完成授权；必须先提供实际可达的 Local callback。当前建议用“直连 Local + Envoy”完成 WorkBuddy 验收。严格只允许 Local 主动连出的部署，需要补齐回调支持后再启用此 OAuth 路径。

**Outbound-only relay limitation:** `serve --relay` has no TCP listener and S-GATEWAY currently does not proxy the WorkBuddy OAuth callback. A Gateway domain alone cannot complete authorization; an actually reachable Local callback is required. Validate WorkBuddy using direct Local behind Envoy first. Strict outbound-only deployments need additional callback support before enabling this OAuth path.

直连部署可在 [Envoy 参考配置](docs/deploy/envoy.yaml) 启用对应 callback route；若使用独立回调域名，也须配置该域名证书、virtual host 和正确 Local upstream。授权 state 10 分钟有效、单次使用；重启中断授权或过期时，从手机重新发起，不能重复打开旧回调。access/refresh token 加密存 Local，按有效期刷新；首次分页最多 10,000 条，超出明确报错，之后按账号独立保存增量游标。

For direct deployment, enable the callback route in the [Envoy reference](docs/deploy/envoy.yaml). A separate callback domain also needs its certificate, virtual host and correct Local upstream. OAuth state is single-use and valid for ten minutes; restart an interrupted/expired flow from the phone rather than reopening an old callback. Tokens are encrypted in Local and refreshed on expiry. Initial pagination caps at 10,000 messages with an explicit overflow error; subsequent cursors are account-isolated.

WorkBuddy 这一路经过官方服务；AgentDeck 的 Local/App 通道和缓存加密，并不意味着 WorkBuddy 官方服务无法读取其消息。应用权限和接口说明见 [WorkBuddy OpenAPI](https://open.workbuddy.cn/docs/openapi)。当前配置/授权入口与协议测试已实现，真实应用审批及真实账号端到端尚待验收。

This integration communicates through WorkBuddy's official service. AgentDeck channel/cache encryption does not prevent that service from reading its messages. See [WorkBuddy OpenAPI](https://open.workbuddy.cn/docs/openapi). Configuration, authorization entry points and protocol tests are implemented; real application approval and account end-to-end acceptance remain outstanding.

## 升级、重启与数据保留 / Upgrade, restart and retain data

同一 OS 用户使用同一 `--data`、Agent 配置与密钥环境重启；手机保留安全存储即可自动重连。以下是操作顺序，**不是删除/重建数据目录**：

Restart as the same OS user with the same data directory, agent configuration and secret environment; the phone reconnects using secure storage. Follow this sequence while **retaining the existing data directory**:

1. 先检查是否有任务正在执行，等完成或按实际能力停止；停止对应 Local/Gateway 服务。 / Check active turns, finish or stop them through supported controls, then stop the appropriate Local/Gateway service.
2. 备份完整数据目录及其 storage.key；备份也包含敏感凭据，须受限保存。不能只复制数据库丢掉 storage.key。 / Back up the complete data directory including storage.key in restricted storage. A database without its storage key is insufficient.
3. 按构建章节生成 `.next` 二进制再替换；使用原启动参数/服务管理器重启，重新注入环境文件。Gateway 与 Local 分别保留各自数据。 / Build to `.next`, replace, restart with the original flags/service manager and reload environment files. Preserve Gateway and Local data independently.
4. App 新包用相同签名覆盖安装（Android `adb install -r`）；不要先卸载或清除 App 数据。签名不匹配时先解决签名问题，否则会丢失手机配对和布局。 / Install the app over the existing version with the same signature (`adb install -r` on Android). Avoid uninstalling/clearing data; resolve signing differences first to retain pairing and layout.
5. 确认健康检查、手机连接、历史/图片以及一条真实消息；Gateway 另外确认隧道在线。服务退出后终端启动的进程不会自动恢复；长期运行使用服务管理器。 / Verify health, phone connection, history/images and a real message; additionally check the Gateway tunnel. Foreground terminal processes do not restart automatically; use a service manager for persistent operation.

Linux 用户级服务模板见 [Gateway service](docs/deploy/agentdeck-gateway.service) 和 [relay Local service](docs/deploy/agentdeck-relay-local.service)。复制到 `~/.config/systemd/user/` 前先编辑绝对路径、项目范围和环境文件；运行 `systemctl --user daemon-reload` 后再 enable/start 对应服务。同机直连应按自己的直连参数编写服务，不要误用 relay 模板。macOS 可使用 launchd，并保持相同用户、PATH、配置和数据路径。

For Linux user services, adapt the linked templates with absolute paths, allowed projects and environment files before installing under `~/.config/systemd/user/`. Reload the user manager before enabling/starting them. Direct mode needs direct-mode arguments rather than the relay template. macOS can use launchd with the same user, PATH, configuration and data paths.

## 加密与授权 / Encryption and authorization

| 项目 / Item | 实现与边界 / Implementation and boundary |
| --- | --- |
| 配对 / Pairing | 临时 X25519 + HKDF-SHA256；一次邀请或网关 SAS 确认。 / Ephemeral X25519 + HKDF-SHA256; a one-time invitation or gateway SAS confirmation. |
| 会话 / Sessions | AES-256-GCM；独立方向/epoch key、严格 seq、AAD 和重放校验。 / AES-256-GCM with directional/epoch keys, strict sequence numbers, AAD, and replay checks. |
| 轮换 / Rotation | 默认每 15 分钟更新 traffic key，每次重连随机 salt；长期 root 保留。 / Traffic keys rotate every 15 minutes by default; fresh salt per reconnection; long-term roots remain. |
| 时间基准 / Time authority | 直连以 S-LOCAL 为准；网关模式下 S-LOCAL 与 C-APP 均以 S-GATEWAY 为准。加密挑战校时后用单调时钟推进，重连与 App 恢复前台重新校准。 / Direct connections use S-LOCAL time; with a Gateway, both S-LOCAL and C-APP use S-GATEWAY time. An encrypted challenge authenticates calibration, then monotonic clocks advance it; reconnect and app resume recalibrate. |
| 网关 / Gateway | 仅持有身份/路由/enrollment key，无 Local/App 会话 root。 / Holds identity/routing/enrollment keys but no Local/App session roots. |
| 服务端存储 / Server storage | bbolt value 由独立 storage.key 加密，目录 0700、文件 0600。 / bbolt values encrypted by a separate storage key; directories 0700, files 0600. |
| 手机存储 / Phone storage | root/cache key 放 Keychain/Android secure storage，缓存为 AEAD 密文，图片仅在内存。 / Roots/cache keys in Keychain/Android secure storage; AEAD cache ciphertext; images only in memory. |
| 本机管理 / Local management | owner-only 0600 control.sock，不通过公网发布。 / Owner-only mode-0600 control.sock, never published publicly. |

网关能看见路由标识、时间、长度并影响可用性，但无法用 routing/enrollment key 解密会话。OAuth callback code/Cookie 依赖 HTTPS 与安全代理上游；不能把全部私网流量描述成独立端到端密文。长期 root 泄露后必须撤销重配；epoch 轮换**不是前向保密双棘轮**。storage.key 可被主机 owner 读取，因此不防御已取得 owner 权限的攻击者。

The gateway can observe routing IDs, timing, and lengths and affect availability, but cannot decrypt sessions with routing/enrollment keys. OAuth callback codes/cookies rely on HTTPS and a secure proxy upstream; not all private-hop traffic is independently end-to-end encrypted. Revoke and re-pair after root compromise; epoch rotation **is not a forward-secret double ratchet**. Host owners can read storage.key, so this does not defend against attackers with owner privileges.

不导入系统/开发者指令、内部上下文、认证事件、encrypted reasoning 内容；只显示 agent 公开输出的思考摘要。常见秘密字符串会脱敏，但无法保证识别所有业务秘密，手机仍须属于可信所有者。

System/developer instructions, internal context, authentication events, and encrypted reasoning content are excluded. Only publicly emitted reasoning summaries are shown. Common secret patterns are redacted, but arbitrary business secrets cannot be reliably detected; paired phones must still be trusted.

本地附件仅允许 `--media-roots` 中的普通文件，防路径/符号链接逃逸。链接预览还要求文件已经在当前会话中引用；敏感凭据文件拒绝预览。支持图片、文本/Markdown、Office 文档以及原生音视频预览，具体格式与限额见上文附件说明及 [Office 与文本预览](docs/OFFICE_PREVIEW.md)。附件在手机内存中解密，音视频通过带随机访问令牌的回环内存服务交给原生播放器，关闭时停止服务。截图位于系统临时目录时，需将对应目录显式加入允许范围；删除的临时文件不能恢复。后端不抓取远程 URL；公开 HTTPS 图片由手机直接读取（不携带配对密钥），网页点击打开手机浏览器。SVG 和未支持的二进制格式不能预览。

Local attachments are restricted to regular files under `--media-roots`, preventing path/symlink escapes. Linked previews also require a reference in the current chat; credential files are refused. Images, text/Markdown, Office documents and native audio/video previews are supported; formats and limits are described above and in [Office and text previews](docs/OFFICE_PREVIEW.md). Decrypted attachment bytes remain in phone memory; audio/video reaches the native player through a loopback memory server protected by a random access token, which stops on close. Screenshots in a system temporary directory require that directory to be explicitly allowed; deleted temporary files cannot be recovered. The backend does not fetch remote URLs. The phone reads public HTTPS images directly without pairing keys, and web links open its browser. SVG and unsupported binary formats cannot be previewed.

### 撤销访问 / Revoke access

App“忘记工作站”只清手机密钥、缓存与账号。取消服务器授权请在对应机器、同一个 `--data` 下执行：

The app’s “Forget workstation” clears phone keys, cache, and account state. Revoke server-side authorization on the corresponding machine using the same `--data` directory:

```sh
# On S-LOCAL
./artifacts/agentdeck devices --data "$HOME/.agentdeck"
./artifacts/agentdeck revoke --data "$HOME/.agentdeck" --device DEVICE_ID

# On S-GATEWAY
./artifacts/agentdeck gateway-locals --data "$HOME/.agentdeck-gateway"
./artifacts/agentdeck gateway-accounts --data "$HOME/.agentdeck-gateway"
./artifacts/agentdeck gateway-revoke --data "$HOME/.agentdeck-gateway" \
  --kind grant --request GRANT_ID
# --kind account cascades to grants; --kind local disconnects that workstation
```

grant 撤销只影响相应手机，account 撤销级联该登录发放的所有 grants，Local 撤销断开整个工作站。飞书离职/账号禁用事件目前不自动同步，管理员须显式撤销 AgentDeck 凭据。

Grant revocation affects the corresponding phone; account revocation cascades to all grants issued by that login; Local revocation disconnects the workstation. Feishu offboarding/account-disable events are not automatically synchronized; administrators must explicitly revoke AgentDeck credentials.

详见 [SECURITY.md](docs/SECURITY.md)。 / See [SECURITY.md](docs/SECURITY.md).

## 命令与配置 / Commands and configuration

| 命令 / Command | 用途 / Purpose |
| --- | --- |
| `serve` | 启动直连 Local；`--role gateway` 启动 Gateway；`--relay` 启动主动连出的 Local。 / Start a direct Local, a Gateway with `--role gateway`, or outbound Local with `--relay`. |
| `local-login` | 用飞书首次关联 Local。 / Initial Feishu Local association. |
| `pair` | 生成 owner 带外配对 JSON/PNG。 / Generate owner out-of-band pairing JSON/PNG. |
| `pair-pending` / `pair-approve` | 在 Local 查看/批准 SAS 请求。 / Inspect/approve SAS requests on Local. |
| `devices` / `revoke` | Local 枚举设备/删除设备 root。 / List Local devices/delete a device root. |
| `input-file` | Agent 读取上传附件的本机只读引用，由集成自动使用。 / Owner-only uploaded-file read helper used by integrations. |
| `bridge` | 旁观 agent stdout JSON。 / Mirror agent stdout JSON. |
| `gateway-locals` / `gateway-accounts` | Gateway 本机查看关联与登录，输出不含密钥。 / Inspect Gateway associations/logins locally without outputting keys. |
| `gateway-revoke` | 撤销 local、grant 或 account。 / Revoke a local, grant, or account. |
| `gateway-add-local` | 受控运维手工 enrollment，输出 owner-only JSON；日常推荐飞书流程。 / Manual operational enrollment producing owner-only JSON; prefer Feishu for regular use. |

| 参数 / Flag | 默认与用途 / Default and purpose |
| --- | --- |
| `--data` | `~/.agentdeck`；Gateway 建议独立目录。 / `~/.agentdeck`; use a separate Gateway directory. |
| `--listen` | `127.0.0.1:8443`；relay 模式不开放 TCP。 / `127.0.0.1:8443`; relay mode opens no TCP listener. |
| `--public-url` | `https://localhost:8443`；部署时设置实际 HTTPS origin。 / `https://localhost:8443`; set the real HTTPS origin for deployment. |
| `--behind-proxy` | 私网上游 HTTP/WS，不读取 Go TLS 私钥。 / Private upstream HTTP/WS, without reading Go TLS keys. |
| `--cert` / `--key` | Go 直接 TLS 的 PEM 文件。 / PEM files for Go-terminated TLS. |
| `--public-tls-pin` | 可选外部 SPKI SHA-256 hex；显式 pin 更换时需更新关联。 / Optional external SPKI SHA-256 hex; changing an explicit pin requires updating the association. |
| `--rotation` | `15m`；最短 `1s` 仅用于测试。 / `15m`; minimum `1s` is intended for tests. |
| `--codex-sessions` / `--claude-sessions` | 默认为空，不导入；显式配置源目录。 / Empty by default: no import; explicitly configure source directories. |
| `--project-roots` | 逗号分隔允许手机启动和续聊 agent 的项目目录，默认关闭。 / Comma-separated directories allowing mobile agent operations; disabled by default. |
| `--media-roots` | 逗号分隔允许的图片/会话附件目录，默认无本地 root。 / Comma-separated image/chat attachment directories; none allowed by default. |
| `--codex-desktop-socket` | 可选 owner-only 桌面 Unix socket，启用电脑 Codex 原会话发送，属于版本相关接口。 / Optional owner-only desktop socket for original-chat sending; version-dependent. |
| `--voice-command-file` | 旧版工作站转写的私有 argv 文件；当前手机端离线语音不需要。 / Private argv file for legacy workstation transcription; unnecessary for current on-device voice. |
| `--agents-config` | owner-only JSON：OpenCode 服务、Qoder/TRAE CLI、WorkBuddy 应用 metadata；密钥通过环境变量。 / Owner-only JSON for provider connections and application metadata; secrets use environment variables. |
| `--lookback` / `--history-bytes` | `72h` / `8388608`；`0` 选择完整范围。 / `72h` / `8388608`; `0` selects the full range. |

所有管理、配对和 bridge 命令必须与服务使用同一个数据目录。`--relay` 不能与 proxy/certificate flags 混用；Gateway 不接受 agent source/media 配置。`serve --help` 可查看完整参数。

Management, pairing, and bridge commands must use the same data directory as the service. `--relay` cannot be combined with proxy/certificate flags; Gateway rejects agent source/media configuration. Use `serve --help` for the complete flags.

## Codex 实时同步 / Codex live synchronization

0.4.11 起，配置桌面 IPC 后，打开原 Codex 会话即订阅桌面原生快照与增量补丁。手机收到发送确认后立即显示用户消息与“已送达 / 等待电脑响应”，原生执行状态到达后切换为转圈“执行中”，结束或失败后停止；项目、折叠项目、最新会话列表同步更新。手机发送后离开会话仍保留订阅，直到观察到执行结束或最长 30 分钟。

Since 0.4.11, an opened original Codex chat subscribes to native desktop snapshots and patches when IPC is configured. Acknowledged input appears immediately with Delivered / Waiting for desktop response. Native execution state starts the spinner; completion or failure stops it. Projects, collapsed projects and Recent chats update together. After a mobile send, subscription remains active after leaving the chat until observed completion or a 30-minute limit.

订阅只接受发现的 owner、原会话 ID 与 local host，校验 revision；断线或补丁缺口重新获取快照。文字更新合并约 80ms 后发布，手机加密通道按 250ms 周期推送（不含网络与设备耗时）。仅保存展示消息、公开思考摘要、工具和媒体；原始桌面上下文/设置/加密思考不落盘。源 JSONL 仍负责历史恢复，实时数据与日志对齐，避免重复消息和旧日志把结束状态改回执行中。

Subscriptions validate the discovered owner, original chat ID, local host and revision. Disconnects or patch gaps trigger a new snapshot. Text is coalesced for roughly 80ms and the encrypted phone channel checks updates every 250ms, excluding network/device latency. Only presentation messages, public reasoning summaries, tools and media are stored. Raw desktop context, settings and encrypted reasoning remain unpersisted. JSONL still recovers history, reconciled with native updates to prevent duplicate messages and stale running status.

当前兼容已安装桌面的 stream version 11；该接口不是公开稳定 API。未配置/不兼容时仍可从落盘日志同步，不能保证工具调用前更新。真实端到端延迟需要目标设备与会话实测；本地时序测试不等于公网验收。

The current implementation matches stream version 11 of the inspected desktop. This is not a public stable API. Without compatible IPC, flushed-log synchronization remains available and may wait for tool activity. Actual end-to-end latency depends on the device/chat; local timing tests do not establish public-network acceptance.

## 测试与验收 / Tests and acceptance

从根目录运行完整本地验证；需要 Go、Flutter、Python 3，以及能找到 `flutter` 的 PATH。

Run full local verification from the project root. It requires Go, Flutter, Python 3, and `flutter` on PATH.

```sh
./scripts/verify.sh

# Individual checks
(cd backend && GOWORK=off go test -race ./...)
(cd backend && GOWORK=off go vet ./...)
(cd app && flutter analyze)
(cd app && flutter test)
```

`verify.sh` 启动临时回环服务，运行 Go race/vet/build、Flutter analyze、跨语言 wire fixture、直连 TLS、TLS 代理到实际 HTTP/WS CLI、实际 Gateway/Local/Flutter 与模拟飞书 PKCE/回调。覆盖 SAS、租户/账号隔离、图片、轮换、重连、缓存、篡改/重放和撤销；结束清理临时进程与状态。单独 `flutter test` 未提供 fixture 环境时，不能替代完整网络验收。

`verify.sh` starts temporary loopback services and runs Go race/vet/build, Flutter analysis, cross-language wire fixtures, direct TLS, a TLS proxy to the real HTTP/WS CLI, and real Gateway/Local/Flutter flows with simulated Feishu PKCE/callbacks. It covers SAS, tenant/account isolation, images, rotation, reconnection, caching, tampering/replay, and revocation, then cleans temporary processes/state. Standalone `flutter test` without fixture environment does not replace complete network acceptance.

0.2.0 验证记录（2026-10-07）：23 个 Go 顶层测试、13 个 Flutter 测试通过；Android split-ABI 和 iOS unsigned 编译成功。Android GM1900 已验证 0.2.0 安装/冷启动；0.1.0 另有真实日志直连/安全存储验证。**真实飞书扫码、公网 Envoy/移动网络、iOS 真机与生产签名尚未验收。** 本地 fixture 不调用计费模型、不读取真实用户日志。

The 0.2.0 record (2026-10-07) contains 23 passing top-level Go tests and 13 Flutter tests, Android split-ABI builds, and an unsigned iOS build. Android GM1900 installation/cold launch was verified for 0.2.0; 0.1.0 has separate real-log/direct-connection/secure-storage evidence. **Real Feishu scanning, public Envoy/mobile-network access, iOS devices, and production signing remain unverified.** Local fixtures neither call paid models nor read real user logs.

0.3.0 验证记录（2026-10-08）：项目过滤、创建/发送/停止/审批、重复请求去重、重启状态修复及加密 Gateway 转发已验证。两种真实 CLI 均完成连续对话、fork 和 resume；GM1900 已安装 0.3.0 并实际发送项目读取提示、显示回复。当前体验链路是 USB 转发 HTTPS/WSS，保留原配对。

The 0.3.0 record (2026-10-08) verifies project filtering, create/send/interrupt/approval commands, request deduplication, restart recovery and encrypted gateway forwarding. Both real CLIs pass multi-turn, fork and resume checks; GM1900 has 0.3.0 installed and sends project-reading prompts with rendered replies. The current hands-on connection uses USB-forwarded HTTPS/WSS with pairing preserved.

详细证据与历史版本范围见 [ACCEPTANCE.md](docs/ACCEPTANCE.md)，不是生产就绪承诺。

See [ACCEPTANCE.md](docs/ACCEPTANCE.md) for evidence and historical version scopes; this is not a production-readiness claim.

0.3.1 界面更新（2026-10-08）：项目文件夹分组默认显示 5 条，可展开/收起；最新会话跨项目按更新时间排序。执行中在列表与会话页转圈，用户消息靠右，思考/工具折叠，输入区和“回到最新消息”按钮重新布局。查看历史时新输出不会强制拉回底部。电脑会话运行中仍可复制上下文续聊，停止按钮用于手机启动的任务。

The 0.3.1 UI update (2026-10-08) adds project folders with five initial chats and expansion, and a cross-project recent view sorted by update time. Running turns animate in lists and chats; user messages sit on the right, reasoning/tools collapse, and the composer and jump-to-latest control are redesigned. Reading history is not interrupted by new output. Running desktop chats can still be copied into mobile continuations; the stop button controls mobile-started tasks.

0.3.2 项目管理（2026-10-08）：整组折叠、拖动/箭头排序、勾选显示/隐藏及全部显示/隐藏，布局使用本机加密安全存储，重开保留。隐藏项目仍可从最新会话进入。Go race/vet、Flutter analyze 和 26 个 Flutter 测试通过，涵盖布局恢复、保存失败回退、排序与显示互不覆盖、小屏大字体及直连/代理/Gateway 加密集成。

The 0.3.2 project-management update (2026-10-08) adds whole-group folding, drag/arrow ordering, visibility checkboxes and show/hide-all controls. Layout preferences use local encrypted secure storage and survive restarts. Hidden projects remain accessible through recent chats. Go race/vet, Flutter analysis and 26 Flutter tests pass, covering layout restoration, failed-save rollback, independent ordering/visibility, small screens/large text and encrypted direct/proxy/gateway integration.

0.3.3 图片与链接修复（2026-10-08）：Markdown 图片在会话内显示并可放大；HTTP/HTTPS 网页链接可打开；本地图片与 UTF-8 文本/源码链接通过加密请求读取，支持空格/中文路径和行号。会话右上角“附件”集中列出图片、文件和链接。旧会话中新增支持的结构化图片会从已导入的近期日志补齐，只追加图片，不重放消息或改变会话排序、配对与源读取位置。加密的会话索引避免附件读取反复扫描其他会话。完整验证通过 33 个 Flutter 测试及 Go race/vet；附件直连、代理和 Gateway 链路均通过。

The 0.3.3 image/link fix (2026-10-08) renders and enlarges Markdown images, opens HTTP/HTTPS web links, and reads local image/UTF-8 text/source links over encrypted requests with spaces, Chinese paths and line numbers supported. The chat attachment button collects images, files and links. Newly supported structured images in existing chats are recovered from the bounded imported history without replaying messages or changing chat order, pairing or source offsets. An encrypted session index avoids scanning unrelated chats for attachment reads. Full verification passes 33 Flutter tests and Go race/vet, including attachment flows through direct, proxy and gateway connections.

0.3.4 会话性能与入口（2026-10-08）：首页去掉“全部”，以 Codex / Claude 为一级入口，每个 agent 下独立分项目与最新会话；项目排序、折叠、隐藏分别保存，并兼容旧布局。后端保留完整事件日志，另建加密的完整消息索引；打开/重连先取最近最多 40 个展示块，向上翻按需分页，实时更新发送完整快照，避免跨页丢失增量前缀。手机使用从最新消息开始的惰性列表、复用未变化的 Markdown、合并通知、每 5 秒最多一个缓存写入，只缓存最近会话的 40 个块，内存保留最多 3 个会话和约 32 MiB 图片原始数据。附件列表对应当前已加载历史；分页不会删除后端历史，也不会减少 agent 续聊上下文。

The 0.3.4 performance update (2026-10-08) removes All and makes Codex / Claude the primary entries, each with its own projects, recent chats and saved order/collapse/visibility settings. Existing layouts migrate compatibly. The backend retains the full event log and adds an encrypted index of complete items. Opening/reconnecting fetches at most 40 recent items; scrolling up pages older history, while live snapshots preserve delta prefixes across page boundaries. The phone uses a lazy list starting at the latest message, reuses unchanged Markdown, coalesces notifications and allows at most one cache write every five seconds. It caches 40 items from the latest chat and retains at most three chats plus roughly 32 MiB of raw image data in memory. Attachments reflect currently loaded history. Paging does not delete backend history or reduce agent continuation context.


## 排障 / Troubleshooting

| 现象 / Symptom | 检查 / Check |
| --- | --- |
| 首页没有会话 / No sessions | 确认已配对、显式配置来源、源文件在 lookback 内，且 agent 已 flush。 / Check pairing, source flags, lookback, and source-agent flushing. |
| 手机访问不到 localhost / Phone cannot reach localhost | localhost 指手机；USB 用 adb reverse，远程使用公网 HTTPS。 / localhost is the phone; use adb reverse for USB or public HTTPS remotely. |
| TLS 被拒绝 / TLS rejected | 检查域名、CA、有效期和 pin；不要关闭证书验证。 / Check hostname, CA, lifetime, and pin; do not disable certificate verification. |
| 飞书登录 503 / Feishu login returns 503 | 网关没有正确注入 App ID/Secret。 / Gateway application ID/secret is not correctly configured. |
| 飞书回调失败 / Feishu callback fails | 核对应用范围、回调 URL、Cookie/浏览器连续性和 flow 有效期。 / Check app availability, callback URL, cookie/browser continuity, and flow expiry. |
| 工作站列表为空 / Empty workstation list | 两端必须是同应用、同租户、同账号；Local 先完成关联。 / Both ends must use the same app/tenant/account; associate Local first. |
| 工作站离线 / Workstation offline | 检查 Local 的出站 HTTPS/WSS、已保存关联和 Envoy upgrade/timeout。 / Check outbound HTTPS/WSS, saved enrollment, and Envoy upgrade/timeouts. |
| 等待批准 / Waiting for approval | 在可信 Local 核对 SAS 后执行 pair-approve；请求 5 分钟过期。 / Compare SAS on trusted Local and run pair-approve; requests expire after five minutes. |
| owner socket 不可用 / Owner socket unavailable | 服务需已启动，管理命令使用同用户、同 data，socket 不能经过公网。 / Start the service and use the same user/data; never expose the socket publicly. |
| 图片未显示 / Image unavailable | 检查附件 root（含截图临时目录）、文件是否仍存在、图片格式及 8 MiB 限额；网页图片需 HTTPS。 / Check allowed roots including screenshot temp directories, file existence, format and 8 MiB limit; web images require HTTPS. |
| 没有新建/发送入口 / Missing create/send controls | 配置真实 `--project-roots`，确认 CLI 登录、Agent 能力及项目路径；导入日志不自动开放执行。WorkBuddy 无新建功能。 / Configure allowed projects, CLI login and actual capabilities; log import alone does not enable execution. WorkBuddy has no create operation. |
| 发给电脑 Codex 却没有原会话变化 / Desktop Codex chat unchanged | 确认配置了桌面 socket、电脑原会话已打开、项目被允许；未配置时是明确标注的手机分支。0.4.11 起接收原生实时文字/状态；检查已送达、执行状态和连接，勿重复发送。 / Check opt-in desktop socket, an open original chat and allowed project. Without it, continuation is a labeled mobile branch. Since 0.4.11, native text/status streaming is used; inspect delivery, execution and connection state before retrying. |
| 桌面会话页面报错且刷新无效 / Desktop page error persists after reload | 先升级 Local 的同步修复，检查桌面兼容性及错误日志；不要删除源历史或反复重发。旧桌面缓存故障需针对具体会话诊断，升级后还要实际打开验证。 / Upgrade Local's sync fixes and check desktop compatibility/logs. Preserve source history and avoid duplicate sending; diagnose the specific old cache and verify reopening separately. |
| “文件不在工作站允许的附件目录内” / File outside allowed attachment roots | 核对链接实际绝对路径，将所属目录加入现有 `--media-roots` 后重启；macOS `$TMPDIR` 与 `/private/tmp` 分别处理，仍须当前会话引用且文件存在。 / Check the actual absolute path, allow its directory and restart. Distinguish macOS temp directories; current-chat reference and file existence remain required. |
| 大会话卡顿或找不到旧消息 / Large chat slow or older messages absent | 使用对话目录跳转、按需分页；检查源首次导入范围。需要全量时显式设 lookback/history 为 0，首次导入会增加负载。 / Use directory jumps/pagination and check import bounds. Explicit full import increases initial load. |
| Agent 设置显示未安装/未配置 / Agent missing/unconfigured | 服务用户的 PATH 与终端可能不同；确认 CLI 绝对路径、JSON 0600 权限、环境变量及修改后的 Local 重启。 / Check the service user's PATH, absolute CLI paths, mode-0600 JSON, environment variables and restart. |
| OpenCode 401 或无法连接 / OpenCode unauthorized/unreachable | 确认服务在线、URL/端口一致；两进程密码与用户名一致，无密码服务移除 password_env。 / Check server, URL/port and matching credentials; omit password_env for an unauthenticated server. |
| Qoder/TRAE 没有旧 IDE 会话 / Qoder/TRAE IDE history absent | 先完成 CLI 登录；当前接入 ACP，只有 Provider 广告 list/load 时恢复原 ID，不接管私有 IDE 历史。 / Complete CLI login; native restoration requires ACP list/load and does not import private IDE history. |
| WorkBuddy 提示先配置应用 / WorkBuddy requests application configuration | 需要已审核应用的 client ID、回调和 Local 环境中的 secret；勾选显示或飞书登录不能代替。 / Configure an approved app ID, callback and Local secret; visibility/Feishu login cannot substitute. |
| WorkBuddy 授权回调 404/失败 / WorkBuddy callback fails | 回调精确匹配平台配置，并到达 Local；Gateway 不代转，纯 relay 无 TCP listener。过期 state 从手机重新授权。 / Match the registered callback and route to Local; Gateway does not proxy it and relay has no TCP listener. Restart expired authorization from the phone. |
| WorkBuddy 已授权但无回复 / WorkBuddy authorized without reply | 确认电脑同账号的本地助理在线、权限批准、官方服务可达；等待轮询并查看真实错误，非逐 token 流。 / Check the same-account PC assistant, approved scopes and service access; inspect polling errors. This is not token streaming. |
| 语音首次等待/识别不准 / Voice loading or inaccurate | 确认麦克风权限及已打包模型；首次预加载与整句校正需时间。检查草稿、修正技术词后手动发送。 / Check microphone permission and packaged models; preload/correction take time. Edit technical terms before explicit sending. |
| 上传图片被拒绝 / Image input rejected | Agent/当前模型需广告图片输入能力；WorkBuddy 暂不支持。草稿保留，可移除图片后发文字/文件。 / The agent/model must support image input; WorkBuddy currently does not. Remove the image from the retained draft to send text/files. |
| 多节点网关无法路由 / Multi-node gateway routing fails | 当前单节点 bbolt/内存隧道，不支持多副本部署。 / Current bbolt/in-memory tunnels are single-node; multi-replica deployment is unsupported. |

## 项目结构与开发 / Project layout and development

```text
backend/
  cmd/agentdeck/       CLI, Local/Gateway startup and management
  internal/agent/     CLI execution, JSONL import, OpenCode/ACP/WorkBuddy adapters
  internal/server/    Local pairing, sessions, streams and owner socket
  internal/gateway/   Feishu identity, tenant routing and relay
  internal/relay/     Outbound encrypted multiplexed tunnel
  internal/secure/    Cryptographic primitives and wire envelopes
  internal/store/     Encrypted bbolt values
  testsupport/        Synthetic TLS/Feishu fixtures; not production routes
app/
  lib/core/           Transport, crypto, client, gateway and secure storage
  lib/                Native UI
  test/               Wire, integration and widget tests
scripts/              Full verification and synthetic agent fixtures
docs/                 Security, acceptance, deployment and demo screenshots
artifacts/            Local builds/receipts, excluded from Git
```

添加 agent 适配器时，规范化展示记录并保留 delta/snapshot 区别；不要导入系统/认证/内部上下文。修改 wire/密码学时，同时验证 Go/Dart 兼容、方向与 AAD、配对和重放边界。密钥、配对串、原始会话、存储库、签名材料和真实 `.env` 不提交；示例截图只能使用合成内容。

When adding agent adapters, normalize presentation records and preserve delta/snapshot semantics; exclude system/authentication/internal context. Wire/crypto changes require Go/Dart compatibility, direction/AAD, pairing, and replay-boundary checks. Do not commit keys, pairing material, raw sessions, data stores, signing credentials, or real `.env` files; screenshots must use synthetic content.

当前没有指定开源许可证；仓库尚无 LICENSE 文件。

An open-source license has not been selected; the repository currently has no LICENSE file.

## 当前限制 / Current limitations

- 移动端只能在 `--project-roots` 授权范围内启动 agent；没有独立远程 shell API。Codex 使用 workspace-write/on-request；Claude 使用 default + stdio 审批，不使用权限绕过参数。 / Mobile agent sessions are limited to `--project-roots`; no standalone shell API is exposed. Codex uses workspace-write/on-request, and Claude uses default permissions with stdio approval prompts, without bypass flags.
- 现有日志同步受源 flush 限制；已有 PC 私有连接不自动接管。 / Existing-log synchronization depends on source flushing; existing private desktop connections are not automatically adopted.
- App 一次保存一个工作站；多个手机可以独立绑定同一工作站。 / The app stores one workstation at a time; multiple phones can independently pair with the same workstation.
- 已配对可信手机可查看该 Local 的全部已导入会话；暂无按项目细分授权。 / Trusted paired phones can view all imported sessions on that Local; no per-project authorization yet.
- 单节点网关；无多副本/分布式高可用、自动飞书离职同步、硬件密钥绑定或 root 双棘轮。 / Single-node gateway; no distributed HA, automatic Feishu offboarding sync, hardware-bound keys, or root double ratchet.
- Flutter Web、SVG/音视频、PDF/其他二进制文件预览尚未实现。 / Flutter Web, SVG/audio/video and PDF/other binary previews are not implemented.

## 相关文档 / Further documentation

- [旧 Office 解析、范围与许可证 / Legacy Office parsing, bounds and licenses](docs/OFFICE_PREVIEW.md)

| 文档 / Document | 内容 / Content |
| --- | --- |
| [Security](docs/SECURITY.md) | 加密协议、信任与存储边界。 / Cryptographic protocol, trust, and storage boundaries. |
| [Gateway](docs/GATEWAY.md) | 飞书关联、中继与管理细节。 / Feishu association, relay, and management details. |
| [Deployment](docs/deploy/README.md) | Envoy 与 Go TLS 部署。 / Envoy and Go TLS deployment. |
| [Agents](docs/AGENTS.md) | 各 Agent 协议与配置边界。 / Agent protocols and configuration boundaries. |
| [Inputs](docs/INPUTS.md) | 模型、语音、图片和文件能力。 / Models, voice, images and file inputs. |
| [Design](docs/DESIGN.md) | 界面设计与交互。 / Interface and interaction notes. |
| [Acceptance](docs/ACCEPTANCE.md) | 已验证项与未验收项。 / Verified behavior and outstanding acceptance. |

详细文档当前主要为中文，核心安装和操作已在此 README 中提供英文对照。

Detailed documents are currently primarily in Chinese; this README provides English counterparts for the core installation and operating procedures.

## 版本历史 / Version history

- **0.4.14**：输入框上方队列与三点菜单；Codex 原生本地队列立即提交、独立状态同步、引导／编辑／删除与版本冲突提示。 / Queue cards above the composer, native local Codex queue submission, independent state updates, steer/edit/delete and stale-version rejection.
- **0.4.13**：旧 DOC/XLS 内容预览、UTF-8/UTF-16/GB18030 文本与有界大文件预览、原生音频播放、真机 README 截图。 / Legacy DOC/XLS content previews, UTF-8/UTF-16/GB18030 text with bounded large-file previews, native audio playback and real device screenshots in this README.

以下是历史版本记录；当前操作以以上 How to use 为准。特别是 0.4.1–0.4.3 的工作站语音路径已经由 0.4.6+ 手机端默认路径替代，不要按旧公告安装当前不需要的 Whisper 服务。

These notes describe historical versions; follow the current How to use above. In particular, the workstation voice path from 0.4.1–0.4.3 was superseded by the default on-device path in 0.4.6+; do not install an unnecessary Whisper service based on older release notes.

0.4.8：附件选择完全在手机本地完成，点「＋」后照片、拍照、文件立即可选，不查询远程模型列表。图片支持在点击发送后、创建续聊或上传前校验；不支持时保留草稿。麦克风切换整块「按住说话」，识别与校正沿用 0.4.6。

0.4.8: Attachment selection is entirely local: Photo / Camera / File are immediately available without querying remote model lists. Image support is checked after Send, before conversation adoption or uploads, preserving drafts on rejection. The mic toggles a full-width Hold to speak button; recognition and correction retain 0.4.6 behavior.

0.4.6：修复语音解码器量化造成的错字、漏字，新增手机端 SenseVoiceSmall 整句校正。**长按说话时出字、停顿或松开时校正、文字可编辑、上滑取消**，仍需手动发送。音频全程留在手机内存，不上传；模型随包提供并预加载。校正和首次加载需要时间，英文技术名词仍可能误识别。[客户端语音与实测](docs/INPUTS.md#实时语音--live-voice)。

0.4.6: Fixes deletion/substitution errors caused by the quantized speech decoder and adds on-device SenseVoiceSmall utterance correction. **Hold for live text, pause/release for correction, edit before sending, slide up to cancel.** Audio remains in phone memory without upload. Models ship with the app and preload; correction and initial loading take time. English technical names can still be misrecognized. [Client voice and measurements](docs/INPUTS.md#实时语音--live-voice).

0.4.4：六个 Agent 都提供实时语音与文件入口；OpenCode 支持实际模型、Variant 与原生图片参数；Qoder/TRAE 根据 ACP 返回的模型、思考强度和图片能力启用控件。WorkBuddy 当前接口不提供模型切换或图片输入，界面显示限制原因。普通文件通过加密附件的本机只读引用读取。[各 Agent 输入能力](docs/INPUTS.md#各-agent-输入能力--agent-input-capabilities)。

0.4.4: All six agents expose live voice and general-file controls. OpenCode uses its actual model/Variant catalog and native image parts; Qoder/TRAE use advertised ACP model, thought-level and image capabilities. WorkBuddy's current API lacks model switching and image input, with the limitation shown in the UI. General files use owner-only read references backed by encrypted attachments. [Agent input capabilities](docs/INPUTS.md#各-agent-输入能力--agent-input-capabilities).

0.4.3：语音输入支持录音期间持续更新文字，结束后整段校正；本机 Whisper 模型常驻以减少重复加载。[实时语音说明](docs/INPUTS.md#实时语音--live-voice)。

0.4.3: Voice hypotheses update while recording, followed by full-recording correction. The local Whisper worker keeps weights in memory to avoid repeated loading. [Live voice details](docs/INPUTS.md#实时语音--live-voice).

**0.4.2 同步修复 / Synchronization fix:** 接管电脑 Codex 原会话后继续导入日志，并一次性补回近期漏掉的记录；发送文本补齐 `text_elements`，避免桌面会话页面渲染异常；输入文字后发送箭头保持白色。原会话内容在日志落盘后同步，本修复不代表桌面原会话逐 token 推送。详情见 [验收记录](docs/ACCEPTANCE.md)。

**0.4.2:** Desktop-owned Codex conversations keep importing flushed logs after mobile adoption, with a one-time recovery of recently skipped records. Text input includes `text_elements` to avoid a desktop rendering error, and the enabled send arrow stays white. This is log-flush synchronization; native per-token desktop streaming is not claimed. See [acceptance evidence](docs/ACCEPTANCE.md).

**0.4.1：会话输入框支持 Codex/Claude 模型与 effort、照片/拍照/文件，以及本机语音转文字。** 左下角选模型，右侧「＋」选附件，麦克风录音后点击「完成」；文字可编辑，再点击发送。使用与加密边界见 [会话输入说明](docs/INPUTS.md)。语音需要给 Local 启用一次离线转写服务。

**0.4.1 adds Codex/Claude model and effort selection, photos/camera/files and workstation speech-to-text.** Choose a model at the lower left, tap + for attachments, or record with the microphone and tap Done. Edit the transcript, then explicitly send. See [conversation input](docs/INPUTS.md) for usage and encryption boundaries. Voice requires a one-time Local offline-transcription setup.


0.3.5 对话目录（2026-10-08）：会话右上角的目录按钮列出已导入会话的全部用户消息、Agent 回复和图片摘要，含尚未加载的旧记录；可搜索摘要并直接跳转。目录使用独立的加密分页索引，正文仅读取目标附近最多 40 个展示块，不用下载中间所有消息。选中记录会高亮；浏览历史时新的输出不会拉回底部，点击“回到最新消息”重新加载最近记录。目录中的刷新按钮可获取最新摘要。目录覆盖已导入历史；源日志的首次读取时间与大小限制仍适用。

The 0.3.5 conversation directory (2026-10-08) is available from the top-right list button. It includes every imported user message, agent reply and image preview, including unloaded older records. Search previews and tap to jump directly. A separate encrypted paged index supplies the directory; the body loads at most 40 items near the selection without downloading intervening history. The selected record is highlighted. New output preserves historical reading; Jump to latest reloads recent records. Refresh updates the previews. The directory covers imported history; source-log initial age and size limits still apply.

0.3.6 时间校准（2026-10-08）：直连以 S-LOCAL 为时间源，网关模式下 Local 与 App 都校准到 S-GATEWAY。校时请求通过已持有的配对/路由/登录密钥验证，不依赖调用者系统时间；响应绑定随机挑战、身份和协议版本。接受最长 10 秒 RTT，并用 RTT/2 估计响应传输耗时。连接内使用单调计时，系统时间跳变不会改变既有连接的 epoch；每次重连、App 回到前台重新校准并换连接 salt，沿用原配对 root。校时失败停止握手，不退回手机系统时间。校准只作用于协议时间，TLS 证书仍按系统时间验证。

The 0.3.6 clock update (2026-10-08) uses S-LOCAL as the direct time authority and S-GATEWAY for both Local and App in relay mode. Calibration authenticates an existing pairing, routing or login key without requiring the caller's wall-clock timestamp. Replies bind a random challenge, identity and protocol version. RTT is limited to 10 seconds; RTT/2 estimates response transit time. Monotonic time advances connection epochs, so wall-clock jumps do not move an existing connection. Reconnect and app resume recalibrate and refresh the connection salt while preserving the pairing root. Failed calibration stops the handshake without a phone-time fallback. Calibration affects protocol time only; TLS certificate validity still uses system time.

0.3.7 界面精修（2026-10-08）：首页统一工作空间标题、连接状态、agent 切换和视图指示线；项目列表采用白色分组面板，长会话标题可显示两行，补充更新时间与当前选择。会话页统一字体、Markdown 标题/引用、思考与工具折叠块、图片圆角和输入面板；工作站、搜索、弹窗与底部导航保持同一套样式。项目排序/隐藏、加密校时、目录、历史分页、图片链接及发送能力继续保留。细节与参考见 [界面说明](docs/DESIGN.md)。

The 0.3.7 refinement (2026-10-08) adds a consistent workspace heading, connection state, agent switch and view indicators. White grouped project panels display two-line chat titles, update timestamps and the current selection. Conversation typography, Markdown headings/quotes, reasoning/tool panels, image corners and composer are refined; workstation, search, dialogs and navigation share the same visual language. Project order/visibility, authenticated clock calibration, directory, paged history, images/links and messaging remain available. See [interface notes](docs/DESIGN.md) for the reference and details.

0.3.8 尺度与品牌（2026-10-09）：标题、项目和正文缩小一档，保留系统大字体及触摸区域；“项目 / 最新会话”采用无水波纹的平滑分段切换。App 名称统一为 AgentDeck，新增叠层终端图标，替换 Android 启动图标并提供 iOS 对应尺寸。详见 [界面说明](docs/DESIGN.md)。

The 0.3.8 scale/brand update (2026-10-09) reduces heading, project and body sizes while retaining system text scaling and touch targets. Projects/Recent use a quiet segmented transition without label ripples. The display name is consistently AgentDeck, with original layered-terminal artwork for Android launchers and matching iOS assets. See [interface notes](docs/DESIGN.md).


0.4.9：设置新增简体中文、繁体中文、英文和俄文，默认英文。界面、Flutter 菜单和已知错误提示随语言切换，选择在手机安全存储中保存；项目名、会话内容和模型名不翻译。语言选择与附件弹窗支持小屏滚动，语音识别模型沿用 0.4.6，界面语言不会切换识别模型。

0.4.9: Settings now supports Simplified Chinese, Traditional Chinese, English and Russian, with English as the default. Interface text, Material controls and known app errors follow the selected language, stored in device secure storage. Project names, conversation content and model names remain unchanged. Language and attachment sheets scroll on small screens. Speech models retain 0.4.6 behavior; selecting an interface language does not change the speech model.


0.4.10：补齐 WorkBuddy 授权、OpenCode/ACP 状态及后端错误的四语显示，首页错误条、授权提示和配对失败跟随所选语言。HTTP 状态码、Agent 名称等参数保持原值；后端生成的图片提示和默认标题可翻译，会话正文和实际文件名保留原文。新增后端文案扫描测试，新增中文错误未提供翻译会导致检查失败。第三方授权网页使用其自身语言设置。

0.4.10: WorkBuddy authorization, OpenCode/ACP statuses and backend errors now follow the selected interface language, including home error banners and pairing failures. HTTP codes and agent names are preserved. Backend image notices and generated default titles are localized while conversation bodies and real filenames remain unchanged. A source-based coverage test detects backend messages with missing translations. Third-party authorization pages retain their own language settings.
