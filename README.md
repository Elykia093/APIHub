# APIHub

专门用于公益 API 站每日签到和公告聚合的轻量自托管服务。它只管理签到与公告，不承担模型代理、计费或 API Key 网关职责。

## 功能

- 多公益站管理，支持独立时区、签到计划和公告同步计划。
- 自动识别 New API、Sub2API、ZenAPI，并按适配器能力启用可用操作。
- 手动/定时签到，同站点同自然日由 PostgreSQL 唯一约束保证幂等。
- 聚合 `/api/status` 结构化公告与 `/api/notice` 文本通知，按正文指纹去重。
- AES-256-GCM 加密保存站点访问令牌，管理 API 不回显明文或密文。
- 遇到 Turnstile、验证码或二次验证时标记“需人工处理”，不尝试绕过。
- 自带中文管理页、健康检查、请求限流和 Docker Compose 部署文件。
- 提供 Android 管理客户端；定时签到仍只在服务器执行。

## 架构与迁移状态

- `server/`：Go 1.26.5、Gin、Ent、pgx，仅支持 PostgreSQL 17；启动只执行项目自己的 v1/v2 前向迁移，不启用 Ent 自动建表。
- `web/`：Vue 3、Vite、TypeScript；生产产物通过 `go:embed` 编入同一个 Go 二进制。
- `androidApp/`：Kotlin 2.4.0、Compose Multiplatform 1.11.1、Miuix 0.9.3；Miuix 0.9.3 AAR metadata 声明 `minCompileSdk=37`，因此为保证可构建性使用 `compileSdk 37`，应用仍保持 `minSdk 26`、`targetSdk 36`。这不是按原计划完全采用 `compileSdk 36`。
- 根目录 `src/` 与 `tests/`：原 Node 实现和兼容测试基线。迁移验收完成前保留，普通 `docker compose` 仍以 `Dockerfile.node` 为默认入口。

Web 与 Android 共用 `/api/v1`，不增加移动端专用接口。两端统一使用 Anheyu `brand_blue` 亮暗主题令牌。

## 快速部署

要求 Docker 与 Docker Compose。复制环境变量模板：

```powershell
Copy-Item .env.example .env
```

编辑 `.env`，填写三个不同的随机值，并配置数据库连接 URL：

- `ADMIN_TOKEN`：至少 16 个字符，用于登录管理页。
- `APP_SECRET`：至少 32 个字符，用于加密站点访问令牌；部署后必须稳定保存。
- `POSTGRES_PASSWORD`：PostgreSQL 原始密码，Compose 用它初始化数据库。
- `DATABASE_URL`：`postgresql://apihub:<URL编码后的密码>@postgres:5432/apihub`；其中密码必须与 `POSTGRES_PASSWORD` 相同，URI 保留字符需要百分号编码。

可用 Node.js 生成随机值：

```powershell
node -e "console.log(require('node:crypto').randomBytes(32).toString('base64url'))"
```

启动当前默认的 Node 兼容服务与 PostgreSQL 17：

```powershell
docker compose up -d --build
```

Go 实现当前是 candidate，必须显式叠加 override 才会启动：

```powershell
docker compose -f docker-compose.yml -f docker-compose.go.yml up -d --build
```

该命令用于迁移验证，不代表已经完成生产切换。契约差分、真实 PostgreSQL、容器压力与退出、安全扫描、双架构 OCI/SBOM/provenance 证据全部通过前，生产入口必须继续使用 Node。

访问 `http://127.0.0.1:4180`，输入 `ADMIN_TOKEN`。公网部署必须放在 HTTPS 反向代理后；Compose 不应直接暴露到公网。站点访问令牌应从对应站点的正规账号设置或登录流程中获取；New API 还需要填写用户 ID。APIHub 不接管用户名、密码、浏览器 Cookie 或 OAuth 会话。

```powershell
docker compose ps
docker compose logs -f apihub
```

## 本地开发

后端需要 Go 1.26.5，Web 需要 Node.js 24，数据库需要 PostgreSQL 17。可先只启动 Compose 中的数据库：

