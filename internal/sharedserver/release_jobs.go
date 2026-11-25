package sharedserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ideploy/internal/registry"
	"ideploy/internal/sharedworkspace"
)

func (s *Server) createReleaseJobHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.workerEnabled {
		http.Error(w, "remote build worker is not configured", http.StatusServiceUnavailable)
		return
	}
	var input RemoteReleaseJob
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&input); err != nil || input.validate() != nil {
		http.Error(w, "invalid release job", http.StatusBadRequest)
		return
	}
	if err := s.validateReleaseJobResources(r.Context(), input); err != nil {
		http.Error(w, "release resources do not match shared workspace", http.StatusConflict)
		return
	}
	job, err := s.createReleaseJob(r.Context(), input)
	if err != nil {
		http.Error(w, "cannot enqueue release job", http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(job)
}

func (s *Server) getReleaseJobHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !releaseIDPattern.MatchString(id) {
		http.Error(w, "invalid job id", http.StatusBadRequest)
		return
	}
	s.readReleaseJobHTTP(w, r, "id", id)
}

func (s *Server) getReleaseJobByRequestHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !releaseIDPattern.MatchString(id) {
		http.Error(w, "invalid request id", http.StatusBadRequest)
		return
	}
	s.readReleaseJobHTTP(w, r, "request_id", id)
}

