package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
)

func TestDatabaseDSNFromMySQLEnvironment(t *testing.T) {
	values := map[string]string{"IDEPLOY_DB_HOST": "db.example.test", "IDEPLOY_DB_USER": "ideploy", "IDEPLOY_DB_PASSWORD": "p@ss:/word", "IDEPLOY_DB_NAME": "ideploy_shared"}
	dsn, err := databaseDSN("mysql", func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if config.Addr != "db.example.test:3306" || config.Passwd != values["IDEPLOY_DB_PASSWORD"] || config.DBName != "ideploy_shared" || config.TLSConfig != "true" {
		t.Fatal("MySQL environment configuration did not round trip")
	}
	values["IDEPLOY_DB_TLS_MODE"] = "skip-verify"
	dsn, err = databaseDSN("mysql", func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	config, err = mysql.ParseDSN(dsn)
	if err != nil || config.TLSConfig != "skip-verify" {
		t.Fatal("MySQL skip-verify TLS mode was not applied")
	}
	values["IDEPLOY_DB_TLS_MODE"] = "disable"
	if _, err := databaseDSN("mysql", func(key string) string { return values[key] }); err == nil {
		t.Fatal("unencrypted MySQL mode was accepted")
	}
	values["IDEPLOY_DB_PORT"] = "70000"
	if _, err := databaseDSN("mysql", func(key string) string { return values[key] }); err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatal("invalid MySQL port was accepted")
	}
}

func TestDatabaseDSNReadsMySQLPasswordFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte("file-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"IDEPLOY_DB_HOST": "db.example.test", "IDEPLOY_DB_USER": "ideploy", "IDEPLOY_DB_NAME": "shared", "IDEPLOY_DB_PASSWORD_FILE": path}
	dsn, err := databaseDSN("mysql", func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil || config.Passwd != "file-secret" {
		t.Fatal("MySQL password file was not used")
	}
}
