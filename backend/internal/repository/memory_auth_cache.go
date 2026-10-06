package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type memoryAPIKeyCache struct{ core *memoryCacheCore }

func NewMemoryAPIKeyCache(core *memoryCacheCore) service.APIKeyCache {
	return &memoryAPIKeyCache{core: memoryAssertCore(core)}
}

var _ service.APIKeyCache = (*memoryAPIKeyCache)(nil)

func (c *memoryAPIKeyCache) GetCreateAttemptCount(ctx context.Context, userID int64) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	value, _ := c.core.get(apiKeyRateLimitKey(userID))
	return int(memoryInt64(value)), nil
}
func (c *memoryAPIKeyCache) IncrementCreateAttemptCount(ctx context.Context, userID int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := c.core.increment(apiKeyRateLimitKey(userID), apiKeyRateLimitDuration, true)
	return err
}
func (c *memoryAPIKeyCache) IncrementCreateCount(ctx context.Context, userID int64, window time.Duration) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return c.core.increment(apiKeyCreateCountKey(userID), window, false)
}
func (c *memoryAPIKeyCache) IncrementDailyUsage(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := c.core.increment(key, 0, false)
	return err
}
func (c *memoryAPIKeyCache) SetDailyUsageExpiry(ctx context.Context, key string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.core.touch(key, ttl)
	return nil
}
func (c *memoryAPIKeyCache) GetAuthCache(ctx context.Context, key string) (*service.APIKeyAuthCacheEntry, error) {
	entry, hit, err := readMemoryJSON[service.APIKeyAuthCacheEntry](ctx, c.core, apiKeyAuthCacheKey(key))
	if err != nil {
		return nil, err
	}
	if !hit {
		return nil, errMemoryCacheMiss
	}
	return &entry, nil
}
func (c *memoryAPIKeyCache) SetAuthCache(ctx context.Context, key string, entry *service.APIKeyAuthCacheEntry, ttl time.Duration) error {
	if entry == nil {
		return nil
	}
	return writeMemoryJSON(ctx, c.core, apiKeyAuthCacheKey(key), *entry, ttl)
}
func (c *memoryAPIKeyCache) DeleteAuthCache(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.core.delete(apiKeyAuthCacheKey(key))
	return nil
}
func (c *memoryAPIKeyCache) PublishAuthCacheInvalidation(ctx context.Context, key string) error {
	return c.core.events.publish(ctx, authCacheInvalidateChannel, key)
}
func (c *memoryAPIKeyCache) SubscribeAuthCacheInvalidation(ctx context.Context, handler func(string)) error {
	if err := c.core.events.subscribe(ctx, authCacheInvalidateChannel, handler); err != nil {
		return err
	}
	service.NotifyAuthCacheSubscriptionReady(ctx)
	return nil
}

type memoryChannelCache struct{ core *memoryCacheCore }

func NewMemoryChannelCache(core *memoryCacheCore) service.ChannelCachePubSub {
	return &memoryChannelCache{memoryAssertCore(core)}
}
func (c *memoryChannelCache) NotifyUpdate(ctx context.Context) error {
	return c.core.events.publish(ctx, "channel:updated", "")
}
func (c *memoryChannelCache) SubscribeUpdates(ctx context.Context, handler func()) {
	if handler != nil {
		_ = c.core.events.subscribe(ctx, "channel:updated", func(string) { handler() })
	}
}

type memoryLeaderLockCache struct{ core *memoryCacheCore }

func NewMemoryLeaderLockCache(core *memoryCacheCore) service.LeaderLockCache {
	return &memoryLeaderLockCache{memoryAssertCore(core)}
}
func (c *memoryLeaderLockCache) TryAcquireLeaderLock(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	c.core.mu.Lock()
	defer c.core.mu.Unlock()
	key = "leader:" + key
	if e := c.core.entries[key]; e != nil && !c.core.expiredLocked(e) {
		return false, nil
	}
	if ttl <= 0 {
		ttl = time.Millisecond
	}
	err := c.core.putLocked(key, owner, ttl, true)
	return err == nil, err
}
func (c *memoryLeaderLockCache) ReleaseLeaderLock(ctx context.Context, key, owner string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.core.mu.Lock()
	defer c.core.mu.Unlock()
	if e := c.core.entries["leader:"+key]; e != nil && e.value == owner {
		c.core.removeLocked(e)
	}
	return nil
}