```powershell
Copy-Item .env.example .env
docker compose up -d postgres
$env:DATABASE_URL = "postgresql://apihub:<URL编码后的密码>@127.0.0.1:5432/apihub"
$env:ADMIN_TOKEN = "local-admin-token-change-me"
$env:APP_SECRET = "local-encryption-secret-change-me-32chars"
cd web
npm ci
npm run build
cd ../server
go run ./cmd/apihub
```

服务默认监听 `127.0.0.1:4180`。启动时会在 PostgreSQL 事务中自动执行尚未应用的版本迁移。Web 热更新可在 `web/` 运行 `npm run dev`。

原 Node 基线和默认容器入口可单独验证：

```powershell
npm ci
npm test
npm run typecheck
npm run build
docker build -f Dockerfile.node -t apihub-node:compat .
```

Android 工程要求 JDK 17，并安装 Android SDK 37 作为编译平台；`targetSdk` 仍为 36：

```powershell
cd androidApp
./gradlew.bat testDebugUnitTest lintDebug assembleRelease
```

Release 默认拒绝明文 HTTP；仅 debug manifest 允许本地 HTTP。管理员令牌经 Android Keystore AES-GCM 加密后再写入 DataStore，DataStore 不保存明文。

## 配置

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `ADMIN_TOKEN` | 无 | 必填，至少 16 个字符 |
| `APP_SECRET` | 无 | 必填，至少 32 个字符；用于凭据加密 |
| `DATABASE_URL` | 无 | 必填，PostgreSQL 连接 URL |
| `DATABASE_POOL_MAX` | `5` | 单应用实例最大数据库连接数，范围 1–20 |
| `DATABASE_IDLE_TIMEOUT_MS` | `30000` | 空闲连接回收时间 |
| `DATABASE_CONNECTION_TIMEOUT_MS` | `5000` | 获取/建立连接的超时 |
| `DATABASE_STATEMENT_TIMEOUT_MS` | `15000` | SQL 服务端执行超时 |
| `HOST` | `127.0.0.1` | 监听地址；容器内设为 `0.0.0.0` |
| `PORT` | `4180` | HTTP 端口 |
| `HTTP_TIMEOUT_MS` | `10000` | 单个上游请求超时 |
| `MAX_RESPONSE_BYTES` | `1048576` | 单个上游响应正文上限 |
| `ALLOW_PRIVATE_SITES` | `false` | 是否允许站点解析到私网/保留地址 |
| `ALLOW_INSECURE_HTTP` | `false` | 是否允许 HTTP 站点 |
| `GOMEMLIMIT` | `96MiB` | 仅 Go candidate 使用；Compose 内存硬限制仍为 128 MiB |

上述变量均由 Compose 透传；`.env.example` 给出了默认值。默认只允许公网 HTTPS。只有明确需要接入内网站点时才开启私网访问；开启后应同时限制容器网络，避免把服务暴露为 SSRF 跳板。

应用进程继续兼容 Node `Number()` 可接受的 `PORT` 形式；但 Docker Compose 的 `ports` 语法只接受规范十进制端口。通过 Compose 启动时，`PORT` 必须写成 `1`–`65535` 的十进制整数字符串（例如 `4180`），不能写 `1e3`、`4180.0` 或十六进制。直接运行 Node/Go 进程时不受这条 Compose 解析限制。

连接池默认 5 条连接，按照当前“单实例、少量公益站、低频定时任务”假设设置。增加应用副本或大幅提高任务频率前，应先按 `副本数 × DATABASE_POOL_MAX` 复核 PostgreSQL `max_connections` 和 PgBouncer 配置。

## 站点适配器与兼容行为

| 适配器 | 自动识别信号 | 服务端签到 | 公告同步 | 用户 ID |
| --- | --- | --- | --- | --- |
| `new-api` | `/api/status` JSON 契约 | 支持 | `/api/status` + `/api/notice` | 必填 |
| `sub2api` | `/api/v1/auth/me` JSON 契约 | 不支持 | `/api/v1/announcements` | 不需要 |
| `zen-api` | `/api/public/site-info` JSON 契约 | `/api/u/checkin` | 不支持 | 不需要 |

