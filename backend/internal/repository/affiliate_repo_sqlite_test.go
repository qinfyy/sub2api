package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSQLiteAffiliateAccrualThawTransferAndWithdraw(t *testing.T) {
	usage, userID, _, _, ctx := newSQLiteUsageLogStatsRepo(t)
	r := NewAffiliateRepository(usage.client, usage.db)
	summary, err := r.EnsureUserAffiliate(ctx, userID)
	require.NoError(t, err)
	require.NotEmpty(t, summary.AffCode)
	invitee, err := usage.client.User.Create().SetEmail("invitee@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	bound, err := r.BindInviter(ctx, invitee.ID, userID)
	require.NoError(t, err)
	require.True(t, bound)
	_, err = r.AccrueQuota(ctx, userID, invitee.ID, 3, 1, nil)
	require.NoError(t, err)
	thawed, err := r.ThawFrozenQuota(ctx, userID)
	require.NoError(t, err)
	require.Zero(t, thawed)
	_, err = usage.db.ExecContext(ctx, `UPDATE user_affiliate_ledger SET frozen_until=? WHERE user_id=?`, time.Now().UTC().Add(-time.Minute), userID)
	require.NoError(t, err)
	thawed, err = r.ThawFrozenQuota(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, 3.0, thawed)
	_, err = r.WithdrawQuota(ctx, userID, 1, "test-withdraw")
	require.NoError(t, err)
	replay, err := r.WithdrawQuota(ctx, userID, 1, "test-withdraw")
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	transferred, balance, err := r.TransferQuotaToBalance(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, 2.0, transferred)
	require.Equal(t, 2.0, balance)
	_, _, err = r.TransferQuotaToBalance(ctx, userID)
	require.ErrorIs(t, err, service.ErrAffiliateQuotaEmpty)
	rate := 10.0
	require.NoError(t, r.BatchSetUserRebateRate(ctx, []int64{userID, invitee.ID}, &rate))
	_, err = r.GetAffiliateUserOverview(ctx, userID)
	require.NoError(t, err)
	_, _, err = r.ListUsersWithCustomSettings(ctx, service.AffiliateAdminFilter{Search: "EXAMPLE"})
	require.NoError(t, err)
	filter := service.AffiliateRecordFilter{Page: 1, PageSize: 10}
	_, _, err = r.ListAffiliateInviteRecords(ctx, filter)
	require.NoError(t, err)
	_, _, err = r.ListAffiliateRebateRecords(ctx, filter)
	require.NoError(t, err)
	_, _, err = r.ListAffiliateTransferRecords(ctx, filter)
	require.NoError(t, err)
}
