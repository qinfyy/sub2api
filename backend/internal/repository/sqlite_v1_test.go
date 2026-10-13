package repository

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent/migrate"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSQLiteV1RunsWholeTriggerScript(t *testing.T) {
	db := newSQLiteTestDB(t)
	files := []sqliteMigrationFile{
		{name: "001_base.sql", content: `CREATE TABLE source(id INTEGER PRIMARY KEY, value TEXT); CREATE TABLE audit(value TEXT);`},
		{name: "002_trigger.sql", content: `CREATE TRIGGER source_changed AFTER INSERT ON source BEGIN
INSERT INTO audit(value) VALUES ('first;second');
INSERT INTO audit(value) VALUES(NEW.value);
END; INSERT INTO source VALUES(1,'original');`},
	}
	ctx := context.Background()
	require.NoError(t, applySQLiteV1Files(ctx, db, files))
	require.NoError(t, applySQLiteV1Files(ctx, db, files))
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM audit`).Scan(&count))
	require.Equal(t, 2, count)
	files[1].content += ` SELECT 1;`
	require.ErrorContains(t, applySQLiteV1Files(ctx, db, files), "history mismatch")
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM audit`).Scan(&count))
	require.Equal(t, 2, count)
}

func TestSQLiteV1FailedBootstrapRollsBackSchemaAndMarker(t *testing.T) {
	db := newSQLiteTestDB(t)
	files := []sqliteMigrationFile{
		{name: "001_base.sql", content: `CREATE TABLE sample(id INTEGER PRIMARY KEY);`},
		{name: "002_invalid.sql", content: `INSERT INTO missing VALUES(1);`},
	}
	require.Error(t, applySQLiteV1Files(context.Background(), db, files))
	var count, appID int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRow(`PRAGMA application_id`).Scan(&appID))
	require.Zero(t, appID)
	require.NoError(t, applySQLiteV1Files(context.Background(), db, files[:1]))
}

func TestSQLiteV1RejectsLegacyBeforeOpeningWritable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open(sqliteDriverName, path)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE preserve_me(value TEXT); INSERT INTO preserve_me VALUES('existing data');`)
	require.NoError(t, err)
	require.ErrorIs(t, applySQLiteV1Schema(context.Background(), db), ErrUnsupportedSQLiteSchema)
	require.NoError(t, db.Close())
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	cfg := &config.Config{Database: config.DatabaseConfig{Type: config.DatabaseTypeSQLite, Path: path}}
	driver, err := openSQLiteDriver(cfg)
	require.ErrorIs(t, err, ErrUnsupportedSQLiteSchema)
	require.Nil(t, driver)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestSQLiteV1RecognitionReadsCommittedWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.db")
	db, err := sql.Open(sqliteDriverName, "file:"+filepath.ToSlash(path)+"?_pragma=journal_mode(WAL)&_pragma=wal_autocheckpoint(0)")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	defer db.Close()
	require.NoError(t, applySQLiteV1Files(context.Background(), db, []sqliteMigrationFile{{name: "001_base.sql", content: `CREATE TABLE sample(id INTEGER PRIMARY KEY);`}}))
	require.NoError(t, validateExistingSQLiteFile(path))
}

func TestSQLiteV1UpgradeFailurePreservesCommittedVersion(t *testing.T) {
	db := newSQLiteTestDB(t)
	ctx := context.Background()
	files := []sqliteMigrationFile{
		{name: "001_base.sql", content: `CREATE TABLE sample(id INTEGER PRIMARY KEY, value TEXT); INSERT INTO sample VALUES(1,'original');`},
		{name: "002_change.sql", content: `ALTER TABLE sample ADD COLUMN extra TEXT; UPDATE sample SET value='changed';`},
		{name: "003_failure.sql", content: `INSERT INTO missing VALUES(1);`},
	}
	require.NoError(t, applySQLiteV1Files(ctx, db, files[:1]))
	require.Error(t, applySQLiteV1Files(ctx, db, files))
	var value string
	require.NoError(t, db.QueryRow(`SELECT value FROM sample WHERE id=1`).Scan(&value))
	require.Equal(t, "original", value)
	var versions, columns, appID int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM ent_schema_versions`).Scan(&versions))
	require.Equal(t, 1, versions)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sample')`).Scan(&columns))
	require.Equal(t, 2, columns)
	require.NoError(t, db.QueryRow(`PRAGMA application_id`).Scan(&appID))
	require.Equal(t, sqliteApplicationID, appID)
	require.NoError(t, applySQLiteV1Files(ctx, db, files[:2]))
	require.NoError(t, db.QueryRow(`SELECT value FROM sample WHERE id=1`).Scan(&value))
	require.Equal(t, "changed", value)
}

func TestSQLiteV1RejectsGapInHistoryBeforeWriting(t *testing.T) {
	db := newSQLiteTestDB(t)
	ctx := context.Background()
	files := []sqliteMigrationFile{
		{name: "001_base.sql", content: `CREATE TABLE sample(id INTEGER PRIMARY KEY);`},
		{name: "002_data.sql", content: `INSERT INTO sample VALUES(1);`},
		{name: "003_data.sql", content: `INSERT INTO sample VALUES(2);`},
		{name: "004_pending.sql", content: `INSERT INTO sample VALUES(3);`},
	}
	require.NoError(t, applySQLiteV1Files(ctx, db, files[:3]))
	_, err := db.Exec(`DELETE FROM ent_schema_versions WHERE filename='002_data.sql'`)
	require.NoError(t, err)
	require.ErrorContains(t, applySQLiteV1Files(ctx, db, files), "not a contiguous prefix")
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sample`).Scan(&count))
	require.Equal(t, 2, count)
}

func TestSQLiteV1CoversEntTablesAndColumns(t *testing.T) {
	db := newSQLiteTestDB(t)
	require.NoError(t, applySQLiteV1Schema(context.Background(), db))
	for _, table := range migrate.Tables {
		t.Run(table.Name, func(t *testing.T) {
			rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table.Name)
			require.NoError(t, err)
			columns := make(map[string]bool)
			for rows.Next() {
				var name string
				require.NoError(t, rows.Scan(&name))
				columns[name] = true
			}
			require.NoError(t, rows.Err())
			require.NoError(t, rows.Close())
			require.NotEmpty(t, columns, "every Ent entity needs a versioned table")
			for _, column := range table.Columns {
				require.True(t, columns[column.Name], "missing versioned column %s.%s", table.Name, column.Name)
			}
		})
	}
}
