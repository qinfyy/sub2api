// 本文件实现 Sub2API 缓存接口的内存（memory）后端。
//
// 语义边界：内存后端是单实例实现，所有互斥（锁、计数、SetNX、滚动窗口）都靠
// 进程内互斥保证，不提供跨进程协调。需要跨重启保留的状态（刷新令牌、插件 KV）
// 在构造函数传入非 nil 的 *sql.DB 时写穿 SQLite 的 kv_store 表。
//
// 值编码沿用 Redis 版的 JSON 约定：内存里直接存结构体或标量；需要 JSON 的
// 接口（与持久层解耦、或调用方可能修改返回值）才序列化。
package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ─── 通用助手 ─────────────────────────────────────────────────────────────

// errMemoryCacheMiss 表示内存缓存未命中。各接口按 Redis 版约定翻译成各自的
// miss 返回值（nil / 零值 / 哨兵错误），调用方无需感知底层差异。
var errMemoryCacheMiss = errors.New("memory cache miss")

// memoryBytes 把任意缓存值转成字节切片。
func memoryBytes(value any) []byte {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		return v
	case string:
		return []byte(v)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return payload
}

// memoryInt64 把缓存值转成 int64。
func memoryInt64(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		parsed, _ := strconv.ParseInt(v, 10, 64)
		return parsed
	case []byte:
		parsed, _ := strconv.ParseInt(string(v), 10, 64)
		return parsed
	}
	return 0
}

// unmarshalMemoryJSON 从 JSON 字节反序列化到 T。
func unmarshalMemoryJSON[T any](payload []byte) (T, error) {
	var out T
	if len(payload) == 0 {
		return out, errMemoryCacheMiss
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return out, err
	}
	return out, nil
}

// remainingTTL 从绝对到期时间算剩余 TTL；已到期返回极短 TTL 促使尽快清理。
func remainingTTL(expiresAt time.Time) time.Duration {
	if expiresAt.IsZero() {
		return 0
	}
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return time.Millisecond
	}
	return ttl
}

// memoryLockKey 构造并加锁 keyed mutex，配合 memoryUnlock 使用。
func memoryLockKey(key string) *keyedMutex {
	mutex := newKeyedMutex("memory:" + key)
	mutex.Lock()
	return mutex
}

// memoryUnlock 释放由 memoryLockKey 获取的锁。
func memoryUnlock(mutex *keyedMutex) {
	mutex.Unlock()
	mutex.release()
}

// memoryIncrementCounter 原子递增带 TTL 的计数器，返回新值。
func memoryIncrementCounter(core *memoryCacheCore, key string, ttl time.Duration) (int64, error) {
	return core.increment(key, ttl, true)
}

func memoryIncrementFixedCounter(core *memoryCacheCore, key string, ttl time.Duration) (int64, error) {
	return core.increment(key, ttl, false)
}

// memorySetNX 等价于 Redis SETNX：键已存在（且未过期）返回 false。
func memorySetNX(core *memoryCacheCore, key string, value any, ttl time.Duration) bool {
	core.mu.Lock()
	defer core.mu.Unlock()
	if entry := core.entries[key]; entry != nil && !core.expiredLocked(entry) {
		return false
	}
	return core.putLocked(key, value, ttl, true) == nil
}

// memoryAssertCore 防止 nil core 被误传入导致运行期 panic。
func memoryAssertCore(core *memoryCacheCore) *memoryCacheCore {
	if core == nil {
		return newMemoryCacheCore(0)
	}
	return core
}

// ─── GatewayCache ─────────────────────────────────────────────────────────

// memoryGatewayCache 用内存实现粘性会话、Grok 计费快照与 reasoning 缓存。
type memoryGatewayCache struct {
	core  *memoryCacheCore
	state *memoryKVBackend
}

// NewMemoryGatewayCache 返回单实例语义的 GatewayCache。
func NewMemoryGatewayCache(core *memoryCacheCore, dbs ...*sql.DB) service.GatewayCache {
	core = memoryAssertCore(core)
	c := &memoryGatewayCache{core: core}
	if len(dbs) > 0 && dbs[0] != nil {
		c.state = &memoryKVBackend{core: core, db: dbs[0]}
	}
	return c
}

var _ service.GatewayCache = (*memoryGatewayCache)(nil)

func (c *memoryGatewayCache) GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error) {
	value, ok := c.core.get(buildSessionKey(groupID, sessionHash))
	if !ok {
		return 0, service.ErrStickySessionNotFound
	}
	accountID := memoryInt64(value)
	if accountID <= 0 {
		return 0, service.ErrStickySessionNotFound
	}
	return accountID, nil
}

func (c *memoryGatewayCache) SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	c.core.set(buildSessionKey(groupID, sessionHash), accountID, ttl)
	return nil
}

func (c *memoryGatewayCache) RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error {
	// Redis 版对不存在的键调用 EXPIRE 返回 0 且不报错，这里同样静默。
	c.core.touch(buildSessionKey(groupID, sessionHash), ttl)
	return nil
}

func (c *memoryGatewayCache) DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error {
	c.core.delete(buildSessionKey(groupID, sessionHash))
	return nil
}

func (c *memoryGatewayCache) SetGrokVideoPendingBilling(ctx context.Context, key string, payload []byte, ttl time.Duration) error {
	key = strings.TrimSpace(key)
	if key == "" || len(payload) == 0 {
		return errors.New("invalid grok video pending billing payload")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	if c.state != nil {
		return c.state.setBytes(ctx, grokVideoPendingBillingPrefix+key, payload, ttl)
	}
	c.core.set(grokVideoPendingBillingPrefix+key, append([]byte(nil), payload...), ttl)
	return nil
}

func (c *memoryGatewayCache) GetGrokVideoPendingBilling(ctx context.Context, key string) ([]byte, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("invalid grok video pending billing key")
	}
	if c.state != nil {
		value, err := c.state.get(ctx, grokVideoPendingBillingPrefix+key)
		if errors.Is(err, errMemoryCacheMiss) {
			return nil, nil
		}
		return value, err
	}
	value, ok := c.core.get(grokVideoPendingBillingPrefix + key)
	if !ok {
		return nil, nil
	}
	return append([]byte(nil), memoryBytes(value)...), nil
}

func (c *memoryGatewayCache) ClaimGrokVideoBilled(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return false, errors.New("invalid grok video billed key")
	}
	if ttl <= 0 {
		ttl = 48 * time.Hour
	}
	if c.state != nil {
		return c.state.setNX(ctx, grokVideoBilledPrefix+key, []byte("1"), ttl)
	}
	return memorySetNX(c.core, grokVideoBilledPrefix+key, 1, ttl), nil
}

func (c *memoryGatewayCache) ReleaseGrokVideoBilled(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("invalid grok video billed key")
	}
	if c.state != nil {
		return c.state.remove(ctx, grokVideoBilledPrefix+key)
	}
	c.core.delete(grokVideoBilledPrefix + key)
	return nil
}

func (c *memoryGatewayCache) SetReasoningContent(ctx context.Context, itemID string, content string, ttl time.Duration) error {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" || content == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = reasoningContentDefaultTTL
	}
	c.core.set(reasoningContentPrefix+itemID, content, ttl)
	return nil
}

func (c *memoryGatewayCache) GetReasoningContent(ctx context.Context, itemID string) (string, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return "", service.ErrReasoningContentNotFound
	}
	value, ok := c.core.get(reasoningContentPrefix + itemID)
	if !ok {
		return "", service.ErrReasoningContentNotFound
	}
	content, _ := value.(string)
	return content, nil
}

// ─── EmailCache ───────────────────────────────────────────────────────────

// memoryEmailCache 存验证码、密码重置令牌与通知限流计数。
// 尝试次数与验证码放在同一条记录里，锁保证与 Redis 脚本一致的原子语义。
type memoryEmailCache struct {
	core *memoryCacheCore
}

// NewMemoryEmailCache 返回单实例语义的 EmailCache。
func NewMemoryEmailCache(core *memoryCacheCore) service.EmailCache {
	return &memoryEmailCache{core: memoryAssertCore(core)}
}

var _ service.EmailCache = (*memoryEmailCache)(nil)

