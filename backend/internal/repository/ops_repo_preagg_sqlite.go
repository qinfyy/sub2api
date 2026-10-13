package repository

import (
	"context"
	"time"
)

// SQLite uses explicit dimension sets and window ranks for continuous percentiles.
func (r *opsRepository) upsertHourlyMetricsSQLite(ctx context.Context, start, end time.Time) error {
	q := `
WITH usage_base AS (
  SELECT
    sub2api_time_bucket(ul.created_at, 'hour', 'UTC') AS bucket_start,
    g.platform AS platform,
    ul.group_id AS group_id,
    ul.duration_ms AS duration_ms,
    ul.first_token_ms AS first_token_ms,
    (ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens) AS tokens
  FROM usage_logs ul
  JOIN groups g ON g.id = ul.group_id
  WHERE pg_rfc3339_micros(ul.created_at) >= pg_rfc3339_micros($1) AND pg_rfc3339_micros(ul.created_at) < pg_rfc3339_micros($2)
),
usage_dims AS (
  SELECT bucket_start, platform, group_id FROM usage_base
  UNION
  SELECT bucket_start, platform, NULL FROM usage_base
  UNION
  SELECT bucket_start, NULL, NULL FROM usage_base
),
usage_agg AS (
  SELECT
    d.bucket_start,
    d.platform,
    d.group_id,
    COUNT(*) AS success_count,
    SUM(CASE WHEN u.first_token_ms IS NOT NULL THEN 1 ELSE 0 END) AS ttft_sample_count,
    COALESCE(SUM(u.tokens), 0) AS token_consumed,
    AVG(u.duration_ms) AS duration_avg_ms,
    MAX(u.duration_ms) AS duration_max_ms,
    AVG(u.first_token_ms) AS ttft_avg_ms,
    MAX(u.first_token_ms) AS ttft_max_ms
  FROM usage_dims d
  JOIN usage_base u
    ON u.bucket_start = d.bucket_start
   AND (d.platform IS NULL OR u.platform = d.platform)
   AND (d.group_id IS NULL OR u.group_id = d.group_id)
  GROUP BY d.bucket_start, d.platform, d.group_id
),
latency_samples AS (
  SELECT d.bucket_start, d.platform, d.group_id, 'duration' AS metric, u.duration_ms AS value_ms
  FROM usage_dims d JOIN usage_base u ON u.bucket_start=d.bucket_start
    AND (d.platform IS NULL OR u.platform=d.platform) AND (d.group_id IS NULL OR u.group_id=d.group_id)
  WHERE u.duration_ms IS NOT NULL
  UNION ALL
  SELECT d.bucket_start, d.platform, d.group_id, 'ttft', u.first_token_ms
  FROM usage_dims d JOIN usage_base u ON u.bucket_start=d.bucket_start
    AND (d.platform IS NULL OR u.platform=d.platform) AND (d.group_id IS NULL OR u.group_id=d.group_id)
  WHERE u.first_token_ms IS NOT NULL
),
ordered AS (
  SELECT *, ROW_NUMBER() OVER (PARTITION BY bucket_start,platform,group_id,metric ORDER BY value_ms) AS rn,
    COUNT(*) OVER (PARTITION BY bucket_start,platform,group_id,metric) AS total
  FROM latency_samples
),
latency_percentiles AS (
  SELECT bucket_start AS lbucket, platform AS lplatform, group_id AS lgroup, metric,
    ` + opsSQLitePercentileExpr("value_ms", 0.50) + ` AS p50,
    ` + opsSQLitePercentileExpr("value_ms", 0.90) + ` AS p90,
    ` + opsSQLitePercentileExpr("value_ms", 0.95) + ` AS p95,
    ` + opsSQLitePercentileExpr("value_ms", 0.99) + ` AS p99
  FROM ordered GROUP BY bucket_start,platform,group_id,metric
),
latency_agg AS (
 SELECT lbucket,lplatform,lgroup,
    MAX(CASE WHEN metric='duration' THEN p50 END) AS duration_p50_ms,
    MAX(CASE WHEN metric='duration' THEN p90 END) AS duration_p90_ms,
    MAX(CASE WHEN metric='duration' THEN p95 END) AS duration_p95_ms,
    MAX(CASE WHEN metric='duration' THEN p99 END) AS duration_p99_ms,
    MAX(CASE WHEN metric='ttft' THEN p50 END) AS ttft_p50_ms,
    MAX(CASE WHEN metric='ttft' THEN p90 END) AS ttft_p90_ms,
    MAX(CASE WHEN metric='ttft' THEN p95 END) AS ttft_p95_ms,
    MAX(CASE WHEN metric='ttft' THEN p99 END) AS ttft_p99_ms
 FROM latency_percentiles GROUP BY lbucket,lplatform,lgroup
),
error_base AS (
  SELECT
    sub2api_time_bucket(created_at, 'hour', 'UTC') AS bucket_start,
    COALESCE(platform, 'unknown') AS platform,
    group_id AS group_id,
    is_business_limited AS is_business_limited,
    error_owner AS error_owner,
    status_code AS client_status_code,
    COALESCE(upstream_status_code, status_code, 0) AS effective_status_code
  FROM ops_error_logs
  WHERE pg_rfc3339_micros(created_at) >= pg_rfc3339_micros($1) AND pg_rfc3339_micros(created_at) < pg_rfc3339_micros($2)
    AND is_count_tokens = 0
),
error_dims AS (
  SELECT bucket_start, platform, group_id FROM error_base
  UNION
  SELECT bucket_start, platform, NULL FROM error_base
  UNION
  SELECT bucket_start, NULL, NULL FROM error_base
),
error_agg AS (
  SELECT
    d.bucket_start,
    d.platform,
    d.group_id,
    SUM(CASE WHEN COALESCE(e.client_status_code, 0) >= 400 THEN 1 ELSE 0 END) AS error_count_total,
    SUM(CASE WHEN COALESCE(e.client_status_code, 0) >= 400 AND e.is_business_limited THEN 1 ELSE 0 END) AS business_limited_count,
    SUM(CASE WHEN COALESCE(e.client_status_code, 0) >= 400 AND NOT e.is_business_limited THEN 1 ELSE 0 END) AS error_count_sla,
    SUM(CASE WHEN e.error_owner = 'provider' AND NOT e.is_business_limited
              AND COALESCE(e.effective_status_code, 0) NOT IN (429, 529) THEN 1 ELSE 0 END) AS upstream_error_count_excl_429_529,
    SUM(CASE WHEN e.error_owner = 'provider' AND NOT e.is_business_limited
              AND COALESCE(e.effective_status_code, 0) = 429 THEN 1 ELSE 0 END) AS upstream_429_count,
    SUM(CASE WHEN e.error_owner = 'provider' AND NOT e.is_business_limited
              AND COALESCE(e.effective_status_code, 0) = 529 THEN 1 ELSE 0 END) AS upstream_529_count
  FROM error_dims d
  JOIN error_base e
    ON e.bucket_start = d.bucket_start
   AND (d.platform IS NULL OR e.platform = d.platform)
   AND (d.group_id IS NULL OR e.group_id = d.group_id)
  GROUP BY d.bucket_start, d.platform, d.group_id
),
combined AS (
  SELECT
    COALESCE(u.bucket_start, e.bucket_start) AS bucket_start,
    COALESCE(u.platform, e.platform) AS platform,
    COALESCE(u.group_id, e.group_id) AS group_id,
    COALESCE(u.success_count, 0) AS success_count,
    COALESCE(u.ttft_sample_count, 0) AS ttft_sample_count,
    COALESCE(e.error_count_total, 0) AS error_count_total,
    COALESCE(e.business_limited_count, 0) AS business_limited_count,
    COALESCE(e.error_count_sla, 0) AS error_count_sla,
    COALESCE(e.upstream_error_count_excl_429_529, 0) AS upstream_error_count_excl_429_529,
    COALESCE(e.upstream_429_count, 0) AS upstream_429_count,
    COALESCE(e.upstream_529_count, 0) AS upstream_529_count,
    COALESCE(u.token_consumed, 0) AS token_consumed,
    u.duration_avg_ms,
    u.duration_max_ms,
    u.ttft_avg_ms,
    u.ttft_max_ms
  FROM usage_agg u
  LEFT JOIN error_agg e
    ON u.bucket_start = e.bucket_start
   AND COALESCE(u.platform, '') = COALESCE(e.platform, '')
   AND COALESCE(u.group_id, 0) = COALESCE(e.group_id, 0)
  UNION
  SELECT
    e.bucket_start,
    e.platform,
    e.group_id,
    0, 0,
    e.error_count_total,
    e.business_limited_count,
    e.error_count_sla,
    e.upstream_error_count_excl_429_529,
    e.upstream_429_count,
    e.upstream_529_count,
    0,
    NULL, NULL, NULL, NULL
  FROM error_agg e
  WHERE NOT EXISTS (
    SELECT 1 FROM usage_agg u
    WHERE u.bucket_start = e.bucket_start
      AND COALESCE(u.platform, '') = COALESCE(e.platform, '')
      AND COALESCE(u.group_id, 0) = COALESCE(e.group_id, 0)
  )
)
INSERT INTO ops_metrics_hourly (
  bucket_start, platform, group_id, success_count, ttft_sample_count,
  error_count_total, business_limited_count, error_count_sla,
  upstream_error_count_excl_429_529, upstream_429_count, upstream_529_count,
  token_consumed, duration_avg_ms, duration_max_ms, ttft_avg_ms, ttft_max_ms,
  duration_p50_ms, duration_p90_ms, duration_p95_ms, duration_p99_ms,
  ttft_p50_ms, ttft_p90_ms, ttft_p95_ms, ttft_p99_ms, computed_at
)
SELECT
  bucket_start,
  NULLIF(platform, ''),
  group_id,
  success_count,
  ttft_sample_count,
  error_count_total,
  business_limited_count,
  error_count_sla,
  upstream_error_count_excl_429_529,
  upstream_429_count,
  upstream_529_count,
  token_consumed,
  duration_avg_ms,
  CAST(duration_max_ms AS INTEGER),
  ttft_avg_ms,
  CAST(ttft_max_ms AS INTEGER),
  CAST(ROUND(duration_p50_ms) AS INTEGER), CAST(ROUND(duration_p90_ms) AS INTEGER),
  CAST(ROUND(duration_p95_ms) AS INTEGER), CAST(ROUND(duration_p99_ms) AS INTEGER),
  CAST(ROUND(ttft_p50_ms) AS INTEGER), CAST(ROUND(ttft_p90_ms) AS INTEGER),
  CAST(ROUND(ttft_p95_ms) AS INTEGER), CAST(ROUND(ttft_p99_ms) AS INTEGER),
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM combined LEFT JOIN latency_agg ON bucket_start=lbucket AND platform IS lplatform AND group_id IS lgroup
WHERE bucket_start IS NOT NULL
ON CONFLICT (bucket_start, COALESCE(platform, ''), COALESCE(group_id, 0)) DO UPDATE SET
  success_count = excluded.success_count,
  ttft_sample_count = excluded.ttft_sample_count,
  error_count_total = excluded.error_count_total,
  business_limited_count = excluded.business_limited_count,
  error_count_sla = excluded.error_count_sla,
  upstream_error_count_excl_429_529 = excluded.upstream_error_count_excl_429_529,
  upstream_429_count = excluded.upstream_429_count,
  upstream_529_count = excluded.upstream_529_count,
  token_consumed = excluded.token_consumed,
  duration_avg_ms = excluded.duration_avg_ms,
  duration_max_ms = excluded.duration_max_ms,
  ttft_avg_ms = excluded.ttft_avg_ms,
  ttft_max_ms = excluded.ttft_max_ms,
  duration_p50_ms = excluded.duration_p50_ms,
  duration_p90_ms = excluded.duration_p90_ms,
  duration_p95_ms = excluded.duration_p95_ms,
  duration_p99_ms = excluded.duration_p99_ms,
  ttft_p50_ms = excluded.ttft_p50_ms,
  ttft_p90_ms = excluded.ttft_p90_ms,
  ttft_p95_ms = excluded.ttft_p95_ms,
  ttft_p99_ms = excluded.ttft_p99_ms,
  computed_at = excluded.computed_at
`
	_, err := r.db.ExecContext(ctx, q, start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano))
	return err
}

