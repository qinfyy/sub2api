package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newSQLiteAccountRepo(t *testing.T) (*accountRepository, context.Context) {
	t.Helper()
	cfg := config.NewDefaultConfig()
	cfg.Database.Type = config.DatabaseTypeSQLite
	cfg.Database.Path = filepath.Join(t.TempDir(), "accounts.db")
	cfg.Redis.Type = config.CacheTypeMemory
	client, db, err := InitEnt(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return newAccountRepositoryWithSQL(client, db, nil), context.Background()
}

func TestSQLiteUpdateCredentialsClearsManagedUsageState(t *testing.T) {
	repo, ctx := newSQLiteAccountRepo(t)
	account := &service.Account{
		Name:     "ollama",
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeAPIKey,
		Status:   service.StatusActive,
		Credentials: map[string]any{
			"api_key":  "old-key",
			"base_url": "https://ollama.com",
		},
		Extra: map[string]any{
			service.OllamaCloudUsageSessionExtraKey:     "session",
			service.OllamaCloudUsageAutoRefreshExtraKey: true,
			service.UpstreamBillingProbeExtraKey:        map[string]any{"status": "ok"},
			"keep":                                      "value",
		},
		Schedulable: true,
	}
	require.NoError(t, repo.Create(ctx, account))

	require.NoError(t, repo.UpdateCredentials(ctx, account.ID, map[string]any{
		"api_key":  "new-key",
		"base_url": "https://ollama.com",
	}))

	loaded, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, "new-key", loaded.Credentials["api_key"])
	require.Equal(t, "value", loaded.Extra["keep"])
	require.NotContains(t, loaded.Extra, service.OllamaCloudUsageSessionExtraKey)
	require.NotContains(t, loaded.Extra, service.OllamaCloudUsageAutoRefreshExtraKey)
	require.NotContains(t, loaded.Extra, service.UpstreamBillingProbeExtraKey)
}

func TestSQLiteLockAndMergeAccountProbeExtraPreservesMatchingIdentity(t *testing.T) {
	repo, ctx := newSQLiteAccountRepo(t)
	account := &service.Account{
		Name:     "ollama",
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeAPIKey,
		Status:   service.StatusActive,
		Credentials: map[string]any{
			"api_key":  "key",
			"base_url": "https://ollama.com",
		},
		Extra: map[string]any{
			service.OllamaCloudUsageSessionExtraKey:     "session",
			service.OllamaCloudUsageAutoRefreshExtraKey: true,
		},
		Schedulable: true,
	}
	require.NoError(t, repo.Create(ctx, account))

	merged, err := lockAndMergeAccountProbeExtra(ctx, repo.client, account, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "session", merged[service.OllamaCloudUsageSessionExtraKey])
	require.Equal(t, true, merged[service.OllamaCloudUsageAutoRefreshExtraKey])
}

func TestSQLiteListSchedulableCapacityByGroupIDs(t *testing.T) {
	groupRepo, accountRepo, ctx := newSQLiteGroupRepo(t)
	group := &service.Group{Name: "g", Platform: service.PlatformOpenAI, Status: service.StatusActive}
	require.NoError(t, groupRepo.Create(ctx, group))
	account := &service.Account{
		Name: "a", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 3,
		Credentials: map[string]any{"api_key": "key"},
		Extra:       map[string]any{"custom": "x"},
	}
	require.NoError(t, accountRepo.Create(ctx, account))
	require.NoError(t, accountRepo.BindGroups(ctx, account.ID, []int64{group.ID}))

	rows, err := accountRepo.ListSchedulableCapacityByGroupIDs(ctx, []int64{group.ID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, group.ID, rows[0].GroupID)
	require.Equal(t, account.ID, rows[0].AccountID)
	require.Equal(t, "x", rows[0].Extra["custom"])
}

func TestSQLiteSetModelRateLimitPersistsNestedJSON(t *testing.T) {
	repo, ctx := newSQLiteAccountRepo(t)
	account := &service.Account{
		Name: "a", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "key"},
		Extra:       map[string]any{"keep": "value"},
	}
	require.NoError(t, repo.Create(ctx, account))

	require.NoError(t, repo.SetModelRateLimit(ctx, account.ID, "gpt-4o", time.Now().Add(time.Hour)))
	loaded, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	limits, ok := loaded.Extra["model_rate_limits"].(map[string]any)
	require.True(t, ok)
	entry, ok := limits["gpt-4o"].(map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, entry["rate_limit_reset_at"])
	require.Equal(t, "value", loaded.Extra["keep"])
}

func TestSQLiteBatchUpdateLastUsed(t *testing.T) {
	repo, ctx := newSQLiteAccountRepo(t)
	account := &service.Account{
		Name: "a", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "key"},
	}
	require.NoError(t, repo.Create(ctx, account))
	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, repo.BatchUpdateLastUsed(ctx, map[int64]time.Time{account.ID: now}))
	loaded, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded.LastUsedAt)
	require.WithinDuration(t, now, *loaded.LastUsedAt, time.Millisecond)
}

func TestSQLiteListWithFiltersSortByUpstreamBillingRate(t *testing.T) {
	repo, ctx := newSQLiteAccountRepo(t)
	create := func(name, status string, rate any) {
		t.Helper()
		extra := map[string]any{}
		if rate != nil {
			extra[service.UpstreamBillingProbeExtraKey] = map[string]any{
				"status": status,
				"data": map[string]any{
					"effective_rate_multiplier": rate,
				},
			}
		}
		require.NoError(t, repo.Create(ctx, &service.Account{
			Name: name, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true,
			Credentials: map[string]any{"api_key": name},
			Extra:       extra,
		}))
	}
	create("high-rate", service.UpstreamBillingProbeStatusOK, 0.8)
	create("low-rate", service.UpstreamBillingProbeStatusOK, 0.03)
	create("missing-rate", "", nil)
	create("unsupported-rate", service.UpstreamBillingProbeStatusUnsupported, 0.01)

	for _, tc := range []struct {
		order string
		want  []string
	}{
		{order: "asc", want: []string{"low-rate", "high-rate", "missing-rate", "unsupported-rate"}},
		{order: "desc", want: []string{"high-rate", "low-rate", "unsupported-rate", "missing-rate"}},
	} {
		accounts, _, err := repo.ListWithFilters(ctx, pagination.PaginationParams{
			Page: 1, PageSize: 10, SortBy: "upstream_billing_rate", SortOrder: tc.order,
		}, "", "", "", "", 0, "")
		require.NoError(t, err)
		require.Len(t, accounts, 4)
		for i, want := range tc.want {
			require.Equal(t, want, accounts[i].Name)
		}
	}
}
