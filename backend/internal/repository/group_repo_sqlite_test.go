package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newSQLiteGroupRepo(t *testing.T) (*groupRepository, *accountRepository, context.Context) {
	t.Helper()
	cfg := config.NewDefaultConfig()
	cfg.Database.Type = config.DatabaseTypeSQLite
	cfg.Database.Path = filepath.Join(t.TempDir(), "groups.db")
	cfg.Redis.Type = config.CacheTypeMemory
	client, db, err := InitEnt(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	groupRepo := newGroupRepositoryWithSQL(client, db)
	accountRepo := newAccountRepositoryWithSQL(client, db, nil)
	return groupRepo, accountRepo, context.Background()
}

func TestSQLiteGroupAccountCountsAndBindings(t *testing.T) {
	groupRepo, accountRepo, ctx := newSQLiteGroupRepo(t)

	group := &service.Group{Name: "g1", Platform: service.PlatformAnthropic, Status: service.StatusActive}
	require.NoError(t, groupRepo.Create(ctx, group))

	account := &service.Account{Name: "a1", Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true}
	require.NoError(t, accountRepo.Create(ctx, account))

	require.NoError(t, accountRepo.BindGroups(ctx, account.ID, []int64{group.ID}))

	total, active, err := groupRepo.GetAccountCount(ctx, group.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, int64(1), active)

	ids, err := groupRepo.GetAccountIDsByGroupIDs(ctx, []int64{group.ID})
	require.NoError(t, err)
	require.Equal(t, []int64{account.ID}, ids)

	counts, err := groupRepo.loadAccountCounts(ctx, []int64{group.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), counts[group.ID].Total)
	require.Equal(t, int64(1), counts[group.ID].Active)
}

func TestSQLiteGroupCascadeDelete(t *testing.T) {
	groupRepo, accountRepo, ctx := newSQLiteGroupRepo(t)

	group := &service.Group{Name: "g2", Platform: service.PlatformAnthropic, Status: service.StatusActive}
	require.NoError(t, groupRepo.Create(ctx, group))
	account := &service.Account{Name: "a2", Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true}
	require.NoError(t, accountRepo.Create(ctx, account))
	require.NoError(t, accountRepo.BindGroups(ctx, account.ID, []int64{group.ID}))

	_, err := groupRepo.DeleteCascade(ctx, group.ID)
	require.NoError(t, err)

	var groupCount int
	require.NoError(t, groupRepo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM groups WHERE id = ? AND deleted_at IS NULL`, group.ID).Scan(&groupCount))
	require.Equal(t, 0, groupCount)

	var bindingCount int
	require.NoError(t, groupRepo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_groups WHERE group_id = ?`, group.ID).Scan(&bindingCount))
	require.Equal(t, 0, bindingCount)
}

func TestSQLiteGroupUpdateSortOrders(t *testing.T) {
	groupRepo, _, ctx := newSQLiteGroupRepo(t)

	g1 := &service.Group{Name: "s1", Platform: service.PlatformAnthropic, Status: service.StatusActive}
	g2 := &service.Group{Name: "s2", Platform: service.PlatformAnthropic, Status: service.StatusActive}
	require.NoError(t, groupRepo.Create(ctx, g1))
	require.NoError(t, groupRepo.Create(ctx, g2))

	require.NoError(t, groupRepo.UpdateSortOrders(ctx, []service.GroupSortOrderUpdate{
		{ID: g1.ID, SortOrder: 10},
		{ID: g2.ID, SortOrder: 20},
	}))

	var order1, order2 int
	require.NoError(t, groupRepo.db.QueryRowContext(ctx, `SELECT sort_order FROM groups WHERE id = ?`, g1.ID).Scan(&order1))
	require.NoError(t, groupRepo.db.QueryRowContext(ctx, `SELECT sort_order FROM groups WHERE id = ?`, g2.ID).Scan(&order2))
	require.Equal(t, 10, order1)
	require.Equal(t, 20, order2)
}
