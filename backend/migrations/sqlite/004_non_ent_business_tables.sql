-- Tables used directly by business code but not represented by Ent models.
-- Keep these definitions aligned with their PostgreSQL migrations while using
-- SQLite storage types and expression-free defaults.

CREATE TABLE IF NOT EXISTS ops_system_logs (
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

CREATE INDEX IF NOT EXISTS idx_ops_system_logs_created_at_id
    ON ops_system_logs (created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_ops_system_logs_level_created_at
    ON ops_system_logs (level, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ops_system_logs_component_created_at
    ON ops_system_logs (component, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ops_system_logs_request_id
    ON ops_system_logs (request_id);
CREATE INDEX IF NOT EXISTS idx_ops_system_logs_client_request_id
    ON ops_system_logs (client_request_id);
CREATE INDEX IF NOT EXISTS idx_ops_system_logs_user_id_created_at
    ON ops_system_logs (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ops_system_logs_account_id_created_at
    ON ops_system_logs (account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ops_system_logs_api_key_id_created_at
    ON ops_system_logs (api_key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ops_system_logs_platform_model_created_at
    ON ops_system_logs (platform, model, created_at DESC);

CREATE TABLE IF NOT EXISTS ops_system_log_cleanup_audits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    operator_id INTEGER NOT NULL,
    conditions TEXT NOT NULL DEFAULT '{}',
    deleted_rows INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_ops_system_log_cleanup_audits_created_at
    ON ops_system_log_cleanup_audits (created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS scheduled_test_plans (
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

CREATE INDEX IF NOT EXISTS idx_stp_account_id ON scheduled_test_plans(account_id);
CREATE INDEX IF NOT EXISTS idx_stp_enabled_next_run
    ON scheduled_test_plans(enabled, next_run_at)
    WHERE enabled = 1;

CREATE TABLE IF NOT EXISTS scheduled_test_results (
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

CREATE INDEX IF NOT EXISTS idx_str_plan_created
    ON scheduled_test_results(plan_id, created_at DESC);

CREATE TABLE IF NOT EXISTS sub2api_plugin_installations (
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

CREATE TABLE IF NOT EXISTS sub2api_plugin_bindings (
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

CREATE INDEX IF NOT EXISTS idx_sub2api_plugin_bindings_plugin_id
    ON sub2api_plugin_bindings(plugin_id);
CREATE INDEX IF NOT EXISTS idx_sub2api_plugin_bindings_enabled_scope
    ON sub2api_plugin_bindings(platform, account_type, capability)
    WHERE enabled = 1;
CREATE UNIQUE INDEX IF NOT EXISTS idx_sub2api_plugin_bindings_one_enabled_scope
    ON sub2api_plugin_bindings(capability, platform, account_type)
    WHERE enabled = 1;

CREATE TABLE IF NOT EXISTS ops_error_logs (
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
);

CREATE INDEX IF NOT EXISTS idx_ops_error_logs_created_at ON ops_error_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ops_error_logs_platform_time ON ops_error_logs(platform, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ops_error_logs_group_time ON ops_error_logs(group_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ops_error_logs_account_time ON ops_error_logs(account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ops_error_logs_status_time ON ops_error_logs(status_code, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ops_error_logs_request_id ON ops_error_logs(request_id);
CREATE INDEX IF NOT EXISTS idx_ops_error_logs_client_request_id ON ops_error_logs(client_request_id);

CREATE TABLE IF NOT EXISTS ops_system_metrics (
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
);

CREATE INDEX IF NOT EXISTS idx_ops_system_metrics_created_at ON ops_system_metrics(created_at DESC);

CREATE TABLE IF NOT EXISTS ops_job_heartbeats (
    job_name VARCHAR(64) PRIMARY KEY,
    last_run_at TEXT,
    last_success_at TEXT,
    last_error_at TEXT,
    last_error TEXT,
    last_duration_ms INTEGER,
    last_result TEXT,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS ops_alert_rules (
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
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ops_alert_rules_name_unique
    ON ops_alert_rules(name);

CREATE TABLE IF NOT EXISTS ops_alert_events (
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

CREATE INDEX IF NOT EXISTS idx_ops_alert_events_rule_status
    ON ops_alert_events(rule_id, status);
CREATE INDEX IF NOT EXISTS idx_ops_alert_events_fired_at
    ON ops_alert_events(fired_at DESC);

CREATE TABLE IF NOT EXISTS ops_metrics_hourly (
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

CREATE UNIQUE INDEX IF NOT EXISTS idx_ops_metrics_hourly_unique_dim
    ON ops_metrics_hourly(bucket_start, COALESCE(platform, ''), COALESCE(group_id, 0));
CREATE INDEX IF NOT EXISTS idx_ops_metrics_hourly_bucket
    ON ops_metrics_hourly(bucket_start DESC);

CREATE TABLE IF NOT EXISTS ops_metrics_daily (
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

CREATE UNIQUE INDEX IF NOT EXISTS idx_ops_metrics_daily_unique_dim
    ON ops_metrics_daily(bucket_date, COALESCE(platform, ''), COALESCE(group_id, 0));
CREATE INDEX IF NOT EXISTS idx_ops_metrics_daily_bucket
    ON ops_metrics_daily(bucket_date DESC);

CREATE TABLE IF NOT EXISTS audit_logs (
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

CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at_id ON audit_logs(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_created ON audit_logs(actor_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);
CREATE INDEX IF NOT EXISTS idx_audit_logs_client_ip ON audit_logs(client_ip);

CREATE TABLE IF NOT EXISTS deleted_api_key_audits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key VARCHAR(128) NOT NULL,
    api_key_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    key_name VARCHAR(100) NOT NULL DEFAULT '',
    deleted_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS deletedapikeyaudit_key ON deleted_api_key_audits(key);
CREATE INDEX IF NOT EXISTS deletedapikeyaudit_user_id ON deleted_api_key_audits(user_id);

CREATE TABLE IF NOT EXISTS prompt_audit_jobs (
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

CREATE INDEX IF NOT EXISTS idx_prompt_audit_jobs_schedule
    ON prompt_audit_jobs(status, next_attempt_at, id);
CREATE INDEX IF NOT EXISTS idx_prompt_audit_jobs_request
    ON prompt_audit_jobs(request_id);
CREATE INDEX IF NOT EXISTS idx_prompt_audit_jobs_created
    ON prompt_audit_jobs(created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS prompt_audit_events (
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

CREATE INDEX IF NOT EXISTS idx_prompt_audit_events_job ON prompt_audit_events(job_id);
CREATE INDEX IF NOT EXISTS idx_prompt_audit_events_request ON prompt_audit_events(request_id);
CREATE INDEX IF NOT EXISTS idx_prompt_audit_events_decision_created
    ON prompt_audit_events(decision, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_prompt_audit_events_risk_created
    ON prompt_audit_events(risk_level, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_prompt_audit_events_created
    ON prompt_audit_events(created_at DESC, id DESC);
