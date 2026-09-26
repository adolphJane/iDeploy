CREATE TABLE registry_connections (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('harbor', 'oci')),
    endpoint TEXT NOT NULL UNIQUE,
    credential_ref TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'unknown' CHECK (status IN (
        'unknown', 'checking', 'healthy', 'auth_required', 'degraded', 'unreachable'
    )),
    last_checked_at TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_registry_connections_status ON registry_connections(status);
