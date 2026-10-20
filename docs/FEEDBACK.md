# 网关反馈存储 / Gateway feedback storage

S-GATEWAY 支持 MongoDB、MySQL、PostgreSQL 三种反馈存储，由启动环境变量选择。三者使用同一接口、加密格式、隔离和保留规则；默认关闭，不影响现有连接。**这是网关接口与存储能力；App 反馈页面和自动诊断采集尚未接入，不会自动上传聊天或日志。**

S-GATEWAY supports MongoDB, MySQL and PostgreSQL through one configurable feedback service. All backends share encryption, isolation and retention rules. It defaults off, preserving existing connections. **This implements gateway APIs and storage; the App feedback screen and automatic diagnostic collection are not integrated yet. Chats and logs are not automatically uploaded.**

## 配置 / Configuration

只在 Gateway 注入以下变量，Local 和 App 不接收数据库凭据。修改后重启 Gateway；数据库或表/索引初始化失败会阻止启动，不会静默切换到其他存储。默认使用固定 `agentdeck_feedback_v1` 表或集合，启动时创建不存在的表及索引；需先建立数据库，提供相应建表/建索引权限。

Inject these variables into Gateway only; Local and App never receive database credentials. Restart Gateway after changes. Connection/schema failures prevent startup instead of silently falling back. Startup creates the fixed `agentdeck_feedback_v1` table/collection and missing indexes. Provision the database and schema/index permissions first.

| 环境变量 / Variable | 用途 / Meaning |
| --- | --- |
| `AGENTDECK_FEEDBACK_DRIVER` | `off`（默认 / default）、`mongo`、`mysql`、`pg`。也接受 / aliases: `mongodb`, `postgres`, `postgresql` |
| `AGENTDECK_FEEDBACK_DSN` | 所选驱动的连接串，通过 Secret 注入 / Driver connection string, supplied through a Secret |
| `AGENTDECK_FEEDBACK_DATABASE` | MongoDB 必填的数据库名；SQL 数据库名写在 DSN / Required Mongo database; SQL database belongs in the DSN |
| `AGENTDECK_FEEDBACK_KEY` | base64 编码的独立 32 字节加密密钥，必须稳定保存 / Separate base64-encoded 32-byte encryption key; retain it across restarts |
| `AGENTDECK_FEEDBACK_RETENTION` | 默认 `720h`（30 天），允许 `24h` 至 `8760h` / Default 30 days; range one day to one year |

密钥只生成一次，保存到部署 Secret 并备份；不要每次启动重新生成。可使用 `openssl rand -base64 32` 在安全终端生成。DSN、密钥不要放入 Git、聊天或流水线明文日志。数据库记录不能脱离原密钥读取；此密钥与既有 App/Local 定时轮换的通信密钥分开。

Generate the storage key once in a private terminal, for example with `openssl rand -base64 32`, then retain it in your deployment Secret and backup. Do not regenerate it on every boot or put it/DSNs into Git, chat or plaintext build logs. Existing records require the original key. This storage key is separate from rotating App/Local traffic keys.

连接示例（将占位内容替换为自己的 Secret；三个方案选一个）： / Connection examples (replace placeholders privately and select one):

```sh
# PostgreSQL
AGENTDECK_FEEDBACK_DRIVER=pg
AGENTDECK_FEEDBACK_DSN='postgres://feedback:SECRET@pg.example:5432/agentdeck?sslmode=verify-full&sslrootcert=/run/secrets/db-ca.pem'

# MySQL
AGENTDECK_FEEDBACK_DRIVER=mysql
AGENTDECK_FEEDBACK_DSN='feedback:SECRET@tcp(mysql.example:3306)/agentdeck?tls=true'
# Private CA: configure the Go/system trust store, e.g. SSL_CERT_FILE.

# MongoDB
AGENTDECK_FEEDBACK_DRIVER=mongo
AGENTDECK_FEEDBACK_DATABASE=agentdeck
AGENTDECK_FEEDBACK_DSN='mongodb://feedback:SECRET@mongo.example:27017/agentdeck?authSource=admin&tls=true&tlsCAFile=/run/secrets/db-ca.pem'
```

三种驱动都需额外提供 `AGENTDECK_FEEDBACK_KEY`。非回环 TCP 数据库必须启用验证证书的 TLS；不接受跳过验证或可能回退明文的配置。仅本机回环地址和 Unix socket 支持无 TLS，供隔离开发测试使用。不要在公网/内网通过 `skip-verify`、`tlsInsecure` 或 PostgreSQL `sslmode=disable/require` 绕过验证。

