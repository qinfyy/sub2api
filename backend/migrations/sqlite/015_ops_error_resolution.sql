-- Match PostgreSQL migrations 038 and 147 without discarding early SQLite data.
ALTER TABLE ops_error_logs ADD COLUMN resolved BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE ops_error_logs ADD COLUMN resolved_by_user_id INTEGER;
ALTER TABLE ops_error_logs ADD COLUMN api_key_prefix VARCHAR(32);
UPDATE ops_error_logs SET resolved=is_resolved WHERE is_resolved=1;
UPDATE ops_error_logs SET resolved_by_user_id=resolved_by WHERE resolved_by_user_id IS NULL;
CREATE INDEX IF NOT EXISTS idx_ops_error_logs_resolved_time ON ops_error_logs(resolved,created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ops_error_logs_unresolved_time ON ops_error_logs(created_at DESC) WHERE resolved=0;
CREATE INDEX IF NOT EXISTS idx_ops_error_logs_user_time ON ops_error_logs(user_id,created_at DESC) WHERE user_id IS NOT NULL;
