# APIHub

专门用于公益 API 站每日签到和公告聚合的轻量自托管服务。它只管理签到与公告，不承担模型代理、计费或 API Key 网关职责。

## 功能

- 多公益站管理，支持独立时区、签到计划和公告同步计划。
- 自动识别 New API、Sub2API、ZenAPI，并按适配器能力启用可用操作。
- 手动/定时签到，同站点同自然日由 PostgreSQL 唯一约束保证幂等。
- 聚合 `/api/status` 结构化公告与 `/api/notice` 文本通知，按正文指纹去重。
- AES-256-GCM 加密保存站点访问令牌，管理 API 不回显明文或密文。
- 遇到 Turnstile、验证码或二次验证时标记“需人工处理”，不尝试绕过。
- 可选浏览器伴侣在用户自己的 Chrome 会话中执行页面签到和 OAuth/验证接力；服务端不接收浏览器凭据。
- 自带中文管理页、健康检查、请求限流和 Docker Compose 部署文件。
- 提供 Android 管理客户端；定时签到仍只在服务器执行。

## 架构

- `server/`：Go 1.26.5、Gin、Ent、pgx，唯一验证目标为 PostgreSQL 18.4 Alpine；较低版本不作为兼容目标。启动只执行项目自己的 v1/v2/v3 前向迁移，不启用 Ent 自动建表。
- `web/`：Vue 3、Vite、TypeScript；生产产物通过 `go:embed` 编入同一个 Go 二进制。
- `androidApp/`：Kotlin 2.4.0、Compose Multiplatform 1.11.1、Miuix 0.9.3；Miuix 0.9.3 AAR metadata 声明 `minCompileSdk=37`，因此为保证可构建性使用 `compileSdk 37`，应用仍保持 `minSdk 26`、`targetSdk 36`。这不是按原计划完全采用 `compileSdk 36`。
- Go 是唯一后端实现，Vue 构建仍使用 Node.js 24 工具链，但仓库不再包含 Node 后端或 Node rollback 镜像。

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

启动 Go 服务与 PostgreSQL 18.4：

```powershell
docker compose up -d --build
```

`.env.example` 预置 DaoCloud/NJU 镜像路径和 `goproxy.cn`，同时保留固定 digest。无法访问这些加速源时，可删除 `NODE_BUILD_IMAGE`、`GO_BUILD_IMAGE`、`RUNTIME_IMAGE`、`GOPROXY` 四行，Compose 会回退到 Dockerfile 的官方源。

如果 Docker 主机内存低于 1 GiB，可额外设置 `GO_BUILD_GOMAXPROCS=1`、`GO_BUILD_GOMEMLIMIT=384MiB` 和 `GO_BUILD_FLAGS=-p=1 -tags=nomsgpack`，限制 Go 编译并发并跳过 APIHub 未使用的 Gin MessagePack 绑定；这些变量只影响构建阶段，不改变运行容器的 `GOMEMLIMIT`。

访问 `http://127.0.0.1:4180`，输入 `ADMIN_TOKEN`。公网部署必须放在 HTTPS 反向代理后；Compose 不应直接暴露到公网。站点访问令牌应从对应站点的正规账号设置或登录流程中获取；New API 还需要填写用户 ID。APIHub 不接管用户名、密码、浏览器 Cookie 或 OAuth 会话。

```powershell
docker compose ps
docker compose logs -f apihub
```

## 本地开发

后端需要 Go 1.26.5，Web 需要 Node.js 24，数据库以 PostgreSQL 18.4 为唯一验证目标。可先只启动 Compose 中的数据库：

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

Android 工程以 JDK 26 为唯一验证目标，并安装 Android SDK 37 作为编译平台；`targetSdk` 仍为 36。较低 JDK 不纳入兼容目标：

CI 仪器测试覆盖最低支持版本 Android API 26、Android 15 的 API 35 行为边界和当前最高验证目标 API 36；模拟器证据不替代真实厂商设备、低端机或性能验证。

```powershell
cd androidApp
./gradlew.bat testDebugUnitTest lintDebug assembleRelease
```

也可以用固定摘要的 Temurin 26 + Android SDK 镜像复现完整门禁。该命令为 800 MiB 服务器限制 Gradle 和本地测试 worker 各使用 128 MiB 堆、单 worker、进程内 Kotlin 编译；镜像 init script 仅在此命令显式启用，阿里云 Maven 镜像失败时仍回退到官方仓库：

