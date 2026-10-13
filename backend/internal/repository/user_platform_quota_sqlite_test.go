package repository

import (
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestSQLitePlatformQuotaAtomicUsageAndSnapshot(t *testing.T) {
	usage, userID, _, _, ctx := newSQLiteUsageLogStatsRepo(t)
	r := NewUserPlatformQuotaRepository(usage.client)
	limit := 100.0
	rows := []UserPlatformQuotaRecord{{UserID: userID, Platform: "openai", DailyLimitUSD: &limit}}
	require.NoError(t, r.BulkInsertInitial(ctx, rows))
	require.NoError(t, r.BulkInsertInitial(ctx, rows))
	now := time.Now()
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- r.IncrementUsageWithReset(ctx, userID, "openai", 0.5, now) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	record, err := r.GetByUserPlatform(ctx, userID, "openai")
	require.NoError(t, err)
	require.Equal(t, 10.0, record.DailyUsageUSD)
	require.Equal(t, 10.0, record.MonthlyUsageUSD)

	tx, err := usage.client.Tx(ctx)
	require.NoError(t, err)
	txCtx := dbent.NewTxContext(ctx, tx)
	require.NoError(t, r.IncrementUsageWithReset(txCtx, userID, "openai", 4, now))
	require.NoError(t, r.BatchSnapshotUsage(txCtx, []UserPlatformQuotaSnapshot{{UserID: userID, Platform: "openai", DailyUsageUSD: 50, DailyWindowStart: now, WeeklyWindowStart: now, MonthlyWindowStart: now}}, now))
	require.NoError(t, tx.Rollback())
	record, err = r.GetByUserPlatform(ctx, userID, "openai")
	require.NoError(t, err)
	require.Equal(t, 10.0, record.DailyUsageUSD)

	require.NoError(t, r.IncrementUsageWithReset(ctx, userID, "openai", 2, now.AddDate(0, 0, 1)))
	record, err = r.GetByUserPlatform(ctx, userID, "openai")
	require.NoError(t, err)
	require.Equal(t, 2.0, record.DailyUsageUSD)
	require.Equal(t, 12.0, record.MonthlyUsageUSD)
	require.NoError(t, r.UpsertForUser(ctx, userID, nil))
	require.NoError(t, r.BatchSnapshotUsage(ctx, []UserPlatformQuotaSnapshot{{UserID: userID, Platform: "openai", DailyUsageUSD: 50, DailyWindowStart: now, WeeklyWindowStart: now, MonthlyWindowStart: now}}, now))
	record, err = r.GetByUserPlatform(ctx, userID, "openai")
	require.NoError(t, err)
	require.Nil(t, record, "snapshots must not resurrect removed limits")
}
