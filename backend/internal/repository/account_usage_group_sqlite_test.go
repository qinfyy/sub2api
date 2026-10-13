package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSQLiteOllamaCloudUsageGroupLifecycle(t *testing.T) {
	repo, ctx := newSQLiteAccountRepo(t)
	anchor := &service.Account{
		Name: "ollama", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "shared-key", "base_url": "https://ollama.com"},
		Extra: map[string]any{
			service.OllamaCloudUsageSessionExtraKey:     "cipher:one",
			service.OllamaCloudUsageAutoRefreshExtraKey: true,
		},
	}
	sibling := &service.Account{
		Name: "ollama-sibling", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "shared-key", "base_url": "https://www.ollama.com:443/v1"},
		Extra:       map[string]any{"keep": "yes"},
	}
	require.NoError(t, repo.Create(ctx, anchor))
	require.NoError(t, repo.Create(ctx, sibling))

	group, err := repo.ListOllamaCloudUsageGroupAccounts(ctx, []*service.Account{anchor})
	require.NoError(t, err)
	require.Len(t, group, 2)

	snapshot := &service.OllamaCloudUsageSnapshot{
		Status:        service.OllamaCloudUsageStatusOK,
		LastAttemptAt: ctxTime(),
		NextRefreshAt: ctxTime().Add(time.Hour),
	}
	require.NoError(t, repo.UpdateOllamaCloudUsageSnapshot(ctx, anchor, snapshot))

	loaded, err := repo.GetByID(ctx, sibling.ID)
	require.NoError(t, err)
	require.Equal(t, "yes", loaded.Extra["keep"])
	require.NotNil(t, loaded.Extra[service.OllamaCloudUsageSnapshotExtraKey])
	require.Equal(t, true, loaded.Extra[service.OllamaCloudUsageAutoRefreshExtraKey])
}

func TestSQLiteOpenCodeGoUsageGroupLifecycle(t *testing.T) {
	repo, ctx := newSQLiteAccountRepo(t)
	anchor := &service.Account{
		Name: "opencode", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "shared-key", "base_url": "https://opencode.ai/zen/go/v1"},
		Extra: map[string]any{
			service.OpenCodeGoUsageAutoRefreshExtraKey: true,
		},
	}
	sibling := &service.Account{
		Name: "opencode-sibling", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "shared-key", "base_url": "https://opencode.ai:443/zen/go"},
		Extra:       map[string]any{"keep": "yes"},
	}
	require.NoError(t, repo.Create(ctx, anchor))
	require.NoError(t, repo.Create(ctx, sibling))

	group, err := repo.ListOpenCodeGoUsageGroupAccounts(ctx, []*service.Account{anchor})
	require.NoError(t, err)
	require.Len(t, group, 2)

	snapshot := &service.OpenCodeGoUsageSnapshot{
		Status:        service.OpenCodeGoUsageStatusOK,
		LastAttemptAt: ctxTime(),
		NextRefreshAt: ctxTime().Add(time.Hour),
	}
	require.NoError(t, repo.UpdateOpenCodeGoUsageSnapshot(ctx, anchor, snapshot))

	loaded, err := repo.GetByID(ctx, sibling.ID)
	require.NoError(t, err)
	require.Equal(t, "yes", loaded.Extra["keep"])
	require.NotNil(t, loaded.Extra[service.OpenCodeGoUsageSnapshotExtraKey])
	require.NotContains(t, loaded.Extra, service.OpenCodeGoUsageAutoRefreshExtraKey)
}

func TestSQLiteOpenCodeGoUsageGroupIdentityCAS(t *testing.T) {
	repo, ctx := newSQLiteAccountRepo(t)
	account := &service.Account{
		Name: "opencode", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "shared-key", "base_url": "https://opencode.ai/zen/go/v1"},
		Extra:       map[string]any{service.OpenCodeGoUsageAutoRefreshExtraKey: true},
	}
	require.NoError(t, repo.Create(ctx, account))

	require.NoError(t, repo.UpdateCredentials(ctx, account.ID, map[string]any{
		"api_key": "shared-key", "base_url": "https://opencode.ai/zen/go/v1",
	}))
	loaded, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, true, loaded.Extra[service.OpenCodeGoUsageAutoRefreshExtraKey])
}

func ctxTime() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }
