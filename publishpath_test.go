package main

import (
	"context"
	"ideploy/internal/deployment"
	"ideploy/internal/domain"
	"ideploy/internal/platform/database"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseBasePathUsesSourceSnapshotForRollback(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "release-path.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`INSERT INTO servers(id,name,host,ssh_user,created_at,updated_at) VALUES('server','test','192.0.2.1','root','now','now')`,
		`INSERT INTO projects(id,name,created_at,updated_at) VALUES('project','test','now','now')`,
		`INSERT INTO deployment_targets(id,project_id,server_id,created_at,updated_at) VALUES('target','project','server','now','now')`,
		`INSERT INTO deployment_releases(id,deployment_id,action,version,status,created_at) VALUES('source','target','deploy','v1','succeeded','now'),('rollback','target','rollback','v1','queued','now')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	path := domain.PublishPath{DeploymentID: "target", FQDN: "apps.example.com", PathPrefix: "/demo/", ContainerPort: 3000}
	store := domain.NewStore(db)
	if err := store.SaveReleasePathIntent(ctx, "source", path); err != nil {
		t.Fatal(err)
	}
	service := NewSystemService()
	service.db = db
	rollback := deployment.Release{ID: "rollback", DeploymentID: "target", Action: "rollback", RollbackToID: "source"}
	if got, err := service.releaseBasePath(ctx, rollback); err != nil || got != "/demo/" {
		t.Fatalf("rollback path = %q, %v", got, err)
	}
	if err := store.MarkPublishedPathApplied(ctx, path, "127.0.0.1", 8101, "/cert", "/key", "hash", "nginx:main"); err != nil {
		t.Fatal(err)
	}
	if err := service.validateRollbackBasePath(ctx, deployment.Release{ID: "source", DeploymentID: "target", Action: "deploy"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE shared_path_routes SET path_prefix='/new/' WHERE deployment_id='target'`); err != nil {
		t.Fatal(err)
	}
	if err := service.validateRollbackBasePath(ctx, deployment.Release{ID: "source", DeploymentID: "target", Action: "deploy"}); err == nil || !strings.Contains(err.Error(), "/demo/") {
		t.Fatalf("rollback to incompatible path was accepted: %v", err)
	}
}
