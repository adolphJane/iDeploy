ALTER TABLE deployment_targets ADD COLUMN build_mode TEXT NOT NULL DEFAULT 'local' CHECK (build_mode IN ('local','remote'));
ALTER TABLE deployment_releases ADD COLUMN build_mode TEXT NOT NULL DEFAULT 'local' CHECK (build_mode IN ('local','remote'));
ALTER TABLE deployment_releases ADD COLUMN source_commit TEXT NOT NULL DEFAULT '';
ALTER TABLE deployment_releases ADD COLUMN delivery_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE deployment_releases ADD COLUMN delivery_commit TEXT NOT NULL DEFAULT '';
ALTER TABLE deployment_releases ADD COLUMN delivery_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE deployment_releases ADD COLUMN remote_job_id TEXT NOT NULL DEFAULT '';
