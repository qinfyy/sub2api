package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type memorySchedulerCache struct {
	core  *memoryCacheCore
	state memoryKVBackend
}
type memoryBucketState struct {
	Epoch      int64
	Retired    bool
	Registered bool
}
type memorySnapshot struct {
	Epoch      int64
	AccountIDs []int64
}

const memoryBucketPrefix = "memory:scheduler:bucket:"

func NewMemorySchedulerCache(core *memoryCacheCore, db *sql.DB) service.SchedulerCache {
	core = memoryAssertCore(core)
	c := &memorySchedulerCache{core: core, state: memoryKVBackend{core: core}}
	if db != nil {
		c.state.db = db
	}
	return c
}
func (c *memorySchedulerCache) bucketState(ctx context.Context, b service.SchedulerBucket) (memoryBucketState, error) {
	raw, err := c.state.get(ctx, memoryBucketPrefix+b.String())
	if errors.Is(err, errMemoryCacheMiss) {
		return memoryBucketState{Epoch: 1}, nil
	}
	if err != nil {
		return memoryBucketState{}, err
	}
	var v memoryBucketState
	err = json.Unmarshal(raw, &v)
	return v, err
}
func (c *memorySchedulerCache) CaptureBucketWriteToken(ctx context.Context, b service.SchedulerBucket) (service.SchedulerBucketWriteToken, error) {
	mu := memoryLockKey(memoryBucketPrefix + b.String())
	defer memoryUnlock(mu)
	v, err := c.bucketState(ctx, b)
	if err != nil {
		return service.SchedulerBucketWriteToken{}, err
	}
	if v.Retired {
		return service.SchedulerBucketWriteToken{}, service.ErrSchedulerBucketRetired
	}
	if err := c.state.set(ctx, memoryBucketPrefix+b.String(), v, 0); err != nil {
		return service.SchedulerBucketWriteToken{}, err
	}
	return service.SchedulerBucketWriteToken{Bucket: b, Epoch: v.Epoch}, nil
}
func (c *memorySchedulerCache) SetSnapshot(ctx context.Context, b service.SchedulerBucket, t service.SchedulerBucketWriteToken, accounts []service.Account) error {
	mu := memoryLockKey(memoryBucketPrefix + b.String())
	defer memoryUnlock(mu)
	v, err := c.bucketState(ctx, b)
	if err != nil {
		return err
	}
	if v.Retired {
		return service.ErrSchedulerBucketRetired
	}
	if !t.ValidFor(b) || v.Epoch != t.Epoch {
		return service.ErrSchedulerBucketWriteFenced
	}
	ids := make([]int64, 0, len(accounts))
	for i := range accounts {
		if err := c.SetAccount(ctx, &accounts[i]); err != nil {
			return err
		}
		ids = append(ids, accounts[i].ID)
	}
	v.Registered = true
	if err := c.state.set(ctx, memoryBucketPrefix+b.String(), v, 0); err != nil {
		return err
	}
	return writeMemoryJSON(ctx, c.core, schedulerBucketKey(schedulerSnapshotPrefix, b), memorySnapshot{v.Epoch, ids}, 0)
}
func (c *memorySchedulerCache) GetSnapshot(ctx context.Context, b service.SchedulerBucket) ([]*service.Account, bool, error) {
	mu := memoryLockKey(memoryBucketPrefix + b.String())
	defer memoryUnlock(mu)
	v, err := c.bucketState(ctx, b)
	if err != nil {
		return nil, false, err
	}
	if v.Retired {
		return nil, false, nil
	}
	s, hit, err := readMemoryJSON[memorySnapshot](ctx, c.core, schedulerBucketKey(schedulerSnapshotPrefix, b))
	if err != nil || !hit {
		return nil, false, err
	}
	if s.Epoch != v.Epoch {
		return nil, false, nil
	}
	accounts := make([]*service.Account, 0, len(s.AccountIDs))
	for _, id := range s.AccountIDs {
		a, err := c.GetAccount(ctx, id)
		if err != nil {
			return nil, false, err
		}
		if a == nil {
			return nil, false, nil
		}
		accounts = append(accounts, a)
	}
	return accounts, true, nil
}
func (c *memorySchedulerCache) RetireBucket(ctx context.Context, b service.SchedulerBucket) error {
	mu := memoryLockKey(memoryBucketPrefix + b.String())
	defer memoryUnlock(mu)
	v, err := c.bucketState(ctx, b)
	if err != nil {
		return err
	}
	if !v.Retired {
		v.Epoch++
	}
	v.Retired = true
	v.Registered = false
	if err := c.state.set(ctx, memoryBucketPrefix+b.String(), v, 0); err != nil {
		return err
	}
	c.core.delete(schedulerBucketKey(schedulerSnapshotPrefix, b))
	return nil
}
func (c *memorySchedulerCache) ReopenBucket(ctx context.Context, b service.SchedulerBucket) (service.SchedulerBucketWriteToken, error) {
	mu := memoryLockKey(memoryBucketPrefix + b.String())
	defer memoryUnlock(mu)
	v, err := c.bucketState(ctx, b)
	if err != nil {
		return service.SchedulerBucketWriteToken{}, err
	}
	v.Retired = false
	if err := c.state.set(ctx, memoryBucketPrefix+b.String(), v, 0); err != nil {
		return service.SchedulerBucketWriteToken{}, err
	}
	return service.SchedulerBucketWriteToken{Bucket: b, Epoch: v.Epoch}, nil
}
func (c *memorySchedulerCache) TryAcquireGroupLifecycleLease(ctx context.Context, id int64, ttl time.Duration) (service.SchedulerGroupLifecycleLease, bool, error) {
	if id <= 0 || ttl <= 0 {
		return service.SchedulerGroupLifecycleLease{}, false, service.ErrSchedulerGroupLifecycleLeaseInvalid
	}
	token, err := newBatchImageLockToken()
	if err != nil {
		return service.SchedulerGroupLifecycleLease{}, false, err
	}
	lock := memoryLeaderLockCache{c.core}
	ok, err := lock.TryAcquireLeaderLock(ctx, schedulerGroupLifecycleLockPrefix+strconv.FormatInt(id, 10), token, ttl)
	if !ok || err != nil {
		return service.SchedulerGroupLifecycleLease{}, false, err
	}
	return service.SchedulerGroupLifecycleLease{GroupID: id, OwnerToken: token}, true, nil
}
func (c *memorySchedulerCache) ReleaseGroupLifecycleLease(ctx context.Context, l service.SchedulerGroupLifecycleLease) error {
	if !l.ValidFor(l.GroupID) {
		return service.ErrSchedulerGroupLifecycleLeaseInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.core.mu.Lock()
	defer c.core.mu.Unlock()
	key := "leader:" + schedulerGroupLifecycleLockPrefix + strconv.FormatInt(l.GroupID, 10)
	e := c.core.entries[key]
	if e == nil || c.core.expiredLocked(e) || e.value != l.OwnerToken {
		return service.ErrSchedulerGroupLifecycleLeaseLost
	}
	c.core.removeLocked(e)
	return nil
}
func (c *memorySchedulerCache) GetAccount(ctx context.Context, id int64) (*service.Account, error) {
	v, hit, err := readMemoryJSON[service.Account](ctx, c.core, schedulerAccountPrefix+strconv.FormatInt(id, 10))
	if err != nil || !hit {
		return nil, err
	}
	return &v, nil
}
func (c *memorySchedulerCache) SetAccount(ctx context.Context, a *service.Account) error {
	if a == nil {
		return nil
	}
	return changeMemoryJSON(ctx, c.core, schedulerAccountPrefix+strconv.FormatInt(a.ID, 10), 0, false, func(old service.Account, hit bool) (service.Account, bool, error) {
		next := *a
		if hit && old.LastUsedAt != nil && (next.LastUsedAt == nil || old.LastUsedAt.After(*next.LastUsedAt)) {
			next.LastUsedAt = old.LastUsedAt
		}
		return next, true, nil
	})
}
func (c *memorySchedulerCache) DeleteAccount(ctx context.Context, id int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.core.delete(schedulerAccountPrefix + strconv.FormatInt(id, 10))
	return nil
}
func (c *memorySchedulerCache) UpdateLastUsed(ctx context.Context, updates map[int64]time.Time) error {
	for id, at := range updates {
		err := changeMemoryJSON(ctx, c.core, schedulerAccountPrefix+strconv.FormatInt(id, 10), 0, false, func(a service.Account, hit bool) (service.Account, bool, error) {
			if a.LastUsedAt == nil || at.After(*a.LastUsedAt) {
				v := at
				a.LastUsedAt = &v
			}
			return a, hit, nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
func (c *memorySchedulerCache) TryLockBucket(ctx context.Context, b service.SchedulerBucket, ttl time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return memorySetNX(c.core, schedulerBucketKey(schedulerLockPrefix, b), true, ttl), nil
}
func (c *memorySchedulerCache) UnlockBucket(ctx context.Context, b service.SchedulerBucket) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.core.delete(schedulerBucketKey(schedulerLockPrefix, b))
	return nil
}
func (c *memorySchedulerCache) ListBuckets(ctx context.Context) ([]service.SchedulerBucket, error) {
	var keys []string
	if c.state.db != nil {
		rows, err := c.state.db.QueryContext(ctx, `SELECT key FROM kv_store WHERE key>=? AND key<? ORDER BY key`, memoryBucketPrefix, memoryBucketPrefix+"\uffff")
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				_ = rows.Close()
				return nil, err
			}
			keys = append(keys, key)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
	} else {
		c.core.mu.Lock()
		for key := range c.core.entries {
			if strings.HasPrefix(key, memoryBucketPrefix) {
				keys = append(keys, key)
			}
		}
		c.core.mu.Unlock()
		sort.Strings(keys)
	}
	var buckets []service.SchedulerBucket
	for _, key := range keys {
		b, ok := service.ParseSchedulerBucket(strings.TrimPrefix(key, memoryBucketPrefix))
		if !ok {
			continue
		}
		v, err := c.bucketState(ctx, b)
		if err != nil {
			return nil, err
		}
		if !v.Retired && v.Registered {
			buckets = append(buckets, b)
		}
	}
	return buckets, nil
}
func (c *memorySchedulerCache) GetOutboxWatermark(ctx context.Context) (int64, error) {
	raw, err := c.state.get(ctx, schedulerOutboxWatermarkKey)
	if errors.Is(err, errMemoryCacheMiss) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var n int64
	err = json.Unmarshal(raw, &n)
	return n, err
}
func (c *memorySchedulerCache) SetOutboxWatermark(ctx context.Context, id int64) error {
	if id < 0 {
		return fmt.Errorf("negative scheduler watermark")
	}
	mu := memoryLockKey(schedulerOutboxWatermarkKey)
	defer memoryUnlock(mu)
	old, err := c.GetOutboxWatermark(ctx)
	if err != nil {
		return err
	}
	if old > id {
		return nil
	}
	return c.state.set(ctx, schedulerOutboxWatermarkKey, id, 0)
}
