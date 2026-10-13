-- Create the channels table and align it with the PostgreSQL channel pricing
-- migrations. The table was originally introduced by PostgreSQL migration 081,
-- which is not part of the SQLite baseline, while later SQLite migrations already
-- reference it from channel_groups and channel_model_pricing.

CREATE TABLE IF NOT EXISTS channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    model_mapping TEXT NOT NULL DEFAULT '{}',
    billing_model_source VARCHAR(20) NOT NULL DEFAULT 'channel_mapped',
    restrict_models BOOLEAN NOT NULL DEFAULT 0,
    features TEXT NOT NULL DEFAULT '',
    features_config TEXT NOT NULL DEFAULT '{}',
    apply_pricing_to_account_stats BOOLEAN NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_channels_name ON channels(name);
CREATE INDEX IF NOT EXISTS idx_channels_status ON channels(status);

-- 009 is applied after 007 on new databases, but an interrupted or manually
-- repaired database may have 007 recorded with channel pricing tables already
-- present. Keep the child tables idempotent as well.
CREATE TABLE IF NOT EXISTS channel_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_channel_groups_group_id
    ON channel_groups(group_id);
CREATE INDEX IF NOT EXISTS idx_channel_groups_channel_id
    ON channel_groups(channel_id);

CREATE TABLE IF NOT EXISTS channel_model_pricing (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    platform VARCHAR(50) NOT NULL DEFAULT 'anthropic',
    models TEXT NOT NULL DEFAULT '[]',
    billing_mode VARCHAR(20) NOT NULL DEFAULT 'token',
    input_price REAL,
    output_price REAL,
    cache_write_price REAL,
    cache_write_1h_price REAL,
    cache_read_price REAL,
    image_input_price REAL,
    image_output_price REAL,
    per_request_price REAL,
    fast_multiplier REAL NOT NULL DEFAULT 1,
    flex_multiplier REAL NOT NULL DEFAULT 1,
    max_reasoning_effort_multiplier REAL,
    reasoning_effort_multipliers TEXT NOT NULL DEFAULT '{}',
    time_pricing TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_channel_model_pricing_channel_id
    ON channel_model_pricing(channel_id);
CREATE INDEX IF NOT EXISTS idx_channel_model_pricing_platform
    ON channel_model_pricing(platform);

CREATE TABLE IF NOT EXISTS channel_pricing_intervals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pricing_id INTEGER NOT NULL REFERENCES channel_model_pricing(id) ON DELETE CASCADE,
    min_tokens INTEGER NOT NULL DEFAULT 0,
    max_tokens INTEGER,
    tier_label VARCHAR(50),
    input_price REAL,
    output_price REAL,
    cache_write_price REAL,
    cache_write_1h_price REAL,
    cache_read_price REAL,
    input_multiplier REAL,
    output_multiplier REAL,
    cache_write_multiplier REAL,
    cache_read_multiplier REAL,
    per_request_price REAL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_channel_pricing_intervals_pricing_id
    ON channel_pricing_intervals(pricing_id);

CREATE TABLE IF NOT EXISTS channel_account_stats_pricing_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL DEFAULT '',
    group_ids TEXT NOT NULL DEFAULT '[]',
    account_ids TEXT NOT NULL DEFAULT '[]',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_cas_pricing_rules_channel_id
    ON channel_account_stats_pricing_rules(channel_id);

CREATE TABLE IF NOT EXISTS channel_account_stats_model_pricing (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL REFERENCES channel_account_stats_pricing_rules(id) ON DELETE CASCADE,
    platform VARCHAR(50) NOT NULL DEFAULT '',
    models TEXT NOT NULL DEFAULT '[]',
    billing_mode VARCHAR(20) NOT NULL DEFAULT 'token',
    input_price REAL,
    output_price REAL,
    cache_write_price REAL,
    cache_write_1h_price REAL,
    cache_read_price REAL,
    image_output_price REAL,
    per_request_price REAL,
    reasoning_effort_multipliers TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_cas_model_pricing_rule_id
    ON channel_account_stats_model_pricing(rule_id);

CREATE TABLE IF NOT EXISTS channel_account_stats_pricing_intervals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pricing_id INTEGER NOT NULL REFERENCES channel_account_stats_model_pricing(id) ON DELETE CASCADE,
    min_tokens INTEGER,
    max_tokens INTEGER,
    tier_label VARCHAR(100) NOT NULL DEFAULT '',
    input_price REAL,
    output_price REAL,
    cache_write_price REAL,
    cache_write_1h_price REAL,
    cache_read_price REAL,
    per_request_price REAL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_cas_pricing_intervals_pricing_id
    ON channel_account_stats_pricing_intervals(pricing_id);
