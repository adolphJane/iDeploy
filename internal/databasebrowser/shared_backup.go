package databasebrowser

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
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
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	db, err := openWithReadTimeout(connection, password, 60*time.Second)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var backup sharedDatabaseBackup
	for attempt := 1; ; attempt++ {
		backup, err = readSharedTables(ctx, db, connection.DatabaseName)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return "", fmt.Errorf("MySQL 备份已中断: %w", ctx.Err())
		}
		if !isBackupConnectionError(err) {
			return "", err
		}
		if attempt == 2 {
			return "", fmt.Errorf("本机与 MySQL 的连接在备份期间断开，重试后仍失败。请在数据库页面测试连接并检查 VPN 或数据库代理: %w", err)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return writeSharedBackup(ctx, directory, backup)
}

func isBackupConnectionError(err error) bool {
	if errors.Is(err, mysql.ErrInvalidConn) || errors.Is(err, driver.ErrBadConn) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 2006 || mysqlErr.Number == 2013
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func readSharedTables(ctx context.Context, db *sql.DB, databaseName string) (sharedDatabaseBackup, error) {
	var backup sharedDatabaseBackup
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return backup, fmt.Errorf("建立 MySQL 备份事务: %w", err)
	}
	defer tx.Rollback()
	listed, err := tx.QueryContext(ctx, `SHOW TABLES`)
	if err != nil {
		return backup, fmt.Errorf("列出 MySQL 备份表: %w", err)
	}
	names := []string{}
	for listed.Next() {
		var name string
		if err := listed.Scan(&name); err != nil {
			listed.Close()
			return backup, fmt.Errorf("读取 MySQL 备份表名: %w", err)
		}
		if strings.HasPrefix(name, "shared_") || name == "release_jobs" || name == "release_job_requests" {
			if name != "release_jobs" && name != "release_job_requests" && !sharedTableName.MatchString(name) {
				listed.Close()
				return backup, errors.New("共享服务表名不安全")
			}
			names = append(names, name)
		}
	}
	if err := listed.Err(); err != nil {
		listed.Close()
		return backup, fmt.Errorf("读取 MySQL 备份表列表: %w", err)
	}
	listed.Close()
	if len(names) == 0 {
		return backup, errors.New("数据库中没有共享服务表，无法确认升级备份")
	}
	sort.Strings(names)
	backup = sharedDatabaseBackup{Format: "ideploy-shared-mysql-v1", Database: databaseName,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Tables: []sharedTableBackup{}}
	for _, name := range names {
		var tableName, createSQL string
		if err := tx.QueryRowContext(ctx, "SHOW CREATE TABLE `"+name+"`").Scan(&tableName, &createSQL); err != nil {
			return backup, fmt.Errorf("读取 %s 结构: %w", name, err)
		}
		rows, err := tx.QueryContext(ctx, "SELECT * FROM `"+name+"`")
		if err != nil {
			return backup, fmt.Errorf("备份 %s: %w", name, err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return backup, fmt.Errorf("读取 %s 列名: %w", name, err)
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
				return backup, fmt.Errorf("读取 %s 数据: %w", name, err)
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
			return backup, fmt.Errorf("读取 %s 数据: %w", name, err)
		}
		rows.Close()
		backup.Tables = append(backup.Tables, table)
	}
	if err := tx.Commit(); err != nil {
		return backup, fmt.Errorf("提交 MySQL 备份事务: %w", err)
	}
	return backup, nil
}

func writeSharedBackup(ctx context.Context, directory string, backup sharedDatabaseBackup) (string, error) {
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
	if err := ctx.Err(); err != nil {
		return "", err
	}
	final := filepath.Join(directory, "shared-mysql-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".json")
	if err := os.Rename(temp.Name(), final); err != nil {
		return "", err
	}
	return final, nil
}