func (c *memoryEmailCache) getCode(key string) (*service.VerificationCodeData, error) {
	value, ok := c.core.get(key)
	if !ok {
		return nil, errMemoryCacheMiss
	}
	data, err := unmarshalMemoryJSON[service.VerificationCodeData](memoryBytes(value))
	if err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *memoryEmailCache) setCode(key string, data *service.VerificationCodeData, ttl time.Duration) error {
	if data == nil {
		return errors.New("nil verification code data")
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	c.core.set(key, payload, ttl)
	return nil
}

func (c *memoryEmailCache) incrCodeAttempts(key string) (int, error) {
	mutex := memoryLockKey("email:attempts:" + key)
	defer memoryUnlock(mutex)

	data, err := c.getCode(key)
	if err != nil {
		return 0, err
	}
	data.Attempts++
	// 复用验证码自身的到期时间：验证码过期时尝试计数一并失效。
	c.core.set(key, memoryBytes(data), remainingTTL(data.ExpiresAt))
	return data.Attempts, nil
}

func (c *memoryEmailCache) GetVerificationCode(ctx context.Context, email string) (*service.VerificationCodeData, error) {
	return c.getCode(verifyCodeKey(email))
}

func (c *memoryEmailCache) SetVerificationCode(ctx context.Context, email string, data *service.VerificationCodeData, ttl time.Duration) error {
	return c.setCode(verifyCodeKey(email), data, ttl)
}

func (c *memoryEmailCache) IncrVerificationCodeAttempts(ctx context.Context, email string) (int, error) {
	return c.incrCodeAttempts(verifyCodeKey(email))
}

func (c *memoryEmailCache) DeleteVerificationCode(ctx context.Context, email string) error {
	c.core.delete(verifyCodeKey(email))
	return nil
}

func (c *memoryEmailCache) GetNotifyVerifyCode(ctx context.Context, email string) (*service.VerificationCodeData, error) {
	return c.getCode(notifyVerifyKey(email))
}

func (c *memoryEmailCache) SetNotifyVerifyCode(ctx context.Context, email string, data *service.VerificationCodeData, ttl time.Duration) error {
	return c.setCode(notifyVerifyKey(email), data, ttl)
}

func (c *memoryEmailCache) IncrNotifyVerifyCodeAttempts(ctx context.Context, email string) (int, error) {
	return c.incrCodeAttempts(notifyVerifyKey(email))
}

func (c *memoryEmailCache) DeleteNotifyVerifyCode(ctx context.Context, email string) error {
	c.core.delete(notifyVerifyKey(email))
	return nil
}

func (c *memoryEmailCache) GetPasswordResetToken(ctx context.Context, email string) (*service.PasswordResetTokenData, error) {
	value, ok := c.core.get(passwordResetKey(email))
	if !ok {
		return nil, errMemoryCacheMiss
	}
	data, err := unmarshalMemoryJSON[service.PasswordResetTokenData](memoryBytes(value))
	if err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *memoryEmailCache) SetPasswordResetToken(ctx context.Context, email string, data *service.PasswordResetTokenData, ttl time.Duration) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	c.core.set(passwordResetKey(email), payload, ttl)
	return nil
}

func (c *memoryEmailCache) ConsumePasswordResetToken(ctx context.Context, email, tokenHash string) (bool, error) {
	key := passwordResetKey(email)
	mutex := memoryLockKey("email:reset:" + key)
	defer memoryUnlock(mutex)

	value, ok := c.core.get(key)
	if !ok {
		return false, nil
	}
	data, err := unmarshalMemoryJSON[service.PasswordResetTokenData](memoryBytes(value))
	if err != nil {
		return false, nil
	}
	if data.Token != tokenHash {
		return false, nil
	}
	c.core.delete(key)
	return true, nil
}

func (c *memoryEmailCache) DeletePasswordResetToken(ctx context.Context, email string) error {
	c.core.delete(passwordResetKey(email))
	return nil
}

func (c *memoryEmailCache) IsPasswordResetEmailInCooldown(ctx context.Context, email string) bool {
	_, ok := c.core.get(passwordResetSentAtKey(email))
	return ok
}

func (c *memoryEmailCache) SetPasswordResetEmailCooldown(ctx context.Context, email string, ttl time.Duration) error {
	c.core.set(passwordResetSentAtKey(email), "1", ttl)
	return nil
}

func (c *memoryEmailCache) IncrNotifyCodeUserRate(ctx context.Context, userID int64, window time.Duration) (int64, error) {
	// 幂等刷新窗口，与 Redis INCR + EXPIRE 等价。
	return memoryIncrementCounter(c.core, notifyCodeUserRateKey(userID), window)
}

func (c *memoryEmailCache) GetNotifyCodeUserRate(ctx context.Context, userID int64) (int64, error) {
	value, ok := c.core.get(notifyCodeUserRateKey(userID))
	if !ok {
		return 0, errMemoryCacheMiss
	}
	return memoryInt64(value), nil
}

// ─── IdentityCache ────────────────────────────────────────────────────────

// memoryIdentityCache 缓存账号指纹与伪装会话 ID。
type memoryIdentityCache struct {
	core *memoryCacheCore
}

// NewMemoryIdentityCache 返回单实例语义的 IdentityCache。
func NewMemoryIdentityCache(core *memoryCacheCore) service.IdentityCache {
	return &memoryIdentityCache{core: memoryAssertCore(core)}
}

var _ service.IdentityCache = (*memoryIdentityCache)(nil)

func (c *memoryIdentityCache) GetFingerprint(ctx context.Context, accountID int64) (*service.Fingerprint, error) {
	value, ok := c.core.get(fingerprintKey(accountID))
	if !ok {
		return nil, errMemoryCacheMiss
	}
	data, err := unmarshalMemoryJSON[service.Fingerprint](memoryBytes(value))
	if err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *memoryIdentityCache) SetFingerprint(ctx context.Context, accountID int64, fp *service.Fingerprint) error {
	if fp == nil {
		return errors.New("nil fingerprint")
	}
	payload, err := json.Marshal(fp)
	if err != nil {
		return err
	}
	c.core.set(fingerprintKey(accountID), payload, fingerprintTTL)
	return nil
}

func (c *memoryIdentityCache) GetMaskedSessionID(ctx context.Context, accountID int64) (string, error) {
	value, ok := c.core.get(maskedSessionKey(accountID))
	if !ok {
		return "", nil
	}
	sessionID, _ := value.(string)
	return sessionID, nil
}

func (c *memoryIdentityCache) SetMaskedSessionID(ctx context.Context, accountID int64, sessionID string) error {
	c.core.set(maskedSessionKey(accountID), sessionID, maskedSessionTTL)
	return nil
}

// ─── RedeemCache ──────────────────────────────────────────────────────────

// memoryRedeemCache 缓存兑换尝试次数与兑换码串行锁。
type memoryRedeemCache struct {
	core *memoryCacheCore
}

// NewMemoryRedeemCache 返回单实例语义的 RedeemCache。
func NewMemoryRedeemCache(core *memoryCacheCore) service.RedeemCache {
	return &memoryRedeemCache{core: memoryAssertCore(core)}
}

var _ service.RedeemCache = (*memoryRedeemCache)(nil)

func (c *memoryRedeemCache) GetRedeemAttemptCount(ctx context.Context, userID int64) (int, error) {
	value, ok := c.core.get(redeemRateLimitKey(userID))
	if !ok {
		return 0, nil
	}
	return int(memoryInt64(value)), nil
}

func (c *memoryRedeemCache) IncrementRedeemAttemptCount(ctx context.Context, userID int64) error {
	_, err := memoryIncrementFixedCounter(c.core, redeemRateLimitKey(userID), redeemRateLimitWindow)
	return err
}

func (c *memoryRedeemCache) AcquireRedeemLock(ctx context.Context, code string, ttl time.Duration) (bool, error) {
	return memorySetNX(c.core, redeemLockKey(code), 1, ttl), nil
}

func (c *memoryRedeemCache) ReleaseRedeemLock(ctx context.Context, code string) error {
	c.core.delete(redeemLockKey(code))
	return nil
}

// ─── UpdateCache ──────────────────────────────────────────────────────────

// memoryUpdateCache 缓存 GitHub 版本检查结果。
type memoryUpdateCache struct {
	core *memoryCacheCore
}

// NewMemoryUpdateCache 返回单实例语义的 UpdateCache。
func NewMemoryUpdateCache(core *memoryCacheCore) service.UpdateCache {
	return &memoryUpdateCache{core: memoryAssertCore(core)}
}

var _ service.UpdateCache = (*memoryUpdateCache)(nil)

func (c *memoryUpdateCache) GetUpdateInfo(ctx context.Context) (string, error) {
	value, ok := c.core.get(updateCacheKey)
	if !ok {
		return "", errMemoryCacheMiss
	}
	data, _ := value.(string)
	return data, nil
}

func (c *memoryUpdateCache) SetUpdateInfo(ctx context.Context, data string, ttl time.Duration) error {
	c.core.set(updateCacheKey, data, ttl)
	return nil
}

// ─── TLS 指纹模板 / 错误透传规则 ─────────────────────────────────────────

// memoryRuleCache 支撑"本地二级缓存 + 通知其他实例刷新"的语义。
// 内存后端单实例，SubscribeUpdates 只需回调进程内注册的 handler。
type memoryRuleCache struct {
	core *memoryCacheCore

	mu       sync.RWMutex
	local    any
	handlers []func()
}

func newMemoryRuleCache(core *memoryCacheCore) *memoryRuleCache {
	return &memoryRuleCache{core: memoryAssertCore(core)}
}

