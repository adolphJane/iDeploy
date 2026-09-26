ALTER TABLE deployment_targets ADD COLUMN container_ports_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE deployment_targets ADD COLUMN restart_policy TEXT NOT NULL DEFAULT 'unless-stopped';
ALTER TABLE registry_connections ADD COLUMN username TEXT NOT NULL DEFAULT '';

CREATE TABLE deployment_releases (
    id TEXT PRIMARY KEY,
    deployment_id TEXT NOT NULL REFERENCES deployment_targets(id) ON DELETE CASCADE,
    registry_id TEXT REFERENCES registry_connections(id) ON DELETE SET NULL,
    job_id TEXT REFERENCES jobs(id) ON DELETE SET NULL,
    action TEXT NOT NULL DEFAULT 'deploy' CHECK (action IN ('deploy', 'rollback')),
    version TEXT NOT NULL,
    image_ref TEXT NOT NULL DEFAULT '',
    source_revision TEXT NOT NULL DEFAULT '',
    manifest TEXT NOT NULL DEFAULT '',
    rollback_to_id TEXT REFERENCES deployment_releases(id) ON DELETE SET NULL,
    status TEXT NOT NULL CHECK (status IN (
        'queued', 'preparing', 'building', 'pushing', 'pulling',
        'deploying', 'checking', 'succeeded', 'failed', 'canceled'
    )),
    detail TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    finished_at TEXT
);

CREATE INDEX idx_deployment_releases_target_created
    ON deployment_releases(deployment_id, created_at DESC);

CREATE UNIQUE INDEX idx_deployment_releases_job
    ON deployment_releases(job_id)
    WHERE job_id IS NOT NULL;
