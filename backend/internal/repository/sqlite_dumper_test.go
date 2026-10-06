package repository

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func newSQLiteDumper(t *testing.T) (*SQLiteDumper, *sql.DB) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "dumper.db")
	dsn := "file:" + filepath.ToSlash(dbPath) + "?_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)"
	db, err := sql.Open(sqliteDriverName, dsn)
	require.NoError(t, err)
	// The SQLite pool is single-connection by design; mirror that here.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	cfg := &config.Config{}
	cfg.Database.Type = config.DatabaseTypeSQLite
	cfg.Database.Path = dbPath
	return NewSQLiteDumper(cfg, db).(*SQLiteDumper), db
}

func TestSQLiteDumperDumpProducesReadableSnapshot(t *testing.T) {
	dumper, db := newSQLiteDumper(t)

	_, err := db.Exec("CREATE TABLE items(id INTEGER PRIMARY KEY, name TEXT)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO items(id, name) VALUES (1, 'alpha'), (2, 'beta')")
	require.NoError(t, err)

	reader, err := dumper.Dump(context.Background())
	require.NoError(t, err)

	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.NotEmpty(t, data)
	// SQLite files start with the "SQLite format 3" magic header.
	require.True(t, bytes.HasPrefix(data, []byte("SQLite format 3\x00")), "snapshot is not a SQLite database file")
}

func TestSQLiteDumperDumpIsIsolatedFromLiveDatabase(t *testing.T) {
	dumper, db := newSQLiteDumper(t)

	_, err := db.Exec("CREATE TABLE items(id INTEGER PRIMARY KEY)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO items(id) VALUES (1)")
	require.NoError(t, err)

	reader, err := dumper.Dump(context.Background())
	require.NoError(t, err)
	snapshot, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	// A write after the dump must not appear in the snapshot.
	_, err = db.Exec("INSERT INTO items(id) VALUES (2)")
	require.NoError(t, err)

	restored, err := newSQLiteFromBytes(t, snapshot)
	require.NoError(t, err)
	t.Cleanup(func() { _ = restored.Close() })

	var count int
	require.NoError(t, restored.QueryRow("SELECT COUNT(*) FROM items").Scan(&count))
	require.Equal(t, 1, count, "snapshot must reflect the state at dump time")
}

func TestSQLiteDumperRestoreRoundTrip(t *testing.T) {
	dumper, db := newSQLiteDumper(t)

	_, err := db.Exec("CREATE TABLE items(id INTEGER PRIMARY KEY, name TEXT)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO items(id, name) VALUES (1, 'keep')")
	require.NoError(t, err)

	reader, err := dumper.Dump(context.Background())
	require.NoError(t, err)
	snapshot, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	// Mutate the live database after the snapshot.
	_, err = db.Exec("INSERT INTO items(id, name) VALUES (2, 'discard')")
	require.NoError(t, err)

	require.NoError(t, dumper.Restore(context.Background(), bytes.NewReader(snapshot)))

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM items").Scan(&count))
	require.Equal(t, 1, count, "restore must roll the live database back to the snapshot")

	var name string
	require.NoError(t, db.QueryRow("SELECT name FROM items WHERE id = 1").Scan(&name))
	require.Equal(t, "keep", name)
}

func TestSQLiteDumperRejectsNilInputs(t *testing.T) {
	t.Run("nil db", func(t *testing.T) {
		dumper := &SQLiteDumper{}
		reader, err := dumper.Dump(context.Background())
		require.Nil(t, reader)
		require.ErrorContains(t, err, "nil sql db")
	})

	t.Run("nil reader", func(t *testing.T) {
		dumper := &SQLiteDumper{db: &sql.DB{}}
		require.ErrorContains(t, dumper.Restore(context.Background(), nil), "nil reader")
	})
}

func TestNewPgDumperSelectsBackendByConfig(t *testing.T) {
	t.Run("postgres keeps the pg_dump dumper", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Database.Type = config.DatabaseTypePostgres

		dumper := NewPgDumper(cfg, nil)
		require.IsType(t, &PgDumper{}, dumper)
	})

	t.Run("sqlite uses the vacuum dumper", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Database.Type = config.DatabaseTypeSQLite

		dumper := NewPgDumper(cfg, nil)
		require.IsType(t, &SQLiteDumper{}, dumper)
	})
}

// newSQLiteFromBytes opens a standalone SQLite database from raw file bytes.
func newSQLiteFromBytes(t *testing.T, data []byte) (*sql.DB, error) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "snapshot.db")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, err
	}
	return sql.Open(sqliteDriverName, "file:"+filepath.ToSlash(path))
}
