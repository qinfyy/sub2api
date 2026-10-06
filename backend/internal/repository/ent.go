// Package repository 提供应用程序的基础设施层组件。
// 包括数据库连接初始化、ORM 客户端管理、Redis 连接、数据库迁移等核心功能。
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/migrations"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/lib/pq"
	_ "modernc.org/sqlite"
)

const (
	maxDatabaseInitializationRetries = 8
	databaseInitializationRetryBase  = time.Second
	databaseInitializationRetryMax   = 30 * time.Second
)

// sqliteDriverName is the database/sql driver name used for the SQLite backend.
const sqliteDriverName = "sqlite"

func openPostgresDriver(cfg *config.Config) (*entsql.Driver, error) {
	dsn := cfg.Database.DSNWithTimezone(cfg.Timezone)
	if cfg.Server.EnableServerTiming {
		connector, err := pq.NewConnector(dsn)
		if err != nil {
			return nil, err
		}
		return entsql.OpenDB(dialect.Postgres, sql.OpenDB(newServerTimingConnector(connector))), nil
	}
	return entsql.Open(dialect.Postgres, dsn)
}

// openSQLiteDriver opens the SQLite backend. SQLite serializes writers, so the
// pool is intentionally reduced to a single connection to avoid SQLITE_BUSY
// storms between long-running reads and short write transactions.
func openSQLiteDriver(cfg *config.Config) (*entsql.Driver, error) {
	if err := cfg.Database.ResolveSQLitePath("."); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o750); err != nil {
		return nil, fmt.Errorf("create sqlite database directory: %w", err)
	}
	db, err := sql.Open(sqliteDriverName, cfg.Database.SQLiteDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)
	return entsql.OpenDB(dialect.SQLite, db), nil
}

// initSQLiteSchema creates the Ent-managed schema plus the table that has no
// Ent entity (durable key/value state used by the memory cache backend).
func initSQLiteSchema(ctx context.Context, client *ent.Client, db *sql.DB) error {
	if err := client.Schema.Create(ctx); err != nil {
		return fmt.Errorf("create sqlite schema: %w", err)
	}
	if err := initSQLiteKVSchema(ctx, db); err != nil {
		return err
	}
	return applySQLiteMigrationsFS(ctx, db)
}

func initSQLiteKVSchema(ctx context.Context, db *sql.DB) error {
	const kvStoreDDL = `
CREATE TABLE IF NOT EXISTS kv_store (
    key        TEXT PRIMARY KEY,
    value      BLOB NOT NULL,
    expires_at INTEGER,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_kv_store_expires_at ON kv_store (expires_at);
CREATE TABLE IF NOT EXISTS memory_batch_queue (
    batch_id TEXT PRIMARY KEY,
    state TEXT NOT NULL CHECK (state IN ('ready', 'delayed', 'active')),
    available_at INTEGER NOT NULL,
    heartbeat_at INTEGER NOT NULL DEFAULT 0,
    receipt TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_memory_batch_queue_ready ON memory_batch_queue(state, available_at);
`
	if _, err := db.ExecContext(ctx, kvStoreDDL); err != nil {
		return fmt.Errorf("create sqlite kv_store: %w", err)
	}
	return nil
}

func applySQLiteSchema(ctx context.Context, db *sql.DB) error {
	// This client borrows the caller's connection pool and must not close it.
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	return initSQLiteSchema(ctx, client, db)
}

// initializeDatabaseWithRetry retries only errors that indicate PostgreSQL is
// temporarily unavailable during startup. Permanent configuration, migration,
// and data errors are returned immediately so they remain visible to operators.
func initializeDatabaseWithRetry(ctx context.Context, initialize func(context.Context) error) error {
	return initializeDatabaseWithRetryWithWait(ctx, initialize, waitForDatabaseInitializationRetry)
}

func initializeDatabaseWithRetryWithWait(
	ctx context.Context,
	initialize func(context.Context) error,
	wait func(context.Context, time.Duration) error,
) error {
	for attempt := 1; ; attempt++ {
		if err := initialize(ctx); err == nil {
			return nil
		} else {
			if !isTransientDatabaseInitializationError(err) || attempt > maxDatabaseInitializationRetries {
				return err
			}

			delay := databaseInitializationRetryBase * time.Duration(1<<(attempt-1))
			if delay > databaseInitializationRetryMax {
				delay = databaseInitializationRetryMax
			}
			slog.Warn("database initialization temporarily unavailable; retrying",
				"retry", attempt,
				"max_retries", maxDatabaseInitializationRetries,
				"retry_in", delay,
				"error", err,
			)
			if err := wait(ctx, delay); err != nil {
				return err
			}
		}
	}
}

func waitForDatabaseInitializationRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isTransientDatabaseInitializationError(err error) bool {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return false
	}
	code := string(pqErr.Code)
	return code == "57P03" || strings.HasPrefix(code, "08")
}

// InitEnt 初始化 Ent ORM 客户端并返回客户端实例和底层的 *sql.DB。
//
// 该函数执行以下操作：
//  1. 初始化全局时区设置，确保时间处理一致性
//  2. 建立 PostgreSQL 数据库连接
//  3. 自动执行数据库迁移，确保 schema 与代码同步
//  4. 创建并返回 Ent 客户端实例
//
// 重要提示：调用者必须负责关闭返回的 ent.Client（关闭时会自动关闭底层的 driver/db）。
//
// 参数：
//   - cfg: 应用程序配置，包含数据库连接信息和时区设置
//
// 返回：
//   - *ent.Client: Ent ORM 客户端，用于执行数据库操作
//   - *sql.DB: 底层的 SQL 数据库连接，可用于直接执行原生 SQL
//   - error: 初始化过程中的错误
func InitEnt(cfg *config.Config) (*ent.Client, *sql.DB, error) {
	if cfg == nil {
		return nil, nil, errors.New("nil database config")
	}
	cfg.Database.NormalizeType()
	cfg.Redis.NormalizeType()
	if !cfg.Database.IsSQLite() && !cfg.Database.IsPostgres() {
		return nil, nil, fmt.Errorf("unsupported database type %q", cfg.Database.Type)
	}
	// 优先初始化时区设置，确保所有时间操作使用统一的时区。
	// 这对于跨时区部署和日志时间戳的一致性至关重要。
	if err := timezone.Init(cfg.Timezone); err != nil {
		return nil, nil, err
	}

	var drv *entsql.Driver
	var err error
	if cfg.Database.IsSQLite() {
		drv, err = openSQLiteDriver(cfg)
	} else {
		drv, err = openPostgresDriver(cfg)
	}
	if err != nil {
		return nil, nil, err
	}
	if cfg.Database.IsPostgres() {
		applyDBPoolSettings(drv.DB(), cfg)
	}

	// 确保数据库 schema 已准备就绪。
	// SQL 迁移文件是 schema 的权威来源（source of truth）。
	// 这种方式比 Ent 的自动迁移更可控，支持复杂的迁移场景。
	migrationCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	migrate := func(ctx context.Context) error {
		return applyMigrationsFS(ctx, drv.DB(), migrations.FS)
	}
	if cfg.Database.IsSQLite() {
		// SQLite schema is owned by Ent; the PostgreSQL SQL migrations are not
		// replayed because they contain dialect-specific DDL.
		migrate = func(ctx context.Context) error {
			client := ent.NewClient(ent.Driver(drv))
			// The client shares the driver opened above, so it must not be
			// closed here: that would close the shared connection pool.
			return initSQLiteSchema(ctx, client, drv.DB())
		}
	}
	if err := initializeDatabaseWithRetry(migrationCtx, migrate); err != nil {
		_ = drv.Close() // 迁移失败时关闭驱动，避免资源泄露
		return nil, nil, err
	}

	// 创建 Ent 客户端，绑定到已配置的数据库驱动。
	client := ent.NewClient(ent.Driver(drv))

	// 启动阶段：从配置或数据库中确保系统密钥可用。
	if err := ensureBootstrapSecrets(migrationCtx, client, cfg); err != nil {
		_ = client.Close()
		return nil, nil, err
	}

	// 在密钥补齐后执行完整配置校验，避免空 jwt.secret 导致服务运行时失败。
	if err := cfg.Validate(); err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("validate config after secret bootstrap: %w", err)
	}

	seedCtx, seedCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer seedCancel()
	if err := ensureSimpleModeStartup(seedCtx, client, cfg); err != nil {
		_ = client.Close()
		return nil, nil, err
	}

	return client, drv.DB(), nil
}

// ensureSimpleModeStartup keeps admin concurrency setup independent of group seeding.
func ensureSimpleModeStartup(ctx context.Context, client *ent.Client, cfg *config.Config) error {
	if cfg.RunMode != config.RunModeSimple {
		return nil
	}
	if cfg.SimpleMode.AutoCreateDefaultGroups {
		if err := ensureSimpleModeDefaultGroups(ctx, client); err != nil {
			return err
		}
	}
	return ensureSimpleModeAdminConcurrency(ctx, client)
}
