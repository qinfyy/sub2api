package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSQLiteChannelMonitorV2ConfigAggregationAndQueries(t *testing.T) {
	usage, userID, _, _, base := newSQLiteUsageLogStatsRepo(t)
	ctx, cancel := context.WithTimeout(base, 20*time.Second)
	defer cancel()
	repo := NewChannelMonitorV2Repository(usage.db)
	cfg, err := repo.GetConfig(ctx)
	require.NoError(t, err)
	cfg.IgnoredErrorCategories = []string{"rate_or_capacity"}
	updated, err := repo.UpdateConfig(ctx, *cfg, cfg.Version)
	require.NoError(t, err)
	require.Equal(t, cfg.Version+1, updated.Version)
	_, err = repo.UpdateConfig(ctx, *cfg, cfg.Version)
	require.ErrorIs(t, err, service.ErrChannelMonitorV2ConfigConflict)
	start := time.Now().UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)
	end := start.Add(24 * time.Hour)
	at := start.Add(6*time.Hour + time.Minute)
	_, err = usage.db.ExecContext(ctx, `UPDATE usage_logs SET created_at=?,first_token_ms=75,duration_ms=1200`, at)
	require.NoError(t, err)
	for _, e := range []struct {
		request string
		status  int
		at      time.Time
	}{
		{"retry", 500, at}, {"retry", 429, at.Add(time.Second)}, {"", 401, at},
	} {
		_, err = usage.db.ExecContext(ctx, `INSERT INTO ops_error_logs(request_id,user_id,platform,model,error_phase,error_type,error_owner,status_code,upstream_status_code,upstream_errors,created_at)
		 VALUES(?,?,'openai','claude-3','upstream','upstream','provider',?,?,'[{},{}]',?)`, e.request, userID, e.status, e.status, e.at)
		require.NoError(t, err)
	}
	require.NoError(t, repo.RecomputeRange(ctx, start, end))
	require.NoError(t, repo.RecomputeRange(ctx, start, end))
	var stored int
	require.NoError(t, usage.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(success_requests),0) FROM channel_monitor_v2_metrics_1m`).Scan(&stored))
	require.Equal(t, 3, stored)
	wm, err := repo.GetAggregationWatermark(ctx)
	require.NoError(t, err)
	require.True(t, wm.HasData)
	require.True(t, wm.DataThrough.Equal(end))
	for _, bucket := range []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, time.Hour, 12 * time.Hour, 24 * time.Hour} {
		filter := service.ChannelMonitorV2Filter{Start: start, End: end, Bucket: bucket}
		snapshot, err := repo.GetSnapshot(ctx, filter, *updated, true)
		require.NoError(t, err, "bucket %s", bucket)
		require.EqualValues(t, 3, snapshot.Metrics.SuccessRequests, "bucket %s; coverage %#v", bucket, snapshot.Coverage)
		require.EqualValues(t, 2, snapshot.Metrics.ErrorRequests)
		require.InDelta(t, 0.2, snapshot.Metrics.ErrorRate, 1e-9)
		require.EqualValues(t, 42, snapshot.Metrics.TokenCount)
		require.EqualValues(t, 3, snapshot.Metrics.TTFT.SampleCount)
		require.EqualValues(t, 100, *snapshot.Metrics.TTFT.P95Ms)
		dimensions, err := repo.GetDimensions(ctx, filter, *updated)
		require.NoError(t, err)
		require.NotEmpty(t, dimensions.Models)
		models, err := repo.GetModels(ctx, filter, *updated, true)
		require.NoError(t, err)
		require.NotEmpty(t, models.Items)
		matrix, err := repo.GetMatrix(ctx, filter, *updated, service.ChannelMonitorV2GroupByPlatformModel, true)
		require.NoError(t, err)
		require.NotEmpty(t, matrix.Items)
		users, err := repo.GetUsers(ctx, filter, *updated, true)
		require.NoError(t, err)
		require.Len(t, users.Items, 1)
		errors, err := repo.GetErrors(ctx, filter, *updated, true)
		require.NoError(t, err)
		require.Len(t, errors.Items, 2)
	}
	// A failed rewrite must leave both facts and watermark intact.
	_, err = usage.db.ExecContext(ctx, `CREATE TRIGGER fail_monitor_rewrite BEFORE INSERT ON channel_monitor_v2_metrics_1m BEGIN SELECT RAISE(ABORT,'test rollback'); END`)
	require.NoError(t, err)
	require.Error(t, repo.RecomputeRange(ctx, start, end))
	var requests int
	require.NoError(t, usage.db.QueryRowContext(ctx, `SELECT SUM(success_requests) FROM channel_monitor_v2_metrics_1m`).Scan(&requests))
	require.Equal(t, 3, requests)
}
