package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type memoryBillingCache struct {
	core  *memoryCacheCore
	state memoryKVBackend
}

func NewMemoryBillingCache(core *memoryCacheCore, db *sql.DB) service.BillingCache {
	core = memoryAssertCore(core)
	c := &memoryBillingCache{core: core, state: memoryKVBackend{core: core}}
	if db != nil {
		c.state.db = db
	}
	return c
}

var (
	_ service.BillingCache                      = (*memoryBillingCache)(nil)
	_ service.InflightBalanceReservationCache   = (*memoryBillingCache)(nil)
	_ service.InflightBalanceReservationRenewer = (*memoryBillingCache)(nil)
)

func (c *memoryBillingCache) GetUserBalance(ctx context.Context, id int64) (float64, error) {
	v, hit, err := readMemoryJSON[float64](ctx, c.core, billingBalanceKey(id))
	if err == nil && !hit {
		err = errMemoryCacheMiss
	}
	return v, err
}
func (c *memoryBillingCache) SetUserBalance(ctx context.Context, id int64, value float64) error {
	return writeMemoryJSON(ctx, c.core, billingBalanceKey(id), value, jitteredTTL())
}
func (c *memoryBillingCache) DeductUserBalance(ctx context.Context, id int64, amount float64) error {
	return changeMemoryJSON(ctx, c.core, billingBalanceKey(id), jitteredTTL(), false, func(v float64, hit bool) (float64, bool, error) { return v - amount, hit, nil })
}
func (c *memoryBillingCache) remove(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.core.delete(key)
	return nil
}
func (c *memoryBillingCache) InvalidateUserBalance(ctx context.Context, id int64) error {
	return c.remove(ctx, billingBalanceKey(id))
}
func (c *memoryBillingCache) GetSubscriptionCache(ctx context.Context, userID, groupID int64) (*service.SubscriptionCacheData, error) {
	v, hit, err := readMemoryJSON[service.SubscriptionCacheData](ctx, c.core, billingSubKey(userID, groupID))
	if err != nil {
		return nil, err
	}
	if !hit {
		return nil, errMemoryCacheMiss
	}
	return &v, nil
}
func (c *memoryBillingCache) SetSubscriptionCache(ctx context.Context, userID, groupID int64, value *service.SubscriptionCacheData) error {
	if value == nil {
		return nil
	}
	return writeMemoryJSON(ctx, c.core, billingSubKey(userID, groupID), *value, jitteredTTL())
}
func (c *memoryBillingCache) UpdateSubscriptionUsage(ctx context.Context, userID, groupID int64, cost float64) error {
	return changeMemoryJSON(ctx, c.core, billingSubKey(userID, groupID), jitteredTTL(), false, func(v service.SubscriptionCacheData, hit bool) (service.SubscriptionCacheData, bool, error) {
		v.DailyUsage += cost
		v.WeeklyUsage += cost
		v.MonthlyUsage += cost
		return v, hit, nil
	})
}
func (c *memoryBillingCache) InvalidateSubscriptionCache(ctx context.Context, userID, groupID int64) error {
	return c.remove(ctx, billingSubKey(userID, groupID))
}
func (c *memoryBillingCache) PublishSubscriptionCacheInvalidation(ctx context.Context, key string) error {
	return c.core.events.publish(ctx, subCacheInvalidateChannel, key)
}
func (c *memoryBillingCache) SubscribeSubscriptionCacheInvalidation(ctx context.Context, fn func(string)) error {
	return c.core.events.subscribe(ctx, subCacheInvalidateChannel, fn)
}
func (c *memoryBillingCache) GetAPIKeyRateLimit(ctx context.Context, id int64) (*service.APIKeyRateLimitCacheData, error) {
	v, hit, err := readMemoryJSON[service.APIKeyRateLimitCacheData](ctx, c.core, billingRateLimitKey(id))
	if err != nil {
		return nil, err
	}
	if !hit {
		return nil, errMemoryCacheMiss
	}
	return &v, nil
}
func (c *memoryBillingCache) SetAPIKeyRateLimit(ctx context.Context, id int64, value *service.APIKeyRateLimitCacheData) error {
	if value == nil {
		return nil
	}
	return writeMemoryJSON(ctx, c.core, billingRateLimitKey(id), *value, rateLimitCacheTTL)
}
func (c *memoryBillingCache) UpdateAPIKeyRateLimitUsage(ctx context.Context, id int64, cost float64) error {
	return changeMemoryJSON(ctx, c.core, billingRateLimitKey(id), rateLimitCacheTTL, false, func(v service.APIKeyRateLimitCacheData, hit bool) (service.APIKeyRateLimitCacheData, bool, error) {
		now := c.core.now().Unix()
		for _, w := range []struct {
			usage    *float64
			start    *int64
			duration time.Duration
		}{{&v.Usage5h, &v.Window5h, rateLimitWindow5h}, {&v.Usage1d, &v.Window1d, rateLimitWindow1d}, {&v.Usage7d, &v.Window7d, rateLimitWindow7d}} {
			if *w.start == 0 || now-*w.start >= int64(w.duration/time.Second) {
				*w.usage = cost
				*w.start = now
			} else {
				*w.usage += cost
			}
		}
		return v, hit, nil
	})
}
func (c *memoryBillingCache) InvalidateAPIKeyRateLimit(ctx context.Context, id int64) error {
	return c.remove(ctx, billingRateLimitKey(id))
}

