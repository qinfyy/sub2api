-- Reviewed SQLite v1 baseline. Generated from the schema and experimental
-- migrations at the Ent backend refactor boundary. Do not regenerate in production.

CREATE TABLE `account_groups` (`priority` integer NOT NULL DEFAULT (50), `created_at` datetime NOT NULL, `account_id` integer NOT NULL, `group_id` integer NOT NULL, PRIMARY KEY (`account_id`, `group_id`), CONSTRAINT `account_groups_accounts_account` FOREIGN KEY (`account_id`) REFERENCES `accounts` (`id`) ON DELETE NO ACTION, CONSTRAINT `account_groups_groups_group` FOREIGN KEY (`group_id`) REFERENCES `groups` (`id`) ON DELETE NO ACTION);

CREATE TABLE `accounts` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `deleted_at` datetime NULL, `name` text NOT NULL, `notes` text NULL, `platform` text NOT NULL, `type` text NOT NULL, `credentials` json NOT NULL, `extra` json NOT NULL, `proxy_fallback_origin_id` integer NULL, `concurrency` integer NOT NULL DEFAULT (3), `load_factor` integer NULL, `priority` integer NOT NULL DEFAULT (50), `rate_multiplier` real NOT NULL DEFAULT (1), `status` text NOT NULL DEFAULT ('active'), `error_message` text NULL, `last_used_at` datetime NULL, `expires_at` datetime NULL, `auto_pause_on_expired` bool NOT NULL DEFAULT (true), `schedulable` bool NOT NULL DEFAULT (true), `rate_limited_at` datetime NULL, `rate_limit_reset_at` datetime NULL, `overload_until` datetime NULL, `temp_unschedulable_until` datetime NULL, `temp_unschedulable_reason` text NULL, `session_window_start` datetime NULL, `session_window_end` datetime NULL, `session_window_status` text NULL, `quota_dimension` text NOT NULL DEFAULT ('global'), `proxy_id` integer NULL, `parent_account_id` integer NULL, CONSTRAINT `accounts_proxies_proxy` FOREIGN KEY (`proxy_id`) REFERENCES `proxies` (`id`) ON DELETE SET NULL, CONSTRAINT `accounts_accounts_children` FOREIGN KEY (`parent_account_id`) REFERENCES `accounts` (`id`) ON DELETE RESTRICT);

CREATE TABLE `announcement_reads` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `read_at` datetime NOT NULL, `created_at` datetime NOT NULL, `announcement_id` integer NOT NULL, `user_id` integer NOT NULL, CONSTRAINT `announcement_reads_announcements_reads` FOREIGN KEY (`announcement_id`) REFERENCES `announcements` (`id`) ON DELETE NO ACTION, CONSTRAINT `announcement_reads_users_announcement_reads` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE NO ACTION);

CREATE TABLE `announcements` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `title` text NOT NULL, `content` text NOT NULL, `status` text NOT NULL DEFAULT ('draft'), `notify_mode` text NOT NULL DEFAULT ('silent'), `targeting` json NULL, `starts_at` datetime NULL, `ends_at` datetime NULL, `created_by` integer NULL, `updated_by` integer NULL, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL);

CREATE TABLE `api_keys` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `deleted_at` datetime NULL, `key` text NOT NULL, `name` text NOT NULL, `status` text NOT NULL DEFAULT ('active'), `last_used_at` datetime NULL, `ip_whitelist` json NULL, `ip_blacklist` json NULL, `quota` real NOT NULL DEFAULT (0), `quota_used` real NOT NULL DEFAULT (0), `expires_at` datetime NULL, `rate_limit_5h` real NOT NULL DEFAULT (0), `rate_limit_1d` real NOT NULL DEFAULT (0), `rate_limit_7d` real NOT NULL DEFAULT (0), `usage_5h` real NOT NULL DEFAULT (0), `usage_1d` real NOT NULL DEFAULT (0), `usage_7d` real NOT NULL DEFAULT (0), `window_5h_start` datetime NULL, `window_1d_start` datetime NULL, `window_7d_start` datetime NULL, `group_id` integer NULL, `user_id` integer NOT NULL, CONSTRAINT `api_keys_groups_api_keys` FOREIGN KEY (`group_id`) REFERENCES `groups` (`id`) ON DELETE SET NULL, CONSTRAINT `api_keys_users_api_keys` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE NO ACTION);

CREATE TABLE audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    actor_user_id INTEGER,
    actor_email VARCHAR(255) NOT NULL DEFAULT '',
    actor_role VARCHAR(32) NOT NULL DEFAULT '',
    auth_method VARCHAR(32) NOT NULL DEFAULT '',
    credential_masked VARCHAR(160) NOT NULL DEFAULT '',
    action VARCHAR(128) NOT NULL DEFAULT '',
    method VARCHAR(16) NOT NULL DEFAULT '',
    path VARCHAR(512) NOT NULL DEFAULT '',
    request_id VARCHAR(64) NOT NULL DEFAULT '',
    client_ip VARCHAR(64) NOT NULL DEFAULT '',
    user_agent VARCHAR(512) NOT NULL DEFAULT '',
    request_body TEXT NOT NULL DEFAULT '',
    status_code INTEGER NOT NULL DEFAULT 0,
    latency_ms INTEGER NOT NULL DEFAULT 0,
    extra TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE auth_cache_invalidation_outbox (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cache_key TEXT NOT NULL CHECK (length(cache_key) = 64),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    available_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    delivery_stage INTEGER NOT NULL DEFAULT 0 CHECK (delivery_stage IN (0, 1)),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error TEXT,
    claimed_at TEXT,
    claimed_by TEXT
);

CREATE TABLE `auth_identities` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `provider_type` text NOT NULL, `provider_key` text NOT NULL, `provider_subject` text NOT NULL, `verified_at` datetime NULL, `issuer` text NULL, `metadata` json NOT NULL, `user_id` integer NOT NULL, CONSTRAINT `auth_identities_users_auth_identities` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE);

CREATE TABLE `auth_identity_channels` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `provider_type` text NOT NULL, `provider_key` text NOT NULL, `channel` text NOT NULL, `channel_app_id` text NOT NULL, `channel_subject` text NOT NULL, `metadata` json NOT NULL, `identity_id` integer NOT NULL, CONSTRAINT `auth_identity_channels_auth_identities_channels` FOREIGN KEY (`identity_id`) REFERENCES `auth_identities` (`id`) ON DELETE CASCADE);

CREATE TABLE "batch_image_events" (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL REFERENCES batch_image_jobs(batch_id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    payload JSON,
    event_hash TEXT,
    created_at DATETIME NOT NULL
);

CREATE TABLE "batch_image_items" (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL REFERENCES batch_image_jobs(batch_id) ON DELETE CASCADE,
    custom_id TEXT NOT NULL,
    status TEXT NOT NULL,
    request_hash TEXT,
    prompt_preview TEXT,
    provider_source_object TEXT,
    source_line_number INTEGER,
    source_byte_offset INTEGER,
    source_byte_length INTEGER,
    mime_type TEXT,
    file_extension TEXT,
    image_count INTEGER NOT NULL DEFAULT 0,
    error_code TEXT,
    error_message TEXT,
    billed_amount REAL,
    created_at DATETIME NOT NULL,
    indexed_at DATETIME
);

CREATE TABLE `batch_image_jobs` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `batch_id` text NOT NULL, `user_id` integer NOT NULL, `api_key_id` integer NULL, `account_id` integer NULL, `provider` text NOT NULL, `model` text NOT NULL, `task_name` text NOT NULL DEFAULT (''), `status` text NOT NULL DEFAULT ('created'), `provider_job_name` text NULL, `provider_input_ref` text NULL, `provider_output_ref` text NULL, `gcs_input_uri` text NULL, `gcs_output_uri` text NULL, `item_count` integer NOT NULL, `success_count` integer NOT NULL DEFAULT (0), `fail_count` integer NOT NULL DEFAULT (0), `cancelled_count` integer NOT NULL DEFAULT (0), `estimated_cost` real NOT NULL DEFAULT (0), `hold_amount` real NULL, `actual_cost` real NULL, `currency` text NOT NULL DEFAULT ('USD'), `hold_id` text NULL, `idempotency_key` text NULL, `request_hash` text NULL, `manifest_hash` text NULL, `retry_count` integer NOT NULL DEFAULT (0), `version` integer NOT NULL DEFAULT (0), `output_expires_at` datetime NULL, `input_deleted_at` datetime NULL, `output_deleted_at` datetime NULL, `downloaded_at` datetime NULL, `user_deleted_at` datetime NULL, `last_error_code` text NULL, `last_error_message` text NULL, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `submitted_at` datetime NULL, `started_at` datetime NULL, `finished_at` datetime NULL, `settled_at` datetime NULL, parent_batch_id VARCHAR(64), session_id VARCHAR(255), base_unit_price REAL NOT NULL DEFAULT 0, group_rate_multiplier REAL NOT NULL DEFAULT 1.0, account_rate_multiplier REAL NOT NULL DEFAULT 1.0, batch_discount_multiplier REAL NOT NULL DEFAULT 0.5, hold_multiplier REAL NOT NULL DEFAULT 0.6, billable_unit_price REAL NOT NULL DEFAULT 0, hold_unit_price REAL NOT NULL DEFAULT 0, pricing_snapshot_version INTEGER NOT NULL DEFAULT 0);

CREATE TABLE channel_account_stats_model_pricing (
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
, image_input_price REAL, max_reasoning_effort_multiplier REAL DEFAULT 1);

CREATE TABLE channel_account_stats_pricing_intervals (
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

CREATE TABLE channel_account_stats_pricing_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL DEFAULT '',
    group_ids TEXT NOT NULL DEFAULT '[]',
    account_ids TEXT NOT NULL DEFAULT '[]',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE channel_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE channel_model_pricing (
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
, image_input_price REAL);

CREATE TABLE channel_monitor_aggregation_watermark (
    id INTEGER PRIMARY KEY,
    last_aggregated_at TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
, last_aggregated_date TEXT);

CREATE TABLE `channel_monitor_daily_rollups` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `model` text NOT NULL, `bucket_date` datetime NOT NULL, `total_checks` integer NOT NULL DEFAULT (0), `ok_count` integer NOT NULL DEFAULT (0), `operational_count` integer NOT NULL DEFAULT (0), `degraded_count` integer NOT NULL DEFAULT (0), `failed_count` integer NOT NULL DEFAULT (0), `error_count` integer NOT NULL DEFAULT (0), `sum_latency_ms` integer NOT NULL DEFAULT (0), `count_latency` integer NOT NULL DEFAULT (0), `sum_ping_latency_ms` integer NOT NULL DEFAULT (0), `count_ping_latency` integer NOT NULL DEFAULT (0), `computed_at` datetime NOT NULL, `monitor_id` integer NOT NULL, CONSTRAINT `channel_monitor_daily_rollups_channel_monitors_daily_rollups` FOREIGN KEY (`monitor_id`) REFERENCES `channel_monitors` (`id`) ON DELETE CASCADE);

