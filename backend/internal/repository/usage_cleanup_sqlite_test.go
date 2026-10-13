package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newSQLiteUsageCleanupRepo(t *testing.T) (*usageCleanupRepository, context.Context) {
	t.Helper()
	cfg := config.NewDefaultConfig()
	cfg.Database.Type = config.DatabaseTypeSQLite
	cfg.Database.Path = filepath.Join(t.TempDir(), "cleanup.db")
	cfg.Redis.Type = config.CacheTypeMemory
	client, db, err := InitEnt(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return newUsageCleanupRepositoryWithSQL(client, db), context.Background()
}

func TestSQLiteUsageCleanupTaskLifecycle(t *testing.T) {
	repo, ctx := newSQLiteUsageCleanupRepo(t)

	task := &service.UsageCleanupTask{
		Status:    service.UsageCleanupStatusPending,
		CreatedBy: 1,
	}
	require.NoError(t, repo.CreateTask(ctx, task))
	require.NotZero(t, task.ID)

	claimed, err := repo.ClaimNextPendingTask(ctx, 1800)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, task.ID, claimed.ID)
	require.Equal(t, service.UsageCleanupStatusRunning, claimed.Status)

	// 已被领取且未超时，第二次领取应返回空。
	second, err := repo.ClaimNextPendingTask(ctx, 1800)
	require.NoError(t, err)
	require.Nil(t, second)

	require.NoError(t, repo.UpdateTaskProgress(ctx, task.ID, 7))
	require.NoError(t, repo.MarkTaskSucceeded(ctx, task.ID, 7))

	status, err := repo.GetTaskStatus(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, service.UsageCleanupStatusSucceeded, status)
}

func TestSQLiteUsageCleanupCancelTask(t *testing.T) {
	repo, ctx := newSQLiteUsageCleanupRepo(t)

	task := &service.UsageCleanupTask{Status: service.UsageCleanupStatusPending, CreatedBy: 1}
	require.NoError(t, repo.CreateTask(ctx, task))

	canceled, err := repo.CancelTask(ctx, task.ID, 9)
	require.NoError(t, err)
	require.True(t, canceled)

	canceled, err = repo.CancelTask(ctx, task.ID, 9)
	require.NoError(t, err)
	require.False(t, canceled)
}
