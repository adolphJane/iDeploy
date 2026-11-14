package domain_test

import (
	"context"
	"path/filepath"
	"testing"

	"ideploy/internal/domain"
	"ideploy/internal/platform/database"
)

func TestInternalIngressJobStateAndRecovery(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "ingress.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `INSERT INTO servers(id,name,host,ssh_user,created_at,updated_at) VALUES('server','test','192.0.2.1','root','now','now')`); err != nil {
		t.Fatal(err)
	}
	store := domain.NewStore(db)
	entry, err := store.SaveInternalIngressDraft(ctx, domain.InternalIngress{FQDN: "apps.example.com", ServerID: "server", GatewayPort: 8766})
	if err != nil || entry.Status != "draft" {
		t.Fatalf("draft: %+v, %v", entry, err)
	}
	if err := store.SetInternalIngressJob(ctx, entry.FQDN, "job-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteInternalIngressDraft(ctx, entry.FQDN); err == nil {
		t.Fatal("deleted running ingress")
	}
	if err := store.MarkInternalIngressReady(ctx, entry.FQDN, "server", "wrong-job", "hash", "main"); err == nil {
		t.Fatal("wrong job marked ingress ready")
	}
	if err := store.SetInternalIngressFailure(ctx, entry.FQDN, "job-1", "remote failed"); err != nil {
		t.Fatal(err)
	}
	entry, err = store.GetInternalIngress(ctx, entry.FQDN)
	if err != nil || entry.Status != "failed" || entry.Detail != "remote failed" {
		t.Fatalf("failure: %+v, %v", entry, err)
	}
	if err := store.ReconcileInternalIngressReady(ctx, entry.FQDN, "server", "actual-hash", "main"); err != nil {
		t.Fatal(err)
	}
	entry, err = store.GetInternalIngress(ctx, entry.FQDN)
	if err != nil || entry.Status != "ready" {
		t.Fatalf("recovered: %+v, %v", entry, err)
	}
	ingress, err := store.GetIngress(ctx, "server")
	if err != nil || ingress.ConfigHash != "actual-hash" {
		t.Fatalf("ingress hash: %+v, %v", ingress, err)
	}
}
