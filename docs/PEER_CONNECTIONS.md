# 直连与网关回退 / Direct connections and relay fallback

App 先通过原有 HTTPS/WSS 网关连接工作站并显示会话，在后台尝试 WebRTC 数据通道。直连就绪后，文字、执行过程、审批、图片和附件一起切换。当前会话、消息顺序、滚动位置和已打开的下载句柄保留。

The app connects through the existing HTTPS/WSS gateway first, then tries a WebRTC data channel in the background. Once ready, messages, activity, approvals, images and attachments move together. The current chat, ordering, scroll position and open download handles survive the switch.

App 首页的连接状态显示「点对点直连 / 网关中继 / HTTPS 直连」，点击可看协商或回退原因。电脑管理页在已配对设备旁显示实际连接方式及当前连接 IP。只有通道确认就绪后才显示直连。

Home shows Peer-to-peer, Gateway relay or Direct HTTPS. Tap the status for negotiation/fallback details. The workstation console shows the actual transport and remote IP for each online app. A pending ICE negotiation is never presented as a direct connection.

## 当前没有 STUN 时 / Without STUN

默认不配置第三方 STUN，也不在 Gateway 开 UDP 端口。仍会尝试局域网和可达的 IPv6 地址；两端位于不同 NAT、UDP 被阻断等情况可能无法直连，自动保留网关中继。未成功的探测不会阻塞会话收发，联网期间约两分钟后再试。

No third-party STUN service or Gateway UDP listener is enabled by default. LAN and reachable IPv6 candidates can still work. Separate NATs or blocked UDP may prevent direct connectivity; the existing relay remains usable. Failed probes do not block chat and are retried after about two minutes while connected.

## 日后配置公网 STUN / Configure public STUN later

在 S-GATEWAY 配置以下环境变量，再重启或发布网关。S-LOCAL 重连后收到配置，新建 App 连接会按该配置探测。两端应升级到支持直连的版本；旧客户端仍走原来的网关协议。

Configure these variables on S-GATEWAY and restart/redeploy it. S-LOCAL receives the policy when its tunnel reconnects; new app connections use that policy. Upgrade both endpoints for direct connections. Older clients continue using the existing relay protocol.

```sh
# 示例地址，需要替换为真正可用的服务。不会自动启动 STUN 服务。
# Example only: replace with a real server. This does not start a STUN service.
AGENTDECK_STUN_SERVERS=stun:stun.example.com:3478
AGENTDECK_P2P_ENABLED=true
```

多个 STUN 地址用逗号分隔，最多四个。当前只接受 `stun:host:port` UDP 地址，不接受 TURN 密码或 HTTP 地址。设 `AGENTDECK_P2P_ENABLED=false` 可让新连接仅使用中继。已建立通道的策略随连接关闭，不在执行中强行切换配置。

Use up to four comma-separated STUN UDP addresses. TURN credentials and HTTP URLs are not accepted. Set `AGENTDECK_P2P_ENABLED=false` for relay-only new connections. Policies are captured per connection rather than changed during active work.

STUN 需要可公网访问、能正确看到客户端源 IP/端口的 UDP 服务，常用端口是 3478。现有 Envoy 的 HTTPS `/agentdeck` 路径仍用于认证和协商，不能用 HTTP 前缀路由代替 STUN UDP 入口。本文的改动没有新建公网 UDP 服务、LB 或防火墙规则。

STUN requires a publicly reachable UDP service that observes the client's actual source address/port, commonly on port 3478. Existing Envoy HTTPS routing remains the authentication/signaling path; an HTTP prefix cannot replace a STUN UDP endpoint. These code changes create no public UDP service, load balancer or firewall rule.

## 加密、撤销和恢复 / Encryption, revocation and recovery

- SDP 和证书指纹通过已经配对的端到端加密会话交换，Gateway 不能替换对端身份。直连继续使用原设备根密钥派生的流量密钥、轮换、序号和重放校验，外层另有 WebRTC DTLS。/ SDP/fingerprints travel inside the paired encrypted stream. Existing device-root traffic keys, rotation, sequence/replay checks remain, with DTLS outside them.
- 切换时两条路径按加密序号合并，短暂乱序有界缓存；大数据按不超过 16 KiB 的块发送，并限制发送缓冲。/ Handover merges the bounded cross-path ordering window by encrypted sequence. Large frames use chunks of at most 16 KiB with send backpressure.
- 原 WSS 连接保留为授权、心跳和撤销通道；直连后不再传会话正文或附件。网关撤销授权、工作站删除配对、控制通道或直连断开都会关闭当前逻辑流。/ WSS stays as the authorization/heartbeat/revocation lifeline, without carrying chat payloads or files after activation. Revocation or loss of either lifeline/direct path closes the logical stream.
- App 随后以新盐和新序号重连中继。待发送消息沿用原请求 ID，由现有发件箱和后端回执去重；有执行结果不明的请求不会盲目重新执行。附件按原版本和偏移恢复。直连中断后留出冷却时间，避免来回切换。/ Reconnect uses fresh crypto state. Durable outbox/request IDs retain backend deduplication; uncertain actions are not blindly re-executed. Downloads resume using file version and offset. A cooldown prevents route flapping.

## 验证 / Verification

```sh
scripts/verify.sh
# 仅操作你明确指定的测试手机，使用独立临时 App ID 和合成会话。
# Uses an explicitly selected phone, a separate temporary app and synthetic data.
python3 scripts/verify_peer_native.py --device YOUR_ADB_SERIAL
```

原生测试覆盖：网关首次连接、无 STUN 的真实数据通道、图片、跨切换的下载句柄、直连断开自动回退、附件续传及撤销。测试程序和模拟飞书路由只存在于 `test_support` / `testsupport`，不进入正常 App 入口或生产后端。不同运营商 NAT 的公网打洞成功率，需要配置实际 STUN 后另行验证；不能用局域网成功代替。

Native acceptance covers initial relay, an actual STUN-free data channel, images, download-handle continuity, automatic fallback, resumable downloads and revocation. Test entry points and synthetic Feishu routes are separate from production binaries. Carrier/NAT traversal still needs acceptance against a real public STUN service; LAN success is not evidence of WAN traversal success.
