package repository

import (
	"container/list"
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"
)

// memoryKVStore is the durable write-through dependency for the memory cache
// backend. *sql.DB satisfies this interface directly.
type memoryKVStore interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// memoryCacheEntry is one value in the in-process cache. expiry is zero for
// entries that never expire.
type memoryCacheEntry struct {
	key       string
	value     any
	expiry    time.Time
	element   *list.Element
	cost      int64
	protected bool
}

// memoryCacheCore is a bounded TTL cache with LRU eviction and prefix scans.
// It intentionally does not persist values: the durable subset of state is
// written through memoryCacheStore, while pure caches stay in memory.
type memoryCacheCore struct {
	mu         sync.Mutex
	maxEntries int
	maxBytes   int64
	usedBytes  int64
	entries    map[string]*memoryCacheEntry
	lru        *list.List
	now        func() time.Time
	events     memoryEventBus
}

func newMemoryCacheCore(maxEntries int) *memoryCacheCore {
	if maxEntries <= 0 {
		maxEntries = 100000
	}
	return &memoryCacheCore{
		maxEntries: maxEntries,
		entries:    make(map[string]*memoryCacheEntry),
		lru:        list.New(),
		now:        time.Now,
	}
}

func (c *memoryCacheCore) set(key string, value any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setLocked(key, value, ttl)
}

var errMemoryCapacity = errors.New("memory cache capacity exhausted")

func (c *memoryCacheCore) increment(key string, ttl time.Duration, refreshTTL bool) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[key]; ok {
		if !c.expiredLocked(entry) {
			count := memoryInt64(entry.value) + 1
			entry.value = count
			if refreshTTL {
				entry.expiry = time.Time{}
				if ttl > 0 {
					entry.expiry = c.now().Add(ttl)
				}
			}
			c.lru.MoveToFront(entry.element)
			return count, nil
		}
		c.removeLocked(entry)
	}
	if err := c.putLocked(key, int64(1), ttl, true); err != nil {
		return 0, err
	}
	return 1, nil
}

func (c *memoryCacheCore) setLocked(key string, value any, ttl time.Duration) {
	_ = c.putLocked(key, value, ttl, false)
}

func (c *memoryCacheCore) putLocked(key string, value any, ttl time.Duration, protected bool) error {
	cost := int64(len(key) + len(memoryBytes(value)) + 160)
	existing := c.entries[key]
	if existing != nil && c.expiredLocked(existing) {
		c.removeLocked(existing)
		existing = nil
	}
	delta, extra := cost, 1
	if existing != nil {
		delta -= existing.cost
		extra = 0
		protected = protected || existing.protected
	}
	if c.maxBytes > 0 && cost > c.maxBytes {
		return errMemoryCapacity
	}
	for len(c.entries)+extra > c.maxEntries || c.maxBytes > 0 && c.usedBytes+delta > c.maxBytes {
		var victim *memoryCacheEntry
		for e := c.lru.Back(); e != nil; e = e.Prev() {
			candidate := e.Value.(*memoryCacheEntry)
			if candidate != existing && (!candidate.protected || c.expiredLocked(candidate)) {
				victim = candidate
				break
			}
		}
		if victim == nil {
			return errMemoryCapacity
		}
		c.removeLocked(victim)
	}
	var expiry time.Time
	if ttl > 0 {
		expiry = c.now().Add(ttl)
	}
	if existing != nil {
		existing.value = value
		existing.expiry = expiry
		existing.cost, existing.protected = cost, protected
		c.usedBytes += delta
		c.lru.MoveToFront(existing.element)
		return nil
	}
	entry := &memoryCacheEntry{key: key, value: value, expiry: expiry, cost: cost, protected: protected}
	entry.element = c.lru.PushFront(entry)
	c.entries[key] = entry
	c.usedBytes += cost
	return nil
}

func (c *memoryCacheCore) evictLocked() {
	for c.lru.Len() > c.maxEntries {
		back := c.lru.Back()
		if back == nil {
			return
		}
		entry, _ := back.Value.(*memoryCacheEntry)
		if entry != nil {
			if entry.protected {
				return
			}
			c.removeLocked(entry)
		}
	}
}

func (c *memoryCacheCore) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if c.expiredLocked(entry) {
		c.removeLocked(entry)
		return nil, false
	}
	c.lru.MoveToFront(entry.element)
	return entry.value, true
}

func (c *memoryCacheCore) expiredLocked(entry *memoryCacheEntry) bool {
	return !entry.expiry.IsZero() && !c.now().Before(entry.expiry)
}

func (c *memoryCacheCore) removeLocked(entry *memoryCacheEntry) {
	c.lru.Remove(entry.element)
	delete(c.entries, entry.key)
	c.usedBytes -= entry.cost
}

func (c *memoryCacheCore) delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[key]; ok {
		c.removeLocked(entry)
	}
}

func (c *memoryCacheCore) scan(prefix string, limit int) []any {
	if limit <= 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	values := make([]any, 0, limit)
	for element := c.lru.Front(); element != nil && len(values) < limit; element = element.Next() {
		entry, _ := element.Value.(*memoryCacheEntry)
		if entry == nil || len(prefix) > 0 && !hasPrefix(entry.key, prefix) {
			continue
		}
		if c.expiredLocked(entry) {
			continue
		}
		values = append(values, entry.value)
	}
	return values
}

func (c *memoryCacheCore) gc() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := 0
	for element := c.lru.Front(); element != nil; {
		next := element.Next()
		entry, _ := element.Value.(*memoryCacheEntry)
		if entry != nil && c.expiredLocked(entry) {
			c.removeLocked(entry)
			removed++
		}
		element = next
	}
	return removed
}

// expireAt replaces the TTL of an existing key. It returns false when the key
// is missing or already expired.
func (c *memoryCacheCore) expireAt(key string, expiry time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || c.expiredLocked(entry) {
		return false
	}
	entry.expiry = expiry
	return true
}

// touchLocked refreshes the TTL of an existing key without changing the value.
func (c *memoryCacheCore) touch(key string, ttl time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || c.expiredLocked(entry) {
		return false
	}
	if ttl > 0 {
		entry.expiry = c.now().Add(ttl)
	} else {
		entry.expiry = time.Time{}
	}
	return true
}

func (c *memoryCacheCore) count(prefix string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	total := 0
	for element := c.lru.Front(); element != nil; element = element.Next() {
		entry, _ := element.Value.(*memoryCacheEntry)
		if entry == nil || c.expiredLocked(entry) || len(prefix) > 0 && !hasPrefix(entry.key, prefix) {
			continue
		}
		total++
	}
	return total
}

// ttlLocked returns the remaining lifetime of key. The bool reports whether
// the key exists; a negative duration means the entry never expires.
func (c *memoryCacheCore) ttl(key string) (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return 0, false
	}
	if c.expiredLocked(entry) {
		c.removeLocked(entry)
		return 0, false
	}
	c.lru.MoveToFront(entry.element)
	if entry.expiry.IsZero() {
		return -1, true
	}
	remaining := entry.expiry.Sub(c.now())
	if remaining <= 0 {
		return 0, true
	}
	return remaining, true
}

func hasPrefix(value, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}

// memoryCacheStore is the durable write-through layer used by the memory cache
// backend for state that must survive a restart (refresh tokens, plugin KV,
// one-shot auth sessions). It is backed by the kv_store table created for the
// SQLite backend; when nil the state stays in memory only.
type memoryCacheStore struct {
	db  memoryKVStore
	ttl time.Duration
}
