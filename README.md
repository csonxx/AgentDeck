# AgentDeck · 会话甲板

在手机上查看和继续电脑上的 coding agent 会话。支持 Codex、Claude Code、OpenCode、Qoder、TRAE、WorkBuddy，电脑运行 Go 后端，手机使用 Flutter 原生 App。

当前版本 **0.4.34** · [源码仓库](https://gitlab.ttyuyin.com/azeroth/agentdesk)

[下载最新 Android APK / Download latest APK](https://web-test-omnivibe.ttyuyin.com/agentdeck/v2/app/releases/android/apk) · [0.4.34 存档 / Archive](https://xcdn.ukilive.com/mcp_file/custom_file/agentdeck/0.4.34/c5d6b55ac994e3179e6681c15ffab726f416f6dd47d797eafcf0deff109faf47/agentdeck-0.4.34-arm64-v8a.apk) · 0.4.34 · arm64-v8a · 272.9 MB
已通过 infras 上传并完成匿名公网下载 SHA-256 校验。 / Uploaded through infras; anonymous public download SHA-256 verified.


## 应用更新与设备管理 / App updates and device management

Android App 在启动和回到前台时自动检查已配置网关的新版本（检查间隔 6 小时）；设置 → 应用更新也可立即检查、关闭自动检查、下载或安装。首次使用需手动安装带更新功能的版本，此后可从 App 内升级。下载由 Android 系统负责，可离开页面、切后台，断网或进程退出后继续恢复；也可选择浏览器下载。下载前显示版本说明和大小；安装前校验 SHA-256、包名、版本号和当前应用签名。首次安装更新需在 Android 允许 AgentDeck 安装此来源的应用，然后返回点击安装；安装仍由系统确认，配对与设置保留。

The Android app checks the configured gateway at launch and on foreground resume, at most once every six hours. Settings → App updates supports immediate checks, automatic-check preferences, downloads and installation. Manually install the first update-capable version once, then upgrade within the app. Android owns the download so it continues in the background and resumes after network/process interruptions; browser download is also available. Version notes and size appear before downloading. SHA-256, package identity, version code and the installed app's signing certificate are checked before Android confirms installation. Initially allow installs from AgentDeck in Android settings, return, and tap Install. Pairing and settings are retained.

网关提供 `GET /agentdeck/v2/app/releases/android?abi=arm64-v8a` 版本信息，以及 `GET /agentdeck/v2/app/releases/android/apk` 下载跳转。APK 存放于 infras/CDN，网关只在现有加密数据库中保存发布信息，不新增 PVC，也不通过会话中继传送 APK。发布更新仅允许通过网关 owner Unix 管理口：

The gateway provides a version manifest and APK redirect. Infras/CDN stores APKs; the gateway's existing encrypted database stores only release metadata. No new PVC or conversation relay transfer is needed. Publishing is restricted to the owner Unix socket:

```sh
python3 scripts/android_release_manifest.py \
  --apk artifacts/agentdeck-VERSION-arm64-v8a.apk \
  --receipt artifacts/apk-public-VERSION.json \
  --notes-file release-notes.txt --output android-release.json
agentdeck gateway-publish-release --data /data/agentdeck \
  --android-manifest android-release.json
```

生成清单会核对 APK 原生版本号和上传后的完整下载校验记录；网关拒绝降低版本号或用不同内容替换同一版本。发布后无需重启网关，App 下一次检查即可发现新版本。清单是公开信息，不得放凭据或私密发布说明。

Manifest generation checks native APK version codes and complete public-download verification. The gateway rejects lower version codes and different content under an existing code. Publishing takes effect without a gateway restart. The manifest is public; exclude credentials and private notes.

本机管理页的已配对 App 支持修改显示名称和删除配对。App 设置 → 设备显示名称也能改名；离线修改在重连后同步，电脑改名实时推送到 App。删除配对立即断开连接、清除未使用的连接票据并让旧设备密钥失效，需要重新配对；已有会话历史保留。在线行显示 IP，离线行显示配对时间。直连只读取真实连接地址；中继 IP 由已认证的网关覆写传递。若网关位于 Envoy/Istio 后面，通过 `AGENTDECK_TRUSTED_PROXY_CIDRS` 明确配置可信代理网段，按从右向左的可信代理链读取 X-Forwarded-For，不信任公网客户端自行填写的来源地址。未配置时只显示网关实际看到的连接地址。

The local console supports renaming and revoking paired Apps. App Settings → Device display name supports the same rename, queues offline changes and receives owner changes live. Revocation disconnects streams immediately, removes unused tickets and invalidates the old device key; pairing again is required, while chat history is retained. Online rows show connection IPs; offline rows show pairing time. Direct connections use the actual peer address. Relay metadata is overwritten by the authenticated gateway. Behind Envoy/Istio, explicitly set `AGENTDECK_TRUSTED_PROXY_CIDRS`; the gateway walks trusted forwarding hops from right to left, ignoring untrusted client-supplied IPs. Without that configuration it reports only the actual gateway peer address.

## 能做什么

- 按 Agent、项目或最近会话浏览，搜索历史并跳到指定消息。
- 新建、发送、续聊、停止和工具审批，具体能力取决于 Agent。
- 授权卡片点击「展开完整执行内容」，可滚动查看和复制完整命令、工作目录、文件路径及嵌套参数；关闭详情后选择允许或拒绝。查看详情不会执行操作。 / Tap “Show full execution details” on an approval card to scroll through and copy the complete command, working directory, file paths and nested arguments. Close details to allow or deny; viewing details does not execute the action.
- 每条发送/接收的文字消息下方可复制完整原文，保留 Markdown、链接和代码。 / Copy the complete original text below each sent/received message, including Markdown, links and code.
- 查看 Markdown、思考摘要、工具输出、图片与文件；支持 Office 内容预览和音视频播放。
- 手机端语音转文字，编辑后手动发送；支持图片和文件附件。
- 手机与工作站之间加密通信，可直连，也可通过飞书网关发现工作站和中继。

WorkBuddy 当前只提供每个账号的一个本地助理；各 Agent 的接入要求见 [接入指南](docs/AGENTS.md)。

## 界面

以下为 Android 0.4.13 真机截图，项目组已折叠以隐藏私人会话。

<p>
  <img src="docs/images/app-0.4.13-workspace.png" width="260" alt="工作空间">
  <img src="docs/images/app-0.4.13-settings.png" width="260" alt="设置">
</p>

## 快速上手：Android USB 直连

需要 Go 1.25+、Flutter 3.38+ / Dart 3.10+、Python 3、Android SDK/JDK，以及开启 USB 调试的 Android 7.0+ 手机。在运行后端的同一系统用户下安装并登录 Codex 或 Claude Code。

**1. 克隆并构建**

```sh
git clone git@gitlab.ttyuyin.com:azeroth/agentdesk.git
cd agentdesk
mkdir -p artifacts
(cd backend && GOWORK=off go build -o ../artifacts/agentdeck.next ./cmd/agentdeck)
mv artifacts/agentdeck.next artifacts/agentdeck
(cd app && flutter pub get)
python3 scripts/setup_client_voice.py
(cd app && flutter build apk --release --split-per-abi)
adb -s DEVICE_SERIAL install -r app/build/app/outputs/flutter-apk/app-arm64-v8a-release.apk
```

把 `DEVICE_SERIAL` 换成 `adb devices` 中的设备编号。语音模型首次下载约 300 MB，随 App 打包；当前 APK 使用开发测试签名。

**2. 在电脑启动后端**

将 `$HOME/workspace` 换成实际项目父目录，以下命令从仓库根目录执行，并保持运行：

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

日志目录用于导入历史，`--project-roots` 用于允许手机新建和发送。若要发送到电脑上已打开的 Codex 原会话，给启动命令增加 `--codex-desktop-socket "$HOME/.codex/ipc/ipc.sock"`；未配置时通过手机分支续聊，原会话的停止和审批仍在电脑处理。

**3. 在另一个终端配对**

```sh
adb -s DEVICE_SERIAL reverse tcp:8443 tcp:8443
./artifacts/agentdeck pair --data "$HOME/.agentdeck" --out /tmp/agentdeck-pair
```

打开生成的 `/tmp/agentdeck-pair.png`，在 App 扫码，也可粘贴 JSON。配对码 5 分钟内有效、单次使用，配对后删除 `.png` 和 `.json`。手机连接后，选 Agent → 项目 → 打开或新建会话 → 发送。

USB 断开或电脑休眠会中断连接；重接后再执行 `adb reverse`，已有配对可继续使用。日常通过 Wi-Fi 或移动网络使用，请配置下面的公网连接。

会话向顶部滑动自动加载更早记录，向底部滑动自动补回后续记录，持续分页直到没有更多；不需要点加载按钮。工具调用默认折叠；分页只有工具或过程记录时会继续自动获取，直到取得实际对话消息或到达历史末尾。窗口保持有界并保留阅读位置，新输出不会强制拉回底部。右上角对话目录可筛选「全部 / 我发送的 / 我收到的」，结合摘要搜索定位并跳转。 / Scroll towards the top to load older records and towards the bottom to load newer records, continuing until the end without load buttons. Tool calls default to collapsed. Pages containing only execution records are followed automatically until a dialogue message or the history boundary is reached. Bounded windows preserve the reading position. The conversation outline filters All / Sent by me / Received and combines filtering with preview search and precise navigation.

## Claude 桌面会话同步 / Claude desktop conversations

**实机状态（2026-10-12）：OnePlus 7 已完成公网网关到 Desktop 原会话的真实收发。** 普通文字、图片、运行中追加输入、询问回答、停止执行和 Mac 锁屏后收发均通过。输入在原生记录中是普通用户来源；原会话 UUID、执行 PID 和启动时间保持一致。没有 peer 提示、分支或替换 CLI。OnePlus 7 已安装完整构建的 **0.4.32**，现有配对保留；各项证据和版本范围见 [验收记录](docs/ACCEPTANCE.md)。

**Live status (2026-10-12): real OnePlus 7 round trips through the public gateway to the original Desktop chat passed.** Coverage includes text, images, mid-turn input, question answers, interruption and sending/receiving while the Mac is locked. Native records retain human provenance and the original session UUID, executor PID and process birth. No peer warning, fork or replacement CLI is involved. OnePlus 7 runs a full release build of **0.4.32**, retaining its pairing; see [acceptance evidence and version coverage](docs/ACCEPTANCE.md).

macOS 下启用默认 `~/.claude/projects` 日志目录时，后端自动读取 `~/Library/Application Support/Claude/claude-code-sessions` 的桌面会话目录，按桌面的标题、原项目和归档状态显示。目录不受默认 72 小时日志窗口限制，子 agent 日志不会成为独立会话。已存在的手机分支仍保留。

On macOS, enabling the default `~/.claude/projects` log root automatically discovers the Claude desktop catalog under `~/Library/Application Support/Claude/claude-code-sessions`. Chats follow desktop titles, original projects and archive state. The catalog is not limited by the default 72-hour log window, and subagent logs do not become separate chats. Existing mobile branches remain available.

本机直发是显式选择的实验性 macOS 接入，使用 Desktop 自带调试功能，并非 Anthropic 公开稳定 API。首次需要在 Claude **Help → Troubleshooting → Enable Developer Mode** 开启开发模式（会重启应用），再在 **Developer → Enable Main Process Debugger** 开启本机调试器。仅在确认本机其他程序可信时开启：该调试器有广泛的本机应用控制权限。

Native direct input is an explicitly opted-in experimental macOS integration using Desktop's built-in debugger, not a public stable Anthropic API. Enable **Help → Troubleshooting → Enable Developer Mode** (restarts Desktop), then **Developer → Enable Main Process Debugger**. Enable it only on a trusted workstation: the debugger grants broad local application control.

S-LOCAL 在原有启动参数上增加 `--claude-desktop-debugger`；仍需 `--claude-sessions` 和可读取的 Desktop 目录。该开关默认关闭，不适用于 S-GATEWAY。后端仅连接 `127.0.0.1:9229`，校验监听进程的用户、可执行文件和启动时间，再匹配本机目录中的原会话、项目和消息 UUID。**不要把 9229 加入 Envoy、VS、公网监听或端口转发。** 手机仍只连接自己的 S-LOCAL/S-GATEWAY，不需要登录 Claude 网页，也不直连 Anthropic；模型执行仍由电脑 Claude 使用原账号完成。

Add `--claude-desktop-debugger` to the existing S-LOCAL arguments, with `--claude-sessions` and a readable Desktop catalog. It defaults off and does not belong on S-GATEWAY. The adapter connects only to `127.0.0.1:9229`, verifies the listener's owner, executable and process birth, then matches the original local session, project and message UUID. **Never publish or proxy port 9229 through Envoy/VS or a port forward.** The phone continues connecting only to its own Local/Gateway; it requires no Claude web login or direct Anthropic connection. Desktop retains its original account for model execution.

发送后，后端只有在原生记录中确认同一 UUID 的普通用户输入才返回 `submitted`；它表示原会话收到输入，不表示模型执行成功。回执丢失不会自动重复发送或回退到 peer、工作站队列、分支/替换执行端。额度/API 错误同步为失败。图片作为原生图片输入；运行中追加交给 Desktop 自己的待处理输入。手机可展开审批完整内容、回答问题、单次允许或拒绝，并停止原执行。真机已验证图片、运行中追加、询问回答和停止；工具允许/拒绝的分支另有协议回归，未声称在真机触发 Bash 授权弹窗。此版本的模型参数应在电脑原会话修改；不同参数会明确拒绝，不会假装切换成功。

The backend returns `submitted` only after observing the exact UUID as native human input; this acknowledges reception, not successful inference. Lost acknowledgements never trigger a resend or a fallback to peer delivery, a workstation queue, fork or replacement executor. Quota/API errors are synchronized as failures. Images use native image input, and mid-turn input goes to Desktop's own pending-input handling. The phone exposes full approval details, question answers, one-time allow/deny and interruption. Real phone checks cover images, mid-turn input, question answers and interruption; tool allow/deny branches have protocol regression coverage, without claiming a real Bash permission prompt was exercised. Change model settings in the original Desktop chat; mismatched parameters are explicitly rejected rather than falsely reported as changed.

原会话直发目前必须使用上述本机调试器，按用户授权仅在 AgentDeck 使用期间保留。S-LOCAL 同时增加 `--claude-desktop-close-debugger`，正常退出或收到 SIGTERM/SIGINT 时关闭调试器；它不会结束 Desktop 或原任务。已实际验证退出 Local 后 9229 关闭、原执行进程不变。强杀或系统崩溃无法保证清理，请手动关闭 **Enable Main Process Debugger**。当前常驻 Local 正在使用该入口，9229 仅监听本机；开发设置的持久值仍为关闭，未退出登录或改变隐私选项。

Original-chat input currently requires this local debugger. Under the owner's authorization it remains enabled only while AgentDeck uses it. Also pass `--claude-desktop-close-debugger` to S-LOCAL to close the debugger on normal exit or SIGTERM/SIGINT without stopping Desktop or its tasks. A real Local shutdown closed port 9229 while preserving the executor. Forced termination/system crashes cannot guarantee cleanup; close **Enable Main Process Debugger** manually. The resident Local currently uses the loopback-only endpoint; the persisted developer setting remains disabled, with login and privacy settings preserved.

Desktop 重启后调试器默认关闭，需要通过官方菜单重新开启；Local 不会自动绕过 Electron 的调试限制。升级 Desktop 后必须重新验收内部接口，失配时明确报未发送。Mac 锁屏收发已通过，不需要操作桌面输入框；电脑休眠、Desktop 退出或断网仍会断开。此版本的 Desktop 模型/effort 修改仍需在电脑原会话进行。

Desktop restarts turn the debugger off; reopen it through the official menu. Local does not bypass Electron's debugging restrictions. Revalidate the internal API after Desktop updates; incompatibility fails explicitly. Locked-screen sending and receiving passed without operating the desktop composer. Sleep, Desktop exit and network loss disconnect the route. Desktop model/effort changes still require the original desktop chat in this version.

官方 `claude --print --cloud` 在此前实测中仍被归为其他 Claude 会话请求，不能替代上述普通用户入口；peer 发送器仅保留协议回归测试，不供 App 用户输入调用。S-LOCAL 自己持有的 CLI 仍使用原生 `stream-json`，属于另一种执行方式。研究参考：[Relaymote 原生 Desktop 入口](https://github.com/ashwarsadh/relaymote/blob/main/lib/desktop.js)；AgentDeck 没有安装或运行其安装脚本。

Earlier tests classify official `claude --print --cloud` input as another Claude session, so it cannot replace the owner-input handler. The peer sender is retained only for protocol regressions and never called for app input. S-LOCAL-owned CLIs retain native `stream-json` as a separate execution mode. Research reference: [Relaymote Desktop input](https://github.com/ashwarsadh/relaymote/blob/main/lib/desktop.js); AgentDeck did not install or run its setup scripts.

此前桌面标准输入桥接已退出生产路径。本机 Claude 桌面保留了 `CLAUDE_CODE_LOCAL_BINARY` 字符串，但当前构建不调用初始化逻辑，不能作为可用接口。旧安装可运行 `python3 scripts/install_claude_stdio.py --uninstall` 清除启动配置；不会结束官方桌面任务。实验性桥接仅保留隔离协议回归测试。

The earlier desktop stdin adapter is no longer a production input path. The installed desktop retains the `CLAUDE_CODE_LOCAL_BINARY` string but does not initialize it in this build. Remove the old launch configuration with `python3 scripts/install_claude_stdio.py --uninstall`; this does not stop desktop tasks. The experimental adapter remains only for isolated protocol regression tests.

配置 Codex 桥接后，手机主动继续/发送到原会话时，若电脑尚未加载该会话，macOS 后端自动通过 `codex://threads/<原 ID>` 唤起，再重新发现执行端，始终向原 ID 和授权项目投递。后台刷新、只读订阅不会切换电脑页面。仅明确返回没有持有者时才唤起；版本拒绝、断线或发送后回执丢失不会被当成「没打开」，也不会自动重发消息。需要电脑 Codex 正常运行及本机协议兼容；其他系统暂不自动唤起。

With the Codex bridge configured, explicitly continuing or sending to an unloaded native chat on macOS opens `codex://threads/<original ID>` and rediscovers its owner before delivery. Background reads and subscriptions do not navigate the desktop. Only an explicit no-owner result triggers opening; protocol rejection, transport failures and lost input acknowledgements never trigger an automatic resend or fork. The native desktop must be running and compatible. Automatic opening currently supports macOS.

Codex 项目列表只展示用户会话：从原生 `state_5.sqlite` 只读核对来源和归档状态，过滤 `codex exec` 自动任务及内部子 agent；原生日志头也会阻止新任务导入。旧版本已导入、且原生目录已不再登记的日志会话只从列表隐藏，历史和配对保留。原生目录不可用时保留现有列表，不按目录长相猜测或删聊天。

The Codex project list shows user conversations. S-LOCAL reads source and archive metadata from native `state_5.sqlite`, excluding `codex exec` jobs and internal subagents; rollout headers also prevent new internal imports. Existing orphaned log imports are hidden from the list while encrypted history and pairing remain intact. An unavailable native catalog preserves the existing list; directory names are not used to guess or delete conversations.

Codex 实时快照与落盘记录在合并前统一过滤内部引用标记，避免最终回复重复显示。升级时只修复符合已知特征的展示索引，保留加密原始事件、配对及不同轮次的相同回复。

Codex live snapshots and rollout records filter internal citation metadata before reconciliation, preventing duplicate final replies. Upgrade repairs only matching presentation indexes; encrypted source events, pairing and identical replies in separate turns are retained.

Claude 状态优先读取本机 `~/.claude/sessions/*.json` 的原生 `idle / busy / waiting`，校验 PID 和进程启动时间，排除退出进程、PID 复用及停放任务。状态与桌面目录更新时间分别排序，避免完成事件被较新的目录更新时间挡住。后端每 500 ms 检查原生文件，随后通过加密会话推送；原生 CLI 写文件之前的延迟仍由 Claude 决定。不提供原生状态文件的版本或环境回退到增量日志，延迟取决于日志落盘。等待用户确认时单独显示「等待确认」，已验证的活跃进程退出显示中断。

Claude status prefers the native `idle / busy / waiting` records in local `~/.claude/sessions/*.json`, checking both PID and process birth time and excluding exited/reused PIDs and parked tasks. Status uses its own source clock rather than desktop catalog activity timestamps. The backend polls native records every 500 ms and pushes changes over the encrypted session; Claude still controls when those records are written. Environments without native status records fall back to incremental log evidence and its flush delay. Confirmation waits have their own state; a verified active owner exiting is reported as interruption.

大对话首次仍只取近期 40 条完整记录，历史按页读取。App 保留最多 200 条、约 1 Mi 个 UTF-16 文本单元的浏览窗口，滚到更早记录时自动释放离开的内容，点「回到最新」重新取最新窗口；原始记录仍在后端。单条长消息先显示约 8,000 个文本单元，点击「继续显示」逐段展开已接收内容；工具输出每段约 4,000。全部展开执行过程时，各步骤仍逐项渲染，不一次构建整个工具组。实时推送也按文本预算分包，并保留游标，避免漏消息。日志和 Codex 初次实时快照采用加密批量事务，减少逐条写盘等待。这些是内存与排版边界，不代表所有设备都有固定帧率。

Large chats initially load only the latest 40 complete records and page older history. The app retains a browsing window of at most 200 records and about 1 Mi UTF-16 text units. Moving through older pages releases records outside that window; **Back to latest** reloads the current window, while original history remains on the backend. Individual long messages initially render about 8,000 text units with **Show more**; tool output uses roughly 4,000 per step. Expanding all activity retains lazy rendering for each step. Live updates also use a bounded text budget with lossless cursors. Log imports and initial Codex native snapshots use encrypted batched transactions instead of per-record durable writes. These bounds reduce memory/layout work; they are not a fixed frame-rate guarantee for every device.

打开较早的会话时才加载对应日志的近期内容（默认最多 8 MiB），随后继续跟随增量；不会一次读入所有历史日志。桌面保留记录但本机日志缺失时，会话仍显示并提示内容不可用，不能从手机续聊。更早内容超出首次导入窗口时，需要调整 `--history-bytes`；`0` 会读取整份日志，大日志会增加时间和存储开销。

Older chats load their recent native log content on demand (up to 8 MiB by default), then follow new output. The backend does not ingest all historical logs at once. A desktop record with no matching local log remains visible with an unavailable-history notice and cannot be continued on mobile. Content outside the first-import window requires adjusting `--history-bytes`; `0` imports the entire log and increases processing and storage costs.

自定义日志目录、其他系统或多桌面账号时，可通过 `--claude-desktop-sessions /path/to/catalog` 指定对应账号的桌面目录。未启用桌面目录时按 CLI 日志发现会话，可能与桌面列表不同。

For custom log roots, other operating systems or multiple desktop accounts, set `--claude-desktop-sessions /path/to/catalog` to the appropriate account's catalog. Without a desktop catalog, chats are discovered from CLI logs and can differ from the desktop list.

## Android 附件下载 / Android attachment downloads

附件下载没有 8 MB 上限。打开附件 →「下载原文件」→ 确认 → 用 Android 系统文件选择器选择保存位置（建议本机 Downloads）。首页和附件右上角的下载图标打开统一下载列表，可暂停、续传、重新下载、取消和移除记录；返回会话、切换应用或锁屏不会取消任务。Android 使用前台下载服务和常驻通知保持加密连接；系统回收后会读取加密任务记录继续，系统强制停止后需重新打开 App。Android 的后台服务时长限制和厂商省电策略仍可能暂停任务；重新打开后可续传。本次实现针对 Android，其他平台没有承诺同等后台能力。

Downloads have no 8 MB cap. Open an attachment → **Download original → Confirm** → choose a destination with Android's system picker (prefer local Downloads). The download icon on Home and attachment pages opens a shared task list with pause, resume, restart, cancel, and remove-record actions. Leaving the page, switching apps, or locking the phone keeps the task alive through an Android foreground service and notification. Encrypted task records restore progress after process reclamation; force-stopping requires reopening the app. Android service time limits and vendor battery policies can still pause work; resume after reopening. This implementation targets Android; equivalent background behavior is not promised on other platforms.

工作站附件采用最多 4 路预取、每块 512 KiB 的端到端加密传输，减少网关往返等待；续传重新授权原会话链接，核对大小和文件版本指纹（元数据及首尾采样），源文件变更时暂停，不拼接不同版本。HTTPS 下载使用 Range / If-Range 和响应范围校验；若远端不支持续传则安全地从头下载，界面进度重置。失败与暂停保留已提交进度，只有明确「取消下载」才删除未完成文件。选择本机可持久授权、可定位写入的目录；不支持的文件提供器会报错。任务元数据保存在加密安全存储，原文件明文只写入用户明确选定的导出位置。8 MiB 仍是自动内存预览和发送给模型的单附件边界。

Workstation downloads pipeline up to four encrypted 512 KiB reads to reduce relay round-trip waits. Resume reauthorizes the original chat link and checks size plus a version fingerprint (metadata and head/tail samples); changed sources pause instead of mixing versions. HTTPS uses Range / If-Range with Content-Range validation; servers that ignore ranges restart safely with progress reset. Failed and paused tasks retain committed progress; only explicit cancellation deletes incomplete files. Choose a local provider supporting persistent access and seekable writes; unsupported providers report an error. Task metadata stays in encrypted secure storage; plaintext originals are written only to the user-selected export destination. Automatic previews and model-input uploads retain their 8 MiB bounds.

网页链接可选择「应用内打开」或「浏览器或其他应用打开」；HTTPS 媒体附件右上角也提供外部打开。下载完成后选择「其他应用打开」，由 Android 显示文件打开方式。工作站本地文件需要先通过加密连接下载，系统只向选中的应用授予导出文件读取权限；不会生成绕过配对的公网明文链接。

Web links offer **Open in app** or **Open in browser or another app**. HTTPS media previews also offer external opening. Completed downloads use Android's **Open with** chooser. Workstation files must first download over the encrypted connection; Android grants the chosen viewer read access to the exported document. No public plaintext URL bypasses pairing. See Android's [Storage Access Framework](https://developer.android.com/training/data-storage/shared/documents-files) and [foreground service types](https://developer.android.com/develop/background-work/services/fgs/service-types).

### 会话模型、Effort 与快速模式 / Session model, effort, and fast mode

进入会话后，输入栏显示原生接口能提供的实际模型、Effort 和速度。Codex 优先读当前电脑会话设置；日志返回的参数标记「上次使用」。Claude 读取原会话日志里的模型及原生 effort，OpenCode 读取最近用户消息的模型与 Variant；未提供的值明确显示未知，不拿目录默认值冒充会话当前值。S-LOCAL 持有的 Claude 进程优先返回实际运行模型及已传入的 Effort / 速度；模型变化后不沿用日志中旧模型的未知参数。输入栏只显示原生会话值，不展示手机保存的预选参数。

The composer shows the native model, effort, and speed when available. Codex prefers live desktop settings; log-derived values are labeled **Last used**. Claude reads the original transcript's native model/effort, and OpenCode reads recent user-message model/variant values. Missing values stay unknown rather than being replaced with catalog defaults. S-LOCAL-owned Claude processes expose their running model and explicit effort/speed. Unknown parameters are not carried over from a previous model. The composer displays native session values only, without a separate phone override label.

点击参数 → 选择原生模型和 Effort → 选择「标准 / 快速」→「保存」。Codex 根据本机模型目录中的服务档位启用快速模式；Claude 仅对 CLI 标记 `supportsFastMode` 的模型显示快速选项，通过该会话的 `--settings` 设置 `fastMode`，不修改全局配置。快速模式不降低 Effort，也可能增加用量或费用；账号、组织限制和可用性仍由原生 Agent 执行。OpenCode 的速度变体从其模型目录的 Variant 选择；Qoder/TRAE 显示 ACP 公开的配置，WorkBuddy 未公开相应能力时不伪造开关。参考 [Claude Fast Mode](https://code.claude.com/docs/en/fast-mode)。

Tap the parameters → choose a native model/effort → select **Standard / Fast** → **Save**. Codex uses service tiers advertised by the installed model catalog. Claude exposes Fast only for models advertising `supportsFastMode`, using per-session `--settings` with `fastMode`, without changing global settings. Fast leaves effort unchanged and may increase usage or cost; native agents retain account and organization eligibility checks. OpenCode speed variants remain native Variant selections. Qoder/TRAE use advertised ACP settings; WorkBuddy has no invented switch when its API exposes none. See [Claude Fast Mode](https://code.claude.com/docs/en/fast-mode).

## 首页刷新与执行过程 / Refresh and activity

首页的项目、最新会话和空列表都支持下拉刷新。刷新通过原加密通道重新请求当前列表，收到后端确认才停止转圈；离线时先尝试重连，失败保留已有列表并提示。项目排序、隐藏和折叠偏好不变。平时仍自动推送更新：日志扫描约 500ms、目录变更检查约 250ms、Claude 桌面目录约 2 秒、目标约 5 秒；日志尚未落盘时刷新不能提前读取。每 20 秒发送应用心跳，45 秒未收到端到端响应则重连（给原生发送等较慢操作留出时间）。

Pull down in Projects, Recent chats or an empty home list to refresh over the existing encrypted connection. The spinner waits for the server reply; offline refresh reconnects first, and failures retain the list with a visible message. Sorting, visibility and collapse preferences remain intact. Automatic updates continue: approximately 500ms log scanning, 250ms catalog-change checks, a two-second Claude desktop catalog interval and five-second goal refresh. Refresh cannot read unflushed agent logs. An application heartbeat runs every 20 seconds and reconnects after a 45-second response timeout, allowing slower native send operations.

会话中的连续思考、工具调用／输出及文件变更归入「执行过程」，默认整体折叠；顶部可全部展开／折叠，也可逐组展开，再查看单项详情。正文、图片和审批单独显示，目录定位会自动展开对应执行过程及记录。发送立即显示本地消息与发送阶段，确认后持续显示「已送达，等待 Agent 响应」，原生消息接替时不重复；不再以普通发送 toast 表示完成。请求失败但可能已执行时显示「未确认送达」，保留草稿，不自动重发。临时发送卡片仅存内存，重新打开会话以工作站记录为准。

Consecutive reasoning summaries, tool calls/results and file changes form collapsed Activity groups. Expand/collapse all groups, open one group, then inspect individual details. Messages, media and approvals remain separate; outline navigation expands the target group and record. Sending immediately shows a local input and its stage, then keeps a delivered/waiting state until native activity arrives. Canonical inputs replace local cards without duplicates, without a normal-send toast. An uncertain failure retains the draft and warns before retry; no automatic resend occurs. Transient cards live only in memory; reopened chats use workstation history.

## 启动密码 / App passcode

在 App **设置 → 启动密码**，输入两次 4 位数字开启。冷启动和从后台返回时需解锁；切到后台时遮住会话页面。修改或关闭密码需验证当前密码。连续输错 5 次后固定冷却 10 分钟，期间正确密码也不能解锁；冷却后重置尝试次数，后续冷却仍为 10 分钟，不递增。失败次数及截止时间保存在系统安全存储，重启 App 不会重置。仅保存随机盐和 PBKDF2-SHA256 校验值，不保存或上传明文密码。忘记密码需清除 App 数据并重新配对，工作站会话不会被删除。

Open **Settings → App passcode**, enter and confirm four digits. Unlock is required on cold launch and after returning from the background; chat content is hidden while backgrounded. Changing or disabling the passcode requires the current code. Five incorrect attempts trigger a fixed 10-minute cooldown, including correct attempts during that period. Attempts reset after the cooldown; later cooldowns remain 10 minutes. Attempts and the deadline survive restarts in platform secure storage. Only a random salt and PBKDF2-SHA256 verifier are stored; no plaintext passcode is saved or uploaded. If forgotten, clear app data and pair again; workstation chats remain intact.

## 持续执行的目标 / Continuous goals

与用量和数量同一行的 **目标** 打开当前 Agent 的目标列表，点击可进入原会话。会话列表显示目标状态，会话内显示条件、状态、已用 token / 预算、用时与最新评估原因。目标通过配对的加密通道每 5 秒刷新，查询不启动推理。删除目标有确认框，保留会话及已有内容；旧手机页面不能删除已被替换的新目标。

**Goals**, beside Usage and the count, lists goals for the selected agent and opens the original chat. Chat rows show goal status; the conversation shows the condition, status, token spend / budget, elapsed time and latest evaluation reason. Goals refresh over the paired encrypted channel every five seconds without inference. Deletion requires confirmation and preserves the chat/history; a stale view cannot delete a replacement goal.

- **Codex**：只读读取 Codex 原生 `goals_1.sqlite`（含 WAL），仅展示已发现的会话；原生 `thread/goal/get` 要求加载会话，因此列表查询不使用会触发加载的接口。电脑目标暂停走原会话的 `user-stop`（先暂停目标调度再中断），删除先暂停原目标，再短暂加载同一个原生 thread，校验目标未变化后调用 `thread/goal/clear`，不 fork、不启动新 turn；恢复电脑目标仍需在电脑操作，因为当前桌面 follower 协议没有恢复目标接口。由 AgentDeck 持有的会话可暂停、删除；尚未验证恢复后的持续目标调度，因此不提供恢复按钮。
- **Claude Code**：读取原生 `goal_status` / `active_goal`，目标记录单独加密索引，历史扫描不导入聊天正文。AgentDeck 持有的进程可中断暂停、发送消息继续、通过 `/goal clear` 清除；电脑 Claude 持有的目标目前只显示状态，并提示在电脑操作。不会新开分支冒充控制原目标。

- **Codex**: reads the native `goals_1.sqlite` ledger in read-only mode, including WAL, restricted to discovered chats. Native `thread/goal/get` requires a loaded thread, so discovery does not load chats. Desktop pause uses the original owner's `user-stop` path (pause scheduling, then interrupt); deletion pauses the original goal, briefly loads the same native thread, checks its identity and calls `thread/goal/clear` without forking or starting a turn. Desktop resume remains a computer action because the installed follower protocol has no goal-resume API. AgentDeck-owned chats support pause and delete. Resume is not offered until continued goal scheduling is verified.
- **Claude Code**: reads native `goal_status` / `active_goal` into a separate encrypted goal index without importing chat bodies. AgentDeck-owned processes can interrupt/pause, continue by message and clear via `/goal clear`. Desktop-owned Claude goals currently show their state and direct you to the computer for controls. No fork is created to simulate controlling the original goal. See [Claude's native goal lifecycle](https://code.claude.com/docs/en/goal).

模型设置中的 Claude Effort 来自本机 CLI 的 `supportedEffortLevels`，与 Codex 的 `supportedReasoningEfforts` 分开。当前本机 Claude 2.1.295 返回 `low / medium / high / xhigh / max`；两者有重叠，但不会把 Codex 的 `none / minimal / ultra` 填给 Claude。发送前后端再次校验所选模型的原生档位。

Claude Effort comes from the local CLI's `supportedEffortLevels`, independently of Codex's `supportedReasoningEfforts`. This machine's Claude 2.1.295 advertises `low / medium / high / xhigh / max`. Overlap is expected; Codex-only values such as `none / minimal / ultra` are not offered to Claude. The backend validates the chosen model's native levels before sending.

## 查看用量 / View usage

连接工作站后，选择 **Codex / Claude →「项目 / 最新会话」下方、与会话数量同一行的「用量」**。显示工作站账号的额度窗口、已用与剩余百分比、数据时间，以及接口提供的重置时间；额度由所有项目共享。点击刷新重新查询（后端有 30 秒短时缓存）。

Connect to the workstation, select **Codex / Claude → Usage on the same row as the chat count below the Projects / Recent switch**. The sheet shows account limit windows, used and remaining percentages, observation time and reset times when supplied. Limits are shared across projects. Tap refresh to query again; the backend caches results for 30 seconds.

Codex 通过本机 CLI `account/rateLimits/read` 查询。Claude 使用 CLI 的实验性 `get_usage` 控制接口；建议更新 CLI。如果实时接口未返回额度，macOS 可以显示同机 Claude 桌面缓存，醒目标记「非实时」及原始时间；缓存账号可能与 CLI 登录不同，重置时间未知时显示「暂不可用」。多组织缓存无法可靠选定账号时拒绝猜测。API Key、第三方供应商或未登录的账号可能不提供套餐额度；不会显示为 0%。

Codex queries the local CLI's `account/rateLimits/read` control method. Claude uses its experimental `get_usage` method; keep the CLI updated. If live limits are absent, macOS can show the local Claude desktop account cache with an explicit **not live** label and its original timestamp. The desktop account can differ from the CLI account; unknown reset times are unavailable. Mixed-organization caches are rejected rather than guessed. API-key, third-party-provider or signed-out accounts may not expose subscription limits; missing data is never shown as 0%.

查询不发送推理消息，不读取会话正文；仅通过现有配对加密通道返回额度字段，不向网关或手机传递登录凭据。

Usage queries submit no inference prompt and read no chat bodies. Only limit fields travel over the existing paired encrypted channel; login credentials are not sent to the gateway or phone.

## 公网连接

| 方式 | 适用情况 | 配置 |
| --- | --- | --- |
| HTTPS 直连 | 手机连接自己的工作站；Envoy 持有域名证书 | [部署指南](docs/deploy/README.md) |
| 飞书网关 | 多租户发现工作站，工作站只需主动连出；网关只中继密文 | [网关指南](docs/GATEWAY.md) |

飞书应用的最小配置：启用网页登录能力，在安全设置登记 `https://你的网关域名/v2/auth/callback`，将使用者加入应用可用范围，并发布应用。App ID / App Secret 只配置在 S-GATEWAY；电脑与手机使用同一个应用、同一个飞书账号登录。

当前仅通过 `user_info` 读取租户标识、用户标识和姓名，**无需额外 API 权限，`FEISHU_OAUTH_SCOPE` 留空即可**。无需通讯录、手机号、邮箱、消息/机器人权限或 `offline_access`。官方接口的权限要求见 [获取授权码](https://open.feishu.cn/document/authentication-management/access-token/obtain-oauth-code)和[获取用户信息](https://open.feishu.cn/document/server-docs/authentication-management/login-state-management/get)；详细配置及跨企业使用限制见[网关指南](docs/GATEWAY.md)。

测试网关地址为 `https://web-test-omnivibe.ttyuyin.com/agentdeck`；电脑 `local-login --gateway-url` 和 App 网关登录都填完整地址。飞书回调为该地址加 `/v2/auth/callback`。CICD 构建、NAS 子目录和 VS 配置见[测试部署指南](docs/deploy/TESTING.md)。

测试网关已配置飞书应用，真实电脑端登录及工作站在线已验证。电脑以 `serve --relay` 启动，再打开下面的本机管理页面；首次在页面中关联网关，已有绑定会自动沿用。App 用同一飞书账号登录、选择工作站，在电脑页面输入手机的校验码并批准。App 重置后需要重新配对。完整步骤见[网关指南](docs/GATEWAY.md)，验收范围见[测试验收记录](docs/deploy/ACCEPTANCE-2026-10-10.md)。

The testing gateway uses `https://web-test-omnivibe.ttyuyin.com/agentdeck`. Enter the full URL, including the prefix, in S-LOCAL and the app. Register this URL plus `/v2/auth/callback` in Feishu. See the [testing deployment guide](docs/deploy/TESTING.md).

The testing gateway now has Feishu configured; real workstation sign-in and its online tunnel have been verified. Start `serve --relay`, then open workstation controls below to link through Feishu; saved associations reconnect automatically. Sign in to the app with the same Feishu account, select the workstation, and enter the phone comparison code on the computer to approve pairing. Resetting the app requires a new pairing. See the [gateway guide](docs/GATEWAY.md) and [acceptance record](docs/deploy/ACCEPTANCE-2026-10-10.md).

### 电脑管理页面 / Workstation controls

本地后端启动后，浏览器直接打开 **[http://127.0.0.1:8787/](http://127.0.0.1:8787/)**。可收藏此地址，刷新或重新打开都能使用；不需要额外命令、启动文件或手工复制访问凭据。

After starting S-LOCAL, open **[http://127.0.0.1:8787/](http://127.0.0.1:8787/)** in your browser. Bookmark, reload and reopen this address directly. No extra console command, launcher file or copied credential is needed.

页面顶部显示 **当前在线 App / 已配对 App / 网关隧道**，下面列出每台 App 的名称、在线或离线状态、本次连接时间或最后连接时间。每 2 秒刷新；在线数量按已认证的实时连接统计，同一设备的多个连接只算一台 App。已配对数量包括离线设备，不包括待确认请求；工作站连上网关不代表手机已经连上。后台休眠会断开连接，无响应的网络连接最迟在服务端读超时（90 秒）后清除。最后连接时间加密保存，重启后仍可查看；读取失败显示「状态未知」，不会伪装为 0 台在线。

The overview shows **Online Apps / Paired Apps / Gateway tunnel**, followed by each App's name, online/offline state and current or last connection time. It refreshes every two seconds and counts authenticated live streams; multiple streams from one device count as one App. Paired totals include offline devices and exclude pending requests. An online gateway tunnel does not imply a connected phone. Background suspension disconnects the App; an unresponsive connection is removed by the server read timeout within 90 seconds. Last connection times are encrypted and survive restarts. Failed reads show an unknown state instead of a false zero.

页面支持关联网关、飞书授权、实时查看在线/重连状态，以及填写手机 12 位校验码批准配对。首次关联不再需要停后端执行 `local-login`。后端尚未关联时，`serve --relay` 正常启动并等待网页关联；已有关联会自动重连。

The page supports gateway enrollment, Feishu authorization, live connection status and approval using the phone's 12-character comparison code. An unlinked `serve --relay` waits for browser enrollment without stopping the backend; existing associations reconnect automatically.

默认固定监听 `127.0.0.1:8787`，只允许本机访问，并保留 Host、Origin、Fetch Metadata 校验和有限管理 API。浏览器自动获取仅存内存的访问凭据，不写 URL 或浏览器存储。此模式信任本机用户和进程，不用于共享、不可信的电脑账号。公网网关不会开放这个页面。端口被占用时会明确报错；部署人员可用 `--console-listen 127.0.0.1:其他端口` 修改端口，或用 `--no-console` 关闭页面。多实例必须使用不同管理端口。

The default fixed listener is loopback-only at `127.0.0.1:8787`, with Host, Origin and Fetch Metadata checks and a restricted API. The browser bootstraps an in-memory capability without putting it in URLs or browser storage. This mode trusts local users and processes; use a trusted workstation account. The public gateway does not expose this page. An occupied port produces an explicit error. Administrators may choose another port with `--console-listen 127.0.0.1:PORT`, or disable controls with `--no-console`. Multiple instances need different control ports.

已关联时，页面提供「解除网关关联」：确认后撤销网关端这台工作站及其手机路由、关闭隧道，清除本机关联凭据和未完成配对；本机会话历史及 Agent 配置保留。随后在页面输入网关地址和工作站名称，点击「连接网关 · 飞书授权」即可重新关联，手机需重新配对。操作需要更新后的网关；网络故障或旧版网关不会被误报为解除成功。

When linked, choose **Unlink gateway** and confirm to revoke this workstation and its phone routes remotely, close the tunnel, and clear local enrollment and unfinished pairings. Conversation history and agent settings remain. Enter a gateway URL and workstation name to link again through Feishu; phones need fresh pairing. This operation requires an updated gateway; network errors and old gateways are never reported as successful unlinking.

本地状态统一保存在 `~/.agentdeck`（默认目录），包括加密数据库、密钥和 `agents.json`；Agent 工具保留在 `~/.agentdeck/tools`。网页显示当前数据目录，避免操作错误实例。

Workstation state uses the default `~/.agentdeck` directory, including the encrypted database, wrapping key and `agents.json`. Agent tools remain under `~/.agentdeck/tools`. The page displays the effective state directory.

`agentdeck console --data <相同目录>` 保留用于独立运行管理页面；使用它时应关闭后端内置页面，或指定不同端口。完整步骤及日志排障见[本机管理指南](docs/WORKSTATION.md)。

The standalone `console` command remains available; disable the backend's built-in controls or use a different port when running it separately. See [workstation controls and diagnostics](docs/WORKSTATION.md).

iOS/macOS 也使用原生客户端，需 Xcode 与对应签名配置；不支持 Flutter Web。仅部署后端时只需 Go。

## 使用前了解

- 电脑已有会话的同步依赖源日志落盘；不代表所有桌面会话都能逐 token 同步。
- 已配对手机可以查看该工作站全部已导入会话，目前没有按项目细分的查看权限。
- App 一次保存一个工作站；网关目前只支持单节点。
- 升级时保留 `--data` 目录；替换后端程序后需要重启服务。

## 开发与文档

`backend/` 是 Go 后端，`app/` 是 Flutter 客户端，`docs/` 是配置和协议文档。完整检查运行：

```sh
./scripts/verify.sh
```

检查包含 Go race/vet、Flutter 分析、协议兼容与合成端到端测试；真实公网和飞书配置仍需在自己的环境验证。

- [Agent 接入与配置](docs/AGENTS.md)
- [模型、语音、图片与文件输入](docs/INPUTS.md)
- [Office 预览范围](docs/OFFICE_PREVIEW.md)
- [加密与授权](docs/SECURITY.md)
- [验收记录](docs/ACCEPTANCE.md)

当前未指定开源许可证。
