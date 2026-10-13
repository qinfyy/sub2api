package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSQLiteProxyIdentityAndExpiry(t *testing.T) {
	usage, _, _, _, ctx := newSQLiteUsageLogStatsRepo(t)
	r := NewProxyRepository(usage.client, usage.db).(*proxyRepository)
	now := time.Now()
	expires := now.Add(time.Hour)
	p := &service.Proxy{Name: "proxy", Protocol: "http", Host: "localhost", Port: 8081, Status: service.StatusActive, FallbackMode: service.FallbackModeDirect, ExpiryWarnDays: 1, ExpiresAt: &expires}
	require.NoError(t, r.Create(ctx, p))
	a, err := usage.client.Account.Create().SetName("proxy-account").SetPlatform("openai").SetType("apikey").SetProxyID(p.ID).SetExtra(map[string]any{"upstream_billing_probe": map[string]any{"balance": 1}, "unrelated": "retained"}).Save(ctx)
	require.NoError(t, err)
	count, err := r.CountExpiringSoon(ctx, now)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	p.Host = "127.0.0.1"
	require.NoError(t, r.Update(ctx, p))
	loaded, err := usage.client.Account.Get(ctx, a.ID)
	require.NoError(t, err)
	require.NotContains(t, loaded.Extra, "upstream_billing_probe")
	require.Equal(t, "retained", loaded.Extra["unrelated"])
	count, err = r.SweepExpiredProxies(ctx, now.Add(2*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	loaded, err = usage.client.Account.Get(ctx, a.ID)
	require.NoError(t, err)
	require.Nil(t, loaded.ProxyID)
	require.Equal(t, p.ID, *loaded.ProxyFallbackOriginID)
	count, err = r.SweepExpiredProxies(ctx, now.Add(2*time.Hour))
	require.NoError(t, err)
	require.Zero(t, count)

	// A stale expiry snapshot must not overwrite an administrator's renewal.
	expires = now.Add(24 * time.Hour)
	p.ExpiresAt = &expires
	p.Status = service.StatusActive
	require.NoError(t, r.Update(ctx, p))
	stale := *p
	oldExpiry := now.Add(-time.Hour)
	stale.ExpiresAt = &oldExpiry
	ids, err := r.sweepOneExpiredProxy(ctx, stale, now, nil, true)
	require.NoError(t, err)
	require.Empty(t, ids)
	current, err := r.GetByID(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusActive, current.Status)
}
