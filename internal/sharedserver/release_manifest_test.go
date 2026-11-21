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

func TestComposeManifestEncryptedAtRestAndReadForRelease(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := ensureReleaseJobSchema(ctx, db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE shared_vault(id INTEGER PRIMARY KEY, configuration TEXT)`); err != nil {
		t.Fatal(err)
	}
	config, _, err := sharedworkspace.CreateVault()
	if err != nil {
		t.Fatal(err)
	}
	configJSON, _ := json.Marshal(config)
	if _, err := db.Exec(`INSERT INTO shared_vault(id,configuration) VALUES(1,?)`, string(configJSON)); err != nil {
		t.Fatal(err)
	}
	id, owner := strings.Repeat("a", 32), strings.Repeat("b", 32)
	if _, err := db.Exec(`INSERT INTO release_jobs(id,request_id,deployment_id,release_id,delivery_commit,delivery_digest,repository_url,version,registry_id,registry_host,registry_user,status,stage,lease_owner,lease_generation,lease_until,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, strings.Repeat("c", 32), strings.Repeat("d", 32), strings.Repeat("e", 32), strings.Repeat("f", 40), strings.Repeat("1", 64), "https://example.com/repo.git", "1.0.0", strings.Repeat("2", 32), "registry.example", "user", "running", "building", owner, 1, releaseTime(time.Now().Add(time.Minute)), releaseTime(time.Now()), releaseTime(time.Now())); err != nil {
		t.Fatal(err)
	}
	prefix := `{"services":{"api":{"image":"registry.example/api@sha256:` + strings.Repeat("3", 64) + `","environment":{"PASSWORD":"sample-secret","PAYLOAD":"`
	const suffix = `"}}}}`
	manifest := prefix + strings.Repeat("x", 900*1024-len(prefix)-len(suffix)) + suffix
	s := &Server{db: db, driver: "mysql"}
	if err := s.updateComposeArtifact(ctx, id, owner, 1, "artifact_ready", "artifact_ready", "compose:api:1.0.0", manifest); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := db.QueryRow(`SELECT manifest_envelope FROM release_jobs WHERE id=?`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "sample-secret") || raw == "" {
		t.Fatal("manifest was stored in plaintext or missing")
	}
	secretID := "r.releases:" + strings.Repeat("e", 32) + ":manifest"
	if len(raw) <= 65535 || len(raw) > secretEnvelopeLimit(secretID) {
		t.Fatalf("encoded snapshot size %d does not match sync storage bounds", len(raw))
	}
	req := httptest.NewRequest(http.MethodGet, "/v3/releases/"+id, nil)
	req.SetPathValue("id", id)
	response := httptest.NewRecorder()
	s.getReleaseJobHTTP(response, req)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "sample-secret") {
		t.Fatalf("manifest read failed: HTTP %d", response.Code)
	}
}
