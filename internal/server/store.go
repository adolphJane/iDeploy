package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("server connection not found")

type Store struct{ db *sql.DB }

type ClearAllResult struct {
	Servers        int64
	Deployments    int64
	Snapshots      int64
	Jobs           int64
	CredentialRefs []string
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Create(ctx context.Context, input NewConnection) (Connection, error) {
	input, err := input.Validate()
	if err != nil {
		return Connection{}, err
	}
	id, err := newID()
	if err != nil {
		return Connection{}, err
	}
	now := formatTime(time.Now())
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO servers(id, name, host, port, ssh_user, auth_method, private_key_path, credential_ref, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, id, input.Name, input.Host, input.Port, input.SSHUser, input.AuthMethod, input.PrivateKeyPath, input.CredentialRef, StatusUnknown, now, now)
	if err != nil {
		return Connection{}, fmt.Errorf("create server: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) UpdateCredentialRef(ctx context.Context, id, credentialRef string) (Connection, error) {
	now := formatTime(time.Now())
	result, err := s.db.ExecContext(ctx, `UPDATE servers SET credential_ref = ?, updated_at = ? WHERE id = ?`, credentialRef, now, id)
	if err != nil {
		return Connection{}, fmt.Errorf("update server credential reference: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return Connection{}, err
	}
	return s.Get(ctx, id)
}

func (s *Store) UpdateConnection(ctx context.Context, id string, input NewConnection) (Connection, error) {
	input, err := input.Validate()
	if err != nil {
		return Connection{}, err
	}
	now := formatTime(time.Now())
	result, err := s.db.ExecContext(ctx, `
		UPDATE servers
		SET name = ?, host = ?, port = ?, ssh_user = ?, auth_method = ?, private_key_path = ?,
			credential_ref = ?, status = ?, last_checked_at = NULL, last_message = '', last_error = '', updated_at = ?
		WHERE id = ?
	`, input.Name, input.Host, input.Port, input.SSHUser, input.AuthMethod, input.PrivateKeyPath,
		input.CredentialRef, StatusUnknown, now, id)
	if err != nil {
		return Connection{}, fmt.Errorf("update server connection: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return Connection{}, err
	}
	return s.Get(ctx, id)
}

func (s *Store) Get(ctx context.Context, id string) (Connection, error) {
	value, err := scanConnection(s.db.QueryRowContext(ctx, serverSelect+" WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	if err != nil {
		return Connection{}, fmt.Errorf("get server: %w", err)
	}
	return value, nil
}

func (s *Store) List(ctx context.Context) ([]Connection, error) {
	rows, err := s.db.QueryContext(ctx, serverSelect+" ORDER BY name COLLATE NOCASE, created_at")
	if err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}
	defer rows.Close()
	values := make([]Connection, 0)
	for rows.Next() {
		value, err := scanConnection(rows)
		if err != nil {
			return nil, fmt.Errorf("scan server: %w", err)
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM servers WHERE id = ? AND NOT EXISTS (SELECT 1 FROM deployment_targets WHERE server_id=servers.id) AND NOT EXISTS (SELECT 1 FROM jobs WHERE lock_key='server:' || servers.id AND status IN ('queued','running','canceling'))", id)
	if err != nil {
		return fmt.Errorf("delete server: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return errors.New("服务器不存在、仍有部署关联或任务未完成，请先移除关联并等待任务结束")
	}
	return nil
}

// ClearAll removes every local record owned by the server subsystem in one
// transaction. It refuses to run while a server-scoped task is unfinished.
// Keychain items are returned to the application layer for best-effort cleanup
// after the database transaction commits.
func (s *Store) ClearAll(ctx context.Context) (ClearAllResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ClearAllResult{}, fmt.Errorf("begin clearing server data: %w", err)
	}
	defer tx.Rollback()

	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE lock_key LIKE 'server:%' AND status IN ('queued','running','canceling')`).Scan(&active); err != nil {
		return ClearAllResult{}, fmt.Errorf("check active server jobs: %w", err)
	}
	if active > 0 {
		return ClearAllResult{}, errors.New("仍有服务器任务正在执行，请等待任务完成后再清除")
	}

	result := ClearAllResult{CredentialRefs: make([]string, 0)}
	counts := []struct {
		query string
		dest  *int64
	}{
		{query: "SELECT COUNT(*) FROM servers", dest: &result.Servers},
		{query: "SELECT COUNT(*) FROM deployment_targets", dest: &result.Deployments},
		{query: "SELECT COUNT(*) FROM remote_snapshots", dest: &result.Snapshots},
		{query: "SELECT COUNT(*) FROM jobs WHERE lock_key LIKE 'server:%'", dest: &result.Jobs},
	}
	for _, count := range counts {
		if err := tx.QueryRowContext(ctx, count.query).Scan(count.dest); err != nil {
			return ClearAllResult{}, fmt.Errorf("count server data: %w", err)
		}
	}

	rows, err := tx.QueryContext(ctx, "SELECT credential_ref FROM servers WHERE credential_ref <> ''")
	if err != nil {
		return ClearAllResult{}, fmt.Errorf("list server credential references: %w", err)
	}
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			rows.Close()
			return ClearAllResult{}, fmt.Errorf("scan server credential reference: %w", err)
		}
		result.CredentialRefs = append(result.CredentialRefs, ref)
	}
	if err := rows.Close(); err != nil {
		return ClearAllResult{}, err
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM jobs WHERE lock_key LIKE 'server:%'"); err != nil {
		return ClearAllResult{}, fmt.Errorf("clear server jobs: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM servers"); err != nil {
		return ClearAllResult{}, fmt.Errorf("clear servers: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ClearAllResult{}, fmt.Errorf("commit clearing server data: %w", err)
	}
	return result, nil
}

func (s *Store) UpdateProbe(ctx context.Context, id string, status Status, detail string) (Connection, error) {
	if !validStatus(status) {
		return Connection{}, fmt.Errorf("invalid server status %q", status)
	}
	now := formatTime(time.Now())
	lastError := ""
	if status == StatusDegraded || status == StatusUnreachable || status == StatusAuthRequired {
		lastError = detail
	}
	result, err := s.db.ExecContext(ctx, `
        UPDATE servers SET status = ?, last_checked_at = ?, last_message = ?, last_error = ?, updated_at = ?
        WHERE id = ?
    `, status, now, detail, lastError, now, id)
	if err != nil {
		return Connection{}, fmt.Errorf("update server probe: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return Connection{}, err
	}
	return s.Get(ctx, id)
}

func validStatus(status Status) bool {
	switch status {
	case StatusUnknown, StatusChecking, StatusHealthy, StatusAuthRequired, StatusDegraded, StatusUnreachable:
		return true
	default:
		return false
	}
}

const serverSelect = `
    SELECT id, name, host, port, ssh_user, auth_method, private_key_path, credential_ref, status, last_checked_at,
           last_message, last_error, created_at, updated_at
    FROM servers
`

type scanner interface{ Scan(...any) error }

func scanConnection(row scanner) (Connection, error) {
	var value Connection
	var status string
	var checkedAt sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&value.ID, &value.Name, &value.Host, &value.Port, &value.SSHUser, &value.AuthMethod, &value.PrivateKeyPath, &value.CredentialRef,
		&status, &checkedAt, &value.LastMessage, &value.LastError, &createdAt, &updatedAt); err != nil {
		return Connection{}, err
	}
	value.Status = Status(status)
	var err error
	if value.CreatedAt, err = parseTime(createdAt); err != nil {
		return Connection{}, err
	}
	if value.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Connection{}, err
	}
	if checkedAt.Valid {
		checked, err := parseTime(checkedAt.String)
		if err != nil {
			return Connection{}, err
		}
		value.LastCheckedAt = &checked
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

func formatTime(value time.Time) string         { return value.UTC().Format(time.RFC3339Nano) }
func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }
