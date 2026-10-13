package websearch

import (
	"context"
	"sync"
	"time"
)

// MemoryQuotaStore is the process-local quota/proxy-state backend used when
// the deployment selects the in-memory cache.
type MemoryQuotaStore struct {
	mu     sync.Mutex
	values map[string]memoryQuotaValue
}

type memoryQuotaValue struct {
	count   int64
	expires time.Time
}

func NewMemoryQuotaStore() *MemoryQuotaStore {
	return &MemoryQuotaStore{values: make(map[string]memoryQuotaValue)}
}

func (s *MemoryQuotaStore) IncrementQuota(_ context.Context, key string, ttl time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	v := s.values[key]
	if !v.expires.IsZero() && !now.Before(v.expires) {
		v = memoryQuotaValue{}
	}
	v.count++
	if v.expires.IsZero() {
		v.expires = now.Add(ttl)
	}
	s.values[key] = v
	return v.count, nil
}

func (s *MemoryQuotaStore) DecrementQuota(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.values[key]
	if v.count > 0 {
		v.count--
	}
	s.values[key] = v
	return nil
}

func (s *MemoryQuotaStore) GetQuota(_ context.Context, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.values[key]
	if !v.expires.IsZero() && !time.Now().Before(v.expires) {
		delete(s.values, key)
		return 0, nil
	}
	return v.count, nil
}

func (s *MemoryQuotaStore) ResetQuota(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
	return nil
}

func (s *MemoryQuotaStore) SetProxyUnavailable(_ context.Context, key string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = memoryQuotaValue{count: 1, expires: time.Now().Add(ttl)}
	return nil
}

func (s *MemoryQuotaStore) IsProxyUnavailable(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[key]
	if !ok {
		return false, nil
	}
	if !v.expires.IsZero() && !time.Now().Before(v.expires) {
		delete(s.values, key)
		return false, nil
	}
	return v.count > 0, nil
}
