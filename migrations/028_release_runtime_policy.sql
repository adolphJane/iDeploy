CREATE TABLE deployment_runtime_policies (
    deployment_id TEXT PRIMARY KEY REFERENCES deployment_targets(id) ON DELETE CASCADE,
    revision INTEGER NOT NULL CHECK (revision > 0),
    policy_json TEXT NOT NULL
);

CREATE TABLE release_runtime_policies (
    release_id TEXT PRIMARY KEY REFERENCES deployment_releases(id) ON DELETE CASCADE,
    revision INTEGER NOT NULL CHECK (revision > 0),
    policy_json TEXT NOT NULL,
    before_json TEXT NOT NULL DEFAULT '[]',
    after_json TEXT NOT NULL DEFAULT '[]'
);

ALTER TABLE jobs ADD COLUMN control_request_id TEXT;
CREATE UNIQUE INDEX idx_jobs_control_request_id ON jobs(control_request_id) WHERE control_request_id IS NOT NULL;
