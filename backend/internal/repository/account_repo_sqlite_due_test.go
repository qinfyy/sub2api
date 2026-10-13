package repository

import (
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func sqliteDueAccountIDs(accounts []service.Account) []int64 {
	ids := make([]int64, 0, len(accounts))
	for i := range accounts {
		ids = append(ids, accounts[i].ID)
	}
	return ids
}

func TestSQLiteListDueUpstreamBillingProbeAccounts(t *testing.T) {
	repo, ctx := newSQLiteAccountRepo(t)
	now := time.Date(2026, time.July, 25, 17, 30, 0, 0, time.UTC)

	create := func(name, nextProbeAt string) *service.Account {
		t.Helper()
		account := &service.Account{
			Name: name, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true,
			Credentials: map[string]any{"api_key": name},
			Extra: map[string]any{
				service.UpstreamBillingProbeEnabledExtraKey: true,
				service.UpstreamBillingProbeExtraKey: map[string]any{
					"status":        "ok",
					"next_probe_at": nextProbeAt,
				},
			},
		}
		require.NoError(t, repo.Create(ctx, account))
		return account
	}

	invalid := create("probe-invalid", "2026-99-99T12:00:00Z")
	dueLater := create("probe-due-later", "2026-07-25T17:29:00.123456789Z")
	dueEarlier := create("probe-due-earlier", "2026-07-25T17:28:00.12345678Z")
	_ = create("probe-not-due", "2026-07-25T17:31:00Z")

	accounts, err := repo.ListDueUpstreamBillingProbeAccounts(ctx, now, 20)
	require.NoError(t, err)
	require.Equal(t, []int64{invalid.ID, dueEarlier.ID, dueLater.ID}, sqliteDueAccountIDs(accounts))
}

func TestSQLiteListDueUpstreamBillingProbeAccountsLimitUsesDueTime(t *testing.T) {
	repo, ctx := newSQLiteAccountRepo(t)
	now := time.Date(2026, time.July, 25, 17, 30, 0, 0, time.UTC)
	ids := make([]int64, 0, 25)
	for i := 0; i < 25; i++ {
		due := now.Add(-time.Duration(i+1) * time.Minute)
		account := &service.Account{
			Name:     fmt.Sprintf("probe-order-%02d", i),
			Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true,
			Credentials: map[string]any{"api_key": fmt.Sprintf("probe-order-%02d", i)},
			Extra: map[string]any{
				service.UpstreamBillingProbeEnabledExtraKey: true,
				service.UpstreamBillingProbeExtraKey: map[string]any{
					"status":        "ok",
					"next_probe_at": due.UTC().Format(time.RFC3339Nano),
				},
			},
		}
		require.NoError(t, repo.Create(ctx, account))
		ids = append(ids, account.ID)
	}

	accounts, err := repo.ListDueUpstreamBillingProbeAccounts(ctx, now, 20)
	require.NoError(t, err)
	require.Len(t, accounts, 20)
	want := make([]int64, 0, 20)
	for i := 24; i >= 5; i-- {
		want = append(want, ids[i])
	}
	require.Equal(t, want, sqliteDueAccountIDs(accounts))
}

func TestSQLiteListDueOllamaCloudUsageAccounts(t *testing.T) {
	repo, ctx := newSQLiteAccountRepo(t)
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	fetched := now.Add(-2 * time.Hour)
	activity := now.Add(-5 * time.Minute)

	create := func(name, apiKey, baseURL string, lastUsed *time.Time, snapshot map[string]any) *service.Account {
		t.Helper()
		extra := map[string]any{
			service.OllamaCloudUsageSessionExtraKey:     "cipher:wos-session=fixture",
			service.OllamaCloudUsageAutoRefreshExtraKey: true,
		}
		if snapshot != nil {
			extra[service.OllamaCloudUsageSnapshotExtraKey] = snapshot
		}
		account := &service.Account{
			Name: name, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true,
			Credentials: map[string]any{"api_key": apiKey, "base_url": baseURL},
			Extra:       extra, LastUsedAt: lastUsed,
		}
		require.NoError(t, repo.Create(ctx, account))
		return account
	}

	missing := create("ollama-missing", "missing", "https://ollama.com", nil, nil)
	due := create("ollama-due", "due", "https://ollama.com", &activity, map[string]any{
		"status":          service.OllamaCloudUsageStatusOK,
		"fetched_at":      fetched.UTC().Format(time.RFC3339Nano),
		"last_attempt_at": fetched.UTC().Format(time.RFC3339Nano),
		"next_refresh_at": fetched.Add(time.Hour).UTC().Format(time.RFC3339Nano),
	})
	_ = create("ollama-idle", "idle", "https://ollama.com", nil, map[string]any{
		"status":          service.OllamaCloudUsageStatusOK,
		"fetched_at":      now.Add(-time.Hour).UTC().Format(time.RFC3339Nano),
		"last_attempt_at": now.Add(-time.Hour).UTC().Format(time.RFC3339Nano),
		"next_refresh_at": now.Add(-time.Minute).UTC().Format(time.RFC3339Nano),
	})

	accounts, err := repo.ListDueOllamaCloudUsageAccounts(ctx, now, time.Minute, time.Hour, 10)
	require.NoError(t, err)
	require.Equal(t, []int64{missing.ID, due.ID}, sqliteDueAccountIDs(accounts))
	require.NotNil(t, accounts[1].LastUsedAt)
	require.WithinDuration(t, activity, accounts[1].LastUsedAt.UTC(), time.Millisecond)
}

func TestSQLiteListDueOpenCodeGoUsageAccountsGroupsByAPIKey(t *testing.T) {
	repo, ctx := newSQLiteAccountRepo(t)
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	fetched := now.Add(-2 * time.Hour)
	older := now.Add(-10 * time.Minute)
	newer := now.Add(-2 * time.Minute)

	leader := &service.Account{
		Name: "opencode-leader", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "shared-key", "base_url": "https://opencode.ai/zen/go/v1"},
		Extra: map[string]any{
			service.OpenCodeGoUsageAutoRefreshExtraKey: true,
			service.OpenCodeGoUsageSnapshotExtraKey: map[string]any{
				"status":          service.OpenCodeGoUsageStatusOK,
				"fetched_at":      fetched.UTC().Format(time.RFC3339Nano),
				"last_attempt_at": fetched.UTC().Format(time.RFC3339Nano),
				"next_refresh_at": fetched.Add(time.Hour).UTC().Format(time.RFC3339Nano),
			},
		},
		LastUsedAt: &older,
	}
	require.NoError(t, repo.Create(ctx, leader))
	sibling := &service.Account{
		Name: "opencode-sibling", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "shared-key", "base_url": "https://opencode.ai/zen/go"},
		LastUsedAt:  &newer,
	}
	require.NoError(t, repo.Create(ctx, sibling))

	accounts, err := repo.ListDueOpenCodeGoUsageAccounts(ctx, now, time.Minute, time.Hour, 10)
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.Equal(t, leader.ID, accounts[0].ID)
	require.NotNil(t, accounts[0].LastUsedAt)
	require.WithinDuration(t, newer, accounts[0].LastUsedAt.UTC(), time.Millisecond)
}

func TestSQLiteListDueUsageAccountsInvalidSnapshotFailsOpen(t *testing.T) {
	repo, ctx := newSQLiteAccountRepo(t)
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	account := &service.Account{
		Name: "ollama-invalid-snapshot", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "invalid-key", "base_url": "https://ollama.com"},
		Extra: map[string]any{
			service.OllamaCloudUsageSessionExtraKey:     "cipher:wos-session=fixture",
			service.OllamaCloudUsageAutoRefreshExtraKey: true,
			service.OllamaCloudUsageSnapshotExtraKey: map[string]any{
				"status":     service.OllamaCloudUsageStatusOK,
				"fetched_at": "2026-02-30T09:00:00.123456789Z",
			},
		},
	}
	require.NoError(t, repo.Create(ctx, account))

	accounts, err := repo.ListDueOllamaCloudUsageAccounts(ctx, now, time.Minute, time.Hour, 10)
	require.NoError(t, err)
	require.Equal(t, []int64{account.ID}, sqliteDueAccountIDs(accounts))
}
