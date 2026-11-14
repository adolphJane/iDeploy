package domain_test

import (
	"context"
	"path/filepath"
	"testing"

	"ideploy/internal/domain"
	"ideploy/internal/platform/database"
)

func TestSharedPathRouteLifecycle(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "paths.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`INSERT INTO servers(id,name,host,ssh_user,created_at,updated_at) VALUES('server','test','192.0.2.1','root','now','now')`,
		`INSERT INTO projects(id,name,created_at,updated_at) VALUES('project','test','now','now')`,
		`INSERT INTO deployment_targets(id,project_id,server_id,created_at,updated_at) VALUES('deployment','project','server','now','now')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	store := domain.NewStore(db)
	v, err := store.SavePathRouteDraft(ctx, domain.PathRoute{FQDN: domain.SharedAppsFQDN, PathPrefix: "demo", DeploymentID: "deployment", ContainerPort: 8080})
	if err != nil {
		t.Fatal(err)
	}
	if v.PathPrefix != "/demo/" {
		t.Fatalf("normalized path: %q", v.PathPrefix)
	}
	if _, err := store.SavePathRouteDraft(ctx, domain.PathRoute{FQDN: domain.SharedAppsFQDN, PathPrefix: "/demo/", DeploymentID: "deployment", ContainerPort: 8080}); err == nil {
		t.Fatal("duplicate path accepted")
	}
	if err := store.SetPathRouteJob(ctx, v.ID, "job-1", "applying"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkPathRouteApplied(ctx, v.ID, "server", "job-1", "127.0.0.1", 8101, "hash", "nginx:container"); err != nil {
		t.Fatal(err)
	}
	routes, err := store.ListServerRoutes(ctx, "server")
	if err != nil || len(routes) != 1 || routes[0].PathPrefix != "/demo/" {
		t.Fatalf("server routes: %+v, %v", routes, err)
	}
	if err := store.SetPathRouteJob(ctx, v.ID, "job-2", "removing"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkPathRouteRemoved(ctx, v.ID, "server", "job-2", "hash-2", "nginx:container", true); err != nil {
		t.Fatal(err)
	}
	if routes, err = store.ListServerRoutes(ctx, "server"); err != nil || len(routes) != 0 {
		t.Fatalf("remaining routes: %+v, %v", routes, err)
	}
}
