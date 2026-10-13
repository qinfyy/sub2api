package securityaudit

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/redis/go-redis/v9"
)

// ConfigInvalidationBus publishes config version changes to other instances.
// Memory deployments use the in-process implementation; Redis deployments use
// pub/sub so every replica converges without waiting for the TTL refresh loop.
type ConfigInvalidationBus interface {
	Publish(ctx context.Context, version int64) error
	Subscribe(ctx context.Context, handler func(int64)) (cancel func(), err error)
}

type redisConfigInvalidationBus struct {
	client *redis.Client
}

// NewRedisConfigInvalidationBus returns a Redis pub/sub backed invalidation bus.
func NewRedisConfigInvalidationBus(client *redis.Client) ConfigInvalidationBus {
	if client == nil {
		return nil
	}
	return &redisConfigInvalidationBus{client: client}
}

func (b *redisConfigInvalidationBus) Publish(ctx context.Context, version int64) error {
	if b == nil || b.client == nil {
		return nil
	}
	return b.client.Publish(ctx, ConfigInvalidationChannel, strconv.FormatInt(version, 10)).Err()
}

func (b *redisConfigInvalidationBus) Subscribe(ctx context.Context, handler func(int64)) (func(), error) {
	if b == nil || b.client == nil || handler == nil {
		return func() {}, nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	pubsub := b.client.Subscribe(runCtx, ConfigInvalidationChannel)
	channel := pubsub.Channel()
	go func() {
		for {
			select {
			case <-runCtx.Done():
				return
			case message, ok := <-channel:
				if !ok {
					return
				}
				version, err := strconv.ParseInt(strings.TrimSpace(message.Payload), 10, 64)
				if err != nil || version < 1 {
					continue
				}
				handler(version)
			}
		}
	}()
	return func() {
		cancel()
		_ = pubsub.Close()
	}, nil
}

type localConfigInvalidationBus struct {
	mu          sync.Mutex
	next        uint64
	closed      bool
	subscribers map[uint64]func(int64)
}

// NewLocalConfigInvalidationBus returns a process-local invalidation bus. It is
// used when the deployment selects the in-memory cache and therefore has no
// Redis client to publish through.
func NewLocalConfigInvalidationBus() ConfigInvalidationBus {
	return &localConfigInvalidationBus{subscribers: make(map[uint64]func(int64))}
}

func (b *localConfigInvalidationBus) Publish(ctx context.Context, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b == nil {
		return nil
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return context.Canceled
	}
	handlers := make([]func(int64), 0, len(b.subscribers))
	for _, handler := range b.subscribers {
		handlers = append(handlers, handler)
	}
	b.mu.Unlock()
	for _, handler := range handlers {
		handler(version)
	}
	return nil
}

func (b *localConfigInvalidationBus) Subscribe(ctx context.Context, handler func(int64)) (func(), error) {
	if err := ctx.Err(); err != nil {
		return func() {}, err
	}
	if b == nil || handler == nil {
		return func() {}, nil
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return func() {}, context.Canceled
	}
	b.next++
	id := b.next
	b.subscribers[id] = handler
	b.mu.Unlock()

	var once sync.Once
	stop := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subscribers, id)
			b.mu.Unlock()
		})
	}
	context.AfterFunc(ctx, stop)
	return stop, nil
}

func (b *localConfigInvalidationBus) close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.closed = true
	b.subscribers = nil
	b.mu.Unlock()
}
