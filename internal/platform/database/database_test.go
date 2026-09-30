package database_test

import (
	"context"
	"path/filepath"
	"testing"

	"ideploy/internal/platform/database"
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
	if migrations != 10 {
		t.Fatalf("expected 10 applied migrations, got %d", migrations)
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
	if migrations != 10 {
		t.Fatalf("migration was not idempotent: got %d rows", migrations)
	}
}