func (s *Server) readReleaseJobHTTP(w http.ResponseWriter, r *http.Request, field, id string) {
	if err := s.expireReleaseLeases(r.Context(), time.Now()); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	var job RemoteReleaseJob
	var rawManifest string
	err := s.db.QueryRowContext(r.Context(), s.query(`SELECT id,request_id,deployment_id,release_id,delivery_commit,delivery_digest,
		repository_url,version,registry_id,registry_host,registry_user,image_ref,COALESCE(manifest_envelope,''),status,stage,created_at,updated_at
		FROM release_jobs WHERE `+field+`=$1`), id).Scan(&job.ID, &job.RequestID, &job.DeploymentID, &job.ReleaseID, &job.DeliveryCommit, &job.DeliveryDigest,
		&job.RepositoryURL, &job.Version, &job.RegistryID, &job.RegistryHost, &job.RegistryUser, &job.ImageRef, &rawManifest, &job.Status, &job.Stage, &job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	if rawManifest != "" {
		manifest, err := s.decryptReleaseManifest(r.Context(), job.ID, rawManifest)
		if err != nil {
			http.Error(w, "manifest unavailable", http.StatusServiceUnavailable)
			return
		}
		job.Manifest = manifest
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(job)
}

func (s *Server) cancelReleaseJobHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !releaseIDPattern.MatchString(id) {
		http.Error(w, "invalid request id", http.StatusBadRequest)
		return
	}
	if err := s.cancelReleaseJob(r.Context(), id); err != nil {
		http.Error(w, "cannot cancel release job", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) cancelReleaseJob(ctx context.Context, requestID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.lockReleaseRequest(ctx, tx, requestID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.query(`UPDATE release_job_requests SET canceled=true WHERE request_id=$1`), requestID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.query(`UPDATE release_jobs SET
		stage=CASE WHEN status='queued' THEN 'finished' ELSE stage END,
		status=CASE WHEN status='queued' THEN 'canceled' ELSE 'cancel_requested' END, updated_at=$1
		WHERE request_id=$2 AND status IN ('queued','running')`), releaseTime(time.Now()), requestID); err != nil {
		return err
	}
	return tx.Commit()
}

// Serialize submit and cancel even if the first submit response was lost, or
// if cancellation reaches the server before the original submit completes.
func (s *Server) lockReleaseRequest(ctx context.Context, tx *sql.Tx, requestID string) error {
	query := `INSERT INTO release_job_requests(request_id,canceled) VALUES($1,false)
		ON CONFLICT(request_id) DO UPDATE SET request_id=excluded.request_id`
	if s.driver == "mysql" {
		query = `INSERT INTO release_job_requests(request_id,canceled) VALUES($1,false)
			ON DUPLICATE KEY UPDATE request_id=release_job_requests.request_id`
	}
	_, err := tx.ExecContext(ctx, s.query(query), requestID)
	return err
}

func (s *Server) validateReleaseJobResources(ctx context.Context, job RemoteReleaseJob) error {
	read := func(kind, id string) (map[string]any, error) {
		var raw []byte
		err := s.db.QueryRowContext(ctx, s.query(`SELECT data FROM shared_resources WHERE kind=$1 AND id=$2 AND deleted=false`), kind, id).Scan(&raw)
		if err != nil {
			return nil, err
		}
		var value map[string]any
		err = json.Unmarshal(raw, &value)
		return value, err
	}
	deployment, err := read("r.deployments", job.DeploymentID)
	if err != nil {
		return err
	}
	projectID, _ := deployment["project_id"].(string)
	if !releaseIDPattern.MatchString(projectID) {
		return errors.New("deployment project missing")
	}
	project, err := read("r.project_sources", projectID)
	if err != nil {
		return err
	}
	if project["kind"] != "git" || project["repository_url"] != job.RepositoryURL {
		return errors.New("project Git source mismatch")
	}
	release, err := read("r.releases", job.ReleaseID)
	if err != nil {
		return err
	}
	tag, _ := release["image_tag"].(string)
	if tag == "" {
		tag, _ = release["version"].(string)
	}
	if release["deployment_id"] != job.DeploymentID || tag != job.Version || release["build_mode"] != "remote" {
		return errors.New("release snapshot mismatch")
	}
	reg, err := read("r.registries", job.RegistryID)
	if err != nil {
		return err
	}
	endpoint, _ := reg["endpoint"].(string)
	if registry.DockerHost(endpoint) != job.RegistryHost || reg["username"] != job.RegistryUser {
		return errors.New("registry mismatch")
	}
	return nil
}

func (s *Server) releaseRegistrySecretHTTP(w http.ResponseWriter, r *http.Request) {
	s.releaseSecretHTTP(w, r, "registry")
}

func (s *Server) releaseGitSecretHTTP(w http.ResponseWriter, r *http.Request) {
	s.releaseSecretHTTP(w, r, "git")
}

func (s *Server) releaseSecretHTTP(w http.ResponseWriter, r *http.Request, purpose string) {
	id, workerID := r.PathValue("id"), r.Header.Get("X-iDeploy-Worker-ID")
	generation, err := strconv.ParseInt(r.Header.Get("X-iDeploy-Lease-Generation"), 10, 64)
	if err != nil || !releaseIDPattern.MatchString(id) || !releaseIDPattern.MatchString(workerID) || generation < 1 {
		http.Error(w, "invalid lease", http.StatusBadRequest)
		return
	}
	var registryID, deploymentID string
	err = s.db.QueryRowContext(r.Context(), s.query(`SELECT registry_id,deployment_id FROM release_jobs WHERE id=$1 AND lease_owner=$2 AND lease_generation=$3 AND status='running' AND lease_until>$4`), id, workerID, generation, releaseTime(time.Now())).Scan(&registryID, &deploymentID)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "lease unavailable", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	var rawConfig, rawEnvelope string
	if err := s.db.QueryRowContext(r.Context(), `SELECT configuration FROM shared_vault WHERE id=1`).Scan(&rawConfig); err != nil {
		http.Error(w, "vault unavailable", http.StatusServiceUnavailable)
		return
	}
	secretID := "r.registries:" + registryID + ":credential"
	if purpose == "git" {
		var raw []byte
		if err := s.db.QueryRowContext(r.Context(), s.query(`SELECT data FROM shared_resources WHERE kind=$1 AND id=$2 AND deleted=false`), "r.deployments", deploymentID).Scan(&raw); err != nil {
			http.Error(w, "project unavailable", http.StatusServiceUnavailable)
			return
		}
		var deployment map[string]any
		if json.Unmarshal(raw, &deployment) != nil {
			http.Error(w, "project unavailable", http.StatusServiceUnavailable)
			return
		}
		projectID, _ := deployment["project_id"].(string)
		if !releaseIDPattern.MatchString(projectID) {
			http.Error(w, "project unavailable", http.StatusServiceUnavailable)
			return
		}
		secretID = "r.project_sources:" + projectID + ":credential"
	}
	if err := s.db.QueryRowContext(r.Context(), s.query(`SELECT envelope FROM shared_secrets WHERE id=$1`), secretID).Scan(&rawEnvelope); err != nil {
		http.Error(w, "credential unavailable", http.StatusNotFound)
		return
	}
	var config sharedworkspace.VaultConfig
	var envelope sharedworkspace.Envelope
	if json.Unmarshal([]byte(rawConfig), &config) != nil || json.Unmarshal([]byte(rawEnvelope), &envelope) != nil {
		http.Error(w, "encrypted credential invalid", http.StatusServiceUnavailable)
		return
	}
	key, err := sharedworkspace.VaultKey(config)
	if err != nil {
		http.Error(w, "vault unavailable", http.StatusServiceUnavailable)
		return
	}
	secret, err := sharedworkspace.DecryptSecret(key, secretID, envelope)
	if err != nil || secret == "" {
		http.Error(w, "credential unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if purpose == "git" {
		_ = json.NewEncoder(w).Encode(map[string]string{"credential": secret})
	} else {
		_ = json.NewEncoder(w).Encode(map[string]string{"password": secret})
	}
}

func (s *Server) claimReleaseJobHTTP(w http.ResponseWriter, r *http.Request) {
	var input struct {
		WorkerID string `json:"workerId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	job, err := s.claimReleaseJob(r.Context(), input.WorkerID, 60*time.Second)
	if errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		http.Error(w, "cannot claim task", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(job)
}

func (s *Server) updateReleaseJobHTTP(w http.ResponseWriter, r *http.Request) {
	var input struct {
		WorkerID   string `json:"workerId"`
		Generation int64  `json:"generation"`
		Status     string `json:"status"`
		Stage      string `json:"stage"`
		ImageRef   string `json:"imageRef"`
		Manifest   string `json:"manifest"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 6*1024*1024)).Decode(&input); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	var err error
	if input.Manifest != "" {
		err = s.updateComposeArtifact(r.Context(), r.PathValue("id"), input.WorkerID, input.Generation, input.Status, input.Stage, input.ImageRef, input.Manifest)
	} else {
		err = s.updateReleaseJob(r.Context(), r.PathValue("id"), input.WorkerID, input.Generation, input.Stage, input.Status, input.ImageRef, 60*time.Second)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoteReleaseJob contains identifiers and immutable Git references only.
// Secret values are never accepted as task payload or persisted here.
type RemoteReleaseJob struct {
	ID              string `json:"id"`
	RequestID       string `json:"requestId"`
	DeploymentID    string `json:"deploymentId"`
	ReleaseID       string `json:"releaseId"`
	DeliveryCommit  string `json:"deliveryCommit"`
	DeliveryDigest  string `json:"deliveryDigest"`
	RepositoryURL   string `json:"repositoryUrl"`
	Version         string `json:"version"`
	RegistryID      string `json:"registryId"`
	RegistryHost    string `json:"registryHost"`
	RegistryUser    string `json:"registryUser"`
	ImageRef        string `json:"imageRef,omitempty"`
	Manifest        string `json:"manifest,omitempty"`
	Status          string `json:"status"`
	Stage           string `json:"stage"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
	LeaseGeneration int64  `json:"leaseGeneration,omitempty"`
}

// claimReleaseJob serializes claims in the shared database. The generation is
// a fencing token: stale workers cannot update a task after it is reclaimed.
func (s *Server) claimReleaseJob(ctx context.Context, workerID string, lease time.Duration) (RemoteReleaseJob, error) {
	if !releaseIDPattern.MatchString(workerID) || lease < 10*time.Second || lease > 5*time.Minute {
		return RemoteReleaseJob{}, errors.New("执行器身份或租约时长无效")
	}
	now := time.Now().UTC()
	if err := s.expireReleaseLeases(ctx, now); err != nil {
		return RemoteReleaseJob{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RemoteReleaseJob{}, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM release_jobs WHERE status='queued' ORDER BY created_at,id LIMIT 1`).Scan(&id)
	if err != nil {
		return RemoteReleaseJob{}, err
	}
	claimed, err := tx.ExecContext(ctx, s.query(`UPDATE release_jobs SET status='running',stage='checking_out',lease_owner=$1,
		lease_generation=lease_generation+1,lease_until=$2,updated_at=$3 WHERE id=$4 AND status='queued'`),
		workerID, releaseTime(now.Add(lease)), releaseTime(now), id)
	if err != nil {
		return RemoteReleaseJob{}, err
	}
	count, err := claimed.RowsAffected()
	if err != nil {
		return RemoteReleaseJob{}, err
	}
	if count != 1 {
		return RemoteReleaseJob{}, sql.ErrNoRows
	}
	var job RemoteReleaseJob
	err = tx.QueryRowContext(ctx, s.query(`SELECT id,request_id,deployment_id,release_id,delivery_commit,delivery_digest,
		repository_url,version,registry_id,registry_host,registry_user,image_ref,status,stage,created_at,updated_at,lease_generation
		FROM release_jobs WHERE id=$1`), id).Scan(&job.ID, &job.RequestID, &job.DeploymentID, &job.ReleaseID, &job.DeliveryCommit, &job.DeliveryDigest,
		&job.RepositoryURL, &job.Version, &job.RegistryID, &job.RegistryHost, &job.RegistryUser, &job.ImageRef,
		&job.Status, &job.Stage, &job.CreatedAt, &job.UpdatedAt, &job.LeaseGeneration)
	if err != nil {
		return RemoteReleaseJob{}, err
	}
	if err = tx.Commit(); err != nil {
		return RemoteReleaseJob{}, err
	}
	return job, nil
}

func (s *Server) expireReleaseLeases(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, s.query(`UPDATE release_jobs SET status='needs_reconcile',stage='checking',updated_at=$1
		WHERE status IN ('running','cancel_requested') AND lease_until<>'' AND lease_until<=$2`), releaseTime(now), releaseTime(now))
	return err
}

func (s *Server) updateReleaseJob(ctx context.Context, id, workerID string, generation int64, stage, status, imageRef string, lease time.Duration) error {
	if !releaseIDPattern.MatchString(id) || !releaseIDPattern.MatchString(workerID) || generation < 1 || lease < 10*time.Second || lease > 5*time.Minute {
		return errors.New("任务租约无效")
	}
	if status != "running" && status != "artifact_ready" && status != "failed" && status != "canceled" && status != "needs_reconcile" {
		return errors.New("任务状态无效")
	}
	if status == "artifact_ready" && (stage != "artifact_ready" || !strings.Contains(imageRef, "@sha256:") || len(imageRef) > 512) {
		return errors.New("镜像制品摘要无效")
	}
	switch stage {
	case "", "checking_out", "building", "pushing", "artifact_ready", "deploying", "checking", "finished":
	default:
		return errors.New("任务阶段无效")
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, s.query(`UPDATE release_jobs SET status=$1,stage=COALESCE(NULLIF($2,''),stage),
		image_ref=COALESCE(NULLIF($3,''),image_ref),lease_until=$4,updated_at=$5
		WHERE id=$6 AND lease_owner=$7 AND lease_generation=$8 AND lease_until>$9 AND (
			(status='running' AND $10 IN ('running','failed','artifact_ready','needs_reconcile')) OR
			(status='cancel_requested' AND $11 IN ('canceled','needs_reconcile','artifact_ready'))
		)`), status, stage, imageRef, releaseTime(now.Add(lease)), releaseTime(now), id, workerID, generation, releaseTime(now), status, status)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("任务租约已失效")
	}
	return nil
}

var releaseIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var releaseCommitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var releaseDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var releaseVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func releaseTime(value time.Time) string { return value.UTC().Format("2006-01-02T15:04:05.000000000Z") }

func (j RemoteReleaseJob) validate() error {
	if !releaseIDPattern.MatchString(j.RequestID) || !releaseIDPattern.MatchString(j.DeploymentID) || !releaseIDPattern.MatchString(j.ReleaseID) ||
		!releaseCommitPattern.MatchString(j.DeliveryCommit) || !releaseDigestPattern.MatchString(j.DeliveryDigest) ||
		!releaseIDPattern.MatchString(j.RegistryID) || !releaseVersionPattern.MatchString(j.Version) {
		return errors.New("远端发布任务标识或 Git 摘要无效")
	}
	parsed, err := url.Parse(j.RepositoryURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "ssh") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("远端 Git 仓库地址无效或包含认证信息")
	}
	if j.RegistryHost == "" || j.RegistryUser == "" || strings.ContainsAny(j.RegistryHost+j.RegistryUser, "\r\n\x00 ") || strings.Contains(j.RegistryHost, "/") {
		return errors.New("镜像仓库配置无效")
	}
	return nil
}

func ensureReleaseJobSchema(ctx context.Context, db *sql.DB, driver string) error {
	manifestType := "TEXT"
	if driver == "mysql" {
		manifestType = "MEDIUMTEXT"
	}
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS release_jobs (
		id VARCHAR(32) PRIMARY KEY,
		request_id VARCHAR(32) NOT NULL UNIQUE,
		deployment_id VARCHAR(32) NOT NULL,
		release_id VARCHAR(32) NOT NULL UNIQUE,
		delivery_commit VARCHAR(40) NOT NULL,
		delivery_digest VARCHAR(64) NOT NULL,
		repository_url TEXT NOT NULL,
		version VARCHAR(128) NOT NULL,
		registry_id VARCHAR(32) NOT NULL,
		registry_host VARCHAR(255) NOT NULL,
		registry_user VARCHAR(255) NOT NULL,
		image_ref VARCHAR(512) NOT NULL DEFAULT '',
		manifest_envelope `+manifestType+`,
		status VARCHAR(32) NOT NULL,
		stage VARCHAR(32) NOT NULL,
		lease_owner VARCHAR(64) NOT NULL DEFAULT '',
		lease_generation BIGINT NOT NULL DEFAULT 0,
		lease_until VARCHAR(40) NOT NULL DEFAULT '',
		created_at VARCHAR(40) NOT NULL,
		updated_at VARCHAR(40) NOT NULL
	)`)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS release_job_requests (
		request_id VARCHAR(32) PRIMARY KEY, canceled BOOLEAN NOT NULL
	)`); err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, "SELECT * FROM release_jobs LIMIT 0")
	if err != nil {
		return err
	}
	columns, err := rows.Columns()
	rows.Close()
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for _, column := range columns {
		existing[column] = true
	}
	for _, addition := range []struct{ name, definition string }{
		{"repository_url", "VARCHAR(2048) NOT NULL DEFAULT ''"},
		{"version", "VARCHAR(128) NOT NULL DEFAULT ''"},
		{"registry_id", "VARCHAR(32) NOT NULL DEFAULT ''"},
		{"registry_host", "VARCHAR(255) NOT NULL DEFAULT ''"},
		{"registry_user", "VARCHAR(255) NOT NULL DEFAULT ''"},
		{"image_ref", "VARCHAR(512) NOT NULL DEFAULT ''"},
		{"manifest_envelope", manifestType},
	} {
		if !existing[addition.name] {
			if _, err := db.ExecContext(ctx, "ALTER TABLE release_jobs ADD COLUMN "+addition.name+" "+addition.definition); err != nil {
				return err
			}
		}
	}
	if driver == "mysql" && existing["manifest_envelope"] {
		if err := widenMySQLText(ctx, db, "release_jobs", "manifest_envelope", `ALTER TABLE release_jobs MODIFY COLUMN manifest_envelope MEDIUMTEXT`); err != nil {
			return err
		}
	}
	_, err = db.ExecContext(ctx, `UPDATE release_jobs SET status='needs_reconcile',stage='checking'
		WHERE status='queued' AND (repository_url='' OR version='' OR registry_id='' OR registry_host='')`)
	return err
}

