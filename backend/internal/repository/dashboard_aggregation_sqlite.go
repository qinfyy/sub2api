package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/dbtime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type sqliteDashboardAggregation struct {
	db    *sql.DB
	clock func() time.Time
}

var _ service.DashboardAggregationRepository = (*sqliteDashboardAggregation)(nil)

func (r *sqliteDashboardAggregation) transaction(ctx context.Context, change func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := change(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *sqliteDashboardAggregation) AggregateRange(ctx context.Context, start, end time.Time) error {
	if !end.After(start) {
		return nil
	}
	return r.transaction(ctx, func(tx *sql.Tx) error { return rebuildSQLiteDashboard(ctx, tx, start, end) })
}

func (r *sqliteDashboardAggregation) RecomputeRange(ctx context.Context, start, end time.Time) error {
	if !end.After(start) {
		return nil
	}
	return r.transaction(ctx, func(tx *sql.Tx) error {
		if err := lockGroupUsageRollupState(ctx, tx, true); err != nil {
			return err
		}
		if err := invalidateGroupUsageRollupsAt(ctx, tx, true, start); err != nil {
			return err
		}
		if err := rebuildSQLiteDashboard(ctx, tx, start, end); err != nil {
			return err
		}
		return syncSQLiteGroupRollups(ctx, tx, service.GroupUsageTodayStart(r.clock()))
	})
}

func rebuildSQLiteDashboard(ctx context.Context, tx *sql.Tx, start, end time.Time) error {
	loc := timezone.Location()
	hourFloor := func(t time.Time) time.Time {
		t = t.In(loc)
		return t.Add(-time.Duration(t.Minute())*time.Minute - time.Duration(t.Second())*time.Second - time.Duration(t.Nanosecond()))
	}
	hourStart, hourEnd := hourFloor(start), hourFloor(end)
	if end.After(hourEnd) {
		hourEnd = hourEnd.Add(time.Hour)
	}
	dayStart, dayEnd := timezone.StartOfDay(start), timezone.StartOfDay(end)
	if end.After(dayEnd) {
		dayEnd = dayEnd.AddDate(0, 0, 1)
	}
	for _, table := range []string{"usage_dashboard_hourly", "usage_dashboard_hourly_users"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE pg_rfc3339_micros(bucket_start)>=? AND pg_rfc3339_micros(bucket_start)<?`, hourStart.UnixMicro(), hourEnd.UnixMicro()); err != nil {
			return err
		}
	}
	args := []any{hourStart.UnixMicro(), hourEnd.UnixMicro(), loc.String()}
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_dashboard_hourly_users(bucket_start,user_id)
		SELECT DISTINCT sub2api_time_bucket(created_at,'hour',$3),user_id FROM usage_logs
		WHERE pg_rfc3339_micros(created_at)>=$1 AND pg_rfc3339_micros(created_at)<$2
		ON CONFLICT DO NOTHING`, args...); err != nil {
		return err
	}
	const cols = `total_requests,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,total_cost,actual_cost,account_cost,total_duration_ms`
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_dashboard_hourly(bucket_start,`+cols+`,active_users,computed_at)
		SELECT sub2api_time_bucket(created_at,'hour',$3),COUNT(*),COALESCE(SUM(input_tokens),0),COALESCE(SUM(output_tokens),0),
		COALESCE(SUM(cache_creation_tokens),0),COALESCE(SUM(cache_read_tokens),0),COALESCE(SUM(total_cost),0),COALESCE(SUM(actual_cost),0),
		COALESCE(SUM(COALESCE(account_stats_cost,total_cost)*COALESCE(account_rate_multiplier,1)),0),COALESCE(SUM(duration_ms),0),COUNT(DISTINCT user_id),NOW()
		FROM usage_logs WHERE pg_rfc3339_micros(created_at)>=$1 AND pg_rfc3339_micros(created_at)<$2 GROUP BY 1`, args...); err != nil {
		return err
	}
	dayArgs := []any{dayStart.Format("2006-01-02"), dayEnd.Format("2006-01-02"), loc.String()}
	for _, table := range []string{"usage_dashboard_daily", "usage_dashboard_daily_users"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE bucket_date>=? AND bucket_date<?`, dayArgs[:2]...); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_dashboard_daily_users(bucket_date,user_id)
		SELECT DISTINCT sub2api_time_bucket(bucket_start,'day',$3),user_id FROM usage_dashboard_hourly_users
		WHERE sub2api_time_bucket(bucket_start,'day',$3)>=$1 AND sub2api_time_bucket(bucket_start,'day',$3)<$2`, dayArgs...); err != nil {
		return err
	}
	sums := make([]string, 0, 9)
	for _, col := range strings.Split(cols, ",") {
		sums = append(sums, "COALESCE(SUM("+col+"),0)")
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO usage_dashboard_daily(bucket_date,`+cols+`,active_users,computed_at)
		SELECT sub2api_time_bucket(h.bucket_start,'day',$3),`+strings.Join(sums, ",")+`,
		(SELECT COUNT(*) FROM usage_dashboard_daily_users u WHERE u.bucket_date=sub2api_time_bucket(h.bucket_start,'day',$3)),NOW()
		FROM usage_dashboard_hourly h WHERE sub2api_time_bucket(h.bucket_start,'day',$3)>=$1
		AND sub2api_time_bucket(h.bucket_start,'day',$3)<$2 GROUP BY 1`, dayArgs...)
	return err
}

