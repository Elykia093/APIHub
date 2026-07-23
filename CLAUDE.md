# APIHub 开发规范

APIHub 是面向公益 API 站签到与公告聚合的轻量自托管管理系统。Go 是唯一后端实现，仓库同时包含 Vue 管理页、Android 管理客户端和可选 Chrome 浏览器伴侣。

本文是仓库级开发约定。开始修改前先读与改动直接相关的源码、manifest、测试和文档；代码与构建事实高于本文中的概述。本文参考 `YuKongA/Mishka` 的 `CLAUDE.md` 组织方式与 MiuiX UI 规范，但不继承 Mishka 的代理内核、Room、Koin、Navigation3、ROOT 或 JNI 约束。

## 核心原则

1. 先定位真实入口、生产者、消费者、配置来源和验证入口，再修改。
2. 不把 README、旧文档、生成产物或参考项目当作唯一事实；冲突时以当前 manifest、源码、测试和 CI 为准。
3. 修改接口、字段、枚举、迁移、配置、主题 token 或公开行为前，必须搜索所有 Go、Web、Android、扩展、fixture 和文档消费者。
4. 修复必须处理根因；同一方案两次无效时停止叠补丁，重新检查入口、状态、并发和环境差异。
5. 不顺手重构无关代码，不覆盖或回退工作区中非本次任务产生的改动。
6. 不打印、提交、截图或写入文档中的真实管理员令牌、站点令牌、数据库密码、`APP_SECRET`、Cookie、OAuth code 或完整带凭据 URL。
7. 未运行的命令不写成已通过；本地通过不等于 CI、真实 PostgreSQL、容器或生产已通过。

## 当前系统边界

- APIHub 只负责站点管理、签到、公告聚合和浏览器伴侣任务，不承担模型代理、计费或 API Key 网关职责。
- 服务端不会绕过 Turnstile、验证码、二次验证或 OAuth。需要真实浏览器身份时只允许由用户本机的 `companion-extension/` 承接。
- Web 与 Android 共用 `/api/v1`，不得新增仅为绕开契约问题的移动端专用接口。
- 普通 `docker compose` 直接构建并启动 Go 后端。Node 后端和 Node rollback 已退役，不得重新引入第二套服务端实现。
- 生产调度当前只支持单应用副本。增加副本前必须设计任务租约、幂等和连接预算，不能只调整 Compose 副本数。

## 技术栈与唯一真源

不要在文档中重复维护易漂移的依赖版本。需要确认当前版本时读取以下文件：

| 范围 | 当前技术 | 版本与配置真源 |
| --- | --- | --- |
| Go 后端 | Go、Gin、Ent、pgx | `server/go.mod`、`server/go.sum` |
| Web | Vue、Vue Router、Vite、TypeScript、Vitest、Playwright | `web/package.json`、`web/package-lock.json`、`web/vite.config.ts` |
| Android | 单模块 `:app`、Kotlin、Compose Multiplatform、MiuiX、Ktor、DataStore | `androidApp/gradle/libs.versions.toml`、`androidApp/app/build.gradle.kts` |
| 数据库 | PostgreSQL，项目自有前向迁移 | `server/internal/migrate/migrate.go`、`server/internal/testdata/compatibility-vectors.json` |
| 部署与质量 | Docker Compose、GitHub Actions、Trivy、OCI/SBOM/provenance | `Dockerfile`、`docker-compose.yml`、`.github/workflows/ci.yml` |

当前本地基线由 manifest 和 README 共同约束：Web Node、Go、Android JDK/SDK 各自不同，不能用某一端能运行来推断其他端环境也满足。Android 构建以 JDK 26、`compileSdk 37` 为当前验证目标，`targetSdk` 仍为 36；`minSdk 26` 是最低支持版本，CI 仪器测试覆盖 API 26、35 和 36。变更时以 Gradle 文件和 CI matrix 为准。

## 目录与所有权

