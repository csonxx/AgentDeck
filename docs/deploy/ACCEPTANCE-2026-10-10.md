# 测试部署验收 / Testing deployment acceptance

验收日期：2026-10-10，Asia/Shanghai。测试网关已发布；真实飞书扫码尚未验收，凭据按要求保留 `TODO`。

Verified on 2026-10-10, Asia/Shanghai. The testing gateway is deployed. Real Feishu QR sign-in remains unverified; credentials intentionally remain `TODO`.

| 项目 / Item | 证据 / Evidence |
| --- | --- |
| Source | `9bed622`, `main`, GitLab `azeroth/agentdeck` |
| Pipeline | [460370 / R1 / run 567223](https://cicd.ttyuyin.com/#/pipeline/single/detail/460370?buildNumber=1&projectId=83) |
| Stages | Compile, image push, testing deployment successful; production left at manual action |
| Artifact | `V20261010105327-9bed622` |
| Image digest | `sha256:94cbfbb87fc5fa81227ba30c7b3a72d6d81add38b5922d4ced47b6bbad77609b` |
| Configuration | `206482`, version `4` |
| Runtime | `k8s-hs-bj-1-test / ai-social`, StatefulSet `agent-desk`, one ready replica |
| Release | Initial `649425`; persistence verification restart `649429` |
| Public URL | `https://web-test-omnivibe.ttyuyin.com/agentdeck` |
| Route | VirtualService `ai-social/agentdeck-testing`, gateway `istio-ingress/unified-external`, prefix preserved |
| Storage | Existing PVC `pvc-nas-test-hs-bj-ai-socail-dify-plugin-daemon-ai-social`, only `agentdeck-gateway` subdirectory |

验收结果 / Results:

- `scripts/verify.sh`：Go race/vet、Flutter analyze、110 项 Flutter 测试及原生直连/代理/带前缀网关集成通过。飞书身份提供方为合成测试服务。
- 公网 TLS 使用系统 CA 验证，未跳过证书校验；`/agentdeck/healthz` 返回网关 JSON、`feishu_configured: false`。
- 无凭据的 `/v2/local/tunnel`、`/v2/account/locals` 返回 401；`POST /v2/auth/flows` 返回 503，符合 TODO 状态。
- 使用 owner Unix socket 创建临时测试工作站，原生 Dart 客户端经真实公网 HTTPS/WSS 完成加密配对、会话读取、实时增量、消息发送、图片解码、文件链接读取和断线重连；测试见 `app/test/deployed_gateway_test.dart`。
- 发送测试使用临时合成 Codex runtime，验证实际产品发送链路，未调用用户真实 Codex 会话；此项不代表外部模型服务验收。
- 实际 Pod 的 PVC、subPath、启动命令和 TODO 环境变量与保存配置一致；数据库和存储密钥为 `0600`，目录为 `0700`。第二个进程打开同一个 NAS 数据库被文件锁拒绝。
- 经 CICD 重启产生新 Pod 后，存储密钥指纹一致、工作站关联保留，S-LOCAL 自动重新建立认证隧道，公网健康检查恢复。
- 临时工作站及其 grant 已撤销，测试进程和含配对材料的临时目录已清理；网关工作站列表恢复为空。
- Android ARM64 release APK `0.4.15 / versionCode 7032` 已覆盖安装到重新连接的 GM1900，安装成功、实际版本和前台启动已核对，手机 APK SHA-256 与交付包一致。本次未修改生产部署或既有域名路由规则。

The full local verification suite passed, including Go race/vet, Flutter analysis, 110 Flutter tests and native direct/proxy/prefixed-gateway integration with a synthetic identity provider. Public HTTPS/WSS was then tested against the deployed gateway with normal system CA verification. A temporary owner-enrolled workstation and native Dart client exercised encrypted pairing, session reads, live updates, sending, images, linked attachments, and reconnect. Sending used a synthetic agent runtime rather than a user's real Codex conversation. NAS permissions and concurrent-writer exclusion passed. A CICD restart replaced the Pod while preserving the machine key and workstation association; the authenticated tunnel reconnected. Temporary credentials and processes were removed. The ARM64 release APK, version 0.4.15 / code 7032, was installed on the reconnected GM1900. Its installed version, foreground launch, and APK hash match were verified. Production was not deployed.

## TODO

1. 在 CICD 测试配置安全替换 `FEISHU_APP_ID`、`FEISHU_APP_SECRET`，保持 `FEISHU_OAUTH_SCOPE` 留空。
2. 在飞书登记 `https://web-test-omnivibe.ttyuyin.com/agentdeck/v2/auth/callback`，发布应用并设置用户可用范围，然后重新部署网关。
3. 使用目标飞书应用和真实手机验收 S-LOCAL 扫码、C-APP 同账号发现工作站及双方校验码绑定。
4. GM1900 已升级带前缀支持的客户端；真实飞书流程仍需在该手机上验收。

Securely replace both Feishu placeholders, register the full callback, publish the application, set its availability range, and redeploy. Then verify real workstation/mobile sign-in and comparison-code pairing on the upgraded GM1900.
