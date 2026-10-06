# SQLite 增量迁移

此目录存放 SQLite 后端的增量迁移 SQL，由 `migrations/sqlite` 包嵌入二进制。

SQLite 的基线 schema 不在此处：它由 Ent 的 `client.Schema.Create()` 在启动时创建
（见 `backend/internal/repository/ent.go` 的 `initSQLiteSchema`）。本目录只承载
基线之后的增量变更。

## 命名

`NNN_description.sql`，数字前缀决定应用顺序，全局唯一且不可复用。

## 约束

- 幂等：可重复执行而不产生错误（用 `IF NOT EXISTS` / `IF EXISTS`）。
- 迁移文件没有绑定参数，不允许使用 `$1`、`$2` 等占位符。
- 不支持 `_notx.sql` 后缀，SQLite 没有 `CREATE/DROP INDEX CONCURRENTLY`。
- 不支持 `CONCURRENTLY` 关键字。
- 不要在此处重放 PostgreSQL 迁移文件，它们包含 SQLite 无法解析的方言特性。

## 校验

已应用的增量迁移记录在独立的 `ent_schema_versions` 表中，不复用 PostgreSQL 的
`schema_migrations` 表及历史校验和兼容规则。文件内容变更会导致校验和不匹配
并拒绝启动。修改已应用迁移等同于破坏不可变性，请新增文件代替。
