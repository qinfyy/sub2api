-- Columns added to usage_logs by PostgreSQL migrations after the Ent baseline.
-- 008 covered the image/session columns present when the SQLite bring-up began.
-- Later migrations added the remaining request/endpoint/billing audit columns.

ALTER TABLE usage_logs ADD COLUMN reasoning_effort VARCHAR(20);
ALTER TABLE usage_logs ADD COLUMN openai_ws_mode BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE usage_logs ADD COLUMN request_type SMALLINT NOT NULL DEFAULT 0;
ALTER TABLE usage_logs ADD COLUMN service_tier VARCHAR(16);
ALTER TABLE usage_logs ADD COLUMN inbound_endpoint VARCHAR(128);
ALTER TABLE usage_logs ADD COLUMN upstream_endpoint VARCHAR(128);
ALTER TABLE usage_logs ADD COLUMN native_compaction_v2 BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE usage_logs ADD COLUMN upstream_request_id VARCHAR(128);

CREATE INDEX IF NOT EXISTS idx_usage_logs_request_type_created_at
    ON usage_logs (request_type, created_at);
CREATE INDEX IF NOT EXISTS idx_usage_logs_service_tier_created_at
    ON usage_logs (service_tier, created_at);
