# 安全协议 v1

## 信任与配对

后端由工作站所有者运行。`control.sock` 是 0600 Unix socket，目录为 0700。生成配对、流式导入、枚举设备和撤销设备只能通过这个本机入口；直连模式公网只有 health、pair、session、stream；网关公开路由见 [三角色协议](GATEWAY.md)。

每个邀请有 32 字节随机 token、独立 X25519 密钥对、5 分钟过期时间。配对 JSON 带 HTTPS origin、TLS 信任模式、邀请公钥和 token。直接 TLS 默认 `tls_trust: pin`（旧配对码缺省同此），绑定服务器 TLS **SubjectPublicKeyInfo SHA-256**，HTTPS 与 WSS 均验证 pin 和有效期；同私钥正常续期保持 pin，换私钥须重新配对。

Envoy 终止 TLS 时，`--behind-proxy` 默认生成 `tls_trust: system` 且 pin 为空；App 使用平台根证书验证外部证书链、域名和有效期，不设置证书放行回调。外部证书私钥更新无需重新配对。`--public-tls-pin` 可改为显式绑定 Envoy 公钥。未知信任模式、空 pin 模式、system 与 pin 冲突均拒绝；App 不接受 HTTP/WS 公网 origin。HTTPS 请求禁止重定向。

代理模式 Go 使用 HTTP/WS，仅部署于回环或受限私网，授权仍由设备 root 和一次性 ticket 校验，不信任 forwarded headers；外部 URL 只由显式配置决定。Envoy 不持有设备 root，正常会话正文/图片仍是 App 到 Go 的 AEAD 密文；新版 App 将初次配对 token 放在派生 root 的 AEAD 内，设备 ID、邀请哈希、公钥、握手长度和时序可被 Envoy/上游网络观察；旧直连客户端仍可能发送明文 token，因此这些组件属于配对信任边界。跨不可信网络的上游应额外使用 mTLS/隧道。

后端原子消费邀请；重复、过期、未知设备、X25519 低阶点拒绝。设备 root 为：

```
HKDF-SHA256(ikm = X25519(clientPrivate, invitePublic), salt = invitationToken,
            info = "agentdeck/pair/v1", length = 32)
```

邀请私钥不发给手机；完成配对后邀请记录删除。root 在后端作为加密 device value 保存，在手机存入 Keychain / Android secure storage。配对码授权可信手机读取已导入会话，并操作 project-roots 范围内的 agent 项目。

## 连接与轮换

直连时间源为 S-LOCAL；有 S-GATEWAY 时，Local→Gateway 隧道、Local↔App 会话以及 App 的路由签名统一以 Gateway 为准。Gateway 的时间在进程启动时取系统时间锚点，此后单调推进；Local 与 App 使用受身份密钥保护的时间响应建立对应锚点，连接内不修改它。Local 经隧道传入的时间锚点保存在握手 ticket 内，因此重连后的新锚点不会改变旧连接的 epoch。Gateway 离线时 relay Local 的新配对/批准失败，不回退本机时间。

`POST /v1/time-sync` 使用 device root；`POST /v2/time-sync` 使用 local enrollment、grant、account 或 flow 密钥，各身份有独立 AAD scope。请求 AEAD 固定 epoch/seq=0，携带随机 challenge，服务端先验证密钥、身份有效性、路由、nonce 重放与限流；不要求调用者时间戳，以免时钟偏差导致无法校准。响应 AEAD 包含 challenge、server_time_ms、version=1；客户端验证后用单调 RTT/2 估计传输耗时，并拒绝超过 10 秒的 RTT。该近似不能保证毫秒精度，安全边界不依赖时间响应裸文本。

