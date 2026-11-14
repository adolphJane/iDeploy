CREATE TABLE shared_path_routes (
    id TEXT PRIMARY KEY,
    fqdn TEXT NOT NULL,
    path_prefix TEXT NOT NULL,
    deployment_id TEXT NOT NULL REFERENCES deployment_targets(id) ON DELETE RESTRICT,
    service_name TEXT NOT NULL DEFAULT '',
    container_port INTEGER NOT NULL CHECK(container_port BETWEEN 1 AND 65535),
    host_ip TEXT NOT NULL DEFAULT '',
    host_port INTEGER NOT NULL DEFAULT 0,
    certificate_path TEXT NOT NULL DEFAULT '',
    certificate_key_path TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','applying','active','removing')),
    detail TEXT NOT NULL DEFAULT '',
    job_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(fqdn,path_prefix)
);
CREATE INDEX idx_shared_path_routes_deployment ON shared_path_routes(deployment_id);