```text
APIHub/
├── server/
│   ├── cmd/apihub/              Go 进程入口与 healthcheck
│   ├── internal/                Go 配置、迁移、适配器、服务、API 与嵌入式 Web
│   └── ent/
│       ├── schema/              Ent schema 源，可修改
│       └── 其余生成文件          `go generate ./ent` 生成，禁止手改
├── web/                         Vue 管理页源码与测试
├── server/internal/webui/dist/  `web` 构建生成且提交到仓库的 Go 嵌入产物
├── androidApp/                  单模块 Android 管理客户端
├── companion-extension/         Chrome 浏览器伴侣
├── docs/                        产品、API、数据库与伴侣契约
├── Dockerfile                   Go + Vue 多阶段生产镜像
└── docker-compose.yml           Go 服务与 PostgreSQL 18
```

生成物规则：

- `server/ent/schema/*.go` 是源；`server/ent/` 下标记 `Code generated ... DO NOT EDIT` 的文件只能通过 Ent 重新生成。
- `web` 的 `npm run build` 会清空并重建 `server/internal/webui/dist/`。修改 Web 后必须提交新产物及被删除的旧 hash 文件。
- `server/internal/webui/dist/` 供 Go `go:embed` 使用，是唯一生产管理页产物。
- 不直接编辑 APK、OCI、SBOM、Playwright report、coverage 或其他构建输出。

## 入口与依赖流

### Go 后端

```text
server/cmd/apihub/main.go
  -> config.Load
  -> app.Build
     -> project migrations
     -> Ent / services / adapters
     -> Gin router
     -> embedded server/internal/webui/dist
  -> graceful shutdown
```

Go 启动只运行 `server/internal/migrate` 的项目迁移，不启用 Ent 自动建表。`server/ent/migrate` 的存在不代表生产可调用自动迁移。

### Vue 管理页

`web/src/main.ts` -> `web/src/router.ts` -> view -> `web/src/api.ts`。管理会话只保存在当前标签页 `sessionStorage`；不要改为 `localStorage`、Cookie 或持久化明文 token。公共壳复用 `AppShell.vue`，加载/错误/空状态优先复用 `PageState.vue` 与 `useAsyncData`。

### Android 客户端

```text
MainActivity
  -> ApiHubRepository(CredentialStore)
  -> MainViewModel.Factory
  -> ApiHubApp
     -> MainUiState
     -> ApiHubDataSource
        -> ApiHubRepository
           -> ApiClient (/api/v1)
           -> CredentialStore -> AndroidKeystoreCipher
```

- 当前没有 Koin/Hilt；`MainActivity` 是组合根。不要因为参考项目使用 DI 就无理由引入新框架。
- 当前虽使用 Compose Multiplatform 组件与插件，但工程仍是纯 Android 单模块 `:app`，源码位于 `src/main`，没有 `commonMain`/`androidMain` 或 KMP target。不要按参考项目推断不存在的跨平台边界。
- `ApiHubRepository` 拥有 `ApiClient` 的关闭责任，替换会话前先关闭旧 client；调用方不得另建长期 client 绕过该所有权。
- `MainViewModel` 用 `sessionVersion` 与 `dashboardRequestVersion` 阻止断开、重连或刷新竞态中的旧响应回写。修改请求编排时必须保留等价的失效机制。
- `CancellationException` 必须继续抛出，不能被通用错误映射吞掉。
- `SavedStateHandle` 可保存页面和非敏感草稿，但不得保存 `accessToken`。编辑既有站点时令牌保持空表示“不修改”，不得从服务端或本地存储回填到表单。
- `CredentialStore` 只保存服务器地址和 Keystore AES-GCM 密文；`StoredCredentials.toString()` 必须继续脱敏。
- Release 只接受 HTTPS。Debug 的 HTTP 例外仅限 `ApiClient.normalizeBaseUrl` 允许的本地开发主机，不得扩大为任意明文 HTTP。

## API 与跨端契约

