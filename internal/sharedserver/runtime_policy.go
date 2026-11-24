package sharedserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"ideploy/internal/deployment"
)

type storedRuntimePolicy struct {
	Revision int64  `json:"revision"`
	Document string `json:"policy_json"`
	Before   string `json:"before_json"`
	After    string `json:"after_json"`
}

func decodeRuntimePolicy(raw []byte) (deployment.RuntimePolicy, error) {
	var record storedRuntimePolicy
	var policy deployment.RuntimePolicy
	if err := json.Unmarshal(raw, &record); err != nil || record.Revision < 1 || len(record.Document) > 8192 {
		return policy, errors.New("invalid runtime policy record")
	}
	decoder := json.NewDecoder(strings.NewReader(record.Document))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil || policy.Revision != record.Revision {
		return policy, errors.New("invalid runtime policy document")
	}
	kind := "container"
	if len(policy.Services) != 0 {
		kind = "compose"
	}
	if err := policy.Validate(kind); err != nil {
		return policy, errors.New("invalid runtime policy rules")
	}
	for _, document := range []string{record.Before, record.After} {
		if document == "" {
			continue
		}
		var checks []deployment.RuntimeCheck
		decoder := json.NewDecoder(strings.NewReader(document))
		decoder.DisallowUnknownFields()
		if len(document) > 24*1024 || decoder.Decode(&checks) != nil {
			return policy, errors.New("invalid runtime verification summary")
		}
	}
	return policy, nil
}

func (s *Server) getRuntimePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !releaseIDPattern.MatchString(id) {
		http.Error(w, "invalid deployment id", http.StatusBadRequest)
		return
	}
	var raw []byte
	err := s.db.QueryRowContext(r.Context(), s.query(`SELECT data FROM shared_resources WHERE kind='r.runtime_policies' AND id=$1 AND deleted=false`), id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "runtime policy not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "runtime policy unavailable", http.StatusServiceUnavailable)
		return
	}
	policy, err := decodeRuntimePolicy(raw)
	if err != nil {
		http.Error(w, "runtime policy unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(policy)
}

func (s *Server) serverHasEnforcedPolicy(ctx context.Context, tx *sql.Tx, serverID string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT p.data,d.data FROM shared_resources p JOIN shared_resources d ON d.id=p.id
		WHERE p.kind='r.runtime_policies' AND d.kind='r.deployments' AND p.deleted=false AND d.deleted=false`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var policyRaw, targetRaw []byte
		if err := rows.Scan(&policyRaw, &targetRaw); err != nil {
			return false, err
		}
		var target struct {
			ServerID string `json:"server_id"`
		}
		if err := json.Unmarshal(targetRaw, &target); err != nil {
			return false, err
		}
		if target.ServerID == serverID {
			policy, err := decodeRuntimePolicy(policyRaw)
			if err != nil || policy.Mode == deployment.RuntimeEnforce {
				return true, err
			}
		}
	}
	return false, rows.Err()
}