```powershell
docker build -f androidApp/Dockerfile.jdk26 -t apihub/android-jdk26:26.0.1 androidApp
docker run --rm --memory=700m --memory-swap=2g `
  --volume "${PWD}/androidApp:/workspace" `
  --volume apihub-android-gradle-cache:/opt/gradle-cache `
  --workdir /workspace `
  apihub/android-jdk26:26.0.1 `
  bash -lc './gradlew testDebugUnitTest lintDebug assembleRelease --init-script /opt/gradle/docker-mirrors.init.gradle.kts --no-daemon --no-watch-fs --max-workers=1 -PjavaToolchainVersion=26 -Pkotlin.compiler.execution.strategy=in-process -Dorg.gradle.jvmargs="-Xmx128m -XX:+UseSerialGC -Dfile.encoding=UTF-8" --stacktrace'
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
| `GOMEMLIMIT` | `96MiB` | Go 运行时软限制；Compose 内存硬限制仍为 128 MiB |

上述变量均由 Compose 透传；`.env.example` 给出了默认值。默认只允许公网 HTTPS。只有明确需要接入内网站点时才开启私网访问；开启后应同时限制容器网络，避免把服务暴露为 SSRF 跳板。

Go 配置解析器继续兼容历史 `Number()` 形式；但 Docker Compose 的 `ports` 语法只接受规范十进制端口。通过 Compose 启动时，`PORT` 必须写成 `1`–`65535` 的十进制整数字符串（例如 `4180`），不能写 `1e3`、`4180.0` 或十六进制。

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

linux.do OAuth、页面按钮点击和 Cloudflare/Turnstile 人工验证依赖真实浏览器身份上下文，服务端不直接执行。可选的 `companion-extension/` 在用户本机 Chrome 中领取同源任务，复用当前浏览器会话执行页面按钮签到；遇到登录或人机验证时默认前置页面等待用户完成，也可在扩展弹窗关闭前置开关。扩展不申请 `cookies`、`webRequest` 或 `scripting` 权限，不读取或上传 Cookie、LocalStorage、SessionStorage、Authorization、OAuth code、验证码或页面完整正文，只回传状态、短说明和可选余额文本。

使用方式：管理页进入“浏览器伴侣”生成五分钟单次配对码，在 `chrome://extensions` 以开发者模式加载 `companion-extension/`，然后在扩展弹窗填写 APIHub 地址和配对码。设备令牌只在本机扩展存储；管理员可随时撤销设备并释放其未完成任务。完整契约与安全边界见 `docs/browser-companion.md`。

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

Compose 将 PostgreSQL 18 数据持久化到 `apihub-postgres-18-data` 卷。应用启动时使用事务和 advisory lock 串行执行前向迁移；迁移失败会整体回滚并阻止服务启动。旧 PostgreSQL 17 卷不能直接复用，必须先逻辑导出再恢复到新卷。

数据库卷初始化后，单独修改 `.env` 中的 `POSTGRES_PASSWORD` 不会自动修改已有 PostgreSQL 角色密码；需要先在数据库执行受控的密码轮换，再同步更新 `POSTGRES_PASSWORD` 和使用 URL 编码密码的 `DATABASE_URL`。空库开发环境也可以删除数据卷后重新初始化，但这会永久删除卷内数据。

生产环境应配置定期 `pg_dump` 或托管数据库快照/PITR，并实际演练恢复。恢复已有数据时必须同时保留原来的 `APP_SECRET`，否则已加密的站点令牌无法解密。

本项目在切换 PostgreSQL 前没有本地 SQLite 数据文件，因此仓库不包含 SQLite→PostgreSQL 数据搬迁脚本。若你之后需要导入其他实例的数据，应单独做 dry-run、行数/唯一约束校验和备份后再写入。

### 版本策略

