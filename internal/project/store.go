package project

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("project not found")

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Create(ctx context.Context, input Input, inspection Inspection) (Project, error) {
	input, err := input.Validate()
	if err != nil {
		return Project{}, err
	}
	id, err := newID()
	if err != nil {
		return Project{}, err
	}
	now := time.Now().UTC()
	status := inspection.Status
	if status == "" {
		status = StatusNeedsConfiguration
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("begin project creation: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects(id, name, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, id, input.Name, input.Description, formatTime(now), formatTime(now)); err != nil {
		return Project{}, fmt.Errorf("create project: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO project_sources(project_id, kind, local_path, repository_url, ref_kind, ref_value, status, last_checked_at, last_error)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, id, input.SourceKind, input.LocalPath, input.RepositoryURL, input.RefKind, input.RefValue, status, checkedAt(inspection), inspection.Detail); err != nil {
		return Project{}, fmt.Errorf("create project source: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO build_configs(project_id, context_path, dockerfile_path, compose_path, image_repository)
        VALUES (?, ?, ?, ?, ?)
    `, id, input.ContextPath, input.DockerfilePath, input.ComposePath, input.ImageRepository); err != nil {
		return Project{}, fmt.Errorf("create build config: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit project creation: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) Get(ctx context.Context, id string) (Project, error) {
	value, err := scanProject(s.db.QueryRowContext(ctx, projectSelect+" WHERE p.id = ? AND p.deleted_at IS NULL", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("get project: %w", err)
	}
	return value, nil
}

func (s *Store) List(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, projectSelect+" WHERE p.deleted_at IS NULL ORDER BY p.name COLLATE NOCASE, p.created_at")
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	values := make([]Project, 0)
	for rows.Next() {
		value, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) Update(ctx context.Context, id string, input Input, inspection Inspection) (Project, error) {
	input, err := input.Validate()
	if err != nil {
		return Project{}, err
	}
	current, err := s.Get(ctx, id)
	if err != nil {
		return Project{}, err
	}
	status := inspection.Status
	if status == "" {
		status = current.Status
	}
	lastError := current.LastError
	lastChecked := nullableTime(current.LastCheckedAt)
	if inspection.Status != "" {
		lastError = inspection.Detail
		lastChecked = formatTime(time.Now())
	}
	now := formatTime(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("begin project update: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE projects SET name = ?, description = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, input.Name, input.Description, now, id)
	if err != nil {
		return Project{}, fmt.Errorf("update project: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return Project{}, err
	}
	if _, err := tx.ExecContext(ctx, `
        UPDATE project_sources SET kind = ?, local_path = ?, repository_url = ?, ref_kind = ?, ref_value = ?,
            status = ?, last_checked_at = ?, last_error = ? WHERE project_id = ?
    `, input.SourceKind, input.LocalPath, input.RepositoryURL, input.RefKind, input.RefValue, status, lastChecked, lastError, id); err != nil {
		return Project{}, fmt.Errorf("update project source: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
        UPDATE build_configs SET context_path = ?, dockerfile_path = ?, compose_path = ?, image_repository = ? WHERE project_id = ?
    `, input.ContextPath, input.DockerfilePath, input.ComposePath, input.ImageRepository, id); err != nil {
		return Project{}, fmt.Errorf("update build config: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit project update: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) UpdateInspection(ctx context.Context, id string, inspection Inspection) (Project, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return Project{}, err
	}
	now := formatTime(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("begin project inspection update: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
        UPDATE project_sources SET status = ?, last_checked_at = ?, last_error = ? WHERE project_id = ?
    `, inspection.Status, now, inspection.Detail, id); err != nil {
		return Project{}, fmt.Errorf("update project inspection: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
        UPDATE build_configs
        SET dockerfile_path = CASE WHEN dockerfile_path = '' THEN ? ELSE dockerfile_path END,
            compose_path = CASE WHEN compose_path = '' THEN ? ELSE compose_path END
        WHERE project_id = ?
    `, inspection.DockerfilePath, inspection.ComposePath, id); err != nil {
		return Project{}, fmt.Errorf("update detected configuration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit project inspection update: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE projects SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM deployment_targets WHERE project_id=projects.id) AND NOT EXISTS (SELECT 1 FROM jobs WHERE lock_key='project:' || projects.id AND status IN ('queued','running','canceling'))`, formatTime(time.Now()), formatTime(time.Now()), id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return errors.New("项目不存在、仍有部署关联或任务未完成，请先移除关联并等待任务结束")
	}
	return nil
}

const projectSelect = `
    SELECT p.id, p.name, p.description, s.kind, s.local_path, s.repository_url, s.ref_kind, s.ref_value,
           s.resolved_commit, s.status, s.last_checked_at, s.last_error,
           b.context_path, b.dockerfile_path, b.compose_path, b.image_repository, p.created_at, p.updated_at
    FROM projects p
    JOIN project_sources s ON s.project_id = p.id
    JOIN build_configs b ON b.project_id = p.id
`

type scanner interface{ Scan(...any) error }

func scanProject(row scanner) (Project, error) {
	var value Project
	var kind, refKind, status string
	var checkedAt sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&value.ID, &value.Name, &value.Description, &kind, &value.LocalPath, &value.RepositoryURL,
		&refKind, &value.RefValue, &value.ResolvedCommit, &status, &checkedAt, &value.LastError,
		&value.ContextPath, &value.DockerfilePath, &value.ComposePath, &value.ImageRepository, &createdAt, &updatedAt); err != nil {
		return Project{}, err
	}
	value.SourceKind, value.RefKind, value.Status = SourceKind(kind), RefKind(refKind), Status(status)
	var err error
	if value.CreatedAt, err = parseTime(createdAt); err != nil {
		return Project{}, err
	}
	if value.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Project{}, err
	}
	if checkedAt.Valid {
		checked, err := parseTime(checkedAt.String)
		if err != nil {
			return Project{}, err
		}
		value.LastCheckedAt = &checked
	}
	return value, nil
}

func checkedAt(inspection Inspection) any {
	if inspection.Status == "" {
		return nil
	}
	return formatTime(time.Now())
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
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
