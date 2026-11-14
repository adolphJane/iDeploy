CREATE TABLE shared_job_summaries (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    lock_key TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    progress INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TRIGGER shared_job_summary_insert AFTER INSERT ON jobs
WHEN EXISTS(SELECT 1 FROM shared_sync_state WHERE id=1 AND mode='shared')
BEGIN
    INSERT INTO shared_job_summaries(id,type,lock_key,status,progress,created_at,updated_at)
    VALUES(NEW.id,NEW.type,NEW.lock_key,NEW.status,NEW.progress,NEW.created_at,NEW.updated_at);
END;

CREATE TRIGGER shared_job_summary_update AFTER UPDATE ON jobs
WHEN EXISTS(SELECT 1 FROM shared_job_summaries WHERE id=NEW.id)
BEGIN
    UPDATE shared_job_summaries SET status=NEW.status,progress=NEW.progress,updated_at=NEW.updated_at WHERE id=NEW.id;
END;