APIHub 采用[语义化版本 2.0.0（SemVer）](https://semver.org/lang/zh-CN/)。版本号使用 `X.Y.Z`。从 `1.0.0` 起，不兼容的公开契约变更递增主版本号，向下兼容的功能新增或废弃声明递增次版本号，向下兼容的问题修正递增修订号。`0.y.z` 表示公开契约仍处于初始开发阶段；此阶段新增功能或不兼容变更递增次版本号，兼容修复递增修订号，首个稳定契约版本为 `1.0.0`。

- Git 发布标签使用 `vX.Y.Z`；先行版本可使用 `vX.Y.Z-alpha.N`、`vX.Y.Z-beta.N` 或 `vX.Y.Z-rc.N`。标签中的 `v` 不是版本号本身的一部分。
- 同一次发布的 Git 标签、Go `VERSION`、OCI `org.opencontainers.image.version` 与 Android `versionName` 必须一致；Android `versionCode` 另行保持单调递增。
- 源码中的版本值不等于已经发布；只有对应的不可变 Git 标签和制品完成发布后，才能声明该版本已发布。已发布的版本号和标签不可移动、覆盖或复用。
- 公开 API、配置语义、数据库兼容性、浏览器伴侣协议或客户端契约发生变化时，必须按影响选择下一个版本并写入发布说明。

### Go 发布与回滚

Go 是唯一后端实现。发布前必须通过 Go、Vue、Android、PostgreSQL 18.4 集成、容器压力、SIGTERM、依赖扫描和双架构 OCI 门禁，并备份 PostgreSQL 与 `APP_SECRET`。

生产部署必须使用已验证的不可变镜像 digest，而不是重新构建或使用可变 tag：

```powershell
$env:APIHUB_IMAGE = "registry.example/apihub-go@sha256:<verified-digest>"
docker compose up -d --no-build --force-recreate apihub
```

回滚使用上一个已验证的 Go digest，数据库和 `APP_SECRET` 必须保持不变。新增迁移只能前向追加，回滚镜像必须理解当前数据库 schema；不能通过删除卷或恢复旧 `APP_SECRET` 规避兼容问题。

Go 镜像以 Distroless `nonroot` 用户运行，使用只读根文件系统、删除全部 Linux capabilities、`/tmp` 16 MiB tmpfs、128 MiB 内存硬限制和 `GOMEMLIMIT=96MiB`。Dockerfile、Compose 和 CI 镜像都固定 tag 与多架构索引 digest；依赖升级时必须重新审查并显式更新 digest。

数据库模型、索引和上线检查见 `docs/postgresql.md`。

## 验证

```powershell
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

# Android（JDK 26 + Android SDK）
cd ../androidApp
./gradlew.bat testDebugUnitTest lintDebug assembleRelease
```

真实 PostgreSQL 集成测试要求专用数据库名 `apihub_test`，避免误操作其他库：

```powershell
$env:APIHUB_INTEGRATION_DATABASE_URL = "postgresql://apihub:<密码>@127.0.0.1:5432/apihub_test"
cd server
go test -tags=integration -v ./internal/service
```

它覆盖空库、v1→v2→v3、checksum 漂移、PATCH `false`、签到幂等与失败重试、签到及公告取消后的终态写入、公告去重/排序和连接重开持久化。GitHub Actions 还覆盖 race、vet、lint、govulncheck、Playwright 桌面/Pixel 7、Android API 26/35/36、Release/R8、PostgreSQL 18.4、128 MiB 下的数据库/静态资源压力、SIGTERM 退出码、依赖与 secret 扫描、PostgreSQL runtime 扫描，以及同一最终归档逐架构扫描并带 SBOM/provenance/digest/checksum 证据的 amd64/arm64 Go OCI 构建。CI 未实际运行前不能视为这些门禁已通过。

## 目录

```text
server/             Go 后端、Ent schema/生成代码与嵌入式 Web 产物
web/                Vue 3 管理页
companion-extension/ 可选 Chrome 浏览器伴侣
androidApp/         Kotlin + Compose + Miuix Android 客户端
docs/               产品边界、API 契约和 PostgreSQL 说明
.github/workflows/  全栈、真实 PostgreSQL、Android 与 OCI 门禁
Dockerfile          Go + Vue 多阶段生产镜像
docker-compose.yml  Go 服务与 PostgreSQL 18.4
```

更完整的产品边界与接口定义见 `docs/product-brief.md` 和 `docs/api-contract.md`。

## 参考项目

- [qixing-jk/all-api-hub](https://github.com/qixing-jk/all-api-hub)（AGPL-3.0）
- [cita-777/metapi](https://github.com/cita-777/metapi/tree/41767a65ec8e5470a9a70f4615b47dc24949afff)（MIT）
- [aceHubert/newapi-ai-check-in](https://github.com/aceHubert/newapi-ai-check-in/tree/082e18b8abd875f813302cb63d73edec6e9c49fa)（BSD-2-Clause）
- [Jasonliu-0/Newapi-checkin](https://github.com/Jasonliu-0/Newapi-checkin/tree/9ab62be42f38783da7767a2b1b810c33561d6d0e)（MIT）
- [ken861222/newapi-manager](https://github.com/ken861222/newapi-manager/tree/43e2fb0027a2f13cd74ace8eeed2275c1572a127)（README 声明 MIT）

APIHub 仅参考这些项目的公开功能与协议边界，没有复制其源码。具体功能取舍、许可证边界和核对版本见 `docs/reference-analysis.md`。
