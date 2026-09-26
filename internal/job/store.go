package job

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("job not found")

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Enqueue(ctx context.Context, input NewJob) (Job, error) {
	if input.Type == "" {
		return Job{}, errors.New("job type is required")
	}
	if input.Payload == "" {
		input.Payload = "{}"
	}
	id, err := newID()
	if err != nil {
		return Job{}, err
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
        INSERT INTO jobs(id, type, lock_key, payload, status, created_at, updated_at)
        SELECT ?, ?, ?, ?, ?, ?, ? WHERE ? = '' OR NOT EXISTS (
            SELECT 1 FROM jobs WHERE lock_key=? AND status IN ('queued','running','canceling')
        )
    `, id, input.Type, input.LockKey, input.Payload, StatusQueued, formatTime(now), formatTime(now), input.LockKey, input.LockKey)
	if err != nil {
		return Job{}, fmt.Errorf("enqueue job: %w", err)
	}
	if n, err := result.RowsAffected(); err != nil {
		return Job{}, err
	} else if n == 0 {
		return Job{}, errors.New("该资源已有任务正在执行，请等待完成")
	}
	return s.Get(ctx, id)
}

func (s *Store) Get(ctx context.Context, id string) (Job, error) {
	row := s.db.QueryRowContext(ctx, jobSelect+" WHERE id = ?", id)
	result, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("get job: %w", err)
	}
	return result, nil
}

func (s *Store) List(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, jobSelect+" ORDER BY created_at DESC LIMIT ?", limit)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]Job, 0, limit)
	for rows.Next() {
		item, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job list: %w", err)
		}
		jobs = append(jobs, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate job list: %w", err)
	}
	return jobs, nil
}

func (s *Store) RecoverInterrupted(ctx context.Context) (int64, error) {
	now := formatTime(time.Now().UTC())
	result, err := s.db.ExecContext(ctx, `
        UPDATE jobs
        SET status = ?, error = 'iDeploy restarted before the job completed',
            message = 'Interrupted by application restart', finished_at = ?, updated_at = ?, lease_until = NULL
        WHERE status IN (?, ?)
    `, StatusFailed, now, now, StatusRunning, StatusCanceling)
	if err != nil {
		return 0, fmt.Errorf("recover interrupted jobs: %w", err)
	}
	return result.RowsAffected()
}

func (s *Store) ClaimNext(ctx context.Context, leaseDuration time.Duration) (*Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin job claim: %w", err)
	}
	defer tx.Rollback()

	var id string
	err = tx.QueryRowContext(ctx, `
        SELECT id
        FROM jobs
        WHERE status = ? AND cancel_requested = 0
        ORDER BY created_at ASC
        LIMIT 1
    `, StatusQueued).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select queued job: %w", err)
	}

	now := time.Now().UTC()
	leaseUntil := now.Add(leaseDuration)
	result, err := tx.ExecContext(ctx, `
        UPDATE jobs
        SET status = ?, attempt = attempt + 1, started_at = ?, lease_until = ?,
            message = 'Starting', progress = 0, updated_at = ?
        WHERE id = ? AND status = ?
    `, StatusRunning, formatTime(now), formatTime(leaseUntil), formatTime(now), id, StatusQueued)
	if err != nil {
		return nil, fmt.Errorf("claim queued job: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("read claimed job count: %w", err)
	}
	if affected != 1 {
		return nil, nil
	}

	row := tx.QueryRowContext(ctx, jobSelect+" WHERE id = ?", id)
	claimed, err := scanJob(row)
	if err != nil {
		return nil, fmt.Errorf("load claimed job: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit job claim: %w", err)
	}
	return &claimed, nil
}

func (s *Store) UpdateProgress(ctx context.Context, id string, progress int, message string) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	result, err := s.db.ExecContext(ctx, `
        UPDATE jobs SET progress = ?, message = ?, updated_at = ?
        WHERE id = ? AND status IN (?, ?)
    `, progress, message, formatTime(time.Now().UTC()), id, StatusRunning, StatusCanceling)
	if err != nil {
		return fmt.Errorf("update job progress: %w", err)
	}
	return requireAffected(result)
}

func (s *Store) AppendLog(ctx context.Context, id, level, message string) error {
	if level == "" {
		level = "info"
	}
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO job_logs(job_id, level, message, created_at) VALUES (?, ?, ?, ?)
    `, id, level, message, formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("append job log: %w", err)
	}
	return nil
}

