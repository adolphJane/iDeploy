CREATE TABLE deployment_publish_paths (
    deployment_id TEXT PRIMARY KEY REFERENCES deployment_targets(id) ON DELETE CASCADE,
    fqdn TEXT NOT NULL,
    path_prefix TEXT NOT NULL,
    service_name TEXT NOT NULL DEFAULT '',
    container_port INTEGER NOT NULL CHECK(container_port BETWEEN 1 AND 65535),
    updated_at TEXT NOT NULL,
    UNIQUE(fqdn, path_prefix)
);

CREATE TABLE release_path_intents (
    release_id TEXT PRIMARY KEY REFERENCES deployment_releases(id) ON DELETE CASCADE,
    fqdn TEXT NOT NULL,
    path_prefix TEXT NOT NULL,
    service_name TEXT NOT NULL DEFAULT '',
    container_port INTEGER NOT NULL CHECK(container_port BETWEEN 1 AND 65535)
);
