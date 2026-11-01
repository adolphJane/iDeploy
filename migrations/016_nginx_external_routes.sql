CREATE TABLE nginx_external_routes (
    host_id TEXT PRIMARY KEY REFERENCES domain_hosts(id) ON DELETE RESTRICT,
    server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE RESTRICT,
    host_ip TEXT NOT NULL,
    host_port INTEGER NOT NULL CHECK(host_port BETWEEN 1 AND 65535),
    upstream_scheme TEXT NOT NULL DEFAULT 'http' CHECK(upstream_scheme IN ('http','https')),
    certificate_path TEXT NOT NULL DEFAULT '',
    certificate_key_path TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','applying','active','removing')),
    detail TEXT NOT NULL DEFAULT '',
    job_id TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_nginx_external_routes_server ON nginx_external_routes(server_id, status);
