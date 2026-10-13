package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

func newSQLiteUsageLogStatsRepo(t *testing.T) (*usageLogRepository, int64, time.Time, time.Time, context.Context) {
	t.Helper()
	cfg := config.NewDefaultConfig()
	cfg.Database.Type = config.DatabaseTypeSQLite
	cfg.Database.Path = filepath.Join(t.TempDir(), "usage-stats.db")
	cfg.Redis.Type = config.CacheTypeMemory

	client, db, err := InitEnt(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	ctx := context.Background()
	user, err := client.User.Create().
		SetEmail("usage-stats@example.com").
		SetPasswordHash("hash").
		Save(ctx)
	require.NoError(t, err)
	apiKey, err := client.APIKey.Create().
		SetUserID(user.ID).
		SetKey("usage-stats-key").
		SetName("usage-stats").
		Save(ctx)
	require.NoError(t, err)
	account, err := client.Account.Create().
		SetName("usage-stats-account").
		SetPlatform("openai").
		SetType("api_key").
		Save(ctx)
	require.NoError(t, err)

	inboundEndpoint := "/v1/messages"
	upstreamEndpoint := "/v1/responses"
	for i := 0; i < 3; i++ {
		_, err := db.ExecContext(ctx, `
			INSERT INTO usage_logs (
				user_id, api_key_id, account_id, request_id, model,
				input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
				total_cost, actual_cost, created_at,
				inbound_endpoint, upstream_endpoint
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			user.ID, apiKey.ID, account.ID, "req-"+string(rune('a'+i)), "claude-3",
			2, 3, 4, 5,
			0.5, 0.4, time.Now().UTC(),
			inboundEndpoint, upstreamEndpoint,
		)
		require.NoError(t, err)
	}

	repo, ok := NewUsageLogRepository(client, db).(*usageLogRepository)
	require.True(t, ok)
	now := time.Now().UTC()
	return repo, user.ID, now.Add(-time.Hour), now.Add(time.Hour), ctx
}

func TestSQLiteUsageLogGetStatsWithFiltersAggregatesAndEndpoints(t *testing.T) {
	repo, userID, start, end, ctx := newSQLiteUsageLogStatsRepo(t)

	stats, err := repo.GetStatsWithFilters(ctx, usagestats.UsageLogFilters{
		UserID:    userID,
		StartTime: &start,
		EndTime:   &end,
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), stats.TotalRequests)
	require.Equal(t, int64(6), stats.TotalInputTokens)
	require.Equal(t, int64(9), stats.TotalOutputTokens)
	require.Equal(t, int64(27), stats.TotalCacheTokens)
	require.Equal(t, int64(12), stats.TotalCacheCreationTokens)
	require.Equal(t, int64(15), stats.TotalCacheReadTokens)
	require.InDelta(t, 1.2, stats.TotalActualCost, 1e-9)
	require.NotNil(t, stats.TotalAccountCost)
	require.InDelta(t, 1.5, *stats.TotalAccountCost, 1e-9)
	require.Len(t, stats.Endpoints, 1)
	require.Equal(t, "/v1/messages", stats.Endpoints[0].Endpoint)
	require.Equal(t, int64(3), stats.Endpoints[0].Requests)
	require.Equal(t, int64(42), stats.Endpoints[0].TotalTokens)
	require.InDelta(t, 1.5, stats.Endpoints[0].Cost, 1e-9)
	require.InDelta(t, 1.2, stats.Endpoints[0].ActualCost, 1e-9)

	require.Len(t, stats.UpstreamEndpoints, 1)
	require.Equal(t, "/v1/responses", stats.UpstreamEndpoints[0].Endpoint)
	require.Equal(t, int64(3), stats.UpstreamEndpoints[0].Requests)
	require.Equal(t, int64(42), stats.UpstreamEndpoints[0].TotalTokens)
	require.InDelta(t, 1.5, stats.UpstreamEndpoints[0].Cost, 1e-9)
	require.InDelta(t, 1.2, stats.UpstreamEndpoints[0].ActualCost, 1e-9)

	require.Len(t, stats.EndpointPaths, 1)
	require.Equal(t, "/v1/messages -> /v1/responses", stats.EndpointPaths[0].Endpoint)
	require.Equal(t, int64(3), stats.EndpointPaths[0].Requests)
	require.Equal(t, int64(42), stats.EndpointPaths[0].TotalTokens)
	require.InDelta(t, 1.5, stats.EndpointPaths[0].Cost, 1e-9)
	require.InDelta(t, 1.2, stats.EndpointPaths[0].ActualCost, 1e-9)
}
