package repository

import (
	"context"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestInitEntSQLiteRunsIncrementalMigrationsAndReopens(t *testing.T) {
	previous := sqliteIncrementalMigrationsFS
	sqliteIncrementalMigrationsFS = fstest.MapFS{
		"001_bootstrap_test.sql": {Data: []byte("CREATE TABLE bootstrap_probe (value TEXT NOT NULL)")},
	}
	t.Cleanup(func() { sqliteIncrementalMigrationsFS = previous })

	cfg := config.NewDefaultConfig()
	cfg.Database.Type = config.DatabaseTypeSQLite
	cfg.Database.Path = filepath.Join(t.TempDir(), "nested", "state.db")
	cfg.Redis.Type = config.CacheTypeMemory
	cfg.Timezone = "UTC"

	for attempt := 0; attempt < 2; attempt++ {
		client, db, err := InitEnt(cfg)
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })
		var journal string
		require.NoError(t, db.QueryRow("PRAGMA journal_mode").Scan(&journal))
		require.Equal(t, "wal", journal)
		var foreignKeys int
		require.NoError(t, db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys))
		require.Equal(t, 1, foreignKeys)
		require.Equal(t, 1, db.Stats().MaxOpenConnections)
		_, err = db.ExecContext(context.Background(), "INSERT INTO bootstrap_probe VALUES (?)", "ok")
		require.NoError(t, err)
		var count int
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM ent_schema_versions").Scan(&count))
		require.Equal(t, 1, count)
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name = 'schema_migrations'").Scan(&count))
		require.Zero(t, count)
		require.NoError(t, client.Close())
	}
}

func TestSQLiteBaselinePreservesActiveUniqueConstraints(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Database.Type = config.DatabaseTypeSQLite
	cfg.Database.Path = filepath.Join(t.TempDir(), "unique.db")
	cfg.Redis.Type = config.CacheTypeMemory
	client, db, err := InitEnt(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	create := func() error {
		return client.User.Create().SetEmail("same@example.com").SetPasswordHash("hash").Exec(ctx)
	}
	require.NoError(t, create())
	require.Error(t, create())
	_, err = db.Exec("UPDATE users SET deleted_at = ?", time.Now())
	require.NoError(t, err)
	require.NoError(t, create(), "soft-deleted emails can be reused")
	for _, name := range []string{"users_email_unique_active", "groups_name_unique_active", "user_subscriptions_user_group_unique_active"} {
		var ddl string
		require.NoError(t, db.QueryRow("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", name).Scan(&ddl))
		require.Contains(t, ddl, "UNIQUE INDEX")
		require.Contains(t, ddl, "deleted_at IS NULL")
	}
	require.NoError(t, client.Close())
	reopened, _, err := InitEnt(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	require.Error(t, reopened.User.Create().SetEmail("same@example.com").SetPasswordHash("hash").Exec(ctx),
		"Ent schema updates must retain the supplemental indexes")
}
