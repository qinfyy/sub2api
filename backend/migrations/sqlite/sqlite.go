// Package sqlite 包含 SQLite 后端专用的增量迁移文件。
//
// 与 PostgreSQL 的关系：
//   - PostgreSQL 的 291 个迁移文件以 SQL 迁移为 schema 的权威来源，
//     包含 JSONB、数组、PL/pgSQL、GIN 索引等方言特性，不适用于 SQLite。
//   - SQLite 的基线 schema 由 Ent 的 client.Schema.Create() 维护（见
//     repository/initSQLiteSchema），本目录只承载基线之后的增量变更。
//
// 命名规范沿用 PostgreSQL 侧：NNN_description.sql，按数字前缀顺序应用。
// 迁移必须是幂等的（可重复执行而不产生错误），推荐使用
// IF NOT EXISTS / IF EXISTS。
//
// 方言约束：
//   - 迁移文件执行时没有绑定参数，不能使用 $N 占位符
//   - 不支持 *_notx.sql 命名（SQLite 没有 CREATE/DROP INDEX CONCURRENTLY）
//   - 不支持 CONCURRENTLY 关键字
package sqlite

import "embed"

//go:embed *.sql
var FS embed.FS
