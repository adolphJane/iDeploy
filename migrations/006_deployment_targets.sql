CREATE TABLE deployment_targets (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    desired_state TEXT NOT NULL DEFAULT 'running' CHECK (desired_state IN ('running', 'stopped')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(project_id, server_id)
);

CREATE INDEX idx_deployment_targets_server ON deployment_targets(server_id, created_at);
