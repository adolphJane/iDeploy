package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ideploy/internal/deployment"
	"ideploy/internal/platform/database"
	"ideploy/internal/server"
	"ideploy/internal/sharedworkspace"
)

type composeTestReporter struct{}

func (composeTestReporter) Progress(int, string) {}
func (composeTestReporter) Log(string)           {}

func TestComposeFailureRestoresPreviousRelease(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("local Compose release snapshots use the macOS Keychain interface")
	}
	for _, failure := range []string{"up", "health"} {
		t.Run(failure, func(t *testing.T) {
			work := t.TempDir()
			t.Setenv("PATH", work+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("IDE_TEST_COMPOSE_DIR", work)
			t.Setenv("IDE_TEST_COMPOSE_FAILURE", failure)
			setComposeTestKeychain(t, work)
			if err := os.WriteFile(filepath.Join(work, "ssh"), []byte(`#!/bin/sh
for command do :; done
work="$IDE_TEST_COMPOSE_DIR"
printf '%s\n' "$command" >> "$work/commands"
case "$command" in
  *'docker ps -a'*)
    if [ -f "$work/unhealthy" ]; then exit 1; fi
    printf '%s\n' '{"ID":"workload","Names":"app-api","State":"running","Labels":"com.docker.compose.project=app"}' ;;
  *'cat > '*)
    cat > "$work/remote-compose.yml" ;;
  *"IMAGE_TAG='2.0.0'"*'up -d'*)
    if [ "$IDE_TEST_COMPOSE_FAILURE" = up ]; then exit 1; fi
    touch "$work/unhealthy" ;;
  *"IMAGE_TAG='1.0.0'"*'up -d'*)
    rm -f "$work/unhealthy" ;;
esac
`), 0o700); err != nil {
				t.Fatal(err)
			}

			ctx := context.Background()
			service, store, target := newComposeTestStore(t, work)
			oldManifest := `{"services":{"api":{"image":"registry.example/app@sha256:` + strings.Repeat("a", 64) + `"}}}`
			newManifest := strings.Replace(oldManifest, strings.Repeat("a", 64), strings.Repeat("b", 64), 1)
			previous, err := store.CreateRelease(ctx, deployment.ReleaseInput{DeploymentID: target.ID, Version: "1.0.0"})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.UpdateRelease(ctx, previous.ID, deployment.ReleaseSucceeded, "", "compose:app:1.0.0", "", oldManifest); err != nil {
				t.Fatal(err)
			}
			release, err := store.CreateRelease(ctx, deployment.ReleaseInput{DeploymentID: target.ID, Version: "2.0.0"})
			if err != nil {
				t.Fatal(err)
			}

			host := server.Connection{ID: "server", Host: "127.0.0.1", SSHUser: "test", Port: 22}
			err = service.deployAndCheckCompose(ctx, store, release, target, host, "test-job", composeTestReporter{}, server.NewRemote(), "compose:app:2.0.0", "", newManifest, "deploy")
			if err == nil || !strings.Contains(err.Error(), "已恢复上一版") {
				t.Fatalf("publish failure did not report a restore: %v", err)
			}
			payload, err := os.ReadFile(filepath.Join(work, "remote-compose.yml"))
			if err != nil || string(payload) != oldManifest {
				t.Fatalf("previous manifest was not restored: %v", err)
			}
			commands, err := os.ReadFile(filepath.Join(work, "commands"))
			if err != nil || !strings.Contains(string(commands), "IMAGE_TAG='1.0.0'") {
				t.Fatalf("previous version was not reapplied: %v", err)
			}
			containers, _, err := store.Snapshot(ctx, host.ID)
			if err != nil || len(containers) != 1 {
				t.Fatalf("previous runtime snapshot not refreshed: %#v, %v", containers, err)
			}
		})
	}
}

func TestRecoveredRemoteComposeSnapshotCanResumeWithoutRebuild(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("local Compose release snapshots use the macOS Keychain interface")
	}
	work := t.TempDir()
	t.Setenv("PATH", work+string(os.PathListSeparator)+os.Getenv("PATH"))
	setComposeTestKeychain(t, work)
	_, store, target := newComposeTestStore(t, work)
	ctx := context.Background()
	source, err := store.CreateRelease(ctx, deployment.ReleaseInput{
		DeploymentID: target.ID, Version: "1.0.0", BuildMode: deployment.BuildRemote,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetReleaseDelivery(ctx, source.ID, "", "", "", "", strings.Repeat("c", 32)); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateRelease(ctx, source.ID, deployment.ReleaseFailed, deployment.InterruptedReleaseDetail, "", "", ""); err != nil {
		t.Fatal(err)
	}
	candidates, err := store.InterruptedRemoteReleases(ctx)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("interrupted release missing from recovery scan: %v", err)
	}
	manifest := `{"services":{"api":{"image":"registry.example/app@sha256:` + strings.Repeat("a", 64) + `"}}}`
	recovered, err := saveRecoveredRemoteArtifact(ctx, store, candidates[0], sharedworkspace.RemoteReleaseJob{
		Status: "artifact_ready", ImageRef: "compose:" + target.ID + ":1.0.0", Manifest: manifest,
	})
	if err != nil || recovered.ImageRef != "compose:app:1.0.0" || recovered.Manifest != manifest {
		t.Fatalf("remote Compose artifact was not saved for resume: %v", err)
	}
	if _, err := store.CreateRelease(ctx, deployment.ReleaseInput{
		DeploymentID: target.ID, Version: recovered.Version, BuildMode: recovered.BuildMode,
		ImageRef: recovered.ImageRef, Manifest: recovered.Manifest, RollbackToID: source.ID, AllowResume: true,
	}); err != nil {
		t.Fatalf("recovered artifact cannot be resumed: %v", err)
	}
}

func setComposeTestKeychain(t *testing.T, work string) {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IDE_TEST_COMPOSE_KEY", hex.EncodeToString(key))
	if err := os.WriteFile(filepath.Join(work, "security"), []byte(`#!/bin/sh
case "$*" in
  *release-manifests-v1*) printf '%s\n' "$IDE_TEST_COMPOSE_KEY";;
  *) exit 1;;
esac
`), 0o700); err != nil {
		t.Fatal(err)
	}
}

func newComposeTestStore(t *testing.T, work string) (*SystemService, *deployment.Store, deployment.Target) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(work, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, statement := range []string{
		`INSERT INTO projects(id,name,created_at,updated_at) VALUES('project','API','2026-01-01','2026-01-01')`,
		`INSERT INTO servers(id,name,host,port,ssh_user,created_at,updated_at) VALUES('server','Dev','127.0.0.1',22,'test','2026-01-01','2026-01-01')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	store := deployment.NewStore(db)
	target, err := store.Create(ctx, deployment.Input{ProjectID: "project", ServerID: "server", WorkloadKind: "compose", WorkloadName: "app"})
	if err != nil {
		t.Fatal(err)
	}

	return &SystemService{db: db}, store, target
}
