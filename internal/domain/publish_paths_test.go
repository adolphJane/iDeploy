package domain_test

import (
	"context"
	"path/filepath"
	"testing"

	"ideploy/internal/domain"
	"ideploy/internal/platform/database"
)

func TestPublishPathReservationAndReleaseIntent(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "publish.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`INSERT INTO servers(id,name,host,ssh_user,created_at,updated_at) VALUES('server','test','192.0.2.1','root','now','now')`,
		`INSERT INTO projects(id,name,created_at,updated_at) VALUES('project','test','now','now'),('other-project','other','now','now')`,
		`INSERT INTO deployment_targets(id,project_id,server_id,created_at,updated_at) VALUES('one','project','server','now','now'),('two','other-project','server','now','now')`,
		`INSERT INTO deployment_releases(id,deployment_id,action,version,status,created_at) VALUES('release','one','deploy','v1','queued','now')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	store := domain.NewStore(db)
	first := domain.PublishPath{DeploymentID: "one", FQDN: "apps.example.com", PathPrefix: "/demo/", ContainerPort: 8080}
	if err := store.ReservePublishPath(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := store.ReservePublishPath(ctx, domain.PublishPath{DeploymentID: "two", FQDN: first.FQDN, PathPrefix: first.PathPrefix, ContainerPort: 80}); err == nil {
		t.Fatal("another deployment reserved the same path")
	}
	if err := store.SaveReleasePathIntent(ctx, "release", first); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetReleasePathIntent(ctx, "release")
	if err != nil || loaded != first {
		t.Fatalf("release path: %+v, %v", loaded, err)
	}
	if err := store.MarkPublishedPathApplied(ctx, first, "127.0.0.1", 8101, "/cert", "/key", "hash", "nginx:main"); err != nil {
		t.Fatal(err)
	}
	routes, err := store.ListPathRoutes(ctx)
	if err != nil || len(routes) != 1 || routes[0].Status != "active" || routes[0].HostPort != 8101 {
		t.Fatalf("active path: %+v, %v", routes, err)
	}
	if err := store.ReservePublishPath(ctx, domain.PublishPath{DeploymentID: "one", FQDN: "other.example.com", PathPrefix: "/demo/", ContainerPort: 8080}); err == nil {
		t.Fatal("active path changed without removal")
	}
	if err := store.MarkPublishedPathApplied(ctx, first, "127.0.0.1", 8102, "/cert", "/key", "hash2", "nginx:main"); err != nil {
		t.Fatal(err)
	}
	routes, err = store.ListPathRoutes(ctx)
	if err != nil || len(routes) != 1 || routes[0].HostPort != 8102 {
		t.Fatalf("updated path: %+v, %v", routes, err)
	}
}
