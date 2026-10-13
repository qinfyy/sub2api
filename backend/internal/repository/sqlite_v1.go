package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	sqlitev1 "github.com/Wei-Shaw/sub2api/migrations/sqlite_v1"
)

const sqliteApplicationID = 0x53324131 // S2A1

var ErrUnsupportedSQLiteSchema = errors.New("unsupported SQLite schema; experimental databases require an explicit offline migration")

// Inspect the header before opening with WAL/other write-capable pragmas. Even
// a failed startup must not change an unknown database or its journal mode.
func validateExistingSQLiteFile(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	var header [100]byte
	n, err := io.ReadFull(f, header[:])
	if n == 0 && errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil || string(header[:16]) != "SQLite format 3\x00" {
		return ErrUnsupportedSQLiteSchema
	}
	// The committed application_id may still be in WAL after a crash. Let
	// SQLite read the committed view; do not guess from the main-file header.
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path), OmitHost: true}
	u.RawQuery = "mode=ro"
	db, err := sql.Open(sqliteDriverName, u.String())
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var appID, objects int
	if err := db.QueryRowContext(ctx, `PRAGMA application_id`).Scan(&appID); err != nil {
		return fmt.Errorf("read SQLite format: %w", err)
	}
	if appID == sqliteApplicationID {
		return nil
	}
	if appID == 0 {
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'`).Scan(&objects); err != nil {
			return err
		}
		if objects == 0 {
			return nil
		}
	}
	return ErrUnsupportedSQLiteSchema
}

func applySQLiteV1Schema(ctx context.Context, db *sql.DB) error {
	files, err := loadSQLiteMigrationFiles(sqlitev1.FS)
	if err != nil {
		return err
	}
	return applySQLiteV1Files(ctx, db, files)
}

func sqliteMigrationChecksum(content string) string {
	sum := sha256.Sum256([]byte(strings.ReplaceAll(strings.TrimSpace(content), "\r\n", "\n")))
	return hex.EncodeToString(sum[:])
}

func applySQLiteV1Files(ctx context.Context, db *sql.DB, files []sqliteMigrationFile) error {
	if db == nil {
		return errors.New("nil SQLite database")
	}
	lock := newKeyedMutex(sqliteMigrationsLockKey)
	lock.Lock()
	defer func() { lock.Unlock(); lock.release() }()
	var appID, tableCount int
	if err := db.QueryRowContext(ctx, `PRAGMA application_id`).Scan(&appID); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'`).Scan(&tableCount); err != nil {
		return err
	}
	fresh := appID == 0 && tableCount == 0
	if !fresh && appID != sqliteApplicationID {
		return ErrUnsupportedSQLiteSchema
	}
	known := make(map[string]string, len(files))
	if len(files) == 0 {
		return errors.New("empty SQLite migration set")
	}
	for _, file := range files {
		if err := validateSQLiteMigrationExecutionMode(file.name, file.content); err != nil {
			return fmt.Errorf("validate SQLite migration %s: %w", file.name, err)
		}
		if _, exists := known[file.name]; exists {
			return fmt.Errorf("duplicate SQLite migration %s", file.name)
		}
		known[file.name] = sqliteMigrationChecksum(file.content)
	}
	applied := make(map[string]bool)
	if !fresh {
		rows, err := db.QueryContext(ctx, `SELECT filename, checksum FROM ent_schema_versions`)
		if err != nil {
			return fmt.Errorf("read SQLite migration history: %w", err)
		}
		for rows.Next() {
			var name, checksum string
			if err := rows.Scan(&name, &checksum); err != nil {
				_ = rows.Close()
				return err
			}
			if known[name] == "" || known[name] != checksum {
				_ = rows.Close()
				return fmt.Errorf("SQLite migration history mismatch: %s", name)
			}
			applied[name] = true
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(applied) == 0 {
			return fmt.Errorf("%w: empty migration history", ErrUnsupportedSQLiteSchema)
		}
		pending := false
		for _, file := range files {
			if !applied[file.name] {
				pending = true
			} else if pending {
				return fmt.Errorf("SQLite migration history is not a contiguous prefix: %s", file.name)
			}
		}
	}
	if len(applied) == len(files) {
		return nil
	}
	// All pending files, the ledger and the application marker commit together.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if fresh {
		if _, err := tx.ExecContext(ctx, sqliteSchemaMigrationsTableDDL); err != nil {
			return err
		}
	}
	for _, file := range files {
		if applied[file.name] {
			continue
		}
		// modernc executes complete scripts, including trigger bodies. No SQL
		// token rewriting or duplicate-column error suppression is allowed here.
		if _, err := tx.ExecContext(ctx, file.content); err != nil {
			return fmt.Errorf("apply SQLite migration %s: %w", file.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ent_schema_versions(filename,checksum) VALUES(?,?)`, file.name, known[file.name]); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	violated := rows.Next()
	rowErr := rows.Err()
	closeErr := rows.Close()
	if violated {
		return errors.New("SQLite migration failed foreign_key_check")
	}
	if err := errors.Join(rowErr, closeErr); err != nil {
		return err
	}
	if fresh {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id = %d", sqliteApplicationID)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
