# 使用手册 / Setup and operation

[返回首页 / README](../README.md)

从源码运行工作站、连接手机、自建网关及发布更新的步骤。现有安装保留数据目录、密钥和配对；只修改需要的配置。

Instructions for workstation setup, phone pairing, self-hosting and update publication. Keep the existing state directory, keys and pairing when upgrading.

所有命令从仓库根目录执行，特别注明的除外。让 AI 协助安装时，提供系统、项目目录、启用的 Agent、连接方式和网关地址；已有安装说明保留数据。登录、配对校验和系统安装确认由用户完成。凭据通过本机安全配置提供。

Run commands from the repository root unless stated otherwise. For AI-assisted setup, provide the OS, project root, enabled agents, connection mode and gateway URL, and state whether an existing installation must be preserved. The user completes sign-in, code comparison and OS installation approval; credentials stay in protected local configuration.

需要后台常驻和网关自动更新，优先使用[工作站安装器](LOCAL_UPDATES.md)，无需自行维护启动脚本。下面的源码 `serve` 适合开发或手动管理，不会自动替换程序。

For managed background operation and gateway updates, use the [workstation installer](LOCAL_UPDATES.md). The source `serve` setup below is for development/manual management and does not replace its own executable.

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
git clone https://gitlab.ttyuyin.com/azeroth/agentdesk.git AgentDeck
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
test -d "$PROJECTS_ROOT" || exit 1
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
test -d "$PROJECTS_ROOT" || exit 1
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

直连和 relay 选一种启动同一数据目录，不要同时运行两个进程。直连工作站经公网 HTTPS 使用时，见 [Envoy/TLS 部署](deploy/README.md)。/ Run one mode/process per data directory. For direct public HTTPS, use the Envoy/TLS deployment guide.

## 连接手机 / Connect the phone

### 安装 / Install

手机下载上方对应 APK，按 Android 提示允许该安装来源并安装。USB 安装也可使用 / Download an edition on the phone and approve its Android install source, or install with ADB:

```sh
adb -s DEVICE_SERIAL install -r /absolute/path/to/agentdeck-0.4.47-arm64-v8a-lite.apk
```

签名不一致会拒绝覆盖；不要通过自动卸载来“修复”，卸载会丢失手机配对和设置。/ A signing mismatch prevents replacement. Do not automatically uninstall to work around it; uninstalling loses phone pairing and preferences.

### 网关配对 / Gateway pairing

1. 手机选「通过网关 · 飞书登录」，填写与电脑完全一致的 HTTPS 地址和前缀。/ Select gateway/Feishu sign-in and enter exactly the workstation's gateway URL, including its prefix.
2. 用**同一飞书账号**授权，选择在线工作站；同一手机可点授权链接，无需扫描自己屏幕的二维码。/ Authorize as the **same Feishu account**, then select the online workstation. On one phone, follow the authorization link rather than scanning its own QR.
3. 电脑打开 http://127.0.0.1:8787/，将**手机显示的 12 位十六进制校验码**填入待配对请求，点击校验并批准。/ Enter the **phone's 12-character hexadecimal code** in the local console and approve that request.
4. 手机确认两边一致；等待中的手机会在电脑批准后继续。确认首页显示「实时连接」，并能打开真实会话。/ Confirm the code matches on the phone; a waiting phone proceeds after approval. Verify Live connection and real chat history.

飞书登录只确认归属，不代替端到端配对；网关绑定完成不等于手机已配对。请求过期后重选工作站即可，不需要重置后端。/ Feishu establishes ownership, not the end-to-end pairing key. Gateway enrollment alone does not pair a phone. If a request expires, select the workstation again without resetting backend state. [详细流程 / Detailed workflow](WORKSTATION.md)

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

没有认证的回环示例只供本机可信用户；有密码时使用 `password_env` 指向安全注入的环境变量，OpenCode 和 S-LOCAL 必须继承同一密码。/ The unauthenticated example assumes trusted local users. For authentication, use password_env and inject the same password into both processes. [完整配置、WorkBuddy 权限和授权 / Full integration instructions](AGENTS.md)

