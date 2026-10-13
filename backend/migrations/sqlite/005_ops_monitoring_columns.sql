-- 004 created the ops monitoring tables from the base schema, but the
-- PostgreSQL migration chain later added columns that business code expects.
-- Keep this as a separate migration because 004 may already be applied.

ALTER TABLE ops_system_metrics
    ADD COLUMN redis_conn_total INTEGER;

ALTER TABLE ops_system_metrics
    ADD COLUMN redis_conn_idle INTEGER;

ALTER TABLE ops_alert_rules
    ADD COLUMN notify_email BOOLEAN NOT NULL DEFAULT 1;
