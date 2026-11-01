CREATE TABLE domains (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    note TEXT NOT NULL DEFAULT '',
    dns_connection_id TEXT REFERENCES dns_connections(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE domain_hosts (
    id TEXT PRIMARY KEY,
    domain_id TEXT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    fqdn TEXT NOT NULL UNIQUE,
    note TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(domain_id, label)
);
CREATE INDEX idx_domain_hosts_domain ON domain_hosts(domain_id, label);

CREATE TABLE dns_connections (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    access_key_id TEXT NOT NULL,
    credential_ref TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE domain_bindings (
    id TEXT PRIMARY KEY,
    host_id TEXT NOT NULL UNIQUE REFERENCES domain_hosts(id) ON DELETE RESTRICT,
    deployment_id TEXT NOT NULL REFERENCES deployment_targets(id) ON DELETE RESTRICT,
    service_name TEXT NOT NULL DEFAULT '',
    container_port INTEGER NOT NULL DEFAULT 0,
    upstream_protocol TEXT NOT NULL DEFAULT 'http' CHECK (upstream_protocol IN ('http','https')),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','applying','active','failed','removing')),
    detail TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_domain_bindings_deployment ON domain_bindings(deployment_id);
