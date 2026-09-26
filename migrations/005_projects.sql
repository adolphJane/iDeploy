CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);

CREATE UNIQUE INDEX idx_projects_name_active
    ON projects(name COLLATE NOCASE)
    WHERE deleted_at IS NULL;

CREATE TABLE project_sources (
    project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('local', 'git')),
    local_path TEXT NOT NULL DEFAULT '',
    repository_url TEXT NOT NULL DEFAULT '',
    ref_kind TEXT NOT NULL DEFAULT 'branch' CHECK (ref_kind IN ('branch', 'tag', 'commit')),
    ref_value TEXT NOT NULL DEFAULT '',
    resolved_commit TEXT NOT NULL DEFAULT '',
    credential_ref TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'needs_configuration' CHECK (status IN (
        'ready', 'needs_configuration', 'unavailable', 'checking', 'failed'
    )),
    last_checked_at TEXT,
    last_error TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX idx_project_sources_local_path
    ON project_sources(local_path)
    WHERE local_path != '';

CREATE UNIQUE INDEX idx_project_sources_git_ref
    ON project_sources(repository_url, ref_kind, ref_value)
    WHERE repository_url != '';

CREATE TABLE build_configs (
    project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    context_path TEXT NOT NULL DEFAULT '.',
    dockerfile_path TEXT NOT NULL DEFAULT '',
    compose_path TEXT NOT NULL DEFAULT '',
    image_repository TEXT NOT NULL DEFAULT '',
    build_args_json TEXT NOT NULL DEFAULT '{}',
    target TEXT NOT NULL DEFAULT '',
    platforms_json TEXT NOT NULL DEFAULT '[]'
);