`docs/api-contract.md` 是公开契约说明，真实实现和测试是行为证据。修改 `/api/v1` 时按以下顺序检查：

1. Go router、domain/service/adapter、错误映射与测试。
2. `web/src/api.ts`、`web/src/types.ts` 及相关 view/test。
3. Android `data/model/Models.kt`、`data/api/ApiClient.kt`、repository/ViewModel/UI/test。
4. `companion-extension/` 的任务契约和测试。
5. `docs/api-contract.md`、`docs/browser-companion.md`、README 和兼容 fixture。

强制约定：

- 管理接口使用 `Authorization: Bearer <ADMIN_TOKEN>`，响应带 `Cache-Control: no-store`。
- 错误体保持 `{ error: { code, message, retryable, requestId } }`；稳定错误码、状态码和重试语义不能只改一端。
- 未知请求字段必须拒绝；PATCH 的“字段缺失”和显式 `false`/空字符串不能用 truthy 判断混淆。
- `accessToken` 只写不回显；站点响应不得包含明文、密文或可逆凭据材料。
- 新增可选响应字段通常兼容；删除、改名、改类型、收紧必填或改变状态码属于破坏性变更，必须给出兼容与回滚方案。
- 适配器能力是服务端权威。新增适配器时同步更新 Go registry、探测顺序、数据库约束、Web/Android 展示、契约、迁移和测试。

## PostgreSQL 与迁移

数据库变更是高风险改动，默认遵循“影响面 -> dry-run -> 备份/回滚 -> 写入 -> 后验复核”。生产写入、恢复、密码轮换、删除卷和发布必须另行确认。

- 迁移只前向追加。已经发布的 v1/v2/v3 SQL 及其空白字节不可修改，否则 checksum 漂移会阻止启动。
- 每个新版本只允许追加到 `server/internal/migrate/migrate.go`，不得修改已发布 SQL 的 UTF-8 字节。
- 同步更新 `server/internal/testdata/compatibility-vectors.json` 中的版本、SHA-256 和字节数，并让 Go 迁移测试通过。
- 启动迁移必须继续在事务与 PostgreSQL advisory transaction lock 内执行；未知版本、checksum 不匹配或执行失败必须阻止启动。
- 外部 HTTP 不能放在数据库事务中。签到、公告与浏览器任务的幂等和租约必须由条件更新、唯一约束或明确状态机兜底。
- 不启用 Ent `Schema.Create` 替代显式迁移。修改 Ent 模型时先改 `server/ent/schema/`，再在 `server/` 执行 `go generate ./ent` 并审查生成 diff。
- 迁移后的回滚程序必须能解释新枚举和新增 schema 版本；不满足时先处理数据与兼容构建，不能直接切旧镜像。

## 安全与凭据边界

- Go 日志必须脱敏 Authorization、token、Cookie、密码与密钥；错误响应不暴露堆栈、SQL、上游响应正文或内部路径。
- `APP_SECRET` 是服务端站点令牌解密根；备份数据库时必须同步保护原 secret，但不能将其放进仓库、镜像或文档。
- 上游请求必须继续经过 SSRF 防护：默认仅公网 HTTPS、禁止重定向、限制 DNS/IP、超时和响应大小。不得让新适配器直接使用裸 HTTP client 绕过 `netclient`。
- 启用私网或明文 HTTP 是部署者显式选择，不得作为代码默认值或测试便利长期保留。
- 不自动重试签到 POST。公告各来源允许部分成功，但必须保持 `partial` 语义和已成功数据。
- 浏览器伴侣不得申请 `cookies`、`webRequest` 或 `scripting` 权限，不得读取或上传 Cookie、LocalStorage、SessionStorage、Authorization、OAuth code、验证码或完整页面正文。
- 配对码、设备 token 和租约 token 在服务端只保存摘要；目标 URL origin 必须与站点 `baseUrl` 严格一致。
- 调试输出只能使用合成凭据。fixture 中的固定测试字符串不得误标为生产 secret。

