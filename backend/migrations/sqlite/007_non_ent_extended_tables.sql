-- Additional non-Ent tables used by business code. Kept in a new migration
-- because 004 may already be recorded as applied on existing SQLite databases.

CREATE TABLE IF NOT EXISTS content_moderation_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id VARCHAR(128) NOT NULL DEFAULT '',
    user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    user_email VARCHAR(255) NOT NULL DEFAULT '',
    api_key_id INTEGER REFERENCES api_keys(id) ON DELETE SET NULL,
    api_key_name VARCHAR(100) NOT NULL DEFAULT '',
    group_id INTEGER REFERENCES groups(id) ON DELETE SET NULL,
    group_name VARCHAR(255) NOT NULL DEFAULT '',
    endpoint VARCHAR(128) NOT NULL DEFAULT '',
    provider VARCHAR(64) NOT NULL DEFAULT '',
    model VARCHAR(255) NOT NULL DEFAULT '',
    mode VARCHAR(32) NOT NULL DEFAULT '',
    action VARCHAR(32) NOT NULL DEFAULT '',
    flagged BOOLEAN NOT NULL DEFAULT 0,
    highest_category VARCHAR(64) NOT NULL DEFAULT '',
    highest_score REAL NOT NULL DEFAULT 0,
    category_scores TEXT NOT NULL DEFAULT '{}',
    threshold_snapshot TEXT NOT NULL DEFAULT '{}',
    input_excerpt TEXT NOT NULL DEFAULT '',
    upstream_latency_ms INTEGER,
    error TEXT NOT NULL DEFAULT '',
    violation_count INTEGER NOT NULL DEFAULT 0,
    auto_banned BOOLEAN NOT NULL DEFAULT 0,
    email_sent BOOLEAN NOT NULL DEFAULT 0,
    queue_delay_ms INTEGER,
    engine_meta TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_created_at
    ON content_moderation_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_group_created_at
    ON content_moderation_logs(group_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_flagged_created_at
    ON content_moderation_logs(flagged, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_user_created_at
    ON content_moderation_logs(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_api_key_created_at
    ON content_moderation_logs(api_key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_endpoint_created_at
    ON content_moderation_logs(endpoint, created_at DESC);

CREATE TABLE IF NOT EXISTS user_provider_default_grants (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_type VARCHAR(20) NOT NULL,
    grant_reason VARCHAR(20) NOT NULL DEFAULT 'first_bind',
    granted_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CONSTRAINT user_provider_default_grants_provider_type_check
        CHECK (provider_type IN ('email', 'linuxdo', 'wechat', 'oidc')),
    CONSTRAINT user_provider_default_grants_reason_check
        CHECK (grant_reason IN ('signup', 'first_bind'))
);

CREATE UNIQUE INDEX IF NOT EXISTS user_provider_default_grants_user_provider_reason_key
    ON user_provider_default_grants(user_id, provider_type, grant_reason);
CREATE INDEX IF NOT EXISTS user_provider_default_grants_user_id_idx
    ON user_provider_default_grants(user_id);

CREATE TABLE IF NOT EXISTS passkey_user_handles (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    user_handle BLOB NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS passkey_credentials (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id BLOB NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL DEFAULT 'Passkey',
    credential_data TEXT NOT NULL,
    last_used_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS passkey_credentials_user_id_idx
    ON passkey_credentials(user_id);
CREATE INDEX IF NOT EXISTS passkey_credentials_last_used_at_idx
    ON passkey_credentials(last_used_at);

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
    platform VARCHAR(50) NOT NULL DEFAULT '',
    models TEXT NOT NULL DEFAULT '[]',
    billing_mode VARCHAR(20) NOT NULL DEFAULT 'token',
    input_price REAL,
    output_price REAL,
    cache_write_price REAL,
    cache_read_price REAL,
    cache_write_1h_price REAL,
    image_output_price REAL,
    per_request_price REAL,
    fast_multiplier REAL NOT NULL DEFAULT 1,
    flex_multiplier REAL NOT NULL DEFAULT 1,
    max_reasoning_effort_multiplier REAL NOT NULL DEFAULT 1,
    reasoning_effort_multipliers TEXT NOT NULL DEFAULT '{}',
    time_pricing TEXT NOT NULL DEFAULT '{}',
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
    min_tokens BIGINT,
    max_tokens BIGINT,
    tier_label VARCHAR(100) NOT NULL DEFAULT '',
    input_price REAL,
    output_price REAL,
    cache_write_price REAL,
    cache_read_price REAL,
    cache_write_1h_price REAL,
    per_request_price REAL,
    input_multiplier REAL NOT NULL DEFAULT 1,
    output_multiplier REAL NOT NULL DEFAULT 1,
    cache_write_multiplier REAL NOT NULL DEFAULT 1,
    cache_read_multiplier REAL NOT NULL DEFAULT 1,
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
    cache_read_price REAL,
    cache_write_1h_price REAL,
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
    min_tokens BIGINT,
    max_tokens BIGINT,
    tier_label VARCHAR(100) NOT NULL DEFAULT '',
    input_price REAL,
    output_price REAL,
    cache_write_price REAL,
    cache_read_price REAL,
    cache_write_1h_price REAL,
    per_request_price REAL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_cas_pricing_intervals_pricing_id
    ON channel_account_stats_pricing_intervals(pricing_id);

CREATE TABLE IF NOT EXISTS user_affiliates (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    aff_code VARCHAR(32) NOT NULL UNIQUE,
    inviter_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    aff_count INTEGER NOT NULL DEFAULT 0,
    aff_quota REAL NOT NULL DEFAULT 0,
    aff_frozen_quota REAL NOT NULL DEFAULT 0,
    aff_history_quota REAL NOT NULL DEFAULT 0,
    aff_rebate_rate_percent REAL,
    aff_code_custom BOOLEAN NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_user_affiliates_inviter_id
    ON user_affiliates(inviter_id);
CREATE INDEX IF NOT EXISTS idx_user_affiliates_aff_quota
    ON user_affiliates(aff_quota);
CREATE INDEX IF NOT EXISTS idx_user_affiliates_admin_settings
    ON user_affiliates(updated_at)
    WHERE aff_code_custom = 1 OR aff_rebate_rate_percent IS NOT NULL;

CREATE TABLE IF NOT EXISTS user_affiliate_ledger (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action VARCHAR(32) NOT NULL,
    amount REAL NOT NULL,
    source_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    source_order_id INTEGER REFERENCES payment_orders(id) ON DELETE SET NULL,
    balance_after REAL,
    aff_quota_after REAL,
    aff_frozen_quota_after REAL,
    aff_history_quota_after REAL,
    frozen_until TEXT,
    operation_id VARCHAR(64),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_user_affiliate_ledger_user_id
    ON user_affiliate_ledger(user_id);
CREATE INDEX IF NOT EXISTS idx_user_affiliate_ledger_action
    ON user_affiliate_ledger(action);
CREATE INDEX IF NOT EXISTS idx_ual_frozen_thaw
    ON user_affiliate_ledger(user_id, frozen_until)
    WHERE frozen_until IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_user_affiliate_ledger_source_order_id
    ON user_affiliate_ledger(source_order_id)
    WHERE source_order_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_user_affiliate_ledger_rebate_lookup
    ON user_affiliate_ledger(action, source_order_id, user_id, source_user_id, created_at)
    WHERE action = 'accrue';
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_affiliate_ledger_operation_id
    ON user_affiliate_ledger(operation_id)
    WHERE operation_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS user_group_rate_multipliers (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    rate_multiplier REAL,
    rpm_override INTEGER,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (user_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_user_group_rate_multipliers_group_id
    ON user_group_rate_multipliers(group_id);

CREATE TABLE IF NOT EXISTS usage_dashboard_hourly (
    bucket_start TEXT PRIMARY KEY,
    total_requests BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    total_cost REAL NOT NULL DEFAULT 0,
    actual_cost REAL NOT NULL DEFAULT 0,
    account_cost REAL NOT NULL DEFAULT 0,
    total_duration_ms BIGINT NOT NULL DEFAULT 0,
    active_users BIGINT NOT NULL DEFAULT 0,
    computed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_usage_dashboard_hourly_bucket_start
    ON usage_dashboard_hourly(bucket_start DESC);

CREATE TABLE IF NOT EXISTS usage_dashboard_daily (
    bucket_date TEXT PRIMARY KEY,
    total_requests BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    total_cost REAL NOT NULL DEFAULT 0,
    actual_cost REAL NOT NULL DEFAULT 0,
    account_cost REAL NOT NULL DEFAULT 0,
    total_duration_ms BIGINT NOT NULL DEFAULT 0,
    active_users BIGINT NOT NULL DEFAULT 0,
    computed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_usage_dashboard_daily_bucket_date
    ON usage_dashboard_daily(bucket_date DESC);

CREATE TABLE IF NOT EXISTS usage_dashboard_hourly_users (
    bucket_start TEXT NOT NULL,
    user_id INTEGER NOT NULL,
    PRIMARY KEY (bucket_start, user_id)
);

CREATE INDEX IF NOT EXISTS idx_usage_dashboard_hourly_users_bucket_start
    ON usage_dashboard_hourly_users(bucket_start);

CREATE TABLE IF NOT EXISTS usage_dashboard_daily_users (
    bucket_date TEXT NOT NULL,
    user_id INTEGER NOT NULL,
    PRIMARY KEY (bucket_date, user_id)
);

CREATE INDEX IF NOT EXISTS idx_usage_dashboard_daily_users_bucket_date
    ON usage_dashboard_daily_users(bucket_date);

CREATE TABLE IF NOT EXISTS usage_dashboard_aggregation_watermark (
    id INTEGER PRIMARY KEY,
    last_aggregated_at TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO usage_dashboard_aggregation_watermark (id)
VALUES (1)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS usage_group_daily_rollups (
    bucket_date TEXT NOT NULL,
    group_id INTEGER NOT NULL,
    actual_cost REAL NOT NULL DEFAULT 0,
    computed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (bucket_date, group_id)
);

CREATE TABLE IF NOT EXISTS usage_group_rollup_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    closed_before TEXT NOT NULL DEFAULT '1970-01-01',
    retained_from TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    timezone_name TEXT NOT NULL DEFAULT 'Asia/Shanghai',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO usage_group_rollup_state (id, closed_before, retained_from)
VALUES (1, '1970-01-01', '1970-01-01T00:00:00Z')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS ops_alert_silences (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL,
    platform VARCHAR(64) NOT NULL,
    group_id INTEGER,
    region VARCHAR(64),
    until TEXT NOT NULL,
    reason TEXT,
    created_by INTEGER,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_ops_alert_silences_lookup
    ON ops_alert_silences(rule_id, platform, group_id, region, until);

CREATE TABLE IF NOT EXISTS ops_ingress_reject_aggregates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    bucket_start TEXT NOT NULL,
    reject_reason VARCHAR(64) NOT NULL,
    route_family VARCHAR(64) NOT NULL,
    protocol VARCHAR(32) NOT NULL,
    client_ip TEXT NOT NULL,
    user_id INTEGER NOT NULL DEFAULT 0,
    api_key_id INTEGER NOT NULL DEFAULT 0,
    request_count BIGINT NOT NULL DEFAULT 0,
    first_seen TEXT NOT NULL,
    last_seen TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CONSTRAINT ops_ingress_reject_aggregates_dimensions_unique UNIQUE
        (bucket_start, reject_reason, route_family, protocol, client_ip, user_id, api_key_id)
);

CREATE INDEX IF NOT EXISTS idx_ops_ingress_reject_aggregates_bucket
    ON ops_ingress_reject_aggregates(bucket_start DESC);
CREATE INDEX IF NOT EXISTS idx_ops_ingress_reject_aggregates_reason_bucket
    ON ops_ingress_reject_aggregates(reject_reason, bucket_start DESC);
CREATE INDEX IF NOT EXISTS idx_ops_ingress_reject_aggregates_ip_bucket
    ON ops_ingress_reject_aggregates(client_ip, bucket_start DESC);

CREATE TABLE IF NOT EXISTS channel_monitor_aggregation_watermark (
    id INTEGER PRIMARY KEY,
    last_aggregated_at TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS channel_monitor_v2_config (
    id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    version INTEGER NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT 1,
    refresh_interval_seconds INTEGER NOT NULL DEFAULT 60
        CHECK (refresh_interval_seconds IN (60, 300)),
    platforms TEXT NOT NULL DEFAULT '[{"platform":"anthropic","enabled":true,"models":[]},{"platform":"openai","enabled":true,"models":[]},{"platform":"grok","enabled":true,"models":[]},{"platform":"kiro","enabled":true,"models":[]},{"platform":"gemini","enabled":true,"models":[]},{"platform":"antigravity","enabled":true,"models":[]}]',
    group_ids TEXT NOT NULL DEFAULT '[]',
    ignored_error_categories TEXT NOT NULL DEFAULT '[]',
    health_thresholds TEXT NOT NULL DEFAULT '{}',
    updated_by INTEGER,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO channel_monitor_v2_config (id) VALUES (1)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS channel_monitor_v2_metrics_1m (
    bucket_start TEXT NOT NULL,
    platform TEXT NOT NULL,
    group_id INTEGER NOT NULL DEFAULT 0,
    model TEXT NOT NULL,
    success_requests BIGINT NOT NULL DEFAULT 0,
    error_requests BIGINT NOT NULL DEFAULT 0,
    upstream_affected_requests BIGINT NOT NULL DEFAULT 0,
    upstream_attempt_count BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    ttft_sum_ms BIGINT NOT NULL DEFAULT 0,
    ttft_count BIGINT NOT NULL DEFAULT 0,
    duration_sum_ms BIGINT NOT NULL DEFAULT 0,
    duration_count BIGINT NOT NULL DEFAULT 0,
    computed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (bucket_start, platform, group_id, model)
);

CREATE INDEX IF NOT EXISTS idx_channel_monitor_v2_metrics_platform_time
    ON channel_monitor_v2_metrics_1m(platform, bucket_start DESC);
CREATE INDEX IF NOT EXISTS idx_channel_monitor_v2_metrics_group_time
    ON channel_monitor_v2_metrics_1m(group_id, bucket_start DESC);
CREATE INDEX IF NOT EXISTS idx_channel_monitor_v2_metrics_model_time
    ON channel_monitor_v2_metrics_1m(model, bucket_start DESC);

CREATE TABLE IF NOT EXISTS channel_monitor_v2_user_metrics_1m (
    bucket_start TEXT NOT NULL,
    platform TEXT NOT NULL,
    group_id INTEGER NOT NULL DEFAULT 0,
    model TEXT NOT NULL,
    user_id INTEGER NOT NULL,
    success_requests BIGINT NOT NULL DEFAULT 0,
    error_requests BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    ttft_sum_ms BIGINT NOT NULL DEFAULT 0,
    ttft_count BIGINT NOT NULL DEFAULT 0,
    duration_sum_ms BIGINT NOT NULL DEFAULT 0,
    duration_count BIGINT NOT NULL DEFAULT 0,
    computed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (bucket_start, platform, group_id, model, user_id)
);

CREATE INDEX IF NOT EXISTS idx_channel_monitor_v2_user_metrics_user_time
    ON channel_monitor_v2_user_metrics_1m(user_id, bucket_start DESC);
CREATE INDEX IF NOT EXISTS idx_channel_monitor_v2_user_metrics_time
    ON channel_monitor_v2_user_metrics_1m(bucket_start DESC);

CREATE TABLE IF NOT EXISTS channel_monitor_v2_error_metrics_1m (
    bucket_start TEXT NOT NULL,
    platform TEXT NOT NULL,
    group_id INTEGER NOT NULL DEFAULT 0,
    model TEXT NOT NULL,
    error_category TEXT NOT NULL,
    taxonomy_version SMALLINT NOT NULL,
    error_requests BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_start, platform, group_id, model, error_category, taxonomy_version)
);

CREATE INDEX IF NOT EXISTS idx_channel_monitor_v2_errors_time
    ON channel_monitor_v2_error_metrics_1m(bucket_start DESC);
CREATE INDEX IF NOT EXISTS idx_channel_monitor_v2_errors_category_time
    ON channel_monitor_v2_error_metrics_1m(error_category, bucket_start DESC);

CREATE TABLE IF NOT EXISTS channel_monitor_v2_latency_histograms_1m (
    bucket_start TEXT NOT NULL,
    platform TEXT NOT NULL,
    group_id INTEGER NOT NULL DEFAULT 0,
    model TEXT NOT NULL,
    user_id INTEGER NOT NULL DEFAULT 0,
    metric TEXT NOT NULL CHECK (metric IN ('ttft', 'duration')),
    upper_bound_ms INTEGER NOT NULL,
    sample_count BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_start, platform, group_id, model, user_id, metric, upper_bound_ms)
);

CREATE INDEX IF NOT EXISTS idx_channel_monitor_v2_histograms_time
    ON channel_monitor_v2_latency_histograms_1m(bucket_start DESC, metric);

CREATE TABLE IF NOT EXISTS channel_monitor_v2_watermarks (
    id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    usage_coverage_start TEXT,
    error_coverage_start TEXT,
    data_through TEXT,
    last_successful_at TEXT,
    backfill_cursor TEXT,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO channel_monitor_v2_watermarks (id) VALUES (1)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS channel_monitor_v2_metrics_rollup (
    bucket_start TEXT NOT NULL,
    bucket_seconds INTEGER NOT NULL CHECK (bucket_seconds IN (300, 3600, 43200, 86400)),
    platform TEXT NOT NULL,
    group_id INTEGER NOT NULL DEFAULT 0,
    model TEXT NOT NULL,
    success_requests BIGINT NOT NULL DEFAULT 0,
    error_requests BIGINT NOT NULL DEFAULT 0,
    upstream_affected_requests BIGINT NOT NULL DEFAULT 0,
    upstream_attempt_count BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    ttft_sum_ms BIGINT NOT NULL DEFAULT 0,
    ttft_count BIGINT NOT NULL DEFAULT 0,
    duration_sum_ms BIGINT NOT NULL DEFAULT 0,
    duration_count BIGINT NOT NULL DEFAULT 0,
    computed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (bucket_seconds, bucket_start, platform, group_id, model)
);

CREATE INDEX IF NOT EXISTS idx_channel_monitor_v2_metrics_rollup_platform_time
    ON channel_monitor_v2_metrics_rollup(bucket_seconds, platform, bucket_start DESC);
CREATE INDEX IF NOT EXISTS idx_channel_monitor_v2_metrics_rollup_group_time
    ON channel_monitor_v2_metrics_rollup(bucket_seconds, group_id, bucket_start DESC);
CREATE INDEX IF NOT EXISTS idx_channel_monitor_v2_metrics_rollup_model_time
    ON channel_monitor_v2_metrics_rollup(bucket_seconds, model, bucket_start DESC);

CREATE TABLE IF NOT EXISTS channel_monitor_v2_user_metrics_rollup (
    bucket_start TEXT NOT NULL,
    bucket_seconds INTEGER NOT NULL CHECK (bucket_seconds IN (300, 3600, 43200, 86400)),
    platform TEXT NOT NULL,
    group_id INTEGER NOT NULL DEFAULT 0,
    model TEXT NOT NULL,
    user_id INTEGER NOT NULL,
    success_requests BIGINT NOT NULL DEFAULT 0,
    error_requests BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    ttft_sum_ms BIGINT NOT NULL DEFAULT 0,
    ttft_count BIGINT NOT NULL DEFAULT 0,
    duration_sum_ms BIGINT NOT NULL DEFAULT 0,
    duration_count BIGINT NOT NULL DEFAULT 0,
    computed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (bucket_seconds, bucket_start, platform, group_id, model, user_id)
);

CREATE INDEX IF NOT EXISTS idx_channel_monitor_v2_user_rollup_user_time
    ON channel_monitor_v2_user_metrics_rollup(bucket_seconds, user_id, bucket_start DESC);
CREATE INDEX IF NOT EXISTS idx_channel_monitor_v2_user_rollup_time
    ON channel_monitor_v2_user_metrics_rollup(bucket_seconds, bucket_start DESC);

CREATE TABLE IF NOT EXISTS channel_monitor_v2_error_metrics_rollup (
    bucket_start TEXT NOT NULL,
    bucket_seconds INTEGER NOT NULL CHECK (bucket_seconds IN (300, 3600, 43200, 86400)),
    platform TEXT NOT NULL,
    group_id INTEGER NOT NULL DEFAULT 0,
    model TEXT NOT NULL,
    error_category TEXT NOT NULL,
    taxonomy_version SMALLINT NOT NULL,
    error_requests BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_seconds, bucket_start, platform, group_id, model, error_category, taxonomy_version)
);

CREATE INDEX IF NOT EXISTS idx_channel_monitor_v2_errors_rollup_time
    ON channel_monitor_v2_error_metrics_rollup(bucket_seconds, bucket_start DESC);
CREATE INDEX IF NOT EXISTS idx_channel_monitor_v2_errors_rollup_category_time
    ON channel_monitor_v2_error_metrics_rollup(bucket_seconds, error_category, bucket_start DESC);

CREATE TABLE IF NOT EXISTS channel_monitor_v2_latency_histograms_rollup (
    bucket_start TEXT NOT NULL,
    bucket_seconds INTEGER NOT NULL CHECK (bucket_seconds IN (300, 3600, 43200, 86400)),
    platform TEXT NOT NULL,
    group_id INTEGER NOT NULL DEFAULT 0,
    model TEXT NOT NULL,
    user_id INTEGER NOT NULL DEFAULT 0,
    metric TEXT NOT NULL CHECK (metric IN ('ttft', 'duration')),
    upper_bound_ms INTEGER NOT NULL,
    sample_count BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_seconds, bucket_start, platform, group_id, model, user_id, metric, upper_bound_ms)
);

CREATE INDEX IF NOT EXISTS idx_channel_monitor_v2_histograms_rollup_time
    ON channel_monitor_v2_latency_histograms_rollup(bucket_seconds, bucket_start DESC, metric);
