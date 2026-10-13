package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func newSQLiteAuthInvalidationOutbox(t *testing.T) (*authCacheInvalidationOutboxRepository, context.Context) {
	t.Helper()
	cfg := config.NewDefaultConfig()
	cfg.Database.Type = config.DatabaseTypeSQLite
	cfg.Database.Path = filepath.Join(t.TempDir(), "outbox.db")
	cfg.Redis.Type = config.CacheTypeMemory
	client, db, err := InitEnt(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return &authCacheInvalidationOutboxRepository{db: db, sqlite: true}, context.Background()
}

func insertAuthInvalidationOutboxRow(t *testing.T, repo *authCacheInvalidationOutboxRepository, ctx context.Context, key string) int64 {
	t.Helper()
	sum := sha256.Sum256([]byte(key))
	key = hex.EncodeToString(sum[:])
	res, err := repo.db.ExecContext(ctx, `
		INSERT INTO auth_cache_invalidation_outbox (cache_key, available_at, attempts, delivery_stage)
		VALUES (?, ?, 0, 0)
	`, key, nowSQLiteUTC())
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

func TestSQLiteAuthInvalidationClaimAndLease(t *testing.T) {
	repo, ctx := newSQLiteAuthInvalidationOutbox(t)
	id := insertAuthInvalidationOutboxRow(t, repo, ctx, "key-a")

	events, err := repo.Claim(ctx, "worker-1", 10, 30*time.Second)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, id, events[0].ID)

	// 同一 worker 在租约内不能重复领取。
	events, err = repo.Claim(ctx, "worker-2", 10, 30*time.Second)
	require.NoError(t, err)
	require.Empty(t, events)

	// 释放租约后可以被再次领取。
	require.NoError(t, repo.RetryClaimed(ctx, id, "worker-1", time.Now().UTC(), "boom"))
	events, err = repo.Claim(ctx, "worker-2", 10, 30*time.Second)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, 1, events[0].Attempts)

	// 旧持有者不能删除或二次投递新的领取。
	require.Error(t, repo.DeleteClaimed(ctx, id, "worker-1"))
	require.Error(t, repo.ScheduleSecondPass(ctx, id, "worker-1", time.Now().UTC()))
	require.NoError(t, repo.DeleteClaimed(ctx, id, "worker-2"))

	var pending int64
	require.NoError(t, repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_cache_invalidation_outbox`).Scan(&pending))
	require.Equal(t, int64(0), pending)
}

func TestSQLiteAuthInvalidationStats(t *testing.T) {
	repo, ctx := newSQLiteAuthInvalidationOutbox(t)
	id := insertAuthInvalidationOutboxRow(t, repo, ctx, "key-b")
	_, err := repo.Claim(ctx, "worker-1", 10, 30*time.Second)
	require.NoError(t, err)
	require.NoError(t, repo.RetryClaimed(ctx, id, "worker-1", time.Now().UTC(), "db down"))

	stats, err := repo.Stats(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.Pending)
	require.Equal(t, "db down", stats.LastError)
	require.NotNil(t, stats.OldestCreatedAt)
}