## Android 与 MiuiX UI 规范

本节是 Android UI 的目标规范，不是对当前迁移中页面的合规声明。当前 `ui/ApiHubApp.kt` 仍有单文件页面、硬编码字符串、手写导航壳和旧圆角等债务；新增或修改 UI 必须按本节落地，触碰旧代码时顺手收敛，不得复制旧债务。

MiuiX 0.9.3 是当前 Android 组件基线。MiuiX 是所有新增页面的主组件和视觉体系；只有 MiuiX 没有等价能力时，才可局部使用 Compose Foundation 或 Material3，并在代码或 PR 中说明原因。不能因为 Mishka 使用过某个组件，就假定 APIHub 当前依赖一定导出了相同签名。

### 主题与颜色

- 应用根必须由 `APIHubTheme` 包裹，并在内部配置 `MiuixTheme`。
- 亮暗主题与语义色唯一源是 `androidApp/.../ui/Theme.kt`。屏幕层只使用 `MiuixTheme.colorScheme` 与 `LocalSemanticColors`，禁止散落新的 `Color(0x...)`。
- 品牌色和 success/warning/danger/info token 要与 `web/src/styles/theme.css` 保持语义一致；修改任何一端时同步检查另一端和 `ThemeTest`。
- 深浅色判定只在主题层完成。屏幕和可复用组件不得自行建立另一套主题判断或硬编码相反背景。
- 文字与背景必须保持至少 WCAG AA 的正文对比度；新增 token 时补充对比度测试。

### 组件优先级

1. 优先使用 `top.yukonga.miuix.kmp.*` 提供的 Card、Text、TextButton、TextField、Switch、TopAppBar、NavigationBar、Dialog 等组件。
2. 跨页面复用的行为与样式通过 `Theme.kt` 中现有 `ApiText`、`ApiCard`、`ApiPrimaryButton`、`ApiSecondaryButton`、`ApiTextField`、`ApiSwitch` 或新的 `ui/component` wrapper 收敛。
3. 只有 MiuiX 无等价组件时才使用 Compose Foundation/Material3；使用原因应能从代码上下文看出，且颜色仍走项目 token。
4. 不为单个页面复制一套 Card/Button/TextField 样式，也不在 Card 内再套装饰性 Card。

可复用 composable 的第一个可选参数使用 `modifier: Modifier = Modifier`，并把它应用到最外层语义节点。事件以 callback 向上传递；屏幕 composable 不直接持有 Activity、Keystore、DataStore 或网络 client。

- 操作型图标按钮至少有 35.dp 的可点击宽高，并使用语义色背景；图标必须有 content description，纯装饰图标明确使用空语义。
- 返回优先使用 MiuiX 或项目已有的统一 back icon wrapper；APIHub 当前 0.9.3 本地 AAR 未导出 Mishka 的 `MiuixIcons.Back`，不能直接抄入页面，必须以当前依赖导出或项目 wrapper 为准。其他工具操作同样不为单个按钮手绘 SVG 或引入整套图标依赖。

### 形状、间距与布局

