ALTER TABLE deployment_targets ADD COLUMN workload_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE deployment_targets ADD COLUMN workload_name TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX idx_deployment_workload ON deployment_targets(server_id, workload_kind, workload_name) WHERE workload_name != '';
CREATE TABLE remote_snapshots (
    server_id TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    containers_json TEXT NOT NULL,
    observed_at TEXT NOT NULL
);
