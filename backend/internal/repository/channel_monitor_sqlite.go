package repository

import (
	"context"
	"time"
)

const channelMonitorSQLiteLatestSQL = `WITH ranked AS (
 SELECT *,row_number() OVER(PARTITION BY monitor_id,model ORDER BY pg_rfc3339_micros(checked_at) DESC,id DESC) AS rn
 FROM channel_monitor_histories WHERE monitor_id IN(SELECT value FROM json_each($1)))
 SELECT monitor_id,model,status,latency_ms,ping_latency_ms,checked_at,quota FROM ranked WHERE rn=1 ORDER BY monitor_id,model`

const channelMonitorSQLiteRecentSQL = `WITH targets AS (
 SELECT ids.value AS monitor_id,models.value AS model FROM json_each($1) ids JOIN json_each($2) models ON ids.key=models.key
),ranked AS (
 SELECT h.*,row_number() OVER(PARTITION BY h.monitor_id ORDER BY pg_rfc3339_micros(h.checked_at) DESC,h.id DESC) AS rn
 FROM channel_monitor_histories h JOIN targets t ON t.monitor_id=h.monitor_id AND t.model=h.model)
 SELECT monitor_id,status,latency_ms,ping_latency_ms,checked_at FROM ranked WHERE rn<=$3 ORDER BY monitor_id,rn`

const channelMonitorSQLiteAvailabilitySQL = `SELECT monitor_id,model,COUNT(*),
 COUNT(*) FILTER(WHERE status IN('operational','degraded')),AVG(latency_ms)
 FROM channel_monitor_histories WHERE monitor_id IN(SELECT value FROM json_each($1))
 AND pg_rfc3339_micros(checked_at)>=pg_rfc3339_micros($2) GROUP BY monitor_id,model`

const channelMonitorSQLitePruneHistorySQL = `DELETE FROM channel_monitor_histories WHERE id IN(
 SELECT id FROM channel_monitor_histories WHERE pg_rfc3339_micros(checked_at)<pg_rfc3339_micros($1) ORDER BY id LIMIT $2)`

const channelMonitorSQLitePruneRollupSQL = `DELETE FROM channel_monitor_daily_rollups WHERE id IN(
 SELECT id FROM channel_monitor_daily_rollups WHERE pg_rfc3339_micros(bucket_date)<pg_rfc3339_micros($1) ORDER BY id LIMIT $2)`

func (r *channelMonitorRepository) upsertDailyRollupsSQLite(ctx context.Context, date time.Time) (int64, error) {
	start := date.UTC().Truncate(24 * time.Hour)
	res, err := r.db.ExecContext(ctx, `INSERT INTO channel_monitor_daily_rollups (
	 monitor_id,model,bucket_date,total_checks,ok_count,operational_count,degraded_count,failed_count,error_count,
	 sum_latency_ms,count_latency,sum_ping_latency_ms,count_ping_latency,computed_at)
	 SELECT monitor_id,model,$1,COUNT(*),COUNT(*) FILTER(WHERE status IN('operational','degraded')),
	 COUNT(*) FILTER(WHERE status='operational'),COUNT(*) FILTER(WHERE status='degraded'),
	 COUNT(*) FILTER(WHERE status='failed'),COUNT(*) FILTER(WHERE status='error'),
	 COALESCE(SUM(latency_ms),0),COUNT(latency_ms),COALESCE(SUM(ping_latency_ms),0),COUNT(ping_latency_ms),strftime('%Y-%m-%dT%H:%M:%fZ','now')
	 FROM channel_monitor_histories WHERE pg_rfc3339_micros(checked_at)>=pg_rfc3339_micros($1) AND pg_rfc3339_micros(checked_at)<pg_rfc3339_micros($2)
	 GROUP BY monitor_id,model ON CONFLICT(monitor_id,model,bucket_date) DO UPDATE SET
	 total_checks=excluded.total_checks,ok_count=excluded.ok_count,operational_count=excluded.operational_count,
	 degraded_count=excluded.degraded_count,failed_count=excluded.failed_count,error_count=excluded.error_count,
	 sum_latency_ms=excluded.sum_latency_ms,count_latency=excluded.count_latency,sum_ping_latency_ms=excluded.sum_ping_latency_ms,
	 count_ping_latency=excluded.count_ping_latency,computed_at=excluded.computed_at`, start.Format(time.RFC3339), start.AddDate(0, 0, 1).Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
