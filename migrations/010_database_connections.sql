CREATE TABLE database_connections (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    driver TEXT NOT NULL CHECK (driver IN ('postgres', 'mysql')),
    host TEXT NOT NULL,
    port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    database_name TEXT NOT NULL,
    username TEXT NOT NULL,
    ssl_mode TEXT NOT NULL DEFAULT 'require' CHECK (ssl_mode IN ('require', 'disable')),
    credential_ref TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_connected_at TEXT,
    last_error TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_database_connections_name ON database_connections(name COLLATE NOCASE, created_at);
