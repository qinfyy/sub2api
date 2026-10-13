package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

// newSQLiteTestDB opens a file-backed SQLite database for migration tests.
// File-backed (rather than :memory:) because VACUUM INTO and the online backup
// API need a real file path.
func newSQLiteTestDB(t *testing.T) *sql.DB {
	t.Helper()
	require.NoError(t, registerSQLiteDomainFunctions())

	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "migrate.db")) +
		"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open(sqliteDriverName, dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestLoadSQLiteMigrationFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"002_second.sql": &fstest.MapFile{Data: []byte("SELECT 2;")},
		"001_first.sql":  &fstest.MapFile{Data: []byte("\n  SELECT 1;  \n")},
		"README.md":      &fstest.MapFile{Data: []byte("ignored")},
		"nested/003.md":  &fstest.MapFile{Data: []byte("ignored")},
	}

	files, err := loadSQLiteMigrationFiles(fsys)
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Equal(t, "001_first.sql", files[0].name)
	require.Equal(t, "SELECT 1;", files[0].content, "content is trimmed")
	require.Equal(t, "002_second.sql", files[1].name)
}

func TestLoadSQLiteMigrationFilesNilFS(t *testing.T) {
	files, err := loadSQLiteMigrationFiles(nil)
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestApplySQLiteMigrationFilesAppliesAndIsIdempotent(t *testing.T) {
	db := newSQLiteTestDB(t)
	files := []sqliteMigrationFile{
		{name: "001_create.sql", content: "CREATE TABLE demo(id INTEGER PRIMARY KEY, name TEXT)"},
		{name: "002_insert.sql", content: "INSERT INTO demo(id, name) VALUES (1, 'a')"},
	}

	require.NoError(t, applySQLiteMigrationFiles(context.Background(), db, files))

	var name string
	require.NoError(t, db.QueryRow("SELECT name FROM demo WHERE id = 1").Scan(&name))
	require.Equal(t, "a", name)

	// Re-applying must be a no-op: records are tracked by filename.
	require.NoError(t, applySQLiteMigrationFiles(context.Background(), db, files))

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM demo").Scan(&count))
	require.Equal(t, 1, count, "rows must not be duplicated on re-apply")
}

func TestApplySQLiteMigrationFilesConvertsPlaceholders(t *testing.T) {
	db := newSQLiteTestDB(t)
	_, err := db.Exec("CREATE TABLE seed(id INTEGER PRIMARY KEY)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO seed(id) VALUES (1), (2)")
	require.NoError(t, err)

	// Migration SQL is raw SQL with no bound arguments, so placeholders are
	// rejected rather than silently converted.
	files := []sqliteMigrationFile{{
		name:    "001_param.sql",
		content: "INSERT INTO seed(id) VALUES ($1)",
	}}

	err = applySQLiteMigrationFiles(context.Background(), db, files)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot use parameter placeholders")

	// A '$1' inside a string literal is not a placeholder and stays allowed.
	require.NoError(t, applySQLiteMigrationFiles(context.Background(), db, []sqliteMigrationFile{{
		name:    "002_literal.sql",
		content: "INSERT INTO seed(id) VALUES (3) /* $1 in a comment */",
	}}))
}

func TestApplySQLiteMigrationFilesRejectsChecksumMismatch(t *testing.T) {
	db := newSQLiteTestDB(t)

	first := []sqliteMigrationFile{{name: "001_x.sql", content: "CREATE TABLE t(id INTEGER)"}}
	require.NoError(t, applySQLiteMigrationFiles(context.Background(), db, first))

	changed := []sqliteMigrationFile{{name: "001_x.sql", content: "CREATE TABLE t(id INTEGER, extra TEXT)"}}
	err := applySQLiteMigrationFiles(context.Background(), db, changed)
	require.Error(t, err)
	require.Contains(t, err.Error(), "checksum mismatch")
}

func TestApplySQLiteMigrationFilesRollsBackOnFailure(t *testing.T) {
	db := newSQLiteTestDB(t)

	files := []sqliteMigrationFile{{
		name: "001_bad.sql",
		// Second statement fails, so the first must be rolled back too.
		content: "CREATE TABLE keep_me(id INTEGER); SELECT * FROM does_not_exist",
	}}

	err := applySQLiteMigrationFiles(context.Background(), db, files)
	require.Error(t, err)

	var count int
	err = db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='keep_me'",
	).Scan(&count)
	require.NoError(t, err)
	require.Zero(t, count, "failed migration must not leave partial schema behind")
}

