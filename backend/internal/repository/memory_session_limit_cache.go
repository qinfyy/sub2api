package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type memorySessionLimitCache struct {
	core *memoryCacheCore
	idle time.Duration
}
type memorySessionEntries struct {
	Seen map[string]int64
	Idle time.Duration
}

func (v memorySessionEntries) memoryExpiry() time.Time {
	var latest int64
	for _, seen := range v.Seen {
		if seen > latest {
			latest = seen
		}
	}
	return time.Unix(0, latest).Add(v.Idle + time.Minute)
}
func NewMemorySessionLimitCache(core *memoryCacheCore, idleMinutes int) service.SessionLimitCache {
	if idleMinutes <= 0 {
		idleMinutes = 5
	}
	return &memorySessionLimitCache{memoryAssertCore(core), time.Duration(idleMinutes) * time.Minute}
}
func (c *memorySessionLimitCache) change(ctx context.Context, id int64, idle time.Duration, fn func(map[string]int64) error) error {
	if idle <= 0 {
		idle = c.idle
	}
	return changeMemoryJSON(ctx, c.core, fmt.Sprintf("%s%d", sessionLimitKeyPrefix, id), 0, true, func(v memorySessionEntries, _ bool) (memorySessionEntries, bool, error) {
		if v.Seen == nil {
			v.Seen = make(map[string]int64)
		}
		cutoff := c.core.now().Add(-idle).UnixNano()
		for k, seen := range v.Seen {
			if seen <= cutoff {
				delete(v.Seen, k)
			}
		}
		v.Idle = idle
		err := fn(v.Seen)
		return v, len(v.Seen) > 0, err
	})
}
func (c *memorySessionLimitCache) RegisterSession(ctx context.Context, id int64, session string, max int, idle time.Duration) (bool, error) {
	if max <= 0 || session == "" {
		return true, ctx.Err()
	}
	var allowed bool
	err := c.change(ctx, id, idle, func(v map[string]int64) error {
		if _, ok := v[session]; ok || len(v) < max {
			v[session] = c.core.now().UnixNano()
			allowed = true
		}
		return nil
	})
	return allowed && err == nil, err
}
func (c *memorySessionLimitCache) RefreshSession(ctx context.Context, id int64, session string, idle time.Duration) error {
	return c.change(ctx, id, idle, func(v map[string]int64) error {
		if _, ok := v[session]; ok {
			v[session] = c.core.now().UnixNano()
		}
		return nil
	})
}
func (c *memorySessionLimitCache) UnregisterSession(ctx context.Context, id int64, session string) error {
	return c.change(ctx, id, 0, func(v map[string]int64) error { delete(v, session); return nil })
}
func (c *memorySessionLimitCache) GetActiveSessionCount(ctx context.Context, id int64) (int, error) {
	var n int
	err := c.change(ctx, id, 0, func(v map[string]int64) error { n = len(v); return nil })
	return n, err
}
func (c *memorySessionLimitCache) GetActiveSessionCountBatch(ctx context.Context, ids []int64, idles map[int64]time.Duration) (map[int64]int, error) {
	out := make(map[int64]int, len(ids))
	for _, id := range ids {
		var n int
		err := c.change(ctx, id, idles[id], func(v map[string]int64) error { n = len(v); return nil })
		if err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, nil
}
func (c *memorySessionLimitCache) IsSessionActive(ctx context.Context, id int64, session string) (bool, error) {
	var active bool
	err := c.change(ctx, id, 0, func(v map[string]int64) error { _, active = v[session]; return nil })
	return active, err
}
func (c *memorySessionLimitCache) GetWindowCost(ctx context.Context, id int64) (float64, bool, error) {
	return readMemoryJSON[float64](ctx, c.core, fmt.Sprintf("%s%d", windowCostKeyPrefix, id))
}
func (c *memorySessionLimitCache) SetWindowCost(ctx context.Context, id int64, cost float64) error {
	return writeMemoryJSON(ctx, c.core, fmt.Sprintf("%s%d", windowCostKeyPrefix, id), cost, windowCostCacheTTL)
}
func (c *memorySessionLimitCache) GetWindowCostBatch(ctx context.Context, ids []int64) (map[int64]float64, error) {
	out := make(map[int64]float64, len(ids))
	for _, id := range ids {
		v, hit, err := c.GetWindowCost(ctx, id)
		if err != nil {
			return nil, err
		}
		if hit {
			out[id] = v
		}
	}
	return out, nil
}
