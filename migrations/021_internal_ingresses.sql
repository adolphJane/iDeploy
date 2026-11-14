CREATE TABLE internal_ingresses (
    fqdn TEXT PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE RESTRICT,
    gateway_port INTEGER NOT NULL DEFAULT 8766 CHECK(gateway_port BETWEEN 1 AND 65535),
    certificate_path TEXT NOT NULL DEFAULT '',
    certificate_key_path TEXT NOT NULL DEFAULT '',
    webroot TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','applying','ready','failed')),
    detail TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_internal_ingresses_server ON internal_ingresses(server_id);