func (s *Store) Finish(ctx context.Context, id string, status Status, message, errorMessage string) error {
	if status != StatusSucceeded && status != StatusFailed && status != StatusCanceled {
		return fmt.Errorf("invalid terminal job status %q", status)
	}
	progress := 100
	if status != StatusSucceeded {
		progress = 0
	}
	now := formatTime(time.Now().UTC())
	result, err := s.db.ExecContext(ctx, `
        UPDATE jobs
        SET status = ?, progress = ?, message = ?, error = ?, finished_at = ?,
            lease_until = NULL, updated_at = ?
        WHERE id = ?
    `, status, progress, message, errorMessage, now, now, id)
	if err != nil {
		return fmt.Errorf("finish job: %w", err)
	}
	return requireAffected(result)
}

func (s *Store) RequestCancel(ctx context.Context, id string) error {
	now := formatTime(time.Now().UTC())
	result, err := s.db.ExecContext(ctx, `
        UPDATE jobs
        SET cancel_requested = 1,
            status = CASE WHEN status = ? THEN ? ELSE ? END,
            message = CASE WHEN status = ? THEN 'Canceled before start' ELSE 'Cancel requested' END,
            finished_at = CASE WHEN status = ? THEN ? ELSE finished_at END,
            updated_at = ?
        WHERE id = ? AND status IN (?, ?)
    `, StatusQueued, StatusCanceled, StatusCanceling, StatusQueued, StatusQueued, now, now, id, StatusQueued, StatusRunning)
	if err != nil {
		return fmt.Errorf("request job cancellation: %w", err)
	}
	return requireAffected(result)
}

func (s *Store) CountActive(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM jobs WHERE status IN (?, ?, ?)", StatusQueued, StatusRunning, StatusCanceling).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active jobs: %w", err)
	}
	return count, nil
}

const jobSelect = `
    SELECT id, type, lock_key, payload, status, progress, message, error, attempt,
           cancel_requested, created_at, started_at, finished_at, lease_until, updated_at
    FROM jobs
`

type scanner interface {
	Scan(dest ...any) error
}

func scanJob(row scanner) (Job, error) {
	var result Job
	var status string
	var cancelRequested int
	var createdAt, updatedAt string
	var startedAt, finishedAt, leaseUntil sql.NullString

	err := row.Scan(
		&result.ID, &result.Type, &result.LockKey, &result.Payload, &status,
		&result.Progress, &result.Message, &result.Error, &result.Attempt,
		&cancelRequested, &createdAt, &startedAt, &finishedAt, &leaseUntil, &updatedAt,
	)
	if err != nil {
		return Job{}, err
	}

	result.Status = Status(status)
	result.CancelRequested = cancelRequested != 0
	if result.CreatedAt, err = parseTime(createdAt); err != nil {
		return Job{}, err
	}
	if result.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Job{}, err
	}
	if result.StartedAt, err = parseNullableTime(startedAt); err != nil {
		return Job{}, err
	}
	if result.FinishedAt, err = parseNullableTime(finishedAt); err != nil {
		return Job{}, err
	}
	if result.LeaseUntil, err = parseNullableTime(leaseUntil); err != nil {
		return Job{}, err
	}
	return result, nil
}

func requireAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored time %q: %w", value, err)
	}
	return parsed, nil
}

func parseNullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
