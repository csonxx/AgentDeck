# 公网部署

在运行 Codex/Claude 的机器上安装后端，以能读取 agent 日志的同一个受信任用户运行。配对、撤销、导入仍通过同用户的 0600 `control.sock`。没有执行实际公网部署。

三角色中继部署见 [飞书网关指南](../GATEWAY.md)：公网 Envoy 指向 S-GATEWAY，S-LOCAL 仅主动连出。下面命令演示直连工作站模式。

## Envoy 终止 TLS（你的部署方式）

```text
Flutter App ── HTTPS/WSS :443 ── Envoy（域名证书）
                               └── HTTP/WS :8080 ── AgentDeck Go
```

Go 无需证书或私钥；配对码仍使用外部 HTTPS origin。默认 App 使用平台信任根验证 Envoy 证书链、域名和有效期，正常续期/换证书私钥无需重新配对。后端和手机之间的 X25519 配对、AES-GCM 帧加密、15 分钟流量密钥轮换及静态加密保持启用。

```sh
agentdeck serve \
  --behind-proxy \
  --data "$HOME/.agentdeck" \
  --listen 127.0.0.1:8080 \
  --public-url https://agent.example.com \
  --codex-sessions "$HOME/.codex/sessions" \
  --claude-sessions "$HOME/.claude/projects" \
  --media-roots "$HOME/workspace" \
  --rotation 15m
```

Envoy 与 Go 不同机器或容器时，可改监听到私网地址或容器 `0.0.0.0:8080`，必须用防火墙/NetworkPolicy 仅允许 Envoy 到上游；HTTP 上游不能开放公网。跨不可信网络时，另用隧道或服务网格 mTLS 保护该段。新版 App 配对 token 已在 AEAD 内；旧 App 的 token 与设备 ID 会经过代理，因此 Envoy 和该私网属于信任边界。

[`envoy.yaml`](envoy.yaml) 是独立 Envoy 参考配置：证书留在 Envoy、HTTP/1.1 上游、WebSocket upgrade、流路由 `timeout: 0s`、120 秒空闲超时（App 心跳 20 秒）。已有 Envoy 时将对应 virtual host/route/cluster 合入现有配置，保留自己的证书/SDS 管理。无需额外开放 Go HTTPS。不要用 buffer filter 缓冲 WebSocket，不要移除 Authorization；关闭配对与会话请求的重试，避免重复消费一次性 token/ticket；访问日志不要记录 Authorization、X-Relay-Auth、Cookie、请求体或 OAuth callback 查询参数。参考 [Envoy upgrade 文档](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/http/upgrades) 与 [超时文档](https://www.envoyproxy.io/docs/envoy/latest/faq/configuration/timeouts)。

后端不依赖 `X-Forwarded-Proto`、`X-Forwarded-Host` 来生成配对地址，也不把这些 header 当成授权依据，公开地址仅来自 `--public-url`。路径必须原样转发 `/v1/pair`、`/v1/session`、`/v1/stream`、`/healthz`。网关模式还需转发 `/v2/auth/*`、`/v2/account/*`、`/v2/local/tunnel`、`/v2/local/grants`、`/v2/relay/*`；当前不支持 URL 子路径前缀。

若 Envoy 用自签证书，或需要额外绑定公网证书公钥，传 `--public-tls-pin <64位SPKI SHA256 hex>`。仅需从受信任证书文件算 pin，无需把私钥复制给 Go：

```sh
openssl x509 -in envoy-fullchain.pem -pubkey -noout | \
  openssl pkey -pubin -outform DER | openssl dgst -sha256
```

配置 pin 后，App 验证该公钥及证书有效期；Envoy 所有节点必须使用同一公钥。更换私钥需要重启后端更新 pin 并重新配对。`--behind-proxy` 不可与 Go 的 `--cert/--key` 同时使用。

## Go 直接终止 TLS

仍支持独立部署，或外层仅 TCP 透传：

```sh
agentdeck serve \
  --data "$HOME/.agentdeck" \
  --listen 0.0.0.0:8443 \
  --public-url https://agent.example.com:8443 \
  --cert "$HOME/.agentdeck/certs/fullchain.pem" \
  --key "$HOME/.agentdeck/certs/privkey.pem" \
  --codex-sessions "$HOME/.codex/sessions" \
  --claude-sessions "$HOME/.claude/projects" \
  --media-roots "$HOME/workspace"
```

该模式默认绑定 Go 证书 SPKI，同私钥续期无需重新配对，换私钥需重新配对；重启 Go 加载证书。无显式证书时只允许回环自签 TLS 测试。

## 配对与部署验收

`agentdeck pair --out /tmp/agentdeck-pair`，手机扫码或粘贴 JSON 后删除一次性文件。旧版 App 不识别 `tls_trust: system`，需要更新 App。已经保存的直接 TLS/pin 配对继续按原模式验证；迁移到不同公钥的 Envoy 时重新配对一次。

[`agentdeck-user.service`](agentdeck-user.service) 默认使用 Envoy 上游模式，替换程序路径、域名与媒体目录后安装。手机通过公网移动网络验证新消息、工具输出、图片、重连和 Envoy 续证；本地测试不能代替实际 Envoy/公网验收。服务端数据目录不变，设备授权和事件继续保留；后端关闭时未镜像的 bridge 事件需要原日志源补齐。
