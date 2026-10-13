package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSQLiteAPIKeyQuotaAndRateLimitWindows(t *testing.T) {
	ctx := context.Background()
	repo, client := newAPIKeyRepoSQLite(t)
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "api-key-windows@test.com")

	key, err := repo.client.APIKey.Create().
		SetUserID(user.ID).
		SetKey("sk-window-test").
		SetName("window").
		SetStatus(service.StatusActive).
		SetQuota(10).
		SetQuotaUsed(9).
		Save(ctx)
	require.NoError(t, err)

	state, err := repo.IncrementQuotaUsedAndGetState(ctx, key.ID, 2)
	require.NoError(t, err)
	require.Equal(t, 11.0, state.QuotaUsed)
	require.Equal(t, service.StatusAPIKeyQuotaExhausted, state.Status)

	require.NoError(t, repo.IncrementRateLimitUsage(ctx, key.ID, 1.5))
	rate, err := repo.GetRateLimitData(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, 1.5, rate.Usage5h)
	require.Equal(t, 1.5, rate.Usage1d)
	require.Equal(t, 1.5, rate.Usage7d)
	require.NotNil(t, rate.Window5hStart)
	require.NotNil(t, rate.Window1dStart)
	require.NotNil(t, rate.Window7dStart)

	// Expire every window and verify both counters and start times roll forward.
	expired := time.Now().UTC().Add(-8 * 24 * time.Hour)
	_, err = repo.db.ExecContext(ctx, `
		UPDATE api_keys
		SET window_5h_start = ?, window_1d_start = ?, window_7d_start = ?
		WHERE id = ?
	`, expired, expired, expired, key.ID)
	require.NoError(t, err)
	require.NoError(t, repo.IncrementRateLimitUsage(ctx, key.ID, 2))

	rate, err = repo.GetRateLimitData(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, 2.0, rate.Usage5h)
	require.Equal(t, 2.0, rate.Usage1d)
	require.Equal(t, 2.0, rate.Usage7d)
	require.True(t, rate.Window5hStart.After(expired))
	require.True(t, rate.Window1dStart.After(expired))
	require.True(t, rate.Window7dStart.After(expired))

	_, err = repo.db.ExecContext(ctx, `
		UPDATE api_keys
		SET window_5h_start = ?, window_1d_start = ?, window_7d_start = ?
		WHERE id = ?
	`, expired, expired, expired, key.ID)
	require.NoError(t, err)
	require.NoError(t, repo.ResetRateLimitWindows(ctx, key.ID))

	rate, err = repo.GetRateLimitData(ctx, key.ID)
	require.NoError(t, err)
	require.Zero(t, rate.Usage5h)
	require.Zero(t, rate.Usage1d)
	require.Zero(t, rate.Usage7d)
	require.True(t, rate.Window5hStart.After(expired))
	require.True(t, rate.Window1dStart.After(expired))
	require.True(t, rate.Window7dStart.After(expired))
}