- MiuiX 组件自带圆角/超椭圆语义时直接使用，不再额外 `clip`。
- 自定义形状按用途选择当前依赖实际导出的 squircle modifier：纯色且无需裁剪用 squircle background，图片或必须裁剪用 squircle clip，可点击表面用 squircle surface 加 clickable；每次引入前先用当前版本编译确认函数名和签名，不凭 Mishka 的版本猜 API。
- 只有极小状态 Badge 可使用 `clip(RoundedCornerShape(3.dp))` 例外；Badge 的字号、字重和等宽语义要稳定，不得用随机圆角掩盖布局问题。
- 新增独立页面默认采用 MiuiX `Scaffold` + 带 `scrollBehavior` 的 `TopAppBar` + `LazyColumn`。`LazyColumn` 应接入滚动结束触觉、overscroll 和 `nestedScroll(scrollBehavior.nestedScrollConnection)`；content padding 至少处理顶部 `innerPadding.calculateTopPadding()`，末尾用 `Spacer(Modifier.height(24.dp).navigationBarsPadding())` 吸收导航栏和留白。
- 二级页面不要额外接收一个手写 `bottomPadding` 参数；由外层 Scaffold 的 inset 或末尾 navigation-bar spacer 统一处理。只有确实由外层 bottom bar 承担 inset 时，才通过明确的 shell contract 透传，而不是每页自行猜 padding。
- 页面根处理 edge-to-edge、`safeDrawingPadding`、display cutout、IME 和 system bar inset。横屏有侧边缺口时补水平 cutout padding；动态列表优先 `LazyColumn`，末尾必须为导航栏和触控区域留出空间。
- 内容横向留白以 12-16.dp 为基线，页面级大间距 24-32.dp；同类元素保持一致，不通过随机 margin 修视觉问题。
- 触控区域不得小于平台可访问性基线。图标按钮必须有可读的 content description；纯装饰图标明确标为空语义。
- 固定格式控件要有稳定尺寸或响应式约束，加载、选中、错误和长文案不能导致布局跳动或相互遮挡。
- 小屏不得依赖横向滚动完成主要操作。现有底部导航是迁移期实现；新增顶级入口前先评估导航容量，避免继续堆文本按钮。

### 透明栏、宽屏与迁移规则

- 当前 APIHub 没有 `BlurredBar`、`AdaptiveTopAppBar` 或 NavigationRail 公共实现；不得直接复制 Mishka 的这些符号。未来引入半透明顶栏/底栏时，必须先建立共享 wrapper：有 backdrop 时使用透明栏和内容 layer 抓取，没有 backdrop 时回退到 `MiuixTheme.colorScheme.surface`，不能让每个页面各写一套毛玻璃逻辑。
- 宽度达到 600.dp 后使用窗口尺寸而不是设备型号做 adaptive 分支。目标是可展开的 NavigationRail、固定不折叠的宽屏顶栏、手机与宽屏共用页面内容 lambda，以及 800.dp 内容上限；LazyColumn 保持全宽，只把 side padding 放入 content padding，不能压窄滚动节点制造死区。
- 宽屏/折叠屏、分屏、字体缩放、RTL、TalkBack 和键鼠输入属于 UI 设计约束。APIHub 尚未有统一 `WindowSize` helper 时，先补 helper 和测试，再在页面内添加分支；不得散落 `width >= ...` 判断。
- 参考项目的具体屏幕名、Tab 数、图标映射、代理/ROOT/JNI 逻辑、`GroupedCardItems` 文件路径和 Dialog 类名不属于 APIHub 事实。可迁移的是布局、性能、状态和可访问性原则；迁移时必须换成 APIHub 当前入口和 wrapper 名称。

### 卡片、列表与性能

- 多组件卡片在 LazyColumn 中按可见行拆成独立 item，使用稳定 key；需要视觉连续时通过项目 wrapper 组合首/中/末段，不把整张可变长卡片一次性组合。拆分不得默认增加动画，展开/收起动画交给 item 级 `animateItem` 或等价机制。
- 有圆角的首/末段必须裁剪点击涟漪，中间段使用无裁剪背景；先以实际 MiuiX 组件行为和滚动性能为证据，再决定是否引入新的 wrapper。
- 卡片项统一以水平 12.dp、底部 12.dp 为基线；表单 TextField 默认不包 Card。页面级大间距使用 24-32.dp，同类元素保持一致，不通过随机 margin 修视觉问题。
- 列表、网格、表单和 Dialog 的性能问题用 Compose UI test、Macrobenchmark、Perfetto 或 FrameTimeline 取证，不用肉眼“感觉流畅”代替验证。

### 页面状态与交互

