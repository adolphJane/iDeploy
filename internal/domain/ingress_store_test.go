package domain_test

import (
	"context"
	"path/filepath"
	"testing"

	"ideploy/internal/domain"
	"ideploy/internal/platform/database"
)

func TestIngressBindingLifecycle(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "ingress.sqlite"))
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
	d, err := store.CreateDomain(ctx, "example.com", "", "")
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := store.ListHosts(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	host := hosts[0]
	if _, err := store.CreateBindingDraft(ctx, host.ID, "deployment", "", 3000, "http", "", "", "caddy"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetBindingJob(ctx, host.ID, "job-1", "applying"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkApplied(ctx, host.ID, "server", "0.0.0.0", 18080, "caddy:2.11.7-alpine", "hash-1", "job-1"); err != nil {
		t.Fatal(err)
	}
	routes, err := store.ListServerRoutes(ctx, "server")
	if err != nil || len(routes) != 1 || routes[0].FQDN != "example.com" {
		t.Fatalf("routes: %+v, %v", routes, err)
	}
	if err := store.SetBindingJob(ctx, host.ID, "job-2", "removing"); err != nil {
		t.Fatal(err)
	}
	routes, err = store.ListServerRoutes(ctx, "server")
	if err != nil || len(routes) != 1 {
		t.Fatalf("removing route should remain until job completes: %+v, %v", routes, err)
	}
	if err := store.MarkRemoved(ctx, host.ID, "server", "caddy:2.11.7-alpine", "", true); err != nil {
		t.Fatal(err)
	}
	if has, err := store.HasServerIngress(ctx, "server"); err != nil || has {
		t.Fatalf("ingress should be removed: %v, %v", has, err)
	}
	if _, err := store.GetBindingForHost(ctx, host.ID); err != domain.ErrNotFound {
		t.Fatalf("binding should be gone: %v", err)
	}
}

func TestNginxBindingCertificatePersistsInServerRoutes(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "nginx-binding.sqlite"))
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
	d, err := store.CreateDomain(ctx, "example.com", "", "")
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := store.ListHosts(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	host := hosts[0]
	if _, err := store.CreateBindingDraft(ctx, host.ID, "deployment", "", 8080, "http", "/cert.pem", "/key.pem", "caddy"); err == nil {
		t.Fatal("accepted Nginx certificate in Caddy mode")
	}
	b, err := store.CreateBindingDraft(ctx, host.ID, "deployment", "", 8080, "http", "/cert.pem", "/key.pem", "nginx")
	if err != nil || b.IngressMode != "nginx" || b.CertificatePath != "/cert.pem" {
		t.Fatalf("draft: %+v, %v", b, err)
	}
	if err = store.SetBindingJob(ctx, host.ID, "job", "applying"); err != nil {
		t.Fatal(err)
	}
	if err = store.MarkApplied(ctx, host.ID, "server", "127.0.0.1", 8080, "nginx:proxy", "hash", "job"); err != nil {
		t.Fatal(err)
	}
	routes, err := store.ListServerRoutes(ctx, "server")
	if err != nil || len(routes) != 1 || routes[0].Certificate != "/cert.pem" || routes[0].CertificateKey != "/key.pem" {
		t.Fatalf("routes: %+v, %v", routes, err)
	}
}
