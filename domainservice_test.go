package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"ideploy/internal/domain"
	"ideploy/internal/platform/database"
)

func TestUpdateDNSConnectionPreservesExistingCredential(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "domain.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := domain.NewStore(db)
	created, err := store.CreateDNSConnection(ctx, "aliyun", "Original", "OLD-AK-ID")
	if err != nil {
		t.Fatal(err)
	}
	service := &SystemService{db: db}
	updated, err := service.UpdateDNSConnection(created.ID, DNSConnectionInput{
		Provider: "aliyun", Name: "Renamed", AccessKeyID: created.AccessKeyID,
	})
	if err != nil || updated.Name != "Renamed" || updated.CredentialRef != created.CredentialRef {
		t.Fatalf("metadata edit must preserve credential reference: %+v, %v", updated, err)
	}
	if _, err = service.UpdateDNSConnection(created.ID, DNSConnectionInput{
		Provider: "aliyun", Name: "Renamed", AccessKeyID: "NEW-AK-ID",
	}); err == nil || !strings.Contains(err.Error(), "AccessKey Secret") {
		t.Fatalf("changing AccessKey ID without its Secret should fail: %v", err)
	}
	if _, err = service.UpdateDNSConnection(created.ID, DNSConnectionInput{
		Provider: "cloudflare", Name: "Renamed",
	}); err == nil || !strings.Contains(err.Error(), "服务商类型") {
		t.Fatalf("changing provider in place should fail: %v", err)
	}
	current, err := store.GetDNSConnection(ctx, created.ID)
	if err != nil || current.AccessKeyID != created.AccessKeyID || current.Provider != "aliyun" {
		t.Fatalf("rejected edit changed connection: %+v, %v", current, err)
	}
}
