package sharedserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSharedRuntimePolicyIsScopedToTheDeploymentAndServer(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE shared_resources(kind TEXT,id TEXT,data TEXT,deleted BOOL)`); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 32)
	record, err := json.Marshal(map[string]any{"deployment_id": id, "revision": 1, "policy_json": `{"revision":1,"mode":"enforce","services":["api"]}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO shared_resources VALUES('r.runtime_policies',?,?,0),('r.deployments',?,?,0)`, id, string(record), id, `{"server_id":"selected-server"}`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, driver: "mysql"}
	request := httptest.NewRequest(http.MethodGet, "/v3/runtime-policies/"+id, nil)
	request.SetPathValue("id", id)
	response := httptest.NewRecorder()
	s.getRuntimePolicy(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "enforce") {
		t.Fatalf("failed to read policy: %d", response.Code)
	}
	legacy := httptest.NewRequest(http.MethodPost, "/v2/leases/server:selected-server", strings.NewReader(`{"requestId":"legacy-release-request"}`))
	legacy.SetPathValue("id", "server:selected-server")
	response = httptest.NewRecorder()
	s.acquireLease(response, legacy)
	if response.Code != http.StatusPreconditionFailed {
		t.Fatalf("legacy client received %d when the server requires policy enforcement", response.Code)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for id, want := range map[string]bool{"selected-server": true, "different-server": false} {
		got, err := s.serverHasEnforcedPolicy(ctx, tx, id)
		if err != nil || got != want {
			t.Fatalf("enforcement for %s: %t %v", id, got, err)
		}
	}
}
