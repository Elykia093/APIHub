# APIHub · PostgreSQL 设计与运维边界

## 目标环境

- Compose 固定 PostgreSQL 18.4 Alpine；较低版本不作为兼容目标，是否可运行仅属尽力而为，不进入测试矩阵。
- 单管理员、单应用实例，默认连接池上限 5。
- 当前容量假设：少于 100 个站点；每站每天 1 条签到当前记录；公告和同步历史低频增长。超过该规模前需重新验证查询计划、保留期和索引体积。

## 模型与不变量

- `sites` 是站点主数据；`checkin_runs`、`announcement_sync_runs`、`announcements` 通过受限外键归属站点。
- `sites.base_url` 唯一，避免重复配置同一上游。
- `sites.adapter` 由 `CHECK` 约束限制为 `new-api`、`sub2api`、`zen-api`；API 输入中的 `auto` 不持久化。
- `checkin_runs(site_id, local_date)` 唯一，数据库兜底同站点同自然日签到幂等；失败/跳过状态可通过条件更新重试，终态不被覆盖。
- `announcements(site_id, fingerprint)` 唯一，正文 SHA-256 指纹负责公告去重。
- 状态、来源、非负计数均有 `CHECK` 约束；所有业务时间使用 `TIMESTAMPTZ`，站点自然日使用 `DATE`。
- 访问令牌仅以 AES-256-GCM 密文存入 `sites.access_token_ciphertext`。

## 访问路径与索引

- 站点：按 UUID 点查、按规范化 URL 唯一写入、按名称列出。
- 签到：按 `(site_id, local_date)` 点查/条件更新；按 `started_at DESC, id DESC` 查询最近记录。
- 今日概览：按 `local_date` 过滤并按状态聚合，使用 `(local_date, status)` 索引。
- 公告：按 `(site_id, fingerprint)` 点查/去重；按 `COALESCE(published_at, first_seen_at) DESC, id DESC` 查询信息流。
- 未读公告使用 `read_at IS NULL` 部分索引；外键和站点维度列表均有对应索引。
- 列表接口最多返回 100 条，不提供无界导出或深分页。

## 迁移与并发

- `schema_migrations` 记录已应用版本和 SQL SHA-256 校验和；未知版本或校验和漂移会阻止应用启动。
- v2 只扩展 `sites.adapter` 的允许值并保留 v1 校验和；回滚到只理解 `new-api` 的旧程序前，必须先确认没有新适配器数据。
- 启动期在单个事务中获取 PostgreSQL advisory transaction lock，再执行未应用迁移；失败回滚且应用拒绝启动。
- 迁移 SQL 只做前向扩展。进入真实生产数据阶段后，字段收紧、索引重建和大表变更必须改用 expand-contract，并单独评估锁级别。
- v3 新增 `companion_pairing_codes`、`companion_devices` 与 `browser_tasks`。配对码、设备令牌和任务租约仅保存 SHA-256 摘要；部分唯一索引保证同一站点最多一个活动任务，任务领取通过条件更新保证同一时刻最多一个设备成功租用，租约过期可重新排队。
- 签到执行权通过条件 `UPDATE`、唯一约束和 `INSERT ... ON CONFLICT DO NOTHING` 获取；外部 HTTP 不在数据库事务中执行。

## 连接池与超时

- 默认每实例 5 条连接、5 秒连接超时、15 秒 statement timeout。
- 总连接预算按 `应用最大副本数 × DATABASE_POOL_MAX + 迁移/运维预留` 计算，并低于数据库可用连接数。
- 如果引入 PgBouncer transaction pooling，需要重新验证 session 级配置和迁移连接行为。

## 上线前检查

1. 在目标 PostgreSQL 版本执行空库迁移，核对 `schema_migrations`、表、约束与索引。
2. 重启应用，确认迁移幂等且 readiness 能完成读写检查。
3. 用真实 PostgreSQL 运行站点创建、PATCH false、失败签到重试、终态重复签到和公告去重测试。
4. 核对连接使用率、慢查询、锁等待、死锁、autovacuum、存储增长和错误率。
5. 配置备份/PITR并演练恢复；恢复后用原 `APP_SECRET` 验证凭据可解密。

## 未验证项

当前本机没有 Docker、`psql` 或真实 PostgreSQL 服务，因此本机只完成了 SQL 静态审查和 Go 非集成测试。仓库已增加以专用 `apihub_test` 数据库为门禁的 PostgreSQL 18.4 集成测试和 CI service；只有对应 CI job 实际通过后，才能把完整集成门禁标为已验证。2026-07-22 已在独立 Debian 12 Docker 主机（Docker 29.6.2、Compose 5.3.1）完成 PostgreSQL 18.4 空库启动、v1→v2→v3 迁移、浏览器伴侣写路径、重启持久化和容器加固 smoke；Docker Hub 构建阶段使用 DaoCloud 加速，Distroless 运行镜像使用已验证的 NJU 镜像路径。锁行为、备份恢复和生产数据量下的查询计划仍需在具备代表性数据的环境补证据。

PostgreSQL 18 官方镜像的默认 `PGDATA` 为 `/var/lib/postgresql/18/docker`，Compose 因此把新卷 `apihub-postgres-18-data` 挂载到 `/var/lib/postgresql`。不能把旧 `apihub-postgres-data` 物理卷直接挂给 18；升级前必须先从旧实例执行逻辑 `pg_dump`，再在全新的 18.4 卷中恢复并校验。原 `APP_SECRET` 也必须同步保留，否则站点访问令牌无法解密。