CREATE TABLE `channel_monitor_histories` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `model` text NOT NULL, `status` text NOT NULL, `latency_ms` integer NULL, `ping_latency_ms` integer NULL, `message` text NULL DEFAULT (''), `quota` json NULL, `checked_at` datetime NOT NULL, `monitor_id` integer NOT NULL, CONSTRAINT `channel_monitor_histories_channel_monitors_history` FOREIGN KEY (`monitor_id`) REFERENCES `channel_monitors` (`id`) ON DELETE CASCADE);

CREATE TABLE `channel_monitor_request_templates` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `name` text NOT NULL, `provider` text NOT NULL, `api_mode` text NOT NULL DEFAULT ('chat_completions'), `description` text NULL DEFAULT (''), `extra_headers` json NOT NULL, `body_override_mode` text NOT NULL DEFAULT ('off'), `body_override` json NULL);

CREATE TABLE channel_monitor_v2_config (
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

CREATE TABLE channel_monitor_v2_error_metrics_1m (
    bucket_start TEXT NOT NULL,
    platform TEXT NOT NULL,
    group_id INTEGER NOT NULL DEFAULT 0,
    model TEXT NOT NULL,
    error_category TEXT NOT NULL,
    taxonomy_version SMALLINT NOT NULL,
    error_requests BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_start, platform, group_id, model, error_category, taxonomy_version)
);

