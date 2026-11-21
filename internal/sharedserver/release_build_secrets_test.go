package sharedserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ideploy/internal/sharedworkspace"
)

func TestBuildSecretsRequireLeaseAndMatchingEnvironmentRevision(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`CREATE TABLE shared_resources(kind TEXT,id TEXT,deleted BOOLEAN,data TEXT,PRIMARY KEY(kind,id))`,
		`CREATE TABLE shared_vault(id INTEGER PRIMARY KEY,configuration TEXT)`,
		`CREATE TABLE shared_secrets(id TEXT PRIMARY KEY,envelope TEXT)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := ensureReleaseJobSchema(ctx, db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	jobID, deploymentID, releaseID, variableID, workerID := strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32), strings.Repeat("d", 32), strings.Repeat("e", 32)
	for _, item := range []struct{ kind, id, data string }{
		{"r.deployments", deploymentID, `{"environment_revision":2}`},
		{"r.releases", releaseID, `{"deployment_id":"` + deploymentID + `","environment_revision":2}`},
		{"r.env", variableID, `{"deployment_id":"` + deploymentID + `","name":"PACKAGE_TOKEN","kind":"secret","build_secret":1}`},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO shared_resources(kind,id,deleted,data) VALUES(?,?,0,?)`, item.kind, item.id, item.data); err != nil {
			t.Fatal(err)
		}
	}
	config, key, err := sharedworkspace.CreateVault()
	if err != nil {
		t.Fatal(err)
	}
	configJSON, _ := json.Marshal(config)
	if _, err := db.ExecContext(ctx, `INSERT INTO shared_vault(id,configuration) VALUES(1,?)`, string(configJSON)); err != nil {
		t.Fatal(err)
	}
	secretID := "r.env:" + variableID + ":credential"
	envelope, err := sharedworkspace.EncryptSecret(key, secretID, "private-build-value")
	if err != nil {
		t.Fatal(err)
	}
	envelopeJSON, _ := json.Marshal(envelope)
	if _, err := db.ExecContext(ctx, `INSERT INTO shared_secrets(id,envelope) VALUES(?,?)`, secretID, string(envelopeJSON)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO release_jobs(id,request_id,deployment_id,release_id,delivery_commit,delivery_digest,repository_url,version,registry_id,registry_host,registry_user,status,stage,lease_owner,lease_generation,lease_until,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		jobID, strings.Repeat("f", 32), deploymentID, releaseID, strings.Repeat("1", 40), strings.Repeat("2", 64), "https://example.com/repo.git", "v1", strings.Repeat("3", 32), "registry.example", "user", "running", "building", workerID, 1, releaseTime(time.Now().Add(time.Minute)), releaseTime(time.Now()), releaseTime(time.Now())); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, driver: "mysql"}
	call := func(owner string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/v3/worker/releases/"+jobID+"/build-secrets", nil)
		req.SetPathValue("id", jobID)
		req.Header.Set("X-iDeploy-Worker-ID", owner)
		req.Header.Set("X-iDeploy-Lease-Generation", "1")
		response := httptest.NewRecorder()
		s.releaseBuildSecretsHTTP(response, req)
		return response
	}
	if response := call(strings.Repeat("0", 32)); response.Code != http.StatusConflict {
		t.Fatalf("wrong worker status: %d", response.Code)
	}
	response := call(workerID)
	if response.Code != http.StatusOK {
		t.Fatalf("valid worker status: %d %s", response.Code, response.Body.String())
	}
	var result struct {
		Secrets map[string]string `json:"secrets"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Secrets["PACKAGE_TOKEN"] != "private-build-value" {
		t.Fatalf("secret response: %#v %v", result, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE shared_resources SET data=? WHERE kind='r.deployments' AND id=?`, `{"environment_revision":3}`, deploymentID); err != nil {
		t.Fatal(err)
	}
	if response := call(workerID); response.Code != http.StatusConflict {
		t.Fatalf("changed environment status: %d", response.Code)
	}
}