App 后台关闭连接，恢复前台重新校准并生成新连接；未确认的发送不会自动重放。Local 隧道重连也重新校准。飞书 flow/account 的到期和签名使用 Gateway 时间。配对码由服务器权威检查到期，App 不再用未经校准的手机时间预先拒绝。校时 nonce 防重放记录保留 2 分钟，客户端每次产生新挑战并拒绝旧响应。网关配对邀请的绝对到期时间由 Gateway 根据最多 300 秒 TTL 发放，Local 使用返回的同一到期值。校准失败拒绝连接；不放宽原有 hello ±60 秒、相邻 epoch、严格 next seq 和证书校验。协议校时不修改操作系统时间，系统时钟超出 TLS 证书有效期仍须修复系统时间。


1. `/v1/session` 接收 device ID 与 root 加密 hello（timestamp、随机 challenge）。接受时钟偏差 ±60 秒。重复 nonce 在 2 分钟重放窗口内拒绝。
2. 后端返回 root 加密 welcome，绑定 challenge，含 30 秒单次 WebSocket ticket、新的 32 字节随机连接 salt、轮换间隔。
3. WSS ticket 在 Authorization header 中；不放 URL。后端拒绝浏览器 Origin，连接上限 32，未消费 ticket 上限 256，握手全局 120 次/分钟。
4. 每个方向、每个 epoch 使用独立 AES-256-GCM key：

```
epoch = calibratedMonotonicMilliseconds / rotationMilliseconds
HKDF-SHA256(root, sessionSalt,
  "agentdeck/traffic/v1/{s2c|c2s}/{epoch}", 32)
```

默认每 15 分钟更换 traffic key，每次重连更换 salt。配对 root 是长期设备信任锚，不随 epoch 改变，因此不需要反复扫码。撤销删除后端 root 并在约 250ms 内关闭既有连接；后续连接拒绝。配对 root 泄露时须撤销后重新配对。

所有 AEAD frame 包含 epoch、严格递增方向 seq、随机 12 字节 nonce、ciphertext + 16 字节 tag。AAD 为 `agentdeck/v1/{scope}/{epoch}/{seq}`。接收只允许相邻 epoch、精确 next seq；方向密钥独立。内容、历史、图片、会话列表、错误详情均在 AEAD 内。health、设备 ID、帧长度/时间和 IP 不是隐藏元数据。AES/HKDF/X25519 使用 Go/Dart 库，不自实现密码学。

这实现了定期流量密钥更新，**不是具备前向保密的双棘轮协议**：如果长期 root 后来泄露、攻击者同时保存了 salt 和历史密文，就可能解密相应旧流量。直接模式及参考 Envoy 配置的 TLS 1.3 另提供公网段传输保护。

## 静态存储

后端 bbolt 所有持久 value（会话、事件、媒体、设备、邀请、offset）都由随机 storage.key 使用 AES-256-GCM 加密；AAD 绑定 bucket/key。数据库 key 包含随机设备/会话 ID、事件序号和哈希，并不是全盘元数据加密。storage.key 与 vault.db 为 0600，目录 0700。能够以工作站所有者身份读取 storage.key 的进程属于信任边界内；源 agent 自己的原始日志不会被改写或加密。

手机安全存储持有 device root 和独立 cache key；缓存文件只有 AES-GCM envelope，写入临时密文后原子替换。缓存保存完整 snapshot 避免截断 delta 后丢失回答开头。忘记设备先清缓存（含临时文件）再清安全存储。Android 禁止应用备份。图片只保留在内存。

## 导入边界

只导入显式配置目录和显式 JSON stdout 桥接。跳过系统消息、开发者指令、world_state、认证/账户事件、encrypted_content。展示文本做常见 API key/JWT/Bearer/密码/签名 URL 脱敏，**该规则不能保证识别任意秘密字符串**，因此已配对手机必须属于可信所有者。代码/工具可能包含业务信息。