CREATE TABLE channel_monitor_v2_error_metrics_rollup (
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

CREATE TABLE channel_monitor_v2_latency_histograms_1m (
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

CREATE TABLE channel_monitor_v2_latency_histograms_rollup (
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

CREATE TABLE channel_monitor_v2_metrics_1m (
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

CREATE TABLE channel_monitor_v2_metrics_rollup (
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

CREATE TABLE channel_monitor_v2_user_metrics_1m (
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

CREATE TABLE channel_monitor_v2_user_metrics_rollup (
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

CREATE TABLE channel_monitor_v2_watermarks (
    id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    usage_coverage_start TEXT,
    error_coverage_start TEXT,
    data_through TEXT,
    last_successful_at TEXT,
    backfill_cursor TEXT,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE `channel_monitors` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `name` text NOT NULL, `provider` text NOT NULL, `check_mode` text NOT NULL DEFAULT ('probe'), `account_id` integer NULL, `api_mode` text NOT NULL DEFAULT ('chat_completions'), `endpoint` text NOT NULL, `api_key_encrypted` text NOT NULL, `primary_model` text NOT NULL, `extra_models` json NOT NULL, `group_name` text NULL DEFAULT (''), `enabled` bool NOT NULL DEFAULT (true), `interval_seconds` integer NOT NULL, `jitter_seconds` integer NOT NULL DEFAULT (0), `last_checked_at` datetime NULL, `created_by` integer NOT NULL, `extra_headers` json NOT NULL, `body_override_mode` text NOT NULL DEFAULT ('off'), `body_override` json NULL, `template_id` integer NULL, CONSTRAINT `channel_monitors_channel_monito_a5daa16611505caf79f06364bb25a7f7` FOREIGN KEY (`template_id`) REFERENCES `channel_monitor_request_templates` (`id`) ON DELETE SET NULL);

CREATE TABLE channel_pricing_intervals (
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

CREATE TABLE channels (
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

CREATE TABLE `composite_model_routes` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `deleted_at` datetime NULL, `public_model` text NOT NULL, `match_type` text NOT NULL DEFAULT ('exact'), `target_platform` text NOT NULL DEFAULT ('openai'), `upstream_model` text NOT NULL DEFAULT (''), `endpoint` text NOT NULL DEFAULT ('any'), `priority` integer NOT NULL DEFAULT (100), `enabled` bool NOT NULL DEFAULT (true), `notes` text NULL, `group_id` integer NOT NULL, CONSTRAINT `composite_model_routes_groups_group` FOREIGN KEY (`group_id`) REFERENCES `groups` (`id`) ON DELETE NO ACTION);

CREATE TABLE content_moderation_logs (
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

CREATE TABLE deleted_api_key_audits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key VARCHAR(128) NOT NULL,
    api_key_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    key_name VARCHAR(100) NOT NULL DEFAULT '',
    deleted_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE `error_passthrough_rules` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `name` text NOT NULL, `enabled` bool NOT NULL DEFAULT (true), `priority` integer NOT NULL DEFAULT (0), `error_codes` json NULL, `keywords` json NULL, `match_mode` text NOT NULL DEFAULT ('any'), `platforms` json NULL, `passthrough_code` bool NOT NULL DEFAULT (true), `response_code` integer NULL, `passthrough_body` bool NOT NULL DEFAULT (true), `custom_message` text NULL, `skip_monitoring` bool NOT NULL DEFAULT (false), `description` text NULL);

CREATE TABLE `groups` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `deleted_at` datetime NULL, `name` text NOT NULL, `description` text NULL, `rate_multiplier` real NOT NULL DEFAULT (1), `peak_rate_enabled` bool NOT NULL DEFAULT (false), `peak_start` text NOT NULL DEFAULT (''), `peak_end` text NOT NULL DEFAULT (''), `peak_rate_multiplier` real NOT NULL DEFAULT (1), `is_exclusive` bool NOT NULL DEFAULT (false), `status` text NOT NULL DEFAULT ('active'), `duplicate_operation_id` text NULL, `platform` text NOT NULL DEFAULT ('anthropic'), `subscription_type` text NOT NULL DEFAULT ('standard'), `daily_limit_usd` real NULL, `weekly_limit_usd` real NULL, `monthly_limit_usd` real NULL, `default_validity_days` integer NOT NULL DEFAULT (30), `allow_image_generation` bool NOT NULL DEFAULT (false), `allow_batch_image_generation` bool NOT NULL DEFAULT (false), `image_rate_independent` bool NOT NULL DEFAULT (false), `image_rate_multiplier` real NOT NULL DEFAULT (1), `image_price_1k` real NULL, `image_price_2k` real NULL, `image_price_4k` real NULL, `batch_image_discount_multiplier` real NOT NULL DEFAULT (0.5), `batch_image_hold_multiplier` real NOT NULL DEFAULT (0.6), `video_rate_independent` bool NOT NULL DEFAULT (false), `video_rate_multiplier` real NOT NULL DEFAULT (1), `video_price_480p` real NULL, `video_price_720p` real NULL, `video_price_1080p` real NULL, `video_model_prices` json NULL, `web_search_price_per_call` real NULL, `search_price_per_1k` real NULL, `audio_realtime_price_per_min` real NULL, `audio_tts_price_per_million_chars` real NULL, `audio_stt_price_per_hour` real NULL, `long_context_pricing_enabled` bool NOT NULL DEFAULT (true), `model_pricing` json NULL, `claude_code_only` bool NOT NULL DEFAULT (false), `fallback_group_id` integer NULL, `fallback_group_id_on_invalid_request` integer NULL, `model_routing` json NULL, `model_routing_enabled` bool NOT NULL DEFAULT (false), `mcp_xml_inject` bool NOT NULL DEFAULT (true), `supported_model_scopes` json NOT NULL, `sort_order` integer NOT NULL DEFAULT (0), `allow_messages_dispatch` bool NOT NULL DEFAULT (false), `allow_live` bool NOT NULL DEFAULT (false), `force_openai_fast` bool NOT NULL DEFAULT (false), `free_openai_fast` bool NOT NULL DEFAULT (false), `require_oauth_only` bool NOT NULL DEFAULT (false), `require_privacy_set` bool NOT NULL DEFAULT (false), `default_mapped_model` text NOT NULL DEFAULT (''), `messages_dispatch_model_config` json NOT NULL, `model_allowlist` json NOT NULL, `codex_models_manifest_config` json NOT NULL, `rpm_limit` integer NOT NULL DEFAULT (0), `max_reasoning_effort` text NOT NULL DEFAULT (''), `max_reasoning_effort_over_limit` text NOT NULL DEFAULT ('downgrade'), `reasoning_effort_mappings` json NOT NULL, `profit_control_enabled` bool NOT NULL DEFAULT (false), `profit_min_margin` real NOT NULL DEFAULT (0), `profit_safety_buffer` real NOT NULL DEFAULT (0));

CREATE TABLE `idempotency_records` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `scope` text NOT NULL, `idempotency_key_hash` text NOT NULL, `request_fingerprint` text NOT NULL, `status` text NOT NULL, `response_status` integer NULL, `response_body` text NULL, `error_reason` text NULL, `locked_until` datetime NULL, `expires_at` datetime NOT NULL);

CREATE TABLE `identity_adoption_decisions` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `adopt_display_name` bool NOT NULL DEFAULT (false), `adopt_avatar` bool NOT NULL DEFAULT (false), `decided_at` datetime NOT NULL, `identity_id` integer NULL, `pending_auth_session_id` integer NOT NULL, CONSTRAINT `identity_adoption_decisions_auth_identities_adoption_decisions` FOREIGN KEY (`identity_id`) REFERENCES `auth_identities` (`id`) ON DELETE SET NULL, CONSTRAINT `identity_adoption_decisions_pen_207f864dd5b2369ca1c49b4476527b1b` FOREIGN KEY (`pending_auth_session_id`) REFERENCES `pending_auth_sessions` (`id`) ON DELETE CASCADE);

CREATE TABLE `kv_store` (`key` text NOT NULL, `value` blob NOT NULL, `expires_at` integer NULL, `updated_at` text NOT NULL, PRIMARY KEY (`key`));

CREATE TABLE kv_store_namespace (
    key        TEXT PRIMARY KEY,
    namespace  TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE memory_batch_queue (
    batch_id TEXT PRIMARY KEY,
    state TEXT NOT NULL CHECK (state IN ('ready', 'delayed', 'active')),
    available_at INTEGER NOT NULL,
    heartbeat_at INTEGER NOT NULL DEFAULT 0,
    receipt TEXT NOT NULL DEFAULT ''
);

CREATE TABLE ops_alert_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER,
    severity VARCHAR(16) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'firing',
    title VARCHAR(200),
    description TEXT,
    metric_value REAL,
    threshold_value REAL,
    dimensions TEXT,
    fired_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    resolved_at TEXT,
    email_sent BOOLEAN NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE ops_alert_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name VARCHAR(128) NOT NULL,
    description TEXT,
    enabled BOOLEAN NOT NULL DEFAULT 1,
    severity VARCHAR(16) NOT NULL DEFAULT 'warning',
    metric_type VARCHAR(64) NOT NULL,
    operator VARCHAR(8) NOT NULL,
    threshold REAL NOT NULL,
    window_minutes INTEGER NOT NULL DEFAULT 5,
    sustained_minutes INTEGER NOT NULL DEFAULT 5,
    cooldown_minutes INTEGER NOT NULL DEFAULT 10,
    filters TEXT,
    last_triggered_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
, notify_email BOOLEAN NOT NULL DEFAULT 1);

CREATE TABLE ops_alert_silences (
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

CREATE TABLE ops_error_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id VARCHAR(64),
    client_request_id VARCHAR(64),
    user_id INTEGER,
    api_key_id INTEGER,
    account_id INTEGER,
    group_id INTEGER,
    client_ip VARCHAR(64),
    platform VARCHAR(32),
    model VARCHAR(100),
    request_path VARCHAR(256),
    stream BOOLEAN NOT NULL DEFAULT 0,
    user_agent TEXT,
    error_phase VARCHAR(32) NOT NULL,
    error_type VARCHAR(64) NOT NULL,
    severity VARCHAR(8) NOT NULL DEFAULT 'P2',
    status_code INTEGER,
    is_business_limited BOOLEAN NOT NULL DEFAULT 0,
    is_count_tokens BOOLEAN NOT NULL DEFAULT 0,
    error_message TEXT,
    error_body TEXT,
    error_source VARCHAR(64),
    error_owner VARCHAR(32),
    account_status VARCHAR(50),
    upstream_status_code INTEGER,
    upstream_error_message TEXT,
    upstream_error_detail TEXT,
    provider_error_code VARCHAR(64),
    provider_error_type VARCHAR(64),
    network_error_type VARCHAR(50),
    retry_after_seconds INTEGER,
    duration_ms INTEGER,
    time_to_first_token_ms INTEGER,
    auth_latency_ms INTEGER,
    routing_latency_ms INTEGER,
    upstream_latency_ms INTEGER,
    response_latency_ms INTEGER,
    request_body TEXT,
    request_headers TEXT,
    request_body_truncated BOOLEAN NOT NULL DEFAULT 0,
    request_body_bytes INTEGER,
    is_retryable BOOLEAN NOT NULL DEFAULT 0,
    retry_count INTEGER NOT NULL DEFAULT 0,
    attempted_key_prefix VARCHAR(32),
    deleted_key_owner_user_id INTEGER,
    deleted_key_name VARCHAR(100),
    is_resolved BOOLEAN NOT NULL DEFAULT 0,
    resolved_at TEXT,
    resolved_by INTEGER,
    resolution_note TEXT,
    upstream_errors TEXT,
    upstream_model VARCHAR(255),
    upstream_request_id VARCHAR(128),
    inbound_endpoint VARCHAR(128),
    upstream_endpoint VARCHAR(128),
    requested_model VARCHAR(255),
    request_type INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
, resolved BOOLEAN NOT NULL DEFAULT 0, resolved_by_user_id INTEGER, api_key_prefix VARCHAR(32));

CREATE TABLE ops_ingress_reject_aggregates (
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

CREATE TABLE ops_job_heartbeats (
    job_name VARCHAR(64) PRIMARY KEY,
    last_run_at TEXT,
    last_success_at TEXT,
    last_error_at TEXT,
    last_error TEXT,
    last_duration_ms INTEGER,
    last_result TEXT,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE ops_metrics_daily (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    bucket_date TEXT NOT NULL,
    platform VARCHAR(32),
    group_id INTEGER,
    success_count INTEGER NOT NULL DEFAULT 0,
    ttft_sample_count INTEGER NOT NULL DEFAULT 0,
    error_count_total INTEGER NOT NULL DEFAULT 0,
    business_limited_count INTEGER NOT NULL DEFAULT 0,
    error_count_sla INTEGER NOT NULL DEFAULT 0,
    upstream_error_count_excl_429_529 INTEGER NOT NULL DEFAULT 0,
    upstream_429_count INTEGER NOT NULL DEFAULT 0,
    upstream_529_count INTEGER NOT NULL DEFAULT 0,
    token_consumed INTEGER NOT NULL DEFAULT 0,
    duration_p50_ms INTEGER,
    duration_p90_ms INTEGER,
    duration_p95_ms INTEGER,
    duration_p99_ms INTEGER,
    duration_avg_ms REAL,
    duration_max_ms INTEGER,
    ttft_p50_ms INTEGER,
    ttft_p90_ms INTEGER,
    ttft_p95_ms INTEGER,
    ttft_p99_ms INTEGER,
    ttft_avg_ms REAL,
    ttft_max_ms INTEGER,
    computed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE ops_metrics_hourly (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    bucket_start TEXT NOT NULL,
    platform VARCHAR(32),
    group_id INTEGER,
    success_count INTEGER NOT NULL DEFAULT 0,
    ttft_sample_count INTEGER NOT NULL DEFAULT 0,
    error_count_total INTEGER NOT NULL DEFAULT 0,
    business_limited_count INTEGER NOT NULL DEFAULT 0,
    error_count_sla INTEGER NOT NULL DEFAULT 0,
    upstream_error_count_excl_429_529 INTEGER NOT NULL DEFAULT 0,
    upstream_429_count INTEGER NOT NULL DEFAULT 0,
    upstream_529_count INTEGER NOT NULL DEFAULT 0,
    token_consumed INTEGER NOT NULL DEFAULT 0,
    duration_p50_ms INTEGER,
    duration_p90_ms INTEGER,
    duration_p95_ms INTEGER,
    duration_p99_ms INTEGER,
    duration_avg_ms REAL,
    duration_max_ms INTEGER,
    ttft_p50_ms INTEGER,
    ttft_p90_ms INTEGER,
    ttft_p95_ms INTEGER,
    ttft_p99_ms INTEGER,
    ttft_avg_ms REAL,
    ttft_max_ms INTEGER,
    computed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE ops_system_log_cleanup_audits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    operator_id INTEGER NOT NULL,
    conditions TEXT NOT NULL DEFAULT '{}',
    deleted_rows INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE ops_system_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    host VARCHAR(255),
    level VARCHAR(16) NOT NULL,
    component VARCHAR(128) NOT NULL DEFAULT '',
    message TEXT NOT NULL,
    request_id VARCHAR(128),
    client_request_id VARCHAR(128),
    user_id INTEGER,
    api_key_id INTEGER,
    account_id INTEGER,
    platform VARCHAR(32),
    model VARCHAR(128),
    extra TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE ops_system_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    window_minutes INTEGER NOT NULL DEFAULT 1,
    platform VARCHAR(32),
    group_id INTEGER,
    success_count INTEGER NOT NULL DEFAULT 0,
    error_count_total INTEGER NOT NULL DEFAULT 0,
    business_limited_count INTEGER NOT NULL DEFAULT 0,
    error_count_sla INTEGER NOT NULL DEFAULT 0,
    upstream_error_count_excl_429_529 INTEGER NOT NULL DEFAULT 0,
    upstream_429_count INTEGER NOT NULL DEFAULT 0,
    upstream_529_count INTEGER NOT NULL DEFAULT 0,
    token_consumed INTEGER NOT NULL DEFAULT 0,
    account_switch_count INTEGER NOT NULL DEFAULT 0,
    qps REAL,
    tps REAL,
    duration_p50_ms INTEGER,
    duration_p90_ms INTEGER,
    duration_p95_ms INTEGER,
    duration_p99_ms INTEGER,
    duration_avg_ms REAL,
    duration_max_ms INTEGER,
    ttft_p50_ms INTEGER,
    ttft_p90_ms INTEGER,
    ttft_p95_ms INTEGER,
    ttft_p99_ms INTEGER,
    ttft_avg_ms REAL,
    ttft_max_ms INTEGER,
    cpu_usage_percent REAL,
    memory_used_mb INTEGER,
    memory_total_mb INTEGER,
    memory_usage_percent REAL,
    db_ok BOOLEAN,
    redis_ok BOOLEAN,
    db_conn_active INTEGER,
    db_conn_idle INTEGER,
    db_conn_waiting INTEGER,
    goroutine_count INTEGER,
    concurrency_queue_depth INTEGER
, redis_conn_total INTEGER, redis_conn_idle INTEGER);

CREATE TABLE passkey_credentials (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id BLOB NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL DEFAULT 'Passkey',
    credential_data TEXT NOT NULL,
    last_used_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE passkey_user_handles (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    user_handle BLOB NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE `payment_audit_logs` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `order_id` text NOT NULL, `action` text NOT NULL, `detail` text NOT NULL DEFAULT (''), `operator` text NOT NULL DEFAULT ('system'), `created_at` datetime NOT NULL);

CREATE TABLE `payment_orders` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `user_email` text NOT NULL, `user_name` text NOT NULL, `user_notes` text NULL, `amount` real NOT NULL, `pay_amount` real NOT NULL, `fee_rate` real NOT NULL DEFAULT (0), `bonus_amount` real NOT NULL DEFAULT (0), `recharge_code` text NOT NULL, `out_trade_no` text NOT NULL DEFAULT (''), `payment_type` text NOT NULL, `payment_trade_no` text NOT NULL, `pay_url` text NULL, `qr_code` text NULL, `qr_code_img` text NULL, `order_type` text NOT NULL DEFAULT ('balance'), `plan_id` integer NULL, `subscription_group_id` integer NULL, `subscription_days` integer NULL, `provider_instance_id` text NULL, `provider_key` text NULL, `provider_snapshot` json NULL, `status` text NOT NULL DEFAULT ('PENDING'), `refund_amount` real NOT NULL DEFAULT (0), `refund_reason` text NULL, `refund_at` datetime NULL, `force_refund` bool NOT NULL DEFAULT (false), `refund_requested_at` datetime NULL, `refund_request_reason` text NULL, `refund_requested_by` text NULL, `expires_at` datetime NOT NULL, `paid_at` datetime NULL, `completed_at` datetime NULL, `failed_at` datetime NULL, `failed_reason` text NULL, `client_ip` text NOT NULL, `src_host` text NOT NULL, `src_url` text NULL, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `user_id` integer NOT NULL, CONSTRAINT `payment_orders_users_payment_orders` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE NO ACTION);

CREATE TABLE `payment_provider_instances` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `provider_key` text NOT NULL, `name` text NOT NULL DEFAULT (''), `config` text NOT NULL, `supported_types` text NOT NULL DEFAULT (''), `enabled` bool NOT NULL DEFAULT (true), `payment_mode` text NOT NULL DEFAULT (''), `sort_order` integer NOT NULL DEFAULT (0), `limits` text NOT NULL DEFAULT (''), `refund_enabled` bool NOT NULL DEFAULT (false), `allow_user_refund` bool NOT NULL DEFAULT (false), `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL);

CREATE TABLE `pending_auth_sessions` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `session_token` text NOT NULL, `intent` text NOT NULL, `provider_type` text NOT NULL, `provider_key` text NOT NULL, `provider_subject` text NOT NULL, `redirect_to` text NOT NULL DEFAULT (''), `resolved_email` text NOT NULL DEFAULT (''), `registration_password_hash` text NOT NULL DEFAULT (''), `upstream_identity_claims` json NOT NULL, `local_flow_state` json NOT NULL, `browser_session_key` text NOT NULL DEFAULT (''), `completion_code_hash` text NOT NULL DEFAULT (''), `completion_code_expires_at` datetime NULL, `email_verified_at` datetime NULL, `password_verified_at` datetime NULL, `totp_verified_at` datetime NULL, `expires_at` datetime NOT NULL, `consumed_at` datetime NULL, `target_user_id` integer NULL, CONSTRAINT `pending_auth_sessions_users_pending_auth_sessions` FOREIGN KEY (`target_user_id`) REFERENCES `users` (`id`) ON DELETE SET NULL);

CREATE TABLE `promo_code_usages` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `bonus_amount` real NOT NULL, `used_at` datetime NOT NULL, `promo_code_id` integer NOT NULL, `user_id` integer NOT NULL, CONSTRAINT `promo_code_usages_promo_codes_usage_records` FOREIGN KEY (`promo_code_id`) REFERENCES `promo_codes` (`id`) ON DELETE NO ACTION, CONSTRAINT `promo_code_usages_users_promo_code_usages` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE NO ACTION);

CREATE TABLE `promo_codes` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `code` text NOT NULL, `bonus_amount` real NOT NULL DEFAULT (0), `max_uses` integer NOT NULL DEFAULT (0), `used_count` integer NOT NULL DEFAULT (0), `status` text NOT NULL DEFAULT ('active'), `expires_at` datetime NULL, `notes` text NULL, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL);

CREATE TABLE prompt_audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id INTEGER NOT NULL REFERENCES prompt_audit_jobs(id) ON DELETE CASCADE,
    request_id VARCHAR(128) NOT NULL DEFAULT '',
    user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    username_snapshot VARCHAR(255) NOT NULL DEFAULT '',
    user_email_snapshot VARCHAR(320) NOT NULL DEFAULT '',
    api_key_id INTEGER REFERENCES api_keys(id) ON DELETE SET NULL,
    api_key_name_snapshot VARCHAR(255) NOT NULL DEFAULT '',
    group_id INTEGER REFERENCES groups(id) ON DELETE SET NULL,
    group_name VARCHAR(255) NOT NULL DEFAULT '',
    provider VARCHAR(64) NOT NULL DEFAULT '',
    endpoint VARCHAR(128) NOT NULL DEFAULT '',
    protocol VARCHAR(64) NOT NULL DEFAULT '',
    model VARCHAR(255) NOT NULL DEFAULT '',
    prompt_hash VARCHAR(64) NOT NULL DEFAULT '',
    redacted_preview TEXT NOT NULL DEFAULT '',
    full_prompt TEXT NOT NULL DEFAULT '',
    stage VARCHAR(32) NOT NULL DEFAULT 'http',
    decision VARCHAR(32) NOT NULL DEFAULT 'pass',
    risk_level VARCHAR(32) NOT NULL DEFAULT 'low',
    action VARCHAR(32) NOT NULL DEFAULT 'Allow',
    categories TEXT NOT NULL DEFAULT '[]',
    matched_scanners TEXT NOT NULL DEFAULT '[]',
    scanner_scores TEXT NOT NULL DEFAULT '{}',
    scanner_evidence TEXT NOT NULL DEFAULT '{}',
    scanner_backend VARCHAR(64) NOT NULL DEFAULT 'qwen3guard-openai',
    scanner_version VARCHAR(128) NOT NULL DEFAULT '',
    guard_endpoint_id VARCHAR(128) NOT NULL DEFAULT '',
    policy_id VARCHAR(128) NOT NULL DEFAULT '',
    policy_version INTEGER NOT NULL DEFAULT 0,
    config_version INTEGER NOT NULL DEFAULT 1,
    chunk_total INTEGER NOT NULL DEFAULT 0,
    latency_ms INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CONSTRAINT chk_prompt_audit_events_decision
        CHECK (decision IN ('pass', 'flag', 'critical')),
    CONSTRAINT chk_prompt_audit_events_risk_level
        CHECK (risk_level IN ('low', 'medium', 'high', 'critical')),
    CONSTRAINT chk_prompt_audit_events_action
        CHECK (action IN ('Allow', 'Warn', 'Block')),
    CONSTRAINT chk_prompt_audit_events_nonnegative
        CHECK (policy_version >= 0 AND config_version >= 1 AND chunk_total >= 0 AND latency_ms >= 0)
);

CREATE TABLE prompt_audit_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id VARCHAR(128) NOT NULL DEFAULT '',
    user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    username_snapshot VARCHAR(255) NOT NULL DEFAULT '',
    user_email_snapshot VARCHAR(320) NOT NULL DEFAULT '',
    api_key_id INTEGER REFERENCES api_keys(id) ON DELETE SET NULL,
    api_key_name_snapshot VARCHAR(255) NOT NULL DEFAULT '',
    group_id INTEGER REFERENCES groups(id) ON DELETE SET NULL,
    group_name VARCHAR(255) NOT NULL DEFAULT '',
    provider VARCHAR(64) NOT NULL DEFAULT '',
    endpoint VARCHAR(128) NOT NULL DEFAULT '',
    protocol VARCHAR(64) NOT NULL DEFAULT '',
    model VARCHAR(255) NOT NULL DEFAULT '',
    prompt_hash VARCHAR(64) NOT NULL DEFAULT '',
    redacted_preview TEXT NOT NULL DEFAULT '',
    prompt_length INTEGER NOT NULL DEFAULT 0,
    message_count INTEGER NOT NULL DEFAULT 0,
    stage VARCHAR(32) NOT NULL DEFAULT 'http',
    execution_mode VARCHAR(32) NOT NULL DEFAULT 'async_audit',
    config_version INTEGER NOT NULL DEFAULT 1,
    status VARCHAR(32) NOT NULL DEFAULT 'staging',
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    claim_version INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    processing_started_at TEXT,
    processed_at TEXT,
    last_error_code VARCHAR(64) NOT NULL DEFAULT '',
    last_error_message VARCHAR(512) NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CONSTRAINT chk_prompt_audit_jobs_status
        CHECK (status IN ('staging', 'queued', 'processing', 'retry', 'done', 'failed')),
    CONSTRAINT chk_prompt_audit_jobs_execution_mode
        CHECK (execution_mode IN ('async_audit', 'blocking')),
    CONSTRAINT chk_prompt_audit_jobs_nonnegative
        CHECK (attempts >= 0 AND max_attempts >= 0 AND claim_version >= 0 AND
               prompt_length >= 0 AND message_count >= 0 AND config_version >= 1)
);

CREATE TABLE `proxies` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `deleted_at` datetime NULL, `name` text NOT NULL, `protocol` text NOT NULL, `host` text NOT NULL, `port` integer NOT NULL, `username` text NULL, `password` text NULL, `status` text NOT NULL DEFAULT ('active'), `expires_at` datetime NULL, `fallback_mode` text NOT NULL DEFAULT ('none'), `expiry_warn_days` integer NOT NULL DEFAULT (7), `backup_proxy_id` integer NULL, CONSTRAINT `proxies_proxies_backup_proxy` FOREIGN KEY (`backup_proxy_id`) REFERENCES `proxies` (`id`) ON DELETE SET NULL);

CREATE TABLE `redeem_codes` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `code` text NOT NULL, `type` text NOT NULL DEFAULT ('balance'), `value` real NOT NULL DEFAULT (0), `status` text NOT NULL DEFAULT ('unused'), `used_at` datetime NULL, `notes` text NULL, `created_at` datetime NOT NULL, `expires_at` datetime NULL, `validity_days` integer NOT NULL DEFAULT (30), `group_id` integer NULL, `used_by` integer NULL, CONSTRAINT `redeem_codes_groups_redeem_codes` FOREIGN KEY (`group_id`) REFERENCES `groups` (`id`) ON DELETE SET NULL, CONSTRAINT `redeem_codes_users_redeem_codes` FOREIGN KEY (`used_by`) REFERENCES `users` (`id`) ON DELETE SET NULL);

CREATE TABLE scheduled_test_plans (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    model_id VARCHAR(100) NOT NULL DEFAULT '',
    cron_expression VARCHAR(100) NOT NULL DEFAULT '*/30 * * * *',
    enabled BOOLEAN NOT NULL DEFAULT 1,
    max_results INTEGER NOT NULL DEFAULT 50,
    auto_recover BOOLEAN NOT NULL DEFAULT 0,
    last_run_at TEXT,
    next_run_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE scheduled_test_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_id INTEGER NOT NULL REFERENCES scheduled_test_plans(id) ON DELETE CASCADE,
    status VARCHAR(20) NOT NULL DEFAULT 'success',
    response_text TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    started_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    finished_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE scheduler_outbox (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL,
    account_id INTEGER,
    group_id INTEGER,
    payload TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    dedup_key TEXT
);

CREATE TABLE `security_secrets` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `key` text NOT NULL, `value` text NOT NULL);

CREATE TABLE `settings` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `key` text NOT NULL, `value` text NOT NULL, `updated_at` datetime NOT NULL);

CREATE TABLE sub2api_plugin_bindings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    plugin_id INTEGER NOT NULL REFERENCES sub2api_plugin_installations(id) ON DELETE CASCADE,
    capability VARCHAR(160) NOT NULL,
    platform VARCHAR(32) NOT NULL,
    account_type VARCHAR(32) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT 0,
    rollout_percent INTEGER NOT NULL DEFAULT 100,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CONSTRAINT sub2api_plugin_bindings_rollout_check
        CHECK (rollout_percent BETWEEN 0 AND 100),
    CONSTRAINT sub2api_plugin_bindings_scope_unique
        UNIQUE (plugin_id, capability, platform, account_type)
);

CREATE TABLE sub2api_plugin_installations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    plugin_key VARCHAR(160) NOT NULL UNIQUE,
    name VARCHAR(160) NOT NULL,
    version VARCHAR(64) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    author VARCHAR(160) NOT NULL DEFAULT '',
    manifest TEXT NOT NULL DEFAULT '{}',
    artifact_data BLOB,
    artifact_path TEXT NOT NULL,
    install_path TEXT NOT NULL,
    binary_path TEXT NOT NULL,
    binary_sha256 VARCHAR(64) NOT NULL,
    signature_status VARCHAR(32) NOT NULL DEFAULT 'unsigned',
    state VARCHAR(32) NOT NULL DEFAULT 'disabled',
    config_encrypted TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    installed_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    installed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    enabled_at TEXT,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CONSTRAINT sub2api_plugin_installations_state_check
        CHECK (state IN ('disabled', 'starting', 'enabled', 'error', 'incompatible')),
    CONSTRAINT sub2api_plugin_installations_signature_status_check
        CHECK (signature_status IN ('trusted', 'unsigned'))
);

CREATE TABLE `subscription_plans` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `group_id` integer NOT NULL, `name` text NOT NULL, `description` text NOT NULL DEFAULT (''), `price` real NOT NULL, `original_price` real NULL, `currency` text NOT NULL DEFAULT (''), `validity_days` integer NOT NULL DEFAULT (30), `validity_unit` text NOT NULL DEFAULT ('day'), `features` text NOT NULL DEFAULT (''), `product_name` text NOT NULL DEFAULT (''), `for_sale` bool NOT NULL DEFAULT (true), `sort_order` integer NOT NULL DEFAULT (0), `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL);

CREATE TABLE `tls_fingerprint_profiles` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `name` text NOT NULL, `description` text NULL, `enable_grease` bool NOT NULL DEFAULT (false), `cipher_suites` json NULL, `curves` json NULL, `point_formats` json NULL, `signature_algorithms` json NULL, `alpn_protocols` json NULL, `supported_versions` json NULL, `key_share_groups` json NULL, `psk_modes` json NULL, `extensions` json NULL);

CREATE TABLE usage_billing_dedup (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id TEXT NOT NULL,
    api_key_id INTEGER NOT NULL,
    request_fingerprint TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE usage_billing_dedup_archive (
    request_id TEXT NOT NULL,
    api_key_id INTEGER NOT NULL,
    request_fingerprint TEXT NOT NULL,
    created_at TEXT NOT NULL,
    archived_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (request_id, api_key_id)
);

CREATE TABLE `usage_cleanup_tasks` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `status` text NOT NULL, `filters` json NOT NULL, `created_by` integer NOT NULL, `deleted_rows` integer NOT NULL DEFAULT (0), `error_message` text NULL, `canceled_by` integer NULL, `canceled_at` datetime NULL, `started_at` datetime NULL, `finished_at` datetime NULL);

CREATE TABLE usage_dashboard_aggregation_watermark (
    id INTEGER PRIMARY KEY,
    last_aggregated_at TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE usage_dashboard_daily (
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

CREATE TABLE usage_dashboard_daily_users (
    bucket_date TEXT NOT NULL,
    user_id INTEGER NOT NULL,
    PRIMARY KEY (bucket_date, user_id)
);

CREATE TABLE usage_dashboard_hourly (
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

CREATE TABLE usage_dashboard_hourly_users (
    bucket_start TEXT NOT NULL,
    user_id INTEGER NOT NULL,
    PRIMARY KEY (bucket_start, user_id)
);

CREATE TABLE usage_group_daily_rollups (
    bucket_date TEXT NOT NULL,
    group_id INTEGER NOT NULL,
    actual_cost REAL NOT NULL DEFAULT 0,
    computed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (bucket_date, group_id)
);

CREATE TABLE usage_group_rollup_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    closed_before TEXT NOT NULL DEFAULT '1970-01-01',
    retained_from TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    timezone_name TEXT NOT NULL DEFAULT 'Asia/Shanghai',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE `usage_logs` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `request_id` text NOT NULL, `model` text NOT NULL, `requested_model` text NULL, `upstream_model` text NULL, `upstream_response_model` text NULL, `upstream_model_mismatch` bool NULL, `channel_id` integer NULL, `model_mapping_chain` text NULL, `billing_tier` text NULL, `billing_mode` text NULL, `input_tokens` integer NOT NULL DEFAULT (0), `output_tokens` integer NOT NULL DEFAULT (0), `cache_creation_tokens` integer NOT NULL DEFAULT (0), `cache_read_tokens` integer NOT NULL DEFAULT (0), `cache_creation_5m_tokens` integer NOT NULL DEFAULT (0), `cache_creation_1h_tokens` integer NOT NULL DEFAULT (0), `input_cost` real NOT NULL DEFAULT (0), `output_cost` real NOT NULL DEFAULT (0), `cache_creation_cost` real NOT NULL DEFAULT (0), `cache_read_cost` real NOT NULL DEFAULT (0), `total_cost` real NOT NULL DEFAULT (0), `actual_cost` real NOT NULL DEFAULT (0), `rate_multiplier` real NOT NULL DEFAULT (1), `long_context_billing_applied` bool NOT NULL DEFAULT (false), `account_rate_multiplier` real NULL, `billing_type` integer NOT NULL DEFAULT (0), `stream` bool NOT NULL DEFAULT (false), `duration_ms` integer NULL, `first_token_ms` integer NULL, `user_agent` text NULL, `ip_address` text NULL, `image_count` integer NOT NULL DEFAULT (0), `image_size` text NULL, `image_input_size` text NULL, `image_output_size` text NULL, `image_size_source` text NULL, `image_size_breakdown` json NULL, `video_count` integer NOT NULL DEFAULT (0), `video_resolution` text NULL, `video_duration_seconds` integer NULL, `cache_ttl_overridden` bool NOT NULL DEFAULT (false), `created_at` datetime NOT NULL, `api_key_id` integer NOT NULL, `account_id` integer NOT NULL, `group_id` integer NULL, `user_id` integer NOT NULL, `subscription_id` integer NULL, image_output_tokens INTEGER NOT NULL DEFAULT 0, image_output_cost REAL NOT NULL DEFAULT 0, account_stats_cost REAL, image_input_tokens INTEGER NOT NULL DEFAULT 0, image_input_cost REAL NOT NULL DEFAULT 0, session_id VARCHAR(255), requested_reasoning_effort VARCHAR(20), reasoning_effort VARCHAR(20), openai_ws_mode BOOLEAN NOT NULL DEFAULT 0, request_type SMALLINT NOT NULL DEFAULT 0, service_tier VARCHAR(16), inbound_endpoint VARCHAR(128), upstream_endpoint VARCHAR(128), native_compaction_v2 BOOLEAN NOT NULL DEFAULT 0, upstream_request_id VARCHAR(128), CONSTRAINT `usage_logs_api_keys_usage_logs` FOREIGN KEY (`api_key_id`) REFERENCES `api_keys` (`id`) ON DELETE NO ACTION, CONSTRAINT `usage_logs_accounts_usage_logs` FOREIGN KEY (`account_id`) REFERENCES `accounts` (`id`) ON DELETE NO ACTION, CONSTRAINT `usage_logs_groups_usage_logs` FOREIGN KEY (`group_id`) REFERENCES `groups` (`id`) ON DELETE SET NULL, CONSTRAINT `usage_logs_users_usage_logs` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE NO ACTION, CONSTRAINT `usage_logs_user_subscriptions_usage_logs` FOREIGN KEY (`subscription_id`) REFERENCES `user_subscriptions` (`id`) ON DELETE SET NULL);

CREATE TABLE user_affiliate_ledger (
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

CREATE TABLE user_affiliates (
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

CREATE TABLE `user_allowed_groups` (`created_at` datetime NOT NULL, `user_id` integer NOT NULL, `group_id` integer NOT NULL, PRIMARY KEY (`user_id`, `group_id`), CONSTRAINT `user_allowed_groups_users_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE NO ACTION, CONSTRAINT `user_allowed_groups_groups_group` FOREIGN KEY (`group_id`) REFERENCES `groups` (`id`) ON DELETE NO ACTION);

CREATE TABLE `user_attribute_definitions` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `deleted_at` datetime NULL, `key` text NOT NULL, `name` text NOT NULL, `description` text NOT NULL DEFAULT (''), `type` text NOT NULL, `options` json NOT NULL, `required` bool NOT NULL DEFAULT (false), `validation` json NOT NULL, `placeholder` text NOT NULL DEFAULT (''), `display_order` integer NOT NULL DEFAULT (0), `enabled` bool NOT NULL DEFAULT (true));

CREATE TABLE `user_attribute_values` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `value` text NOT NULL DEFAULT (''), `user_id` integer NOT NULL, `attribute_id` integer NOT NULL, CONSTRAINT `user_attribute_values_users_attribute_values` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE NO ACTION, CONSTRAINT `user_attribute_values_user_attribute_definitions_values` FOREIGN KEY (`attribute_id`) REFERENCES `user_attribute_definitions` (`id`) ON DELETE NO ACTION);

CREATE TABLE user_avatars (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    storage_provider VARCHAR(32) NOT NULL DEFAULT '',
    storage_key VARCHAR(255) NOT NULL DEFAULT '',
    url TEXT NOT NULL DEFAULT '',
    content_type VARCHAR(128) NOT NULL DEFAULT '',
    byte_size INTEGER NOT NULL DEFAULT 0,
    sha256 VARCHAR(64) NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE user_group_rate_multipliers (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    rate_multiplier REAL,
    rpm_override INTEGER,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (user_id, group_id)
);

CREATE TABLE `user_platform_quotas` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `deleted_at` datetime NULL, `platform` text NOT NULL, `daily_limit_usd` real NULL, `weekly_limit_usd` real NULL, `monthly_limit_usd` real NULL, `daily_usage_usd` real NOT NULL DEFAULT (0), `weekly_usage_usd` real NOT NULL DEFAULT (0), `monthly_usage_usd` real NOT NULL DEFAULT (0), `daily_window_start` datetime NULL, `weekly_window_start` datetime NULL, `monthly_window_start` datetime NULL, `user_id` integer NOT NULL, CONSTRAINT `user_platform_quotas_users_platform_quotas` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE NO ACTION);

CREATE TABLE user_provider_default_grants (
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

CREATE TABLE `user_subscriptions` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `deleted_at` datetime NULL, `starts_at` datetime NOT NULL, `expires_at` datetime NOT NULL, `status` text NOT NULL DEFAULT ('active'), `daily_window_start` datetime NULL, `weekly_window_start` datetime NULL, `monthly_window_start` datetime NULL, `daily_usage_usd` real NOT NULL DEFAULT (0), `weekly_usage_usd` real NOT NULL DEFAULT (0), `monthly_usage_usd` real NOT NULL DEFAULT (0), `assigned_at` datetime NOT NULL, `notes` text NULL, `group_id` integer NOT NULL, `user_id` integer NOT NULL, `assigned_by` integer NULL, CONSTRAINT `user_subscriptions_groups_subscriptions` FOREIGN KEY (`group_id`) REFERENCES `groups` (`id`) ON DELETE NO ACTION, CONSTRAINT `user_subscriptions_users_subscriptions` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE NO ACTION, CONSTRAINT `user_subscriptions_users_assigned_subscriptions` FOREIGN KEY (`assigned_by`) REFERENCES `users` (`id`) ON DELETE SET NULL);

CREATE TABLE `users` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `deleted_at` datetime NULL, `email` text NOT NULL, `password_hash` text NOT NULL, `role` text NOT NULL DEFAULT ('user'), `balance` real NOT NULL DEFAULT (0), `frozen_balance` real NOT NULL DEFAULT (0), `concurrency` integer NOT NULL DEFAULT (5), `status` text NOT NULL DEFAULT ('active'), `username` text NOT NULL DEFAULT (''), `notes` text NOT NULL DEFAULT (''), `totp_secret_encrypted` text NULL, `totp_enabled` bool NOT NULL DEFAULT (false), `totp_enabled_at` datetime NULL, `signup_source` text NOT NULL DEFAULT ('email'), `last_login_at` datetime NULL, `last_active_at` datetime NULL, `restrict_public_groups` bool NOT NULL DEFAULT (false), `balance_notify_enabled` bool NOT NULL DEFAULT (true), `balance_notify_threshold_type` text NOT NULL DEFAULT ('fixed'), `balance_notify_threshold` real NULL, `balance_notify_extra_emails` text NOT NULL DEFAULT ('[]'), `total_recharged` real NOT NULL DEFAULT (0), `rpm_limit` integer NOT NULL DEFAULT (0));

CREATE INDEX `account_deleted_at` ON `accounts` (`deleted_at`);

CREATE INDEX `account_last_used_at` ON `accounts` (`last_used_at`);

CREATE INDEX `account_overload_until` ON `accounts` (`overload_until`);

CREATE INDEX `account_parent_account_id` ON `accounts` (`parent_account_id`);

CREATE INDEX `account_platform` ON `accounts` (`platform`);

CREATE INDEX `account_platform_priority` ON `accounts` (`platform`, `priority`);

CREATE INDEX `account_priority` ON `accounts` (`priority`);

CREATE INDEX `account_priority_status` ON `accounts` (`priority`, `status`);

CREATE INDEX `account_proxy_id` ON `accounts` (`proxy_id`);

CREATE INDEX `account_rate_limit_reset_at` ON `accounts` (`rate_limit_reset_at`);

CREATE INDEX `account_rate_limited_at` ON `accounts` (`rate_limited_at`);

CREATE INDEX `account_schedulable` ON `accounts` (`schedulable`);

CREATE INDEX `account_status` ON `accounts` (`status`);

CREATE INDEX `account_type` ON `accounts` (`type`);

CREATE INDEX `accountgroup_group_id` ON `account_groups` (`group_id`);

CREATE INDEX `accountgroup_priority` ON `account_groups` (`priority`);

CREATE INDEX `announcement_created_at` ON `announcements` (`created_at`);

CREATE INDEX `announcement_ends_at` ON `announcements` (`ends_at`);

CREATE INDEX `announcement_starts_at` ON `announcements` (`starts_at`);

CREATE INDEX `announcement_status` ON `announcements` (`status`);

CREATE INDEX `announcementread_announcement_id` ON `announcement_reads` (`announcement_id`);

CREATE UNIQUE INDEX `announcementread_announcement_id_user_id` ON `announcement_reads` (`announcement_id`, `user_id`);

CREATE INDEX `announcementread_read_at` ON `announcement_reads` (`read_at`);

CREATE INDEX `announcementread_user_id` ON `announcement_reads` (`user_id`);

CREATE UNIQUE INDEX `api_keys_key_key` ON `api_keys` (`key`);

CREATE INDEX `apikey_deleted_at` ON `api_keys` (`deleted_at`);

CREATE INDEX `apikey_expires_at` ON `api_keys` (`expires_at`);

CREATE INDEX `apikey_group_id` ON `api_keys` (`group_id`);

CREATE INDEX `apikey_last_used_at` ON `api_keys` (`last_used_at`);

CREATE INDEX `apikey_quota_quota_used` ON `api_keys` (`quota`, `quota_used`);

CREATE INDEX `apikey_status` ON `api_keys` (`status`);

CREATE INDEX `apikey_user_id` ON `api_keys` (`user_id`);

CREATE UNIQUE INDEX `authidentity_provider_type_provider_key_provider_subject` ON `auth_identities` (`provider_type`, `provider_key`, `provider_subject`);

CREATE INDEX `authidentity_user_id` ON `auth_identities` (`user_id`);

CREATE INDEX `authidentity_user_id_provider_type` ON `auth_identities` (`user_id`, `provider_type`);

CREATE INDEX `authidentitychannel_identity_id` ON `auth_identity_channels` (`identity_id`);

CREATE UNIQUE INDEX `authidentitychannel_provider_ty_9e5e83a6073c8096ae3b1e15cac03ec6` ON `auth_identity_channels` (`provider_type`, `provider_key`, `channel`, `channel_app_id`, `channel_subject`);

CREATE INDEX batch_image_jobs_parent_batch_id_idx
    ON batch_image_jobs(parent_batch_id) WHERE parent_batch_id IS NOT NULL;

CREATE INDEX batch_image_jobs_task_name_idx ON batch_image_jobs(task_name);

CREATE INDEX batchimageevent_event_type ON batch_image_events(event_type);

CREATE INDEX batchimageevent_job_id_created_at ON batch_image_events(job_id, created_at);

CREATE UNIQUE INDEX batchimageevent_job_id_event_hash ON batch_image_events(job_id, event_hash)
WHERE event_hash IS NOT NULL AND event_hash <> '';

CREATE UNIQUE INDEX batchimageitem_job_id_custom_id ON batch_image_items(job_id, custom_id);

CREATE INDEX batchimageitem_job_id_status ON batch_image_items(job_id, status);

CREATE INDEX batchimageitem_provider_source_object ON batch_image_items(provider_source_object);

CREATE UNIQUE INDEX `batchimagejob_batch_id` ON `batch_image_jobs` (`batch_id`);

CREATE INDEX `batchimagejob_downloaded_at` ON `batch_image_jobs` (`downloaded_at`);

CREATE INDEX `batchimagejob_idempotency_key` ON `batch_image_jobs` (`idempotency_key`) WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';

CREATE UNIQUE INDEX `batchimagejob_manifest_hash` ON `batch_image_jobs` (`manifest_hash`) WHERE manifest_hash IS NOT NULL AND manifest_hash <> '';

CREATE INDEX `batchimagejob_output_expires_at` ON `batch_image_jobs` (`output_expires_at`);

CREATE INDEX `batchimagejob_provider_status` ON `batch_image_jobs` (`provider`, `status`);

CREATE INDEX `batchimagejob_status` ON `batch_image_jobs` (`status`);

CREATE INDEX `batchimagejob_user_deleted_at` ON `batch_image_jobs` (`user_deleted_at`);

CREATE INDEX `batchimagejob_user_id_created_at` ON `batch_image_jobs` (`user_id`, `created_at`);

CREATE INDEX `channelmonitor_account_id` ON `channel_monitors` (`account_id`);

CREATE INDEX `channelmonitor_enabled_last_checked_at` ON `channel_monitors` (`enabled`, `last_checked_at`);

CREATE INDEX `channelmonitor_group_name` ON `channel_monitors` (`group_name`);

CREATE INDEX `channelmonitor_provider` ON `channel_monitors` (`provider`);

CREATE INDEX `channelmonitor_provider_api_mode` ON `channel_monitors` (`provider`, `api_mode`);

CREATE INDEX `channelmonitor_template_id` ON `channel_monitors` (`template_id`);

CREATE INDEX `channelmonitordailyrollup_bucket_date` ON `channel_monitor_daily_rollups` (`bucket_date`);

CREATE UNIQUE INDEX `channelmonitordailyrollup_monitor_id_model_bucket_date` ON `channel_monitor_daily_rollups` (`monitor_id`, `model`, `bucket_date`);

CREATE INDEX `channelmonitorhistory_checked_at` ON `channel_monitor_histories` (`checked_at`);

CREATE INDEX `channelmonitorhistory_monitor_id_model_checked_at` ON `channel_monitor_histories` (`monitor_id`, `model`, `checked_at`);

CREATE INDEX `channelmonitorrequesttemplate_provider_api_mode` ON `channel_monitor_request_templates` (`provider`, `api_mode`);

CREATE UNIQUE INDEX `channelmonitorrequesttemplate_provider_name` ON `channel_monitor_request_templates` (`provider`, `name`);

CREATE INDEX `compositemodelroute_deleted_at` ON `composite_model_routes` (`deleted_at`);

CREATE INDEX `compositemodelroute_group_id` ON `composite_model_routes` (`group_id`);

CREATE INDEX `compositemodelroute_group_id_enabled` ON `composite_model_routes` (`group_id`, `enabled`);

CREATE INDEX `compositemodelroute_group_id_endpoint` ON `composite_model_routes` (`group_id`, `endpoint`);

CREATE INDEX `compositemodelroute_group_id_target_platform` ON `composite_model_routes` (`group_id`, `target_platform`);

CREATE INDEX `compositemodelroute_priority` ON `composite_model_routes` (`priority`);

CREATE INDEX deletedapikeyaudit_key ON deleted_api_key_audits(key);

CREATE INDEX deletedapikeyaudit_user_id ON deleted_api_key_audits(user_id);

CREATE INDEX `errorpassthroughrule_enabled` ON `error_passthrough_rules` (`enabled`);

CREATE INDEX `errorpassthroughrule_priority` ON `error_passthrough_rules` (`priority`);

CREATE INDEX `group_deleted_at` ON `groups` (`deleted_at`);

CREATE INDEX `group_is_exclusive` ON `groups` (`is_exclusive`);

CREATE INDEX `group_platform` ON `groups` (`platform`);

CREATE INDEX `group_sort_order` ON `groups` (`sort_order`);

CREATE INDEX `group_status` ON `groups` (`status`);

CREATE INDEX `group_subscription_type` ON `groups` (`subscription_type`);

CREATE UNIQUE INDEX groups_name_unique_active
    ON groups(name) WHERE deleted_at IS NULL;

CREATE INDEX `idempotencyrecord_expires_at` ON `idempotency_records` (`expires_at`);

CREATE UNIQUE INDEX `idempotencyrecord_scope_idempotency_key_hash` ON `idempotency_records` (`scope`, `idempotency_key_hash`);

CREATE INDEX `idempotencyrecord_status_locked_until` ON `idempotency_records` (`status`, `locked_until`);

CREATE UNIQUE INDEX `identity_adoption_decisions_pending_auth_session_id_key` ON `identity_adoption_decisions` (`pending_auth_session_id`);

CREATE INDEX `identityadoptiondecision_identity_id` ON `identity_adoption_decisions` (`identity_id`);

CREATE UNIQUE INDEX `identityadoptiondecision_pending_auth_session_id` ON `identity_adoption_decisions` (`pending_auth_session_id`);

CREATE INDEX idx_audit_logs_action ON audit_logs(action);

CREATE INDEX idx_audit_logs_actor_created ON audit_logs(actor_user_id, created_at DESC);

CREATE INDEX idx_audit_logs_client_ip ON audit_logs(client_ip);

CREATE INDEX idx_audit_logs_created_at_id ON audit_logs(created_at DESC, id DESC);

CREATE INDEX idx_auth_cache_invalidation_outbox_available
    ON auth_cache_invalidation_outbox (available_at, id)
    WHERE claimed_at IS NULL;

CREATE INDEX idx_auth_cache_invalidation_outbox_cache_key
    ON auth_cache_invalidation_outbox (cache_key);

CREATE INDEX idx_auth_cache_invalidation_outbox_created_at
    ON auth_cache_invalidation_outbox (created_at);

CREATE INDEX idx_auth_cache_invalidation_outbox_lease
    ON auth_cache_invalidation_outbox (claimed_at)
    WHERE claimed_at IS NOT NULL;

CREATE INDEX idx_cas_model_pricing_rule_id
    ON channel_account_stats_model_pricing(rule_id);

CREATE INDEX idx_cas_pricing_intervals_pricing_id
    ON channel_account_stats_pricing_intervals(pricing_id);

CREATE INDEX idx_cas_pricing_rules_channel_id
    ON channel_account_stats_pricing_rules(channel_id);

CREATE INDEX idx_channel_groups_channel_id
    ON channel_groups(channel_id);

CREATE UNIQUE INDEX idx_channel_groups_group_id
    ON channel_groups(group_id);

CREATE INDEX idx_channel_model_pricing_channel_id
    ON channel_model_pricing(channel_id);

CREATE INDEX idx_channel_model_pricing_platform
    ON channel_model_pricing(platform);

CREATE INDEX idx_channel_monitor_v2_errors_category_time
    ON channel_monitor_v2_error_metrics_1m(error_category, bucket_start DESC);

CREATE INDEX idx_channel_monitor_v2_errors_rollup_category_time
    ON channel_monitor_v2_error_metrics_rollup(bucket_seconds, error_category, bucket_start DESC);

CREATE INDEX idx_channel_monitor_v2_errors_rollup_time
    ON channel_monitor_v2_error_metrics_rollup(bucket_seconds, bucket_start DESC);

CREATE INDEX idx_channel_monitor_v2_errors_time
    ON channel_monitor_v2_error_metrics_1m(bucket_start DESC);

CREATE INDEX idx_channel_monitor_v2_histograms_rollup_time
    ON channel_monitor_v2_latency_histograms_rollup(bucket_seconds, bucket_start DESC, metric);

CREATE INDEX idx_channel_monitor_v2_histograms_time
    ON channel_monitor_v2_latency_histograms_1m(bucket_start DESC, metric);

CREATE INDEX idx_channel_monitor_v2_metrics_group_time
    ON channel_monitor_v2_metrics_1m(group_id, bucket_start DESC);

CREATE INDEX idx_channel_monitor_v2_metrics_model_time
    ON channel_monitor_v2_metrics_1m(model, bucket_start DESC);

CREATE INDEX idx_channel_monitor_v2_metrics_platform_time
    ON channel_monitor_v2_metrics_1m(platform, bucket_start DESC);

CREATE INDEX idx_channel_monitor_v2_metrics_rollup_group_time
    ON channel_monitor_v2_metrics_rollup(bucket_seconds, group_id, bucket_start DESC);

CREATE INDEX idx_channel_monitor_v2_metrics_rollup_model_time
    ON channel_monitor_v2_metrics_rollup(bucket_seconds, model, bucket_start DESC);

CREATE INDEX idx_channel_monitor_v2_metrics_rollup_platform_time
    ON channel_monitor_v2_metrics_rollup(bucket_seconds, platform, bucket_start DESC);

CREATE INDEX idx_channel_monitor_v2_user_metrics_time
    ON channel_monitor_v2_user_metrics_1m(bucket_start DESC);

CREATE INDEX idx_channel_monitor_v2_user_metrics_user_time
    ON channel_monitor_v2_user_metrics_1m(user_id, bucket_start DESC);

CREATE INDEX idx_channel_monitor_v2_user_rollup_time
    ON channel_monitor_v2_user_metrics_rollup(bucket_seconds, bucket_start DESC);

CREATE INDEX idx_channel_monitor_v2_user_rollup_user_time
    ON channel_monitor_v2_user_metrics_rollup(bucket_seconds, user_id, bucket_start DESC);

CREATE INDEX idx_channel_pricing_intervals_pricing_id
    ON channel_pricing_intervals(pricing_id);

CREATE UNIQUE INDEX idx_channels_name ON channels(name);

CREATE INDEX idx_channels_status ON channels(status);

CREATE INDEX idx_content_moderation_logs_api_key_created_at
    ON content_moderation_logs(api_key_id, created_at DESC);

CREATE INDEX idx_content_moderation_logs_created_at
    ON content_moderation_logs(created_at DESC);

CREATE INDEX idx_content_moderation_logs_endpoint_created_at
    ON content_moderation_logs(endpoint, created_at DESC);

CREATE INDEX idx_content_moderation_logs_flagged_created_at
    ON content_moderation_logs(flagged, created_at DESC);

CREATE INDEX idx_content_moderation_logs_group_created_at
    ON content_moderation_logs(group_id, created_at DESC);

CREATE INDEX idx_content_moderation_logs_user_created_at
    ON content_moderation_logs(user_id, created_at DESC);

CREATE UNIQUE INDEX `idx_groups_duplicate_operation_id_active` ON `groups` (`duplicate_operation_id`) WHERE duplicate_operation_id IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX `idx_kv_store_expires_at` ON `kv_store` (`expires_at`);

CREATE INDEX idx_kv_store_namespace_ns
    ON kv_store_namespace (namespace);

CREATE INDEX idx_memory_batch_queue_ready ON memory_batch_queue(state, available_at);

CREATE INDEX idx_ops_alert_events_fired_at
    ON ops_alert_events(fired_at DESC);

CREATE INDEX idx_ops_alert_events_rule_status
    ON ops_alert_events(rule_id, status);

CREATE UNIQUE INDEX idx_ops_alert_rules_name_unique
    ON ops_alert_rules(name);

CREATE INDEX idx_ops_alert_silences_lookup
    ON ops_alert_silences(rule_id, platform, group_id, region, until);

CREATE INDEX idx_ops_error_logs_account_time ON ops_error_logs(account_id, created_at DESC);

CREATE INDEX idx_ops_error_logs_client_request_id ON ops_error_logs(client_request_id);

CREATE INDEX idx_ops_error_logs_created_at ON ops_error_logs(created_at DESC);

CREATE INDEX idx_ops_error_logs_group_time ON ops_error_logs(group_id, created_at DESC);

CREATE INDEX idx_ops_error_logs_platform_time ON ops_error_logs(platform, created_at DESC);

CREATE INDEX idx_ops_error_logs_request_id ON ops_error_logs(request_id);

CREATE INDEX idx_ops_error_logs_resolved_time ON ops_error_logs(resolved,created_at DESC);

CREATE INDEX idx_ops_error_logs_status_time ON ops_error_logs(status_code, created_at DESC);

CREATE INDEX idx_ops_error_logs_unresolved_time ON ops_error_logs(created_at DESC) WHERE resolved=0;

CREATE INDEX idx_ops_error_logs_user_time ON ops_error_logs(user_id,created_at DESC) WHERE user_id IS NOT NULL;

CREATE INDEX idx_ops_ingress_reject_aggregates_bucket
    ON ops_ingress_reject_aggregates(bucket_start DESC);

CREATE INDEX idx_ops_ingress_reject_aggregates_ip_bucket
    ON ops_ingress_reject_aggregates(client_ip, bucket_start DESC);

CREATE INDEX idx_ops_ingress_reject_aggregates_reason_bucket
    ON ops_ingress_reject_aggregates(reject_reason, bucket_start DESC);

CREATE INDEX idx_ops_metrics_daily_bucket
    ON ops_metrics_daily(bucket_date DESC);

CREATE UNIQUE INDEX idx_ops_metrics_daily_unique_dim
    ON ops_metrics_daily(bucket_date, COALESCE(platform, ''), COALESCE(group_id, 0));

CREATE INDEX idx_ops_metrics_hourly_bucket
    ON ops_metrics_hourly(bucket_start DESC);

CREATE UNIQUE INDEX idx_ops_metrics_hourly_unique_dim
    ON ops_metrics_hourly(bucket_start, COALESCE(platform, ''), COALESCE(group_id, 0));

CREATE INDEX idx_ops_system_log_cleanup_audits_created_at
    ON ops_system_log_cleanup_audits (created_at DESC, id DESC);

CREATE INDEX idx_ops_system_logs_account_id_created_at
    ON ops_system_logs (account_id, created_at DESC);

CREATE INDEX idx_ops_system_logs_api_key_id_created_at
    ON ops_system_logs (api_key_id, created_at DESC);

CREATE INDEX idx_ops_system_logs_client_request_id
    ON ops_system_logs (client_request_id);

CREATE INDEX idx_ops_system_logs_component_created_at
    ON ops_system_logs (component, created_at DESC);

CREATE INDEX idx_ops_system_logs_created_at_id
    ON ops_system_logs (created_at DESC, id DESC);

CREATE INDEX idx_ops_system_logs_level_created_at
    ON ops_system_logs (level, created_at DESC);

CREATE INDEX idx_ops_system_logs_platform_model_created_at
    ON ops_system_logs (platform, model, created_at DESC);

CREATE INDEX idx_ops_system_logs_request_id
    ON ops_system_logs (request_id);

CREATE INDEX idx_ops_system_logs_user_id_created_at
    ON ops_system_logs (user_id, created_at DESC);

CREATE INDEX idx_ops_system_metrics_created_at ON ops_system_metrics(created_at DESC);

CREATE INDEX idx_prompt_audit_events_created
    ON prompt_audit_events(created_at DESC, id DESC);

CREATE INDEX idx_prompt_audit_events_decision_created
    ON prompt_audit_events(decision, created_at DESC, id DESC);

CREATE INDEX idx_prompt_audit_events_job ON prompt_audit_events(job_id);

CREATE INDEX idx_prompt_audit_events_request ON prompt_audit_events(request_id);

CREATE INDEX idx_prompt_audit_events_risk_created
    ON prompt_audit_events(risk_level, created_at DESC, id DESC);

CREATE INDEX idx_prompt_audit_jobs_created
    ON prompt_audit_jobs(created_at DESC, id DESC);

CREATE INDEX idx_prompt_audit_jobs_request
    ON prompt_audit_jobs(request_id);

CREATE INDEX idx_prompt_audit_jobs_schedule
    ON prompt_audit_jobs(status, next_attempt_at, id);

CREATE INDEX idx_scheduler_outbox_created_at
    ON scheduler_outbox (created_at);

CREATE UNIQUE INDEX idx_scheduler_outbox_pending_dedup_key
    ON scheduler_outbox (dedup_key)
    WHERE dedup_key IS NOT NULL;

CREATE INDEX idx_stp_account_id ON scheduled_test_plans(account_id);

CREATE INDEX idx_stp_enabled_next_run
    ON scheduled_test_plans(enabled, next_run_at)
    WHERE enabled = 1;

CREATE INDEX idx_str_plan_created
    ON scheduled_test_results(plan_id, created_at DESC);

CREATE INDEX idx_sub2api_plugin_bindings_enabled_scope
    ON sub2api_plugin_bindings(platform, account_type, capability)
    WHERE enabled = 1;

CREATE UNIQUE INDEX idx_sub2api_plugin_bindings_one_enabled_scope
    ON sub2api_plugin_bindings(capability, platform, account_type)
    WHERE enabled = 1;

CREATE INDEX idx_sub2api_plugin_bindings_plugin_id
    ON sub2api_plugin_bindings(plugin_id);

CREATE INDEX idx_ual_frozen_thaw
    ON user_affiliate_ledger(user_id, frozen_until)
    WHERE frozen_until IS NOT NULL;

CREATE INDEX idx_usage_billing_dedup_created_at
    ON usage_billing_dedup (created_at);

CREATE UNIQUE INDEX idx_usage_billing_dedup_request_api_key
    ON usage_billing_dedup (request_id, api_key_id);

CREATE INDEX idx_usage_dashboard_daily_bucket_date
    ON usage_dashboard_daily(bucket_date DESC);

CREATE INDEX idx_usage_dashboard_daily_users_bucket_date
    ON usage_dashboard_daily_users(bucket_date);

CREATE INDEX idx_usage_dashboard_hourly_bucket_start
    ON usage_dashboard_hourly(bucket_start DESC);

CREATE INDEX idx_usage_dashboard_hourly_users_bucket_start
    ON usage_dashboard_hourly_users(bucket_start);

CREATE INDEX idx_usage_logs_request_type_created_at
    ON usage_logs (request_type, created_at);

CREATE INDEX idx_usage_logs_service_tier_created_at
    ON usage_logs (service_tier, created_at);

CREATE INDEX idx_user_affiliate_ledger_action
    ON user_affiliate_ledger(action);

CREATE UNIQUE INDEX idx_user_affiliate_ledger_operation_id
    ON user_affiliate_ledger(operation_id)
    WHERE operation_id IS NOT NULL;

CREATE INDEX idx_user_affiliate_ledger_rebate_lookup
    ON user_affiliate_ledger(action, source_order_id, user_id, source_user_id, created_at)
    WHERE action = 'accrue';

CREATE INDEX idx_user_affiliate_ledger_source_order_id
    ON user_affiliate_ledger(source_order_id)
    WHERE source_order_id IS NOT NULL;

CREATE INDEX idx_user_affiliate_ledger_user_id
    ON user_affiliate_ledger(user_id);

CREATE INDEX idx_user_affiliates_admin_settings
    ON user_affiliates(updated_at)
    WHERE aff_code_custom = 1 OR aff_rebate_rate_percent IS NOT NULL;

CREATE INDEX idx_user_affiliates_aff_quota
    ON user_affiliates(aff_quota);

CREATE INDEX idx_user_affiliates_inviter_id
    ON user_affiliates(inviter_id);

CREATE INDEX idx_user_group_rate_multipliers_group_id
    ON user_group_rate_multipliers(group_id);

CREATE INDEX passkey_credentials_last_used_at_idx
    ON passkey_credentials(last_used_at);

CREATE INDEX passkey_credentials_user_id_idx
    ON passkey_credentials(user_id);

CREATE INDEX `paymentauditlog_order_id` ON `payment_audit_logs` (`order_id`);

CREATE INDEX `paymentorder_created_at` ON `payment_orders` (`created_at`);

CREATE INDEX `paymentorder_expires_at` ON `payment_orders` (`expires_at`);

CREATE INDEX `paymentorder_order_type` ON `payment_orders` (`order_type`);

CREATE UNIQUE INDEX `paymentorder_out_trade_no` ON `payment_orders` (`out_trade_no`) WHERE out_trade_no <> '';

CREATE INDEX `paymentorder_paid_at` ON `payment_orders` (`paid_at`);

CREATE INDEX `paymentorder_payment_type_paid_at` ON `payment_orders` (`payment_type`, `paid_at`);

CREATE INDEX `paymentorder_status` ON `payment_orders` (`status`);

CREATE INDEX `paymentorder_user_id` ON `payment_orders` (`user_id`);

CREATE INDEX `paymentproviderinstance_enabled` ON `payment_provider_instances` (`enabled`);

CREATE INDEX `paymentproviderinstance_provider_key` ON `payment_provider_instances` (`provider_key`);

CREATE INDEX `pendingauthsession_completion_code_hash` ON `pending_auth_sessions` (`completion_code_hash`);

CREATE INDEX `pendingauthsession_expires_at` ON `pending_auth_sessions` (`expires_at`);

CREATE INDEX `pendingauthsession_provider_type_provider_key_provider_subject` ON `pending_auth_sessions` (`provider_type`, `provider_key`, `provider_subject`);

CREATE UNIQUE INDEX `pendingauthsession_session_token` ON `pending_auth_sessions` (`session_token`);

CREATE INDEX `pendingauthsession_target_user_id` ON `pending_auth_sessions` (`target_user_id`);

CREATE UNIQUE INDEX `promo_codes_code_key` ON `promo_codes` (`code`);

CREATE INDEX `promocode_expires_at` ON `promo_codes` (`expires_at`);

CREATE INDEX `promocode_status` ON `promo_codes` (`status`);

CREATE INDEX `promocodeusage_promo_code_id` ON `promo_code_usages` (`promo_code_id`);

CREATE UNIQUE INDEX `promocodeusage_promo_code_id_user_id` ON `promo_code_usages` (`promo_code_id`, `user_id`);

CREATE INDEX `promocodeusage_user_id` ON `promo_code_usages` (`user_id`);

CREATE INDEX `proxy_backup_proxy_id` ON `proxies` (`backup_proxy_id`);

CREATE INDEX `proxy_deleted_at` ON `proxies` (`deleted_at`);

CREATE INDEX `proxy_expires_at` ON `proxies` (`expires_at`);

CREATE INDEX `proxy_status` ON `proxies` (`status`);

CREATE UNIQUE INDEX `redeem_codes_code_key` ON `redeem_codes` (`code`);

CREATE INDEX `redeemcode_expires_at` ON `redeem_codes` (`expires_at`);

CREATE INDEX `redeemcode_group_id` ON `redeem_codes` (`group_id`);

CREATE INDEX `redeemcode_status` ON `redeem_codes` (`status`);

CREATE INDEX `redeemcode_used_by` ON `redeem_codes` (`used_by`);

CREATE UNIQUE INDEX `security_secrets_key_key` ON `security_secrets` (`key`);

CREATE UNIQUE INDEX `settings_key_key` ON `settings` (`key`);

CREATE INDEX `subscriptionplan_for_sale` ON `subscription_plans` (`for_sale`);

CREATE INDEX `subscriptionplan_group_id` ON `subscription_plans` (`group_id`);

CREATE UNIQUE INDEX `tls_fingerprint_profiles_name_key` ON `tls_fingerprint_profiles` (`name`);

CREATE INDEX `usagecleanuptask_canceled_at` ON `usage_cleanup_tasks` (`canceled_at`);

CREATE INDEX `usagecleanuptask_created_at` ON `usage_cleanup_tasks` (`created_at`);

CREATE INDEX `usagecleanuptask_status_created_at` ON `usage_cleanup_tasks` (`status`, `created_at`);

CREATE INDEX `usagelog_account_id` ON `usage_logs` (`account_id`);

CREATE INDEX `usagelog_api_key_id` ON `usage_logs` (`api_key_id`);

CREATE INDEX `usagelog_api_key_id_created_at` ON `usage_logs` (`api_key_id`, `created_at`);

CREATE INDEX `usagelog_created_at` ON `usage_logs` (`created_at`);

CREATE INDEX `usagelog_group_id` ON `usage_logs` (`group_id`);

CREATE INDEX `usagelog_group_id_created_at` ON `usage_logs` (`group_id`, `created_at`);

CREATE INDEX `usagelog_model` ON `usage_logs` (`model`);

CREATE INDEX `usagelog_request_id` ON `usage_logs` (`request_id`);

CREATE INDEX `usagelog_requested_model` ON `usage_logs` (`requested_model`);

CREATE INDEX `usagelog_subscription_id` ON `usage_logs` (`subscription_id`);

CREATE INDEX `usagelog_user_id` ON `usage_logs` (`user_id`);

CREATE INDEX `usagelog_user_id_created_at` ON `usage_logs` (`user_id`, `created_at`);

CREATE UNIQUE INDEX user_avatars_user_id_key
    ON user_avatars(user_id);

CREATE INDEX `user_deleted_at` ON `users` (`deleted_at`);

CREATE INDEX user_provider_default_grants_user_id_idx
    ON user_provider_default_grants(user_id);

CREATE UNIQUE INDEX user_provider_default_grants_user_provider_reason_key
    ON user_provider_default_grants(user_id, provider_type, grant_reason);

CREATE INDEX `user_status` ON `users` (`status`);

CREATE UNIQUE INDEX user_subscriptions_user_group_unique_active
    ON user_subscriptions(user_id, group_id) WHERE deleted_at IS NULL;

CREATE INDEX `userallowedgroup_group_id` ON `user_allowed_groups` (`group_id`);

CREATE INDEX `userattributedefinition_deleted_at` ON `user_attribute_definitions` (`deleted_at`);

CREATE INDEX `userattributedefinition_display_order` ON `user_attribute_definitions` (`display_order`);

CREATE INDEX `userattributedefinition_enabled` ON `user_attribute_definitions` (`enabled`);

CREATE INDEX `userattributedefinition_key` ON `user_attribute_definitions` (`key`);

CREATE INDEX `userattributevalue_attribute_id` ON `user_attribute_values` (`attribute_id`);

CREATE UNIQUE INDEX `userattributevalue_user_id_attribute_id` ON `user_attribute_values` (`user_id`, `attribute_id`);

CREATE INDEX `userplatformquota_user_id` ON `user_platform_quotas` (`user_id`);

CREATE UNIQUE INDEX `userplatformquota_user_id_platform` ON `user_platform_quotas` (`user_id`, `platform`) WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX users_email_unique_active
    ON users(email) WHERE deleted_at IS NULL;

CREATE INDEX `usersubscription_assigned_by` ON `user_subscriptions` (`assigned_by`);

CREATE INDEX `usersubscription_deleted_at` ON `user_subscriptions` (`deleted_at`);

CREATE INDEX `usersubscription_expires_at` ON `user_subscriptions` (`expires_at`);

CREATE INDEX `usersubscription_group_id` ON `user_subscriptions` (`group_id`);

CREATE INDEX `usersubscription_status` ON `user_subscriptions` (`status`);

CREATE INDEX `usersubscription_user_id` ON `user_subscriptions` (`user_id`);

CREATE INDEX `usersubscription_user_id_group_id` ON `user_subscriptions` (`user_id`, `group_id`);

CREATE INDEX `usersubscription_user_id_status_expires_at` ON `user_subscriptions` (`user_id`, `status`, `expires_at`);

INSERT INTO channel_monitor_v2_config VALUES(1,1,1,60,'[{"platform":"anthropic","enabled":true,"models":[]},{"platform":"openai","enabled":true,"models":[]},{"platform":"grok","enabled":true,"models":[]},{"platform":"kiro","enabled":true,"models":[]},{"platform":"gemini","enabled":true,"models":[]},{"platform":"antigravity","enabled":true,"models":[]}]','[]','[]','{}',NULL,s2a_utc_now());
INSERT INTO channel_monitor_v2_watermarks VALUES(1,NULL,NULL,NULL,NULL,NULL,s2a_utc_now());
INSERT INTO ops_alert_rules VALUES(1,'错误率过高','当错误率超过 5% 且持续 5 分钟时触发告警',1,'P1','error_rate','>',5,5,5,20,NULL,NULL,s2a_utc_now(),s2a_utc_now(),1);
INSERT INTO ops_alert_rules VALUES(2,'成功率过低','当成功率低于 95% 且持续 5 分钟时触发告警（服务可用性下降）',1,'P0','success_rate','<',95,5,5,15,NULL,NULL,s2a_utc_now(),s2a_utc_now(),1);
INSERT INTO ops_alert_rules VALUES(3,'P99延迟过高','当 P99 延迟超过 3000ms 且持续 10 分钟时触发告警',1,'P2','p99_latency_ms','>',3000,5,10,30,NULL,NULL,s2a_utc_now(),s2a_utc_now(),1);
INSERT INTO ops_alert_rules VALUES(4,'P95延迟过高','当 P95 延迟超过 2000ms 且持续 10 分钟时触发告警',1,'P2','p95_latency_ms','>',2000,5,10,30,NULL,NULL,s2a_utc_now(),s2a_utc_now(),1);
INSERT INTO ops_alert_rules VALUES(5,'CPU使用率过高','当 CPU 使用率超过 85% 且持续 10 分钟时触发告警',1,'P2','cpu_usage_percent','>',85,5,10,30,NULL,NULL,s2a_utc_now(),s2a_utc_now(),1);
INSERT INTO ops_alert_rules VALUES(6,'内存使用率过高','当内存使用率超过 90% 且持续 10 分钟时触发告警（可能导致 OOM）',1,'P1','memory_usage_percent','>',90,5,10,20,NULL,NULL,s2a_utc_now(),s2a_utc_now(),1);
INSERT INTO ops_alert_rules VALUES(7,'并发队列积压','当并发队列深度超过 100 且持续 5 分钟时触发告警（系统处理能力不足）',1,'P1','concurrency_queue_depth','>',100,5,5,20,NULL,NULL,s2a_utc_now(),s2a_utc_now(),1);
INSERT INTO ops_alert_rules VALUES(8,'错误率极高','当错误率超过 20% 且持续 1 分钟时触发告警（服务严重异常）',1,'P0','error_rate','>',20,1,1,15,NULL,NULL,s2a_utc_now(),s2a_utc_now(),1);
INSERT INTO usage_dashboard_aggregation_watermark VALUES(1,'1970-01-01T00:00:00Z',s2a_utc_now());
INSERT INTO usage_group_rollup_state VALUES(1,'1970-01-01','1970-01-01T00:00:00Z','Asia/Shanghai',s2a_utc_now());
