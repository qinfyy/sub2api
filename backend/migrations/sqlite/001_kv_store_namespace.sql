-- kv_store_namespace 支持按前缀扫描键空间。
--
-- Redis 的 SCAN 在 SQLite 中没有等价物，内存缓存后端需要用前缀查询
-- 列出某个命名空间下的所有键（例如插件 KV）。把前缀显式存成独立列并
-- 建索引，比在 BLOB 主键上做 LIKE 前缀匹配更可控。
--
-- 与 kv_store（由 Ent 启动流程创建）配套：
--   kv_store.key        完整键
--   kv_store_namespace  键的前缀（命名空间）

CREATE TABLE IF NOT EXISTS kv_store_namespace (
    key        TEXT PRIMARY KEY,
    namespace  TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_kv_store_namespace_ns
    ON kv_store_namespace (namespace);