`auto` 只用于创建或修改时的受控探测，数据库会保存识别出的具体适配器。无法可靠识别时请求失败并要求用户明确选择，不会静默回退到 New API。适配器能力可通过 `GET /api/v1/site-adapters` 查询。

### New API

签到使用以下接口：

- `GET /api/status`：读取签到开关与 Turnstile 状态。
- `GET /api/user/checkin?month=YYYY-MM`：判断今日是否已经签到。
- `POST /api/user/checkin`：执行签到。

认证请求发送原始站点访问令牌到 `Authorization`，并发送 `New-Api-User` 用户 ID。公告同步读取 `/api/status` 与 `/api/notice`；其中一路失败时会保存另一路结果并把同步标为 `partial`。

不同公益站的分支版本可能修改接口或鉴权方式。当前只保证项目自动化测试覆盖的契约；失败时会保留稳定错误分类，不会伪报成功。

### 浏览器能力边界

参考扩展支持的 linux.do OAuth、Cookie/LocalStorage 捕获、页面按钮点击和 Cloudflare/Turnstile 页面协助依赖真实浏览器身份上下文，服务端版本不直接实现。需要这些能力的站点会保持相应操作关闭或标记为需人工处理，避免在错误账号页面上误签到。

## 安全边界

- 不保存用户名、密码或浏览器 Cookie。
- 管理令牌只由浏览器放在当前标签页的 `sessionStorage`，API 响应不会返回站点令牌。
- Android 管理令牌只以 Keystore AES-GCM 密文落盘；应用日志和错误对象不记录令牌。
- 上游请求默认禁止重定向，超时 10 秒，响应正文最多 1 MiB。
- DNS 解析结果包含私网或保留地址时默认拒绝访问。
- 管理页用 `textContent` 渲染公告正文，避免把上游公告当 HTML 执行。
- PostgreSQL 密码、管理员令牌和加密主密钥只通过环境变量注入，不写入仓库或镜像。
- 生产环境保持单应用副本；当前调度器不支持多实例任务租约。

## PostgreSQL、备份与恢复

Compose 将数据库持久化到 `apihub-postgres-data` 卷。应用启动时使用事务和 advisory lock 串行执行前向迁移；迁移失败会整体回滚并阻止服务启动。

数据库卷初始化后，单独修改 `.env` 中的 `POSTGRES_PASSWORD` 不会自动修改已有 PostgreSQL 角色密码；需要先在数据库执行受控的密码轮换，再同步更新 `POSTGRES_PASSWORD` 和使用 URL 编码密码的 `DATABASE_URL`。空库开发环境也可以删除数据卷后重新初始化，但这会永久删除卷内数据。

生产环境应配置定期 `pg_dump` 或托管数据库快照/PITR，并实际演练恢复。恢复已有数据时必须同时保留原来的 `APP_SECRET`，否则已加密的站点令牌无法解密。

本项目在切换 PostgreSQL 前没有本地 SQLite 数据文件，因此仓库不包含 SQLite→PostgreSQL 数据搬迁脚本。若你之后需要导入其他实例的数据，应单独做 dry-run、行数/唯一约束校验和备份后再写入。

### Go 与 Node 回滚

v1/v2 SQL 字节和 checksum 在两种实现间保持一致，本轮不新增 v3。当前默认入口仍是 Node；Go 只通过 `docker-compose.go.yml` 进入 candidate 验证。切换前必须满足以下门禁：

1. Node 与 Go 对同一数据库和同一请求集的状态码、稳定响应体、内容类型及缓存语义无差异；Node 写入的加密站点必须能由 Go 实际签到，Go 更新的令牌也必须能由 Node 回滚实例实际签到。
2. Node 基线、Go、Vue、Android、PostgreSQL 17 集成、容器压力和 SIGTERM 退出码门禁全部通过。
3. Node rollback 与 Go candidate 都已生成 amd64/arm64 OCI、SBOM、provenance、OCI digest 和归档 SHA-256；最终上传的同一 OCI 归档已逐架构通过依赖、secret 和镜像扫描，未隐藏无修复版本的 High/Critical。
4. 已记录旧 Node 可拉取 digest，备份 PostgreSQL 与 `APP_SECRET`，并由人工批准切换和回滚窗口。
5. 发布到目标 registry 后，已对最终 digest 完成可信签名和身份、issuer、subject 验证；本地 OCI 归档的 BuildKit provenance 不能替代 registry 签名。