func (c *memoryRuleCache) getLocal() any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.local
}

func (c *memoryRuleCache) setLocal(value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.local = value
}

func (c *memoryRuleCache) clearLocal() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.local = nil
}

func (c *memoryRuleCache) subscribe(handler func()) {
	if handler == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers = append(c.handlers, handler)
}

func (c *memoryRuleCache) notify() {
	c.mu.RLock()
	handlers := make([]func(), len(c.handlers))
	copy(handlers, c.handlers)
	c.mu.RUnlock()

	for _, handler := range handlers {
		handler()
	}
}

type memoryTLSFingerprintProfileCache struct {
	*memoryRuleCache
}

// NewMemoryTLSFingerprintProfileCache 返回单实例语义的 TLS 指纹模板缓存。
func NewMemoryTLSFingerprintProfileCache(core *memoryCacheCore) service.TLSFingerprintProfileCache {
	return &memoryTLSFingerprintProfileCache{memoryRuleCache: newMemoryRuleCache(core)}
}

var _ service.TLSFingerprintProfileCache = (*memoryTLSFingerprintProfileCache)(nil)

func (c *memoryTLSFingerprintProfileCache) Get(ctx context.Context) ([]*model.TLSFingerprintProfile, bool) {
	if local := c.getLocal(); local != nil {
		profiles, ok := local.([]*model.TLSFingerprintProfile)
		return profiles, ok
	}
	value, ok := c.core.get(tlsFPProfileCacheKey)
	if !ok {
		return nil, false
	}
	profiles, err := unmarshalMemoryJSON[[]*model.TLSFingerprintProfile](memoryBytes(value))
	if err != nil {
		return nil, false
	}
	c.setLocal(profiles)
	return profiles, true
}

func (c *memoryTLSFingerprintProfileCache) Set(ctx context.Context, profiles []*model.TLSFingerprintProfile) error {
	payload, err := json.Marshal(profiles)
	if err != nil {
		return err
	}
	c.core.set(tlsFPProfileCacheKey, payload, tlsFPProfileCacheTTL)
	c.setLocal(profiles)
	return nil
}

func (c *memoryTLSFingerprintProfileCache) Invalidate(ctx context.Context) error {
	c.clearLocal()
	c.core.delete(tlsFPProfileCacheKey)
	return nil
}

func (c *memoryTLSFingerprintProfileCache) NotifyUpdate(ctx context.Context) error {
	c.clearLocal()
	c.notify()
	return nil
}

func (c *memoryTLSFingerprintProfileCache) SubscribeUpdates(ctx context.Context, handler func()) {
	c.subscribe(handler)
}

type memoryErrorPassthroughCache struct {
	*memoryRuleCache
}

// NewMemoryErrorPassthroughCache 返回单实例语义的错误透传规则缓存。
func NewMemoryErrorPassthroughCache(core *memoryCacheCore) service.ErrorPassthroughCache {
	return &memoryErrorPassthroughCache{memoryRuleCache: newMemoryRuleCache(core)}
}

var _ service.ErrorPassthroughCache = (*memoryErrorPassthroughCache)(nil)

func (c *memoryErrorPassthroughCache) Get(ctx context.Context) ([]*model.ErrorPassthroughRule, bool) {
	if local := c.getLocal(); local != nil {
		rules, ok := local.([]*model.ErrorPassthroughRule)
		return rules, ok
	}
	value, ok := c.core.get(errorPassthroughCacheKey)
	if !ok {
		return nil, false
	}
	rules, err := unmarshalMemoryJSON[[]*model.ErrorPassthroughRule](memoryBytes(value))
	if err != nil {
		return nil, false
	}
	c.setLocal(rules)
	return rules, true
}

func (c *memoryErrorPassthroughCache) Set(ctx context.Context, rules []*model.ErrorPassthroughRule) error {
	payload, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	c.core.set(errorPassthroughCacheKey, payload, errorPassthroughCacheTTL)
	c.setLocal(rules)
	return nil
}

func (c *memoryErrorPassthroughCache) Invalidate(ctx context.Context) error {
	c.clearLocal()
	c.core.delete(errorPassthroughCacheKey)
	return nil
}

func (c *memoryErrorPassthroughCache) NotifyUpdate(ctx context.Context) error {
	c.clearLocal()
	c.notify()
	return nil
}

func (c *memoryErrorPassthroughCache) SubscribeUpdates(ctx context.Context, handler func()) {
	c.subscribe(handler)
}

// ─── ContentModerationHashCache ───────────────────────────────────────────

// memoryContentModerationHashCache 存被拦截内容的哈希集合。
// core 没有集合原语，因此每个哈希单独占一条 entry，另加一把 mutex 维护
// 成员表，保证 Record / Has / Delete / Clear 的原子性。
type memoryContentModerationHashCache struct {
	core *memoryCacheCore

	mu    sync.Mutex
	items map[string]struct{}
}

// NewMemoryContentModerationHashCache 返回单实例语义的内容审核哈希缓存。
func NewMemoryContentModerationHashCache(core *memoryCacheCore) service.ContentModerationHashCache {
	return &memoryContentModerationHashCache{
		core:  memoryAssertCore(core),
		items: make(map[string]struct{}),
	}
}

var _ service.ContentModerationHashCache = (*memoryContentModerationHashCache)(nil)

func contentModerationHashKey(hash string) string {
	return contentModerationFlaggedHashSetKey + ":" + hash
}

func (c *memoryContentModerationHashCache) RecordFlaggedInputHash(ctx context.Context, inputHash string) error {
	inputHash = strings.TrimSpace(inputHash)
	if inputHash == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.items[inputHash]; exists {
		return nil
	}
	c.items[inputHash] = struct{}{}
	c.core.set(contentModerationHashKey(inputHash), "1", 0)
	return nil
}