func (r *opsRepository) upsertDailyMetricsSQLite(ctx context.Context, start, end time.Time) error {
	q := `
INSERT INTO ops_metrics_daily (
  bucket_date, platform, group_id, success_count, ttft_sample_count,
  error_count_total, business_limited_count, error_count_sla,
  upstream_error_count_excl_429_529, upstream_429_count, upstream_529_count,
  token_consumed, duration_avg_ms, duration_max_ms, ttft_avg_ms, ttft_max_ms, computed_at
)
SELECT
  substr(bucket_start, 1, 10) AS bucket_date,
  platform,
  group_id,
  COALESCE(SUM(success_count), 0),
  COALESCE(SUM(ttft_sample_count), 0),
  COALESCE(SUM(error_count_total), 0),
  COALESCE(SUM(business_limited_count), 0),
  COALESCE(SUM(error_count_sla), 0),
  COALESCE(SUM(upstream_error_count_excl_429_529), 0),
  COALESCE(SUM(upstream_429_count), 0),
  COALESCE(SUM(upstream_529_count), 0),
  COALESCE(SUM(token_consumed), 0),
  SUM(duration_avg_ms * success_count) / NULLIF(SUM(CASE WHEN duration_avg_ms IS NOT NULL THEN success_count ELSE 0 END), 0),
  MAX(duration_max_ms),
  SUM(ttft_avg_ms * ttft_sample_count) / NULLIF(SUM(CASE WHEN ttft_avg_ms IS NOT NULL THEN ttft_sample_count ELSE 0 END), 0),
  MAX(ttft_max_ms),
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM ops_metrics_hourly
WHERE pg_rfc3339_micros(bucket_start) >= pg_rfc3339_micros($1) AND pg_rfc3339_micros(bucket_start) < pg_rfc3339_micros($2)
GROUP BY 1, 2, 3
ON CONFLICT (bucket_date, COALESCE(platform, ''), COALESCE(group_id, 0)) DO UPDATE SET
  success_count = excluded.success_count,
  ttft_sample_count = excluded.ttft_sample_count,
  error_count_total = excluded.error_count_total,
  business_limited_count = excluded.business_limited_count,
  error_count_sla = excluded.error_count_sla,
  upstream_error_count_excl_429_529 = excluded.upstream_error_count_excl_429_529,
  upstream_429_count = excluded.upstream_429_count,
  upstream_529_count = excluded.upstream_529_count,
  token_consumed = excluded.token_consumed,
  duration_avg_ms = excluded.duration_avg_ms,
  duration_max_ms = excluded.duration_max_ms,
  ttft_avg_ms = excluded.ttft_avg_ms,
  ttft_max_ms = excluded.ttft_max_ms,
  computed_at = excluded.computed_at
`
	_, err := r.db.ExecContext(ctx, q, start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano))
	return err
}
