# APIHub Agent Guide

`CLAUDE.md` 是本仓库完整的架构约束、跨端契约、UI 规范和验证矩阵。开始改动前先读与任务相关的章节；这里仅保留日常工作中必须遵守的摘要。新增长期有效的架构、并发、安全或平台约束时，同步更新 `CLAUDE.md`。

## 项目边界

- APIHub 只负责公益 API 站点管理、签到、公告聚合和浏览器伴侣任务，不承担模型代理、计费或 API Key 网关职责。
- Go 是唯一后端实现，普通 `docker compose` 直接构建根 `Dockerfile`。根 Node 后端、旧静态页和 Node rollback 已退役；不要重新引入第二套后端实现。
- `server/` 是 Go 后端；`web/` 是 Vue 管理页；`androidApp/` 是纯 Android 单模块 `:app`；`companion-extension/` 是可选 Chrome 浏览器伴侣。
- Web 与 Android 共用 `/api/v1`。不要为某一客户端新增绕开现有契约的专用接口。
- `server/ent/schema/` 是 Ent schema 源；`server/ent/` 中标记为 generated 的文件不得手改。`server/internal/webui/dist/` 由 `web` 构建生成并供 Go `go:embed` 使用；Web 变更必须连同新增、删除的 hash 产物一起检查。
- 当前生产调度只支持单应用副本。增加副本前必须先设计任务租约、幂等和数据库连接预算。

## 架构与契约

- Go 组合根是 `server/internal/app/app.go`。数据库、HTTP client、scheduler 和其他长生命周期资源必须有唯一所有者，并在关闭路径中释放。
- 修改接口、字段、枚举、适配器、配置或公开行为前，搜索 Go、Web、Android、浏览器扩展、fixture 和文档的全部生产者与消费者。
- `/api/v1` 的错误体保持 `{ error: { code, message, retryable, requestId } }`；稳定状态码、错误码、缓存头和重试语义必须与 Web、Android 和扩展消费者一致。
- `accessToken` 只写不回显；PATCH 必须区分字段缺失与显式 `false` 或空字符串。新增适配器时同步更新 Go registry、探测顺序、数据库约束、Web/Android 展示、契约、迁移和测试。
- 项目迁移只允许前向追加，并同步更新 `server/internal/testdata/compatibility-vectors.json`。不得修改已发布迁移，也不得用 Ent 自动建表替代 `server/internal/migrate`。
- 上游请求必须经过 Go `netclient` 的 SSRF 防护；默认仅公网 HTTPS、禁止重定向，并保留 DNS/IP、超时和响应大小限制。验证码、Turnstile、二次验证或 OAuth 不得由服务端绕过。
- 浏览器身份只能由用户本机 `companion-extension/` 使用。扩展不得读取或上传 Cookie、浏览器存储、Authorization、OAuth code、验证码或完整页面正文；新增权限、host permission 或注入行为属于安全变更。

## Android 与 Web UI

- Android 版本与构建事实以 `androidApp/gradle/libs.versions.toml`、`androidApp/app/build.gradle.kts` 和 CI 为准。工程当前没有 `commonMain`、Koin 或 Hilt，不得从参考项目推断不存在的跨平台或 DI 边界。
- `MainActivity` 是 Android 组合根；`ApiHubRepository` 拥有 `ApiClient` 的关闭责任。`MainViewModel` 的 session/request version 用于阻止断开、重连和刷新竞态中的旧响应回写，修改请求编排时必须保留等价的失效机制；`CancellationException` 必须继续向上抛出。
- 新增 Android UI 以当前 MiuiX 依赖和 `androidApp/app/src/main/java/com/elykia/apihub/ui/Theme.kt` 的语义 token/wrapper 为主。Flow 使用 `collectAsStateWithLifecycle()`，UI state 保持不可变；Composable 正文不得直接做网络、数据库或不可控协程副作用。
- Android 用户可见字符串使用资源；SavedState 不保存 token。Release 仅允许 HTTPS，debug 的明文 HTTP 例外只能覆盖现有本地开发主机边界。
- Web 颜色只取 `web/src/styles/theme.css`，页面壳和状态优先复用 `AppShell.vue`、`AppIcon.vue`、`PageState.vue` 与 `useAsyncData`。管理 token 只保存在当前标签页 `sessionStorage`，不得改为 `localStorage`、Cookie 或其他明文持久化。
- Web 和 Android 都必须覆盖 loading、error、empty、disabled、retry、401/session expired 与长文案/窄屏状态；有缓存内容时，刷新失败优先保留内容并显示局部错误。

## 验证

- 每次改动至少运行 `git diff --check`，再按影响面执行下列门槛。没有实际运行的命令不得写成已通过；本地通过不等于 CI、真实 PostgreSQL、容器或生产已通过。
- Go 后端：在 `server/` 运行 `go test ./...`、`go test -race ./...`、`go vet ./...`；修改 Ent schema 后先运行 `go generate ./ent` 并审查生成 diff。
- Vue 管理页：在 `web/` 运行 `npm run typecheck`、`npm run lint`、`npm test`、`npm run build`；交互或响应式变更再运行 `npm run e2e`，并检查 `server/internal/webui/dist/` 是否与源码同步。
- Android：在 `androidApp/` 运行 `./gradlew.bat testDebugUnitTest lintDebug assembleRelease`；MiuiX 组件、可访问性或真实交互变更再运行 `./gradlew.bat connectedDebugAndroidTest`。没有 API 36 设备、release 或真机证据时，明确写未完成的验证边界。
- PostgreSQL 集成测试只能连接可丢弃的专用 `apihub_test` 数据库，再在 `server/` 运行 `go test -tags=integration -v ./internal/service`。不得把开发库或生产库用于集成测试。
- API、迁移、浏览器伴侣、容器、安全或发布改动必须扩大到所有受影响端，并以 `.github/workflows/ci.yml` 对应 job 为最终门槛。CI 未实际运行时写明“本地已验证，CI 待运行”。

## Git 与工作区

- 保留用户已有的未提交改动；不要使用破坏性 reset/checkout 清理工作区，也不要覆盖、回退或格式化与当前任务无关的文件。
- 不修改或输出真实 `ADMIN_TOKEN`、站点 token、数据库密码、`APP_SECRET`、Cookie、OAuth code、Keystore 信息或完整带凭据 URL；日志、fixture 和截图只使用合成或脱敏数据。
- 完成修改后先报告实际 diff、验证结果和未运行项。除非用户在当前请求中明确授权，不执行 `git add`、`git commit`、`git push`、部署、数据库写入或其他外部可见操作。