func (c *memoryContentModerationHashCache) HasFlaggedInputHash(ctx context.Context, inputHash string) (bool, error) {
	inputHash = strings.TrimSpace(inputHash)
	if inputHash == "" {
		return false, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, exists := c.items[inputHash]
	return exists, nil
}

func (c *memoryContentModerationHashCache) DeleteFlaggedInputHash(ctx context.Context, inputHash string) (bool, error) {
	inputHash = strings.TrimSpace(inputHash)
	if inputHash == "" {
		return false, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.items[inputHash]; !exists {
		return false, nil
	}
	delete(c.items, inputHash)
	c.core.delete(contentModerationHashKey(inputHash))
	return true, nil
}

func (c *memoryContentModerationHashCache) ClearFlaggedInputHashes(ctx context.Context) (int64, error) {
	c.mu.Lock()
	deleted := int64(len(c.items))
	keys := make([]string, 0, len(c.items))
	for inputHash := range c.items {
		keys = append(keys, contentModerationHashKey(inputHash))
	}
	c.items = make(map[string]struct{})
	c.mu.Unlock()

	for _, key := range keys {
		c.core.delete(key)
	}
	return deleted, nil
}

func (c *memoryContentModerationHashCache) CountFlaggedInputHashes(ctx context.Context) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return int64(len(c.items)), nil
}

// ─── ProxyLatencyCache ────────────────────────────────────────────────────

// memoryProxyLatencyCache 缓存各代理的连通性延迟。
type memoryProxyLatencyCache struct {
	core *memoryCacheCore
}

// NewMemoryProxyLatencyCache 返回单实例语义的 ProxyLatencyCache。
func NewMemoryProxyLatencyCache(core *memoryCacheCore) service.ProxyLatencyCache {
	return &memoryProxyLatencyCache{core: memoryAssertCore(core)}
}

var _ service.ProxyLatencyCache = (*memoryProxyLatencyCache)(nil)

func (c *memoryProxyLatencyCache) GetProxyLatencies(ctx context.Context, proxyIDs []int64) (map[int64]*service.ProxyLatencyInfo, error) {
	results := make(map[int64]*service.ProxyLatencyInfo, len(proxyIDs))
	for _, proxyID := range proxyIDs {
		value, ok := c.core.get(proxyLatencyKey(proxyID))
		if !ok {
			continue
		}
		info, err := unmarshalMemoryJSON[service.ProxyLatencyInfo](memoryBytes(value))
		if err != nil {
			continue
		}
		results[proxyID] = &info
	}
	return results, nil
}

func (c *memoryProxyLatencyCache) SetProxyLatency(ctx context.Context, proxyID int64, info *service.ProxyLatencyInfo) error {
	if info == nil {
		return nil
	}
	payload, err := json.Marshal(info)
	if err != nil {
		return err
	}
	// ttl=0 表示不过期，与 Redis 版 Set(..., 0) 语义一致。
	c.core.set(proxyLatencyKey(proxyID), payload, 0)
	return nil
}

// ─── TempUnschedCache / OpenAIAPIKeyHealthCache ────────────────────────────

// memoryTempUnschedCache 存账号临时不可调度状态与 OpenAI API Key 失败窗口。
type memoryTempUnschedCache struct {
	core *memoryCacheCore
}

// NewMemoryTempUnschedCache 返回单实例语义的 TempUnschedCache。
func NewMemoryTempUnschedCache(core *memoryCacheCore) service.TempUnschedCache {
	return &memoryTempUnschedCache{core: memoryAssertCore(core)}
}

var _ service.TempUnschedCache = (*memoryTempUnschedCache)(nil)
var _ service.OpenAIAPIKeyHealthCache = (*memoryTempUnschedCache)(nil)

func (c *memoryTempUnschedCache) SetTempUnsched(ctx context.Context, accountID int64, state *service.TempUnschedState) error {
	if state == nil {
		return errors.New("nil temp unsched state")
	}
	key := fmt.Sprintf("%s%d", tempUnschedPrefix, accountID)

	mutex := memoryLockKey("temp_unsched:" + key)
	defer memoryUnlock(mutex)

	// 已有更晚的到期时间时不覆盖，这是 Redis 版 Lua 脚本的核心语义。
	if value, ok := c.core.get(key); ok {
		existing, err := unmarshalMemoryJSON[service.TempUnschedState](memoryBytes(value))
		if err == nil && existing.UntilUnix >= state.UntilUnix {
			return nil
		}
	}

	ttl := time.Until(time.Unix(state.UntilUnix, 0))
	if ttl <= 0 {
		return nil
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	c.core.set(key, payload, ttl)
	return nil
}

func (c *memoryTempUnschedCache) GetTempUnsched(ctx context.Context, accountID int64) (*service.TempUnschedState, error) {
	value, ok := c.core.get(fmt.Sprintf("%s%d", tempUnschedPrefix, accountID))
	if !ok {
		return nil, nil
	}
	state, err := unmarshalMemoryJSON[service.TempUnschedState](memoryBytes(value))
	if err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	return &state, nil
}

func (c *memoryTempUnschedCache) DeleteTempUnsched(ctx context.Context, accountID int64) error {
	c.core.delete(fmt.Sprintf("%s%d", tempUnschedPrefix, accountID))
	return nil
}

func openAIAPIKeyHealthMemoryKey(accountID int64) string {
	return fmt.Sprintf("%s{%d}:failures", openAIAPIKeyHealthFailurePrefix, accountID)
}

// memoryOpenAIAPIKeyHealthWindow 是一个账号在窗口内的失败时间戳序列。
type memoryOpenAIAPIKeyHealthWindow struct {
	Timestamps []int64 `json:"timestamps"`
}

func (c *memoryTempUnschedCache) RecordOpenAIAPIKeyHealthFailure(ctx context.Context, accountID int64, windowMinutes, threshold int) (int64, bool, error) {
	if windowMinutes < 1 {
		windowMinutes = 1
	}
	if threshold < 1 {
		threshold = 1
	}
	key := openAIAPIKeyHealthMemoryKey(accountID)

	mutex := memoryLockKey("openai_apikey_health:" + key)
	defer memoryUnlock(mutex)

	now := time.Now()
	cutoff := now.Add(-time.Duration(windowMinutes) * time.Minute).UnixMilli()

	window := &memoryOpenAIAPIKeyHealthWindow{}
	if value, ok := c.core.get(key); ok {
		existing, err := unmarshalMemoryJSON[memoryOpenAIAPIKeyHealthWindow](memoryBytes(value))
		if err == nil {
			window = &existing
		}
	}

	kept := make([]int64, 0, len(window.Timestamps)+1)
	for _, ts := range window.Timestamps {
		if ts > cutoff {
			kept = append(kept, ts)
		}
	}
	window.Timestamps = append(kept, now.UnixMilli())

	count := int64(len(window.Timestamps))
	ttl := time.Duration(windowMinutes+1) * time.Minute
	if ttl < time.Minute {
		ttl = time.Minute
	}
	if count >= int64(threshold) {
		// 与 Redis 版一致：达到阈值后清空窗口，等待恢复期重新累积。
		c.core.delete(key)
		return count, true, nil
	}
	payload, err := json.Marshal(window)
	if err != nil {
		return 0, false, fmt.Errorf("marshal openai apikey health window: %w", err)
	}
	c.core.set(key, payload, ttl)
	return count, false, nil
}

// ─── 计数器类 ─────────────────────────────────────────────────────────────

// memoryTimeoutCounterCache 存按窗口过期的超时计数器。
type memoryTimeoutCounterCache struct {
	core *memoryCacheCore
}

// NewMemoryTimeoutCounterCache 返回单实例语义的 TimeoutCounterCache。
func NewMemoryTimeoutCounterCache(core *memoryCacheCore) service.TimeoutCounterCache {
	return &memoryTimeoutCounterCache{core: memoryAssertCore(core)}
}

var _ service.TimeoutCounterCache = (*memoryTimeoutCounterCache)(nil)

func (c *memoryTimeoutCounterCache) IncrementTimeoutCount(ctx context.Context, accountID int64, windowMinutes int) (int64, error) {
	ttlSeconds := windowMinutes * 60
	if ttlSeconds < 60 {
		ttlSeconds = 60
	}
	key := fmt.Sprintf("%s%d", timeoutCounterPrefix, accountID)
	return memoryIncrementFixedCounter(c.core, key, time.Duration(ttlSeconds)*time.Second)
}

func (c *memoryTimeoutCounterCache) GetTimeoutCount(ctx context.Context, accountID int64) (int64, error) {
	key := fmt.Sprintf("%s%d", timeoutCounterPrefix, accountID)
	value, ok := c.core.get(key)
	if !ok {
		return 0, nil
	}
	return memoryInt64(value), nil
}

func (c *memoryTimeoutCounterCache) ResetTimeoutCount(ctx context.Context, accountID int64) error {
	c.core.delete(fmt.Sprintf("%s%d", timeoutCounterPrefix, accountID))
	return nil
}

func (c *memoryTimeoutCounterCache) GetTimeoutCountTTL(ctx context.Context, accountID int64) (time.Duration, error) {
	key := fmt.Sprintf("%s%d", timeoutCounterPrefix, accountID)
	ttl, ok := c.core.ttl(key)
	if !ok {
		// Redis TTL 对不存在的键返回 -2。
		return -2, nil
	}
	return ttl, nil
}

// memoryInternal500CounterCache 存 24 小时兜底的连续 INTERNAL 500 计数。
type memoryInternal500CounterCache struct {
	core *memoryCacheCore
}

// NewMemoryInternal500CounterCache 返回单实例语义的 Internal500CounterCache。
func NewMemoryInternal500CounterCache(core *memoryCacheCore) service.Internal500CounterCache {
	return &memoryInternal500CounterCache{core: memoryAssertCore(core)}
}

var _ service.Internal500CounterCache = (*memoryInternal500CounterCache)(nil)

func (c *memoryInternal500CounterCache) IncrementInternal500Count(ctx context.Context, accountID int64) (int64, error) {
	key := fmt.Sprintf("%s%d", internal500CounterPrefix, accountID)
	return memoryIncrementFixedCounter(c.core, key, internal500CounterTTLSeconds*time.Second)
}

func (c *memoryInternal500CounterCache) ResetInternal500Count(ctx context.Context, accountID int64) error {
	c.core.delete(fmt.Sprintf("%s%d", internal500CounterPrefix, accountID))
	return nil
}

// memoryOpenAI403CounterCache 存按窗口过期的 OpenAI 403 计数。
type memoryOpenAI403CounterCache struct {
	core *memoryCacheCore
}

// NewMemoryOpenAI403CounterCache 返回单实例语义的 OpenAI403CounterCache。
func NewMemoryOpenAI403CounterCache(core *memoryCacheCore) service.OpenAI403CounterCache {
	return &memoryOpenAI403CounterCache{core: memoryAssertCore(core)}
}

var _ service.OpenAI403CounterCache = (*memoryOpenAI403CounterCache)(nil)

func (c *memoryOpenAI403CounterCache) IncrementOpenAI403Count(ctx context.Context, accountID int64, windowMinutes int) (int64, error) {
	ttlSeconds := windowMinutes * 60
	if ttlSeconds < 60 {
		ttlSeconds = 60
	}
	key := fmt.Sprintf("%s%d", openAI403CounterPrefix, accountID)
	return memoryIncrementFixedCounter(c.core, key, time.Duration(ttlSeconds)*time.Second)
}

func (c *memoryOpenAI403CounterCache) ResetOpenAI403Count(ctx context.Context, accountID int64) error {
	c.core.delete(fmt.Sprintf("%s%d", openAI403CounterPrefix, accountID))
	return nil
}

// ─── RPMCache / UserRPMCache ──────────────────────────────────────────────

// memoryMinuteClock 统一提供分钟桶时间源，让测试可以注入可控时钟。
type memoryMinuteClock struct {
	core *memoryCacheCore
}

// minute 取当前分钟时间戳。复用 core 的 now，这样 TTL 测试推进时钟时
// 分钟桶与条目过期保持一致。
func (m memoryMinuteClock) minute() int64 {
	return m.core.now().Unix() / 60
}

// memoryRPMCache 存账号级分钟 RPM 计数。
type memoryRPMCache struct {
	core  *memoryCacheCore
	clock memoryMinuteClock
}

// NewMemoryRPMCache 返回单实例语义的 RPMCache。
func NewMemoryRPMCache(core *memoryCacheCore) service.RPMCache {
	core = memoryAssertCore(core)
	return &memoryRPMCache{core: core, clock: memoryMinuteClock{core: core}}
}

var _ service.RPMCache = (*memoryRPMCache)(nil)

// memoryRPMKey 生成 rpm:{accountID}:{minute} 键，格式与 Redis 版一致。
func memoryRPMKey(accountID int64, minute int64) string {
	return fmt.Sprintf("%s%d:%d", rpmKeyPrefix, accountID, minute)
}

func (c *memoryRPMCache) IncrementRPM(ctx context.Context, accountID int64) (int, error) {
	key := memoryRPMKey(accountID, c.clock.minute())
	n, err := memoryIncrementCounter(c.core, key, rpmKeyTTL)
	return int(n), err
}

func (c *memoryRPMCache) GetRPM(ctx context.Context, accountID int64) (int, error) {
	key := memoryRPMKey(accountID, c.clock.minute())
	value, ok := c.core.get(key)
	if !ok {
		return 0, nil
	}
	return int(memoryInt64(value)), nil
}

func (c *memoryRPMCache) GetRPMBatch(ctx context.Context, accountIDs []int64) (map[int64]int, error) {
	if len(accountIDs) == 0 {
		return map[int64]int{}, nil
	}
	minute := c.clock.minute()
	result := make(map[int64]int, len(accountIDs))
	for _, id := range accountIDs {
		key := memoryRPMKey(id, minute)
		value, ok := c.core.get(key)
		if !ok {
			result[id] = 0
			continue
		}
		result[id] = int(memoryInt64(value))
	}
	return result, nil
}

// memoryUserRPMCache 存用户级与用户分组级分钟 RPM 计数。
type memoryUserRPMCache struct {
	core  *memoryCacheCore
	clock memoryMinuteClock
}

// NewMemoryUserRPMCache 返回单实例语义的 UserRPMCache。
func NewMemoryUserRPMCache(core *memoryCacheCore) service.UserRPMCache {
	core = memoryAssertCore(core)
	return &memoryUserRPMCache{core: core, clock: memoryMinuteClock{core: core}}
}

var _ service.UserRPMCache = (*memoryUserRPMCache)(nil)

func (c *memoryUserRPMCache) IncrementUserGroupRPM(ctx context.Context, userID, groupID int64) (int, error) {
	key := fmt.Sprintf("%s%d:%d:%d", userGroupRPMKeyPrefix, userID, groupID, c.clock.minute())
	n, err := memoryIncrementCounter(c.core, key, userRPMKeyTTL)
	return int(n), err
}

func (c *memoryUserRPMCache) IncrementUserRPM(ctx context.Context, userID int64) (int, error) {
	key := fmt.Sprintf("%s%d:%d", userRPMKeyPrefix, userID, c.clock.minute())
	n, err := memoryIncrementCounter(c.core, key, userRPMKeyTTL)
	return int(n), err
}

func (c *memoryUserRPMCache) GetUserGroupRPM(ctx context.Context, userID, groupID int64) (int, error) {
	key := fmt.Sprintf("%s%d:%d:%d", userGroupRPMKeyPrefix, userID, groupID, c.clock.minute())
	value, ok := c.core.get(key)
	if !ok {
		return 0, nil
	}
	return int(memoryInt64(value)), nil
}

func (c *memoryUserRPMCache) GetUserRPM(ctx context.Context, userID int64) (int, error) {
	key := fmt.Sprintf("%s%d:%d", userRPMKeyPrefix, userID, c.clock.minute())
	value, ok := c.core.get(key)
	if !ok {
		return 0, nil
	}
	return int(memoryInt64(value)), nil
}

// ─── GeminiTokenCache ─────────────────────────────────────────────────────

// memoryGeminiTokenCache 存 Gemini OAuth 访问令牌与刷新锁。
type memoryGeminiTokenCache struct {
	core *memoryCacheCore
}

// NewMemoryGeminiTokenCache 返回单实例语义的 GeminiTokenCache。
func NewMemoryGeminiTokenCache(core *memoryCacheCore) service.GeminiTokenCache {
	return &memoryGeminiTokenCache{core: memoryAssertCore(core)}
}

var _ service.GeminiTokenCache = (*memoryGeminiTokenCache)(nil)

func (c *memoryGeminiTokenCache) GetAccessToken(ctx context.Context, cacheKey string) (string, error) {
	value, ok := c.core.get(oauthTokenKeyPrefix + cacheKey)
	if !ok {
		return "", errMemoryCacheMiss
	}
	token, _ := value.(string)
	return token, nil
}

func (c *memoryGeminiTokenCache) SetAccessToken(ctx context.Context, cacheKey string, token string, ttl time.Duration) error {
	c.core.set(oauthTokenKeyPrefix+cacheKey, token, ttl)
	return nil
}

func (c *memoryGeminiTokenCache) DeleteAccessToken(ctx context.Context, cacheKey string) error {
	c.core.delete(oauthTokenKeyPrefix + cacheKey)
	return nil
}

func (c *memoryGeminiTokenCache) AcquireRefreshLock(ctx context.Context, cacheKey string, ttl time.Duration) (bool, error) {
	return memorySetNX(c.core, oauthRefreshLockKeyPrefix+cacheKey, 1, ttl), nil
}

func (c *memoryGeminiTokenCache) ReleaseRefreshLock(ctx context.Context, cacheKey string) error {
	c.core.delete(oauthRefreshLockKeyPrefix + cacheKey)
	return nil
}

// ─── TotpCache ────────────────────────────────────────────────────────────

// memoryTotpCache 存 TOTP 设置会话、登录会话、尝试次数与 step-up 授权。
type memoryTotpCache struct {
	core *memoryCacheCore
}

// NewMemoryTotpCache 返回单实例语义的 TotpCache。
func NewMemoryTotpCache(core *memoryCacheCore) service.TotpCache {
	return &memoryTotpCache{core: memoryAssertCore(core)}
}

var _ service.TotpCache = (*memoryTotpCache)(nil)

func (c *memoryTotpCache) GetSetupSession(ctx context.Context, userID int64) (*service.TotpSetupSession, error) {
	key := fmt.Sprintf("%s%d", totpSetupKeyPrefix, userID)
	value, ok := c.core.get(key)
	if !ok {
		return nil, nil
	}
	session, err := unmarshalMemoryJSON[service.TotpSetupSession](memoryBytes(value))
	if err != nil {
		return nil, fmt.Errorf("unmarshal setup session: %w", err)
	}
	return &session, nil
}

func (c *memoryTotpCache) SetSetupSession(ctx context.Context, userID int64, session *service.TotpSetupSession, ttl time.Duration) error {
	key := fmt.Sprintf("%s%d", totpSetupKeyPrefix, userID)
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal setup session: %w", err)
	}
	c.core.set(key, payload, ttl)
	return nil
}

func (c *memoryTotpCache) DeleteSetupSession(ctx context.Context, userID int64) error {
	c.core.delete(fmt.Sprintf("%s%d", totpSetupKeyPrefix, userID))
	return nil
}

func (c *memoryTotpCache) GetLoginSession(ctx context.Context, tempToken string) (*service.TotpLoginSession, error) {
	value, ok := c.core.get(totpLoginKeyPrefix + tempToken)
	if !ok {
		return nil, nil
	}
	session, err := unmarshalMemoryJSON[service.TotpLoginSession](memoryBytes(value))
	if err != nil {
		return nil, fmt.Errorf("unmarshal login session: %w", err)
	}
	return &session, nil
}

func (c *memoryTotpCache) SetLoginSession(ctx context.Context, tempToken string, session *service.TotpLoginSession, ttl time.Duration) error {
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal login session: %w", err)
	}
	c.core.set(totpLoginKeyPrefix+tempToken, payload, ttl)
	return nil
}

