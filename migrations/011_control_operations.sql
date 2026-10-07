CREATE TABLE control_operations (
    id TEXT PRIMARY KEY,
    command TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    response_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_control_operations_updated_at
    ON control_operations(updated_at DESC);
