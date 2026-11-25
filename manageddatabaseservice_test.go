package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"ideploy/internal/databasebrowser"
	"ideploy/internal/platform/database"
)

func TestManagedDatabaseRestoreRequiresFreshTargetInSameEnvironment(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "managed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `INSERT INTO servers(id,name,host,ssh_user,created_at,updated_at) VALUES
		('test','Test','test.example','deploy','x','x'),
		('alias','Alias','TEST.EXAMPLE.','other','x','x'),
		('prod','Production','prod.example','deploy','x','x')`); err != nil {
		t.Fatal(err)
	}
	store := databasebrowser.NewStore(db)
	config := databasebrowser.InstanceConfig{
		Name: "Test", Environment: "test", ServerID: "test", Driver: databasebrowser.DriverPostgres,
		EngineVersion: "17", ListenIP: "10.0.0.1", Port: 5432, DatabaseName: "api",
	}
	source, err := store.CreateInstance(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetInstanceStatus(ctx, source.ID, "ready"); err != nil {
		t.Fatal(err)
	}
	backupID := strings.Repeat("a", 32)
	if err := store.RecordBackup(ctx, backupID, source.ID); err != nil {
		t.Fatal(err)
	}
	config.Port = 5433
	target, err := store.CreateInstance(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	operation := managedDatabaseOperation{InstanceID: target.ID, Action: "restore", BackupID: backupID}
	if _, err := validateManagedDatabaseOperation(ctx, store, target, operation); err != nil {
		t.Fatal(err)
	}
	config.ServerID, config.Environment = "alias", "production"
	if _, err := store.CreateInstance(ctx, config); err == nil {
		t.Fatal("registered opposite environments on the same SSH endpoint")
	}
	config.ServerID = "prod"
	production, err := store.CreateInstance(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateManagedDatabaseOperation(ctx, store, production, operation); err == nil {
		t.Fatal("allowed restoring test backup into production")
	}
	if err := store.SetInstanceStatus(ctx, target.ID, "restoring"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileInterruptedInstances(ctx); err != nil {
		t.Fatal(err)
	}
	failed, err := store.GetInstance(ctx, target.ID)
	if err != nil || failed.Status != "restore_failed" {
		t.Fatalf("interrupted restore not recovered: %+v, %v", failed, err)
	}
	if _, err := validateManagedDatabaseOperation(ctx, store, failed, operation); err == nil {
		t.Fatal("allowed restoring into partially restored instance")
	}
}
