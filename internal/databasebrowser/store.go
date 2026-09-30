package databasebrowser

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"
)

var ErrNotFound = errors.New("数据库连接不存在")
var passwordInError = regexp.MustCompile(`(?i)(password|passwd)=([^\s&]+)`)

type Store struct{ db *sql.DB }

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
	if _, err := s.db.ExecContext(ctx, `INSERT INTO database_connections(id,name,driver,host,port,database_name,username,ssl_mode,credential_ref,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, input.Name, input.Driver, input.Host, input.Port, input.DatabaseName, input.Username, input.SSLMode, input.CredentialRef, now, now); err != nil {
		return Connection{}, fmt.Errorf("创建数据库连接: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) Update(ctx context.Context, id string, input NewConnection) (Connection, error) {
	input, err := input.Validate()
	if err != nil {
		return Connection{}, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE database_connections SET name=?,driver=?,host=?,port=?,database_name=?,username=?,ssl_mode=?,credential_ref=?,last_error='',updated_at=? WHERE id=?`, input.Name, input.Driver, input.Host, input.Port, input.DatabaseName, input.Username, input.SSLMode, input.CredentialRef, formatTime(time.Now()), id)
	if err != nil {
		return Connection{}, fmt.Errorf("更新数据库连接: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return Connection{}, err
	}
	return s.Get(ctx, id)
}

func (s *Store) Get(ctx context.Context, id string) (Connection, error) {
	value, err := scanConnection(s.db.QueryRowContext(ctx, connectionSelect+" WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	if err != nil {
		return Connection{}, fmt.Errorf("读取数据库连接: %w", err)
	}
	return value, nil
}

func (s *Store) List(ctx context.Context) ([]Connection, error) {
	rows, err := s.db.QueryContext(ctx, connectionSelect+" ORDER BY name COLLATE NOCASE, created_at")
	if err != nil {
		return nil, fmt.Errorf("列出数据库连接: %w", err)
	}
	defer rows.Close()
	values := make([]Connection, 0)
	for rows.Next() {
		value, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) Delete(ctx context.Context, id string) (string, error) {
	value, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	result, err := s.db.ExecContext(ctx, "DELETE FROM database_connections WHERE id=?", id)
	if err != nil {
		return "", fmt.Errorf("删除数据库连接: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return "", err
	}
	return value.CredentialRef, nil
}

func (s *Store) RecordConnection(ctx context.Context, id string, connectionErr error) error {
	now := formatTime(time.Now())
	lastError := ""
	lastConnected := any(now)
	if connectionErr != nil {
		lastError = passwordInError.ReplaceAllString(connectionErr.Error(), "$1=***")
		if len([]rune(lastError)) > 500 {
			lastError = string([]rune(lastError)[:500]) + "…"
		}
		lastConnected = nil
	}
	_, err := s.db.ExecContext(ctx, "UPDATE database_connections SET last_connected_at=?,last_error=?,updated_at=? WHERE id=?", lastConnected, lastError, now, id)
	if err != nil {
		return fmt.Errorf("记录数据库连接状态: %w", err)
	}
	return nil
}

const connectionSelect = `SELECT id,name,driver,host,port,database_name,username,ssl_mode,credential_ref,created_at,updated_at,last_connected_at,last_error FROM database_connections`

type scanner interface{ Scan(...any) error }

func scanConnection(row scanner) (Connection, error) {
	var value Connection
	var driver string
	var created, updated string
	var connected sql.NullString
	err := row.Scan(&value.ID, &value.Name, &driver, &value.Host, &value.Port, &value.DatabaseName, &value.Username, &value.SSLMode, &value.CredentialRef, &created, &updated, &connected, &value.LastError)
	if err != nil {
		return Connection{}, err
	}
	value.Driver = Driver(driver)
	var parseErr error
	if value.CreatedAt, parseErr = parseTime(created); parseErr != nil {
		return Connection{}, parseErr
	}
	if value.UpdatedAt, parseErr = parseTime(updated); parseErr != nil {
		return Connection{}, parseErr
	}
	if connected.Valid {
		parsed, err := parseTime(connected.String)
		if err != nil {
			return Connection{}, err
		}
		value.LastConnected = &parsed
	}
	return value, nil
}
func requireAffected(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}
func formatTime(value time.Time) string         { return value.UTC().Format(time.RFC3339Nano) }
func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }
