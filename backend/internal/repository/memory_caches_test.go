package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// newTestCore 返回一个不受 GC 干扰的 core，并把 now 换成可控时钟。
func newTestCore(now *time.Time) *memoryCacheCore {
	core := newMemoryCacheCore(0)
	core.now = func() time.Time { return *now }
	return core
}

// ─── TTL 过期 ─────────────────────────────────────────────────────────────

func TestMemoryGatewayCache_TTLExpiry(t *testing.T) {
	now := time.Now()
	core := newTestCore(&now)
	cache := NewMemoryGatewayCache(core)
	ctx := context.Background()

	if err := cache.SetSessionAccountID(ctx, 7, "sess", 42, 30*time.Second); err != nil {
		t.Fatalf("SetSessionAccountID: %v", err)
	}
	accountID, err := cache.GetSessionAccountID(ctx, 7, "sess")
	if err != nil {
		t.Fatalf("GetSessionAccountID: %v", err)
	}
	if accountID != 42 {
		t.Fatalf("accountID = %d, want 42", accountID)
	}

	// RefreshSessionTTL 续期后仍在窗口内。
	now = now.Add(20 * time.Second)
	if err := cache.RefreshSessionTTL(ctx, 7, "sess", time.Minute); err != nil {
		t.Fatalf("RefreshSessionTTL: %v", err)
	}
	now = now.Add(50 * time.Second)
	if _, err := cache.GetSessionAccountID(ctx, 7, "sess"); err != nil {
		t.Fatalf("expected entry alive after refresh, got %v", err)
	}

	// 越过续期后的窗口，应命中 miss。
	now = now.Add(30 * time.Second)
	if _, err := cache.GetSessionAccountID(ctx, 7, "sess"); !errors.Is(err, service.ErrStickySessionNotFound) {
		t.Fatalf("expected ErrStickySessionNotFound after TTL, got %v", err)
	}
}

func TestMemoryUpdateCache_TTLExpiry(t *testing.T) {
	now := time.Now()
	core := newTestCore(&now)
	cache := NewMemoryUpdateCache(core)
	ctx := context.Background()

	if _, err := cache.GetUpdateInfo(ctx); !errors.Is(err, errMemoryCacheMiss) {
		t.Fatalf("expected miss on empty cache, got %v", err)
	}
	if err := cache.SetUpdateInfo(ctx, "v1", 10*time.Second); err != nil {
		t.Fatalf("SetUpdateInfo: %v", err)
	}
	if data, _ := cache.GetUpdateInfo(ctx); data != "v1" {
		t.Fatalf("data = %q, want v1", data)
	}
	now = now.Add(11 * time.Second)
	if _, err := cache.GetUpdateInfo(ctx); !errors.Is(err, errMemoryCacheMiss) {
		t.Fatalf("expected miss after TTL, got %v", err)
	}
}

func TestMemoryTimeoutCounter_TTLAndReset(t *testing.T) {
	now := time.Now()
	core := newTestCore(&now)
	cache := NewMemoryTimeoutCounterCache(core)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		count, err := cache.IncrementTimeoutCount(ctx, 11, 5)
		if err != nil {
			t.Fatalf("IncrementTimeoutCount: %v", err)
		}
		if count != int64(i) {
			t.Fatalf("count = %d, want %d", count, i)
		}
	}
	ttl, err := cache.GetTimeoutCountTTL(ctx, 11)
	if err != nil {
		t.Fatalf("GetTimeoutCountTTL: %v", err)
	}
	if ttl <= 0 || ttl > 5*time.Minute {
		t.Fatalf("ttl = %v, want (0, 5m]", ttl)
	}

	now = now.Add(6 * time.Minute)
	if count, _ := cache.GetTimeoutCount(ctx, 11); count != 0 {
		t.Fatalf("count after window = %d, want 0", count)
	}

	if _, err := cache.IncrementTimeoutCount(ctx, 11, 5); err != nil {
		t.Fatalf("IncrementTimeoutCount after reset: %v", err)
	}
	if err := cache.ResetTimeoutCount(ctx, 11); err != nil {
		t.Fatalf("ResetTimeoutCount: %v", err)
	}
	if count, _ := cache.GetTimeoutCount(ctx, 11); count != 0 {
		t.Fatalf("count after reset = %d, want 0", count)
	}
}

// ─── 计数与并发 ───────────────────────────────────────────────────────────

func TestMemoryRPMCache_CountsPerMinute(t *testing.T) {
	now := time.Now()
	core := newTestCore(&now)
	cache := NewMemoryRPMCache(core)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		count, err := cache.IncrementRPM(ctx, 100)
		if err != nil {
			t.Fatalf("IncrementRPM: %v", err)
		}
		if count != i {
			t.Fatalf("count = %d, want %d", count, i)
		}
	}
	batch, err := cache.GetRPMBatch(ctx, []int64{100, 200})
	if err != nil {
		t.Fatalf("GetRPMBatch: %v", err)
	}
	if batch[100] != 5 || batch[200] != 0 {
		t.Fatalf("batch = %v, want {100:5 200:0}", batch)
	}

	// 跨分钟后新桶从 1 开始。
	now = now.Add(90 * time.Second)
	if count, _ := cache.GetRPM(ctx, 100); count != 0 {
		t.Fatalf("count in new minute = %d, want 0", count)
	}
	if count, _ := cache.IncrementRPM(ctx, 100); count != 1 {
		t.Fatalf("first count in new minute = %d, want 1", count)
	}
}

func TestMemoryRedeemCache_CountAndSetNXLock(t *testing.T) {
	now := time.Now()
	core := newTestCore(&now)
	cache := NewMemoryRedeemCache(core)
	ctx := context.Background()

	if count, _ := cache.GetRedeemAttemptCount(ctx, 5); count != 0 {
		t.Fatalf("initial count = %d, want 0", count)
	}
	if err := cache.IncrementRedeemAttemptCount(ctx, 5); err != nil {
		t.Fatalf("IncrementRedeemAttemptCount: %v", err)
	}
	if err := cache.IncrementRedeemAttemptCount(ctx, 5); err != nil {
		t.Fatalf("IncrementRedeemAttemptCount: %v", err)
	}
	if count, _ := cache.GetRedeemAttemptCount(ctx, 5); count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	acquired, err := cache.AcquireRedeemLock(ctx, "CODE-1", time.Minute)
	if err != nil || !acquired {
		t.Fatalf("first AcquireRedeemLock = %v, %v; want true", acquired, err)
	}
	acquired, err = cache.AcquireRedeemLock(ctx, "CODE-1", time.Minute)
	if err != nil || acquired {
		t.Fatalf("second AcquireRedeemLock = %v, %v; want false", acquired, err)
	}
	if err := cache.ReleaseRedeemLock(ctx, "CODE-1"); err != nil {
		t.Fatalf("ReleaseRedeemLock: %v", err)
	}
	if acquired, _ := cache.AcquireRedeemLock(ctx, "CODE-1", time.Minute); !acquired {
		t.Fatalf("acquire after release = false, want true")
	}
}

func TestMemoryCounter_ConcurrentIncrement(t *testing.T) {
	core := newMemoryCacheCore(0)
	cache := NewMemoryInternal500CounterCache(core)
	ctx := context.Background()

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if _, err := cache.IncrementInternal500Count(ctx, 9); err != nil {
				t.Errorf("IncrementInternal500Count: %v", err)
			}
		}()
	}
	wg.Wait()

	value, ok := core.get(internal500CounterPrefix + "9")
	if !ok || memoryInt64(value) != goroutines {
		t.Fatalf("concurrent increments lost: %v", value)
	}
}

func TestMemoryTimeoutCounterDoesNotExtendFixedWindow(t *testing.T) {
	now := time.Now()
	cache := NewMemoryTimeoutCounterCache(newTestCore(&now))
	ctx := context.Background()
	_, err := cache.IncrementTimeoutCount(ctx, 7, 5)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(4 * time.Minute)
	if count, err := cache.IncrementTimeoutCount(ctx, 7, 5); err != nil || count != 2 {
		t.Fatalf("increment = %d, %v", count, err)
	}
	now = now.Add(2 * time.Minute)
	if count, err := cache.GetTimeoutCount(ctx, 7); err != nil || count != 0 {
		t.Fatalf("counter outlived its original window: %d, %v", count, err)
	}
}

// ─── Passkey 一次性消费 ───────────────────────────────────────────────────

