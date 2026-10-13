package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// The input must be an offline, consistent backup, never a live database file.
// Even that input is copied again before migrations or other writes occur.
func TestSQLiteRuntimeExistingSnapshotUpgrade(t *testing.T) {
	snapshot := os.Getenv("SUB2API_RUNTIME_DB_COPY")
	if snapshot == "" {
		t.Skip("set SUB2API_RUNTIME_DB_COPY to an offline SQLite backup")
	}
	data, err := os.ReadFile(snapshot)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "existing.db")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	cfg := config.NewDefaultConfig()
	cfg.Database.Type, cfg.Database.Path = config.DatabaseTypeSQLite, path
	cfg.Redis.Type = config.CacheTypeMemory
	client, db, err := InitEnt(cfg)
	require.NoError(t, err)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var integrity string
	require.NoError(t, db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity))
	require.Equal(t, "ok", integrity)
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	require.NoError(t, err)
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	var versions int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ent_schema_versions`).Scan(&versions))
	require.Equal(t, 5, versions)
	accounts, err := client.Account.Query().All(ctx)
	require.NoError(t, err)
	users, err := client.User.Query().Count(ctx)
	require.NoError(t, err)
	audit := NewAuditLogRepository(db)
	list, err := audit.List(ctx, &service.AuditLogFilter{})
	require.NoError(t, err)
	for _, entry := range list.Logs {
		_, err := audit.GetByID(ctx, entry.ID)
		require.NoError(t, err)
	}
	after, err := os.ReadFile(snapshot)
	require.NoError(t, err)
	require.Equal(t, data, after, "input snapshot must be unchanged")
	t.Logf("COPY ACCEPTANCE: versions=%d users=%d accounts=%d audit=%d integrity=%s", versions, users, len(accounts), list.Total, integrity)
}

func TestSQLiteRuntimeUsageMigrationDuplicateFailsWithoutDeleting(t *testing.T) {
	r, _, _, _, ctx := newSQLiteUsageLogStatsRepo(t)
	_, err := r.db.ExecContext(ctx, `DROP INDEX idx_usage_logs_request_id_api_key_unique;
DELETE FROM ent_schema_versions WHERE filename='005_usage_log_request_dedup.sql';
INSERT INTO usage_logs(user_id,api_key_id,account_id,request_id,model,created_at)
SELECT user_id,api_key_id,account_id,request_id,model,created_at FROM usage_logs LIMIT 1;`)
	require.NoError(t, err)
	require.ErrorContains(t, applySQLiteV1Schema(ctx, r.db), "005_usage_log_request_dedup.sql")
	var count int
	require.NoError(t, r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_logs`).Scan(&count))
	require.Equal(t, 4, count)
	require.NoError(t, r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ent_schema_versions`).Scan(&count))
	require.Equal(t, 4, count)
}
