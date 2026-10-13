-- Align channel pricing tables created by 007 with the latest PostgreSQL
-- pricing migrations. SQLite does not support ADD COLUMN IF NOT EXISTS, but the
-- migration runner skips an ADD COLUMN when the target column already exists.

ALTER TABLE channel_model_pricing ADD COLUMN image_input_price REAL;
ALTER TABLE channel_model_pricing ADD COLUMN max_reasoning_effort_multiplier REAL DEFAULT 1;

ALTER TABLE channel_account_stats_model_pricing ADD COLUMN image_input_price REAL;
ALTER TABLE channel_account_stats_model_pricing ADD COLUMN max_reasoning_effort_multiplier REAL DEFAULT 1;
