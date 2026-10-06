package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	moderncsqlite "modernc.org/sqlite"
)

// SQLiteDumper implements service.DBDumper for the SQLite backend.
//
// Dump 使用 VACUUM INTO 生成一致性快照：它在一个事务内读取整个库并写出
// 独立文件，不需要停写，也不需要复制 -wal / -shm 文件。
//
// Restore 把归档写回临时文件后，用 SQLite 的 online backup API 覆盖在线库。
// 由于连接池被限制为单写连接，回填期间业务写入会等待，这是恢复操作的
// 预期语义。
type SQLiteDumper struct {
	cfg *config.DatabaseConfig
	db  *sql.DB
}

// NewSQLiteDumper creates a SQLite-backed DBDumper.
func NewSQLiteDumper(cfg *config.Config, db *sql.DB) service.DBDumper {
	var databaseCfg *config.DatabaseConfig
	if cfg != nil {
		databaseCfg = &cfg.Database
	}
	return &SQLiteDumper{cfg: databaseCfg, db: db}
}

// Dump generates a consistent snapshot and returns a streaming reader for it.
func (d *SQLiteDumper) Dump(ctx context.Context) (io.ReadCloser, error) {
	if d.db == nil {
		return nil, errors.New("create sqlite backup snapshot: nil sql db")
	}

	// 快照写入临时文件而不是内存：SQLite 库可能远大于可用堆内存。
	snapshot, err := os.CreateTemp("", "sub2api-sqlite-snapshot-*.db")
	if err != nil {
		return nil, fmt.Errorf("create sqlite backup snapshot file: %w", err)
	}
	snapshotPath := snapshot.Name()
	// VACUUM INTO 要求目标文件不存在。
	if err := snapshot.Close(); err != nil {
		_ = os.Remove(snapshotPath)
		return nil, fmt.Errorf("close sqlite backup snapshot placeholder: %w", err)
	}
	if err := os.Remove(snapshotPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove sqlite backup snapshot placeholder: %w", err)
	}

	// VACUUM INTO 不支持绑定参数，路径必须内联到 SQL 中。
	// snapshotPath 来自 os.CreateTemp，不含引号，但仍做一次转义防御。
	escapedPath := escapeSQLiteStringLiteral(snapshotPath)
	if _, err := d.db.ExecContext(ctx, "VACUUM INTO "+escapedPath); err != nil {
		_ = os.Remove(snapshotPath)
		return nil, fmt.Errorf("vacuum into sqlite backup snapshot: %w", err)
	}

	file, err := os.Open(snapshotPath)
	if err != nil {
		_ = os.Remove(snapshotPath)
		return nil, fmt.Errorf("open sqlite backup snapshot: %w", err)
	}
	return &sqliteSnapshotReadCloser{File: file, path: snapshotPath}, nil
}

// Restore overwrites the live database with the archived snapshot.
func (d *SQLiteDumper) Restore(ctx context.Context, data io.Reader) error {
	if d.db == nil {
		return errors.New("restore sqlite backup: nil sql db")
	}
	if data == nil {
		return errors.New("restore sqlite backup: nil reader")
	}

	// online backup API 需要一个磁盘上的源库，因此先把归档流落到临时文件。
	source, err := os.CreateTemp("", "sub2api-sqlite-restore-*.db")
	if err != nil {
		return fmt.Errorf("create sqlite restore temp file: %w", err)
	}
	sourcePath := source.Name()
	defer func() { _ = os.Remove(sourcePath) }()

	if _, err := io.Copy(source, data); err != nil {
		_ = source.Close()
		return fmt.Errorf("write sqlite restore temp file: %w", err)
	}
	if err := source.Close(); err != nil {
		return fmt.Errorf("close sqlite restore temp file: %w", err)
	}

	// 单写连接池下，Conn 会等待当前业务查询结束，从而保证回填期间没有并发写入。
	conn, err := d.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire sqlite restore connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	rawErr := conn.Raw(func(driverConn any) error {
		backuper, ok := driverConn.(sqliteBackupDriver)
		if !ok {
			return fmt.Errorf("sqlite driver %T does not support the online backup API", driverConn)
		}

		backup, err := backuper.NewRestore(sourcePath)
		if err != nil {
			return fmt.Errorf("open sqlite restore source: %w", err)
		}
		// Step(-1) 一次复制全部剩余页；返回 true 表示仍有页未复制。
		for more := true; more; {
			more, err = backup.Step(-1)
			if err != nil {
				_ = backup.Finish()
				return fmt.Errorf("sqlite restore step: %w", err)
			}
		}
		if err := backup.Finish(); err != nil {
			return fmt.Errorf("sqlite restore finish: %w", err)
		}
		return nil
	})
	if rawErr != nil {
		return fmt.Errorf("restore sqlite backup: %w", rawErr)
	}
	return nil
}

// sqliteBackupDriver is the optional online backup API exposed by
// modernc.org/sqlite connections.
type sqliteBackupDriver interface {
	NewBackup(dstURI string) (*moderncsqlite.Backup, error)
	NewRestore(srcURI string) (*moderncsqlite.Backup, error)
}

// sqliteSnapshotReadCloser 删除 Close 时使用的快照临时文件。
type sqliteSnapshotReadCloser struct {
	*os.File
	path      string
	closeOnce sync.Once
	closeErr  error
}

// Close closes the snapshot file and removes the temporary copy.
func (c *sqliteSnapshotReadCloser) Close() error {
	c.closeOnce.Do(func() {
		var errs []error
		if err := c.File.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close sqlite backup snapshot: %w", err))
		}
		if err := os.Remove(c.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove sqlite backup snapshot %s: %w", c.path, err))
		}
		c.closeErr = errors.Join(errs...)
	})
	return c.closeErr
}

// escapeSQLiteStringLiteral escapes a value for safe inlining into a SQL
// string literal. Callers must not pass user-controlled input.
func escapeSQLiteStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// sqliteDatabaseFile resolves the on-disk path of the SQLite database.
func sqliteDatabaseFile(cfg *config.DatabaseConfig) string {
	if cfg == nil {
		return ""
	}
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		path = "data/sub2api.db"
	}
	return filepath.Clean(strings.TrimPrefix(path, "file:"))
}
