-- Columns added to usage_logs by PostgreSQL migrations after the Ent baseline.
-- The SQLite migration runner records this file only after the whole
-- transaction commits, so a failed or partial run is retried from scratch.

ALTER TABLE usage_logs ADD COLUMN image_output_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE usage_logs ADD COLUMN image_output_cost REAL NOT NULL DEFAULT 0;
ALTER TABLE usage_logs ADD COLUMN account_stats_cost REAL;
ALTER TABLE usage_logs ADD COLUMN image_input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE usage_logs ADD COLUMN image_input_cost REAL NOT NULL DEFAULT 0;
ALTER TABLE usage_logs ADD COLUMN session_id VARCHAR(255);
ALTER TABLE usage_logs ADD COLUMN requested_reasoning_effort VARCHAR(20);
