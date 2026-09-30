// Package databasebrowser provides read-only metadata and table browsing for
// user-configured remote databases. It is deliberately separate from
// iDeploy's own SQLite persistence.
package databasebrowser

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

type Driver string

const (
	DriverPostgres Driver = "postgres"
	DriverMySQL    Driver = "mysql"
)

type Connection struct {
	ID            string
	Name          string
	Driver        Driver
	Host          string
	Port          int
	DatabaseName  string
	Username      string
	SSLMode       string
	CredentialRef string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastConnected *time.Time
	LastError     string
}

type NewConnection struct {
	Name          string
	Driver        Driver
	Host          string
	Port          int
	DatabaseName  string
	Username      string
	SSLMode       string
	CredentialRef string
}

func (input NewConnection) Validate() (NewConnection, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Host = strings.TrimSpace(input.Host)
	input.DatabaseName = strings.TrimSpace(input.DatabaseName)
	input.Username = strings.TrimSpace(input.Username)
	input.SSLMode = strings.TrimSpace(input.SSLMode)
	if input.Name == "" || len([]rune(input.Name)) > 80 {
		return input, errors.New("数据库连接名称需为 1 到 80 个字符")
	}
	if input.Driver != DriverPostgres && input.Driver != DriverMySQL {
		return input, errors.New("仅支持 PostgreSQL 或 MySQL/MariaDB")
	}
	if input.Host == "" || len(input.Host) > 253 || strings.ContainsAny(input.Host, " \t\r\n/@") || strings.HasPrefix(input.Host, "-") {
		return input, errors.New("数据库主机名或 IP 地址无效")
	}
	if net.ParseIP(input.Host) == nil && strings.Contains(input.Host, ":") {
		return input, errors.New("数据库主机名无效")
	}
	if input.Port == 0 {
		if input.Driver == DriverPostgres {
			input.Port = 5432
		} else {
			input.Port = 3306
		}
	}
	if input.Port < 1 || input.Port > 65535 {
		return input, errors.New("数据库端口必须在 1 到 65535 之间")
	}
	if len(input.DatabaseName) > 128 || strings.ContainsAny(input.DatabaseName, "\r\n\x00") {
		return input, errors.New("数据库名称无效")
	}
	// MySQL can authenticate without selecting a default database. This lets the
	// browser enumerate only the databases available to the supplied account.
	// PostgreSQL needs a database as part of its connection target.
	if input.Driver == DriverPostgres && input.DatabaseName == "" {
		return input, errors.New("PostgreSQL 连接需要指定数据库名称")
	}
	if input.Username == "" || len(input.Username) > 128 || strings.ContainsAny(input.Username, "\r\n\x00") {
		return input, errors.New("数据库用户名无效")
	}
	if input.SSLMode == "" {
		input.SSLMode = "require"
	}
	switch input.SSLMode {
	case "require", "disable":
	default:
		return input, errors.New("TLS 模式仅支持 require 或 disable")
	}
	return input, nil
}

func newID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("生成数据库连接 ID: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

type Schema struct{ Name string }

// Overview contains values that a read-only SQL connection can report. The
// database size is logical data/index storage, not cloud disk capacity.
type Overview struct {
	Version           string
	DatabaseSizeBytes *int64
	SizeDetail        string
}

type Table struct {
	Schema string
	Name   string
	Type   string
}

type Column struct {
	Name         string
	DataType     string
	Nullable     bool
	DefaultValue string
	PrimaryKey   bool
	Comment      string
}

type Index struct {
	Name    string
	Columns []string
	Unique  bool
}

// TableQuery intentionally exposes only bounded browsing controls. It never
// carries caller-provided SQL.
type TableQuery struct {
	Page       int
	PageSize   int
	SortColumn string
	SortDesc   bool
}

func (query TableQuery) normalized(columns []Column) (TableQuery, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 100
	}
	if query.PageSize > 500 {
		query.PageSize = 500
	}
	if len(columns) == 0 {
		return query, errors.New("数据表没有可读取字段")
	}
	if query.SortColumn == "" {
		query.SortColumn = columns[0].Name
		for _, column := range columns {
			if column.PrimaryKey {
				query.SortColumn = column.Name
				break
			}
		}
	}
	for _, column := range columns {
		if query.SortColumn == column.Name {
			return query, nil
		}
	}
	return query, errors.New("排序字段不属于当前数据表")
}

type TablePage struct {
	Columns    []Column
	Rows       []map[string]string
	Page       int
	PageSize   int
	HasNext    bool
	SortColumn string
	SortDesc   bool
}
