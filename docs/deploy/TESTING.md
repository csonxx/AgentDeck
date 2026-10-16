# 测试网关部署 / Testing gateway deployment

本指南记录 AgentDeck 的测试部署配置；部署是否成功及真实飞书登录是否通过，以验收记录为准。

当前验收：0.4.41 双包更新网关已部署并完成 OnePlus 7 App 内双向换包，见[验收记录](../ACCEPTANCE.md)。早期环境搭建记录见 [2026-10-10 测试验收](ACCEPTANCE-2026-10-10.md)。 / Current acceptance: the 0.4.41 gateway and both in-app edition switches passed on OnePlus 7; see the acceptance log.

This guide describes the testing deployment configuration. Deployment and real Feishu sign-in acceptance must be verified separately.

| 项目 / Item | 配置 / Configuration |
| --- | --- |
| CICD | project `83`, service `8209` (`agent-desk`), pipeline `460370` |
| Git | `git@gitlab.ttyuyin.com:azeroth/agentdeck.git`, `main` |
| 测试集群 / Cluster | `k8s-hs-bj-1-test`, namespace `ai-social` |
| 网关 / Gateway | `https://web-test-omnivibe.ttyuyin.com/agentdeck` |
| 飞书回调 / Callback | `https://web-test-omnivibe.ttyuyin.com/agentdeck/v2/auth/callback` |
| 容器 / Container | StatefulSet, 1 replica, HTTP `8080`, `/healthz` probes |
| PVC | `pvc-nas-test-hs-bj-ai-socail-dify-plugin-daemon-ai-social` |
| 独立子目录 / Isolated subdirectory | `subPath: agentdeck-gateway`, mounted at `/data/agentdeck` |

流水线的构建步骤进入 `backend/`，执行 `go vet`、`go test`，再交叉编译 Linux amd64，生成仓库根目录的 `main`；可复用 [build-backend.sh](../../ci/build-backend.sh)。打包配置见 [Dockerfile-packing](../../ci/Dockerfile-packing)，包含访问飞书所需 CA 证书。测试环境自动部署；生产阶段保持手动，不在本次测试发布范围内。

The build runs Go vet/tests in `backend/` and cross-compiles a Linux amd64 executable to `main` at the repository root. The container includes CA certificates for Feishu. Testing deploys automatically; production remains manual and is outside this testing release.

启动命令 / Startup command:

```sh
umask 077
exec /app/agentdeck serve --role gateway --behind-proxy \
  --listen 0.0.0.0:8080 \
  --public-url https://web-test-omnivibe.ttyuyin.com/agentdeck \
  --data /data/agentdeck --rotation 15m
```

PVC 仅保存网关加密数据库、本机密钥和账号/工作站/路由关联，网关不保存 agent 会话正文、图片或 Local/App 根密钥。仅挂载独立子目录，不挂载 Dify 插件数据根目录。保持单副本，禁止 HPA；应验证 NAS 文件锁、写入权限和重启持久性，并为数据库与本机密钥一起做受控备份。

The PVC holds the encrypted gateway database, its machine key, and account/workstation/routing associations. Agent content, images, and Local/App root keys are not stored by the gateway. Mount only the isolated subdirectory. Keep one replica with no HPA; verify NAS file locking, permissions, and restart persistence. Back up the database and machine key together under access control.

初次部署使用 `FEISHU_APP_ID=TODO`、`FEISHU_APP_SECRET=TODO`，随后已配置真实应用；完整回调已登记，电脑端真实飞书登录已通过，健康检查现报告 `feishu_configured: true`。`FEISHU_OAUTH_SCOPE` 保持为空。后续部署新网关时，凭据必须安全注入、成对配置；两个值均为 `TODO` 时登录接口返回 503，健康检查报告 false。不要在仓库、构建命令或日志中放入 Secret。飞书网页应用需登记上表完整回调并发布，使用者需在可用范围内；手机登录与首次配对仍需独立验收。

The initial deployment used `TODO` placeholders. Real application credentials are now configured, the full callback is registered, and actual workstation sign-in has passed; health reports `feishu_configured: true`. Keep the OAuth scope empty. For a new gateway, inject both credentials securely; two `TODO` values disable login with 503. Never place secrets in the repository, build commands, or logs. Register the full callback, publish the application, and include intended users in its availability range. Mobile sign-in and first pairing require separate acceptance.

VS 参考 [testing-vs.yaml](testing-vs.yaml)：仅匹配 `/agentdeck/`，保留前缀，不重写为 `/`；长连接不设总超时，HTTP/1.1 WebSocket Upgrade 需透传。现有域名证书由外层网关管理。公网健康检查地址为 `/agentdeck/healthz`；内部探针用 `/healthz`。无凭据的隧道/账号请求应返回 401，不能只以 200 健康检查代替登录、配对、实时会话和图片验收。

The VS matches `/agentdeck/` and preserves the prefix. Forward WebSocket upgrades and disable the total route timeout. TLS remains at the existing ingress. Use `/agentdeck/healthz` publicly and `/healthz` for internal probes. Unauthenticated tunnel/account requests must return 401. Health alone does not prove sign-in, pairing, live sessions, or images work.

模板省略 `timeout`，使用 Istio 默认禁用请求总超时的行为；本集群 CRD 拒绝显式 `0s`。[Istio 官方说明](https://istio.io/latest/docs/tasks/traffic-management/request-timeouts/)。

The template omits `timeout`, using Istio's disabled-by-default request timeout. This cluster's CRD rejects an explicit `0s`.

电脑与手机连接 / Connect workstation and app:

```sh
agentdeck local-login \
  --gateway-url https://web-test-omnivibe.ttyuyin.com/agentdeck \
  --name '我的工作站' --data "$HOME/.agentdeck"
```

App 选择“通过网关 · 飞书登录”，填写相同完整地址，以同一个飞书账号登录，再按[网关指南](../GATEWAY.md)完成工作站选择和双方校验码配对。旧版不支持前缀的 App 需要更新；工作站需运行含前缀支持的新后端。

Choose gateway/Feishu login in the app, enter the same full URL, and use the same Feishu account. Follow the [gateway guide](../GATEWAY.md) for workstation selection and comparison-code pairing. Upgrade older clients and workstation binaries that only support origins without paths.