func TestApplySQLiteMigrationsFSRunsEmbeddedMigrations(t *testing.T) {
	db := newSQLiteTestDB(t)

	require.NoError(t, applySQLiteSchema(context.Background(), db))

	// The embedded set creates kv_store_namespace for prefix scans.
	var count int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='kv_store_namespace'",
	).Scan(&count))
	require.Equal(t, 1, count)

	// Running again must be a no-op.
	require.NoError(t, applySQLiteV1Schema(context.Background(), db))
}

func TestApplyMigrationsFSDispatchesSQLiteWithoutPostgresAdvisoryLock(t *testing.T) {
	db := newSQLiteTestDB(t)

	// applyMigrationsFS is the generic entry point; on SQLite it must not try
	// pg_try_advisory_lock (which would fail with a syntax error).
	require.NoError(t, applyMigrationsFS(context.Background(), db, migrations.FS))
}

func TestApplyMigrationsNilDB(t *testing.T) {
	require.Error(t, applyMigrationsFS(context.Background(), nil, nil))
	require.Error(t, applySQLiteMigrationsFS(context.Background(), nil))
}

func TestValidateSQLiteMigrationExecutionMode(t *testing.T) {
	t.Run("plain migration is allowed", func(t *testing.T) {
		require.NoError(t, validateSQLiteMigrationExecutionMode("001_add.sql", "ALTER TABLE t ADD COLUMN c TEXT"))
	})

	t.Run("notx suffix is rejected", func(t *testing.T) {
		err := validateSQLiteMigrationExecutionMode("001_add_notx.sql", "CREATE INDEX i ON t(a)")
		require.Error(t, err)
		require.Contains(t, err.Error(), "_notx.sql")
	})

	t.Run("concurrently is rejected", func(t *testing.T) {
		err := validateSQLiteMigrationExecutionMode("001_add.sql", "CREATE INDEX CONCURRENTLY i ON t(a)")
		require.Error(t, err)
		require.Contains(t, err.Error(), "CONCURRENTLY")
	})
}

func TestSQLiteMigrationLineEndingsPreserveLedger(t *testing.T) {
	db := newSQLiteTestDB(t)
	ctx := context.Background()
	const name = "007_non_ent_extended_tables.sql"
	const content = "CREATE TABLE preserved(id INTEGER PRIMARY KEY);\nINSERT INTO preserved VALUES (1);"
	require.NoError(t, applySQLiteMigrationFiles(ctx, db, []sqliteMigrationFile{{name, content}}))
	crlf := "CREATE TABLE preserved(id INTEGER PRIMARY KEY);\r\nINSERT INTO preserved VALUES (1);"
	legacy := fmt.Sprintf("%x", sha256.Sum256([]byte(crlf)))
	_, err := db.Exec("UPDATE ent_schema_versions SET checksum = ? WHERE filename = ?", legacy, name)
	require.NoError(t, err)
	require.NoError(t, applySQLiteMigrationFiles(ctx, db, []sqliteMigrationFile{{name, content}}))
	var recorded string
	require.NoError(t, db.QueryRow("SELECT checksum FROM ent_schema_versions WHERE filename = ?", name).Scan(&recorded))
	require.Equal(t, legacy, recorded)
	require.ErrorContains(t, applySQLiteMigrationFiles(ctx, db, []sqliteMigrationFile{{name, content + "\nSELECT 2;"}}), "checksum mismatch")
}
