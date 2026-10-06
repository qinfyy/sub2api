package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type memoryMembers map[string]int64

func (v memoryMembers) memoryExpiry() time.Time {
	var latest int64
	for _, expires := range v {
		if expires > latest {
			latest = expires
		}
	}
	return time.Unix(0, latest)
}
func (v memoryMembers) prune(now time.Time) {
	for id, expires := range v {
		if expires <= now.UnixNano() {
			delete(v, id)
		}
	}
}

func changeMemoryMembers(ctx context.Context, core *memoryCacheCore, key string, change func(memoryMembers) error) error {
	return changeMemoryJSON(ctx, core, key, 0, true, func(v memoryMembers, _ bool) (memoryMembers, bool, error) {
		if v == nil {
			v = make(memoryMembers)
		}
		v.prune(core.now())
		err := change(v)
		return v, len(v) > 0, err
	})
}

type memoryConcurrencyCache struct {
	core             *memoryCacheCore
	slotTTL, waitTTL time.Duration
}

func NewMemoryConcurrencyCache(core *memoryCacheCore, slotMinutes, waitSeconds int) service.ConcurrencyCache {
	if slotMinutes <= 0 {
		slotMinutes = defaultSlotTTLMinutes
	}
	if waitSeconds <= 0 {
		waitSeconds = slotMinutes * 60
	}
	return &memoryConcurrencyCache{memoryAssertCore(core), time.Duration(slotMinutes) * time.Minute, time.Duration(waitSeconds) * time.Second}
}

var (
	_ service.ConcurrencyCache          = (*memoryConcurrencyCache)(nil)
	_ service.APIKeyConcurrencyCache    = (*memoryConcurrencyCache)(nil)
	_ service.OpenAIWSIngressLeaseCache = (*memoryConcurrencyCache)(nil)
)

