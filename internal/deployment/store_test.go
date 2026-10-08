package deployment_test

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

func TestStoreCreatesAndDeletesTarget(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "deployments.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, "INSERT INTO projects(id, name, created_at, updated_at) VALUES ('project-1', 'API', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')"); err != nil {
		t.Fatalf("create project fixture: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO servers(id, name, host, port, ssh_user, created_at, updated_at) VALUES ('server-1', 'Dev', '127.0.0.1', 22, 'deploy', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')"); err != nil {
		t.Fatalf("create server fixture: %v", err)
	}

	store := deployment.NewStore(db)
	created, err := store.Create(ctx, deployment.Input{ProjectID: "project-1", ServerID: "server-1", WorkloadKind: "container", WorkloadName: "api", ContainerPorts: []string{"8080:80"}, RestartPolicy: "always"})
	if err != nil || created.DesiredState != deployment.DesiredRunning {
		t.Fatalf("create deployment target: %#v, %v", created, err)
	}
	if len(created.ContainerPorts) != 1 || created.ContainerPorts[0] != "8080:80" || created.RestartPolicy != "always" {
		t.Fatalf("runtime configuration was not persisted: %#v", created)
	}
	release, err := store.CreateRelease(ctx, deployment.ReleaseInput{DeploymentID: created.ID, Version: "v1.2.3"})
	if err != nil || release.Status != deployment.ReleaseQueued {
		t.Fatalf("create release: %#v, %v", release, err)
	}
	if err := store.UpdateRelease(ctx, release.ID, deployment.ReleaseSucceeded, "healthy", "registry.example/api:v1.2.3", "abc123", ""); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"v1.2.3", "1.2.3", "1.2.2"} {
		if _, err := store.CreateRelease(ctx, deployment.ReleaseInput{DeploymentID: created.ID, Version: version}); err == nil {
			t.Fatalf("accepted duplicate or downgrade %s", version)
		}
	}
	releases, err := store.ListReleases(ctx, created.ID, 10)
	if err != nil || len(releases) != 1 || releases[0].ImageRef != "registry.example/api:v1.2.3" || releases[0].FinishedAt == nil {
		t.Fatalf("invalid release history: %#v, %v", releases, err)
	}
	if err := store.SaveSnapshot(ctx, "server-1", []server.Container{{ID: "full-id", Name: "api", Health: "healthy"}}); err != nil {
		t.Fatal(err)
	}
	cached, observed, err := deployment.NewStore(db).Snapshot(ctx, "server-1")
	if err != nil || len(cached) != 1 || cached[0].Health != "healthy" || observed == "" {
		t.Fatalf("invalid persistent snapshot: %#v %s %v", cached, observed, err)
	}
	if err := store.Bind(ctx, created.ID, "compose", "api-stack"); err != nil {
		t.Fatal(err)
	}
	if _, err := job.NewStore(db).Enqueue(ctx, job.NewJob{Type: "test", LockKey: "server:server-1"}); err != nil {
		t.Fatal(err)
	}
	// Port inference is part of the release task that owns the server lock. It
	// must be able to persist the resolved mapping without blocking itself.
	if err := store.UpdateContainerPorts(ctx, created.ID, []string{"8080:8080"}); err != nil {
		t.Fatalf("update inferred ports during active server job: %v", err)
	}
	updated, err := store.Get(ctx, created.ID)
	if err != nil || len(updated.ContainerPorts) != 1 || updated.ContainerPorts[0] != "8080:8080" {
		t.Fatalf("inferred ports were not persisted: %#v, %v", updated, err)
	}
	if err := store.Bind(ctx, created.ID, "container", "other"); err == nil {
		t.Fatal("binding changed during queued operation")
	}
	if err := store.Delete(ctx, created.ID); err == nil {
		t.Fatal("deleted binding during queued operation")
	}
	if _, err := job.NewStore(db).ClaimNext(ctx, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := job.NewStore(db).RecoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete deployment target: %v", err)
	}
}

func TestDeploymentEnvironmentRevisionAndCleanupMetadata(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "environment.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "INSERT INTO projects(id,name,created_at,updated_at) VALUES('project-1','API','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO servers(id,name,host,port,ssh_user,created_at,updated_at) VALUES('server-1','Dev','127.0.0.1',22,'deploy','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	store := deployment.NewStore(db)
	target, err := store.Create(ctx, deployment.Input{ProjectID: "project-1", ServerID: "server-1", WorkloadKind: "container", WorkloadName: "api"})
	if err != nil {
		t.Fatal(err)
	}
	secretRef := deployment.EnvironmentSecretCredentialRef("secret-1")
	values, cleanup, err := store.ApplyEnvironmentChanges(ctx, target.ID, []deployment.EnvironmentChange{
		{Name: "DB_MODE", Kind: deployment.EnvironmentKindPlain, PlainValue: "mysql"},
		{Name: "DATABASE_PASSWORD", Kind: deployment.EnvironmentKindSecret, CredentialRef: secretRef},
	}, nil)
	if err != nil || len(cleanup) != 0 || len(values) != 2 {
		t.Fatalf("apply environment: values=%#v cleanup=%#v err=%v", values, cleanup, err)
	}
	target, err = store.Get(ctx, target.ID)
	if err != nil || target.EnvironmentRevision != 1 || target.AppliedEnvironmentRevision != 0 {
		t.Fatalf("unexpected environment revision: %#v err=%v", target, err)
	}
	if err := store.MarkEnvironmentApplied(ctx, target.ID, 1); err != nil {
		t.Fatal(err)
	}
	_, cleanup, err = store.ApplyEnvironmentChanges(ctx, target.ID, nil, []string{"DATABASE_PASSWORD"})
	if err != nil || len(cleanup) != 1 || cleanup[0] != secretRef {
		t.Fatalf("remove secret: cleanup=%#v err=%v", cleanup, err)
	}
}