func (c *memoryTotpCache) DeleteLoginSession(ctx context.Context, tempToken string) error {
	c.core.delete(totpLoginKeyPrefix + tempToken)
	return nil
}

func (c *memoryTotpCache) IncrementVerifyAttempts(ctx context.Context, userID int64) (int, error) {
	key := fmt.Sprintf("%s%d", totpAttemptsKeyPrefix, userID)
	n, err := memoryIncrementCounter(c.core, key, totpAttemptsTTL)
	return int(n), err
}

func (c *memoryTotpCache) GetVerifyAttempts(ctx context.Context, userID int64) (int, error) {
	key := fmt.Sprintf("%s%d", totpAttemptsKeyPrefix, userID)
	value, ok := c.core.get(key)
	if !ok {
		return 0, nil
	}
	return int(memoryInt64(value)), nil
}

func (c *memoryTotpCache) ClearVerifyAttempts(ctx context.Context, userID int64) error {
	c.core.delete(fmt.Sprintf("%s%d", totpAttemptsKeyPrefix, userID))
	return nil
}

func (c *memoryTotpCache) SetStepUpGrant(ctx context.Context, userID int64, sessionKey string, ttl time.Duration) error {
	c.core.set(totpStepUpKey(userID, sessionKey), "1", ttl)
	return nil
}

