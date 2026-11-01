package domain_test

import (
	"context"
	"path/filepath"
	"testing"

	"ideploy/internal/domain"
	"ideploy/internal/platform/database"
)

func TestDNSRecordsIdentifyServiceHostnames(t *testing.T) {
	for _, tt := range []struct {
		rr, kind, want string
	}{
		{"@", "A", "example.com"},
		{"IFX", "A", "ifx.example.com"},
		{"ifx", "AAAA", "ifx.example.com"},
		{"api.dev", "CNAME", "api.dev.example.com"},
		{"*", "A", ""},
		{"*.dev", "CNAME", ""},
		{"_acme-challenge", "TXT", ""},
		{"mail", "MX", ""},
		{"_service", "A", ""},
		{"", "A", ""},
		{"api/other", "A", ""},
	} {
		t.Run(tt.rr+"/"+tt.kind, func(t *testing.T) {
			records := domain.WithDNSBindingInfo("example.com", []domain.DNSRecord{{RR: tt.rr, Type: tt.kind, Value: "target.other.example"}})
			got := records[0]
			if got.FQDN != tt.want || got.BindingEligible != (tt.want != "") {
				t.Fatalf("wrong service binding candidate: %+v", got)
			}
			if !got.BindingEligible && got.BindingDisabledReason == "" {
				t.Fatal("ineligible record must explain why")
			}
		})
	}
}

func TestDNSRecordBindingReusesHostAndPreservesBinding(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "dns-binding.sqlite"))
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
	hosts, _ := store.ListHosts(ctx, d.ID)
	rootID := hosts[0].ID
	root, err := store.EnsureDNSRecordHost(ctx, d.ID, domain.DNSRecord{RR: "@", Type: "A"})
	if err != nil || root.ID != rootID {
		t.Fatalf("root host should be reused: %+v, %v", root, err)
	}
	local, err := store.CreateHost(ctx, d.ID, "ifx", "keep this note")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := store.CreateBindingDraft(ctx, local.ID, "deployment", "service", 8080, "http", "", "", "caddy")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"A", "AAAA", "CNAME"} {
		got, err := store.EnsureDNSRecordHost(ctx, d.ID, domain.DNSRecord{RR: "IFX", Type: kind})
		if err != nil || got.ID != local.ID || got.Note != local.Note || got.Binding == nil ||
			got.Binding.ID != binding.ID || got.Binding.UpdatedAt != binding.UpdatedAt || got.Binding.ContainerPort != 8080 {
			t.Fatalf("existing draft must survive DNS matching: %+v, %v", got, err)
		}
	}
	if err := store.SetBindingJob(ctx, local.ID, "job", "applying"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkApplied(ctx, local.ID, "server", "0.0.0.0", 18080, "test-image", "config-hash", "job"); err != nil {
		t.Fatal(err)
	}
	active, err := store.EnsureDNSRecordHost(ctx, d.ID, domain.DNSRecord{RR: "ifx", Type: "AAAA"})
	if err != nil || active.Binding == nil || active.Binding.Status != "active" || active.Binding.ID != binding.ID {
		t.Fatalf("matching DNS must preserve the active route: %+v, %v", active, err)
	}
	for i := 0; i < 2; i++ {
		got, err := store.EnsureDNSRecordHost(ctx, d.ID, domain.DNSRecord{RR: "api.dev", Type: "A"})
		if err != nil || got.FQDN != "api.dev.example.com" {
			t.Fatalf("nested DNS hostname was not resolved: %+v, %v", got, err)
		}
	}
	if _, err := store.EnsureDNSRecordHost(ctx, d.ID, domain.DNSRecord{RR: "*", Type: "A"}); err == nil {
		t.Fatal("wildcard must not create a service host")
	}
	hosts, err = store.ListHosts(ctx, d.ID)
	if err != nil || len(hosts) != 3 {
		t.Fatalf("duplicate DNS records must not duplicate hosts: %+v, %v", hosts, err)
	}
}