Claude Desktop 原会话发送是实验功能，默认关闭，需要按版本重新验证。不要为了安装 AgentDeck 自动退出登录、改隐私设置或开启调试器。需要这项能力时由所有者按[本机管理指南](WORKSTATION.md#claude-app-原会话发送--original-claude-chats-in-the-app)明确启用并重新验收；9229 只允许回环，不能经过公网代理。

Claude Desktop input is experimental, off by default and requires validation for each version. Installation must not sign the user out, change privacy settings or automatically enable debugging. Owners requiring it should explicitly follow the workstation guide and revalidate. Never publish the loopback debugger on port 9229.

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
2. 在网关安全环境文件中注入 `FEISHU_APP_ID` / `FEISHU_APP_SECRET`，不是在手机或 Local 填。当前登录只读取身份，不需要机器人/通讯录功能；scope 及应用类型要求见[飞书指南](GATEWAY.md)。/ Inject the application credentials into the gateway's protected environment, never the phone/Local. Identity-only login does not use bots/address-book features; see the detailed guide for scopes/application availability.
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

完整模板 / Templates: [Gateway](GATEWAY.md) · [Envoy](deploy/README.md) · [gateway.env.example](deploy/gateway.env.example) · [systemd](deploy/agentdeck-gateway.service)

## 日常启动停止与数据 / Service and state management

- **启动/停止 / Start/stop:** 示例前台进程使用 Ctrl+C 正常停止。常驻 Linux 可按实际路径安装 [Local systemd user service](deploy/agentdeck-relay-local.service)；macOS 使用自己的 LaunchAgent，写实际绝对路径、Agent CLI PATH、日志位置，并以登录 Agent 的用户运行。不要把本仓库开发者的电脑路径复制到其他机器。/ Foreground examples stop with Ctrl+C. Use the supplied Linux user-service template or a workstation-specific macOS LaunchAgent with real absolute paths, CLI PATH and logs.
- **本机管理 / Console:** http://127.0.0.1:8787/；改端口用 `--console-listen 127.0.0.1:PORT`。只监听回环，不能交给公网代理。/ Loopback only; never expose this administration interface through a public proxy.
- **数据 / State:** Local 默认 `~/.agentdeck`，Gateway 单独 `~/.agentdeck-gateway`；同一目录只运行一个后端。升级不换目录；迁移前正常停止并成对备份 `vault.db` 和 `storage.key`。/ Separate Local/Gateway state; one process per directory. Preserve directories on upgrade and back up database/key together after a clean stop.
- **App 管理 / Devices:** 电脑管理页可改名、查看在线 IP、撤销配对；手机设置也可改名。撤销使旧手机密钥失效，需要重新配对，历史保留。/ Rename, inspect online IP and revoke from the console; the phone can rename itself. Revocation invalidates the device key, requiring a new pairing without deleting history.
- **网关解绑 / Unlink:** 在电脑页解除关联；网关验证撤销并回执后清除本机关联。重新关联获得新身份，手机需重新选工作站及配对。网关不可用时保留原关联供重试。/ Unlink waits for authenticated gateway revocation; re-enrollment requires phone selection/pairing again. Failed unlink keeps the original association.
- **保活 / Availability:** Mac 锁屏不等于休眠。电脑休眠、退出 Agent 或停止 Local 会影响执行/同步；不要用修改 Agent 登录和权限解决网络问题。/ Screen lock and sleep differ. Sleep, agent exit or stopping Local interrupts service; do not alter credentials/permissions to fix transport issues.

## Codex 额度与重置卡 / Codex usage and reset cards

此功能需要更新 App 和 S-LOCAL，并使用支持重置卡信息的 Codex CLI。/ Update both the App and S-LOCAL, and use a Codex CLI that exposes reset card information.

在首页选择 Codex → **用量**，可查看可用重置卡的标题、说明、获得和到期时间，以及官方领取／使用记录。点「使用这张」后，App 会重新核对电脑登录账号和额度，再弹出确认；只有确认后才会使用一张。五小时或每周核心额度剩余不超过 10% 时可用，与电脑端条件相同。/ Select Codex → **Usage** on the home screen for available cards, descriptions, grant/expiry dates and official received/used history. Selecting a card checks the desktop account and fresh usage before asking for confirmation. Only confirmation uses a card. Either the five-hour or weekly core limit must have 10% or less remaining, matching the desktop requirement.

发生断网或结果未知时，先刷新记录和额度，再用「核对原请求」处理。App 不会自动重试使用重置卡；手动核对会再次确认，并沿用同一请求编号，避免重复消耗。电脑 CLI 太旧或官方接口未返回字段时会显示暂不可读，不会将未知数量当作零。历史通过电脑登录态读取，手机和网关不会收到 Codex 登录凭据。/ If disconnected or the outcome is unknown, refresh history and usage before checking the original request. The app never retries card usage automatically. A manual retry asks again and preserves the original request ID. Missing fields or an old CLI are shown as unavailable, not zero. History is read using the workstation login; Codex credentials never reach the phone or gateway.

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

网关默认文本 INFO 日志，连接变化立即打印；每 30 秒输出 `connection_stats`（在线工作站、App 流及请求数）。`--log-level debug` 可辅助排障，仍不应记录凭据或会话正文。代理后真实 IP 仅信任 `AGENTDECK_TRUSTED_PROXY_CIDRS` 指定的来源。/ Gateway INFO text logs include immediate connection changes and a 30-second connection_stats summary. Debug logging remains credential/content safe; forwarded IPs require explicitly configured trusted proxy CIDRs. [日志与本机排障 / Diagnostics](WORKSTATION.md)

0.4.47 的 App 与 S-LOCAL 会用 `request` 关联 `session_open`（本地排队、历史读取、写出）和 `session_open_client`（发送前等待、往返、接收排队、解密、应用数据、首帧显示）。计时使用手机单调时钟；日志只包含会话编号、耗时和数据量。排查“已连接但点开慢”时对照这两条记录，不能把本地读取耗时当作手机实际打开耗时。/ App and S-LOCAL 0.4.47 correlate `session_open` and `session_open_client` by request ID. They separate Local processing from client send/round-trip/receive-queue/decode/apply/first-paint time. Client timings use a monotonic clock; logs contain identifiers, durations and sizes only. Compare both records when a connected phone opens slowly; Local read time alone does not measure phone opening time.

完整验收至少检查：手机看到真实历史 → 专用测试会话发送且在目标 Agent 原会话执行 → 输出/图片同步 → 后台恢复和断线补齐 → 需要时完整审批 → 已配对设备撤销。没有真实授权的 Agent 或没有现场执行的分支，标为未验收。/ Acceptance requires real history, execution in a dedicated intended chat, live output/images, background/reconnect recovery, applicable approval and device revocation. Mark unexercised agents/branches explicitly as unaccepted.

## 自行构建与开发 / Build and develop

上方公开 APK 使用开发测试签名。自建版本需要固定自己的签名；签名不同不能覆盖安装，不要通过自动卸载处理，否则会丢失手机数据。语音模型许可见 [说明](CLIENT_VOICE_LICENSE.md)。

Published APKs use a development signing key. Keep a stable signer for self-built releases; a different signer cannot replace an existing installation. Do not uninstall automatically, as that loses phone data. See [speech licensing](CLIENT_VOICE_LICENSE.md).

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
