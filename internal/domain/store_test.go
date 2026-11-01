package domain_test

import (
	"context"
	"path/filepath"
	"testing"

	"ideploy/internal/domain"
	"ideploy/internal/platform/database"
)

func TestDomainAndHostLifecycle(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "domain.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := domain.NewStore(db)
	created, err := store.CreateDomain(ctx, " EXAMPLE.COM. ", "Production", "")
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "example.com" {
		t.Fatalf("unexpected canonical name: %q", created.Name)
	}
	hosts, err := store.ListHosts(ctx, created.ID)
	if err != nil || len(hosts) != 1 || hosts[0].Label != "@" || hosts[0].FQDN != "example.com" {
		t.Fatalf("expected root host, got %+v, %v", hosts, err)
	}
	if _, err := store.CreateDomain(ctx, "example.com", "", ""); err == nil {
		t.Fatal("expected duplicate domain to be rejected")
	}
	child, err := store.CreateHost(ctx, created.ID, "API", "backend")
	if err != nil || child.FQDN != "api.example.com" {
		t.Fatalf("unexpected child: %+v, %v", child, err)
	}
	if err := store.DeleteDomain(ctx, created.ID); err == nil {
		t.Fatal("expected delete to reject domain with child")
	}
	if err := store.DeleteHost(ctx, hosts[0].ID); err == nil {
		t.Fatal("root host must not be removable")
	}
	if err := store.DeleteHost(ctx, child.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteDomain(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDomainValidation(t *testing.T) {
	for _, input := range []string{"https://example.com", "api.example.com", "example.com:443", "*.example.com", "com"} {
		if _, err := domain.NormalizeDomain(input); err == nil {
			t.Errorf("expected %q to be rejected", input)
		}
	}
	for _, input := range []string{"@", "a.b", "-api", "api/other", ""} {
		if _, err := domain.NormalizeLabel(input); err == nil {
			t.Errorf("expected label %q to be rejected", input)
		}
	}
}

func TestDNSConnectionProviders(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "dns.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := domain.NewStore(db)
	ali, err := store.CreateDNSConnection(ctx, "aliyun", "Ali", "AK-ID")
	if err != nil || ali.Provider != "aliyun" || ali.CredentialRef != domain.DNSCredentialRef(ali.ID) {
		t.Fatalf("legacy AliDNS credential reference changed: %+v, %v", ali, err)
	}
	cf, err := store.CreateDNSConnection(ctx, "cloudflare", "CF", "")
	if err != nil || cf.Provider != "cloudflare" || cf.AccessKeyID != "" || cf.CredentialRef == ali.CredentialRef {
		t.Fatalf("unexpected Cloudflare connection: %+v, %v", cf, err)
	}
	created, err := store.CreateDomain(ctx, "example.com", "", cf.ID)
	if err != nil || created.DNSConnectionID != cf.ID {
		t.Fatalf("domain connection not saved: %+v, %v", created, err)
	}
	updatedAli, err := store.UpdateDNSConnection(ctx, ali.ID, " Ali Updated ", "NEW-AK-ID")
	if err != nil || updatedAli.ID != ali.ID || updatedAli.Provider != "aliyun" ||
		updatedAli.Name != "Ali Updated" || updatedAli.AccessKeyID != "NEW-AK-ID" ||
		updatedAli.CredentialRef != ali.CredentialRef {
		t.Fatalf("AliDNS connection identity changed during edit: %+v, %v", updatedAli, err)
	}
	updatedCF, err := store.UpdateDNSConnection(ctx, cf.ID, "Cloudflare Updated", "")
	if err != nil || updatedCF.ID != cf.ID || updatedCF.Provider != "cloudflare" ||
		updatedCF.CredentialRef != cf.CredentialRef {
		t.Fatalf("Cloudflare connection identity changed during edit: %+v, %v", updatedCF, err)
	}
	linked, err := store.GetDomain(ctx, created.ID)
	if err != nil || linked.DNSConnectionID != cf.ID {
		t.Fatalf("editing a connection broke domain association: %+v, %v", linked, err)
	}
	if _, err := store.UpdateDNSConnection(ctx, ali.ID, "Cloudflare Updated", "NEW-AK-ID"); err == nil {
		t.Fatal("duplicate connection name should be rejected")
	}
	if _, err := store.UpdateDNSConnection(ctx, ali.ID, "Ali Updated", ""); err == nil {
		t.Fatal("empty AliDNS AccessKey ID should be rejected")
	}
	if err := store.DeleteDNSConnection(ctx, cf.ID); err == nil {
		t.Fatal("referenced connection should not be deleted")
	}
}
