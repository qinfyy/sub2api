package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/dbtime"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const channelMonitorV2SQLiteWindow = `pg_rfc3339_micros(ul.created_at) >= pg_rfc3339_micros($1)
 AND pg_rfc3339_micros(ul.created_at) < pg_rfc3339_micros($2)`

const channelMonitorV2SQLiteUsageValues = `COUNT(DISTINCT COALESCE(NULLIF(ul.request_id,''),'usage:'||ul.id))
 FILTER (WHERE COALESCE(ul.request_type,0) NOT IN (4,6) AND ` + usageLogSuccessFilterUL + `),
 COALESCE(SUM(ul.input_tokens) FILTER (WHERE ` + usageLogSuccessFilterUL + `),0),
 COALESCE(SUM(ul.output_tokens) FILTER (WHERE ` + usageLogSuccessFilterUL + `),0),
 COALESCE(SUM(ul.cache_creation_tokens) FILTER (WHERE ` + usageLogSuccessFilterUL + `),0),
 COALESCE(SUM(ul.cache_read_tokens) FILTER (WHERE ` + usageLogSuccessFilterUL + `),0),
 COALESCE(SUM(ul.first_token_ms) FILTER (WHERE ` + usageLogSuccessFilterUL + `),0),
 COUNT(ul.first_token_ms) FILTER (WHERE ` + usageLogSuccessFilterUL + `),
 COALESCE(SUM(ul.duration_ms) FILTER (WHERE ` + usageLogSuccessFilterUL + `),0),
 COUNT(ul.duration_ms) FILTER (WHERE ` + usageLogSuccessFilterUL + `)`

const channelMonitorV2SQLiteUsageJoins = ` FROM usage_logs ul
 LEFT JOIN groups g ON g.id=ul.group_id LEFT JOIN accounts a ON a.id=ul.account_id `

func (r *channelMonitorV2Repository) recomputeRangeSQLite(ctx context.Context, start, end time.Time) error {
	start, end = start.UTC().Truncate(time.Minute), end.UTC().Truncate(time.Minute)
	now := time.Now().UTC().Truncate(time.Minute)
	floor := channelMonitorV2RetentionCutoff(now, channelMonitorV2MaxRetention())
	if start.Before(floor) {
		start = floor
	}
	if !start.Before(end) {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range []string{"metrics", "user_metrics", "error_metrics", "latency_histograms"} {
		if _, err = tx.ExecContext(ctx, `DELETE FROM channel_monitor_v2_`+table+`_1m
		 WHERE pg_rfc3339_micros(bucket_start)>=pg_rfc3339_micros($1) AND pg_rfc3339_micros(bucket_start)<pg_rfc3339_micros($2)`, start, end); err != nil {
			return err
		}
	}
	for _, perUser := range []bool{false, true} {
		table, columns, values, group, extra := "metrics", "", "", "", ""
		if perUser {
			table = "user_metrics"
			columns = ",user_id"
			values = ",ul.user_id"
			group = ",5"
			extra = " AND ul.user_id IS NOT NULL"
		}
		query := `INSERT INTO channel_monitor_v2_` + table + `_1m (bucket_start,platform,group_id,model` + columns + `,
		 success_requests,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,ttft_sum_ms,ttft_count,duration_sum_ms,duration_count)
		 SELECT sub2api_time_bucket(ul.created_at,'minute','UTC'),` + channelMonitorV2PlatformSQL + `,COALESCE(ul.group_id,0),` + channelMonitorV2ModelSQL + values + `,` +
			channelMonitorV2SQLiteUsageValues + channelMonitorV2SQLiteUsageJoins + ` WHERE ` + channelMonitorV2SQLiteWindow + extra + ` GROUP BY 1,2,3,4` + group
		if _, err = tx.ExecContext(ctx, query, start, end); err != nil {
			return fmt.Errorf("aggregate sqlite channel monitor %s: %w", table, err)
		}
	}
	query := `WITH samples AS (
	 SELECT sub2api_time_bucket(ul.created_at,'minute','UTC') AS bucket_start,` + channelMonitorV2PlatformSQL + ` AS platform,
	 COALESCE(ul.group_id,0) AS group_id,` + channelMonitorV2ModelSQL + ` AS model,
	 CASE audience.id WHEN 0 THEN 0 ELSE ul.user_id END AS user_id,latency.metric,
	 CASE latency.metric WHEN 'ttft' THEN ul.first_token_ms ELSE ul.duration_ms END AS value_ms
	 ` + channelMonitorV2SQLiteUsageJoins + `
	 CROSS JOIN (SELECT 0 AS id UNION ALL SELECT 1) audience
	 CROSS JOIN (SELECT 'ttft' AS metric UNION ALL SELECT 'duration') latency
	 WHERE ` + channelMonitorV2SQLiteWindow + ` AND ` + usageLogSuccessFilterUL + `)
	 INSERT INTO channel_monitor_v2_latency_histograms_1m (bucket_start,platform,group_id,model,user_id,metric,upper_bound_ms,sample_count)
	 SELECT bucket_start,platform,group_id,model,user_id,metric,` + channelMonitorV2HistogramBoundSQL("value_ms") + `,COUNT(*) FROM samples
	 WHERE user_id IS NOT NULL AND value_ms IS NOT NULL AND value_ms>=0 GROUP BY 1,2,3,4,5,6,7`
	if _, err = tx.ExecContext(ctx, query, start, end); err != nil {
		return fmt.Errorf("aggregate sqlite channel monitor histograms: %w", err)
	}
	if err = r.aggregateErrorsSQLite(ctx, tx, start, end); err != nil {
		return err
	}
	if err = r.recomputeRollupsSQLite(ctx, tx, start, end); err != nil {
		return err
	}
	if err = r.pruneRetentionSQLite(ctx, tx, now); err != nil {
		return err
	}
	// Store canonical UTC text for all new watermarks while accepting legacy timestamp formats.
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_monitor_v2_watermarks
	 (id,usage_coverage_start,error_coverage_start,data_through,last_successful_at,backfill_cursor,updated_at)
	 VALUES(1,$1,$1,$2,$3,$1,$3) ON CONFLICT(id) DO UPDATE SET
	 usage_coverage_start=strftime('%Y-%m-%dT%H:%M:%SZ',max(pg_rfc3339_micros($4),min(COALESCE(pg_rfc3339_micros(usage_coverage_start),pg_rfc3339_micros($1)),pg_rfc3339_micros($1)))/1000000,'unixepoch'),
	 error_coverage_start=strftime('%Y-%m-%dT%H:%M:%SZ',max(pg_rfc3339_micros($4),min(COALESCE(pg_rfc3339_micros(error_coverage_start),pg_rfc3339_micros($1)),pg_rfc3339_micros($1)))/1000000,'unixepoch'),
	 data_through=strftime('%Y-%m-%dT%H:%M:%SZ',max(COALESCE(pg_rfc3339_micros(data_through),pg_rfc3339_micros($2)),pg_rfc3339_micros($2))/1000000,'unixepoch'),
	 last_successful_at=$3,backfill_cursor=strftime('%Y-%m-%dT%H:%M:%SZ',min(COALESCE(pg_rfc3339_micros(backfill_cursor),pg_rfc3339_micros($1)),pg_rfc3339_micros($1))/1000000,'unixepoch'),updated_at=$3`,
		start.Format(time.RFC3339), end.Format(time.RFC3339), now.Format(time.RFC3339), floor.Format(time.RFC3339))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *channelMonitorV2Repository) aggregateErrorsSQLite(ctx context.Context, tx *sql.Tx, start, end time.Time) error {
	rows, err := tx.QueryContext(ctx, `WITH candidates AS (
	 SELECT DISTINCT request_id FROM ops_error_logs WHERE pg_rfc3339_micros(created_at)>=pg_rfc3339_micros($1)
	 AND pg_rfc3339_micros(created_at)<pg_rfc3339_micros($2) AND NULLIF(request_id,'') IS NOT NULL
	), ranked AS (
	 SELECT e.*,row_number() OVER(PARTITION BY COALESCE(NULLIF(e.request_id,''),'error:'||e.id)
	 ORDER BY pg_rfc3339_micros(e.created_at) DESC,e.id DESC) AS rank
	 FROM ops_error_logs e WHERE NOT e.is_count_tokens AND (COALESCE(e.status_code,0)>=400 OR e.error_type='cyber_policy')
	 AND ((NULLIF(e.request_id,'') IS NULL AND pg_rfc3339_micros(e.created_at)>=pg_rfc3339_micros($1) AND pg_rfc3339_micros(e.created_at)<pg_rfc3339_micros($2))
	 OR (e.request_id IN (SELECT request_id FROM candidates) AND pg_rfc3339_micros(e.created_at)>=pg_rfc3339_micros($3) AND pg_rfc3339_micros(e.created_at)<pg_rfc3339_micros($2))))
	 SELECT sub2api_time_bucket(e.created_at,'minute','UTC'),
	 lower(CASE WHEN g.platform='composite' THEN COALESCE(NULLIF(TRIM(a.platform),''),NULLIF(NULLIF(lower(TRIM(e.platform)),''),'composite'),'unknown') ELSE COALESCE(NULLIF(TRIM(e.platform),''),'unknown') END),
	 COALESCE(e.group_id,0),COALESCE(NULLIF(TRIM(e.requested_model),''),NULLIF(TRIM(e.model),''),'unknown'),e.user_id,
	 COALESCE(e.error_type,''),COALESCE(e.error_owner,''),COALESCE(e.error_source,''),COALESCE(e.status_code,0),COALESCE(e.upstream_status_code,0),
	 COALESCE(e.error_message,'')||' '||COALESCE(e.upstream_error_message,'')||' '||COALESCE(e.upstream_error_detail,'')||' '||COALESCE(e.error_body,''),
	 (CASE WHEN json_valid(e.upstream_errors) THEN CASE WHEN json_type(e.upstream_errors)='array' THEN json_array_length(e.upstream_errors)>0 ELSE 0 END ELSE 0 END OR e.error_owner='provider' OR e.upstream_status_code IS NOT NULL),
	 CASE WHEN json_valid(e.upstream_errors) THEN CASE WHEN json_type(e.upstream_errors)='array' THEN json_array_length(e.upstream_errors) ELSE 0 END ELSE 0 END
	 FROM ranked e LEFT JOIN groups g ON g.id=e.group_id LEFT JOIN accounts a ON a.id=e.account_id
	 WHERE e.rank=1 AND pg_rfc3339_micros(e.created_at)>=pg_rfc3339_micros($1) AND pg_rfc3339_micros(e.created_at)<pg_rfc3339_micros($2)`, start, end, start.Add(-90*time.Minute))
	if err != nil {
		return fmt.Errorf("read sqlite channel monitor errors: %w", err)
	}
	defer func() { _ = rows.Close() }()
	type key struct {
		bucket, platform, model, category string
		group, user                       int64
		hasUser                           bool
	}
	type counts struct{ requests, affected, attempts int64 }
	aggregates := map[key]counts{}
	for rows.Next() {
		var k key
		var user sql.NullInt64
		var input service.ChannelMonitorV2ErrorInput
		var affected sql.NullBool
		var attempts int64
		if err = rows.Scan(&k.bucket, &k.platform, &k.group, &k.model, &user, &input.ErrorType, &input.ErrorOwner, &input.ErrorSource, &input.StatusCode, &input.UpstreamStatusCode, &input.Message, &affected, &attempts); err != nil {
			return err
		}
		k.category = service.ClassifyChannelMonitorV2Error(input)
		k.user, k.hasUser = user.Int64, user.Valid
		v := aggregates[k]
		v.requests++
		v.attempts += attempts
		if affected.Bool {
			v.affected++
		}
		aggregates[k] = v
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for k, v := range aggregates {
		_, err = tx.ExecContext(ctx, `INSERT INTO channel_monitor_v2_metrics_1m(bucket_start,platform,group_id,model,error_requests,upstream_affected_requests,upstream_attempt_count)
		 VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(bucket_start,platform,group_id,model) DO UPDATE SET
		 error_requests=error_requests+excluded.error_requests,upstream_affected_requests=upstream_affected_requests+excluded.upstream_affected_requests,upstream_attempt_count=upstream_attempt_count+excluded.upstream_attempt_count`, k.bucket, k.platform, k.group, k.model, v.requests, v.affected, v.attempts)
		if err != nil {
			return err
		}
		if k.hasUser {
			_, err = tx.ExecContext(ctx, `INSERT INTO channel_monitor_v2_user_metrics_1m(bucket_start,platform,group_id,model,user_id,error_requests)
			 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(bucket_start,platform,group_id,model,user_id) DO UPDATE SET error_requests=error_requests+excluded.error_requests`, k.bucket, k.platform, k.group, k.model, k.user, v.requests)
			if err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO channel_monitor_v2_error_metrics_1m(bucket_start,platform,group_id,model,error_category,taxonomy_version,error_requests)
		 VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(bucket_start,platform,group_id,model,error_category,taxonomy_version) DO UPDATE SET error_requests=error_requests+excluded.error_requests`, k.bucket, k.platform, k.group, k.model, k.category, service.ChannelMonitorV2TaxonomyVersion, v.requests)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *channelMonitorV2Repository) recomputeRollupsSQLite(ctx context.Context, tx *sql.Tx, start, end time.Time) error {
	for _, seconds := range channelMonitorV2FixedRollupSeconds {
		if seconds >= 43200 && sameFixedRollupBucket(start, end, seconds) {
			continue
		}
		interval := time.Duration(seconds) * time.Second
		from, to := start.Truncate(interval), end.Add(-time.Nanosecond).Truncate(interval).Add(interval)
		for _, spec := range []struct{ table, dimensions, metrics string }{
			{"metrics", "platform,group_id,model", "success_requests,error_requests,upstream_affected_requests,upstream_attempt_count,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,ttft_sum_ms,ttft_count,duration_sum_ms,duration_count"},
			{"user_metrics", "platform,group_id,model,user_id", "success_requests,error_requests,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,ttft_sum_ms,ttft_count,duration_sum_ms,duration_count"},
			{"latency_histograms", "platform,group_id,model,user_id,metric,upper_bound_ms", "sample_count"},
			{"error_metrics", "platform,group_id,model,error_category,taxonomy_version", "error_requests"},
		} {
			table := "channel_monitor_v2_" + spec.table
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+`_rollup WHERE bucket_seconds=$1 AND pg_rfc3339_micros(bucket_start)>=pg_rfc3339_micros($2) AND pg_rfc3339_micros(bucket_start)<pg_rfc3339_micros($3)`, seconds, from, to); err != nil {
				return err
			}
			sums := []string{}
			for _, column := range strings.Split(spec.metrics, ",") {
				sums = append(sums, "SUM("+column+")")
			}
			query := `INSERT INTO ` + table + `_rollup(bucket_start,bucket_seconds,` + spec.dimensions + `,` + spec.metrics + `)
			 SELECT ` + channelMonitorV2DateBinExpr("bucket_start", true) + `,$1,` + spec.dimensions + `,` + strings.Join(sums, ",") + `
			 FROM ` + table + `_1m WHERE pg_rfc3339_micros(bucket_start)>=pg_rfc3339_micros($2) AND pg_rfc3339_micros(bucket_start)<pg_rfc3339_micros($3)
			 GROUP BY 1,2,` + spec.dimensions
			if _, err := tx.ExecContext(ctx, query, seconds, from, to); err != nil {
				return fmt.Errorf("roll up sqlite channel monitor %s %ds: %w", spec.table, seconds, err)
			}
		}
	}
	return nil
}

func (r *channelMonitorV2Repository) pruneRetentionSQLite(ctx context.Context, tx *sql.Tx, now time.Time) error {
	var cursor dbtime.NullTime
	err := tx.QueryRowContext(ctx, `SELECT backfill_cursor FROM channel_monitor_v2_watermarks WHERE id=1`).Scan(&cursor)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if cursor.Valid && cursor.Time.After(channelMonitorV2RetentionCutoff(now, channelMonitorV2RetentionMax)) {
		return nil
	}
	for _, rule := range channelMonitorV2RetentionRules {
		query := `DELETE FROM ` + rule.table + ` WHERE pg_rfc3339_micros(bucket_start)<pg_rfc3339_micros($1)`
		args := []any{channelMonitorV2RetentionCutoff(now, rule.retention)}
		if rule.bucketSeconds > 0 {
			query += ` AND bucket_seconds=$2`
			args = append(args, rule.bucketSeconds)
		}
		if _, err = tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}
	return nil
}
