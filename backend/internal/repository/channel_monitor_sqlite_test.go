package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSQLiteChannelMonitorHistoryRollupAndWatermark(t *testing.T) {
	usage, _, _, _, ctx := newSQLiteUsageLogStatsRepo(t)
	repo := NewChannelMonitorRepository(usage.client, usage.db)
	m := &service.ChannelMonitor{Name: "test", Provider: "openai", Endpoint: "http://127.0.0.1", APIKey: "test-key", PrimaryModel: "model", IntervalSeconds: 60}
	require.NoError(t, repo.Create(ctx, m))
	at := time.Now().UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)
	for i, status := range []string{"operational", "degraded", "failed"} {
		_, err := usage.db.ExecContext(ctx, `INSERT INTO channel_monitor_histories(monitor_id,model,status,latency_ms,checked_at) VALUES(?,'model',?,100,?)`, m.ID, status, at.Add(time.Duration(i)*time.Hour))
		require.NoError(t, err)
	}
	latest, err := repo.ListLatestPerModel(ctx, m.ID)
	require.NoError(t, err)
	require.Len(t, latest, 1)
	require.Equal(t, "failed", latest[0].Status)
	recent, err := repo.ListRecentHistoryForMonitors(ctx, []int64{m.ID}, map[int64]string{m.ID: "model"}, 2)
	require.NoError(t, err)
	require.Len(t, recent[m.ID], 2)
	availability, err := repo.ComputeAvailability(ctx, m.ID, 7)
	require.NoError(t, err)
	require.Len(t, availability, 1)
	require.EqualValues(t, 3, availability[0].TotalChecks)
	require.EqualValues(t, 2, availability[0].OperationalChecks)
	_, err = repo.UpsertDailyRollupsFor(ctx, at)
	require.NoError(t, err)
	_, err = repo.UpsertDailyRollupsFor(ctx, at)
	require.NoError(t, err)
	var count int
	require.NoError(t, usage.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM channel_monitor_daily_rollups`).Scan(&count))
	require.Equal(t, 1, count)
	mark, err := repo.LoadAggregationWatermark(ctx)
	require.NoError(t, err)
	require.Nil(t, mark)
	require.NoError(t, repo.UpdateAggregationWatermark(ctx, at))
	mark, err = repo.LoadAggregationWatermark(ctx)
	require.NoError(t, err)
	require.True(t, mark.Equal(at))
	n, err := repo.DeleteHistoryBefore(ctx, at.Add(24*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 3, n)
	n, err = repo.DeleteRollupsBefore(ctx, at.Add(24*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}