媒体只允许配置 root 中的普通文件，root-scoped 文件操作防止符号链接越界，大小与 MIME 都校验。后端不抓取远程 URL；手机直接读取公开 HTTPS 图片，不携带配对凭据。网页点击由手机浏览器处理。本地图片/UTF-8 文本链接预览必须在当前会话出现、位于 media-roots 内，普通文件且图片不超过 8 MiB/文本不超过 1 MiB；拒绝已知凭据文件，文本继续脱敏。附件明文只保留在内存，不生成公网文件 URL。源记录上限 16MiB，单文本上限 128KiB；过大内容明确跳过/截断。移动端通过加密帧启动项目内的 Codex/Claude 会话、发送消息、停止任务和确认审批；没有独立终端或任意 shell API。项目必须在 project-roots 范围内，符号链接解析后重新验证；不使用 bypassPermissions/danger-full-access。

## 尚需目标环境验证

需要实际公网证书/域名下的连接测试、应用商店签名配置、iOS 真机 Keychain/扫码/锁屏恢复验收。网关提供租户及账号归属隔离；按项目细分授权、硬件绑定与 root 双棘轮尚未实现。后端存储目前由主机权限保护 wrapping key，未接入 HSM/系统 Keychain。

## 三角色网关补充

见 [GATEWAY.md](GATEWAY.md)。Feishu 的 `tenant_key` 与同一应用的 `open_id` 确定工作站归属，授权请求使用 state、PKCE S256 和绑定浏览器的 Secure/HttpOnly/SameSite Cookie。登录流 10 分钟，手机账号凭据 30 天；过期账号须重新飞书登录以发现/绑定新工作站，已配对会话访问由独立长期 grant 管理，直至撤销。

Local enrollment key、App routing grant 和会话 root 相互独立。飞书登录交付的 enrollment/account secrets 用临时 X25519 派生 key 加密；网关与 Local 隧道再使用独立 AEAD、方向 key、连接 salt、epoch/seq 校验。会话正文、工具、媒体始终在 Local/App root 派生的内层 AEAD 内，网关保存的 routing/enrollment secrets 不能解密。

飞书只解决身份发现。首次会话密钥通过双方可信屏幕上的 48-bit 校验码验证，并由 Local owner-only socket 批准；未批准/不一致/过期均拒绝。请在可信 Local 终端实际核对手机显示，不能由网关代替确认。每个手机独立 DH/root，只解自己的访问流；一个可信手机有权读取该工作站全部已导入会话，并操作该工作站显式授权的 agent 项目。长期 root 泄露仍须撤销重配，定期 traffic key 更新不提供 root 泄露后的前向保密。

网关能观察租户/工作站/设备标识、连接时间和帧长度，能中断/延迟服务；它不持有会话 root。飞书 OAuth callback code/Cookie 受 HTTPS 和 Envoy 到 Go 的受限私网保护，不能把整个代理私网都称为独立端到端密文。跨不可信上游须配置 mTLS 或安全隧道。网关数据库 value 和所有待配对/登录结果加密保存，storage.key 仍需主机权限保护。

账号撤销同时撤销该登录发放的全部 grant 并关闭连接；grant 撤销只影响对应手机；Local 撤销断开该工作站并拒绝重新连入。Local owner 可另撤销本机 device root。飞书离职/禁用状态目前没有订阅自动同步，管理员须显式撤销 AgentDeck 凭据。

## 多 Agent 授权边界 / Multi-agent authorization boundary (0.4.0)

`--agents-config` 仅由 S-LOCAL 所有者管理，普通文件、0600、最大 64 KiB、拒绝符号链接；其中保存端点、固定可执行程序及环境变量名称，不保存 provider 密钥值。手机的 Agent catalog 只包含名称、能力、配置状态和通用提示。网关不加载 adapter 配置。

The S-LOCAL owner controls `--agents-config`: regular file, mode 0600, maximum 64 KiB and no symlinks. It holds endpoints, fixed executables and environment-variable names, not provider secret values. The phone catalog exposes names, capabilities, configuration state and generic hints. Gateways do not load adapters.