func TestMemoryPasskeySessionStore_ConsumeIsOneShot(t *testing.T) {
	core := newMemoryCacheCore(0)
	store := NewMemoryPasskeySessionStore(core, nil)
	ctx := context.Background()

	session := &service.PasskeySession{Kind: "login", UserID: 42}
	token, err := store.Store(ctx, session, time.Minute)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	consumed, err := store.Consume(ctx, token)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if consumed.UserID != 42 || consumed.Kind != "login" {
		t.Fatalf("consumed = %+v, want user 42 / login", consumed)
	}

	// 第二次消费必须失败。
	if _, err := store.Consume(ctx, token); !errors.Is(err, service.ErrPasskeySession) {
		t.Fatalf("second Consume = %v, want ErrPasskeySession", err)
	}
	if _, err := store.Consume(ctx, "unknown"); !errors.Is(err, service.ErrPasskeySession) {
		t.Fatalf("unknown token = %v, want ErrPasskeySession", err)
	}
}

func TestMemoryPasskeySessionStore_ConcurrentConsumeSingleWinner(t *testing.T) {
	core := newMemoryCacheCore(0)
	store := NewMemoryPasskeySessionStore(core, nil)
	ctx := context.Background()

	token, err := store.Store(ctx, &service.PasskeySession{Kind: "registration", UserID: 7}, time.Minute)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	const goroutines = 32
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		success int
	)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if _, err := store.Consume(ctx, token); err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if success != 1 {
		t.Fatalf("successful consumes = %d, want exactly 1", success)
	}
}

// ─── 其他代表性语义 ───────────────────────────────────────────────────────

func TestMemoryEmailCache_ConsumePasswordResetToken(t *testing.T) {
	now := time.Now()
	core := newTestCore(&now)
	cache := NewMemoryEmailCache(core)
	ctx := context.Background()

	expiresAt := now.Add(10 * time.Minute)
	data := &service.PasswordResetTokenData{Token: "hash-1", CreatedAt: now}
	if err := cache.SetPasswordResetToken(ctx, "User@Example.com", data, 10*time.Minute); err != nil {
		t.Fatalf("SetPasswordResetToken: %v", err)
	}

	if ok, _ := cache.ConsumePasswordResetToken(ctx, "user@example.com", "wrong"); ok {
		t.Fatal("consume with wrong hash should fail")
	}
	if ok, err := cache.ConsumePasswordResetToken(ctx, "user@example.com", "hash-1"); !ok || err != nil {
		t.Fatalf("ConsumePasswordResetToken = %v, %v; want true, nil", ok, err)
	}
	if ok, _ := cache.ConsumePasswordResetToken(ctx, "user@example.com", "hash-1"); ok {
		t.Fatal("consume should be one-shot")
	}
	_ = expiresAt
}

func TestMemoryTempUnschedCache_OnlyExtends(t *testing.T) {
	now := time.Now()
	core := newTestCore(&now)
	cache := NewMemoryTempUnschedCache(core)
	ctx := context.Background()

	long := &service.TempUnschedState{UntilUnix: now.Add(10 * time.Minute).Unix(), StatusCode: 500}
	if err := cache.SetTempUnsched(ctx, 3, long); err != nil {
		t.Fatalf("SetTempUnsched: %v", err)
	}
	short := &service.TempUnschedState{UntilUnix: now.Add(time.Minute).Unix(), StatusCode: 503}
	if err := cache.SetTempUnsched(ctx, 3, short); err != nil {
		t.Fatalf("SetTempUnsched shorter: %v", err)
	}
	state, err := cache.GetTempUnsched(ctx, 3)
	if err != nil {
		t.Fatalf("GetTempUnsched: %v", err)
	}
	if state.UntilUnix != long.UntilUnix {
		t.Fatalf("until = %d, want %d (shorter overwrite not allowed)", state.UntilUnix, long.UntilUnix)
	}

	// 更长的状态应覆盖。
	longer := &service.TempUnschedState{UntilUnix: now.Add(20 * time.Minute).Unix(), StatusCode: 500}
	if err := cache.SetTempUnsched(ctx, 3, longer); err != nil {
		t.Fatalf("SetTempUnsched longer: %v", err)
	}
	state, _ = cache.GetTempUnsched(ctx, 3)
	if state.UntilUnix != longer.UntilUnix {
		t.Fatalf("until = %d, want %d", state.UntilUnix, longer.UntilUnix)
	}
}

