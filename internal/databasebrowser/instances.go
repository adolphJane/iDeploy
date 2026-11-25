package databasebrowser

import (
	"context"
	"database/sql"
	"errors"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

type InstanceConfig struct {
	Name          string `json:"name"`
	Environment   string `json:"environment"`
	ServerID      string `json:"serverId"`
	Driver        Driver `json:"driver"`
	EngineVersion string `json:"engineVersion"`
	ListenIP      string `json:"listenIp"`
	Port          int    `json:"port"`
	DatabaseName  string `json:"databaseName"`
	AppUsername   string `json:"appUsername"`
}

type Instance struct {
	InstanceConfig
	ID        string `json:"id"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

type Backup struct {
	ID         string `json:"id"`
	InstanceID string `json:"instanceId"`
	CreatedAt  string `json:"createdAt"`
}

var instanceDatabaseName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

func (input InstanceConfig) Validate() (InstanceConfig, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.ServerID = strings.TrimSpace(input.ServerID)
	if input.Name == "" || len([]rune(input.Name)) > 80 || input.ServerID == "" {
		return input, errors.New("请填写实例名称并选择数据库服务器")
	}
	if input.Environment != "test" && input.Environment != "production" {
		return input, errors.New("请选择测试或线上环境")
	}
	if input.Driver != DriverPostgres && input.Driver != DriverMySQL {
		return input, errors.New("请选择 PostgreSQL 或 MySQL")
	}
	if input.Driver == DriverPostgres && input.EngineVersion != "17" || input.Driver == DriverMySQL && input.EngineVersion != "8.4" {
		return input, errors.New("请选择 PostgreSQL 17 或 MySQL 8.4")
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(input.ListenIP))
	if err != nil || !ip.Is4() || !ip.IsPrivate() && !ip.IsLoopback() {
		return input, errors.New("数据库监听地址必须是服务器的私网 IPv4 地址或回环地址")
	}
	input.ListenIP = ip.String()
	if input.Port < 1 || input.Port > 65535 {
		return input, errors.New("数据库端口必须在 1 到 65535 之间")
	}
	input.DatabaseName = strings.TrimSpace(input.DatabaseName)
	if !instanceDatabaseName.MatchString(input.DatabaseName) {
		return input, errors.New("库名须以小写字母开头，最多 63 位，只允许小写字母、数字和下划线")
	}
	input.AppUsername = strings.TrimSpace(input.AppUsername)
	if input.AppUsername == "" {
		input.AppUsername = "app"
	}
	if !instanceDatabaseName.MatchString(input.AppUsername) || len(input.AppUsername) > 32 ||
		input.AppUsername == "root" || input.AppUsername == "postgres" || strings.HasPrefix(input.AppUsername, "pg_") {
		return input, errors.New("应用账号须以小写字母开头，最多 32 位，只允许小写字母、数字和下划线")
	}
	switch input.DatabaseName {
	case "postgres", "template0", "template1", "mysql", "sys", "information_schema", "performance_schema":
		return input, errors.New("请选择业务库名")
	}
	return input, nil
}

func (s *Store) CreateInstance(ctx context.Context, input InstanceConfig) (Instance, error) {
	input, err := input.Validate()
	if err != nil {
		return Instance{}, err
	}
	id, err := newID()
	if err != nil {
		return Instance{}, err
	}
	now := formatTime(time.Now())
	_, err = s.db.ExecContext(ctx, `INSERT INTO managed_database_instances
		(id,name,environment,server_id,driver,engine_version,listen_ip,port,database_name,app_username,created_at)
		SELECT ?,?,?,?,?,?,?,?,?,?,?
		WHERE NOT EXISTS (
			SELECT 1 FROM managed_database_instances d
			JOIN servers current ON current.id=d.server_id
			JOIN servers requested ON requested.id=?
			WHERE d.environment<>? AND lower(rtrim(current.host,'.'))=lower(rtrim(requested.host,'.')) AND current.port=requested.port
		)`, id, input.Name, input.Environment, input.ServerID, input.Driver, input.EngineVersion, input.ListenIP, input.Port, input.DatabaseName, input.AppUsername, now, input.ServerID, input.Environment)
	if err != nil {
		return Instance{}, err
	}
	instance, err := s.GetInstance(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return Instance{}, errors.New("测试和线上数据库实例须部署到不同服务器")
	}
	return instance, err
}

func scanInstance(row scanner) (Instance, error) {
	var instance Instance
	err := row.Scan(&instance.ID, &instance.Name, &instance.Environment, &instance.ServerID, &instance.Driver, &instance.EngineVersion,
		&instance.ListenIP, &instance.Port, &instance.DatabaseName, &instance.AppUsername, &instance.Status, &instance.CreatedAt)
	return instance, err
}

const instanceSelect = `SELECT id,name,environment,server_id,driver,engine_version,listen_ip,port,database_name,app_username,status,created_at FROM managed_database_instances`

func (s *Store) GetInstance(ctx context.Context, id string) (Instance, error) {
	instance, err := scanInstance(s.db.QueryRowContext(ctx, instanceSelect+" WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Instance{}, ErrNotFound
	}
	return instance, err
}

func (s *Store) ListInstances(ctx context.Context) ([]Instance, error) {
	rows, err := s.db.QueryContext(ctx, instanceSelect+" ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	instances := []Instance{}
	for rows.Next() {
		instance, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	return instances, rows.Err()
}

func (s *Store) SetInstanceStatus(ctx context.Context, id, status string) error {
	result, err := s.db.ExecContext(ctx, "UPDATE managed_database_instances SET status=? WHERE id=?", status, id)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) DeleteInstanceConfiguration(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM managed_database_instances WHERE id=? AND status NOT IN ('deploying','restoring')
		AND NOT EXISTS(SELECT 1 FROM jobs WHERE type='database.instance' AND json_extract(payload,'$.instanceId')=managed_database_instances.id AND status IN ('queued','running','canceling'))`, id)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) ReconcileInterruptedInstances(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "UPDATE managed_database_instances SET status=CASE status WHEN 'restoring' THEN 'restore_failed' ELSE 'failed' END WHERE status IN ('deploying','restoring')")
	return err
}

func (s *Store) RecordBackup(ctx context.Context, id, instanceID string) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO managed_database_backups(id,instance_id,created_at) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING", id, instanceID, formatTime(time.Now()))
	return err
}

func (s *Store) GetBackup(ctx context.Context, id string) (Backup, error) {
	var backup Backup
	err := s.db.QueryRowContext(ctx, "SELECT id,instance_id,created_at FROM managed_database_backups WHERE id=?", id).
		Scan(&backup.ID, &backup.InstanceID, &backup.CreatedAt)
	return backup, err
}

func (s *Store) ListBackups(ctx context.Context, id string) ([]Backup, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,instance_id,created_at FROM managed_database_backups WHERE instance_id=? ORDER BY created_at DESC", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	backups := []Backup{}
	for rows.Next() {
		var backup Backup
		if err := rows.Scan(&backup.ID, &backup.InstanceID, &backup.CreatedAt); err != nil {
			return nil, err
		}
		backups = append(backups, backup)
	}
	return backups, rows.Err()
}
