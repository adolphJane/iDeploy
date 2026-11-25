CREATE TABLE managed_database_instances (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    environment TEXT NOT NULL CHECK (environment IN ('test', 'production')),
    server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE RESTRICT,
    driver TEXT NOT NULL CHECK (driver IN ('postgres', 'mysql')),
    engine_version TEXT NOT NULL,
    listen_ip TEXT NOT NULL,
    port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    database_name TEXT NOT NULL,
    app_username TEXT NOT NULL DEFAULT 'app',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'deploying', 'restoring', 'ready', 'failed', 'restore_failed')),
    created_at TEXT NOT NULL,
    UNIQUE(server_id, port)
);

CREATE TABLE managed_database_backups (
    id TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL REFERENCES managed_database_instances(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL
);
CREATE INDEX idx_managed_backups_instance ON managed_database_backups(instance_id, created_at DESC);
