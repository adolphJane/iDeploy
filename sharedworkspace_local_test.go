package main

import (
	"context"
	"path/filepath"
	"testing"

	"ideploy/internal/platform/database"
)

func TestLockedSharedWorkspaceKeepsLocalEdits(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "shared.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO shared_sync_state(id,server_id,device_id,mode,cursor) VALUES(1,'server','device','shared',0)`); err != nil {
		t.Fatal(err)
	}
	s := &SystemService{db: db}
	if err := s.prepareSharedWrite("CreateProject", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects(id,name,created_at,updated_at) VALUES('project','Local','2026-01-01','2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if err := s.completeSharedWrite(); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncSharedWorkspace(); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM projects WHERE id='project'`).Scan(&name); err != nil || name != "Local" {
		t.Fatalf("local edit lost: %q %v", name, err)
	}
}
