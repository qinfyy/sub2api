package repository

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// Values are serialized at the boundary so callers cannot mutate shared state.
func readMemoryJSON[T any](ctx context.Context, core *memoryCacheCore, key string) (T, bool, error) {
	var value T
	if err := ctx.Err(); err != nil {
		return value, false, err
	}
	raw, ok := core.get(key)
	if !ok {
		return value, false, nil
	}
	err := json.Unmarshal(memoryBytes(raw), &value)
	return value, err == nil, err
}

// changeMemoryJSON commits the entire read-modify-write under the core mutex.
// keep=false deletes the key. Failed callbacks/admission never expose mutations.
func changeMemoryJSON[T any](ctx context.Context, core *memoryCacheCore, key string, ttl time.Duration, protected bool, change func(T, bool) (T, bool, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	core.mu.Lock()
	defer core.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	var value T
	entry := core.entries[key]
	if entry != nil && core.expiredLocked(entry) {
		core.removeLocked(entry)
		entry = nil
	}
	if entry != nil {
		if err := json.Unmarshal(memoryBytes(entry.value), &value); err != nil {
			return err
		}
	}
	next, keep, err := change(value, entry != nil)
	if err != nil {
		return err
	}
	if !keep {
		if entry != nil {
			core.removeLocked(entry)
		}
		return nil
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if expiring, ok := any(next).(interface{ memoryExpiry() time.Time }); ok {
		ttl = expiring.memoryExpiry().Sub(core.now())
		if ttl <= 0 {
			if entry != nil {
				core.removeLocked(entry)
			}
			return nil
		}
	}
	return core.putLocked(key, raw, ttl, protected)
}

func writeMemoryJSON[T any](ctx context.Context, core *memoryCacheCore, key string, value T, ttl time.Duration) error {
	return changeMemoryJSON(ctx, core, key, ttl, false, func(_ T, _ bool) (T, bool, error) { return value, true, nil })
}

type memorySubscription struct {
	ctx  context.Context
	fn   func(string)
	stop func() bool
}

type memoryEventBus struct {
	mu       sync.Mutex
	next     uint64
	closed   bool
	channels map[string]map[uint64]*memorySubscription
}

func (b *memoryEventBus) subscribe(ctx context.Context, channel string, fn func(string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if fn == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return context.Canceled
	}
	if b.channels == nil {
		b.channels = make(map[string]map[uint64]*memorySubscription)
	}
	if b.channels[channel] == nil {
		b.channels[channel] = make(map[uint64]*memorySubscription)
	}
	b.next++
	id := b.next
	sub := &memorySubscription{ctx: ctx, fn: fn}
	b.channels[channel][id] = sub
	sub.stop = context.AfterFunc(ctx, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.channels[channel], id)
		if len(b.channels[channel]) == 0 {
			delete(b.channels, channel)
		}
	})
	return nil
}

func (b *memoryEventBus) publish(ctx context.Context, channel, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return context.Canceled
	}
	var subscribers []*memorySubscription
	for _, sub := range b.channels[channel] {
		subscribers = append(subscribers, sub)
	}
	b.mu.Unlock()
	// Never hold a cache/bus mutex while invoking subscribers.
	for _, sub := range subscribers {
		if sub.ctx.Err() == nil {
			sub.fn(value)
		}
	}
	return nil
}

func (b *memoryEventBus) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for _, channel := range b.channels {
		for _, sub := range channel {
			sub.stop()
		}
	}
	b.channels = nil
}