Every driver also needs `AGENTDECK_FEEDBACK_KEY`. Non-loopback TCP databases require verified TLS; insecure certificate bypass or plaintext fallback is rejected. Only loopback/Unix connections permit no TLS for isolated development. Use verified TLS on public and internal networks.

切换 driver 只切换读写目标，**不会自动迁移旧反馈**；保留旧库和密钥，需要迁移时单独执行可核对的导出/导入。反馈外置数据库也不代表整个网关已支持多副本：路由、配对等既有状态仍保持原实现。

Changing the driver changes the target; **existing reports are not automatically migrated**. Preserve the old DB/key for a separately verified export/import. An external feedback database does not make the whole gateway multi-instance: routing and pairing retain their existing implementation.

## App/SDK 接口 / App and SDK API

以下路径均加网关既有前缀，例如 `/agentdeck/v2/feedback/create`。使用飞书登录后现有 Account 证明：`Authorization: Account <id>`、`X-Relay-Auth`，签名范围 `account/<id>`，签名逻辑路径去掉外部前缀。请求体为现有 `secure.Box`，作用域 `account-request/<id>`；响应也加密，作用域 `account-response/<id>`，携带对应证明的 challenge。SDK 必须校验 challenge，不能只相信 HTTP 200。

Use the existing gateway prefix, Account proof and encrypted envelopes: authorization header, replay-protected signed logical path, encrypted account-request scope and encrypted account-response scope with the proof challenge. SDKs must verify the response challenge, not just HTTP status.

| POST 路径 / Path | 解密后的请求 / Decrypted request |
| --- | --- |
| `/v2/feedback/create` | 下方 Submission；返回 `{report, created}` / Submission below; returns report and whether newly created |
| `/v2/feedback/list` | `{"limit":20,"next":""}`；返回摘要 items 和不透明 next 游标 / Summaries and opaque next cursor |
| `/v2/feedback/get` | `{"id":"AD-FB-..."}`；返回完整反馈及状态 / Full report and status |
| `/v2/feedback/delete` | `{"id":"AD-FB-..."}`；删除自己的反馈，可重复调用 / Idempotent deletion of the caller's report |

```json
{
  "id": "AD-FB-0123456789abcdef0123456789abcdef",
  "message": "返回会话时未看到新消息",
  "app_version": "0.4.45",
  "platform": "android",
  "diagnostics": [
    {
      "time": "2026-10-19T08:00:00Z",
      "component": "app",
      "code": "sync_timeout",
      "request_id": "diagnostic-request-id",
      "duration_ms": 5000,
      "status": 504
    }
  ]
}
```

- ID 为 `AD-FB-` 加 32 位小写十六进制随机值。App 先保存 ID，重试复用同 ID 和同一份内容；相同提交只生成一条反馈，改变内容复用 ID 返回 409。 / Generate and persist a random ID before submission; retries reuse the same ID/content. Identical retries are idempotent; changed content under that ID returns 409.
- 不接受客户端传入 tenant/owner，由已验证登录身份确定。`local` 可选，提供时必须属于该身份。相同租户其他用户及其他租户不能查询该反馈，翻页游标也绑定归属。 / Ownership comes from authenticated identity, never request fields. An optional Local must belong to that identity. Records and cursors are isolated by both tenant and user.
- 用户说明最多 16 KiB，诊断最多 1000 条，整个提交明文最多 512 KiB，密文 HTTP 请求最多 1 MiB。诊断仅接受上述固定字段，不接受原始日志、工具参数、聊天内容、token/cookie、配对信息、附件或任意额外字段。 / Description limit 16 KiB, 1,000 diagnostic records, 512 KiB plaintext and 1 MiB encrypted HTTP body. Diagnostics use the fixed schema; raw logs, tools/chat content, credentials, pairing data, attachments and extra fields are not accepted.
- 反馈由用户主动提交给网关所有者排障，网关可以解密反馈；这与网关只透传密文的 Agent 会话通道不同。落库内容为 AES-GCM 密文，归属索引为 HMAC，日志只写操作和请求编号，不写说明或诊断正文。 / Explicit feedback is disclosed to the gateway owner for diagnosis; the separate agent-chat relay remains opaque. Stored content is AES-GCM ciphertext, ownership indexes use HMAC, and logs exclude report bodies.
- 过期后立即不可查询；每 5 分钟按索引清理物理记录。API 返回 400/401/403/404/409/503 区分格式、登录、工作站归属、无记录、冲突和未配置/数据库不可用。数据库故障不伪装成“提交成功”。 / Expiry hides records immediately; indexed cleanup runs every five minutes. Errors distinguish validation, auth/ownership, missing records, conflicts and unavailable storage. DB failures never acknowledge successful storage.