func (r *sqliteDashboardAggregation) GetAggregationWatermark(ctx context.Context) (time.Time, error) {
	var value time.Time
	err := r.db.QueryRowContext(ctx, `SELECT last_aggregated_at FROM usage_dashboard_aggregation_watermark WHERE id=1`).Scan(dbtime.Dest(&value))
	if err == sql.ErrNoRows {
		return time.Unix(0, 0).UTC(), nil
	}
	return value.UTC(), err
}

func (r *sqliteDashboardAggregation) UpdateAggregationWatermark(ctx context.Context, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO usage_dashboard_aggregation_watermark(id,last_aggregated_at,updated_at)
		VALUES(1,?,NOW()) ON CONFLICT(id) DO UPDATE SET last_aggregated_at=excluded.last_aggregated_at,updated_at=excluded.updated_at`, at.UTC().Format(time.RFC3339Nano))
	return err
}

func (r *sqliteDashboardAggregation) CleanupAggregates(ctx context.Context, hourlyCutoff, dailyCutoff time.Time) error {
	return r.transaction(ctx, func(tx *sql.Tx) error {
		for _, table := range []string{"usage_dashboard_hourly", "usage_dashboard_hourly_users"} {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE pg_rfc3339_micros(bucket_start)<?`, hourlyCutoff.UnixMicro()); err != nil {
				return err
			}
		}
		for _, table := range []string{"usage_dashboard_daily", "usage_dashboard_daily_users"} {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE bucket_date<?`, dailyCutoff.In(timezone.Location()).Format("2006-01-02")); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *sqliteDashboardAggregation) CleanupUsageLogs(ctx context.Context, cutoff time.Time) error {
	cleaner := newUsageCleanupRepositoryWithSQL(nil, r.db)
	for {
		n, err := cleaner.deleteUsageLogsBatchWithRollupInvalidation(ctx, r.db, `pg_rfc3339_micros(created_at)<$1`, []any{cutoff.UnixMicro(), usageLogsCleanupBatchSize})
		if err != nil {
			return err
		}
		if n < usageLogsCleanupBatchSize {
			break
		}
	}
	return r.SyncGroupUsageRollups(ctx, service.GroupUsageTodayStart(r.clock()))
}

func (r *sqliteDashboardAggregation) CleanupUsageBillingDedup(ctx context.Context, cutoff time.Time) error {
	for {
		var affected int64
		err := r.transaction(ctx, func(tx *sql.Tx) error {
			// Keep the archive insert and bounded delete in the same write transaction.
			if _, err := tx.ExecContext(ctx, `INSERT INTO usage_billing_dedup_archive(request_id,api_key_id,request_fingerprint,created_at)
				SELECT request_id,api_key_id,request_fingerprint,created_at FROM usage_billing_dedup
				WHERE rowid IN (SELECT rowid FROM usage_billing_dedup WHERE pg_rfc3339_micros(created_at)<? ORDER BY rowid LIMIT ?)
				ON CONFLICT(request_id,api_key_id) DO NOTHING`, cutoff.UnixMicro(), usageBillingDedupCleanupBatchSize); err != nil {
				return err
			}
			result, err := tx.ExecContext(ctx, `DELETE FROM usage_billing_dedup WHERE rowid IN
				(SELECT rowid FROM usage_billing_dedup WHERE pg_rfc3339_micros(created_at)<? ORDER BY rowid LIMIT ?)`, cutoff.UnixMicro(), usageBillingDedupCleanupBatchSize)
			if err != nil {
				return err
			}
			affected, err = result.RowsAffected()
			return err
		})
		if err != nil {
			return err
		}
		if affected < usageBillingDedupCleanupBatchSize {
			return nil
		}
	}
}

func (r *sqliteDashboardAggregation) EnsureUsageLogsPartitions(context.Context, time.Time) error {
	// SQLite uses one indexed table; only PostgreSQL needs physical partitions.
	return nil
}

func (r *sqliteDashboardAggregation) SyncGroupUsageRollups(ctx context.Context, today time.Time) error {
	return r.transaction(ctx, func(tx *sql.Tx) error {
		if err := lockGroupUsageRollupState(ctx, tx, true); err != nil {
			return err
		}
		return syncSQLiteGroupRollups(ctx, tx, service.GroupUsageTodayStart(today))
	})
}

func syncSQLiteGroupRollups(ctx context.Context, tx *sql.Tx, today time.Time) error {
	var closed, zone string
	if err := tx.QueryRowContext(ctx, `SELECT closed_before,timezone_name FROM usage_group_rollup_state WHERE id=1`).Scan(&closed, &zone); err != nil {
		return err
	}
	date, currentZone := service.GroupUsageDate(today), service.GroupUsageTimezoneName()
	if zone == currentZone && closed > date {
		return fmt.Errorf("group usage watermark is in the future: %s", closed)
	}
	if zone == currentZone && closed == date {
		return nil
	}
	var earliest dbtime.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT created_at FROM usage_logs ORDER BY pg_rfc3339_micros(created_at) LIMIT 1`).Scan(&earliest); err != nil && err != sql.ErrNoRows {
		return err
	}
	retained := today
	if earliest.Valid {
		retained = earliest.Time
	}
	retainedDate := service.GroupUsageDate(retained)
	startDate := retainedDate
	if zone == currentZone && closed > startDate {
		startDate = closed
	}
	start, err := service.ParseGroupUsageDate(startDate)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_group_daily_rollups WHERE bucket_date<? OR bucket_date>=?`, retainedDate, startDate); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_group_daily_rollups(bucket_date,group_id,actual_cost,computed_at)
		SELECT sub2api_time_bucket(created_at,'day',$3),group_id,COALESCE(SUM(actual_cost),0),NOW()
		FROM usage_logs WHERE group_id IS NOT NULL AND pg_rfc3339_micros(created_at)>=$1 AND pg_rfc3339_micros(created_at)<$2 GROUP BY 1,2`, start.UnixMicro(), today.UnixMicro(), currentZone); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE usage_group_rollup_state SET closed_before=?,retained_from=?,timezone_name=?,updated_at=NOW() WHERE id=1`, date, retained.UTC().Format(time.RFC3339Nano), currentZone)
	return err
}
