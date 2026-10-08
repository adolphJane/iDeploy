ALTER TABLE deployment_targets ADD COLUMN environment_revision INTEGER NOT NULL DEFAULT 0;
ALTER TABLE deployment_targets ADD COLUMN applied_environment_revision INTEGER NOT NULL DEFAULT 0;
ALTER TABLE deployment_releases ADD COLUMN environment_revision INTEGER NOT NULL DEFAULT 0;

CREATE TABLE deployment_environment_variables (
    id TEXT PRIMARY KEY,
    deployment_id TEXT NOT NULL REFERENCES deployment_targets(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('plain', 'secret')),
    plain_value TEXT NOT NULL DEFAULT '',
    credential_ref TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(deployment_id, name),
    CHECK (
        (kind = 'plain' AND credential_ref = '') OR
        (kind = 'secret' AND plain_value = '' AND credential_ref <> '')
    )
);

CREATE INDEX idx_deployment_environment_variables_deployment
    ON deployment_environment_variables(deployment_id, name);
