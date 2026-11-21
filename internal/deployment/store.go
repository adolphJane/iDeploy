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
		INSERT INTO deployment_targets(id, project_id, server_id, desired_state, created_at, updated_at, workload_kind, workload_name, container_ports_json, restart_policy, build_mode)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, input.ProjectID, input.ServerID, DesiredRunning, now, now, input.WorkloadKind, input.WorkloadName, string(ports), input.RestartPolicy, input.BuildMode)
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
		SET workload_kind=?, workload_name=?, container_ports_json=?, restart_policy=?, build_mode=?, updated_at=?
		WHERE id=? AND NOT EXISTS (
			SELECT 1 FROM jobs WHERE lock_key='server:' || deployment_targets.server_id
			AND status IN ('queued','running','canceling')
		)
	`, input.WorkloadKind, input.WorkloadName, string(ports), input.RestartPolicy, input.BuildMode, formatTime(time.Now()), id)
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
			container_ports_json=?, restart_policy=?, build_mode=?, updated_at=?
		WHERE id=? AND NOT EXISTS (
			SELECT 1 FROM jobs WHERE status IN ('queued','running','canceling')
			AND lock_key IN ('server:' || ?, 'server:' || ?)
		)
	`, input.ProjectID, input.ServerID, input.WorkloadKind, input.WorkloadName,
		string(ports), input.RestartPolicy, input.BuildMode, formatTime(time.Now()), id, current.ServerID, input.ServerID)
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
	if input.BuildMode == "" {
		input.BuildMode = BuildLocal
	}
	if !input.BuildMode.Valid() {
		return Release{}, errors.New("不支持的构建位置")
	}
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
		rows, err := tx.QueryContext(ctx, `SELECT version,action,status,image_ref,manifest,manifest_envelope FROM deployment_releases WHERE deployment_id=?`, input.DeploymentID)
		if err != nil {
			return Release{}, err
		}
		history := []Release{}
		for rows.Next() {
			var r Release
			if err := rows.Scan(&r.Version, &r.Action, &r.Status, &r.ImageRef, &r.Manifest, &r.manifestEnvelope); err != nil {
				rows.Close()
				return Release{}, err
			}
			if r.manifestEnvelope != "" {
				r.Manifest = "encrypted"
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
	manifestEnvelope, err := sealManifest(ctx, input.Manifest)
	if err != nil {
		return Release{}, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO deployment_releases(
			id, deployment_id, registry_id, action, version, image_ref, manifest, manifest_envelope,
			rollback_to_id, status, environment_revision, created_at, build_mode
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, input.DeploymentID, registryID, input.Action, input.Version, input.ImageRef,
		"", manifestEnvelope, rollbackToID, ReleaseQueued, environmentRevision, now, input.BuildMode)
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
		if err := hydrateManifest(ctx, &r); err != nil {
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

func (s *Store) SetReleaseDelivery(ctx context.Context, id, sourceCommit, deliveryRef, deliveryCommit, digest, remoteJobID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE deployment_releases SET source_commit=?,delivery_ref=?,delivery_commit=?,delivery_digest=?,remote_job_id=?,source_revision=? WHERE id=?`, sourceCommit, deliveryRef, deliveryCommit, digest, remoteJobID, sourceCommit, id)
	if err != nil {
		return err
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
	manifestEnvelope, err := sealManifest(ctx, manifest)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE deployment_releases
		SET status=?, detail=?, image_ref=CASE WHEN ?='' THEN image_ref ELSE ? END,
			source_revision=CASE WHEN ?='' THEN source_revision ELSE ? END,
			manifest=CASE WHEN ?='' THEN manifest ELSE '' END,
			manifest_envelope=CASE WHEN ?='' THEN manifest_envelope ELSE ? END, finished_at=?
		WHERE id=?
	`, status, detail, imageRef, imageRef, sourceRevision, sourceRevision, manifest, manifestEnvelope, manifestEnvelope, finishedAt, id)
	if err != nil {
		return fmt.Errorf("update deployment release: %w", err)
	}
	return requireAffected(result)
}

func (s *Store) SetManifest(ctx context.Context, id, manifest string) error {
	envelope, err := sealManifest(ctx, manifest)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE deployment_releases SET manifest='', manifest_envelope=? WHERE id=?`, envelope, id)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

// MigrateLegacyManifests moves snapshots written by older versions out of
// plaintext SQLite columns without changing their release history.
func (s *Store) MigrateLegacyManifests(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id,manifest,manifest_envelope FROM deployment_releases WHERE manifest<>''`)
	if err != nil {
		return err
	}
	type legacy struct{ id, manifest, envelope string }
	var pending []legacy
	for rows.Next() {
		var value legacy
		if err := rows.Scan(&value.id, &value.manifest, &value.envelope); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range pending {
		envelope := item.envelope
		if envelope == "" {
			envelope, err = sealManifest(ctx, item.manifest)
			if err != nil {
				return fmt.Errorf("迁移历史发布快照 %s: %w", item.id, err)
			}
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE deployment_releases SET manifest='',manifest_envelope=? WHERE id=? AND manifest=?`, envelope, item.id, item.manifest); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) GetRelease(ctx context.Context, id string) (Release, error) {
	value, err := scanRelease(s.db.QueryRowContext(ctx, releaseSelect+" WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Release{}, ErrNotFound
	}
	if err != nil {
		return Release{}, fmt.Errorf("get deployment release: %w", err)
	}
	if err := hydrateManifest(ctx, &value); err != nil {
		return Release{}, err
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
		if err := hydrateManifest(ctx, &value); err != nil {
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
	if err := hydrateManifest(ctx, &value); err != nil {
		return Release{}, err
	}
	return value, nil
}

const InterruptedReleaseDetail = "iDeploy 重启前发布任务未完成，请检查远端状态后重新发布"
const PublicationLeaseBusyDetail = "共享服务器上已有发布任务正在执行"

func (s *Store) RecoverInterruptedReleases(ctx context.Context) (int64, error) {
	now := formatTime(time.Now())
	result, err := s.db.ExecContext(ctx, `
		UPDATE deployment_releases
	SET status='failed', detail=?, finished_at=?
		WHERE status NOT IN ('succeeded','failed','canceled')
		AND (job_id IS NULL OR EXISTS (
			SELECT 1 FROM jobs WHERE jobs.id=deployment_releases.job_id
			AND jobs.status IN ('succeeded','failed','canceled')
		))
	`, InterruptedReleaseDetail, now)
	if err != nil {
		return 0, fmt.Errorf("recover interrupted releases: %w", err)
	}
	return result.RowsAffected()
}

// A child that failed to acquire the publication lease never reached the
// target. Other children may already have changed the remote workload.
func (s *Store) InterruptedRemoteReleases(ctx context.Context) ([]Release, error) {
	rows, err := s.db.QueryContext(ctx, releaseSelect+` WHERE build_mode='remote' AND status='failed' AND (remote_job_id<>'' OR (job_id<>'' AND delivery_commit<>''))
		AND detail IN (?,'已从 iDeploy 服务恢复镜像制品')
		AND NOT EXISTS (SELECT 1 FROM jobs WHERE jobs.id=deployment_releases.job_id AND jobs.cancel_requested=1)
		AND NOT EXISTS (SELECT 1 FROM deployment_releases child WHERE child.rollback_to_id=deployment_releases.id
			AND NOT (child.action='deploy' AND child.status='failed' AND child.detail IN (?,?)))
		ORDER BY created_at LIMIT 20`, InterruptedReleaseDetail, PublicationLeaseBusyDetail, "共享服务返回 409：operation already in progress")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var releases []Release
	for rows.Next() {
		value, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		if err := hydrateManifest(ctx, &value); err != nil {
			return nil, err
		}
		releases = append(releases, value)
	}
	return releases, rows.Err()
}

func (s *Store) PendingRemoteCancellations(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT j.id FROM jobs j JOIN deployment_releases r ON r.job_id=j.id
		WHERE j.cancel_requested=1 AND r.build_mode='remote' AND r.delivery_commit<>''
			AND r.remote_cancel_delivered=0
		ORDER BY j.updated_at DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) MarkRemoteCancellationDelivered(ctx context.Context, jobID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE deployment_releases SET remote_cancel_delivered=1 WHERE job_id=?", jobID)
	return err
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
		version, image_ref, source_revision, manifest, manifest_envelope, COALESCE(rollback_to_id,''),
		status, detail, environment_revision, created_at, finished_at, build_mode,
		source_commit, delivery_ref, delivery_commit, delivery_digest, remote_job_id
	FROM deployment_releases
`

func scanRelease(row scanner) (Release, error) {
	var value Release
	var status, createdAt string
	var finishedAt sql.NullString
	if err := row.Scan(&value.ID, &value.DeploymentID, &value.RegistryID, &value.JobID,
		&value.Action, &value.Version, &value.ImageRef, &value.SourceRevision, &value.Manifest, &value.manifestEnvelope,
		&value.RollbackToID, &status, &value.Detail, &value.EnvironmentRevision, &createdAt, &finishedAt,
		&value.BuildMode, &value.SourceCommit, &value.DeliveryRef, &value.DeliveryCommit, &value.DeliveryDigest, &value.RemoteJobID); err != nil {
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

func hydrateManifest(ctx context.Context, value *Release) error {
	if value.manifestEnvelope == "" {
		return nil
	}
	manifest, err := openManifest(ctx, value.manifestEnvelope)
	if err != nil {
		return err
	}
	value.Manifest = manifest
	return nil
}

const targetSelect = `
	SELECT id, project_id, server_id, desired_state, created_at, updated_at, workload_kind, workload_name,
		container_ports_json, restart_policy, environment_revision, applied_environment_revision, build_mode
	FROM deployment_targets
`

type scanner interface{ Scan(...any) error }

func scanTarget(row scanner) (Target, error) {
	var value Target
	var portsJSON string
	var desiredState, createdAt, updatedAt string
	if err := row.Scan(&value.ID, &value.ProjectID, &value.ServerID, &desiredState, &createdAt, &updatedAt, &value.WorkloadKind, &value.WorkloadName, &portsJSON, &value.RestartPolicy, &value.EnvironmentRevision, &value.AppliedEnvironmentRevision, &value.BuildMode); err != nil {
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
