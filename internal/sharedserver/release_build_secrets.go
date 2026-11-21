package sharedserver

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"ideploy/internal/sharedworkspace"
)

var buildSecretNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

func (s *Server) releaseBuildSecretsHTTP(w http.ResponseWriter, r *http.Request) {
	id, workerID := r.PathValue("id"), r.Header.Get("X-iDeploy-Worker-ID")
	generation, err := strconv.ParseInt(r.Header.Get("X-iDeploy-Lease-Generation"), 10, 64)
	if err != nil || !releaseIDPattern.MatchString(id) || !releaseIDPattern.MatchString(workerID) || generation < 1 {
		http.Error(w, "invalid lease", http.StatusBadRequest)
		return
	}
	var deploymentID, releaseID string
	err = s.db.QueryRowContext(r.Context(), s.query(`SELECT deployment_id,release_id FROM release_jobs WHERE id=$1 AND lease_owner=$2 AND lease_generation=$3 AND status='running' AND lease_until>$4`), id, workerID, generation, releaseTime(time.Now())).Scan(&deploymentID, &releaseID)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "lease unavailable", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	resource := func(kind, resourceID string) (map[string]any, error) {
		var raw []byte
		if err := s.db.QueryRowContext(r.Context(), s.query(`SELECT data FROM shared_resources WHERE kind=$1 AND id=$2 AND deleted=false`), kind, resourceID).Scan(&raw); err != nil {
			return nil, err
		}
		var data map[string]any
		err := json.Unmarshal(raw, &data)
		return data, err
	}
	deployment, err := resource("r.deployments", deploymentID)
	if err != nil {
		http.Error(w, "deployment unavailable", http.StatusServiceUnavailable)
		return
	}
	release, err := resource("r.releases", releaseID)
	if err != nil || release["deployment_id"] != deploymentID || release["environment_revision"] != deployment["environment_revision"] {
		http.Error(w, "build environment changed after release snapshot", http.StatusConflict)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), s.query(`SELECT id,data FROM shared_resources WHERE kind=$1 AND deleted=false`), "r.env")
	if err != nil {
		http.Error(w, "environment unavailable", http.StatusServiceUnavailable)
		return
	}
	secretIDs := map[string]string{}
	for rows.Next() {
		var variableID string
		var raw []byte
		if err = rows.Scan(&variableID, &raw); err != nil {
			break
		}
		var variable map[string]any
		if json.Unmarshal(raw, &variable) != nil {
			err = errors.New("invalid environment metadata")
			break
		}
		if variable["deployment_id"] != deploymentID || variable["build_secret"] != float64(1) {
			continue
		}
		name, _ := variable["name"].(string)
		if variable["kind"] != "secret" || !buildSecretNamePattern.MatchString(name) || !releaseIDPattern.MatchString(variableID) {
			err = errors.New("invalid build secret")
			break
		}
		if _, duplicate := secretIDs[name]; duplicate {
			err = errors.New("duplicate build secret")
			break
		}
		secretIDs[name] = "r.env:" + variableID + ":credential"
		if len(secretIDs) > 128 {
			err = errors.New("too many build secrets")
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		http.Error(w, "build secret metadata invalid", http.StatusServiceUnavailable)
		return
	}
	secrets := map[string]string{}
	if len(secretIDs) > 0 {
		var rawConfig string
		if err := s.db.QueryRowContext(r.Context(), `SELECT configuration FROM shared_vault WHERE id=1`).Scan(&rawConfig); err != nil {
			http.Error(w, "vault unavailable", http.StatusServiceUnavailable)
			return
		}
		var config sharedworkspace.VaultConfig
		if json.Unmarshal([]byte(rawConfig), &config) != nil {
			http.Error(w, "vault invalid", http.StatusServiceUnavailable)
			return
		}
		key, err := sharedworkspace.VaultKey(config)
		if err != nil {
			http.Error(w, "vault unavailable", http.StatusServiceUnavailable)
			return
		}
		for name, secretID := range secretIDs {
			var rawEnvelope string
			if err := s.db.QueryRowContext(r.Context(), s.query(`SELECT envelope FROM shared_secrets WHERE id=$1`), secretID).Scan(&rawEnvelope); err != nil {
				http.Error(w, "build secret unavailable", http.StatusServiceUnavailable)
				return
			}
			var envelope sharedworkspace.Envelope
			if json.Unmarshal([]byte(rawEnvelope), &envelope) != nil {
				http.Error(w, "build secret invalid", http.StatusServiceUnavailable)
				return
			}
			value, err := sharedworkspace.DecryptSecret(key, secretID, envelope)
			if err != nil || value == "" {
				http.Error(w, "build secret unavailable", http.StatusServiceUnavailable)
				return
			}
			secrets[name] = value
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"secrets": secrets})
}
