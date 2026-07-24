# APIHub · API 契约

## 通用约定

- Base path：`/api/v1`
- 管理接口认证：`Authorization: Bearer <ADMIN_TOKEN>`
- 时间：RFC 3339 UTC 字符串。
- 内容类型：`application/json`。
- 未知请求字段：拒绝并返回 `VALIDATION_ERROR`。
- 错误结构：`{ "error": { "code": string, "message": string, "retryable": boolean, "requestId": string } }`
- 管理响应：`Cache-Control: no-store`。

## 资源模型

### Site

响应字段：

- `id`、`name`、`baseUrl`、`adapter`、`userId`
- `enabled`、`checkinEnabled`、`announcementEnabled`
- `checkinCron`、`announcementCron`、`timezone`
- `credentialConfigured`、`consecutiveFailures`、`capabilities`
- `createdAt`、`updatedAt`

写入字段不接受 `id`、密文、运行状态或审计时间。`accessToken` 仅写入，不回显。

`adapter` 的持久值为 `new-api | sub2api | zen-api`。创建或修改站点时还可提交 `auto`，服务端会通过受 SSRF 防护的只读探测解析为一个持久值；无法可靠识别时返回 `VALIDATION_ERROR`，不静默猜测。

`capabilities` 是服务端根据适配器生成的只读对象：

    {
      "checkin": true,
      "announcements": true,
      "requiresUserId": true
    }

客户端未显式提交 `checkinEnabled` / `announcementEnabled` 时，创建接口按能力默认启用；显式请求启用适配器不支持的能力时返回 `VALIDATION_ERROR`。`userId` 仅在 `requiresUserId` 为 `true` 时必填。

### CheckinRun

状态：`running | success | already_checked | manual_required | failed | skipped`

同一 `siteId + localDate` 只有一个当前结果；失败记录可重试，成功、已签到和需人工处理为当天终态。

### Announcement

来源：`status | notice`。以 `siteId + fingerprint` 唯一去重。

## 端点

### 健康检查

- `GET /health/live`：进程存活，不访问外部依赖。
- `GET /health/ready`：检查数据库可读写状态。

### 概览

- `GET /api/v1/summary`
- `200`：站点数、按各站点时区计算的今日签到状态统计和未读公告数。

### 站点适配器

- `GET /api/v1/site-adapters`
- `200`：返回可选适配器、展示名称和能力，不包含凭据或上游探测结果。
- 当前能力：
  - `new-api`：令牌签到、结构化公告与文本通知，需要用户 ID。
  - `sub2api`：JWT 公告聚合；浏览器登录/OAuth/页面签到不属于服务端能力，因此签到保持关闭。
  - `zen-api`：Bearer 令牌签到；当前不声明公告能力。

### 站点

- `GET /api/v1/sites`
- `GET /api/v1/sites/{siteId}`
- `POST /api/v1/sites`：创建站点，成功返回 `201` 和 `Location`。
- `PATCH /api/v1/sites/{siteId}`：局部更新；字段缺失表示不变，`false` 和空字符串按契约校验，不使用 truthy 判断。

创建请求：

    {
      "name": "示例公益站",
      "baseUrl": "https://example.com",
      "adapter": "auto",
      "userId": "123",
      "accessToken": "masked-in-docs",
      "enabled": true,
      "checkinEnabled": true,
      "announcementEnabled": true,
      "checkinCron": "15 8 * * *",
      "announcementCron": "*/30 * * * *",
      "timezone": "Asia/Shanghai"
    }

### All API Hub 站点导入

- `POST /api/v1/site-imports`：预览或应用 `qixing-jk/all-api-hub` 的 JSON 备份；管理认证与 `Cache-Control: no-store` 规则不变。
- 请求正文上限为 4 MiB，最多读取 100 个账号。外层仅接受 `source`、`dryRun`、`backup`；`backup` 内未知字段为兼容上游演进而忽略。
- `source` 当前只接受 `all-api-hub`；`dryRun` 必填。`backup` 必须是对象，支持缺省/`1.0` 的旧结构和 `2.0`，要求存在 `timestamp`，账号数组可位于 `accounts`、`accounts.accounts` 或旧版 `data.accounts`。

