package domain

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	alidns "github.com/alibabacloud-go/alidns-20150109/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	"github.com/alibabacloud-go/tea/dara"
)

func testAliDNSClient(t *testing.T, server *httptest.Server) *alidns.Client {
	t.Helper()
	client, err := alidns.NewClient(&openapi.Config{
		AccessKeyId: dara.String("test-key-id"), AccessKeySecret: dara.String("test-secret"),
		Endpoint: dara.String(strings.TrimPrefix(server.URL, "http://")), Protocol: dara.String("HTTP"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestAliDNSListPaginationAndStatus(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		// A repeat request to enable an enabled record must never submit a write.
		if r.Header.Get("x-acs-action") != "DescribeDomainRecords" || r.Form.Get("DomainName") != "example.com" {
			t.Errorf("unexpected request: action=%s domain=%s", r.Header.Get("x-acs-action"), r.Form.Get("DomainName"))
			http.Error(w, "unexpected action", http.StatusBadRequest)
			return
		}
		requests++
		id, status := "first", "ENABLE"
		if r.Form.Get("PageNumber") == "2" {
			id, status = "second", "DISABLE"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"RequestId": "fixture-request", "TotalCount": 2,
			"DomainRecords": map[string]any{"Record": []map[string]any{{
				"RecordId": id, "RR": "@", "Type": "A", "Value": "192.0.2.1", "TTL": 600,
				"Line": "default", "Status": status, "Locked": false, "UpdateTimestamp": 100,
			}}},
		})
	}))
	defer server.Close()
	client := testAliDNSClient(t, server)
	records, err := ListAliDNSRecords(client, "example.com")
	if err != nil || len(records) != 2 || requests != 2 {
		t.Fatalf("records not fully read: %+v, requests=%d, err=%v", records, requests, err)
	}
	if records[0].Status != "Enable" || records[1].Status != "Disable" {
		t.Fatalf("provider statuses not normalized: %+v", records)
	}
	if _, err := SetAliDNSRecordStatus(client, "example.com", "first", "Enable", 100); err != nil {
		t.Fatalf("idempotent enabled status failed: %v", err)
	}
}

func TestAliDNSPermissionFailureIsNotAnEmptyList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"Code": "Forbidden.RAM", "Message": "Not authorized", "RequestId": "fixture-request"})
	}))
	defer server.Close()
	_, err := ListAliDNSRecords(testAliDNSClient(t, server), "example.com")
	if err == nil || !strings.Contains(err.Error(), "Forbidden.RAM") {
		t.Fatalf("permission failure should be returned: %v", err)
	}
}

func TestAliDNSUpdateWritesAndReadsProviderRecord(t *testing.T) {
	value := "192.0.2.1"
	updatedAt := int64(100)
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Header.Get("x-acs-action") {
		case "DescribeDomainRecords":
			if r.Form.Get("DomainName") != "example.com" {
				t.Errorf("unexpected domain: %s", r.Form.Get("DomainName"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"RequestId": "fixture-request", "TotalCount": 1,
				"DomainRecords": map[string]any{"Record": []map[string]any{{
					"RecordId": "record-1", "RR": "www", "Type": "A", "Value": value,
					"TTL": 600, "Line": "default", "Status": "ENABLE", "UpdateTimestamp": updatedAt,
				}}},
			})
		case "UpdateDomainRecord":
			writes++
			if r.Form.Get("RecordId") != "record-1" || r.Form.Get("RR") != "www" ||
				r.Form.Get("Type") != "A" || r.Form.Get("Value") != "192.0.2.2" ||
				r.Form.Get("TTL") != "600" || r.Form.Get("Line") != "default" {
				t.Errorf("unexpected update fields: %v", r.Form)
			}
			value = r.Form.Get("Value")
			updatedAt = 200
			_ = json.NewEncoder(w).Encode(map[string]any{"RequestId": "fixture-request", "RecordId": "record-1"})
		default:
			t.Errorf("unexpected action: %s", r.Header.Get("x-acs-action"))
			http.Error(w, "unexpected action", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	client := testAliDNSClient(t, server)

	_, err := UpdateAliDNSRecord(client, "example.com", "record-1", DNSRecordInput{
		RR: "www", Type: "A", Value: "192.0.2.2", TTL: 600, Line: "default",
	}, 99)
	if err == nil || !strings.Contains(err.Error(), "发生变化") || writes != 0 {
		t.Fatalf("stale edit must not write: writes=%d, err=%v", writes, err)
	}

	result, err := UpdateAliDNSRecord(client, "example.com", "record-1", DNSRecordInput{
		RR: "www", Type: "A", Value: "192.0.2.2", TTL: 600, Line: "default",
	}, 100)
	if err != nil || result.Value != "192.0.2.2" || result.UpdatedAt != 200 || writes != 1 {
		t.Fatalf("provider update was not read back: record=%+v, writes=%d, err=%v", result, writes, err)
	}
}

func TestDNSRecordInputValidation(t *testing.T) {
	valid := []DNSRecordInput{
		{RR: "@", Type: "A", Value: "192.0.2.1", TTL: 600},
		{RR: "api", Type: "AAAA", Value: "2001:db8::1", TTL: 600},
		{RR: "www", Type: "CNAME", Value: "target.example.com.", TTL: 600},
		{RR: "_acme-challenge", Type: "TXT", Value: "proof", TTL: 600},
	}
	for _, item := range valid {
		if _, err := item.Validate(); err != nil {
			t.Errorf("expected %+v to pass: %v", item, err)
		}
	}
	invalid := []DNSRecordInput{
		{RR: "@", Type: "A", Value: "example.com"},
		{RR: "api", Type: "CNAME", Value: "https://example.com"},
		{RR: "api", Type: "CNAME", Value: "bad_name.example.com"},
		{RR: "api", Type: "MX", Value: "mail.example.com"},
	}
	for _, item := range invalid {
		if _, err := item.Validate(); err == nil {
			t.Errorf("expected %+v to fail", item)
		}
	}
}
