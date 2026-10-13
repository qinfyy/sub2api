-- Preserve rows and fail the upgrade on existing duplicate nonempty IDs so
-- operators can reconcile them. Never silently delete potentially billed data.
CREATE UNIQUE INDEX idx_usage_logs_request_id_api_key_unique
    ON usage_logs (request_id, api_key_id) WHERE request_id <> '';
