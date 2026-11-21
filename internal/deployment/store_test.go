package deployment_test

import (
	"context"
	"path/filepath"
	"strings"
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
	if created.BuildMode != deployment.BuildLocal {
		t.Fatalf("legacy deployment must default to local: %#v", created)
	}
	if _, err := store.Create(ctx, deployment.Input{ProjectID: "project-1", ServerID: "server-1", WorkloadKind: "container", WorkloadName: "invalid", BuildMode: "unknown"}); err == nil {
		t.Fatal("accepted unknown build mode")
	}
	release, err := store.CreateRelease(ctx, deployment.ReleaseInput{DeploymentID: created.ID, Version: "v1.2.3"})
	if err != nil || release.Status != deployment.ReleaseQueued {
		t.Fatalf("create release: %#v, %v", release, err)
	}
	if release.BuildMode != deployment.BuildLocal {
		t.Fatalf("legacy release must default to local: %#v", release)
	}
	cancelable, err := store.CreateRelease(ctx, deployment.ReleaseInput{DeploymentID: created.ID, Version: "1.2.4"})
	if err != nil {
		t.Fatal(err)
	}
	jobs := job.NewStore(db)
	queued, err := jobs.Enqueue(ctx, job.NewJob{Type: "deployment.release", Payload: `{"releaseId":"` + cancelable.ID + `"}`})
	if err != nil {
		t.Fatal(err)
	}
	// The runner can expose the job before AttachReleaseJob has completed.
	if err := jobs.RequestCancel(ctx, queued.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.AttachReleaseJob(ctx, cancelable.ID, queued.ID); err != nil {
		t.Fatal(err)
	}
	canceled, err := store.GetRelease(ctx, cancelable.ID)
	if err != nil || canceled.Status != deployment.ReleaseCanceled || canceled.FinishedAt == nil {
		t.Fatalf("canceled job left inconsistent release: %#v, %v", canceled, err)
	}
	if _, err := store.CreateRelease(ctx, deployment.ReleaseInput{DeploymentID: created.ID, Version: "1.2.4", AllowRetry: true}); err != nil {
		t.Fatalf("canceled version cannot be retried: %v", err)
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
	if err != nil || len(releases) != 3 || releases[2].ImageRef != "registry.example/api:v1.2.3" || releases[2].FinishedAt == nil {
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
		{Name: "DATABASE_PASSWORD", Kind: deployment.EnvironmentKindSecret, CredentialRef: secretRef, BuildSecret: true},
	}, nil)
	if err != nil || len(cleanup) != 0 || len(values) != 2 {
		t.Fatalf("apply environment: values=%#v cleanup=%#v err=%v", values, cleanup, err)
	}
	if values[0].Name != "DATABASE_PASSWORD" || !values[0].BuildSecret {
		t.Fatalf("build secret scope was not persisted: %#v", values)
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

func TestInterruptedRemoteReleaseIsSelectedOnce(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`INSERT INTO projects(id,name,created_at,updated_at) VALUES('project','API','2026-01-01','2026-01-01')`,
		`INSERT INTO servers(id,name,host,port,ssh_user,created_at,updated_at) VALUES('server','Dev','127.0.0.1',22,'deploy','2026-01-01','2026-01-01')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	store := deployment.NewStore(db)
	target, err := store.Create(ctx, deployment.Input{ProjectID: "project", ServerID: "server", WorkloadKind: "container", WorkloadName: "api", BuildMode: deployment.BuildRemote})
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.CreateRelease(ctx, deployment.ReleaseInput{DeploymentID: target.ID, Version: "1.0.0", BuildMode: deployment.BuildRemote})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetReleaseDelivery(ctx, source.ID, strings.Repeat("a", 40), "refs/heads/ideploy/releases/ref", strings.Repeat("b", 40), strings.Repeat("c", 64), strings.Repeat("d", 32)); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateRelease(ctx, source.ID, deployment.ReleaseFailed, "iDeploy 重启前发布任务未完成，请检查远端状态后重新发布", "", "", ""); err != nil {
		t.Fatal(err)
	}
	values, err := store.InterruptedRemoteReleases(ctx)
	if err != nil || len(values) != 1 || values[0].ID != source.ID {
		t.Fatalf("recovery candidates: %#v %v", values, err)
	}
	image := "registry.example/api@sha256:" + strings.Repeat("e", 64)
	if err := store.UpdateRelease(ctx, source.ID, deployment.ReleaseFailed, "已从 iDeploy 服务恢复镜像制品", image, "", ""); err != nil {
		t.Fatal(err)
	}
	child, err := store.CreateRelease(ctx, deployment.ReleaseInput{DeploymentID: target.ID, Version: "1.0.0", ImageRef: image, RollbackToID: source.ID, AllowResume: true, BuildMode: source.BuildMode})
	if err != nil {
		t.Fatal(err)
	}
	if child.ID == source.ID || child.BuildMode != deployment.BuildRemote {
		t.Fatalf("resume lost remote provenance: %#v", child)
	}
	values, err = store.InterruptedRemoteReleases(ctx)
	if err != nil || len(values) != 0 {
		t.Fatalf("source selected after resume: %#v %v", values, err)
	}
	for _, detail := range []string{deployment.PublicationLeaseBusyDetail, "共享服务返回 409：operation already in progress"} {
		if err := store.UpdateRelease(ctx, child.ID, deployment.ReleaseFailed, detail, "", "", ""); err != nil {
			t.Fatal(err)
		}
		values, err := store.InterruptedRemoteReleases(ctx)
		if err != nil || len(values) != 1 || values[0].ID != source.ID {
			t.Fatalf("lease contention prevented a safe retry: %#v, %v", values, err)
		}
	}
	if err := store.UpdateRelease(ctx, child.ID, deployment.ReleaseFailed, "target may have changed", "", "", ""); err != nil {
		t.Fatal(err)
	}
	values, err = store.InterruptedRemoteReleases(ctx)
	if err != nil || len(values) != 0 {
		t.Fatalf("selected a release whose resume may have reached the target: %#v, %v", values, err)
	}
}
