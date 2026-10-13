-- Billing idempotency and transactional outbox tables used by the
-- PostgreSQL migrations but absent from the Ent schema.

CREATE TABLE IF NOT EXISTS usage_billing_dedup (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id TEXT NOT NULL,
    api_key_id INTEGER NOT NULL,
    request_fingerprint TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_billing_dedup_request_api_key
    ON usage_billing_dedup (request_id, api_key_id);

CREATE INDEX IF NOT EXISTS idx_usage_billing_dedup_created_at
    ON usage_billing_dedup (created_at);

CREATE TABLE IF NOT EXISTS usage_billing_dedup_archive (
    request_id TEXT NOT NULL,
    api_key_id INTEGER NOT NULL,
    request_fingerprint TEXT NOT NULL,
    created_at TEXT NOT NULL,
    archived_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (request_id, api_key_id)
);

CREATE TABLE IF NOT EXISTS scheduler_outbox (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL,
    account_id INTEGER,
    group_id INTEGER,
    payload TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    dedup_key TEXT
);

CREATE INDEX IF NOT EXISTS idx_scheduler_outbox_created_at
    ON scheduler_outbox (created_at);

CREATE UNIQUE INDEX IF NOT EXISTS idx_scheduler_outbox_pending_dedup_key
    ON scheduler_outbox (dedup_key)
    WHERE dedup_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS auth_cache_invalidation_outbox (
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

CREATE INDEX IF NOT EXISTS idx_auth_cache_invalidation_outbox_available
    ON auth_cache_invalidation_outbox (available_at, id)
    WHERE claimed_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_auth_cache_invalidation_outbox_lease
    ON auth_cache_invalidation_outbox (claimed_at)
    WHERE claimed_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_auth_cache_invalidation_outbox_cache_key
    ON auth_cache_invalidation_outbox (cache_key);

CREATE INDEX IF NOT EXISTS idx_auth_cache_invalidation_outbox_created_at
    ON auth_cache_invalidation_outbox (created_at);