- UI 通过 `MainUiState` 单向渲染，业务状态、请求去重、竞态失效和异步取消留在 ViewModel/repository。`MainUiState` 及跨节点状态应保持 `@Immutable`；动态大集合使用稳定 key 和 immutable collection，缺少依赖时把依赖变更纳入同一任务验证。
- Flow 在 Compose 中使用 `collectAsStateWithLifecycle()`，不用无生命周期的 `collectAsState()`。
- 不在 composition 阶段调用 ViewModel action，也不在 Composable 正文直接做网络、DB、IO 或启动不可控协程。
- loading、error、empty、disabled、retry、offline、timeout、401/session expired 都要可达。有缓存内容时保留内容并显示局部错误，不因刷新失败切换成全屏错误。取消不作为失败文案。
- 同一操作通过稳定 action key 防重复提交。忙碌态必须禁用对应控件，但不能无差别锁死整个应用。
- 错误文案面向用户且不泄露 token、URL query、堆栈或底层异常正文。`CancellationException` 不显示为失败。
- 编辑 Dialog 的按钮顺序统一为“未修改 / 取消 / 确认”；长内容限制内容区高度，内部滚动，操作按钮固定在底部。轻量结果由 UI 层 Toast/Snackbar 提示，ViewModel/repository 不持有 Activity context。
- 新增或修改的用户可见字符串应迁移到 Android string resource；新增时同时维护默认语言和项目支持的中文资源，key 采用页面/用途命名，通用按钮使用 `common_` 前缀。不要继续扩大 `ApiHubApp.kt` 的硬编码中文债务。日志消息英文，代码注释只解释非显然约束。
- 图标优先使用 MiuiX 已提供的语义图标；没有合适图标时复用项目图标方案，不为单个按钮手绘 SVG 或新增整套依赖。

### Android 文件边界

- `ui/Theme.kt`：主题、语义 token 和薄组件 wrapper。
- `ui/ApiHubApp.kt`：当前导航壳与页面组合。新增复杂页面、dialog 或长列表时拆到独立文件，避免继续扩大单文件。
- `ui/MainViewModel.kt`：页面状态、动作编排、SavedState 与竞态失效；不放 Compose UI。
- `data/model/Models.kt`：与 `/api/v1` 对齐的序列化模型；字段变更必须检查 Web/Go 契约。
- `data/api/ApiClient.kt`：HTTP、错误解码、超时与 base URL 安全；不放 UI 文案以外的业务决策。
- `data/ApiHubRepository.kt`：会话与 client 所有权、聚合请求；不直接访问 Compose。
- `data/CredentialStore.kt` 与 `data/security/`：凭据持久化和 Keystore；修改时必须补 JVM/仪器测试并审查备份规则。

### Android UI 验证

- 主题/token 改动：`ThemeTest`，至少覆盖亮暗主题和对比度。
- ViewModel/状态改动：`MainViewModelTest`，覆盖断开/重连、旧请求回写、401 清理和显式 `false`。
- API/序列化改动：`ApiClientTest` 与 repository test，覆盖未知字段、错误体、超时、取消和 client 关闭。
- MiuiX 组件/可访问性交互：更新 `MiuixComponentsTest` 并运行 `connectedDebugAndroidTest`。
- Release 约束、网络安全或 R8 影响：必须跑 `lintDebug assembleRelease`，不能只跑 debug 编译。
- 宽屏、edge-to-edge、字体缩放、RTL、TalkBack 或视觉基线改动：至少覆盖手机/宽屏、亮暗主题与长文案；具备环境时补截图测试、真机或 managed device 证据。
- 没有真机、release 或旧系统证据时，明确写“未完成 Android 发布级验证”，不得把 debug/emulator 结果包装成发布可用。

## Web UI 规范

Web 不引入 MiuiX 运行库；它通过相同品牌/语义 token 与 Android 保持产品一致，而不是复制 Android 组件实现。

