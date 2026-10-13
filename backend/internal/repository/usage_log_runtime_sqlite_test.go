package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSQLiteRuntimeUsageWrites(t *testing.T) {
	r, userID, _, _, ctx := newSQLiteUsageLogStatsRepo(t)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var keyID, accountID int64
	require.NoError(t, r.db.QueryRowContext(ctx, `SELECT api_key_id,account_id FROM usage_logs LIMIT 1`).Scan(&keyID, &accountID))
	makeLog := func(id string) *service.UsageLog {
		return &service.UsageLog{UserID: userID, APIKeyID: keyID, AccountID: accountID,
			RequestID: id, Model: "test-model", InputTokens: 100, OutputTokens: 50,
			InputCost: 0.0001, OutputCost: 0.0001, TotalCost: 0.0002, ActualCost: 0.0002,
			RateMultiplier: 1, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	}
	first := makeLog("insert-once")
	inserted, err := r.Create(ctx, first)
	require.NoError(t, err)
	require.True(t, inserted)
	require.Positive(t, first.ID)
	loaded, err := r.GetByID(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, first.Model, loaded.Model)
	require.True(t, first.CreatedAt.Equal(loaded.CreatedAt))
	duplicate := makeLog(first.RequestID)
	inserted, err = r.Create(ctx, duplicate)
	require.NoError(t, err)
	require.False(t, inserted)
	require.Equal(t, first.ID, duplicate.ID)
	require.True(t, first.CreatedAt.Equal(duplicate.CreatedAt))
	for i := 0; i < 2; i++ {
		inserted, err = r.Create(ctx, makeLog(""))
		require.NoError(t, err)
		require.True(t, inserted)
	}
	batch := []usageLogInsertPrepared{prepareUsageLogInsert(first), prepareUsageLogInsert(makeLog("batch-a")), prepareUsageLogInsert(makeLog("batch-b"))}
	require.Len(t, sqliteUsageLogInsertColumns, len(batch[0].args))
	require.Equal(t, strings.Split(usageLogSelectColumns, ", ")[1:], sqliteUsageLogInsertColumns)
	query, args := buildSQLiteUsageLogInsert(batch)
	_, err = r.db.ExecContext(ctx, query, args...)
	require.NoError(t, err)
	require.NoError(t, r.CreateBestEffort(ctx, makeLog("best-effort")))
	var count int
	require.NoError(t, r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_logs WHERE model='test-model'`).Scan(&count))
	require.Equal(t, 6, count)
	logs, page, err := r.ListByUser(ctx, userID, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, logs, 9)
	require.EqualValues(t, 9, page.Total)
	outbox := NewSchedulerOutboxRepository(r.db)
	_, err = r.db.ExecContext(ctx, `INSERT INTO scheduler_outbox(event_type) VALUES('test')`)
	require.NoError(t, err)
	created, exists, err := outbox.FirstCreatedAtAfter(ctx, 0)
	require.NoError(t, err)
	require.True(t, exists)
	require.False(t, created.IsZero())
	_, exists, err = outbox.FirstCreatedAtAfter(ctx, 1<<60)
	require.NoError(t, err)
	require.False(t, exists)
	users := NewUserRepository(r.client, r.db)
	latest, err := users.GetLatestUsedAtByUserIDs(ctx, []int64{userID, userID, -1})
	require.NoError(t, err)
	require.NotNil(t, latest[userID])
	require.Nil(t, latest[-1])
	singleLatest, err := users.GetLatestUsedAtByUserID(ctx, userID)
	require.NoError(t, err)
	require.True(t, singleLatest.Equal(*latest[userID]))
	bad := makeLog("bad-fk")
	bad.UserID = -1
	_, err = r.Create(ctx, bad)
	require.Error(t, err)
	tx, err := r.client.Tx(ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	inserted, err = r.Create(dbent.NewTxContext(ctx, tx), makeLog("rollback"))
	require.NoError(t, err)
	require.True(t, inserted)
	require.NoError(t, tx.Rollback())
	require.NoError(t, r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_logs WHERE request_id='rollback'`).Scan(&count))
	require.Zero(t, count)
}
