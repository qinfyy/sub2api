package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newSQLiteUsageBillingRepo(t *testing.T) (*usageBillingRepository, context.Context) {
	t.Helper()
	cfg := config.NewDefaultConfig()
	cfg.Database.Type = config.DatabaseTypeSQLite
	cfg.Database.Path = filepath.Join(t.TempDir(), "billing.db")
	cfg.Redis.Type = config.CacheTypeMemory
	client, db, err := InitEnt(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	ctx := context.Background()
	_, err = db.ExecContext(ctx, `
		INSERT INTO users (id, username, email, password_hash, balance, frozen_balance, status, created_at, updated_at)
		VALUES (1, 'u1', 'u1@example.com', 'hash', 10, 0, 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO api_keys (id, user_id, key, name, status, quota, quota_used, created_at, updated_at)
		VALUES (7, 1, 'key7', 'k1', 'active', 5, 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`)
	require.NoError(t, err)
	return &usageBillingRepository{db: db, sqlite: true}, ctx
}

func TestSQLiteUsageBillingApplyIsIdempotentAndUpdatesQuota(t *testing.T) {
	repo, ctx := newSQLiteUsageBillingRepo(t)
	cmd := &service.UsageBillingCommand{
		RequestID:           "req-sqlite-1",
		RequestFingerprint:  "fp-1",
		UserID:              1,
		APIKeyID:            7,
		BalanceCost:         1.25,
		APIKeyQuotaCost:     1.25,
		APIKeyRateLimitCost: 1.25,
	}
	first, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, first.Applied)
	require.NotNil(t, first.NewBalance)
	require.InDelta(t, 8.75, *first.NewBalance, 0.000001)

	second, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, second.Applied)

	var balance, quotaUsed, usage5h float64
	require.NoError(t, repo.db.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = 1`).Scan(&balance))
	require.NoError(t, repo.db.QueryRowContext(ctx, `SELECT quota_used, usage_5h FROM api_keys WHERE id = 7`).Scan(&quotaUsed, &usage5h))
	require.InDelta(t, 8.75, balance, 0.000001)
	require.InDelta(t, 1.25, quotaUsed, 0.000001)
	require.InDelta(t, 1.25, usage5h, 0.000001)
}

func TestSQLiteBatchImageBalanceHoldLifecycle(t *testing.T) {
	repo, ctx := newSQLiteUsageBillingRepo(t)
	hold := &service.BatchImageBalanceHoldCommand{
		RequestID:          service.BatchImageHoldRequestID("imgbatch_sqlite"),
		RequestFingerprint: "hold-fp",
		BatchID:            "imgbatch_sqlite",
		UserID:             1,
		APIKeyID:           7,
		HoldAmount:         2,
	}
	reserved, err := repo.ReserveBatchImageBalance(ctx, hold)
	require.NoError(t, err)
	require.True(t, reserved.Applied)
	require.InDelta(t, 8, *reserved.NewBalance, 0.000001)
	require.InDelta(t, 2, *reserved.FrozenBalance, 0.000001)

	capture := *hold
	capture.RequestID = "capture-sqlite"
	capture.RequestFingerprint = "capture-fp"
	capture.ActualAmount = 0.5
	captured, err := repo.CaptureBatchImageBalance(ctx, &capture)
	require.NoError(t, err)
	require.True(t, captured.Applied)
	require.InDelta(t, 9.5, *captured.NewBalance, 0.000001)
	require.InDelta(t, 0, *captured.FrozenBalance, 0.000001)
}
