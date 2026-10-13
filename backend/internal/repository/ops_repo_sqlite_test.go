package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSQLiteOpsAggregationAndTrendsWithData(t *testing.T) {
	usage, _, _, _, ctx := newSQLiteUsageLogStatsRepo(t)
	r := NewOpsRepository(usage.db).(*opsRepository)
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	g, err := usage.client.Group.Create().SetName("ops").SetPlatform("openai").Save(ctx)
	require.NoError(t, err)
	_, err = usage.db.ExecContext(ctx, `UPDATE usage_logs SET group_id=?,created_at=?,duration_ms=id*200-100,first_token_ms=id*20`, g.ID, start.Add(time.Minute))
	require.NoError(t, err)
	_, err = usage.db.ExecContext(ctx, `UPDATE usage_logs SET created_at=? WHERE id=2`, start.Add(time.Minute).Format(time.RFC3339Nano))
	require.NoError(t, err)
	_, err = usage.db.ExecContext(ctx, `INSERT INTO ops_error_logs(request_id,platform,group_id,error_phase,error_type,error_owner,status_code,upstream_status_code,upstream_errors,created_at) VALUES('ops-test','openai',?,'upstream','http','provider',502,502,?,?)`, g.ID, `[{"kind":"failover:capacity"},{"kind":"retry"}]`, start.Add(time.Minute))
	require.NoError(t, err)
	filter := &service.OpsDashboardFilter{StartTime: start, EndTime: end, Platform: "openai", GroupID: &g.ID}
	duration, ttft, count, err := r.queryUsageLatency(ctx, filter, start, end)
	require.NoError(t, err)
	require.EqualValues(t, 3, count)
	require.Equal(t, 300, *duration.P50)
	require.Equal(t, 460, *duration.P90)
	require.Equal(t, 40, *ttft.P50)
	for i := 0; i < 2; i++ {
		require.NoError(t, r.UpsertHourlyMetrics(ctx, start, end))
	}
	hourly, err := r.listHourlyMetricsRows(ctx, filter, start, end)
	require.NoError(t, err)
	require.Len(t, hourly, 1)
	require.EqualValues(t, 3, hourly[0].successCount)
	require.EqualValues(t, 1, hourly[0].errorCountTotal)
	require.EqualValues(t, 460, hourly[0].durationP90.Int64)
	latest, exists, err := r.GetLatestHourlyBucketStart(ctx)
	require.NoError(t, err)
	require.True(t, exists)
	require.True(t, latest.Equal(start))
	require.NoError(t, r.UpsertDailyMetrics(ctx, start, end))
	_, exists, err = r.GetLatestDailyBucketDate(ctx)
	require.NoError(t, err)
	require.True(t, exists)
	trend, err := r.GetThroughputTrend(ctx, filter, 60)
	require.NoError(t, err)
	var requests, switches int64
	for _, p := range trend.Points {
		requests += p.RequestCount
		switches += p.SwitchCount
	}
	require.EqualValues(t, 4, requests)
	require.EqualValues(t, 1, switches)
	histogram, err := r.GetLatencyHistogram(ctx, filter)
	require.NoError(t, err)
	require.EqualValues(t, 3, histogram.TotalRequests)
	_, err = r.GetErrorTrend(ctx, filter, 60)
	require.NoError(t, err)
	_, err = r.GetErrorLogByID(ctx, 1)
	require.NoError(t, err)
}
