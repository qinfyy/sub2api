package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/middleware"
)

type memoryRateLimitBackend struct{ core *memoryCacheCore }

func (b *memoryRateLimitBackend) Increment(ctx context.Context, key string, window time.Duration) (int64, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	if window < time.Millisecond {
		window = time.Millisecond
	}
	n, err := b.core.increment(key, window, false)
	return n, false, err
}
func (b *memoryRateLimitBackend) TTL(ctx context.Context, key string) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	ttl, _ := b.core.ttl(key)
	return ttl, nil
}
func NewMemoryHTTPRateLimiter(core *memoryCacheCore) *middleware.RateLimiter {
	return middleware.NewRateLimiterWithBackend(&memoryRateLimitBackend{core: memoryAssertCore(core)})
}
func ProvideHTTPRateLimiter(r *BackendResources) *middleware.RateLimiter {
	if r.memory != nil {
		return NewMemoryHTTPRateLimiter(r.memory)
	}
	return middleware.NewRateLimiter(r.Redis)
}
