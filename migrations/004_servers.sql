CREATE TABLE servers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL DEFAULT 22 CHECK (port BETWEEN 1 AND 65535),
    ssh_user TEXT NOT NULL,
    credential_ref TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'unknown' CHECK (status IN (
        'unknown', 'checking', 'healthy', 'auth_required', 'degraded', 'unreachable'
    )),
    last_checked_at TEXT,
    last_message TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(host, port, ssh_user)
);

CREATE INDEX idx_servers_name ON servers(name COLLATE NOCASE, created_at);
