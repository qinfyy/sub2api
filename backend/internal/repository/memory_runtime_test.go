package repository

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestMemoryCapacityPreservesActiveGuards(t *testing.T) {
	c := newMemoryCacheCore(2)
	c.maxBytes = 1024
	require.True(t, memorySetNX(c, "lock", "owner", time.Hour))
	_, err := c.increment("limit", time.Hour, false)
	require.NoError(t, err)
	c.set("ordinary", []byte("discardable"), time.Hour)
	_, ok := c.get("ordinary")
	require.False(t, ok)
	v, ok := c.get("lock")
	require.True(t, ok)
	require.Equal(t, "owner", v)
	_, err = c.increment("another-limit", time.Hour, false)
	require.ErrorIs(t, err, errMemoryCapacity)
	c.delete("lock")
	_, err = c.increment("another-limit", time.Hour, false)
	require.NoError(t, err)
}

func TestMemoryConcurrencyAtomicAndExpiry(t *testing.T) {
	ctx := context.Background()
	core := newMemoryCacheCore(100)
	now := time.Now()
	core.now = func() time.Time { return now }
	cache := NewMemoryConcurrencyCache(core, 1, 20)
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 80; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, err := cache.AcquireAccountSlot(ctx, 1, 4, fmt.Sprint(i))
			if err != nil {
				t.Error(err)
			}
			if ok {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	require.EqualValues(t, 4, accepted.Load())
	n, err := cache.GetAccountConcurrency(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 4, n)
	now = now.Add(time.Minute)
	n, err = cache.GetAccountConcurrency(ctx, 1)
	require.NoError(t, err)
	require.Zero(t, n)
	core.gc()
	require.Zero(t, core.usedBytes)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = cache.AcquireUserSlot(canceled, 1, 1, "canceled")
	require.ErrorIs(t, err, context.Canceled)
}

func TestMemoryLiveLeaseAndRegularSlotsShareLimits(t *testing.T) {
	ctx := context.Background()
	c := NewMemoryConcurrencyCache(nil, 1, 10).(*memoryConcurrencyCache)
	ok, err := c.AcquireLiveLease(ctx, 1, 1, 2, 1, 3, "live", false)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = c.AcquireAccountSlot(ctx, 1, 1, "request")
	require.NoError(t, err)
	require.False(t, ok)
	counts, err := c.GetAPIKeyConcurrencyBatch(ctx, []int64{3})
	require.NoError(t, err)
	require.Equal(t, 1, counts[3])
	require.NoError(t, c.ReleaseLiveLease(ctx, 1, 2, 3, "live"))
	ok, err = c.AcquireAccountSlot(ctx, 1, 1, "request")
	require.NoError(t, err)
	require.True(t, ok)
}

func TestMemoryInflightReservationAtomicAndRenewal(t *testing.T) {
	ctx := context.Background()
	core := newMemoryCacheCore(100)
	now := time.Now()
	core.now = func() time.Time { return now }
	c := NewMemoryBillingCache(core, nil).(*memoryBillingCache)
	ok, total, err := c.ReserveInflightBalance(ctx, 1, "a", 4, 5, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.Zero(t, total)
	ok, total, err = c.ReserveInflightBalance(ctx, 1, "b", 4, 5, time.Minute)
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, float64(4), total)
	now = now.Add(30 * time.Second)
	ok, err = c.RenewInflightBalance(ctx, 1, "a", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	now = now.Add(31 * time.Second)
	ok, _, err = c.ReserveInflightBalance(ctx, 1, "b", 4, 5, time.Minute)
	require.NoError(t, err)
	require.False(t, ok)
	now = now.Add(time.Minute)
	ok, err = c.RenewInflightBalance(ctx, 1, "a", time.Minute)
	require.NoError(t, err)
	require.False(t, ok)
	core.gc()
	require.Zero(t, core.usedBytes)
}

func TestMemoryBillingWindowsAndValueIsolation(t *testing.T) {
	ctx := context.Background()
	core := newMemoryCacheCore(100)
	now := time.Now()
	core.now = func() time.Time { return now }
	c := NewMemoryBillingCache(core, nil)
	entry := &service.APIKeyRateLimitCacheData{Usage5h: 9, Window5h: now.Add(-6 * time.Hour).Unix(), Usage1d: 3, Window1d: now.Unix()}
	require.NoError(t, c.SetAPIKeyRateLimit(ctx, 1, entry))
	entry.Usage1d = 999
	require.NoError(t, c.UpdateAPIKeyRateLimitUsage(ctx, 1, 2))
	got, err := c.GetAPIKeyRateLimit(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, float64(2), got.Usage5h)
	require.Equal(t, float64(5), got.Usage1d)
	got.Usage1d = 999
	got, err = c.GetAPIKeyRateLimit(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, float64(5), got.Usage1d)
}

func TestMemoryQuotaDirtyClaimsSurviveRestartAndAckVersion(t *testing.T) {
	ctx := context.Background()
	db := newMemoryStateTestDB(t)
	c := NewMemoryBillingCache(nil, db).(*memoryBillingCache)
	entry := &service.UserPlatformQuotaCacheEntry{SchemaVersion: service.UserPlatformQuotaCacheSchemaV1, Version: 1}
	require.NoError(t, c.SetUserPlatformQuotaCache(ctx, 1, "openai", entry, time.Hour))
	require.NoError(t, c.IncrUserPlatformQuotaUsageCache(ctx, 1, "openai", 2, time.Hour, true))
	c = NewMemoryBillingCache(nil, db).(*memoryBillingCache)
	keys, err := c.PopDirtyUserPlatformQuotaKeys(ctx, 10)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	entries, err := c.BatchGetUserPlatformQuotaCache(ctx, keys)
	require.NoError(t, err)
	require.Equal(t, float64(2), entries[0].DailyUsageUSD)
	next, err := c.PopDirtyUserPlatformQuotaKeys(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, next)
	require.NoError(t, c.IncrUserPlatformQuotaUsageCache(ctx, 1, "openai", 3, time.Hour, true))
	require.NoError(t, c.AckDirtyUserPlatformQuotaKeys(ctx, keys, entries))
	require.NoError(t, c.ReaddDirtyUserPlatformQuotaKeys(ctx, keys))
	keys, err = c.PopDirtyUserPlatformQuotaKeys(ctx, 10)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	entries, err = c.BatchGetUserPlatformQuotaCache(ctx, keys)
	require.NoError(t, err)
	require.NoError(t, c.AckDirtyUserPlatformQuotaKeys(ctx, keys, entries))
	next, err = c.PopDirtyUserPlatformQuotaKeys(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, next)
}

func TestMemorySchedulerFencesRetiredWritersAndPersistsTombstone(t *testing.T) {
	ctx := context.Background()
	db := newMemoryStateTestDB(t)
	c := NewMemorySchedulerCache(nil, db)
	bucket := service.SchedulerBucket{GroupID: 1, Platform: "openai", Mode: "single"}
	token, err := c.CaptureBucketWriteToken(ctx, bucket)
	require.NoError(t, err)
	require.NoError(t, c.SetSnapshot(ctx, bucket, token, []service.Account{{ID: 1, Name: "original"}}))
	accounts, hit, err := c.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	accounts[0].Name = "mutated"
	a, err := c.GetAccount(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "original", a.Name)
	require.NoError(t, c.RetireBucket(ctx, bucket))
	require.ErrorIs(t, c.SetSnapshot(ctx, bucket, token, nil), service.ErrSchedulerBucketRetired)
	c = NewMemorySchedulerCache(nil, db)
	_, err = c.CaptureBucketWriteToken(ctx, bucket)
	require.ErrorIs(t, err, service.ErrSchedulerBucketRetired)
	reopened, err := c.ReopenBucket(ctx, bucket)
	require.NoError(t, err)
	require.Greater(t, reopened.Epoch, token.Epoch)
	require.ErrorIs(t, c.SetSnapshot(ctx, bucket, token, nil), service.ErrSchedulerBucketWriteFenced)
	require.NoError(t, c.SetSnapshot(ctx, bucket, reopened, nil))
	accounts, hit, err = c.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Empty(t, accounts)
}

func TestMemoryExpiredOwnerCannotReleaseSuccessor(t *testing.T) {
	ctx := context.Background()
	core := newMemoryCacheCore(10)
	now := time.Now()
	core.now = func() time.Time { return now }
	c := NewMemoryLeaderLockCache(core)
	ok, err := c.TryAcquireLeaderLock(ctx, "job", "a", time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	now = now.Add(time.Second)
	ok, err = c.TryAcquireLeaderLock(ctx, "job", "b", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, c.ReleaseLeaderLock(ctx, "job", "a"))
	ok, err = c.TryAcquireLeaderLock(ctx, "job", "c", time.Minute)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestMemoryInvalidationSubscriptionCancellation(t *testing.T) {
	core := newMemoryCacheCore(10)
	cache := NewMemoryAPIKeyCache(core)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	require.NoError(t, cache.SubscribeAuthCacheInvalidation(ctx, func(key string) { require.Equal(t, "key", key); calls++ }))
	require.NoError(t, cache.PublishAuthCacheInvalidation(ctx, "key"))
	require.Equal(t, 1, calls)
	cancel()
	require.NoError(t, cache.PublishAuthCacheInvalidation(context.Background(), "key"))
	require.Equal(t, 1, calls)
	core.events.close()
	require.ErrorIs(t, cache.PublishAuthCacheInvalidation(context.Background(), "key"), context.Canceled)
}

func TestMemoryBatchQueuePersistsAndFencesOldReceipt(t *testing.T) {
	ctx := context.Background()
	db := newMemoryStateTestDB(t)
	q := NewMemoryBatchImageQueue(nil, db)
	id := "imgbatch_test"
	require.NoError(t, q.Enqueue(ctx, id))
	require.ErrorIs(t, q.Enqueue(ctx, id), service.ErrBatchImageAlreadyQueued)
	q = NewMemoryBatchImageQueue(nil, db)
	job, err := q.Reserve(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, id, job.BatchID)
	require.NotEmpty(t, job.Receipt)
	old := service.WithBatchImageReservation(ctx, job)
	_, err = db.Exec(`UPDATE memory_batch_queue SET heartbeat_at=0 WHERE batch_id=?`, id)
	require.NoError(t, err)
	n, err := q.RecoverStaleActive(ctx, time.Second, 10)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	job, err = q.Reserve(ctx, 0)
	require.NoError(t, err)
	current := service.WithBatchImageReservation(ctx, job)
	require.ErrorIs(t, q.Ack(old, id), service.ErrBatchImageLockNotAcquired)
	require.ErrorIs(t, q.Heartbeat(old, id), service.ErrBatchImageLockNotAcquired)
	require.NoError(t, q.RequeueAfter(current, id, time.Millisecond))
	_, err = db.Exec(`UPDATE memory_batch_queue SET available_at=0 WHERE batch_id=?`, id)
	require.NoError(t, err)
	n, err = q.MoveDueDelayedToReady(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	job, err = q.Reserve(ctx, 0)
	require.NoError(t, err)
	require.NoError(t, q.Ack(service.WithBatchImageReservation(ctx, job), id))
	_, err = q.Reserve(ctx, 0)
	require.ErrorIs(t, err, service.ErrBatchImageQueueEmpty)
}

func TestMemoryBatchQueueConcurrentReserveSingleWinner(t *testing.T) {
	ctx := context.Background()
	q := NewMemoryBatchImageQueue(nil, newMemoryStateTestDB(t))
	require.NoError(t, q.Enqueue(ctx, "imgbatch_once"))
	var winners atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := q.Reserve(ctx, 0)
			if err == nil {
				winners.Add(1)
			} else if err != service.ErrBatchImageQueueEmpty {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, winners.Load())
}

func TestMemoryDownloadPermitCannotReleaseSuccessor(t *testing.T) {
	core := newMemoryCacheCore(10)
	now := time.Now()
	core.now = func() time.Time { return now }
	cfg := config.NewDefaultConfig()
	cfg.BatchImage.MaxDownloadConcurrencyPerUser = 1
	cfg.BatchImage.MaxDownloadDurationSeconds = 1
	c := NewMemoryBatchImageDownloadLimiter(core, cfg)
	ctx := context.Background()
	old, err := c.Acquire(ctx, "user", "zip")
	require.NoError(t, err)
	now = now.Add(time.Second)
	current, err := c.Acquire(ctx, "user", "zip")
	require.NoError(t, err)
	require.NoError(t, old.Release(ctx))
	_, err = c.Acquire(ctx, "user", "zip")
	require.ErrorIs(t, err, service.ErrBatchImageDownloadLimited)
	require.NoError(t, current.Release(ctx))
}
