package databasebrowser

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Test verifies network reachability and authentication without reading user
// tables. Callers keep the password outside persisted connection metadata.
func Test(ctx context.Context, input NewConnection, password string) error {
	input, err := input.Validate()
	if err != nil {
		return err
	}
	if password == "" {
		return fmt.Errorf("数据库密码不能为空")
	}
	connection := Connection{Driver: input.Driver, Host: input.Host, Port: input.Port, DatabaseName: input.DatabaseName, Username: input.Username, SSLMode: input.SSLMode}
	networkDetail, err := diagnoseNetwork(ctx, connection.Host, connection.Port)
	if err != nil {
		return err
	}
	db, err := open(connection, password)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("数据库握手或认证失败（%s）: %w", networkDetail, err)
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("创建只读会话: %w", err)
	}
	defer tx.Rollback()
	var value int
	if err := tx.QueryRowContext(ctx, "SELECT 1").Scan(&value); err != nil {
		return fmt.Errorf("验证只读会话: %w", err)
	}
	return tx.Commit()
}

// diagnoseNetwork makes a bounded DNS and TCP check before a driver starts a
// protocol handshake. The result is intentionally password-free so it can be
// safely shown to the user and written to application logs.
func diagnoseNetwork(parent context.Context, host string, port int) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return "", fmt.Errorf("DNS 解析失败：无法解析 %q；请检查主机名、VPN 或 DNS 设置：%w", host, err)
	}
	if len(addresses) > 4 {
		addresses = addresses[:4]
	}
	endpoint := net.JoinHostPort(host, strconv.Itoa(port))
	started := time.Now()
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", endpoint)
	if err != nil {
		var networkErr net.Error
		if errors.As(err, &networkErr) && networkErr.Timeout() {
			return "", fmt.Errorf("TCP 连接 %s 超时（DNS: %s）；数据库端口可能只允许内网/VPN，或被安全组、防火墙拦截", endpoint, strings.Join(addresses, ", "))
		}
		return "", fmt.Errorf("TCP 连接 %s 失败（DNS: %s）：%w", endpoint, strings.Join(addresses, ", "), err)
	}
	_ = connection.Close()
	return fmt.Sprintf("DNS: %s；TCP 已连通，耗时 %s", strings.Join(addresses, ", "), time.Since(started).Round(time.Millisecond)), nil
}

