package deployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrNotFound = errors.New("deployment target not found")

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Create(ctx context.Context, input Input) (Target, error) {
	input, err := input.Validate()
	if err != nil {
		return Target{}, err
	}
	id, err := newID()
	if err != nil {
		return Target{}, err
	}
	now := formatTime(time.Now())
	ports, _ := json.Marshal(input.ContainerPorts)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO deployment_targets(id, project_id, server_id, desired_state, created_at, updated_at, workload_kind, workload_name, container_ports_json, restart_policy)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, input.ProjectID, input.ServerID, DesiredRunning, now, now, input.WorkloadKind, input.WorkloadName, string(ports), input.RestartPolicy)
	if err != nil {
		return Target{}, fmt.Errorf("create deployment target: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) Get(ctx context.Context, id string) (Target, error) {
	value, err := scanTarget(s.db.QueryRowContext(ctx, targetSelect+" WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Target{}, ErrNotFound
	}
	if err != nil {
		return Target{}, fmt.Errorf("get deployment target: %w", err)
	}
	return value, nil
}

func (s *Store) List(ctx context.Context) ([]Target, error) {
	rows, err := s.db.QueryContext(ctx, targetSelect+" ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("list deployment targets: %w", err)
	}
	defer rows.Close()
	values := make([]Target, 0)
	for rows.Next() {
		value, err := scanTarget(rows)
		if err != nil {
			return nil, fmt.Errorf("scan deployment target: %w", err)
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM deployment_targets WHERE id = ? AND NOT EXISTS (SELECT 1 FROM jobs WHERE lock_key='server:' || deployment_targets.server_id AND status IN ('queued','running','canceling'))", id)
	if err != nil {
		return fmt.Errorf("delete deployment target: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return errors.New("关联不存在或服务器存在未完成任务，无法移除")
	}
	return nil
}

func (s *Store) Configure(ctx context.Context, id string, input Input) error {
	current, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	input.ProjectID, input.ServerID = current.ProjectID, current.ServerID
	input, err = input.Validate()
	if err != nil {
		return err
	}
	ports, _ := json.Marshal(input.ContainerPorts)
	result, err := s.db.ExecContext(ctx, `
		UPDATE deployment_targets
		SET workload_kind=?, workload_name=?, container_ports_json=?, restart_policy=?, updated_at=?
		WHERE id=? AND NOT EXISTS (
			SELECT 1 FROM jobs WHERE lock_key='server:' || deployment_targets.server_id
			AND status IN ('queued','running','canceling')
		)
	`, input.WorkloadKind, input.WorkloadName, string(ports), input.RestartPolicy, formatTime(time.Now()), id)
	if err != nil {
		return fmt.Errorf("configure deployment target: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return errors.New("关联不存在或服务器存在未完成任务，无法修改")
	}
	return nil
}

// Update changes the complete local deployment configuration. It never
// modifies a remote workload; callers publish explicitly after reviewing it.
func (s *Store) Update(ctx context.Context, id string, input Input) (Target, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return Target{}, err
	}
	input, err = input.Validate()
	if err != nil {
		return Target{}, err
	}
	ports, _ := json.Marshal(input.ContainerPorts)
	result, err := s.db.ExecContext(ctx, `
		UPDATE deployment_targets
		SET project_id=?, server_id=?, workload_kind=?, workload_name=?,
			container_ports_json=?, restart_policy=?, updated_at=?
		WHERE id=? AND NOT EXISTS (
			SELECT 1 FROM jobs WHERE status IN ('queued','running','canceling')
			AND lock_key IN ('server:' || ?, 'server:' || ?)
		)
	`, input.ProjectID, input.ServerID, input.WorkloadKind, input.WorkloadName,
		string(ports), input.RestartPolicy, formatTime(time.Now()), id, current.ServerID, input.ServerID)
	if err != nil {
		return Target{}, fmt.Errorf("update deployment target: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return Target{}, err
	} else if affected == 0 {
		return Target{}, errors.New("关联不存在或原/目标服务器存在未完成任务，无法修改")
	}
	return s.Get(ctx, id)
}

func (s *Store) UpdateContainerPorts(ctx context.Context, id string, ports []string) error {
	encoded, err := json.Marshal(ports)
	if err != nil {
		return fmt.Errorf("encode container ports: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE deployment_targets SET container_ports_json=?, updated_at=?
		WHERE id=?
	`, string(encoded), formatTime(time.Now()), id)
	if err != nil {
		return fmt.Errorf("update inferred container ports: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateRelease(ctx context.Context, input ReleaseInput) (Release, error) {
	input.DeploymentID = strings.TrimSpace(input.DeploymentID)
	input.RegistryID = strings.TrimSpace(input.RegistryID)
	input.Version = strings.TrimSpace(input.Version)
	if input.DeploymentID == "" || input.Version == "" {
		return Release{}, errors.New("部署目标和版本不能为空")
	}
	if input.Action == "" {
		input.Action = "deploy"
	}
	if input.Action != "deploy" && input.Action != "rollback" {
		return Release{}, errors.New("不支持的发布动作")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Release{}, err
	}
	defer tx.Rollback()
	if input.Action == "deploy" {
		rows, err := tx.QueryContext(ctx, `SELECT version,action,status,image_ref,manifest FROM deployment_releases WHERE deployment_id=?`, input.DeploymentID)
		if err != nil {
			return Release{}, err
		}
		history := []Release{}
		for rows.Next() {
			var r Release
			if err := rows.Scan(&r.Version, &r.Action, &r.Status, &r.ImageRef, &r.Manifest); err != nil {
				rows.Close()
				return Release{}, err
			}
			history = append(history, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return Release{}, err
		}
		var validationErr error
		if input.AllowResume {
			validationErr = ValidateResumeVersion(input.Version, history)
		} else if input.AllowRetry {
			validationErr = ValidateRetryVersion(input.Version, history)
		} else {
			validationErr = ValidateNewVersion(input.Version, history)
		}
		if validationErr != nil {
			return Release{}, validationErr
		}
	}
	id, err := newID()
	if err != nil {
		return Release{}, err
	}
	now := formatTime(time.Now())
	var registryID any
	if input.RegistryID != "" {
		registryID = input.RegistryID
	}
	var rollbackToID any
	if input.RollbackToID != "" {
		rollbackToID = input.RollbackToID
	}
	var environmentRevision int64
	if err := tx.QueryRowContext(ctx, "SELECT environment_revision FROM deployment_targets WHERE id=?", input.DeploymentID).Scan(&environmentRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Release{}, ErrNotFound
		}
		return Release{}, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO deployment_releases(
			id, deployment_id, registry_id, action, version, image_ref, manifest,
			rollback_to_id, status, environment_revision, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, input.DeploymentID, registryID, input.Action, input.Version, input.ImageRef,
		input.Manifest, rollbackToID, ReleaseQueued, environmentRevision, now)
	if err != nil {
		return Release{}, fmt.Errorf("create deployment release: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Release{}, err
	}
	return s.GetRelease(ctx, id)
}

func (s *Store) VersionHistory(ctx context.Context, id string) ([]Release, error) {
	rows, err := s.db.QueryContext(ctx, releaseSelect+" WHERE deployment_id=? ORDER BY created_at DESC, rowid DESC", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Release
	for rows.Next() {
		r, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *Store) AttachReleaseJob(ctx context.Context, releaseID, jobID string) error {
	result, err := s.db.ExecContext(ctx, "UPDATE deployment_releases SET job_id=? WHERE id=?", jobID, releaseID)
	if err != nil {
		return fmt.Errorf("attach release job: %w", err)
	}
	return requireAffected(result)
}

func (s *Store) UpdateRelease(ctx context.Context, id string, status ReleaseStatus, detail, imageRef, sourceRevision, manifest string) error {
	if !validReleaseStatus(status) {
		return fmt.Errorf("invalid release status %q", status)
	}
	finishedAt := any(nil)
	if status == ReleaseSucceeded || status == ReleaseFailed || status == ReleaseCanceled {
		finishedAt = formatTime(time.Now())
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE deployment_releases
		SET status=?, detail=?, image_ref=CASE WHEN ?='' THEN image_ref ELSE ? END,
			source_revision=CASE WHEN ?='' THEN source_revision ELSE ? END,
			manifest=CASE WHEN ?='' THEN manifest ELSE ? END, finished_at=?
		WHERE id=?
	`, status, detail, imageRef, imageRef, sourceRevision, sourceRevision, manifest, manifest, finishedAt, id)
	if err != nil {
		return fmt.Errorf("update deployment release: %w", err)
	}
	return requireAffected(result)
}

func (s *Store) GetRelease(ctx context.Context, id string) (Release, error) {
	value, err := scanRelease(s.db.QueryRowContext(ctx, releaseSelect+" WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Release{}, ErrNotFound
	}
	if err != nil {
		return Release{}, fmt.Errorf("get deployment release: %w", err)
	}
	return value, nil
}

func (s *Store) ListReleases(ctx context.Context, deploymentID string, limit int) ([]Release, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.db.QueryContext(ctx, releaseSelect+" WHERE deployment_id=? ORDER BY created_at DESC LIMIT ?", deploymentID, limit)
	if err != nil {
		return nil, fmt.Errorf("list deployment releases: %w", err)
	}
	defer rows.Close()
	result := make([]Release, 0)
	for rows.Next() {
		value, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (s *Store) LatestSucceededRelease(ctx context.Context, deploymentID string) (Release, error) {
	value, err := scanRelease(s.db.QueryRowContext(ctx, releaseSelect+" WHERE deployment_id=? AND status='succeeded' ORDER BY created_at DESC,rowid DESC LIMIT 1", deploymentID))
	if errors.Is(err, sql.ErrNoRows) {
		return Release{}, ErrNotFound
	}
	if err != nil {
		return Release{}, fmt.Errorf("get latest successful release: %w", err)
	}
	return value, nil
}

func (s *Store) RecoverInterruptedReleases(ctx context.Context) (int64, error) {
	now := formatTime(time.Now())
	result, err := s.db.ExecContext(ctx, `
		UPDATE deployment_releases
		SET status='failed', detail='iDeploy 重启前发布任务未完成，请检查远端状态后重新发布', finished_at=?
		WHERE status NOT IN ('succeeded','failed','canceled')
		AND (job_id IS NULL OR EXISTS (
			SELECT 1 FROM jobs WHERE jobs.id=deployment_releases.job_id
			AND jobs.status IN ('succeeded','failed','canceled')
		))
	`, now)
	if err != nil {
		return 0, fmt.Errorf("recover interrupted releases: %w", err)
	}
	return result.RowsAffected()
}

func validReleaseStatus(status ReleaseStatus) bool {
	switch status {
	case ReleaseQueued, ReleasePreparing, ReleaseBuilding, ReleasePushing, ReleasePulling,
		ReleaseDeploying, ReleaseChecking, ReleaseSucceeded, ReleaseFailed, ReleaseCanceled:
		return true
	default:
		return false
	}
}

const releaseSelect = `
	SELECT id, deployment_id, COALESCE(registry_id,''), COALESCE(job_id,''), action,
		version, image_ref, source_revision, manifest, COALESCE(rollback_to_id,''),
		status, detail, environment_revision, created_at, finished_at
	FROM deployment_releases
`

func scanRelease(row scanner) (Release, error) {
	var value Release
	var status, createdAt string
	var finishedAt sql.NullString
	if err := row.Scan(&value.ID, &value.DeploymentID, &value.RegistryID, &value.JobID,
		&value.Action, &value.Version, &value.ImageRef, &value.SourceRevision, &value.Manifest,
		&value.RollbackToID, &status, &value.Detail, &value.EnvironmentRevision, &createdAt, &finishedAt); err != nil {
		return Release{}, err
	}
	value.Status = ReleaseStatus(status)
	var err error
	if value.CreatedAt, err = parseTime(createdAt); err != nil {
		return Release{}, err
	}
	if finishedAt.Valid {
		parsed, err := parseTime(finishedAt.String)
		if err != nil {
			return Release{}, err
		}
		value.FinishedAt = &parsed
	}
	return value, nil
}

const targetSelect = `
	SELECT id, project_id, server_id, desired_state, created_at, updated_at, workload_kind, workload_name,
		container_ports_json, restart_policy, environment_revision, applied_environment_revision
	FROM deployment_targets
`

type scanner interface{ Scan(...any) error }

func scanTarget(row scanner) (Target, error) {
	var value Target
	var portsJSON string
	var desiredState, createdAt, updatedAt string
	if err := row.Scan(&value.ID, &value.ProjectID, &value.ServerID, &desiredState, &createdAt, &updatedAt, &value.WorkloadKind, &value.WorkloadName, &portsJSON, &value.RestartPolicy, &value.EnvironmentRevision, &value.AppliedEnvironmentRevision); err != nil {
		return Target{}, err
	}
	value.DesiredState = DesiredState(desiredState)
	if err := json.Unmarshal([]byte(portsJSON), &value.ContainerPorts); err != nil {
		return Target{}, fmt.Errorf("decode deployment ports: %w", err)
	}
	var err error
	if value.CreatedAt, err = parseTime(createdAt); err != nil {
		return Target{}, err
	}
	if value.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Target{}, err
	}
	return value, nil
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
