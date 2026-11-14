CREATE TABLE shared_sync_state (
    id INTEGER PRIMARY KEY CHECK(id=1),
    server_id TEXT NOT NULL,
    device_id TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL CHECK(mode IN ('preparing','shared')),
    cursor INTEGER NOT NULL DEFAULT 0,
    batch_id TEXT NOT NULL DEFAULT '',
    last_sync_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE shared_resource_revisions (
    kind TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    content_hash TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(kind, resource_id)
);

CREATE TABLE shared_pending_batches (
    id TEXT PRIMARY KEY,
    payload TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('pending','conflict')),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