func (c *memoryTotpCache) HasStepUpGrant(ctx context.Context, userID int64, sessionKey string) (bool, error) {
	_, ok := c.core.get(totpStepUpKey(userID, sessionKey))
	return ok, nil
}

// ─── DashboardStatsCache ──────────────────────────────────────────────────

// memoryDashboardStatsCache 缓存仪表盘聚合结果。
type memoryDashboardStatsCache struct {
	core  *memoryCacheCore
	build func() string
}

// NewMemoryDashboardCache 返回单实例语义的 DashboardStatsCache。
// cfg 为 nil 时沿用默认前缀，与 Redis 版行为一致。
func NewMemoryDashboardCache(core *memoryCacheCore, cfg *config.Config) service.DashboardStatsCache {
	prefix := "sub2api:"
	if cfg != nil {
		prefix = strings.TrimSpace(cfg.Dashboard.KeyPrefix)
	}
	if prefix != "" && !strings.HasSuffix(prefix, ":") {
		prefix += ":"
	}
	key := dashboardStatsCacheKey
	if prefix != "" {
		key = prefix + dashboardStatsCacheKey
	}
	return &memoryDashboardStatsCache{
		core:  memoryAssertCore(core),
		build: func() string { return key },
	}
}

var _ service.DashboardStatsCache = (*memoryDashboardStatsCache)(nil)

func (c *memoryDashboardStatsCache) GetDashboardStats(ctx context.Context) (string, error) {
	value, ok := c.core.get(c.build())
	if !ok {
		return "", service.ErrDashboardStatsCacheMiss
	}
	data, _ := value.(string)
	return data, nil
}

func (c *memoryDashboardStatsCache) SetDashboardStats(ctx context.Context, data string, ttl time.Duration) error {
	c.core.set(c.build(), data, ttl)
	return nil
}

func (c *memoryDashboardStatsCache) DeleteDashboardStats(ctx context.Context) error {
	c.core.delete(c.build())
	return nil
}

// ─── kv_store 持久化写穿 ─────────────────────────────────────────────────

// memoryKVBackend 是持久状态的 SQLite 写穿后端。
// 键空间与 Redis 版保持一致（因此进程内互相兼容），值统一以 JSON BLOB 落库。
type memoryKVBackend struct {
	core *memoryCacheCore
	db   memoryKVStore
}

// 保留给 kv_store 的写穿接口，便于与其他持久化缓存共享实现细节。
func (b *memoryKVBackend) put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if b.db == nil {
		return nil
	}
	var expiresAt any
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl).UnixMilli()
	}
	_, err := b.db.ExecContext(ctx,
		`INSERT INTO kv_store (key, value, expires_at, updated_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT(key) DO UPDATE SET
		   value = excluded.value,
		   expires_at = excluded.expires_at,
		   updated_at = excluded.updated_at`,
		key, value, expiresAt, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (b *memoryKVBackend) delete(ctx context.Context, keys ...string) error {
	if b.db == nil || len(keys) == 0 {
		return nil
	}
	placeholders := make([]string, len(keys))
	args := make([]any, len(keys))
	for i, key := range keys {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = key
	}
	_, err := b.db.ExecContext(ctx, `DELETE FROM kv_store WHERE key IN (`+strings.Join(placeholders, ",")+`)`, args...)
	return err
}

// Durable state is authoritative in SQL: a cached token must never survive revocation.
func (b *memoryKVBackend) get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.db == nil {
		if value, ok := b.core.get(key); ok {
			return append([]byte(nil), memoryBytes(value)...), nil
		}
		return nil, errMemoryCacheMiss
	}
	var (
		value     []byte
		expiresAt sql.NullInt64
	)
	row := b.db.QueryRowContext(ctx,
		`SELECT value, expires_at FROM kv_store WHERE key = $1`, key)
	if err := row.Scan(&value, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errMemoryCacheMiss
		}
		return nil, err
	}
	if expiresAt.Valid && time.Now().UnixMilli() >= expiresAt.Int64 {
		// Cleanup is separate; deleting here could erase a concurrent renewal.
		return nil, errMemoryCacheMiss
	}
	return value, nil
}

// set serializes structured state; plugin values use setBytes to stay byte-exact.
func (b *memoryKVBackend) set(ctx context.Context, key string, value any, ttl time.Duration) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return b.setBytes(ctx, key, payload, ttl)
}

func (b *memoryKVBackend) setBytes(ctx context.Context, key string, payload []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// The copy also prevents callers from mutating values after Set returns.
	payload = append([]byte{}, payload...)
	if err := b.put(ctx, key, payload, ttl); err != nil {
		return err
	}
	if b.db == nil {
		b.core.set(key, payload, ttl)
	} else {
		b.core.delete(key)
	}
	return nil
}

// remove 同时删内存与 kv_store。
func (b *memoryKVBackend) remove(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := b.delete(ctx, key); err != nil {
		return err
	}
	b.core.delete(key)
	return nil
}

func (b *memoryKVBackend) consume(ctx context.Context, key string) ([]byte, error) {
	if b.db == nil {
		mutex := memoryLockKey("consume:" + key)
		defer memoryUnlock(mutex)
		payload, err := b.get(ctx, key)
		if err != nil {
			return nil, err
		}
		b.core.delete(key)
		return payload, nil
	}
	var payload []byte
	var expiresAt sql.NullInt64
	err := b.db.QueryRowContext(ctx,
		`DELETE FROM kv_store WHERE key = $1 RETURNING value, expires_at`, key,
	).Scan(&payload, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errMemoryCacheMiss
	}
	if err != nil {
		return nil, err
	}
	b.core.delete(key)
	if expiresAt.Valid && expiresAt.Int64 <= time.Now().UnixMilli() {
		return nil, errMemoryCacheMiss
	}
	return payload, nil
}

// addMember 往持久集合里追加一个成员（刷新令牌的 token 集合用）。
func (b *memoryKVBackend) addMember(ctx context.Context, key string, member string, ttl time.Duration) error {
	mutex := memoryLockKey("set:" + key)
	defer memoryUnlock(mutex)
	values, err := b.members(ctx, key)
	if err != nil && !errors.Is(err, errMemoryCacheMiss) {
		return err
	}
	next := make(map[string]struct{}, len(values)+1)
	for _, value := range values {
		next[value] = struct{}{}
	}
	next[member] = struct{}{}
	// A shorter-lived new token must not expire the index of older live tokens.
	if b.db != nil {
		var expiresAt sql.NullInt64
		err := b.db.QueryRowContext(ctx, `SELECT expires_at FROM kv_store WHERE key = $1`, key).Scan(&expiresAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if !expiresAt.Valid {
				ttl = 0
			} else if remaining := time.Until(time.UnixMilli(expiresAt.Int64)); ttl > 0 && remaining > ttl {
				ttl = remaining
			}
		}
	} else if remaining, ok := b.core.ttl(key); ok {
		if remaining < 0 {
			ttl = 0
		} else if ttl > 0 && remaining > ttl {
			ttl = remaining
		}
	}
	return b.set(ctx, key, next, ttl)
}

// members 读取集合成员；内存里以 map[string]struct{} 存储。
func (b *memoryKVBackend) members(ctx context.Context, key string) ([]string, error) {
	payload, err := b.get(ctx, key)
	if err != nil {
		return nil, err
	}
	set, err := unmarshalMemoryJSON[map[string]struct{}](payload)
	if err != nil {
		return nil, err
	}
	members := make([]string, 0, len(set))
	for member := range set {
		members = append(members, member)
	}
	sortStrings(members)
	return members, nil
}

// sortStrings 就地排序，保持集合读取顺序稳定。
func sortStrings(values []string) {
	sort.Strings(values)
}

// WarmMemoryKVStore 启动时把 kv_store 中未过期的键预热进内存。
// 只做 best-effort：加载失败不应阻断启动，后续按需回源仍可工作。
func WarmMemoryKVStore(ctx context.Context, core *memoryCacheCore, db memoryKVStore) error {
	if core == nil || db == nil {
		return nil
	}
	now := time.Now().UnixMilli()
	rows, err := db.QueryContext(ctx, `SELECT key, value, expires_at FROM kv_store`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			key       string
			value     []byte
			expiresAt sql.NullInt64
		)
		if err := rows.Scan(&key, &value, &expiresAt); err != nil {
			return err
		}
		if expiresAt.Valid && now >= expiresAt.Int64 {
			continue
		}
		if expiresAt.Valid {
			ttl := time.Until(time.UnixMilli(expiresAt.Int64))
			if ttl <= 0 {
				continue
			}
			core.set(key, value, ttl)
		} else {
			core.set(key, value, 0)
		}
	}
	return rows.Err()
}

