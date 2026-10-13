-- Retain the early SQLite watermark and add the date column used by the service.
ALTER TABLE channel_monitor_aggregation_watermark ADD COLUMN last_aggregated_date TEXT;
UPDATE channel_monitor_aggregation_watermark
SET last_aggregated_date = sub2api_time_bucket(last_aggregated_at, 'day', 'UTC')
WHERE last_aggregated_date IS NULL
  AND pg_rfc3339_micros(last_aggregated_at) > 0;
