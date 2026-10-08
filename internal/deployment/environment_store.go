package deployment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *Store) ListEnvironment(ctx context.Context, deploymentID string) ([]EnvironmentVariable, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id,deployment_id,name,kind,plain_value,credential_ref,created_at,updated_at
		FROM deployment_environment_variables WHERE deployment_id=? ORDER BY name
	`, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("list deployment environment: %w", err)
	}
	defer rows.Close()
	values := make([]EnvironmentVariable, 0)
	for rows.Next() {
		value, err := scanEnvironmentVariable(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// ApplyEnvironmentChanges updates metadata and plain values in one transaction.
// Secret values are saved to Keychain by the application layer before calling
// this method. Returned references are no longer reachable and can be removed.
func (s *Store) ApplyEnvironmentChanges(ctx context.Context, deploymentID string, changes []EnvironmentChange, remove []string) ([]EnvironmentVariable, []string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	var revision int64
	if err := tx.QueryRowContext(ctx, "SELECT environment_revision FROM deployment_targets WHERE id=?", deploymentID).Scan(&revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}

	existing, err := listEnvironmentTx(ctx, tx, deploymentID)
	if err != nil {
		return nil, nil, err
	}
	byName := make(map[string]EnvironmentVariable, len(existing))
	for _, value := range existing {
		byName[value.Name] = value
	}

	changed := false
	cleanup := make([]string, 0)
	now := formatTime(time.Now())
	seen := map[string]struct{}{}
	for _, change := range changes {
		name, err := ValidateEnvironmentName(change.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", change.Name, err)
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, nil, fmt.Errorf("环境变量 %s 重复", name)
		}
		seen[name] = struct{}{}
		if change.Kind != EnvironmentKindPlain && change.Kind != EnvironmentKindSecret {
			return nil, nil, fmt.Errorf("环境变量 %s 类型无效", name)
		}
		if strings.ContainsAny(change.PlainValue, "\r\n") || len(change.PlainValue) > 65536 {
			return nil, nil, fmt.Errorf("环境变量 %s 的值无效或过长", name)
		}
		if change.Kind == EnvironmentKindSecret && change.CredentialRef == "" {
			return nil, nil, fmt.Errorf("秘密变量 %s 尚未安全保存", name)
		}
		if change.Kind == EnvironmentKindPlain {
			change.CredentialRef = ""
		}
		current, found := byName[name]
		if found && current.Kind == change.Kind && current.PlainValue == change.PlainValue && current.CredentialRef == change.CredentialRef {
			continue
		}
		id := change.ID
		createdAt := now
		if found {
			id = current.ID
			createdAt = formatTime(current.CreatedAt)
			if current.CredentialRef != "" && current.CredentialRef != change.CredentialRef {
				cleanup = append(cleanup, current.CredentialRef)
			}
		}
		if id == "" {
			id, err = newID()
			if err != nil {
				return nil, nil, err
			}
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO deployment_environment_variables(id,deployment_id,name,kind,plain_value,credential_ref,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?)
			ON CONFLICT(deployment_id,name) DO UPDATE SET
				kind=excluded.kind,plain_value=excluded.plain_value,credential_ref=excluded.credential_ref,updated_at=excluded.updated_at
		`, id, deploymentID, name, change.Kind, change.PlainValue, change.CredentialRef, createdAt, now)
		if err != nil {
			return nil, nil, fmt.Errorf("save deployment environment variable %s: %w", name, err)
		}
		changed = true
	}

	for _, rawName := range remove {
		name, err := ValidateEnvironmentName(rawName)
		if err != nil {
			return nil, nil, err
		}
		current, found := byName[name]
		if !found {
			continue
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM deployment_environment_variables WHERE deployment_id=? AND name=?", deploymentID, name); err != nil {
			return nil, nil, err
		}
		if current.CredentialRef != "" {
			cleanup = append(cleanup, current.CredentialRef)
		}
		changed = true
	}
	if changed {
		if _, err := tx.ExecContext(ctx, "UPDATE deployment_targets SET environment_revision=environment_revision+1,updated_at=? WHERE id=?", now, deploymentID); err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	values, err := s.ListEnvironment(ctx, deploymentID)
	return values, cleanup, err
}

func (s *Store) MarkEnvironmentApplied(ctx context.Context, deploymentID string, revision int64) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE deployment_targets SET applied_environment_revision=?,updated_at=?
		WHERE id=?
	`, revision, formatTime(time.Now()), deploymentID)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) SetReleaseEnvironmentRevision(ctx context.Context, releaseID string, revision int64) error {
	result, err := s.db.ExecContext(ctx, "UPDATE deployment_releases SET environment_revision=? WHERE id=?", revision, releaseID)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) EnvironmentCredentialRefs(ctx context.Context, deploymentID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT credential_ref FROM deployment_environment_variables WHERE deployment_id=? AND credential_ref<>''", deploymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (s *Store) ProjectEnvironmentCredentialRefs(ctx context.Context, projectID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT environment.credential_ref
		FROM deployment_environment_variables environment
		JOIN deployment_targets targets ON targets.id=environment.deployment_id
		WHERE targets.project_id=? AND environment.credential_ref<>''
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (s *Store) ServerEnvironmentCredentialRefs(ctx context.Context, serverID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT environment.credential_ref
		FROM deployment_environment_variables environment
		JOIN deployment_targets targets ON targets.id=environment.deployment_id
		WHERE targets.server_id=? AND environment.credential_ref<>''
	`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (s *Store) AllEnvironmentCredentialRefs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT credential_ref FROM deployment_environment_variables WHERE credential_ref<>''")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func listEnvironmentTx(ctx context.Context, tx *sql.Tx, deploymentID string) ([]EnvironmentVariable, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,deployment_id,name,kind,plain_value,credential_ref,created_at,updated_at FROM deployment_environment_variables WHERE deployment_id=?`, deploymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []EnvironmentVariable
	for rows.Next() {
		value, err := scanEnvironmentVariable(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Name < values[j].Name })
	return values, rows.Err()
}

func scanEnvironmentVariable(row scanner) (EnvironmentVariable, error) {
	var value EnvironmentVariable
	var createdAt, updatedAt string
	if err := row.Scan(&value.ID, &value.DeploymentID, &value.Name, &value.Kind, &value.PlainValue, &value.CredentialRef, &createdAt, &updatedAt); err != nil {
		return EnvironmentVariable{}, err
	}
	var err error
	if value.CreatedAt, err = parseTime(createdAt); err != nil {
		return EnvironmentVariable{}, err
	}
	if value.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return EnvironmentVariable{}, err
	}
	return value, nil
}
