-- PostgreSQL migrations 161, 169 and 187 add fields beyond the Ent baseline.
ALTER TABLE batch_image_jobs ADD COLUMN parent_batch_id VARCHAR(64);
ALTER TABLE batch_image_jobs ADD COLUMN session_id VARCHAR(255);
ALTER TABLE batch_image_jobs ADD COLUMN base_unit_price REAL NOT NULL DEFAULT 0;
ALTER TABLE batch_image_jobs ADD COLUMN group_rate_multiplier REAL NOT NULL DEFAULT 1.0;
ALTER TABLE batch_image_jobs ADD COLUMN account_rate_multiplier REAL NOT NULL DEFAULT 1.0;
ALTER TABLE batch_image_jobs ADD COLUMN batch_discount_multiplier REAL NOT NULL DEFAULT 0.5;
ALTER TABLE batch_image_jobs ADD COLUMN hold_multiplier REAL NOT NULL DEFAULT 0.6;
ALTER TABLE batch_image_jobs ADD COLUMN billable_unit_price REAL NOT NULL DEFAULT 0;
ALTER TABLE batch_image_jobs ADD COLUMN hold_unit_price REAL NOT NULL DEFAULT 0;
ALTER TABLE batch_image_jobs ADD COLUMN pricing_snapshot_version INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS batch_image_jobs_parent_batch_id_idx
    ON batch_image_jobs(parent_batch_id) WHERE parent_batch_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS batch_image_jobs_task_name_idx ON batch_image_jobs(task_name);
