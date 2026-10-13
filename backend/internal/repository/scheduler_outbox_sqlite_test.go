package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSQLiteSchedulerOutboxEnqueueDedupAndList(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Database.Type = config.DatabaseTypeSQLite
	cfg.Database.Path = filepath.Join(t.TempDir(), "outbox.db")
	cfg.Redis.Type = config.CacheTypeMemory
	client, db, err := InitEnt(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	ctx := context.Background()
	accountID := int64(42)
	require.NoError(t, enqueueSchedulerOutbox(ctx, db, service.SchedulerOutboxEventAccountChanged, &accountID, nil, map[string]any{"v": 1}))
	require.NoError(t, enqueueSchedulerOutbox(ctx, db, service.SchedulerOutboxEventAccountChanged, &accountID, nil, map[string]any{"v": 1}))

	repo := NewSchedulerOutboxRepository(db)
	events, err := repo.ListAfterAndReleaseDedup(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, service.SchedulerOutboxEventAccountChanged, events[0].EventType)

	// The dedup key is released after the read, so a subsequent identical event
	// is admitted again.
	require.NoError(t, enqueueSchedulerOutbox(ctx, db, service.SchedulerOutboxEventAccountChanged, &accountID, nil, map[string]any{"v": 1}))
	events, err = repo.ListAfterAndReleaseDedup(ctx, events[0].ID, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
}