生产切换必须使用已验证的不可变镜像引用，而不是重新构建或使用可变 tag：

```powershell
$env:APIHUB_GO_IMAGE = "registry.example/apihub-go@sha256:<verified-digest>"
docker compose -f docker-compose.yml -f docker-compose.go.yml up -d --no-build --force-recreate apihub
```

需要回滚时移除 Go override，并使用切换前记录的 Node digest；数据库和 `APP_SECRET` 必须保持不变：

```powershell
$env:APIHUB_IMAGE = "registry.example/apihub-node@sha256:<rollback-digest>"
docker compose up -d --no-build --force-recreate apihub
```

如果库内已存在 `sub2api` 或 `zen-api` 站点，回滚 Node 构建必须是理解 v2 枚举的版本。Go 稳定观察期与回滚演练完成前不得删除 `src/`、`tests/` 或 `Dockerfile.node`。

Node 默认镜像以 `node` 用户运行，Go candidate 以 Distroless `nonroot` 用户运行；两者都使用只读根文件系统、删除全部 Linux capabilities，并把 `/tmp` 挂为 16 MiB tmpfs。Compose 限制应用为 128 MiB，Go candidate 另设 `GOMEMLIMIT=96MiB`。

Dockerfile、PostgreSQL Compose、CI service 和 CI 模拟上游都固定为可读 tag 加官方 registry 多架构索引 digest。当前摘要于 2026-07-18 直接从 Docker Hub/GCR manifest API 解析；依赖升级时必须重新审查并显式更新 tag 与 digest，不能只移动 tag。

数据库模型、索引和上线检查见 `docs/postgresql.md`。

## 验证

```powershell
# 原 Node 兼容基线
npm test
npm run typecheck
npm run build

# Go
cd server
go test ./...
go test -race ./...
go vet ./...

# Vue
cd ../web
npm run typecheck
npm run lint
npm test
npm run build
npm run e2e

# Android（JDK 17 + Android SDK）
cd ../androidApp
./gradlew.bat testDebugUnitTest lintDebug assembleRelease
```

真实 PostgreSQL 集成测试要求专用数据库名 `apihub_test`，避免误操作其他库：

```powershell
$env:APIHUB_INTEGRATION_DATABASE_URL = "postgresql://apihub:<密码>@127.0.0.1:5432/apihub_test"
cd server
go test -tags=integration -v ./internal/service
```

它覆盖空库、v1→v2、checksum 漂移、PATCH `false`、签到幂等与失败重试、签到及公告取消后的终态写入、公告去重/排序和连接重开持久化。GitHub Actions 还覆盖 race、vet、lint、govulncheck、Playwright 桌面/Pixel 7、Android API 26/35/36、Release/R8、PostgreSQL 17、Node→Go→Node 加密站点双向运行时验证、128 MiB 下的数据库/静态资源压力、SIGTERM 退出码、依赖与 secret 扫描、PostgreSQL runtime 扫描，以及同一最终归档逐架构扫描并带 SBOM/provenance/digest/checksum 证据的 amd64/arm64 OCI 构建。CI 未实际运行前不能视为这些门禁已通过。

## 目录

```text
server/             Go 后端、Ent schema/生成代码与嵌入式 Web 产物
web/                Vue 3 管理页
androidApp/         Kotlin + Compose + Miuix Android 客户端
src/                保留的 Node 兼容实现
tests/              Node 测试与跨语言兼容 fixture
docs/               产品边界、API 契约和 PostgreSQL 说明
.github/workflows/  全栈、真实 PostgreSQL、Android 与 OCI 门禁
docker-compose.go.yml  显式 Go candidate 容器 override
```

更完整的产品边界与接口定义见 `docs/product-brief.md` 和 `docs/api-contract.md`；参考项目功能取舍与许可证边界见 `docs/reference-analysis.md`。
