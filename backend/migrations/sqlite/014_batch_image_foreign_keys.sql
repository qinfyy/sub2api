-- Preserve rows while restoring the foreign keys from PostgreSQL migration 159.
-- An orphan in an earlier SQLite database fails the copy and rolls back this file.
CREATE TABLE batch_image_items_with_fk (
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
INSERT INTO batch_image_items_with_fk
SELECT id, job_id, custom_id, status, request_hash, prompt_preview,
       provider_source_object, source_line_number, source_byte_offset, source_byte_length,
       mime_type, file_extension, image_count, error_code, error_message, billed_amount,
       created_at, indexed_at
FROM batch_image_items;
DROP TABLE batch_image_items;
ALTER TABLE batch_image_items_with_fk RENAME TO batch_image_items;
CREATE UNIQUE INDEX batchimageitem_job_id_custom_id ON batch_image_items(job_id, custom_id);
CREATE INDEX batchimageitem_job_id_status ON batch_image_items(job_id, status);
CREATE INDEX batchimageitem_provider_source_object ON batch_image_items(provider_source_object);

CREATE TABLE batch_image_events_with_fk (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL REFERENCES batch_image_jobs(batch_id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    payload JSON,
    event_hash TEXT,
    created_at DATETIME NOT NULL
);
INSERT INTO batch_image_events_with_fk
SELECT id, job_id, event_type, payload, event_hash, created_at FROM batch_image_events;
DROP TABLE batch_image_events;
ALTER TABLE batch_image_events_with_fk RENAME TO batch_image_events;
CREATE INDEX batchimageevent_job_id_created_at ON batch_image_events(job_id, created_at);
CREATE INDEX batchimageevent_event_type ON batch_image_events(event_type);
CREATE UNIQUE INDEX batchimageevent_job_id_event_hash ON batch_image_events(job_id, event_hash)
WHERE event_hash IS NOT NULL AND event_hash <> '';
