package domain_test

import (
	"context"
	"path/filepath"
	"testing"

	"ideploy/internal/domain"
	"ideploy/internal/platform/database"
)

func TestExternalRouteLifecycleAndBindingExclusion(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "external.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`INSERT INTO servers(id,name,host,ssh_user,created_at,updated_at) VALUES('server','test','192.0.2.1','root','now','now')`,
		`INSERT INTO projects(id,name,created_at,updated_at) VALUES('project','test','now','now')`,
		`INSERT INTO deployment_targets(id,project_id,server_id,created_at,updated_at) VALUES('deployment','project','server','now','now')`,
	} {
		if _, err = db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	store := domain.NewStore(db)
	root, err := store.CreateDomain(ctx, "example.com", "", "")
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := store.ListHosts(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	host := hosts[0]
	draft, err := store.SaveExternalRouteDraft(ctx, domain.ExternalRoute{HostID: host.ID, ServerID: "server", HostIP: "127.0.0.1", HostPort: 8080, UpstreamScheme: "http", CertificatePath: "/cert.pem", CertificateKeyPath: "/key.pem"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateBindingDraft(ctx, host.ID, "deployment", "", 8080, "http", "", "", "caddy"); err == nil {
		t.Fatal("deployment binding overlapped external route")
	}
	if err = store.SetExternalRouteJob(ctx, host.ID, "job-1", "applying"); err != nil {
		t.Fatal(err)
	}
	if err = store.MarkExternalRouteApplied(ctx, host.ID, "server", "hash", "nginx:dev-nginx", "job-1"); err != nil {
		t.Fatal(err)
	}
	routes, err := store.ListServerRoutes(ctx, "server")
	if err != nil || len(routes) != 1 || routes[0].Certificate != "/cert.pem" || routes[0].HostPort != 8080 {
		t.Fatalf("route: %+v, %v", routes, err)
	}
	if _, err = store.SaveExternalRouteDraft(ctx, draft); err == nil {
		t.Fatal("changed active route")
	}
	if err = store.SetExternalRouteJob(ctx, host.ID, "job-2", "removing"); err != nil {
		t.Fatal(err)
	}
	if err = store.MarkExternalRouteRemoved(ctx, host.ID, "server", "", "", true); err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetExternalRoute(ctx, host.ID); err != domain.ErrNotFound {
		t.Fatalf("route remained: %v", err)
	}
}
