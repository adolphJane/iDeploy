package databasebrowser

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var sharedTableName = regexp.MustCompile(`^shared_[a-z0-9_]+$`)

type sharedTableBackup struct {
	Name      string   `json:"name"`
	CreateSQL string   `json:"createSql"`
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
}

type sharedDatabaseBackup struct {
	Format    string              `json:"format"`
	Database  string              `json:"database"`
	CreatedAt string              `json:"createdAt"`
	Tables    []sharedTableBackup `json:"tables"`
}

// BackupSharedTables captures a consistent, restorable logical snapshot of
// the shared service tables before the service image can migrate its schema.
func BackupSharedTables(ctx context.Context, connection Connection, password, directory string) (string, error) {
	if connection.Driver != DriverMySQL || connection.DatabaseName == "" {
		return "", errors.New("共享服务备份需要指定 MySQL 数据库")
	}
	db, err := open(connection, password)
	if err != nil {
		return "", err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	listed, err := tx.QueryContext(ctx, `SHOW TABLES`)
	if err != nil {
		return "", err
	}
	names := []string{}
	for listed.Next() {
		var name string
		if err := listed.Scan(&name); err != nil {
			listed.Close()
			return "", err
		}
		if strings.HasPrefix(name, "shared_") {
			if !sharedTableName.MatchString(name) {
				listed.Close()
				return "", errors.New("共享服务表名不安全")
			}
			names = append(names, name)
		}
	}
	if err := listed.Err(); err != nil {
		listed.Close()
		return "", err
	}
	listed.Close()
	if len(names) == 0 {
		return "", errors.New("数据库中没有共享服务表，无法确认升级备份")
	}
	sort.Strings(names)
	backup := sharedDatabaseBackup{Format: "ideploy-shared-mysql-v1", Database: connection.DatabaseName,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Tables: []sharedTableBackup{}}
	for _, name := range names {
		var tableName, createSQL string
		if err := tx.QueryRowContext(ctx, "SHOW CREATE TABLE `"+name+"`").Scan(&tableName, &createSQL); err != nil {
			return "", fmt.Errorf("读取 %s 结构: %w", name, err)
		}
		rows, err := tx.QueryContext(ctx, "SELECT * FROM `"+name+"`")
		if err != nil {
			return "", fmt.Errorf("备份 %s: %w", name, err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return "", err
		}
		table := sharedTableBackup{Name: name, CreateSQL: createSQL, Columns: columns, Rows: [][]any{}}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				rows.Close()
				return "", err
			}
			for i, value := range values {
				switch typed := value.(type) {
				case []byte:
					values[i] = map[string]string{"base64": base64.StdEncoding.EncodeToString(typed)}
				case time.Time:
					values[i] = map[string]string{"time": typed.UTC().Format(time.RFC3339Nano)}
				}
			}
			table.Rows = append(table.Rows, values)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return "", err
		}
		rows.Close()
		backup.Tables = append(backup.Tables, table)
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(directory, ".shared-mysql-backup-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err := temp.Chmod(0o600); err != nil {
		return "", err
	}
	if err := json.NewEncoder(temp).Encode(backup); err != nil {
		return "", err
	}
	if err := temp.Sync(); err != nil {
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	final := filepath.Join(directory, "shared-mysql-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".json")
	if err := os.Rename(temp.Name(), final); err != nil {
		return "", err
	}
	return final, nil
}
