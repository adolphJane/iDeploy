package sharedserver

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRejectSecretsNested(t *testing.T) {
	for _, value := range []map[string]any{
		{"credentialRef": "keychain:server:one"},
		{"build": map[string]any{"api_key": "hidden"}},
		{"variables": []any{map[string]any{"plainValue": "hidden"}}},
	} {
		if err := rejectSecrets(value); err == nil {
			t.Fatalf("accepted sensitive field: %#v", value)
		}
	}
	if err := rejectSecrets(map[string]any{"name": "sample", "build": map[string]any{"dockerfilePath": "Dockerfile"}}); err != nil {
		t.Fatalf("rejected ordinary configuration: %v", err)
	}
}

func TestPortableResourceRejectsRepositoryCredentials(t *testing.T) {
	for _, raw := range []string{
		`{"project_id":"p","repository_url":"https://user@example.com/repo.git"}`,
		`{"project_id":"p","repository_url":"https://example.com/repo.git?x=1"}`,
	} {
		if err := validateImportRecord(importRecord{Kind: "r.project_sources", ID: "p", Data: []byte(raw)}); err == nil {
			t.Fatalf("accepted credential-bearing URL: %s", raw)
		}
	}
}

func TestPortableInternalIngressAcceptsFullLengthDomain(t *testing.T) {
	id := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + ".example.com"
	data, err := json.Marshal(map[string]any{"fqdn": id, "server_id": "server", "gateway_port": 8766, "status": "draft", "updated_at": "2026-01-01"})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateImportRecord(importRecord{Kind: "r.internal_ingresses", ID: id, Data: data}); err != nil {
		t.Fatalf("rejected portable ingress domain: %v", err)
	}
}