- 颜色只取 `web/src/styles/theme.css` CSS variables；view/component 中不散落新的品牌色十六进制。
- 页面壳复用 `AppShell.vue`，图标复用 `AppIcon.vue`，加载/错误/空状态复用 `PageState.vue`。
- 操作型图标按钮提供 `aria-label`，表单有 label，错误使用 `role="alert"`，异步状态使用合适的 `aria-live`。
- 保持桌面与 Pixel 7 Playwright 项目都可用；表格/操作区在窄屏必须重排，不能只缩小字体。
- 不把上游公告作为 HTML 注入；默认使用 Vue 文本插值或 `textContent` 语义。
- 修改 Web 后运行 build，并审查 `server/internal/webui/dist/` 的新增、删除和 hash 变化；不要只提交源码。

## Go 与适配器约定

- Go 代码必须 `gofmt`，错误用 `%w` 保留因果链，所有 I/O 接受并传播 `context.Context`，关闭失败不能静默覆盖主错误。
- Go 是唯一后端实现；数据库迁移字节、AES-GCM 兼容向量、API 状态码、内容类型、缓存头和稳定响应字段以 Go 实现及其跨端测试为准。
- 适配器必须通过 registry/capability 暴露能力。`auto` 只用于受控探测，持久化必须是具体 adapter；探测失败不能静默回退为 New API。
- 验证码、Turnstile 或二次验证统一映射为 `manual_required` / `MANUAL_ACTION_REQUIRED`，不能伪报成功或自动绕过。
- 公告两来源可独立失败；成功来源仍提交，整体状态为 `partial`。不要因一个来源失败丢弃另一个来源数据。
- 定时任务与手动任务共享服务层和幂等规则，不能复制一条行为不同的旁路。

## 浏览器伴侣约定

- 扩展只在用户主动加载且使用自己的 Chrome 会话时工作；服务端永远不接管浏览器身份。
- `manifest.json` 权限保持最小化。新增权限、host permission、存储字段或页面注入行为属于安全变更，必须更新 `docs/browser-companion.md` 与测试。
- 页面 agent 只回传有限状态、短说明和允许的余额文本；不得上传页面全文或浏览器存储。
- 设备撤销、任务租约、心跳、重领和终态回传保持幂等。修改一端时同步检查 Go、Web 管理页、扩展与契约测试。

## 配置、部署与供应链