请求：

    {
      "source": "all-api-hub",
      "dryRun": true,
      "backup": {
        "version": "2.0",
        "timestamp": 1710000000000,
        "accounts": {
          "accounts": []
        }
      }
    }

导入只消费站点账号字段；偏好、标签、书签、渠道配置、API 凭据配置、Cookie、刷新令牌和历史数据均忽略。`account_info.access_token` 会按当前 `APP_SECRET` 重新加密后写入，响应、日志和错误不回显明文或密文。

映射与跳过规则：

- 上游明确属于 New API family 的 `one-api | new-api | anyrouter | Veloera | one-hub | done-hub | v-api | VoAPI | Super-API | Rix-Api | neo-Api | wong-gongyi` 映射为 `new-api`。
- `sub2api` 映射为 `sub2api`；其他站点类型保持逐项 `UNSUPPORTED_SITE_TYPE`，不使用网络探测猜测。
- `authType: cookie`、缺少访问令牌、New API family 缺少用户 ID、字段或 URL 非法的账号逐项跳过。
- URL 经过与普通站点创建相同的协议、私网和规范化校验。同一备份中规范化 URL 重复时保留第一项；数据库已存在相同 `baseUrl` 时按 `ALREADY_EXISTS` 跳过，因此重复应用同一备份不会重复创建。
- `disabled` 映射为站点 `enabled` 的反值；自动签到仅在上游 `checkIn.enableDetection` 为 `true` 且 `autoCheckInEnabled` 未显式关闭时启用。公告同步按目标适配器能力启用。计划与时区使用普通创建接口的默认值。

响应不包含任何令牌：

    {
      "data": {
        "source": "all-api-hub",
        "sourceVersion": "2.0",
        "dryRun": true,
        "summary": {
          "total": 2,
          "ready": 1,
          "created": 0,
          "skipped": 1,
          "duplicates": 1,
          "unsupported": 0,
          "invalid": 0
        },
        "items": [
          {
            "index": 0,
            "name": "示例公益站",
            "baseUrl": "https://example.com",
            "siteType": "new-api",
            "adapter": "new-api",
            "status": "ready"
          }
        ]
      }
    }

`dryRun: true` 零写入。`dryRun: false` 会在单一数据库事务内创建所有 `ready` 项，任一写入失败则整体回滚，成功后统一重载调度器；导入过程中不发起上游 HTTP 请求。并发创建导致唯一约束冲突时返回 `409 CONFLICT`，客户端应重新 dry-run。格式错误、未知版本、缺少账号数组或超过 100 个账号返回 `422 VALIDATION_ERROR`。

### 签到任务

- `POST /api/v1/sites/{siteId}/checkin-runs`：手动创建/重试当天签到任务。
- `GET /api/v1/checkin-runs?limit=50&siteId=<id>`：按 `startedAt DESC, id DESC` 返回，`limit` 范围 1–100。
- `POST` 可能返回 `200`（当日已有终态）或 `201`（本次创建/执行）。

### 公告同步与查询

- `POST /api/v1/sites/{siteId}/announcement-syncs`：同步结构化公告和文本通知。
- `GET /api/v1/announcements?limit=50&siteId=<id>`：按 `publishedAt/firstSeenAt DESC, id DESC` 返回。
- `PATCH /api/v1/announcements/{announcementId}`：当前仅接受 `{ "read": true | false }`。

## 稳定错误语义

管理 API 的顶层错误体可能返回：

