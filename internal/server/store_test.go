package server_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"ideploy/internal/deployment"
	"ideploy/internal/job"
	"ideploy/internal/platform/database"
	"ideploy/internal/server"
)

func TestClearAllServerData(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "servers.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, "INSERT INTO projects(id, name, created_at, updated_at) VALUES ('project-1', 'API', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')"); err != nil {
		t.Fatalf("create project fixture: %v", err)
	}
	servers := server.NewStore(db)
	created, err := servers.Create(ctx, server.NewConnection{
		Name: "Dev", Host: "127.0.0.1", Port: 22, SSHUser: "deploy",
		AuthMethod: server.AuthMethodPassword, CredentialRef: server.PasswordCredentialRef("credential-1"),
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	deployments := deployment.NewStore(db)
	if _, err := deployments.Create(ctx, deployment.Input{ProjectID: "project-1", ServerID: created.ID, WorkloadKind: "container", WorkloadName: "api"}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	if err := deployments.SaveSnapshot(ctx, created.ID, []server.Container{{ID: "container-1", Name: "api"}}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	jobs := job.NewStore(db)
	if _, err := jobs.Enqueue(ctx, job.NewJob{Type: "server.probe", LockKey: "server:" + created.ID}); err != nil {
		t.Fatalf("enqueue server job: %v", err)
	}
	if _, err := servers.ClearAll(ctx); err == nil {
		t.Fatal("cleared server data while a server job was active")
	}
	if _, err := jobs.ClaimNext(ctx, time.Minute); err != nil {
		t.Fatalf("claim server job: %v", err)
	}
	if _, err := jobs.RecoverInterrupted(ctx); err != nil {
		t.Fatalf("finish server job: %v", err)
	}

	cleared, err := servers.ClearAll(ctx)
	if err != nil {
		t.Fatalf("clear server data: %v", err)
	}
	if cleared.Servers != 1 || cleared.Deployments != 1 || cleared.Snapshots != 1 || cleared.Jobs != 1 || len(cleared.CredentialRefs) != 1 {
		t.Fatalf("unexpected clear result: %#v", cleared)
	}
	for _, table := range []string{"servers", "deployment_targets", "remote_snapshots"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("table %s was not cleared: count=%d err=%v", table, count, err)
		}
	}
	var projectCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects").Scan(&projectCount); err != nil || projectCount != 1 {
		t.Fatalf("project data should remain: count=%d err=%v", projectCount, err)
	}
}
