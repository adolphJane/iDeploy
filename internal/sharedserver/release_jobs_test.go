package sharedserver

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestEmptyQueuePersistsExpiredReleaseLease(t *testing.T) {
	ctx := context.Background()
	s, input := releaseJobFixture(t)
	queued, err := s.createReleaseJob(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	worker := strings.Repeat("f", 32)
	if _, err := s.claimReleaseJob(ctx, worker, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE release_jobs SET lease_until=? WHERE id=?`, releaseTime(time.Now().Add(-time.Second)), queued.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.claimReleaseJob(ctx, worker, time.Minute); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("empty claim: %v", err)
	}
	var status string
	if err := s.db.QueryRow("SELECT status FROM release_jobs WHERE id=?", queued.ID).Scan(&status); err != nil || status != "needs_reconcile" {
		t.Fatalf("expired release status = %q, err = %v", status, err)
	}
}

func TestRemoteCancellationFencesSubmissionAndPreservesPushedArtifact(t *testing.T) {
	ctx := context.Background()
	t.Run("cancel before submit", func(t *testing.T) {
		s, input := releaseJobFixture(t)
		if err := s.cancelReleaseJob(ctx, input.RequestID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.createReleaseJob(ctx, input); err == nil {
			t.Fatal("a canceled request enqueued a build")
		}
	})
	t.Run("cancel queued", func(t *testing.T) {
		s, input := releaseJobFixture(t)
		release, err := s.createReleaseJob(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.cancelReleaseJob(ctx, input.RequestID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.claimReleaseJob(ctx, strings.Repeat("f", 32), time.Minute); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("canceled release was claimable: %v", err)
		}
		result, err := s.createReleaseJob(ctx, input)
		if err != nil || result.ID != release.ID || result.Status != "canceled" || result.Stage != "finished" {
			t.Fatalf("duplicate submit after cancellation = %#v, %v", result, err)
		}
	})
	t.Run("cancel after push started", func(t *testing.T) {
		s, input := releaseJobFixture(t)
		if _, err := s.createReleaseJob(ctx, input); err != nil {
			t.Fatal(err)
		}
		worker := strings.Repeat("f", 32)
		task, err := s.claimReleaseJob(ctx, worker, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.updateReleaseJob(ctx, task.ID, worker, task.LeaseGeneration, "pushing", "running", "", time.Minute); err != nil {
			t.Fatal(err)
		}
		if err := s.cancelReleaseJob(ctx, input.RequestID); err != nil {
			t.Fatal(err)
		}
		if err := s.updateReleaseJob(ctx, task.ID, worker, task.LeaseGeneration, "", "running", "", time.Minute); err == nil {
			t.Fatal("worker heartbeat continued after cancellation")
		}
		digest := "registry.example/team/app@sha256:" + strings.Repeat("a", 64)
		if err := s.updateReleaseJob(ctx, task.ID, worker, task.LeaseGeneration, "artifact_ready", "artifact_ready", digest, time.Minute); err != nil {
			t.Fatal(err)
		}
		if err := s.cancelReleaseJob(ctx, input.RequestID); err != nil {
			t.Fatal(err)
		}
		result, err := s.createReleaseJob(ctx, input)
		if err != nil || result.Status != "artifact_ready" || result.ImageRef != digest {
			t.Fatalf("pushed artifact lost = %#v, %v", result, err)
		}
	})
}

func releaseJobFixture(t *testing.T) (*Server, RemoteReleaseJob) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	if err := ensureReleaseJobSchema(context.Background(), db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	return &Server{db: db}, RemoteReleaseJob{
		RequestID: strings.Repeat("a", 32), DeploymentID: strings.Repeat("b", 32), ReleaseID: strings.Repeat("c", 32),
		DeliveryCommit: strings.Repeat("d", 40), DeliveryDigest: strings.Repeat("e", 64),
		RepositoryURL: "https://example.com/repo.git", Version: "1.0.0",
		RegistryID: strings.Repeat("1", 32), RegistryHost: "registry.example", RegistryUser: "builder",
	}
}

func TestReleaseJobChecksSharedResources(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE shared_resources(kind TEXT,id TEXT,deleted BOOLEAN,data TEXT,PRIMARY KEY(kind,id))`); err != nil {
		t.Fatal(err)
	}
	if err := ensureReleaseJobSchema(context.Background(), db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	id, project, release, registryID := strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32), strings.Repeat("d", 32)
	for _, item := range []struct{ kind, id, data string }{
		{"r.deployments", id, `{"project_id":"` + project + `"}`},
		{"r.project_sources", project, `{"kind":"git","repository_url":"https://example.com/app.git"}`},
		{"r.releases", release, `{"deployment_id":"` + id + `","version":"v1","build_mode":"remote"}`},
		{"r.registries", registryID, `{"endpoint":"https://registry.example","username":"push"}`},
	} {
		if _, err := db.Exec(`INSERT INTO shared_resources(kind,id,deleted,data) VALUES(?,?,0,?)`, item.kind, item.id, item.data); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{db: db, driver: "mysql"}
	job := RemoteReleaseJob{RequestID: strings.Repeat("e", 32), DeploymentID: id, ReleaseID: release, DeliveryCommit: strings.Repeat("f", 40), DeliveryDigest: strings.Repeat("1", 64), RepositoryURL: "https://example.com/app.git", Version: "v1", RegistryID: registryID, RegistryHost: "registry.example", RegistryUser: "push"}
	if err := s.validateReleaseJobResources(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	job.RepositoryURL = "https://attacker.example/app.git"
	if err := s.validateReleaseJobResources(context.Background(), job); err == nil {
		t.Fatal("accepted a different source repository")
	}
}

func TestWorkerEndpointRequiresSeparateToken(t *testing.T) {
	s := &Server{}
	if err := s.SetWorkerToken(strings.Repeat("w", 32)); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", strings.Repeat("a", 32)} {
		req := httptest.NewRequest(http.MethodPost, "/v3/worker/releases/claim", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, req)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("token %q returned %d", token, response.Code)
		}
	}
}

func TestReleaseLookupByOriginalRequestID(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := ensureReleaseJobSchema(context.Background(), db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	jobID, requestID := strings.Repeat("a", 32), strings.Repeat("b", 32)
	if _, err := db.Exec(`INSERT INTO release_jobs(id,request_id,deployment_id,release_id,delivery_commit,delivery_digest,repository_url,version,registry_id,registry_host,registry_user,status,stage,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		jobID, requestID, strings.Repeat("c", 32), strings.Repeat("d", 32), strings.Repeat("e", 40), strings.Repeat("f", 64), "https://example.com/repo.git", "1.0.0", strings.Repeat("1", 32), "registry.example", "user", "artifact_ready", "artifact_ready", "2026-01-01", "2026-01-01"); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, driver: "mysql"}
	req := httptest.NewRequest(http.MethodGet, "/v3/releases/by-request/"+requestID, nil)
	req.SetPathValue("id", requestID)
	response := httptest.NewRecorder()
	s.getReleaseJobByRequestHTTP(response, req)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), jobID) {
		t.Fatalf("lookup failed: %d %s", response.Code, response.Body.String())
	}
}

func TestRemoteReleaseJobRejectsInvalidReferences(t *testing.T) {
	valid := RemoteReleaseJob{RequestID: strings.Repeat("a", 32), DeploymentID: strings.Repeat("b", 32), ReleaseID: strings.Repeat("c", 32), DeliveryCommit: strings.Repeat("d", 40), DeliveryDigest: strings.Repeat("e", 64), RepositoryURL: "https://example.com/repo.git", Version: "v1", RegistryID: strings.Repeat("f", 32), RegistryHost: "registry.example", RegistryUser: "push"}
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	valid.DeliveryCommit = "main"
	if err := valid.validate(); err == nil {
		t.Fatal("accepted mutable branch name in place of commit")
	}
}
