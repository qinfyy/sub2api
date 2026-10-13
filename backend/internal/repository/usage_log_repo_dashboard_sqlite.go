package repository

import (
	"context"
	"time"
)

type sqliteDashboardTotals struct {
	requests, input, output, creation, read, duration int64
	cost, actual, account                             float64
}

func (r *usageLogRepository) dashboardTotalsSQLite(ctx context.Context, start, end time.Time) (sqliteDashboardTotals, error) {
	var v sqliteDashboardTotals
	err := scanSingleRow(ctx, r.sql, `SELECT COUNT(*),COALESCE(SUM(input_tokens),0),COALESCE(SUM(output_tokens),0),
		COALESCE(SUM(cache_creation_tokens),0),COALESCE(SUM(cache_read_tokens),0),COALESCE(SUM(total_cost),0),
		COALESCE(SUM(actual_cost),0),COALESCE(SUM(COALESCE(account_stats_cost,total_cost)*COALESCE(account_rate_multiplier,1)),0),
		COALESCE(SUM(duration_ms),0) FROM usage_logs WHERE pg_rfc3339_micros(created_at)>=$1 AND pg_rfc3339_micros(created_at)<$2`,
		[]any{start.UnixMicro(), end.UnixMicro()}, &v.requests, &v.input, &v.output, &v.creation, &v.read, &v.cost, &v.actual, &v.account, &v.duration)
	return v, err
}

func (r *usageLogRepository) fillDashboardUsageStatsSQLite(ctx context.Context, stats *DashboardStats, start, end, today, now time.Time) error {
	total, err := r.dashboardTotalsSQLite(ctx, start, end)
	if err != nil {
		return err
	}
	todayEnd := today.AddDate(0, 0, 1)
	daily, err := r.dashboardTotalsSQLite(ctx, today, todayEnd)
	if err != nil {
		return err
	}
	stats.TotalRequests, stats.TotalInputTokens, stats.TotalOutputTokens = total.requests, total.input, total.output
	stats.TotalCacheCreationTokens, stats.TotalCacheReadTokens = total.creation, total.read
	stats.TotalTokens = total.input + total.output + total.creation + total.read
	stats.TotalCost, stats.TotalActualCost, stats.TotalAccountCost = total.cost, total.actual, total.account
	if total.requests > 0 {
		stats.AverageDurationMs = float64(total.duration) / float64(total.requests)
	}
	stats.TodayRequests, stats.TodayInputTokens, stats.TodayOutputTokens = daily.requests, daily.input, daily.output
	stats.TodayCacheCreationTokens, stats.TodayCacheReadTokens = daily.creation, daily.read
	stats.TodayTokens = daily.input + daily.output + daily.creation + daily.read
	stats.TodayCost, stats.TodayActualCost, stats.TodayAccountCost = daily.cost, daily.actual, daily.account
	hour := now.UTC().Truncate(time.Hour)
	return scanSingleRow(ctx, r.sql, `SELECT COUNT(DISTINCT CASE WHEN pg_rfc3339_micros(created_at)>=$1 AND pg_rfc3339_micros(created_at)<$2 THEN user_id END),
		COUNT(DISTINCT CASE WHEN pg_rfc3339_micros(created_at)>=$3 AND pg_rfc3339_micros(created_at)<$4 THEN user_id END)
		FROM usage_logs WHERE pg_rfc3339_micros(created_at)>=MIN($1,$3) AND pg_rfc3339_micros(created_at)<MAX($2,$4)`,
		[]any{today.UnixMicro(), todayEnd.UnixMicro(), hour.UnixMicro(), hour.Add(time.Hour).UnixMicro()}, &stats.ActiveUsers, &stats.HourlyActiveUsers)
}
