package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	sqlitemigrations "github.com/Wei-Shaw/sub2api/migrations/sqlite"
)

// sqliteMigrationsLockKey 是 SQLite 迁移互斥的进程内 key。
//
// PostgreSQL 侧用 advisory lock 做跨进程迁移互斥；SQLite 没有等价机制。
// 由于 SQLite 后端本身只支持单写进程，这里用 keyed mutex 在进程内互斥即可，
// 而 ValidateSingleInstanceGuard 会拒绝同一库文件上的第二个实例。
const sqliteMigrationsLockKey = "sqlite:migrations"

// sqliteSchemaMigrationsTableDDL tracks SQLite schema additions separately from PostgreSQL.
const sqliteSchemaMigrationsTableDDL = `
CREATE TABLE IF NOT EXISTS ent_schema_versions (
	filename   TEXT PRIMARY KEY,
	checksum   TEXT NOT NULL,
	applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
`

// sqliteIncrementalMigrationsFS 是嵌入的 SQLite 增量迁移文件集。
// 基线 schema 由 Ent 维护，这里只有基线之后的增量变更。
var sqliteIncrementalMigrationsFS fs.FS = sqlitemigrations.FS

// applySQLiteMigrationsFS 在 SQLite 方言下应用嵌入的增量迁移。
//
// 与 PostgreSQL 的差异：
//   - 不使用 advisory lock，改用进程内 keyed mutex
//   - 不做 atlas 基线对齐（Ent 负责 schema 演进）
//   - 使用独立的 ent_schema_versions 记录 SQLite 增量版本
//   - SQLite 的 DDL 本身即事务性（除 VACUUM/CREATE INDEX CONCURRENTLY 之类），
//     因此 *_notx.sql 的 CONCURRENTLY 语义在这里不适用，会被显式拒绝
func applySQLiteMigrationsFS(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("nil sql db")
	}

	lock := newKeyedMutex(sqliteMigrationsLockKey)
	lock.Lock()
	defer func() {
		lock.Unlock()
		lock.release()
	}()

	files, err := loadSQLiteMigrationFiles(sqliteIncrementalMigrationsFS)
	if err != nil {
		return err
	}
	return applySQLiteMigrationFiles(ctx, db, files)
}

// sqliteMigrationFile 是一个待应用的 SQLite 增量迁移。
type sqliteMigrationFile struct {
	name    string
	content string
}

// loadSQLiteMigrationFiles 从文件集读取并按文件名排序所有 .sql 迁移。
func loadSQLiteMigrationFiles(fsys fs.FS) ([]sqliteMigrationFile, error) {
	if fsys == nil {
		return nil, nil
	}
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("list sqlite migrations: %w", err)
	}
	sort.Strings(names)

	out := make([]sqliteMigrationFile, 0, len(names))
	for _, name := range names {
		contentBytes, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read sqlite migration %s: %w", name, err)
		}
		out = append(out, sqliteMigrationFile{
			name:    name,
			content: strings.TrimSpace(string(contentBytes)),
		})
	}
	return out, nil
}

// applySQLiteMigrationFiles 应用给定的 SQLite 增量迁移文件列表。
// 与文件读取分离，便于单元测试注入自定义迁移集。
func applySQLiteMigrationFiles(ctx context.Context, db *sql.DB, files []sqliteMigrationFile) error {
	if _, err := db.ExecContext(ctx, sqliteSchemaMigrationsTableDDL); err != nil {
		return fmt.Errorf("create ent_schema_versions: %w", err)
	}

	for _, file := range files {
		content := file.content
		if content == "" {
			continue
		}

		sum := sha256.Sum256([]byte(strings.ReplaceAll(content, "\r\n", "\n")))
		checksum := hex.EncodeToString(sum[:])

		var existing string
		rowErr := db.QueryRowContext(ctx,
			"SELECT checksum FROM ent_schema_versions WHERE filename = ?",
			file.name,
		).Scan(&existing)
		switch {
		case rowErr == nil:
			if existing != checksum {
				if !matchesSQLiteMigrationLineEndings(content, existing) {
					return fmt.Errorf(
						"migration %s checksum mismatch (db=%s file=%s)\n"+
							"SQLite 迁移与已应用记录不一致，请新增迁移文件而不是修改历史迁移",
						file.name, existing, checksum,
					)
				}
			}
			continue
		case errors.Is(rowErr, sql.ErrNoRows):
			// 未应用，继续执行。
		default:
			return fmt.Errorf("check migration %s: %w", file.name, rowErr)
		}

		if err := validateSQLiteMigrationExecutionMode(file.name, content); err != nil {
			return fmt.Errorf("validate migration %s: %w", file.name, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", file.name, err)
		}

		if err := execSQLiteMigrationContent(ctx, tx, content); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", file.name, err)
		}

		if _, err := tx.ExecContext(ctx,
			"INSERT INTO ent_schema_versions (filename, checksum) VALUES (?, ?)",
			file.name, checksum,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", file.name, err)
		}

		if err := tx.Commit(); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("commit migration %s: %w", file.name, err)
		}
	}

	return nil
}

