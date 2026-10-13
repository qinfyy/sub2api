package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func newSQLiteCompatDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	cfg := config.NewDefaultConfig()
	cfg.Database.Type = config.DatabaseTypeSQLite
	cfg.Database.Path = filepath.Join(t.TempDir(), "compat.db")
	cfg.Redis.Type = config.CacheTypeMemory
	client, db, err := InitEnt(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return context.Background(), db
}

func TestSQLiteCompatFunctions(t *testing.T) {
	ctx, db := newSQLiteCompatDB(t)

	var greatest int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT GREATEST(1, 5, 3)").Scan(&greatest))
	require.Equal(t, 5, greatest)

	var least int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT LEAST(4, 2, 9)").Scan(&least))
	require.Equal(t, 2, least)

	var now string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT NOW()").Scan(&now))
	require.NotEmpty(t, now)

	var btrim string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT BTRIM('  hi  ')").Scan(&btrim))
	require.Equal(t, "hi", btrim)

	var jsonType string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT jsonb_typeof('{"a":1}')`).Scan(&jsonType))
	require.Equal(t, "object", jsonType)
}

func TestSQLiteUsageLogSchemaIncludesPostEntColumns(t *testing.T) {
	ctx, db := newSQLiteCompatDB(t)

	required := []string{
		"reasoning_effort",
		"openai_ws_mode",
		"request_type",
		"service_tier",
		"inbound_endpoint",
		"upstream_endpoint",
		"native_compaction_v2",
		"upstream_request_id",
	}
	for _, column := range required {
		var count int
		require.NoError(t, db.QueryRowContext(
			ctx,
			"SELECT COUNT(*) FROM pragma_table_info('usage_logs') WHERE name = ?",
			column,
		).Scan(&count))
		require.Equalf(t, 1, count, "usage_logs is missing %s", column)
	}

	var indexCount int
	require.NoError(t, db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_usage_logs_request_type_created_at'",
	).Scan(&indexCount))
	require.Equal(t, 1, indexCount)
}
