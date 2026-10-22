# 工作站安装与自动更新 / Workstation installation and updates

S-LOCAL 支持 macOS、Windows、Linux，各提供 arm64 和 amd64。下载自己系统的首次安装包，安装一次；以后通过已关联的网关检查更新，无需反复下载、解压或重新配对。

S-LOCAL has macOS, Windows and Linux builds for arm64 and amd64. Install the matching bundle once. Subsequent releases arrive through the gateway and preserve pairing.

## 安装包 / Downloads

0.4.48；安装器无需 Go 或 Git。以下文件已通过匿名 HTTPS 全量下载与 SHA-256 核验；[完整清单及校验值](../releases/local-0.4.48.json)。首次安装后从已关联的网关获取后续版本。

Installers need neither Go nor Git. All downloads were fully verified over anonymous HTTPS; see the [manifest and checksums](../releases/local-0.4.48.json). Subsequent versions come from the linked gateway.

| 系统 / OS | 架构 / CPU | 下载 / Download | 大小 / Size |
| --- | --- | --- | --- |
| macOS | Apple Silicon | [安装包 / Installer](https://xcdn.ukilive.com/mcp_file/custom_file/agentdeck/local/0.4.48/8fe9774ff74a940c2dc8dbff30c0eba3aef7968d8df291cbc890e7c84822051e/agentdeck-local-0.4.48-darwin-arm64.tar.gz) | 7.9 MB |
| macOS | Intel | [安装包 / Installer](https://xcdn.ukilive.com/mcp_file/custom_file/agentdeck/local/0.4.48/e20a2ebf71aaf5bdca06f5dac2b082bea39e3f7519d0727701d364d91ffb646a/agentdeck-local-0.4.48-darwin-amd64.tar.gz) | 8.4 MB |
| Windows | ARM64 | [安装包 / Installer](https://xcdn.ukilive.com/mcp_file/custom_file/agentdeck/local/0.4.48/8402aafcd9db8657b7de7856dabbfc2978c632c32464cf29295ad2d275faaa80/agentdeck-local-0.4.48-windows-arm64.zip) | 7.7 MB |
| Windows | Intel / AMD x64 | [安装包 / Installer](https://xcdn.ukilive.com/mcp_file/custom_file/agentdeck/local/0.4.48/8b41834c165fed309f21ca9b82d2f627a054369538b4bca969d1a007ac8021a4/agentdeck-local-0.4.48-windows-amd64.zip) | 8.6 MB |
| Linux | ARM64 | [安装包 / Installer](https://xcdn.ukilive.com/mcp_file/custom_file/agentdeck/local/0.4.48/d66dc865a268c276f95c327220013820f86cb2f41c9448972f21477c3a59b897/agentdeck-local-0.4.48-linux-arm64.tar.gz) | 7.6 MB |
| Linux | Intel / AMD x64 | [安装包 / Installer](https://xcdn.ukilive.com/mcp_file/custom_file/agentdeck/local/0.4.48/1ea62c7239d399e02fb06c5a9d301e8ee99bf8c851a583c6bf2d392724313c3e/agentdeck-local-0.4.48-linux-amd64.tar.gz) | 8.3 MB |

验收范围：macOS 安装器和登录启动项已实测，Linux 已验证更新进程流程；Windows 两种架构已编译，尚未做 Windows 实机验收。/ Verified: macOS installer/login service and Linux updater runtime. Both Windows architectures build successfully; native Windows acceptance remains pending.

## 使用者 / Users

1. 先在同一个系统账号下安装、登录需要的 Agent，并确认它能独立工作。安装器不会安装或登录 Codex、Claude 等软件。/ Install and sign in to your agents as the same OS user first. The installer does not install or sign in to agents.
2. 解压匹配的安装包。Apple Silicon、Windows on ARM、Linux aarch64 选 arm64；Intel/AMD 选 amd64。/ Extract the matching bundle. Apple Silicon, Windows on ARM and Linux aarch64 use arm64; Intel/AMD use amd64.
3. macOS / Linux 在解压目录运行 `./install.sh`；Windows 双击 `install.cmd`，也可在 PowerShell 中运行 `./install.ps1`。系统可能要求确认下载的软件；只运行管理员提供、校验一致的安装包。/ Run the installation script from the extracted directory. Approve OS software prompts only for a verified bundle from your administrator.
4. 浏览器打开 **http://127.0.0.1:8787/**，连接网关、完成飞书授权，再核对手机配对码。/ Open the console, link the gateway, authorize with Feishu and confirm the phone comparison code.

默认数据目录是用户主目录下的 `.agentdeck`。默认启用已存在的 Codex/Claude 日志目录，并允许在用户主目录内访问项目和附件。需要收窄范围或配置 OpenCode 等 Agent 时，在安装命令最后加 `--`，后接自己的 `serve` 参数。例如：

State lives in `.agentdeck` under your home directory. The default imports existing Codex/Claude logs and allows projects/attachments under your home. To narrow access or configure other agents, append `--` and your own `serve` arguments:

```sh
./install.sh -- \
  --relay \
  --codex-sessions "$HOME/.codex/sessions" \
  --project-roots "$HOME/workspace" \
  --media-roots "$HOME/workspace" \
  --agents-config "$HOME/.agentdeck/agents.json"
```

自定义参数替换整组默认参数；只填实际存在的目录。安装不会自动开启 Claude Desktop 调试器，也不改变 Agent 权限。Windows 的工作站管理可用，不代表所有 Agent 的桌面专属接口都已支持 Windows；详见 [Agent 接入](AGENTS.md)。

Custom arguments replace the entire default argument set. Use existing directories. Installation neither enables the Claude Desktop debugger nor changes agent permissions. Workstation support on Windows does not imply support for every agent's desktop-specific interface; see [integrations](AGENTS.md).

安装时保留当前 PATH，供后台进程找到已安装的 Agent；不复制终端里的密钥环境变量。已有 `.agentdeck/agents.json` 会沿用。/ The installer preserves PATH for native agent discovery and reuses existing `agents.json`; it does not copy terminal secret environment variables.

安装器注册当前用户的登录启动项：macOS LaunchAgent、Windows 计划任务、Linux systemd 用户服务。Linux 需要可用的 systemd 用户会话；没有时用 `--no-start` 安装后自行运行：

The installer registers a per-user login service. Linux requires a working systemd user session; without one, stage with `--no-start` and run the supervisor yourself:

```sh
"$HOME/.agentdeck/bin/agentdeck" local-run --data "$HOME/.agentdeck"
```

Windows 对应程序为 `$HOME\.agentdeck\bin\agentdeck.exe`。不需要管理员权限，也不保存系统登录密码。首次登录前及系统休眠期间，后台服务不会连接。

On Windows use `$HOME\.agentdeck\bin\agentdeck.exe`. No administrator rights or stored OS password are needed. Login services are inactive before login and while the computer sleeps.

### 已经安装过 / Existing installations

只有已经通过本安装器启动的 Local 才能自动切换程序。旧版手动启动的 Local 必须先正常停止，再从新安装包安装一次，保留原 `.agentdeck`。不要删除数据库、密钥或配对，也不要同时启动旧服务和新服务。旧的自建 LaunchAgent/systemd 启动项也需停用，避免抢端口。

Automatic executable switching requires this managed installation. For an older manually launched Local, stop it normally, disable its old startup service, then install once while keeping `.agentdeck`. Do not remove the vault/keys/pairing or run both services together.

### 更新和排障 / Updating and troubleshooting

管理页的「工作站更新」显示当前版本、可用版本、下载进度、最近检查及失败原因；可以立即检查，或关闭「空闲时自动安装更新」。关闭自动安装仍会后台检查和下载。

The console shows current/available versions, progress, last check and failure reason. You can check immediately or disable automatic installation; checking and downloading continue in the background.

- 启动时检查，此后每 6 小时检查一次。下载中断保留进度，下次续传。/ Check on startup and every six hours; interrupted downloads resume.
- 有正在执行的任务、待审批、活动 Goal、重置卡操作或未确认/排队消息时等待，不强制停止任务。/ Wait for active tasks, approvals, goals, reset-card operations and queued/unconfirmed messages; never force-close work to update.
- 切换前再次校验；旧进程确认退出后才启动新进程，防止两个程序同时写数据库。/ Reverify before switching; the old process must exit before a new writer starts.
- 新进程通过本机健康检查并稳定运行 30 秒后确认，随后恢复接收消息。首次历史迁移可能较慢，启动验证最多等 30 分钟；失败回退旧程序，同一失败 build 不再反复尝试。/ Resume message input only after a matching health response and 30 seconds of stability. Allow up to 30 minutes for first-time history migration. Failed builds roll back and are not retried.
- `logs/local.stderr.log` 记录文本级别日志；`local-update-status` 可查看状态。不要把整个 `.agentdeck` 当反馈包上传，其中有数据库和密钥。/ Text logs live in `logs/local.stderr.log`. Inspect state with `local-update-status`; never upload the entire data directory as feedback.

```sh
"$HOME/.agentdeck/bin/agentdeck" local-update-status --data "$HOME/.agentdeck"
```

更新通道使用重新关联后的网关地址；尚未关联时使用安装器里的地址。更新不需要网关解密会话，也不会把历史发送给发布服务。

Updates follow the currently linked gateway, falling back to the installer's address before enrollment. Distribution neither decrypts sessions at the gateway nor uploads history.

## 发布者 / Publishers

程序通过 HTTPS 下载，并校验安装时固定的 **Ed25519 发布公钥、签名、平台、文件大小、SHA-256、递增 build、协议和数据格式版本**。只有 hash 不够：网关被替换时，也不能伪造发布签名。签名私钥只留在发布端，绝不能放进安装包、镜像、Git 或日志。

Downloads require HTTPS and a signature from an installer-pinned Ed25519 publisher, plus platform, size, SHA-256, monotonic build, protocol and state-schema checks. A gateway cannot forge releases. Keep the signing private key on the publisher, outside installers, images, Git and logs.

一次性生成发布密钥，保存工具输出的公钥，私钥另行安全备份：

Create a publisher key once, keep the public output, and back up the private key securely:

```sh
./artifacts/agentdeck local-release-keygen \
  --key-id company-local-2026 --out "$HOME/.agentdeck-publisher/local-2026.key"
```

用该公钥生成六种目标的二进制、签名清单、首次安装包。`--publisher-key` 填 `key-id:base64-public-key`。构建机需要 Go 和 Python；私钥路径只传给本机签名命令，不写入产物。

Build all six targets with the public key as `key-id:base64-public-key`. Go and Python are required on the publisher. The private key path is never embedded in artifacts.

```sh
python3 scripts/build_local_releases.py \
  --version 0.4.48 --build 9064 \
  --gateway-url https://gateway.example.com/agentdeck \
  --download-base https://cdn.example.com/agentdeck/local/0.4.48 \
  --publisher-key 'company-local-2026:PUBLIC_KEY_BASE64' \
  --signing-key "$HOME/.agentdeck-publisher/local-2026.key"
```

将 `artifacts/local/index.json` 指定的文件上传到对应不可变 URL，并用匿名 HTTPS 完整下载核对 SHA-256。安装包供首次安装；更新直接下载签名清单里的原始可执行文件。之后在网关机器通过所有者管理入口发布每个平台的清单：

Upload objects to the immutable URLs in `index.json` and verify full anonymous HTTPS downloads against SHA-256. Bundles are for initial installation; updates download the raw signed executable. Publish each manifest through the gateway's owner-only interface:

```sh
/app/agentdeck gateway-publish-local-release --data /data/agentdeck \
  --manifest /private/path/release-darwin-arm64.json
```

公开查询接口为 `GET /v2/local/releases/latest?os=darwin&arch=arm64`，保留网关前缀。其余系统名为 `windows`、`linux`；架构为 `arm64`、`amd64`。未发布返回 204。公开接口不能发布版本，也不能改客户端的信任公钥。

The public query endpoint is `GET /v2/local/releases/latest?os=darwin&arch=arm64`, including the gateway prefix. Other OS values are `windows` and `linux`; architectures are `arm64` and `amd64`. Unpublished targets return 204. Public routes cannot publish releases or alter client trust keys.

目前只接受兼容的协议/数据格式版本 1；不做不可逆数据库迁移。启动器和业务程序分开保存，更新业务程序不会覆盖正在执行的 Windows EXE。启动器协议升级、发布公钥更换需重新分发可信安装器，不能由网关悄悄更换。

Only compatible protocol/state schema 1 releases are accepted. No irreversible data migration is allowed. The stable launcher and versioned worker are separate, so updates never overwrite a running Windows EXE. Launcher protocol upgrades and trust-key changes require redistributing a trusted installer.

## 让 AI 帮忙安装 / AI-assisted setup

可以把下面这段连同真实安装包位置交给 AI：

Give an AI the following instructions with the actual bundle path:

> 安装 AgentDeck S-LOCAL。先确认系统和 CPU 架构、已有 Agent 登录状态，以及有没有旧 Local 在运行。保留 `.agentdeck`、配对、密钥、会话和原 Agent 权限。阅读本文件，使用匹配的安装器；已有服务正常停止后再迁移，不强杀活动任务。安装后确认 `http://127.0.0.1:8787/` 可打开、更新状态为受管理版本，再由用户完成飞书登录和手机配对。不要上传数据库、密钥或签名私钥。构建通过不代表这台机器已运行成功。
>
> Install AgentDeck S-LOCAL. Check OS/CPU, agent sign-in and any existing Local process first. Preserve state, pairing, keys, chats and native permissions. Follow this document and use the matching installer. Stop any existing service normally before migration; do not kill active work. Verify the console and managed update status, then let the user complete Feishu and phone pairing. Never upload private state or signing keys. A successful build is not proof of runtime acceptance.
