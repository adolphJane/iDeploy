package domain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudflareRecordLifecycle(t *testing.T) {
	current := cloudflareRecord{
		ID: "record1", Name: "api.example.com", Type: "A", Content: "192.0.2.1",
		TTL: 600, ModifiedOn: "2026-10-21T08:00:00Z",
	}
	var method, path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing API token")
		}
		method, path = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/zones":
			if r.URL.Query().Get("name") != "example.com" {
				t.Error("wrong zone name")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []map[string]string{{"id": "zone1", "name": "example.com"}}})
		case r.Method == "GET" && r.URL.Path == "/zones/zone1/dns_records":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []cloudflareRecord{current}, "result_info": map[string]int{"total_pages": 1}})
		case r.Method == "PATCH" && r.URL.Path == "/zones/zone1/dns_records/record1":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["name"] != "api.example.com" || body["content"] != "192.0.2.2" {
				t.Errorf("wrong update body: %+v", body)
			}
			current.Content = "192.0.2.2"
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": current})
		case r.Method == "DELETE" && r.URL.Path == "/zones/zone1/dns_records/record1":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]string{"id": "record1"}})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewCloudflareDNSClient("test-token")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	client.http = server.Client()
	ctx := context.Background()
	records, err := client.ListRecords(ctx, "example.com")
	if err != nil || len(records) != 1 || records[0].RR != "api" || records[0].UpdatedAt == 0 {
		t.Fatalf("unexpected records: %+v, %v", records, err)
	}
	_, err = client.UpdateRecord(ctx, "example.com", "record1", DNSRecordInput{
		RR: "api", Type: "A", Value: "192.0.2.2", TTL: 600,
	}, records[0].UpdatedAt+1)
	if err == nil || !strings.Contains(err.Error(), "发生变化") {
		t.Fatalf("expected concurrency check: %v", err)
	}
	updated, err := client.UpdateRecord(ctx, "example.com", "record1", DNSRecordInput{
		RR: "api", Type: "A", Value: "192.0.2.2", TTL: 600,
	}, records[0].UpdatedAt)
	if err != nil || updated.Value != "192.0.2.2" || method != "PATCH" || path != "/zones/zone1/dns_records/record1" {
		t.Fatalf("unexpected update: %+v, %v", updated, err)
	}
	if err := client.DeleteRecord(ctx, "example.com", "record1", updated.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if method != "DELETE" {
		t.Fatalf("expected DELETE, got %s", method)
	}
}

func TestCloudflareConnectionTestUsesReadOnlyRequests(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected connection test request: %s %s", r.Method, r.URL.Path)
		}
		paths = append(paths, r.URL.String())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/zones":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []map[string]string{{"id": "zone1", "name": "example.com"}}})
		case "/zones/zone1/dns_records":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []cloudflareRecord{}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewCloudflareDNSClient("test-token")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	client.http = server.Client()
	if err := client.TestAccess(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if err := client.TestAccess(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 3 || paths[0] != "/zones?per_page=1" || paths[1] != "/zones?name=example.com&per_page=50" || paths[2] != "/zones/zone1/dns_records?page=1&per_page=1" {
		t.Fatalf("unexpected test requests: %v", paths)
	}
}

func TestCloudflareCredentialsAndZoneLookup(t *testing.T) {
	if _, err := NewCloudflareDNSClient(""); err == nil {
		t.Fatal("empty token accepted")
	}
	if got := DNSCredentialRefForProvider("aliyun", "id"); got != DNSCredentialRef("id") {
		t.Fatalf("existing AliDNS references changed: %q", got)
	}
	if _, service, ok := credentialAccount(DNSCredentialRefForProvider("cloudflare", "id")); !ok || service != cloudflareSecretService {
		t.Fatal("Cloudflare credential should use its own Keychain service")
	}
}

func TestCloudflareAddRecordAndRejectNonDefaultLine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/zones":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []map[string]string{{"id": "zone1", "name": "example.com"}}})
		case r.Method == "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []cloudflareRecord{}, "result_info": map[string]int{"total_pages": 1}})
		case r.Method == "POST":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["name"] != "example.com" || body["ttl"] != float64(1) {
				t.Errorf("wrong create body: %+v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": cloudflareRecord{
				ID: "new", Name: "example.com", Type: "A", Content: "192.0.2.3", TTL: 1,
			}})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	client, _ := NewCloudflareDNSClient("test-token")
	client.baseURL = server.URL
	client.http = server.Client()
	input := DNSRecordInput{RR: "@", Type: "A", Value: "192.0.2.3", TTL: 0}
	record, err := client.AddRecord(context.Background(), "example.com", input)
	if err != nil || record.ID != "new" || record.RR != "@" {
		t.Fatalf("unexpected create: %+v, %v", record, err)
	}
	input.Line = "telecom"
	if _, err := client.AddRecord(context.Background(), "example.com", input); err == nil {
		t.Fatal("Cloudflare must reject a non-default line")
	}
}