- `AUTH_REQUIRED`：401，缺失或错误管理员令牌。
- `BAD_REQUEST`：400，请求 JSON 无法解析。
- `PAYLOAD_TOO_LARGE`：413，请求正文超过 64 KiB。
- `UNSUPPORTED_MEDIA_TYPE`：415，请求使用了服务不支持的媒体类型。
- `RATE_LIMITED`：429，请求频率超过限制，可重试。
- `VALIDATION_ERROR`：422，输入不符合契约。
- `NOT_FOUND`：404，资源不存在或不可见。
- `CONFLICT`：409，唯一约束或并发冲突。
- `SITE_URL_BLOCKED`：422，站点协议、字面量 IP 或 DNS 解析结果不符合访问策略。
- `INTERNAL_ERROR`：500，未知服务端错误，不暴露堆栈或数据库信息。

签到任务执行错误会先落库，再随 `CheckinRun` 返回；`errorCode` 可能为 `UPSTREAM_TIMEOUT`、`UPSTREAM_REJECTED`、`UPSTREAM_RESPONSE_TOO_LARGE`、`UPSTREAM_REDIRECT_BLOCKED`、`SITE_URL_BLOCKED` 或 `INTERNAL_ERROR`。验证码、Turnstile 或二次验证使用 `status: manual_required` 与 `errorCode: MANUAL_ACTION_REQUIRED`，不是顶层 HTTP 409。

## 浏览器伴侣

- 管理端 `POST /api/v1/companion-pairing-codes` 创建五分钟、单次使用的配对码。
- 管理端 `GET /api/v1/companion-devices` 列设备；`POST /api/v1/companion-devices/{deviceId}/revocations` 撤销设备。
- 管理端 `POST /api/v1/sites/{siteId}/browser-tasks` 创建浏览器任务，请求 `{ "targetUrl": string }`；URL origin 必须与站点 `baseUrl` 完全一致。同一站点已有 `queued` 或 `leased` 任务时返回 `409 CONFLICT`。
- 管理端 `GET /api/v1/browser-tasks?limit=50` 查询任务。
- 伴侣端 `POST /api/v1/companion/pairings` 消费配对码并仅返回一次设备令牌。
- 伴侣端领取、续租和回传分别使用 `/api/v1/companion/tasks/claims`、`/{taskId}/heartbeats` 与 `/{taskId}/results`。
- 每台设备同一时刻最多持有一个租约；已有活动租约时再次领取返回 `204`，不会额外占用队列任务。

任务状态为 `queued -> leased -> success | already_checked | manual_required | failed`。领取带两分钟租约，心跳续租，过期后可重新领取；终态结果回传幂等。设备令牌、配对码、租约令牌在数据库只保存 SHA-256 摘要。任务和响应不包含站点访问令牌、用户 ID、Cookie、LocalStorage、OAuth code 或验证码。详细请求、错误和回滚约束见 `docs/browser-companion.md`。

## 性能与可靠性预算

- JSON body 上限 64 KiB；列表最大 100 条。
- 单个上游请求默认 10 秒超时，响应正文最多 1 MiB，不自动跟随重定向。
- 签到 POST 不做网络级自动重试；调度器只在下一次明确触发时重试。
- 公告 GET 可在单次同步中独立失败；成功来源仍可提交。
- 单站点同类任务同一时刻只运行一个。

## 兼容与回滚

- MVP 为内部 v1 API，新增可选响应字段视为兼容；删除、改名、改类型、收紧必填和改变状态码均视为破坏性变更。
- 数据库迁移只前向扩展；回滚代码必须能读取已有字段。
- 调度可通过停用站点或关闭对应功能立即停止，不删除历史记录。
- 本次新增的 `capabilities` 响应字段与可选创建字段保持旧 New API 客户端兼容；旧客户端不传 `adapter` 时仍按 `new-api` 创建。
- 数据库 v2 迁移只扩展 `sites.adapter` 的允许值，不修改 v1 SQL 或校验和；回滚到旧版本前必须确保库中不存在非 `new-api` 站点，否则旧代码无法解释新枚举。
- 数据库 v3 新增浏览器伴侣表和索引，v4 增加每设备单活动租约的部分唯一索引；两者均不修改更早迁移的 SQL 或校验和。旧构建不认识 v3/v4 时会拒绝启动，回滚必须使用包含全部四个迁移定义的兼容构建。