OpenCode 使用工作站 HTTP/SSE（HTTP 仅回环，远端 HTTPS），不跟随重定向；可用环境变量 Basic authentication。新建、发送不覆盖 provider 的权限配置。Codex/Claude 在手机没有明确选择权限时，新建与恢复会话不强制设置 approval/sandbox/permission mode，交由原生配置解析；这会遵循所有者已设置的自动允许模式。ACP 使用所有者配置的固定命令和原生权限模式，转发真实权限请求，不为客户端终端另加重复审批；单次审批不自动提升为永久允许。ACP 文件操作通过 Go `os.Root` 限定当前项目，终端保留会话身份、工作目录及输出/数量限制。CLI 本身有其自己的模型网络与账号存储边界。

OpenCode uses workstation HTTP/SSE (HTTP on loopback only, HTTPS remotely), rejects redirects and optionally uses Basic authentication from an environment variable. Creation and prompts do not override provider permissions. Codex/Claude creation and resume omit approval/sandbox/permission-mode overrides, delegating to native settings, including owner-configured automatic permission modes. ACP uses the owner's fixed commands and native policy, forwards real permission requests and adds no duplicate terminal approval; one-time approval is never promoted to permanent approval. Client file operations remain within the project through Go `os.Root`; terminals retain session identity, working-directory and output/count bounds. Each CLI also has its own model-network and account-storage boundary.

WorkBuddy client secret 仅留在 Local 环境。OAuth code/state 回调仅用于一次性授权，不回传手机；响应禁止缓存/Referer。令牌及每账号同步游标在 Local vault 加密保存。provider 下行只导入 user/assistant 展示记录，不导入账户/认证/系统提示。WorkBuddy 的官方服务会处理其本地助理通信，因此这一路不作“官方服务也无法看到内容”的端到端声明；AgentDeck 的 Local/App AEAD 和网关密文隔离仍成立。

WorkBuddy's client secret stays in the Local environment. OAuth callbacks consume code/state once without returning codes to the phone; responses disable caching and Referer propagation. Tokens and account-specific cursors are encrypted in the Local vault. Only user/assistant presentation records are imported. WorkBuddy's official service handles Local Assistant communication; no claim is made that its service cannot read content. AgentDeck's Local/App AEAD and gateway ciphertext isolation still apply.


## 手机修改原生会话权限 / Native session permission changes

权限读取及修改均通过已经配对、AEAD 认证的会话协议，继续校验当前会话、授权项目和原生执行端身份。修改只接受服务器提供的模式列表，并携带当前权限版本，过期选择被拒绝。Codex/Claude Desktop 使用原会话的原生 setter，不修改全局配置或创建分支；OpenCode 追加当前会话规则并按原生末项优先语义读回；ACP 只接受 Agent 声明的会话模式。

Permission reads and changes use the paired AEAD-authenticated protocol with session/project/native-owner checks. Only advertised modes and the current permission version are accepted. Desktop setters operate on the original chat without changing global config or creating branches. OpenCode rules remain session-scoped; ACP accepts only agent-advertised modes.

Managed CLI 会话的明确选择保存在现有加密 `agentconfig` 数据中；只为这些明确选择在重连时恢复权限。Claude 的 `--allow-dangerously-skip-permissions` 仅允许用户通过原生 setter 明确切换到 bypass，并不启用 bypass，也不覆盖默认权限。原生账号、组织及沙箱策略仍可拒绝修改；AgentDeck 不自行回答已有工具审批。没有取得确认时，App 显示失败或未提供，而不是伪造已生效状态。

Explicit managed-CLI choices are encrypted in existing agent configuration and replayed only for those sessions. Claude's allow-bypass flag makes the native user-selectable mode available; it does not activate bypass or override defaults. Native account, organization and sandbox restrictions remain enforced. AgentDeck does not answer existing tool requests as a side effect of its setter. Missing acknowledgement is not presented as success.
