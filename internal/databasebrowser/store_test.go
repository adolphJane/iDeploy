package databasebrowser_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"ideploy/internal/databasebrowser"
	"ideploy/internal/platform/database"
)

func TestStoreCreatesUpdatesAndRecordsConnection(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "database-browser.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	store := databasebrowser.NewStore(db)
	created, err := store.Create(ctx, databasebrowser.NewConnection{Name: "Production", Driver: databasebrowser.DriverPostgres, Host: "db.example.test", DatabaseName: "app", Username: "readonly", SSLMode: "require", CredentialRef: "keychain:database-password:test"})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	if created.Port != 5432 || created.Driver != databasebrowser.DriverPostgres {
		t.Fatalf("unexpected connection: %#v", created)
	}
	if err := store.RecordConnection(ctx, created.ID, nil); err != nil {
		t.Fatalf("record success: %v", err)
	}
	updated, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get updated connection: %v", err)
	}
	if updated.LastConnected == nil || updated.LastError != "" {
		t.Fatalf("unexpected successful record: %#v", updated)
	}
	if err := store.RecordConnection(ctx, created.ID, errors.New("access denied")); err != nil {
		t.Fatalf("record error: %v", err)
	}
	updated, err = store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get failed connection: %v", err)
	}
	if updated.LastError != "access denied" {
		t.Fatalf("unexpected error record: %#v", updated)
	}
	credentialRef, err := store.Delete(ctx, created.ID)
	if err != nil || credentialRef != "keychain:database-password:test" {
		t.Fatalf("delete: ref=%q err=%v", credentialRef, err)
	}
}

func TestConnectionValidationUsesDatabaseDefaults(t *testing.T) {
	mysql, err := (databasebrowser.NewConnection{Name: "MySQL", Driver: databasebrowser.DriverMySQL, Host: "127.0.0.1", DatabaseName: "app", Username: "readonly"}).Validate()
	if err != nil {
		t.Fatalf("validate mysql: %v", err)
	}
	if mysql.Port != 3306 || mysql.SSLMode != "require" {
		t.Fatalf("unexpected defaults: %#v", mysql)
	}
}

func TestMySQLConnectionMayOmitDefaultDatabase(t *testing.T) {
	connection, err := (databasebrowser.NewConnection{Name: "MySQL", Driver: databasebrowser.DriverMySQL, Host: "127.0.0.1", Username: "readonly"}).Validate()
	if err != nil {
		t.Fatalf("validate mysql without database: %v", err)
	}
	if connection.DatabaseName != "" {
		t.Fatalf("database name = %q, want empty", connection.DatabaseName)
	}
	if _, err := (databasebrowser.NewConnection{Name: "PostgreSQL", Driver: databasebrowser.DriverPostgres, Host: "127.0.0.1", Username: "readonly"}).Validate(); err == nil {
		t.Fatal("expected PostgreSQL validation to require a database")
	}
}