func (c *memoryConcurrencyCache) acquire(ctx context.Context, key, requestID string, limit int, ttl time.Duration, liveKind string, id int64) (bool, error) {
	var allowed bool
	err := changeMemoryMembers(ctx, c.core, key, func(v memoryMembers) error {
		if _, ok := v[requestID]; ok {
			allowed = true
			return nil
		}
		if limit > 0 && len(v)+c.liveCountLocked(liveKind, id) >= limit {
			return nil
		}
		v[requestID] = c.core.now().Add(ttl).UnixNano()
		allowed = true
		return nil
	})
	return allowed && err == nil, err
}
func (c *memoryConcurrencyCache) release(ctx context.Context, key, id string) error {
	return changeMemoryMembers(ctx, c.core, key, func(v memoryMembers) error { delete(v, id); return nil })
}
func (c *memoryConcurrencyCache) count(ctx context.Context, key, kind string, id int64) (int, error) {
	var count int
	err := changeMemoryMembers(ctx, c.core, key, func(v memoryMembers) error { count = len(v) + c.liveCountLocked(kind, id); return nil })
	return count, err
}
func (c *memoryConcurrencyCache) AcquireAccountSlot(ctx context.Context, id int64, max int, req string) (bool, error) {
	return c.acquire(ctx, accountSlotKey(id), req, max, c.slotTTL, "account", id)
}
func (c *memoryConcurrencyCache) ReleaseAccountSlot(ctx context.Context, id int64, req string) error {
	return c.release(ctx, accountSlotKey(id), req)
}
func (c *memoryConcurrencyCache) GetAccountConcurrency(ctx context.Context, id int64) (int, error) {
	return c.count(ctx, accountSlotKey(id), "account", id)
}
func (c *memoryConcurrencyCache) GetAccountConcurrencyBatch(ctx context.Context, ids []int64) (map[int64]int, error) {
	v := make(map[int64]int, len(ids))
	for _, id := range ids {
		n, err := c.GetAccountConcurrency(ctx, id)
		if err != nil {
			return nil, err
		}
		v[id] = n
	}
	return v, nil
}
func (c *memoryConcurrencyCache) AcquireUserSlot(ctx context.Context, id int64, max int, req string) (bool, error) {
	return c.acquire(ctx, userSlotKey(id), req, max, c.slotTTL, "user", id)
}
func (c *memoryConcurrencyCache) ReleaseUserSlot(ctx context.Context, id int64, req string) error {
	return c.release(ctx, userSlotKey(id), req)
}
func (c *memoryConcurrencyCache) GetUserConcurrency(ctx context.Context, id int64) (int, error) {
	return c.count(ctx, userSlotKey(id), "user", id)
}
func (c *memoryConcurrencyCache) wait(ctx context.Context, key string, max int, increment bool) (bool, error) {
	var allowed bool
	err := changeMemoryJSON(ctx, c.core, key, c.waitTTL, true, func(v int, hit bool) (int, bool, error) {
		if increment {
			if v >= max {
				return v, hit, nil
			}
			v++
			allowed = true
		} else if v > 0 {
			v--
		}
		return v, v > 0, nil
	})
	return allowed && err == nil, err
}
func (c *memoryConcurrencyCache) IncrementWaitCount(ctx context.Context, id int64, max int) (bool, error) {
	return c.wait(ctx, waitQueueKey(id), max, true)
}
func (c *memoryConcurrencyCache) DecrementWaitCount(ctx context.Context, id int64) error {
	_, err := c.wait(ctx, waitQueueKey(id), 0, false)
	return err
}
func (c *memoryConcurrencyCache) IncrementAccountWaitCount(ctx context.Context, id int64, max int) (bool, error) {
	return c.wait(ctx, accountWaitKey(id), max, true)
}
func (c *memoryConcurrencyCache) DecrementAccountWaitCount(ctx context.Context, id int64) error {
	_, err := c.wait(ctx, accountWaitKey(id), 0, false)
	return err
}
func (c *memoryConcurrencyCache) GetAccountWaitingCount(ctx context.Context, id int64) (int, error) {
	v, _, err := readMemoryJSON[int](ctx, c.core, accountWaitKey(id))
	return v, err
}
func (c *memoryConcurrencyCache) GetAccountsLoadBatch(ctx context.Context, accounts []service.AccountWithConcurrency) (map[int64]*service.AccountLoadInfo, error) {
	out := make(map[int64]*service.AccountLoadInfo, len(accounts))
	for _, a := range accounts {
		n, err := c.GetAccountConcurrency(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		w, err := c.GetAccountWaitingCount(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		load := 0
		if a.MaxConcurrency > 0 {
			load = (n + w) * 100 / a.MaxConcurrency
		}
		out[a.ID] = &service.AccountLoadInfo{AccountID: a.ID, CurrentConcurrency: n, WaitingCount: w, LoadRate: load}
	}
	return out, nil
}
func (c *memoryConcurrencyCache) GetUsersLoadBatch(ctx context.Context, users []service.UserWithConcurrency) (map[int64]*service.UserLoadInfo, error) {
	out := make(map[int64]*service.UserLoadInfo, len(users))
	for _, u := range users {
		n, err := c.GetUserConcurrency(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		w, _, err := readMemoryJSON[int](ctx, c.core, waitQueueKey(u.ID))
		if err != nil {
			return nil, err
		}
		load := 0
		if u.MaxConcurrency > 0 {
			load = (n + w) * 100 / u.MaxConcurrency
		}
		out[u.ID] = &service.UserLoadInfo{UserID: u.ID, CurrentConcurrency: n, WaitingCount: w, LoadRate: load}
	}
	return out, nil
}
func (c *memoryConcurrencyCache) CleanupExpiredAccountSlots(ctx context.Context, id int64) error {
	_, err := c.GetAccountConcurrency(ctx, id)
	return err
}
func (c *memoryConcurrencyCache) CleanupExpiredAccountSlotKeys(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.core.gc()
	return nil
}
func (c *memoryConcurrencyCache) CleanupStaleProcessSlots(ctx context.Context, prefix string) error {
	if prefix == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.core.mu.Lock()
	var keys []string
	for key := range c.core.entries {
		if strings.HasPrefix(key, accountSlotKeyPrefix) || strings.HasPrefix(key, userSlotKeyPrefix) {
			keys = append(keys, key)
		}
	}
	c.core.mu.Unlock()
	for _, key := range keys {
		if err := changeMemoryMembers(ctx, c.core, key, func(v memoryMembers) error {
			for id := range v {
				if !strings.HasPrefix(id, prefix) {
					delete(v, id)
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}
func (c *memoryConcurrencyCache) TrackAPIKeySlot(ctx context.Context, id int64, req string) error {
	_, err := c.acquire(ctx, apiKeySlotKey(id), req, 0, c.slotTTL, "", 0)
	return err
}
func (c *memoryConcurrencyCache) ReleaseAPIKeySlot(ctx context.Context, id int64, req string) error {
	return c.release(ctx, apiKeySlotKey(id), req)
}
func (c *memoryConcurrencyCache) GetAPIKeyConcurrencyBatch(ctx context.Context, ids []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(ids))
	for _, id := range ids {
		n, err := c.count(ctx, apiKeySlotKey(id), "api_key", id)
		if err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, nil
}
func (c *memoryConcurrencyCache) AcquireOpenAIWSIngressLease(ctx context.Context, id int64, max int, req string) (bool, error) {
	return c.acquire(ctx, fmt.Sprintf("%s%d", openAIWSIngressLeaseKeyPrefix, id), req, max, 60*time.Second, "", 0)
}
func (c *memoryConcurrencyCache) RefreshOpenAIWSIngressLease(ctx context.Context, id int64, req string) (bool, error) {
	var found bool
	err := changeMemoryMembers(ctx, c.core, fmt.Sprintf("%s%d", openAIWSIngressLeaseKeyPrefix, id), func(v memoryMembers) error {
		if _, ok := v[req]; ok {
			v[req] = c.core.now().Add(60 * time.Second).UnixNano()
			found = true
		}
		return nil
	})
	return found && err == nil, err
}
func (c *memoryConcurrencyCache) ReleaseOpenAIWSIngressLease(ctx context.Context, id int64, req string) error {
	return c.release(ctx, fmt.Sprintf("%s%d", openAIWSIngressLeaseKeyPrefix, id), req)
}

type memoryLiveLease struct{ AccountID, UserID, APIKeyID int64 }

const memoryLivePrefix = "memory:live:"

func (c *memoryConcurrencyCache) liveCountLocked(kind string, id int64) int {
	if kind == "" {
		return 0
	}
	n := 0
	for key, e := range c.core.entries {
		if !strings.HasPrefix(key, memoryLivePrefix) || c.core.expiredLocked(e) {
			continue
		}
		v := e.value.(memoryLiveLease)
		if kind == "account" && v.AccountID == id || kind == "user" && v.UserID == id || kind == "api_key" && v.APIKeyID == id {
			n++
		}
	}
	return n
}
func (c *memoryConcurrencyCache) regularCountLocked(key string) int {
	e := c.core.entries[key]
	if e == nil || c.core.expiredLocked(e) {
		return 0
	}
	var v memoryMembers
	if json.Unmarshal(memoryBytes(e.value), &v) != nil {
		return 0
	}
	v.prune(c.core.now())
	return len(v)
}
func (c *memoryConcurrencyCache) AcquireLiveLease(ctx context.Context, accountID int64, accountMax int, userID int64, userMax int, apiKeyID int64, leaseID string, replacing bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if accountID <= 0 || userID <= 0 || apiKeyID <= 0 || leaseID == "" {
		return false, nil
	}
	c.core.mu.Lock()
	defer c.core.mu.Unlock()
	key := memoryLivePrefix + leaseID
	wanted := memoryLiveLease{accountID, userID, apiKeyID}
	if e := c.core.entries[key]; e != nil && !c.core.expiredLocked(e) {
		return e.value == wanted, nil
	}
	a := c.regularCountLocked(accountSlotKey(accountID)) + c.liveCountLocked("account", accountID)
	u := c.regularCountLocked(userSlotKey(userID)) + c.liveCountLocked("user", userID)
	if replacing {
		if a > 0 {
			a--
		}
		if u > 0 {
			u--
		}
	}
	if accountMax > 0 && a >= accountMax || userMax > 0 && u >= userMax {
		return false, nil
	}
	err := c.core.putLocked(key, wanted, time.Duration(liveLeaseTTLSeconds)*time.Second, true)
	return err == nil, err
}
func (c *memoryConcurrencyCache) RefreshLiveLease(ctx context.Context, accountID, userID, apiKeyID int64, leaseID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	c.core.mu.Lock()
	defer c.core.mu.Unlock()
	key := memoryLivePrefix + leaseID
	e := c.core.entries[key]
	if e == nil || c.core.expiredLocked(e) || e.value != (memoryLiveLease{accountID, userID, apiKeyID}) {
		return false, nil
	}
	e.expiry = c.core.now().Add(time.Duration(liveLeaseTTLSeconds) * time.Second)
	return true, nil
}
func (c *memoryConcurrencyCache) ReleaseLiveLease(ctx context.Context, accountID, userID, apiKeyID int64, leaseID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.core.mu.Lock()
	defer c.core.mu.Unlock()
	e := c.core.entries[memoryLivePrefix+leaseID]
	if e != nil && e.value == (memoryLiveLease{accountID, userID, apiKeyID}) {
		c.core.removeLocked(e)
	}
	return nil
}