## 排障处理 / Owner administration

管理操作只在 Gateway 自己的 0600 Unix socket 上开放，不进入公网 Handler。先从现有 `gateway-accounts` 查询 tenant/owner，所有操作必须明确两者。 / Administration is restricted to Gateway's mode-0600 Unix socket, outside the public handler. Obtain tenant/owner from existing account administration; both are required.

```sh
curl --unix-socket /data/agentdeck/control.sock \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"TENANT","owner":"OWNER","limit":20}' \
  http://unix/feedback/list

curl --unix-socket /data/agentdeck/control.sock \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"TENANT","owner":"OWNER","id":"AD-FB-0123456789abcdef0123456789abcdef","status":"resolved"}' \
  http://unix/feedback/status
```

同样提供 `/feedback/get`、`/feedback/delete`。状态支持 `new`、`in_progress`、`resolved`、`closed`；并发修改使用版本比较，冲突返回 409，避免覆盖。`/healthz` 的 `feedback_configured` 只表示已启用，非持续数据库健康检查；数据库中断由具体操作返回 503，清理失败写 WARN。

Get/delete use the same owner-only routes. Status updates use optimistic revisions to avoid overwriting concurrent changes. Health's configured flag means enabled, not continuous DB reachability; operation failures return 503 and cleanup failures log WARN.

## 验证 / Verification

```sh
cd backend
go test -race ./internal/feedback ./internal/gateway
cd ..
docker pull mysql:8.4
docker pull postgres:17
docker pull mongo:8.0
python3 scripts/verify_feedback.py
```

默认 Go 测试覆盖加密、隔离、重复提交、分页和接口鉴权，真实数据库用例需显式 DSN，平时跳过。脚本自动启动三个独立测试容器，仅映射随机回环端口，使用临时凭据和合成数据；验证真实读写、并发去重、重连、状态修改、删除与过期清理，最后删除自建容器及卷。不会连接现有业务库。离线已有镜像时可通过 `FEEDBACK_TEST_MYSQL_IMAGE`、`FEEDBACK_TEST_PG_IMAGE`、`FEEDBACK_TEST_MONGO_IMAGE` 指定镜像；脚本不自动拉取。

Default tests exercise encryption, isolation, retry semantics, pagination and auth. Real DB tests require explicit disposable DSNs and otherwise skip. The script creates three independent loopback-only containers with temporary credentials/synthetic records, verifies persistence and the gateway path, then removes only its own containers/volumes. It never uses existing business databases. Override image names through the listed variables; the script does not pull images itself.

单库可执行 `python3 scripts/verify_feedback.py --driver pg`（或 `mysql`、`mongo`）。已有独立临时数据库时，Go 集成用例读取 `AGENTDECK_TEST_FEEDBACK_MYSQL_DSN`、`AGENTDECK_TEST_FEEDBACK_PG_DSN`、`AGENTDECK_TEST_FEEDBACK_MONGO_DSN` 和 `AGENTDECK_TEST_FEEDBACK_MONGO_DATABASE`；用 `go test -race -p 1 ./internal/feedback ./internal/gateway -run 'TestConfiguredDatabaseContract|TestFeedbackDatabaseGatewayRoundtrip' -count=1` 运行。**这些变量只能指向可丢弃的临时库：保留期测试会推进时钟并删除该反馈表的过期记录。**

Use `--driver pg` (or mysql/mongo) for one backend. Direct Go integration uses the listed disposable-DB environment variables and the serialized command above. **Never supply a business database: the retention test advances its clock and deletes expired feedback rows.**

驱动文档 / Driver references: [MongoDB Go](https://www.mongodb.com/docs/drivers/go/current/connect/specify-connection-options/), [MySQL Go](https://github.com/go-sql-driver/mysql), [pgx database/sql](https://github.com/jackc/pgx/wiki/Getting-started-with-pgx-through-database-sql).