func TestMemoryContentModerationHashCache_SetOps(t *testing.T) {
	core := newMemoryCacheCore(0)
	cache := NewMemoryContentModerationHashCache(core)
	ctx := context.Background()

	for _, hash := range []string{"h1", "h2", "h3"} {
		if err := cache.RecordFlaggedInputHash(ctx, hash); err != nil {
			t.Fatalf("RecordFlaggedInputHash(%s): %v", hash, err)
		}
	}
	// 重复记录不改变计数。
	if err := cache.RecordFlaggedInputHash(ctx, "h1"); err != nil {
		t.Fatalf("duplicate record: %v", err)
	}
	if count, _ := cache.CountFlaggedInputHashes(ctx); count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
	if has, _ := cache.HasFlaggedInputHash(ctx, "h2"); !has {
		t.Fatal("expected h2 present")
	}
	if has, _ := cache.HasFlaggedInputHash(ctx, "nope"); has {
		t.Fatal("unexpected hash present")
	}
	if deleted, _ := cache.DeleteFlaggedInputHash(ctx, "h2"); !deleted {
		t.Fatal("expected h2 deleted")
	}
	if deleted, _ := cache.DeleteFlaggedInputHash(ctx, "h2"); deleted {
		t.Fatal("second delete should report false")
	}
	cleared, _ := cache.ClearFlaggedInputHashes(ctx)
	if cleared != 2 {
		t.Fatalf("cleared = %d, want 2", cleared)
	}
	if count, _ := cache.CountFlaggedInputHashes(ctx); count != 0 {
		t.Fatalf("count after clear = %d, want 0", count)
	}
}

func TestMemoryGatewayCache_ClaimGrokVideoBilledIsSetNX(t *testing.T) {
	core := newMemoryCacheCore(0)
	cache := NewMemoryGatewayCache(core)
	ctx := context.Background()

	claimed, err := cache.ClaimGrokVideoBilled(ctx, "call-1", time.Minute)
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v; want true", claimed, err)
	}
	claimed, err = cache.ClaimGrokVideoBilled(ctx, "call-1", time.Minute)
	if err != nil || claimed {
		t.Fatalf("second claim = %v, %v; want false", claimed, err)
	}
	if err := cache.ReleaseGrokVideoBilled(ctx, "call-1"); err != nil {
		t.Fatalf("ReleaseGrokVideoBilled: %v", err)
	}
	if claimed, _ := cache.ClaimGrokVideoBilled(ctx, "call-1", time.Minute); !claimed {
		t.Fatal("claim after release should succeed")
	}
}

func TestMemoryUserMsgQueueCache_LockLifecycle(t *testing.T) {
	core := newMemoryCacheCore(0)
	cache := NewMemoryUserMsgQueueCache(core)
	ctx := context.Background()

	acquired, err := cache.AcquireLock(ctx, 88, "req-1", 60_000)
	if err != nil || !acquired {
		t.Fatalf("AcquireLock = %v, %v; want true", acquired, err)
	}
	if acquired, _ := cache.AcquireLock(ctx, 88, "req-2", 60_000); acquired {
		t.Fatal("second holder should not acquire")
	}
	// 同 requestID 重入成功并续租。
	if acquired, _ := cache.AcquireLock(ctx, 88, "req-1", 60_000); !acquired {
		t.Fatal("reentrant acquire should succeed")
	}

	if released, _ := cache.ReleaseLock(ctx, 88, "req-2"); released {
		t.Fatal("wrong requestID should not release")
	}
	if released, err := cache.ReleaseLock(ctx, 88, "req-1"); err != nil || !released {
		t.Fatalf("ReleaseLock = %v, %v; want true", released, err)
	}
	if last, _ := cache.GetLastCompletedMs(ctx, 88); last <= 0 {
		t.Fatal("expected completion timestamp to be recorded")
	}

	// 释放后可再次获取。
	if acquired, _ := cache.AcquireLock(ctx, 88, "req-3", 60_000); !acquired {
		t.Fatal("acquire after release should succeed")
	}
}
