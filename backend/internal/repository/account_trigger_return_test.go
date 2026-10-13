package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSQLiteAccountRepositoryReturnsTriggerNormalizedExtra(t *testing.T) {
	repo, ctx := newSQLiteAccountRepo(t)
	account := &service.Account{
		Name: "parent", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Credentials: map[string]any{"api_key": "parent"},
	}
	require.NoError(t, repo.Create(ctx, account))
	require.Equal(t, false, account.Extra["openai_long_context_billing_enabled"])
	account.Extra["openai_long_context_billing_enabled"] = true
	require.NoError(t, repo.Update(ctx, account))
	account.Extra = map[string]any{"other": "preserved"}
	require.NoError(t, repo.Update(ctx, account))
	require.Equal(t, true, account.Extra["openai_long_context_billing_enabled"], "missing flag retains the old database value")
	child := &service.Account{
		Name: "child", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Credentials: map[string]any{},
		ParentAccountID: &account.ID, QuotaDimension: "spark",
	}
	require.NoError(t, repo.CreateWithAccountGroups(ctx, child, nil))
	require.Equal(t, true, child.Extra["openai_long_context_billing_enabled"])
	child.Extra = map[string]any{"openai_long_context_billing_enabled": false}
	require.NoError(t, repo.Update(ctx, child))
	require.Equal(t, true, child.Extra["openai_long_context_billing_enabled"], "child returns inherited, not submitted value")
}

func TestSQLiteAccountCreateAndOutboxAreAtomic(t *testing.T) {
	repo, ctx := newSQLiteAccountRepo(t)
	_, err := repo.db.Exec(`CREATE TRIGGER reject_account_outbox BEFORE INSERT ON scheduler_outbox
BEGIN SELECT RAISE(ABORT,'outbox unavailable'); END`)
	require.NoError(t, err)
	account := &service.Account{
		Name: "rollback", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Credentials: map[string]any{},
	}
	require.ErrorContains(t, repo.Create(ctx, account), "outbox unavailable")
	require.Zero(t, account.ID, "failed create must not publish an ID")
	require.Zero(t, repo.client.Account.Query().CountX(ctx))
}

func TestSQLiteAccountCreateReusesOuterTransaction(t *testing.T) {
	for _, withGroups := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "with-groups"}[withGroups], func(t *testing.T) {
			repo, _ := newSQLiteAccountRepo(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err := NewTransactionRunner(repo.client).WithinTransaction(ctx, func(ctx context.Context) error {
				account := &service.Account{
					Name: "outer", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
					Status: service.StatusActive, Credentials: map[string]any{},
				}
				if withGroups {
					require.NoError(t, repo.CreateWithAccountGroups(ctx, account, nil))
				} else {
					require.NoError(t, repo.Create(ctx, account))
				}
				require.Equal(t, false, account.Extra["openai_long_context_billing_enabled"])
				return context.Canceled
			})
			require.ErrorIs(t, err, context.Canceled)
			require.Zero(t, repo.client.Account.Query().CountX(ctx))
			var events int
			require.NoError(t, repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_outbox`).Scan(&events))
			require.Zero(t, events)
		})
	}
}
