package repository

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/imagequeueentry"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSQLitePrimaryImageQueueContract(t *testing.T) {
	testPrimaryImageQueueContract(t, newMemoryStateTestDB(t))
}

func testPrimaryImageQueueContract(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	q := NewMemoryBatchImageQueue(nil, db).(*memoryBatchImageQueue)
	id := "imgbatch_" + uuid.NewString()
	t.Cleanup(func() {
		_, err := q.client.ImageQueueEntry.Delete().Where(imagequeueentry.IDEQ(id)).Exec(context.Background())
		require.NoError(t, err)
	})
	// Queue state and business writes can share one caller-owned Ent transaction.
	err := NewTransactionRunner(q.client).WithinTransaction(ctx, func(ctx context.Context) error {
		require.NoError(t, q.Enqueue(ctx, id))
		job, err := q.Reserve(ctx, 0)
		require.NoError(t, err)
		require.Equal(t, id, job.BatchID)
		require.NoError(t, q.Heartbeat(service.WithBatchImageReservation(ctx, job), id))
		return context.Canceled
	})
	require.ErrorIs(t, err, context.Canceled)
	exists, err := q.client.ImageQueueEntry.Query().Where(imagequeueentry.IDEQ(id)).Exist(ctx)
	require.NoError(t, err)
	require.False(t, exists)
	require.NoError(t, q.Enqueue(ctx, id))
	q = NewMemoryBatchImageQueue(nil, db).(*memoryBatchImageQueue)
	old, err := q.Reserve(ctx, 0)
	require.NoError(t, err)
	require.NoError(t, q.client.ImageQueueEntry.UpdateOneID(id).SetHeartbeatAt(0).Exec(ctx))
	n, err := q.RecoverStaleActive(ctx, time.Second, 10)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	current, err := q.Reserve(ctx, 0)
	require.NoError(t, err)
	require.NotEqual(t, old.Receipt, current.Receipt)
	oldCtx := service.WithBatchImageReservation(ctx, old)
	require.ErrorIs(t, q.Ack(oldCtx, id), service.ErrBatchImageLockNotAcquired)
	require.ErrorIs(t, q.Heartbeat(oldCtx, id), service.ErrBatchImageLockNotAcquired)
	require.ErrorIs(t, q.RequeueAfter(oldCtx, id, 0), service.ErrBatchImageLockNotAcquired)
	err = NewTransactionRunner(q.client).WithinTransaction(ctx, func(ctx context.Context) error {
		require.NoError(t, q.Ack(service.WithBatchImageReservation(ctx, current), id))
		return context.Canceled
	})
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, q.Ack(service.WithBatchImageReservation(ctx, current), id))
}

func TestSQLitePrimaryImageQueueConcurrentClaims(t *testing.T) {
	testPrimaryImageQueueConcurrentClaims(t, newMemoryStateTestDB(t))
}

func testPrimaryImageQueueConcurrentClaims(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	q := NewMemoryBatchImageQueue(nil, db).(*memoryBatchImageQueue)
	const count = 24
	ids := make([]string, count)
	for i := range ids {
		ids[i] = "imgbatch_" + uuid.NewString()
		require.NoError(t, q.Enqueue(ctx, ids[i]))
	}
	t.Cleanup(func() {
		_, err := q.client.ImageQueueEntry.Delete().Where(imagequeueentry.IDIn(ids...)).Exec(context.Background())
		require.NoError(t, err)
	})
	jobs := make(chan service.ReservedBatchImageJob, count)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				job, err := q.Reserve(ctx, 0)
				if errors.Is(err, service.ErrBatchImageQueueEmpty) {
					return
				}
				if err != nil {
					t.Error(err)
					return
				}
				jobs <- job
			}
		}()
	}
	workers.Wait()
	close(jobs)
	seen := make(map[string]bool, count)
	for job := range jobs {
		require.False(t, seen[job.BatchID], "a ready job must have only one claimant")
		seen[job.BatchID] = true
		require.NotEmpty(t, job.Receipt)
		require.NoError(t, q.Ack(service.WithBatchImageReservation(ctx, job), job.BatchID))
	}
	require.Len(t, seen, count)
}
