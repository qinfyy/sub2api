package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func TestSQLiteDashboardAggregateReadRecomputeAndRetention(t *testing.T) {
	usage, _, start, end, ctx := newSQLiteUsageLogStatsRepo(t)
	aggregator := NewDashboardAggregationRepository(usage.db)
	require.NotNil(t, aggregator)
	require.NoError(t, aggregator.AggregateRange(ctx, start, end))
	require.NoError(t, aggregator.AggregateRange(ctx, start, end))
	stats, err := usage.GetDashboardStats(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 3, stats.TotalRequests)
	require.EqualValues(t, 3, stats.TodayRequests)
	require.EqualValues(t, 1, stats.ActiveUsers)
	stats, err = usage.GetDashboardStatsWithRange(ctx, start, end)
	require.NoError(t, err)
	require.EqualValues(t, 3, stats.TotalRequests)
	require.NoError(t, aggregator.UpdateAggregationWatermark(ctx, end))
	mark, err := aggregator.GetAggregationWatermark(ctx)
	require.NoError(t, err)
	require.True(t, mark.Equal(end))
	_, err = usage.db.ExecContext(ctx, `DELETE FROM usage_logs WHERE request_id='req-a'`)
	require.NoError(t, err)
	require.NoError(t, aggregator.RecomputeRange(ctx, start, end))
	stats, err = usage.GetDashboardStats(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, stats.TotalRequests)
	_, err = usage.GetAllGroupUsageSummary(ctx, timezone.Today())
	require.NoError(t, err)
	_, err = usage.db.ExecContext(ctx, `INSERT INTO usage_billing_dedup(request_id,api_key_id,request_fingerprint,created_at) VALUES('old',1,'test','2000-01-01T00:00:00Z')`)
	require.NoError(t, err)
	require.NoError(t, aggregator.CleanupUsageBillingDedup(ctx, time.Now()))
	var count int
	require.NoError(t, usage.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_billing_dedup_archive WHERE request_id='old'`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, aggregator.CleanupUsageLogs(ctx, end))
	require.NoError(t, aggregator.CleanupAggregates(ctx, end, end.AddDate(0, 0, 1)))
	require.NoError(t, aggregator.EnsureUsageLogsPartitions(ctx, end))
}

func TestSQLiteTimeBucketsHonorZoneAndDST(t *testing.T) {
	ctx, db := newSQLiteCompatDB(t)
	for _, tt := range []struct{ at, unit, zone, want string }{
		{"2026-01-01T17:00:00Z", "day", "Asia/Shanghai", "2026-01-02"},
		{"2026-03-08T06:30:00Z", "hour", "America/New_York", "2026-03-08T06:00:00Z"},
		{"2026-03-08T07:30:00Z", "hour", "America/New_York", "2026-03-08T07:00:00Z"},
		{"2026-01-01T00:20:00Z", "hour", "Asia/Kathmandu", "2026-01-01T00:15:00Z"},
	} {
		var got string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT sub2api_time_bucket(?,?,?)`, tt.at, tt.unit, tt.zone).Scan(&got))
		require.Equal(t, tt.want, got)
	}
}