- 环境变量定义、默认值、Compose 透传和 Go 解析必须一致。修改配置时搜索 `.env.example`、README、Dockerfile、Compose、服务实现与测试。
- 项目版本遵循 [SemVer 2.0.0](https://semver.org/lang/zh-CN/)；公开契约包括 `/api/v1`、配置语义、数据库兼容边界、浏览器伴侣协议和客户端/服务端契约。从 `1.0.0` 起，不兼容变更递增 major，向下兼容的功能新增或废弃声明递增 minor，向下兼容的修复递增 patch。`0.y.z` 视为初始开发阶段：新增功能或不兼容变更递增 minor，兼容修复递增 patch。
- 发布标签使用 `vX.Y.Z`（可带 SemVer 先行版本）；同次发布的 Git 标签、Go `VERSION`、OCI version label 和 Android `versionName` 必须一致，Android `versionCode` 单独单调递增。已发布版本和标签不得移动、覆盖或复用。
- 镜像 tag 与 digest 必须一起更新，并核对 Dockerfile、Compose、CI service、CI 模拟上游和扫描配置中的所有引用。
- Go 运行镜像使用 Distroless `nonroot`；保持只读根文件系统、`/tmp` 限额、capability drop、`no-new-privileges`、内存与 PID 限制。Node 仅作为 Web 构建工具链，不进入运行镜像。
- 发布使用已经验证的不可变 digest，不在切换窗口重新构建，也不使用可变 tag 替代回滚证据。
- 数据库或镜像发布前记录备份、原 `APP_SECRET`、当前 Go digest、回滚命令和人工批准。
- 本地 OCI provenance、SBOM 或扫描结果不能替代 registry 签名与身份验证。

## 验证矩阵

先跑与改动最接近的快速门槛，再按影响面扩大。不要为纯文档改动伪造全栈通过结论。

### 文档与仓库一致性

```powershell
git diff --check
git status --short
```

同时人工核对文档中的路径、脚本名、环境边界和 Go 唯一后端表述。Markdown 命令块只记录仓库真实存在的命令。

### Go 后端基线

```powershell
cd server
go test ./...
go test -race ./...
go vet ./...
```

修改 Ent schema 后额外运行：

```powershell
cd server
go generate ./ent
go test ./...
```

### Vue 管理页

```powershell
cd web
npm ci
npm run typecheck
npm run lint
npm test
npm run build
npm run e2e
```

E2E 需要 Playwright Chromium。源码构建通过后还要检查嵌入产物 diff。

### Android

```powershell
cd androidApp
./gradlew.bat testDebugUnitTest lintDebug assembleRelease
./gradlew.bat connectedDebugAndroidTest
```

本机没有 API 36 模拟器时不得声称仪器测试通过；可由 CI 的 Android instrumented job 补证据。

### PostgreSQL 集成

只允许连接专用 `apihub_test` 数据库：

```powershell
$env:APIHUB_INTEGRATION_DATABASE_URL = "postgresql://apihub:<password>@127.0.0.1:5432/apihub_test"
cd server
go test -tags=integration -v ./internal/service
```

严禁把开发或生产库 URL 误设为集成测试目标。数据库写测试前先确认目标库名和数据可丢弃。

### 完整交付门槛

涉及跨实现契约、迁移、容器、安全、Android UI 或发布时，以 `.github/workflows/ci.yml` 的对应 job 为最终门槛。CI 未实际运行或结果不可见时，明确写“本地已验证，CI 待运行”。

## 按改动类型的最低要求

| 改动 | 最低检查 |
| --- | --- |
| 纯文档 | diff check、路径/命令/现状核对 |
| Go 实现 | Go test + race + vet；必要时 lint/vuln |
| Web UI | typecheck + lint + unit + build；交互/响应式加 E2E |
| Android UI | unit + lint + release；组件交互加 instrumented test |
| API 契约 | Go + Web + Android 消费方 + 扩展 + docs/fixture |
| 数据库迁移 | Go migration test + fixture + 专用 PostgreSQL integration |
| 浏览器伴侣 | Go service + Web 管理页 + extension test + 安全文档 |
| Docker/发布 | Go smoke、扫描、双架构 OCI、SBOM/provenance/digest/回滚证据 |

## 完成定义

交付前逐项确认：

- 需求与实际 diff 一致，没有无关重构或覆盖用户改动。
- 所有修改对象的生产者、消费者、配置、测试、文档和生成物已检查。
- Go 是唯一后端入口，Web 构建工具链没有被误带入运行镜像。
- API、迁移、凭据、SSRF、浏览器伴侣和回滚不变量仍成立。
- Android 新 UI 以 MiuiX 为核心，颜色走 token，状态和生命周期正确，Web token 如有需要同步。
- 已运行的验证命令、结果和未运行项如实记录；失败项说明是否由本次改动引起。
- `git diff --check` 通过，`git status --short` 中的新文件、删除的 hash 产物和未跟踪文件都已解释。

## 参考来源与适用边界

MiuiX UI 组织方式参考 `YuKongA/Mishka` 根 `CLAUDE.md`，读取基线为 2026-07-18 的 commit `62a22009fb088e25cfb724bc8404ee1c07c73045`，本仓库访问核对日期为 2026-07-22。Android UI 章节基本沿用其 MiuiX 组件优先级、squircle 用途分层、页面骨架、inset、透明栏、宽屏、卡片性能、Dialog、i18n、生命周期感知 Flow、immutable state 与分层验证原则；只移除了 Mishka 专属的屏幕名、Tab 数、文件路径、图标映射、代理、ROOT、Room、Koin、Navigation3 和 JNI 约束。涉及具体 MiuiX 函数时仍以 APIHub 当前 0.9.3 依赖的编译结果为准。