func (s *Server) createReleaseJob(ctx context.Context, input RemoteReleaseJob) (RemoteReleaseJob, error) {
	if err := input.validate(); err != nil {
		return RemoteReleaseJob{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RemoteReleaseJob{}, err
	}
	defer tx.Rollback()
	if err := s.lockReleaseRequest(ctx, tx, input.RequestID); err != nil {
		return RemoteReleaseJob{}, err
	}

	var previous RemoteReleaseJob
	lookup := tx.QueryRowContext(ctx, s.query(`SELECT id,request_id,deployment_id,release_id,delivery_commit,delivery_digest,
		repository_url,version,registry_id,registry_host,registry_user,image_ref,status,stage,created_at,updated_at FROM release_jobs WHERE request_id=$1`), input.RequestID)
	err = lookup.Scan(&previous.ID, &previous.RequestID, &previous.DeploymentID, &previous.ReleaseID, &previous.DeliveryCommit, &previous.DeliveryDigest,
		&previous.RepositoryURL, &previous.Version, &previous.RegistryID, &previous.RegistryHost, &previous.RegistryUser, &previous.ImageRef,
		&previous.Status, &previous.Stage, &previous.CreatedAt, &previous.UpdatedAt)
	if err == nil {
		if previous.DeploymentID != input.DeploymentID || previous.ReleaseID != input.ReleaseID || previous.DeliveryCommit != input.DeliveryCommit || previous.DeliveryDigest != input.DeliveryDigest || previous.RepositoryURL != input.RepositoryURL || previous.Version != input.Version || previous.RegistryID != input.RegistryID || previous.RegistryHost != input.RegistryHost || previous.RegistryUser != input.RegistryUser {
			return RemoteReleaseJob{}, errors.New("请求 ID 已用于其他发布内容")
		}
		return previous, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return RemoteReleaseJob{}, err
	}
	var canceled bool
	if err := tx.QueryRowContext(ctx, s.query(`SELECT canceled FROM release_job_requests WHERE request_id=$1`), input.RequestID).Scan(&canceled); err != nil {
		return RemoteReleaseJob{}, err
	}
	if canceled {
		return RemoteReleaseJob{}, errors.New("远端构建请求已取消")
	}
	id, err := randomHex(16)
	if err != nil {
		return RemoteReleaseJob{}, err
	}
	now := releaseTime(time.Now())
	_, err = tx.ExecContext(ctx, s.query(`INSERT INTO release_jobs(id,request_id,deployment_id,release_id,delivery_commit,delivery_digest,
		repository_url,version,registry_id,registry_host,registry_user,status,stage,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'queued','queued',$12,$13)`), id, input.RequestID, input.DeploymentID, input.ReleaseID, input.DeliveryCommit, input.DeliveryDigest,
		input.RepositoryURL, input.Version, input.RegistryID, input.RegistryHost, input.RegistryUser, now, now)
	if err != nil {
		return RemoteReleaseJob{}, fmt.Errorf("保存远端发布任务: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return RemoteReleaseJob{}, err
	}
	input.ID, input.Status, input.Stage, input.CreatedAt, input.UpdatedAt = id, "queued", "queued", now, now
	return input, nil
}
