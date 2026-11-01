ALTER TABLE domain_bindings ADD COLUMN host_ip TEXT NOT NULL DEFAULT '';
ALTER TABLE domain_bindings ADD COLUMN host_port INTEGER NOT NULL DEFAULT 0;
ALTER TABLE domain_bindings ADD COLUMN job_id TEXT NOT NULL DEFAULT '';

CREATE TABLE server_ingresses (
    server_id TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE RESTRICT,
    config_hash TEXT NOT NULL DEFAULT '',
    image_ref TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