func ListSchemas(ctx context.Context, connection Connection, password string) ([]Schema, error) {
	result := make([]Schema, 0)
	err := withReadOnly(ctx, connection, password, func(tx *sql.Tx) error {
		query := `SELECT schema_name FROM information_schema.schemata WHERE schema_name NOT IN ('information_schema','pg_catalog','mysql','performance_schema','sys') ORDER BY schema_name`
		if connection.Driver == DriverMySQL {
			query = `SELECT schema_name FROM information_schema.schemata WHERE schema_name NOT IN ('information_schema','mysql','performance_schema','sys') ORDER BY schema_name`
		}
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var schema Schema
			if err := rows.Scan(&schema.Name); err != nil {
				return err
			}
			result = append(result, schema)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("读取数据库架构: %w", err)
	}
	return result, nil
}

// GetOverview reads only server metadata. A size permission failure does not
// hide the version; callers can show why that individual metric is unavailable.
func GetOverview(ctx context.Context, connection Connection, password, schema string) (Overview, error) {
	var result Overview
	err := withReadOnly(ctx, connection, password, func(tx *sql.Tx) error {
		versionQuery := "SELECT VERSION()"
		if connection.Driver == DriverPostgres {
			versionQuery = "SELECT current_setting('server_version')"
		}
		return tx.QueryRowContext(ctx, versionQuery).Scan(&result.Version)
	})
	if err != nil {
		return Overview{}, fmt.Errorf("读取数据库版本: %w", err)
	}
	if connection.Driver == DriverMySQL && strings.TrimSpace(schema) == "" {
		result.SizeDetail = "请选择数据库后查看其数据大小"
		return result, nil
	}
	err = withReadOnly(ctx, connection, password, func(tx *sql.Tx) error {
		var size int64
		if connection.Driver == DriverMySQL {
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(COALESCE(data_length,0)+COALESCE(index_length,0)),0) FROM information_schema.tables WHERE table_schema=? AND table_type='BASE TABLE'`, schema).Scan(&size); err != nil {
				return err
			}
		} else {
			if err := tx.QueryRowContext(ctx, "SELECT pg_database_size(current_database())").Scan(&size); err != nil {
				return err
			}
		}
		result.DatabaseSizeBytes = &size
		return nil
	})
	if err != nil {
		result.SizeDetail = "当前账号无法读取数据库大小"
		return result, nil
	}
	if connection.Driver == DriverMySQL {
		result.SizeDetail = "当前库可见表的数据与索引大小，MySQL 结果为估算值；不含实例日志和剩余磁盘空间"
	}
	return result, nil
}

func ListTables(ctx context.Context, connection Connection, password, schema string) ([]Table, error) {
	if strings.TrimSpace(schema) == "" {
		return nil, fmt.Errorf("请选择数据库架构")
	}
	result := make([]Table, 0)
	err := withReadOnly(ctx, connection, password, func(tx *sql.Tx) error {
		query := bind(connection.Driver, `SELECT table_schema, table_name, table_type FROM information_schema.tables WHERE table_schema=? AND table_type IN ('BASE TABLE','VIEW') ORDER BY table_name`)
		rows, err := tx.QueryContext(ctx, query, schema)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var table Table
			if err := rows.Scan(&table.Schema, &table.Name, &table.Type); err != nil {
				return err
			}
			result = append(result, table)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("读取数据表: %w", err)
	}
	return result, nil
}

func GetColumns(ctx context.Context, connection Connection, password, schema, table string) ([]Column, error) {
	if err := validateReference(schema, table); err != nil {
		return nil, err
	}
	result := make([]Column, 0)
	err := withReadOnly(ctx, connection, password, func(tx *sql.Tx) error {
		query := columnQuery(connection.Driver)
		rows, err := tx.QueryContext(ctx, query, schema, table)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var column Column
			var nullable, primary any
			if err := rows.Scan(&column.Name, &column.DataType, &nullable, &column.DefaultValue, &primary, &column.Comment); err != nil {
				return err
			}
			column.Nullable = toBool(nullable)
			column.PrimaryKey = toBool(primary)
			result = append(result, column)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("读取字段结构: %w", err)
	}
	return result, nil
}

func GetIndexes(ctx context.Context, connection Connection, password, schema, table string) ([]Index, error) {
	if err := validateReference(schema, table); err != nil {
		return nil, err
	}
	result := make([]Index, 0)
	err := withReadOnly(ctx, connection, password, func(tx *sql.Tx) error {
		query := indexQuery(connection.Driver)
		rows, err := tx.QueryContext(ctx, query, schema, table)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var index Index
			var columns string
			var unique any
			if err := rows.Scan(&index.Name, &unique, &columns); err != nil {
				return err
			}
			index.Unique = toBool(unique)
			if columns != "" {
				index.Columns = strings.Split(columns, ",")
			}
			result = append(result, index)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("读取索引: %w", err)
	}
	return result, nil
}

func GetTableRows(ctx context.Context, connection Connection, password, schema, table string, query TableQuery) (TablePage, error) {
	if err := validateReference(schema, table); err != nil {
		return TablePage{}, err
	}
	var result TablePage
	err := withReadOnly(ctx, connection, password, func(tx *sql.Tx) error {
		columns, err := getColumnsTx(ctx, tx, connection.Driver, schema, table)
		if err != nil {
			return err
		}
		query, err = query.normalized(columns)
		if err != nil {
			return err
		}
		quotedSchema, quotedTable := quoteIdentifier(connection.Driver, schema), quoteIdentifier(connection.Driver, table)
		order := quoteIdentifier(connection.Driver, query.SortColumn)
		direction := "ASC"
		if query.SortDesc {
			direction = "DESC"
		}
		offset := (query.Page - 1) * query.PageSize
		statement := fmt.Sprintf("SELECT * FROM %s.%s ORDER BY %s %s LIMIT %s OFFSET %s", quotedSchema, quotedTable, order, direction, placeholder(connection.Driver, 1), placeholder(connection.Driver, 2))
		rows, err := tx.QueryContext(ctx, statement, query.PageSize+1, offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		values := make([]map[string]string, 0, query.PageSize)
		names, err := rows.Columns()
		if err != nil {
			return err
		}
		for rows.Next() {
			raw := make([]any, len(names))
			dest := make([]any, len(names))
			for i := range raw {
				dest[i] = &raw[i]
			}
			if err := rows.Scan(dest...); err != nil {
				return err
			}
			if len(values) >= query.PageSize {
				result.HasNext = true
				continue
			}
			item := make(map[string]string, len(names))
			for i, name := range names {
				item[name] = displayValue(raw[i])
			}
			values = append(values, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		result = TablePage{Columns: columns, Rows: values, Page: query.Page, PageSize: query.PageSize, HasNext: result.HasNext, SortColumn: query.SortColumn, SortDesc: query.SortDesc}
		return nil
	})
	if err != nil {
		return TablePage{}, fmt.Errorf("读取表数据: %w", err)
	}
	return result, nil
}

func withReadOnly(ctx context.Context, connection Connection, password string, operation func(*sql.Tx) error) error {
	if password == "" {
		return ErrCredentialUnavailable
	}
	db, err := open(connection, password)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("连接数据库: %w", err)
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := operation(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func open(connection Connection, password string) (*sql.DB, error) {
	var driver, dsn string
	switch connection.Driver {
	case DriverPostgres:
		endpoint := (&url.URL{Scheme: "postgres", User: url.UserPassword(connection.Username, password), Host: net.JoinHostPort(connection.Host, strconv.Itoa(connection.Port)), Path: "/" + connection.DatabaseName}).String()
		parameters := url.Values{"sslmode": []string{connection.SSLMode}, "connect_timeout": []string{"5"}}
		dsn = endpoint + "?" + parameters.Encode()
		driver = "pgx"
	case DriverMySQL:
		config := mysql.NewConfig()
		config.User = connection.Username
		config.Passwd = password
		config.Net = "tcp"
		config.Addr = net.JoinHostPort(connection.Host, strconv.Itoa(connection.Port))
		config.DBName = connection.DatabaseName
		config.ParseTime = true
		config.Timeout = 5 * time.Second
		config.ReadTimeout = 15 * time.Second
		config.WriteTimeout = 15 * time.Second
		if connection.SSLMode == "require" {
			config.TLSConfig = "true"
		}
		dsn = config.FormatDSN()
		driver = "mysql"
	default:
		return nil, fmt.Errorf("不支持的数据库类型 %q", connection.Driver)
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("创建数据库连接: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	db.SetConnMaxLifetime(time.Minute)
	return db, nil
}

func getColumnsTx(ctx context.Context, tx *sql.Tx, driver Driver, schema, table string) ([]Column, error) {
	result := make([]Column, 0)
	rows, err := tx.QueryContext(ctx, columnQuery(driver), schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var column Column
		var nullable, primary any
		if err := rows.Scan(&column.Name, &column.DataType, &nullable, &column.DefaultValue, &primary, &column.Comment); err != nil {
			return nil, err
		}
		column.Nullable = toBool(nullable)
		column.PrimaryKey = toBool(primary)
		result = append(result, column)
	}
	return result, rows.Err()
}

func columnQuery(driver Driver) string {
	if driver == DriverMySQL {
		return `SELECT c.column_name,c.column_type,c.is_nullable,COALESCE(c.column_default,''),c.column_key='PRI',COALESCE(c.column_comment,'') FROM information_schema.columns c WHERE c.table_schema=? AND c.table_name=? ORDER BY c.ordinal_position`
	}
	return `SELECT c.column_name,COALESCE(c.data_type,''),c.is_nullable='YES',COALESCE(c.column_default,''),EXISTS (SELECT 1 FROM information_schema.table_constraints tc JOIN information_schema.key_column_usage kcu ON tc.constraint_name=kcu.constraint_name AND tc.table_schema=kcu.table_schema AND tc.table_name=kcu.table_name WHERE tc.constraint_type='PRIMARY KEY' AND kcu.table_schema=c.table_schema AND kcu.table_name=c.table_name AND kcu.column_name=c.column_name),COALESCE(pg_catalog.col_description(cls.oid,c.ordinal_position),'') FROM information_schema.columns c JOIN pg_catalog.pg_class cls ON cls.relname=c.table_name JOIN pg_catalog.pg_namespace ns ON ns.oid=cls.relnamespace AND ns.nspname=c.table_schema WHERE c.table_schema=$1 AND c.table_name=$2 ORDER BY c.ordinal_position`
}

func indexQuery(driver Driver) string {
	if driver == DriverMySQL {
		return `SELECT index_name,non_unique=0,GROUP_CONCAT(column_name ORDER BY seq_in_index SEPARATOR ',') FROM information_schema.statistics WHERE table_schema=? AND table_name=? GROUP BY index_name,non_unique ORDER BY index_name`
	}
	return `SELECT idx.relname,ix.indisunique,string_agg(att.attname,',' ORDER BY array_position(ix.indkey,att.attnum)) FROM pg_catalog.pg_class tbl JOIN pg_catalog.pg_namespace ns ON ns.oid=tbl.relnamespace JOIN pg_catalog.pg_index ix ON ix.indrelid=tbl.oid JOIN pg_catalog.pg_class idx ON idx.oid=ix.indexrelid JOIN pg_catalog.pg_attribute att ON att.attrelid=tbl.oid AND att.attnum=ANY(ix.indkey) WHERE ns.nspname=$1 AND tbl.relname=$2 GROUP BY idx.relname,ix.indisunique ORDER BY idx.relname`
}

func bind(driver Driver, statement string) string {
	if driver != DriverPostgres {
		return statement
	}
	var result strings.Builder
	index := 0
	for _, character := range statement {
		if character == '?' {
			index++
			result.WriteString(placeholder(driver, index))
		} else {
			result.WriteRune(character)
		}
	}
	return result.String()
}
func placeholder(driver Driver, index int) string {
	if driver == DriverPostgres {
		return "$" + strconv.Itoa(index)
	}
	return "?"
}
func quoteIdentifier(driver Driver, value string) string {
	if driver == DriverMySQL {
		return "`" + strings.ReplaceAll(value, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
func validateReference(schema, table string) error {
	if strings.TrimSpace(schema) == "" || strings.TrimSpace(table) == "" {
		return fmt.Errorf("请选择数据表")
	}
	if len(schema) > 128 || len(table) > 128 || strings.ContainsAny(schema+table, "\x00\r\n") {
		return fmt.Errorf("数据表标识无效")
	}
	return nil
}
func toBool(value any) bool {
	switch value := value.(type) {
	case bool:
		return value
	case int64:
		return value != 0
	case []byte:
		return string(value) == "1" || strings.EqualFold(string(value), "true")
	case string:
		return value == "1" || strings.EqualFold(value, "true")
	}
	return false
}
func displayValue(value any) string {
	if value == nil {
		return "NULL"
	}
	var result string
	switch value := value.(type) {
	case []byte:
		result = string(value)
	case time.Time:
		result = value.UTC().Format(time.RFC3339Nano)
	default:
		result = fmt.Sprint(value)
	}
	if len([]rune(result)) > 4096 {
		return string([]rune(result)[:4096]) + "…"
	}
	return result
}
