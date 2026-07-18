# APIHub · PostgreSQL 设计与运维边界

## 目标环境

- Compose 固定 PostgreSQL 17 Alpine；其他 PostgreSQL 版本尚未作为兼容目标验证。
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

当前本机没有 Docker、`psql` 或真实 PostgreSQL 服务，因此本机只完成了 SQL 静态审查、Node `pg-mem` 与 Go 非集成测试。仓库已增加以专用 `apihub_test` 数据库为门禁的 PostgreSQL 17 集成测试和 CI service；只有对应 CI job 实际通过后，才能把空库、v1→v2、checksum、幂等、去重、排序和重开持久化标为已验证。锁行为、备份恢复和生产数据量下的查询计划仍需在具备代表性数据的环境补证据。
