# APIHub 参考实现与取舍

## 参考来源

- 本地参考包：`api-auto-chekin0624.zip`，MIT License，提供 NewAPI、Sub2API、ZenAPI、页面签到、余额与配置导入导出等浏览器扩展流程。
- 上游参考项目：[qixing-jk/all-api-hub](https://github.com/qixing-jk/all-api-hub)，AGPL-3.0，提供多站点类型、能力化适配器、自动签到、公告聚合、余额/用量与导入导出等完整浏览器扩展能力。
- 上游参考项目：[cita-777/metapi](https://github.com/cita-777/metapi/tree/41767a65ec8e5470a9a70f4615b47dc24949afff)，MIT License；核对 commit `41767a6`（2026-06-24），提供 New API、One API、OneHub、DoneHub、Veloera、AnyRouter、Sub2API 等站点聚合、模型发现、智能路由与用量管理。
- 上游参考项目：[aceHubert/newapi-ai-check-in](https://github.com/aceHubert/newapi-ai-check-in/tree/082e18b8abd875f813302cb63d73edec6e9c49fa)，BSD-2-Clause；核对 commit `082e18b`（2026-05-11），提供多账号签到、GitHub/Linux.do OAuth、Cookie/系统访问令牌认证、浏览器自动化、Cloudflare 处理与多渠道通知。
- 上游参考项目：[Jasonliu-0/Newapi-checkin](https://github.com/Jasonliu-0/Newapi-checkin/tree/9ab62be42f38783da7767a2b1b810c33561d6d0e)，MIT License；核对 commit `9ab62be`（2026-05-21），提供基于 Session Cookie 的 NewAPI HTTP 直连、多站点/多账号签到、GitHub Actions 与钉钉通知。
- 上游参考项目：[ken861222/newapi-manager](https://github.com/ken861222/newapi-manager/tree/43e2fb0027a2f13cd74ace8eeed2275c1572a127)，README 声明 MIT，但仓库未提供独立 `LICENSE` 文件；核对 commit `43e2fb0`（2026-06-19），提供 NewAPI 站点管理、余额与历史消耗监控、自动/批量签到和请求日志追踪。

外部仓库信息最近访问日期为 2026-07-19；新增来源使用固定 commit 链接，避免后续默认分支变化导致证据漂移。

本项目只参考上述来源的公开功能与协议边界，没有复制其源码，尤其未引入 AGPL-3.0 项目的代码。Metapi 的职责包括模型代理、智能路由与用量管理，APIHub 只参考其站点覆盖和组织边界，不引入这些网关职责。Jasonliu-0 方案依赖浏览器 Session Cookie，aceHubert 方案包含 OAuth、浏览器身份状态与页面挑战处理；这些证据进一步支持 APIHub 继续采用“只保存加密站点令牌、无法安全服务端化的流程降级为人工处理”的边界。服务端实现继续沿用现有模块化单体、PostgreSQL、受控 HTTP 客户端和独立测试体系。

## 已采用

| 能力 | APIHub 方案 | 采用原因 |
| --- | --- | --- |
| 站点类型 | `new-api`、`sub2api`、`zen-api` | 覆盖参考实现共同或明确支持的主要公益站类型 |
| 自动识别 | 只读探测后保存具体类型 | 避免运行时长期依赖模糊 `auto` 分支 |
| 能力模型 | 签到、公告、用户 ID 要求分别声明 | 不把“支持账号”误写成“支持所有操作” |
| New API | 令牌 + 用户 ID 签到，公告双源聚合 | 已有实现与测试基线稳定 |
| Sub2API | JWT 公告聚合 | 服务端可以安全完成；参考上游也把账号公告作为独立能力 |
| ZenAPI | Bearer 令牌接口签到 | 不依赖浏览器 Cookie 时可在服务端执行 |

## 明确不直接迁移

| 浏览器能力 | 原因 | 当前降级 |
| --- | --- | --- |
| linux.do OAuth 自动登录 | 依赖浏览器会话、标签页和用户交互 | 服务端不托管；可由本机浏览器伴侣做人机接力 |
| Cookie/LocalStorage 捕获 | 服务端没有浏览器安全上下文 | 不保存 Cookie，只保存加密令牌 |
| 页面按钮点击签到 | 页面身份可能与目标账号不一致 | 服务端不点击；管理员显式下发同源任务给已配对的本机浏览器伴侣 |
| Cloudflare/Turnstile 页面协助 | 涉及真实页面挑战与交互 | 浏览器伴侣前置页面供用户完成，超时标记 `manual_required`，不绕过验证 |
| 后台标签页余额抓取 | 服务端无法可信读取渲染后 DOM | 后续仅考虑有明确 API 契约的余额源 |

## 后续候选

1. 站点配置的加密导入导出与 dry-run 合并预览。
2. 只基于明确 API 契约的余额快照与历史。
3. One API、Veloera、VoAPI v2、AnyRouter 等独立适配器。
4. 多实例任务租约与可观测指标。

任何后续适配器都必须先证明真实端点、认证格式、成功/已签/需人工状态和错误语义，不能仅凭项目血缘归类。