type memoryInflightEntry struct {
	Amount  float64
	Expires time.Time
}
type memoryInflightEntries map[string]memoryInflightEntry

func (v memoryInflightEntries) memoryExpiry() time.Time {
	var latest time.Time
	for _, e := range v {
		if e.Expires.After(latest) {
			latest = e.Expires
		}
	}
	return latest
}

func (c *memoryBillingCache) changeInflight(ctx context.Context, userID int64, change func(memoryInflightEntries) error) error {
	key, _ := billingInflightKeys(userID)
	return changeMemoryJSON(ctx, c.core, key, 0, true, func(v memoryInflightEntries, _ bool) (memoryInflightEntries, bool, error) {
		if v == nil {
			v = make(memoryInflightEntries)
		}
		for id, e := range v {
			if !c.core.now().Before(e.Expires) {
				delete(v, id)
			}
		}
		if err := change(v); err != nil {
			return v, false, err
		}
		return v, len(v) > 0, nil
	})
}
func (c *memoryBillingCache) ReserveInflightBalance(ctx context.Context, userID int64, requestID string, amount, balance float64, ttl time.Duration) (bool, float64, error) {
	if requestID == "" || math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 || math.IsNaN(balance) || math.IsInf(balance, 0) {
		return false, 0, fmt.Errorf("invalid balance reservation")
	}
	if ttl <= 0 {
		ttl = time.Millisecond
	}
	var allowed bool
	var total float64
	err := c.changeInflight(ctx, userID, func(v memoryInflightEntries) error {
		for _, e := range v {
			total += e.Amount
		}
		if existing, ok := v[requestID]; ok {
			if existing.Amount != amount {
				return fmt.Errorf("reservation amount changed for request %s", requestID)
			}
			allowed = true
			return nil
		}
		if len(v) > 0 && balance-total < amount {
			return nil
		}
		v[requestID] = memoryInflightEntry{Amount: amount, Expires: c.core.now().Add(ttl)}
		allowed = true
		return nil
	})
	return allowed && err == nil, total, err
}
func (c *memoryBillingCache) ReleaseInflightBalance(ctx context.Context, userID int64, requestID string) error {
	return c.changeInflight(ctx, userID, func(v memoryInflightEntries) error { delete(v, requestID); return nil })
}
func (c *memoryBillingCache) RenewInflightBalance(ctx context.Context, userID int64, requestID string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		ttl = time.Millisecond
	}
	var found bool
	err := c.changeInflight(ctx, userID, func(v memoryInflightEntries) error {
		if e, ok := v[requestID]; ok {
			e.Expires = c.core.now().Add(ttl)
			v[requestID] = e
			found = true
		}
		return nil
	})
	return found && err == nil, err
}

// Quota state is durable because optional asynchronous flushing makes it the
// source of truth between flushes. Claims expire, so a crash cannot lose work.
type memoryQuotaRecord struct {
	Entry        service.UserPlatformQuotaCacheEntry
	Dirty        bool
	ClaimedUntil int64
	ExpiresAt    int64
}