// Earlier builds hashed checkout line endings. Accept the exact same SQL in
// LF/CRLF form without rewriting the ledger or accepting actual schema changes.
func matchesSQLiteMigrationLineEndings(content, recorded string) bool {
	lf := strings.ReplaceAll(content, "\r\n", "\n")
	for _, variant := range []string{lf, strings.ReplaceAll(lf, "\n", "\r\n")} {
		sum := sha256.Sum256([]byte(variant))
		if hex.EncodeToString(sum[:]) == recorded {
			return true
		}
	}
	return false
}

// execSQLiteMigrationContent executes SQLite migration DDL. SQLite does not
// support ADD COLUMN IF NOT EXISTS, so the runner skips an ADD COLUMN when the
// target column already exists. The rest of the file executes as one script.
func execSQLiteMigrationContent(ctx context.Context, tx *sql.Tx, content string) error {
	statements := splitSQLStatements(content)
	for _, statement := range statements {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if table, column, ok := parseSQLiteAddColumn(statement); ok {
			exists, err := sqliteColumnExists(ctx, tx, table, column)
			if err != nil {
				return err
			}
			if exists {
				continue
			}
		}
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func parseSQLiteAddColumn(statement string) (table, column string, ok bool) {
	normalized := strings.Join(strings.Fields(statement), " ")
	upper := strings.ToUpper(normalized)
	const prefix = "ALTER TABLE "
	if !strings.HasPrefix(upper, prefix) {
		return "", "", false
	}
	rest := strings.TrimSpace(normalized[len(prefix):])
	parts := strings.SplitN(rest, " ", 4)
	if len(parts) < 4 || !strings.EqualFold(parts[1], "ADD") || !strings.EqualFold(parts[2], "COLUMN") {
		return "", "", false
	}
	table = strings.Trim(parts[0], `"`)
	column = strings.Trim(parts[3], `"`)
	if idx := strings.IndexAny(column, " \t\r\n("); idx >= 0 {
		column = column[:idx]
	}
	return table, column, table != "" && column != ""
}

func sqliteColumnExists(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.QueryContext(ctx, "SELECT 1 FROM pragma_table_info(?) WHERE name = ? LIMIT 1", table, column)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	return rows.Next(), rows.Err()
}

// validateSQLiteMigrationExecutionMode 校验 SQLite 增量迁移的执行约束。
//
// SQLite 没有 CREATE/DROP INDEX CONCURRENTLY，因此 *_notx.sql 命名约定在
// SQLite 下无意义；这里显式报错，避免让人以为它被静默忽略了。
func validateSQLiteMigrationExecutionMode(name, content string) error {
	normalizedName := strings.ToLower(strings.TrimSpace(name))
	if strings.HasSuffix(normalizedName, nonTransactionalMigrationSuffix) {
		return errors.New("SQLite 迁移不支持 *_notx.sql（无 CREATE/DROP INDEX CONCURRENTLY 语义）")
	}
	if strings.Contains(strings.ToUpper(content), "CONCURRENTLY") {
		return errors.New("CONCURRENTLY statements are not supported by SQLite")
	}
	// 迁移是原始 SQL，没有绑定参数。$N 只会在执行时被转换成 ? 然后因为缺少
	// 参数而失败，这里提前给出可读的错误。
	if placeholders := scanSQLLiterals(content); len(placeholders) > 0 {
		return fmt.Errorf(
			"migration files cannot use parameter placeholders, found $%d",
			placeholders[0].number,
		)
	}
	return nil
}
