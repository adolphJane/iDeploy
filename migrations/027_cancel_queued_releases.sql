UPDATE deployment_releases
SET status = 'canceled', detail = '发布任务在开始前取消',
    finished_at = (SELECT finished_at FROM jobs WHERE jobs.id = deployment_releases.job_id)
WHERE status = 'queued' AND EXISTS (
    SELECT 1 FROM jobs WHERE jobs.id = deployment_releases.job_id
    AND jobs.status = 'canceled' AND jobs.started_at IS NULL
);

CREATE TRIGGER cancel_queued_deployment_release
AFTER UPDATE OF status ON jobs
WHEN OLD.status = 'queued' AND NEW.status = 'canceled'
    AND NEW.type IN ('deployment.release', 'deployment.resume', 'deployment.rollback')
BEGIN
    UPDATE deployment_releases
    SET status = 'canceled', detail = '发布任务在开始前取消', finished_at = NEW.finished_at
    WHERE status = 'queued'
        AND (job_id = NEW.id OR (job_id IS NULL AND id = CASE
            WHEN json_valid(NEW.payload) THEN json_extract(NEW.payload, '$.releaseId') END));
END;

ALTER TABLE deployment_releases ADD COLUMN remote_cancel_delivered INTEGER NOT NULL DEFAULT 0;
