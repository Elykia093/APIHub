# 浏览器伴侣契约

## 目标与边界

浏览器伴侣让 APIHub 把必须依赖真实浏览器会话的签到任务交给用户自己的 Chrome 执行。APIHub 负责任务、状态、结果与审计；扩展只在已配对的本机浏览器中打开站点页面、复用浏览器现有登录态、执行页面按钮签到，并在遇到登录或人机验证时默认把窗口前置等待用户处理。扩展弹窗可关闭这个前置行为。

浏览器伴侣不得读取或上传 Cookie、LocalStorage、SessionStorage、Authorization、OAuth code、验证码或页面完整正文。扩展只能回传任务状态、受限长度的说明、余额文本和时间。验证码与 Turnstile 只允许人工完成，不允许绕过。

## 资源与状态机

- `companion_pairing_codes`：管理员创建的单次配对凭据，5 分钟过期，只保存 SHA-256 摘要。
- `companion_devices`：已配对浏览器设备，只保存随机设备令牌的 SHA-256 摘要；令牌只在配对成功时返回一次，可由管理员撤销。
- `browser_tasks`：浏览器签到任务。状态为 `queued -> leased -> success | already_checked | manual_required | failed`；租约过期会重新进入 `queued`，设备心跳可延长自己的租约。

任务 URL 必须是 HTTP(S)，且 origin 必须与站点 `baseUrl` 完全一致。服务端不接受任意跨域任务，也不把站点访问令牌、用户 ID 或管理令牌放进任务响应。

同一站点同一时刻最多存在一个 `queued` 或 `leased` 浏览器任务。重复创建返回 `409 CONFLICT`；任务进入终态后可创建下一条任务。
同一设备同一时刻最多持有一个 `leased` 任务；并发领取时其余请求返回 `204`，任务仍留在队列中供其他设备领取。
扩展将当前任务和租约暂存在 `chrome.storage.session`，并在 Manifest V3 service worker 重启后恢复执行；领取侧使用单飞保护，避免闹钟和手动轮询并发占用多个任务。

## 管理 API

全部使用现有管理员 Bearer Token，响应保持 `{ "data": ... }` 包装和现有错误模型。

| Method | Path | 输入 | 成功 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/companion-pairing-codes` | `{}` | `201`，返回一次性 `code` 与 `expiresAt` |
| `GET` | `/api/v1/companion-devices` | 无 | `200`，不返回令牌摘要 |
| `POST` | `/api/v1/companion-devices/{deviceId}/revocations` | 无 | `201`，撤销设备并释放其未完成任务 |
| `POST` | `/api/v1/sites/{siteId}/browser-tasks` | `{ "targetUrl": string }` | `201`，创建 `queued` 任务 |
| `GET` | `/api/v1/browser-tasks?limit=1..100` | 无 | `200`，按创建时间倒序返回 |

## 伴侣 API

`POST /api/v1/companion/pairings` 不需要管理员令牌，只接受一次性配对码并受限流保护。其余接口使用配对成功时返回的设备 Bearer Token。

| Method | Path | 输入 | 成功 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/companion/pairings` | `{ "code": string, "deviceName": string }` | `201`，只返回一次 `deviceToken` |
| `POST` | `/api/v1/companion/tasks/claims` | 无 | `200` 返回任务，或 `204` 表示暂无任务 |
| `POST` | `/api/v1/companion/tasks/{taskId}/heartbeats` | `X-Companion-Lease` 请求头 | `200`，仅允许被分配设备续租 |
| `POST` | `/api/v1/companion/tasks/{taskId}/results` | `{ "leaseToken", "status", "message", "balance"? }` | `200`，终态重放返回同一结果 |

结果 `status` 只允许 `success`、`already_checked`、`manual_required`、`failed`。未知字段拒绝；`message` 最长 500 字符，`balance` 最长 128 字符。设备只能操作分配给自己的任务；撤销、错误设备、错误任务状态分别返回现有 `AUTH_REQUIRED`、`NOT_FOUND` 或 `CONFLICT` 语义。

## 兼容、验证与回滚

本功能只增加 API、表和响应，不修改现有站点、签到、公告契约。数据库 v3 新增伴侣表和索引，v4 增加每设备单活动租约约束，不修改更早迁移的 SQL。回滚必须使用包含 v1-v4 迁移定义但可关闭伴侣入口的 Go 构建；不包含 v3/v4 定义的旧构建会按现有策略拒绝启动。

最小验证覆盖：配对码过期与单次消费、设备令牌错误/撤销、同源 URL 拒绝、并发领取、租约过期重领、跨设备心跳/结果拒绝、终态幂等、未知字段、敏感字段不出现在响应和日志，以及 Go/Web/Android/扩展契约回归。
