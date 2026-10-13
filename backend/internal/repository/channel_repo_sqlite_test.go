package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newSQLiteChannelRepo(t *testing.T) (*channelRepository, *groupRepository, context.Context) {
	t.Helper()
	cfg := config.NewDefaultConfig()
	cfg.Database.Type = config.DatabaseTypeSQLite
	cfg.Database.Path = filepath.Join(t.TempDir(), "channels.db")
	cfg.Redis.Type = config.CacheTypeMemory
	client, db, err := InitEnt(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	return &channelRepository{db: db, sqlite: true}, newGroupRepositoryWithSQL(client, db), context.Background()
}

func TestSQLiteChannelCRUDWithPricingAndGroups(t *testing.T) {
	repo, groupRepo, ctx := newSQLiteChannelRepo(t)

	group := &service.Group{Name: "channel-group", Platform: service.PlatformAnthropic, Status: service.StatusActive}
	require.NoError(t, groupRepo.Create(ctx, group))

	description := "channel description"
	channel := &service.Channel{
		Name:               "channel-one",
		Description:        description,
		Status:             service.StatusActive,
		BillingModelSource: service.BillingModelSourceChannelMapped,
		RestrictModels:     true,
		Features:           `["web_search"]`,
		FeaturesConfig:     map[string]any{"web_search_emulation": true},
		GroupIDs:           []int64{group.ID},
		ModelMapping:       map[string]map[string]string{"anthropic": {"claude-old": "claude-new"}},
		ModelPricing: []service.ChannelModelPricing{
			{
				Platform:                   service.PlatformAnthropic,
				Models:                     []string{"claude-sonnet-4"},
				BillingMode:                service.BillingModeToken,
				CacheWrite1hPrice:          ptrFloat64(0.25),
				ReasoningEffortMultipliers: map[string]float64{"max": 1.5},
				Intervals: []service.PricingInterval{
					{MinTokens: 0, InputMultiplier: ptrFloat64(1.1), SortOrder: 0},
				},
			},
		},
		ApplyPricingToAccountStats: true,
		AccountStatsPricingRules: []service.AccountStatsPricingRule{
			{
				Name:       "stats-rule",
				GroupIDs:   []int64{group.ID},
				AccountIDs: []int64{42},
				SortOrder:  0,
				Pricing: []service.ChannelModelPricing{
					{
						Platform:        service.PlatformAnthropic,
						Models:          []string{"claude-sonnet-4"},
						BillingMode:     service.BillingModeToken,
						InputPrice:      ptrFloat64(0.001),
						OutputPrice:     ptrFloat64(0.002),
						PerRequestPrice: ptrFloat64(0.003),
					},
				},
			},
		},
	}

	require.NoError(t, repo.Create(ctx, channel))
	require.NotZero(t, channel.ID)

	got, err := repo.GetByID(ctx, channel.ID)
	require.NoError(t, err)
	require.Equal(t, channel.Name, got.Name)
	require.Equal(t, description, got.Description)
	require.Equal(t, []int64{group.ID}, got.GroupIDs)
	require.Len(t, got.ModelPricing, 1)
	require.Len(t, got.ModelPricing[0].Intervals, 1)
	require.Equal(t, 1.1, *got.ModelPricing[0].Intervals[0].InputMultiplier)
	require.Equal(t, map[string]float64{"max": 1.5}, got.ModelPricing[0].ReasoningEffortMultipliers)
	require.Len(t, got.AccountStatsPricingRules, 1)
	require.Equal(t, []int64{group.ID}, got.AccountStatsPricingRules[0].GroupIDs)
	require.Equal(t, []int64{42}, got.AccountStatsPricingRules[0].AccountIDs)
	require.Len(t, got.AccountStatsPricingRules[0].Pricing, 1)

	listed, paginationResult, err := repo.List(ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, "", "")
	require.NoError(t, err)
	require.Equal(t, int64(1), paginationResult.Total)
	require.Len(t, listed, 1)
	require.Equal(t, channel.ID, listed[0].ID)

	channel.Name = "channel-one-updated"
	channel.GroupIDs = nil
	require.NoError(t, repo.Update(ctx, channel))
	got, err = repo.GetByID(ctx, channel.ID)
	require.NoError(t, err)
	require.Equal(t, "channel-one-updated", got.Name)

	require.NoError(t, repo.Delete(ctx, channel.ID))
	_, err = repo.GetByID(ctx, channel.ID)
	require.ErrorIs(t, err, service.ErrChannelNotFound)
}

func ptrFloat64(value float64) *float64 {
	return &value
}
