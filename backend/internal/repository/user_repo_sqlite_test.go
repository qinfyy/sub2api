package repository

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newSQLiteUserRepo(t *testing.T) (*userRepository, context.Context) {
	t.Helper()
	cfg := config.NewDefaultConfig()
	cfg.Database.Type = config.DatabaseTypeSQLite
	cfg.Database.Path = filepath.Join(t.TempDir(), "users.db")
	cfg.Redis.Type = config.CacheTypeMemory
	client, db, err := InitEnt(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	ctx := context.Background()
	_, err = db.ExecContext(ctx, `
		INSERT INTO users (id, username, email, password_hash, balance, frozen_balance, concurrency, rpm_limit, status, created_at, updated_at)
		VALUES
			(1, 'u1', 'u1@example.com', 'hash', 10, 0, 3, 60, 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
			(2, 'u2', 'u2@example.com', 'hash', -5, 0, 1, 60, 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`)
	require.NoError(t, err)
	return newUserRepositoryWithSQL(client, db), ctx
}

func TestSQLiteUserBalanceAdjustments(t *testing.T) {
	repo, ctx := newSQLiteUserRepo(t)

	change, err := repo.AdjustBalance(ctx, 1, -4)
	require.NoError(t, err)
	require.InDelta(t, 10, change.Old, 1e-9)
	require.InDelta(t, 6, change.New, 1e-9)

	_, err = repo.AdjustBalance(ctx, 1, -100)
	require.ErrorIs(t, err, service.ErrBalanceNegative)

	deducted, err := repo.DeductAvailableBalance(ctx, 2, 3)
	require.NoError(t, err)
	require.InDelta(t, 0, deducted, 1e-9)

	var balance float64
	require.NoError(t, repo.db.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = 1`).Scan(&balance))
	require.InDelta(t, 6, balance, 1e-9)
}

func TestSQLiteUserConcurrencyBatchUpdates(t *testing.T) {
	repo, ctx := newSQLiteUserRepo(t)

	affected, err := repo.BatchSetConcurrency(ctx, []int64{1, 2}, 5)
	require.NoError(t, err)
	require.Equal(t, 2, affected)

	affected, err = repo.BatchAddConcurrency(ctx, []int64{1, 2}, -3)
	require.NoError(t, err)
	require.Equal(t, 2, affected)

	var c1, c2 int
	require.NoError(t, repo.db.QueryRowContext(ctx, `SELECT concurrency FROM users WHERE id = 1`).Scan(&c1))
	require.NoError(t, repo.db.QueryRowContext(ctx, `SELECT concurrency FROM users WHERE id = 2`).Scan(&c2))
	require.Equal(t, 2, c1)
	require.Equal(t, 2, c2)

	affected, err = repo.BatchUpdateLimits(ctx, []int64{1}, nil, intPtr(120))
	require.NoError(t, err)
	require.Equal(t, 1, affected)
}

func TestSQLiteUserBalanceConcurrentDeductions(t *testing.T) {
	repo, ctx := newSQLiteUserRepo(t)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = repo.AdjustBalance(ctx, 1, -0.25)
		}()
	}
	wg.Wait()

	var balance float64
	require.NoError(t, repo.db.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = 1`).Scan(&balance))
	require.InDelta(t, 5, balance, 1e-6)
}

func intPtr(v int) *int { return &v }