// ─── RefreshTokenCache ────────────────────────────────────────────────────

// memoryRefreshTokenCache 是刷新令牌的内存 + SQLite 写穿实现。
// 单实例下键锁足够保证令牌轮转的原子性；db 为 nil 时状态仅存活于进程内。
type memoryRefreshTokenCache struct {
	backend *memoryKVBackend
}

// NewMemoryRefreshTokenCache 返回单实例语义的 RefreshTokenCache。
// db 非 nil 时刷新令牌、用户令牌集合、家族令牌集合都会写穿 kv_store。
func NewMemoryRefreshTokenCache(core *memoryCacheCore, db *sql.DB) service.RefreshTokenCache {
	return &memoryRefreshTokenCache{
		backend: &memoryKVBackend{core: memoryAssertCore(core), db: memoryKVStoreFromDB(db)},
	}
}

var _ service.RefreshTokenCache = (*memoryRefreshTokenCache)(nil)

func memoryKVStoreFromDB(db *sql.DB) memoryKVStore {
	if db == nil {
		return nil
	}
	return db
}

func (c *memoryRefreshTokenCache) StoreRefreshToken(ctx context.Context, tokenHash string, data *service.RefreshTokenData, ttl time.Duration) error {
	return c.backend.set(ctx, refreshTokenKey(tokenHash), data, ttl)
}

func (c *memoryRefreshTokenCache) GetRefreshToken(ctx context.Context, tokenHash string) (*service.RefreshTokenData, error) {
	payload, err := c.backend.get(ctx, refreshTokenKey(tokenHash))
	if err != nil {
		if errors.Is(err, errMemoryCacheMiss) {
			return nil, service.ErrRefreshTokenNotFound
		}
		return nil, err
	}
	data, err := unmarshalMemoryJSON[service.RefreshTokenData](payload)
	if err != nil {
		return nil, fmt.Errorf("unmarshal refresh token data: %w", err)
	}
	return &data, nil
}

func (c *memoryRefreshTokenCache) DeleteRefreshToken(ctx context.Context, tokenHash string) error {
	return c.backend.remove(ctx, refreshTokenKey(tokenHash))
}

func (c *memoryRefreshTokenCache) DeleteUserRefreshTokens(ctx context.Context, userID int64) error {
	mutex := memoryLockKey("set:" + userRefreshTokensKey(userID))
	defer memoryUnlock(mutex)
	tokenHashes, err := c.GetUserTokenHashes(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user token hashes: %w", err)
	}
	if len(tokenHashes) == 0 {
		return nil
	}
	keys := make([]string, 0, len(tokenHashes)+1)
	for _, hash := range tokenHashes {
		keys = append(keys, refreshTokenKey(hash))
	}
	keys = append(keys, userRefreshTokensKey(userID))
	if err := c.backend.delete(ctx, keys...); err != nil {
		return err
	}
	for _, key := range keys {
		c.backend.core.delete(key)
	}
	return nil
}

func (c *memoryRefreshTokenCache) DeleteTokenFamily(ctx context.Context, familyID string) error {
	mutex := memoryLockKey("set:" + tokenFamilyKey(familyID))
	defer memoryUnlock(mutex)
	tokenHashes, err := c.GetFamilyTokenHashes(ctx, familyID)
	if err != nil {
		return fmt.Errorf("get family token hashes: %w", err)
	}
	if len(tokenHashes) == 0 {
		return nil
	}
	keys := make([]string, 0, len(tokenHashes)+1)
	for _, hash := range tokenHashes {
		keys = append(keys, refreshTokenKey(hash))
	}
	keys = append(keys, tokenFamilyKey(familyID))
	if err := c.backend.delete(ctx, keys...); err != nil {
		return err
	}
	for _, key := range keys {
		c.backend.core.delete(key)
	}
	return nil
}

func (c *memoryRefreshTokenCache) AddToUserTokenSet(ctx context.Context, userID int64, tokenHash string, ttl time.Duration) error {
	return c.backend.addMember(ctx, userRefreshTokensKey(userID), tokenHash, ttl)
}

func (c *memoryRefreshTokenCache) AddToFamilyTokenSet(ctx context.Context, familyID string, tokenHash string, ttl time.Duration) error {
	return c.backend.addMember(ctx, tokenFamilyKey(familyID), tokenHash, ttl)
}

func (c *memoryRefreshTokenCache) GetUserTokenHashes(ctx context.Context, userID int64) ([]string, error) {
	members, err := c.backend.members(ctx, userRefreshTokensKey(userID))
	if err != nil {
		if errors.Is(err, errMemoryCacheMiss) {
			return []string{}, nil
		}
		return nil, err
	}
	return members, nil
}

func (c *memoryRefreshTokenCache) GetFamilyTokenHashes(ctx context.Context, familyID string) ([]string, error) {
	members, err := c.backend.members(ctx, tokenFamilyKey(familyID))
	if err != nil {
		if errors.Is(err, errMemoryCacheMiss) {
			return []string{}, nil
		}
		return nil, err
	}
	return members, nil
}

func (c *memoryRefreshTokenCache) IsTokenInFamily(ctx context.Context, familyID string, tokenHash string) (bool, error) {
	members, err := c.GetFamilyTokenHashes(ctx, familyID)
	if err != nil {
		return false, err
	}
	for _, member := range members {
		if member == tokenHash {
			return true, nil
		}
	}
	return false, nil
}

// ─── PluginKVStore ────────────────────────────────────────────────────────

// memoryPluginKVStore 是插件通用键值存储的内存 + SQLite 写穿实现。
// List queries durable keys directly, independent of cache capacity or warmup.
type memoryPluginKVStore struct {
	backend *memoryKVBackend
}

// NewMemoryPluginKVStore 返回单实例语义的 PluginKVStore。
func NewMemoryPluginKVStore(core *memoryCacheCore, db *sql.DB) service.PluginKVStore {
	return &memoryPluginKVStore{
		backend: &memoryKVBackend{core: memoryAssertCore(core), db: memoryKVStoreFromDB(db)},
	}
}

var _ service.PluginKVStore = (*memoryPluginKVStore)(nil)

func (s *memoryPluginKVStore) namespacePrefix(pluginKey, namespace string) string {
	return pluginKVKeyPrefix + pluginKey + ":" + namespace + ":"
}

func (s *memoryPluginKVStore) fullKey(pluginKey, namespace, key string) string {
	return s.namespacePrefix(pluginKey, namespace) + key
}

func (s *memoryPluginKVStore) Get(ctx context.Context, pluginKey, namespace, key string) ([]byte, bool, error) {
	payload, err := s.backend.get(ctx, s.fullKey(pluginKey, namespace, key))
	if err != nil {
		if errors.Is(err, errMemoryCacheMiss) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("get plugin kv: %w", err)
	}
	return append([]byte(nil), payload...), true, nil
}

func (s *memoryPluginKVStore) Set(ctx context.Context, pluginKey, namespace, key string, value []byte, ttl time.Duration) error {
	if err := s.backend.setBytes(ctx, s.fullKey(pluginKey, namespace, key), value, ttl); err != nil {
		return fmt.Errorf("set plugin kv: %w", err)
	}
	return nil
}

func (s *memoryPluginKVStore) Delete(ctx context.Context, pluginKey, namespace, key string) error {
	if err := s.backend.remove(ctx, s.fullKey(pluginKey, namespace, key)); err != nil {
		return fmt.Errorf("delete plugin kv: %w", err)
	}
	return nil
}

func (s *memoryPluginKVStore) List(ctx context.Context, pluginKey, namespace, keyPrefix string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	internalPrefix := s.namespacePrefix(pluginKey, namespace)
	if s.backend.db != nil {
		prefix := internalPrefix + keyPrefix
		// Range bounds avoid LIKE wildcards and allow the primary-key index to be used.
		upper := []byte(prefix)
		upper[len(upper)-1]++
		rows, err := s.backend.db.QueryContext(ctx,
			`SELECT key FROM kv_store WHERE key >= $1 AND key < $2
			 AND (expires_at IS NULL OR expires_at > $3) ORDER BY key LIMIT $4`,
			prefix, string(upper), time.Now().UnixMilli(), limit)
		if err != nil {
			return nil, fmt.Errorf("list plugin kv: %w", err)
		}
		defer rows.Close()
		keys := make([]string, 0)
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				return nil, err
			}
			keys = append(keys, strings.TrimPrefix(key, internalPrefix))
		}
		return keys, rows.Err()
	}
	seen := make(map[string]struct{}, limit)
	keys := make([]string, 0, limit)
	// core.scan 只返回值不返回键名，因此这里直接遍历 LRU 取键名。
	s.backend.core.mu.Lock()
	for element := s.backend.core.lru.Front(); element != nil; element = element.Next() {
		entry, _ := element.Value.(*memoryCacheEntry)
		if entry == nil || s.backend.core.expiredLocked(entry) {
			continue
		}
		if !hasPrefix(entry.key, internalPrefix+keyPrefix) {
			continue
		}
		key := strings.TrimPrefix(entry.key, internalPrefix)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
		if len(keys) >= limit {
			break
		}
	}
	s.backend.core.mu.Unlock()
	return keys, nil
}

