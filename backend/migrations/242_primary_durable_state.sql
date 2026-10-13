CREATE TABLE IF NOT EXISTS kv_store (
    key TEXT PRIMARY KEY,
    value BYTEA NOT NULL,
    expires_at BIGINT,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_kv_store_expires_at ON kv_store(expires_at);

CREATE TABLE IF NOT EXISTS memory_batch_queue (
    batch_id TEXT PRIMARY KEY,
    state TEXT NOT NULL CHECK (state IN ('ready', 'delayed', 'active')),
    available_at BIGINT NOT NULL,
    heartbeat_at BIGINT NOT NULL DEFAULT 0,
    receipt TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_memory_batch_queue_ready ON memory_batch_queue(state, available_at);