func (c *memoryBillingCache) quota(ctx context.Context, key string) (memoryQuotaRecord, bool, error) {
	raw, err := c.state.get(ctx, key)
	if errors.Is(err, errMemoryCacheMiss) {
		return memoryQuotaRecord{}, false, nil
	}
	if err != nil {
		return memoryQuotaRecord{}, false, err
	}
	var v memoryQuotaRecord
	err = json.Unmarshal(raw, &v)
	return v, err == nil, err
}
func (c *memoryBillingCache) saveQuota(ctx context.Context, key string, v memoryQuotaRecord) error {
	ttl := time.Duration(0)
	if v.ExpiresAt > 0 {
		ttl = time.Until(time.UnixMilli(v.ExpiresAt))
		if ttl <= 0 {
			return c.state.remove(ctx, key)
		}
	}
	return c.state.set(ctx, key, v, ttl)
}
func (c *memoryBillingCache) GetUserPlatformQuotaCache(ctx context.Context, userID int64, platform string) (*service.UserPlatformQuotaCacheEntry, bool, error) {
	v, hit, err := c.quota(ctx, userPlatformQuotaCacheKey(userID, platform))
	if err != nil || !hit {
		return nil, false, err
	}
	return &v.Entry, true, nil
}
func (c *memoryBillingCache) SetUserPlatformQuotaCache(ctx context.Context, userID int64, platform string, value *service.UserPlatformQuotaCacheEntry, ttl time.Duration) error {
	if value == nil {
		return nil
	}
	key := userPlatformQuotaCacheKey(userID, platform)
	mu := memoryLockKey(key)
	defer memoryUnlock(mu)
	v := memoryQuotaRecord{Entry: *value}
	if ttl > 0 {
		v.ExpiresAt = time.Now().Add(ttl).UnixMilli()
	}
	return c.saveQuota(ctx, key, v)
}
func (c *memoryBillingCache) DeleteUserPlatformQuotaCache(ctx context.Context, userID int64, platform string) error {
	key := userPlatformQuotaCacheKey(userID, platform)
	mu := memoryLockKey(key)
	defer memoryUnlock(mu)
	return c.state.remove(ctx, key)
}
func (c *memoryBillingCache) IncrUserPlatformQuotaUsageCache(ctx context.Context, userID int64, platform string, cost float64, ttl time.Duration, markDirty bool) error {
	key := userPlatformQuotaCacheKey(userID, platform)
	mu := memoryLockKey(key)
	defer memoryUnlock(mu)
	v, hit, err := c.quota(ctx, key)
	if err != nil {
		return err
	}
	if !hit || v.Entry.SchemaVersion != service.UserPlatformQuotaCacheSchemaV1 {
		return nil
	}
	v.Entry.DailyUsageUSD += cost
	v.Entry.WeeklyUsageUSD += cost
	v.Entry.MonthlyUsageUSD += cost
	v.Entry.Version++
	v.Dirty = v.Dirty || markDirty
	v.ExpiresAt = 0
	if ttl > 0 {
		v.ExpiresAt = time.Now().Add(ttl).UnixMilli()
	}
	return c.saveQuota(ctx, key, v)
}
func (c *memoryBillingCache) PopDirtyUserPlatformQuotaKeys(ctx context.Context, n int) ([]service.UserPlatformQuotaKey, error) {
	if n <= 0 {
		return nil, nil
	}
	const prefix = "billing:user_platform_quota:"
	var candidates []string
	if c.state.db != nil {
		rows, err := c.state.db.QueryContext(ctx, `SELECT key FROM kv_store WHERE key >= ? AND key < ? AND (expires_at IS NULL OR expires_at > ?) AND json_extract(CAST(value AS TEXT), '$.Dirty') = 1 AND COALESCE(json_extract(CAST(value AS TEXT), '$.ClaimedUntil'), 0) <= ? ORDER BY key LIMIT ?`, prefix, prefix+"\uffff", time.Now().UnixMilli(), time.Now().UnixMilli(), n)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				_ = rows.Close()
				return nil, err
			}
			candidates = append(candidates, key)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
	} else {
		c.core.mu.Lock()
		for key := range c.core.entries {
			if strings.HasPrefix(key, prefix) {
				candidates = append(candidates, key)
			}
		}
		c.core.mu.Unlock()
	}
	var result []service.UserPlatformQuotaKey
	for _, key := range candidates {
		k, ok := parseUserPlatformQuotaDirtyMember(strings.TrimPrefix(key, prefix))
		if !ok {
			continue
		}
		mu := memoryLockKey(key)
		v, hit, err := c.quota(ctx, key)
		if err == nil && hit && v.Dirty && v.ClaimedUntil <= time.Now().UnixMilli() {
			v.ClaimedUntil = time.Now().Add(30 * time.Second).UnixMilli()
			err = c.saveQuota(ctx, key, v)
			if err == nil {
				result = append(result, k)
			}
		}
		memoryUnlock(mu)
		if err != nil {
			return result, err
		}
		if len(result) == n {
			break
		}
	}
	return result, nil
}
func (c *memoryBillingCache) ReaddDirtyUserPlatformQuotaKeys(ctx context.Context, keys []service.UserPlatformQuotaKey) error {
	for _, k := range keys {
		key := userPlatformQuotaCacheKey(k.UserID, k.Platform)
		mu := memoryLockKey(key)
		v, hit, err := c.quota(ctx, key)
		if err == nil && hit {
			v.Dirty = true
			v.ClaimedUntil = 0
			err = c.saveQuota(ctx, key, v)
		}
		memoryUnlock(mu)
		if err != nil {
			return err
		}
	}
	return nil
}
func (c *memoryBillingCache) BatchGetUserPlatformQuotaCache(ctx context.Context, keys []service.UserPlatformQuotaKey) ([]*service.UserPlatformQuotaCacheEntry, error) {
	values := make([]*service.UserPlatformQuotaCacheEntry, len(keys))
	for i, k := range keys {
		v, _, err := c.GetUserPlatformQuotaCache(ctx, k.UserID, k.Platform)
		if err != nil {
			return nil, err
		}
		values[i] = v
	}
	return values, nil
}
func (c *memoryBillingCache) AckDirtyUserPlatformQuotaKeys(ctx context.Context, keys []service.UserPlatformQuotaKey, entries []*service.UserPlatformQuotaCacheEntry) error {
	for i, k := range keys {
		if i >= len(entries) || entries[i] == nil {
			continue
		}
		key := userPlatformQuotaCacheKey(k.UserID, k.Platform)
		mu := memoryLockKey(key)
		v, hit, err := c.quota(ctx, key)
		if err == nil && hit && v.Entry.Version == entries[i].Version {
			v.Dirty = false
			v.ClaimedUntil = 0
			err = c.saveQuota(ctx, key, v)
		}
		memoryUnlock(mu)
		if err != nil {
			return err
		}
	}
	return nil
}
