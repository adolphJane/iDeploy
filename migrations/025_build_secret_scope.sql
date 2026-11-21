ALTER TABLE deployment_environment_variables ADD COLUMN build_secret INTEGER NOT NULL DEFAULT 0 CHECK (build_secret IN (0,1));