// ─── PasskeySessionStore ──────────────────────────────────────────────────

// memoryPasskeySessionStore 是一次性 Passkey 会话存储。
// 令牌本身随机生成、消费时立即删除，键锁保证只有唯一赢家能消费成功。
type memoryPasskeySessionStore struct {
	backend *memoryKVBackend
}

// NewMemoryPasskeySessionStore 返回单实例语义的 PasskeySessionStore。
// db 非 nil 时会话也写入 kv_store：认证跨实例升级窗口内仍可用。
func NewMemoryPasskeySessionStore(core *memoryCacheCore, db *sql.DB) service.PasskeySessionStore {
	return &memoryPasskeySessionStore{
		backend: &memoryKVBackend{core: memoryAssertCore(core), db: memoryKVStoreFromDB(db)},
	}
}

var _ service.PasskeySessionStore = (*memoryPasskeySessionStore)(nil)

func (s *memoryPasskeySessionStore) Store(
	ctx context.Context,
	session *service.PasskeySession,
	ttl time.Duration,
) (string, error) {
	if session == nil || ttl <= 0 {
		return "", fmt.Errorf("invalid passkey session")
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate passkey session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	payload, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("encode passkey session: %w", err)
	}
	if err = s.backend.setBytes(ctx, passkeySessionPrefix+token, payload, ttl); err != nil {
		return "", fmt.Errorf("store passkey session: %w", err)
	}
	return token, nil
}

func (s *memoryPasskeySessionStore) Consume(
	ctx context.Context,
	token string,
) (*service.PasskeySession, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 128 {
		return nil, service.ErrPasskeySession
	}
	key := passkeySessionPrefix + token
	payload, err := s.backend.consume(ctx, key)
	if err != nil {
		if errors.Is(err, errMemoryCacheMiss) {
			return nil, service.ErrPasskeySession
		}
		return nil, fmt.Errorf("consume passkey session: %w", err)
	}
	var session service.PasskeySession
	if err = json.Unmarshal(payload, &session); err != nil {
		return nil, service.ErrPasskeySession
	}
	return &session, nil
}

// ─── ImageTaskStore ───────────────────────────────────────────────────────

// memoryImageTaskStore 是异步图片任务的内存 + SQLite 写穿实现。
type memoryImageTaskStore struct {
	backend *memoryKVBackend
}

// NewMemoryImageTaskStore 返回单实例语义的 ImageTaskStore。
func NewMemoryImageTaskStore(core *memoryCacheCore, db *sql.DB) service.ImageTaskStore {
	return &memoryImageTaskStore{
		backend: &memoryKVBackend{core: memoryAssertCore(core), db: memoryKVStoreFromDB(db)},
	}
}

var _ service.ImageTaskStore = (*memoryImageTaskStore)(nil)

func (s *memoryImageTaskStore) Save(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration) error {
	if task == nil {
		return errors.New("nil image task")
	}
	payload, err := json.Marshal(task)
	if err != nil {
		return err
	}
	key := imageTaskKey(task.ID)
	return s.backend.setBytes(ctx, key, payload, ttl)
}

func (s *memoryImageTaskStore) Get(ctx context.Context, id string) (*service.ImageTaskRecord, error) {
	payload, err := s.backend.get(ctx, imageTaskKey(id))
	if err != nil {
		if errors.Is(err, errMemoryCacheMiss) {
			return nil, service.ErrImageTaskNotFound
		}
		return nil, err
	}
	var task service.ImageTaskRecord
	if err := json.Unmarshal(payload, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// ─── UserMsgQueueCache ────────────────────────────────────────────────────

// memoryUserMsgQueueCache 是账号级串行队列锁的内存实现。
// 锁索引改为进程内 map，reconcile 语义保持"到期候选二次确认"。
type memoryUserMsgQueueCache struct {
	core *memoryCacheCore

	mu        sync.Mutex
	lockIndex map[int64]int64 // accountID -> 锁预计过期时间（Unix 毫秒）
}

// NewMemoryUserMsgQueueCache 返回单实例语义的 UserMsgQueueCache。
func NewMemoryUserMsgQueueCache(core *memoryCacheCore) service.UserMsgQueueCache {
	return &memoryUserMsgQueueCache{
		core:      memoryAssertCore(core),
		lockIndex: make(map[int64]int64),
	}
}

var _ service.UserMsgQueueCache = (*memoryUserMsgQueueCache)(nil)

func (c *memoryUserMsgQueueCache) AcquireLock(ctx context.Context, accountID int64, requestID string, lockTtlMs int) (bool, error) {
	if lockTtlMs <= 0 {
		return false, errors.New("invalid umq lock ttl")
	}
	key := umqLockKey(accountID)
	mutex := memoryLockKey("umq:acquire:" + key)
	defer memoryUnlock(mutex)

	nowMs := time.Now().UnixMilli()
	if value, ok := c.core.get(key); ok {
		holder, _ := value.(string)
		if holder == requestID {
			// 重入：同 requestID 续租。
			c.core.set(key, requestID, time.Duration(lockTtlMs)*time.Millisecond)
			c.indexLock(accountID, nowMs+int64(lockTtlMs))
			return true, nil
		}
		if ttl, exists := c.core.ttl(key); exists && ttl > 0 {
			// 锁仍被他人持有，回填索引让 reconcile 能发现它。
			c.indexLock(accountID, nowMs+ttl.Milliseconds())
			return false, nil
		}
	}

	c.core.set(key, requestID, time.Duration(lockTtlMs)*time.Millisecond)
	c.indexLock(accountID, nowMs+int64(lockTtlMs))
	return true, nil
}

func (c *memoryUserMsgQueueCache) ReleaseLock(ctx context.Context, accountID int64, requestID string) (bool, error) {
	key := umqLockKey(accountID)
	mutex := memoryLockKey("umq:release:" + key)
	defer memoryUnlock(mutex)

	value, ok := c.core.get(key)
	if !ok {
		return false, nil
	}
	holder, _ := value.(string)
	if holder != requestID {
		return false, nil
	}
	c.core.delete(key)
	// 记录完成时间，让服务层据此计算串行间隔。
	c.core.set(umqLastKey(accountID), time.Now().UnixMilli(), 60*time.Second)
	c.removeLockIndex(accountID)
	return true, nil
}

func (c *memoryUserMsgQueueCache) GetLastCompletedMs(ctx context.Context, accountID int64) (int64, error) {
	value, ok := c.core.get(umqLastKey(accountID))
	if !ok {
		return 0, nil
	}
	return memoryInt64(value), nil
}

func (c *memoryUserMsgQueueCache) GetCurrentTimeMs(ctx context.Context) (int64, error) {
	return time.Now().UnixMilli(), nil
}

func (c *memoryUserMsgQueueCache) ReconcileExpiredLockCandidates(ctx context.Context, maxCount int) (int, error) {
	if maxCount <= 0 {
		maxCount = umqLockIndexCleanupBatchSize
	}
	nowMs := time.Now().UnixMilli()
	c.mu.Lock()
	candidates := make([]int64, 0, maxCount)
	for accountID, expireAtMs := range c.lockIndex {
		if expireAtMs > nowMs {
			continue
		}
		candidates = append(candidates, accountID)
		if len(candidates) >= maxCount {
			break
		}
	}
	c.mu.Unlock()

	cleaned := 0
	for _, accountID := range candidates {
		key := umqLockKey(accountID)
		ttl, exists := c.core.ttl(key)
		switch {
		case !exists:
			// 锁已自然过期或被释放，只需移除索引残留。
			c.removeLockIndex(accountID)
		case ttl < 0:
			// 无 TTL 的锁会永久阻塞队列，清理掉并统计一次。
			c.core.delete(key)
			c.removeLockIndex(accountID)
			cleaned++
		default:
			// 锁仍存活，说明索引滞后；按剩余 TTL 重新排期。
			c.indexLock(accountID, nowMs+ttl.Milliseconds())
		}
	}
	return cleaned, nil
}

func (c *memoryUserMsgQueueCache) indexLock(accountID, expireAtMs int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lockIndex[accountID] = expireAtMs
}

func (c *memoryUserMsgQueueCache) removeLockIndex(accountID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.lockIndex, accountID)
}
