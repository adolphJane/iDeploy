package registry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("registry connection not found")

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, input NewConnection) (Connection, error) {
	input, err := input.Validate()
	if err != nil {
		return Connection{}, err
	}
	id, err := newID()
	if err != nil {
		return Connection{}, err
	}
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO registry_connections(
			id, name, kind, endpoint, username, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, input.Name, input.Kind, input.Endpoint, input.Username, StatusUnknown, formatTime(now), formatTime(now))
	if err != nil {
		return Connection{}, fmt.Errorf("create registry connection: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) UpdateCredential(ctx context.Context, id, username, credentialRef string) (Connection, error) {
	result, err := s.db.ExecContext(ctx, "UPDATE registry_connections SET username=?, credential_ref=?, updated_at=? WHERE id=?", username, credentialRef, formatTime(time.Now()), id)
	if err != nil {
		return Connection{}, fmt.Errorf("update registry credential: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return Connection{}, err
	}
	return s.Get(ctx, id)
}

func (s *Store) Update(ctx context.Context, id string, input NewConnection, credentialRef string) (Connection, error) {
	input, err := input.Validate()
	if err != nil {
		return Connection{}, err
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE registry_connections
		SET name=?, kind=?, endpoint=?, username=?, credential_ref=?, status=?,
			last_checked_at=NULL, last_message='', last_error='', updated_at=?
		WHERE id=?
	`, input.Name, input.Kind, input.Endpoint, input.Username, credentialRef, StatusUnknown, formatTime(time.Now()), id)
	if err != nil {
		return Connection{}, fmt.Errorf("update registry connection: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return Connection{}, err
	}
	return s.Get(ctx, id)
}

func (s *Store) Get(ctx context.Context, id string) (Connection, error) {
	connection, err := scanConnection(s.db.QueryRowContext(ctx, connectionSelect+" WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	if err != nil {
		return Connection{}, fmt.Errorf("get registry connection: %w", err)
	}
	return connection, nil
}

func (s *Store) List(ctx context.Context) ([]Connection, error) {
	rows, err := s.db.QueryContext(ctx, connectionSelect+" ORDER BY name COLLATE NOCASE, created_at")
	if err != nil {
		return nil, fmt.Errorf("list registry connections: %w", err)
	}
	defer rows.Close()

	connections := make([]Connection, 0)
	for rows.Next() {
		connection, err := scanConnection(rows)
		if err != nil {
			return nil, fmt.Errorf("scan registry connection: %w", err)
		}
		connections = append(connections, connection)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate registry connections: %w", err)
	}
	return connections, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM registry_connections WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete registry connection: %w", err)
	}
	return requireAffected(result)
}

func (s *Store) UpdateProbe(ctx context.Context, id string, status Status, detail string) (Connection, error) {
	if !validStatus(status) {
		return Connection{}, fmt.Errorf("invalid registry status %q", status)
	}
	now := time.Now().UTC()
	errorDetail := ""
	if status == StatusUnreachable || status == StatusDegraded {
		errorDetail = detail
	}
	result, err := s.db.ExecContext(ctx, `
        UPDATE registry_connections
        SET status = ?, last_checked_at = ?, last_message = ?, last_error = ?, updated_at = ?
        WHERE id = ?
	`, status, formatTime(now), detail, errorDetail, formatTime(now), id)
	if err != nil {
		return Connection{}, fmt.Errorf("update registry probe: %w", err)
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

const connectionSelect = `
	SELECT id, name, kind, endpoint, credential_ref, username, status, last_checked_at,
           last_message, last_error, created_at, updated_at
    FROM registry_connections
`

type scanner interface {
	Scan(dest ...any) error
}

func scanConnection(row scanner) (Connection, error) {
	var connection Connection
	var kind, status string
	var lastChecked sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(
		&connection.ID, &connection.Name, &kind, &connection.Endpoint,
		&connection.CredentialRef, &connection.Username, &status, &lastChecked, &connection.LastMessage, &connection.LastError,
		&createdAt, &updatedAt,
	); err != nil {
		return Connection{}, err
	}
	connection.Kind = Kind(kind)
	connection.Status = Status(status)
	var err error
	if connection.CreatedAt, err = parseTime(createdAt); err != nil {
		return Connection{}, err
	}
	if connection.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Connection{}, err
	}
	if lastChecked.Valid {
		parsed, err := parseTime(lastChecked.String)
		if err != nil {
			return Connection{}, err
		}
		connection.LastCheckedAt = &parsed
	}
	return connection, nil
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
