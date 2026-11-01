package database_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"ideploy/internal/platform/database"
	"ideploy/migrations"
)

func TestOpenAppliesMigrationsIdempotently(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ideploy.db")
	ctx := context.Background()

	first, err := database.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	var migrations int
	if err := first.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&migrations); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if migrations != 17 {
		t.Fatalf("expected 17 applied migrations, got %d", migrations)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first database: %v", err)
	}

	second, err := database.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer second.Close()

	if err := second.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&migrations); err != nil {
		t.Fatalf("count migrations after reopen: %v", err)
	}
	if migrations != 17 {
		t.Fatalf("migration was not idempotent: got %d rows", migrations)
	}
}

func TestUpgradePreservesExistingAliDNSConnection(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, _ := strings.Cut(entry.Name(), "_")
		version, err := strconv.Atoi(prefix)
		if err != nil {
			t.Fatal(err)
		}
		if version >= 15 {
			continue
		}
		contents, err := migrations.Files.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := legacy.ExecContext(ctx, string(contents)); err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if _, err := legacy.ExecContext(ctx, "INSERT INTO schema_migrations VALUES(?, ?, '2026-10-21T00:00:00Z')", version, entry.Name()); err != nil {
			t.Fatal(err)
		}
	}
	_, err = legacy.ExecContext(ctx, "INSERT INTO dns_connections(id,name,access_key_id,credential_ref,created_at,updated_at) VALUES('old','Ali','AK-ID','keychain:alidns-access-key-secret:old','x','x')")
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := database.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var provider, ref string
	if err := upgraded.QueryRowContext(ctx, "SELECT provider,credential_ref FROM dns_connections WHERE id='old'").Scan(&provider, &ref); err != nil {
		t.Fatal(err)
	}
	if provider != "aliyun" || ref != "keychain:alidns-access-key-secret:old" {
		t.Fatalf("legacy connection changed: provider=%q ref=%q", provider, ref)
	}
}
