package sharedserver

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"strings"

	"ideploy/internal/sharedworkspace"
)

var resourceKinds = map[string]bool{
	"project": true, "deployment": true, "server": true,
	"registry": true, "database": true, "domain": true,
}

var resourceFields = map[string]map[string]bool{
	"project":    {"name": true, "description": true, "sourceKind": true, "repositoryURL": true, "refKind": true, "refValue": true, "contextPath": true, "dockerfilePath": true, "composePath": true, "imageRepository": true},
	"deployment": {"name": true, "projectID": true, "serverID": true, "workloadKind": true, "workloadName": true, "containerPorts": true, "restartPolicy": true},
	"server":     {"name": true, "host": true, "port": true, "sshUser": true, "authMethod": true},
	"registry":   {"name": true, "kind": true, "endpoint": true, "username": true},
	"database":   {"name": true, "driver": true, "host": true, "port": true, "databaseName": true, "username": true, "sslMode": true},
	"domain":     {"name": true, "dnsConnectionID": true},
}

type resourceWrite struct {
	ExpectedRevision int64          `json:"expectedRevision"`
	Data             map[string]any `json:"data"`
}

type sharedResource struct {
	ID       string          `json:"id"`
	Revision int64           `json:"revision"`
	Deleted  bool            `json:"deleted"`
	Data     json.RawMessage `json:"data,omitempty"`
}

func validResource(w http.ResponseWriter, r *http.Request, requireID bool) (string, string, bool) {
	kind, id := r.PathValue("kind"), r.PathValue("id")
	_, portable := sharedworkspace.Entity(kind)
	if (!resourceKinds[kind] && !portable) || (requireID && (len(id) == 0 || len(id) > 253 || strings.ContainsAny(id, "/\\\x00\n\r"))) {
		http.Error(w, "invalid resource kind or id", http.StatusBadRequest)
		return "", "", false
	}
	return kind, id, true
}

func rejectSecrets(value any) error {
	switch object := value.(type) {
	case map[string]any:
		for key, item := range object {
			normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
			for _, fragment := range []string{"password", "secret", "token", "credential", "privatekey", "accesskey", "apikey", "plainvalue", "passphrase"} {
				if strings.Contains(normalized, fragment) {
					return errors.New("resource contains a credential field")
				}
			}
			if err := rejectSecrets(item); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range object {
			if err := rejectSecrets(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateResourceCredentials(kind string, data map[string]any) error {
	if flag, present := data["build_secret"]; kind == "r.env" && present {
		// SQLite exports this scope marker as 0 or 1. The credential itself
		// travels separately through the encrypted secrets API.
		scope, ok := flag.(float64)
		if !ok || (scope != 0 && scope != 1) || (scope == 1 && data["kind"] != "secret") {
			return errors.New("invalid build_secret scope flag")
		}
		data = maps.Clone(data)
		delete(data, "build_secret")
	}
	return rejectSecrets(data)
}

func (s *Server) listResources(w http.ResponseWriter, r *http.Request) {
	kind, _, ok := validResource(w, r, false)
	if !ok {
		return
	}
	rows, err := s.db.QueryContext(r.Context(), s.query(`SELECT id, revision, deleted, data FROM shared_resources WHERE kind=$1 ORDER BY id LIMIT 1001`), kind)
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	defer rows.Close()
	items := make([]sharedResource, 0)
	for rows.Next() {
		var item sharedResource
		if err := rows.Scan(&item.ID, &item.Revision, &item.Deleted, &item.Data); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		items = append(items, item)
		if len(items) > 1000 {
			http.Error(w, "resource page too large", http.StatusRequestEntityTooLarge)
			return
		}
	}
	if rows.Err() != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

func (s *Server) putResource(w http.ResponseWriter, r *http.Request) {
	kind, id, ok := validResource(w, r, true)
	if !ok {
		return
	}
	var input resourceWrite
	decoder := json.NewDecoder(io.LimitReader(r.Body, 256*1024+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.ExpectedRevision < 0 || input.Data == nil {
		http.Error(w, "invalid resource write", http.StatusBadRequest)
		return
	}
	if err := validateResourceCredentials(kind, input.Data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for field := range input.Data {
		_, portable := sharedworkspace.Entity(kind)
		if (!portable && !resourceFields[kind][field]) || (portable && !sharedworkspace.AllowedField(kind, field)) {
			http.Error(w, "unsupported resource field", http.StatusBadRequest)
			return
		}
	}
	data, err := json.Marshal(input.Data)
	if err != nil || len(data) > 256*1024 {
		http.Error(w, "resource too large", http.StatusRequestEntityTooLarge)
		return
	}
	if kind == "r.runtime_policies" || kind == "r.release_policies" {
		if err := validateImportRecord(importRecord{Kind: kind, ID: id, Data: data}); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	s.writeResource(w, r, kind, id, input.ExpectedRevision, data, false)
}

func (s *Server) deleteResource(w http.ResponseWriter, r *http.Request) {
	kind, id, ok := validResource(w, r, true)
	if !ok {
		return
	}
	var input struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.ExpectedRevision <= 0 {
		http.Error(w, "expectedRevision is required", http.StatusBadRequest)
		return
	}
	s.writeResource(w, r, kind, id, input.ExpectedRevision, []byte(`{}`), true)
}

func (s *Server) writeResource(w http.ResponseWriter, r *http.Request, kind, id string, expected int64, data []byte, deleted bool) {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	defer tx.Rollback()
	var revision int64
	err = tx.QueryRowContext(r.Context(), s.query(`SELECT revision FROM shared_resources WHERE kind=$1 AND id=$2 FOR UPDATE`), kind, id).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	if revision != expected {
		http.Error(w, "revision conflict", http.StatusConflict)
		return
	}
	if revision == 0 {
		statement := `INSERT INTO shared_resources(kind,id,revision,data,deleted) VALUES($1,$2,1,$3::jsonb,$4)`
		if s.driver == "mysql" {
			statement = `INSERT INTO shared_resources(kind,id,revision,data,deleted) VALUES($1,$2,1,$3,$4)`
		}
		_, err = tx.ExecContext(r.Context(), s.query(statement), kind, id, string(data), deleted)
	} else {
		statement := `UPDATE shared_resources SET revision=revision+1,data=$3::jsonb,deleted=$4,updated_at=now() WHERE kind=$1 AND id=$2`
		args := []any{kind, id, string(data), deleted}
		if s.driver == "mysql" {
			statement = `UPDATE shared_resources SET revision=revision+1,data=?,deleted=?,updated_at=now() WHERE kind=? AND id=? AND revision=?`
			args = []any{string(data), deleted, kind, id, revision}
		}
		var result sql.Result
		result, err = tx.ExecContext(r.Context(), s.query(statement), args...)
		if err == nil {
			var count int64
			count, err = result.RowsAffected()
			if err == nil && count != 1 {
				err = errors.New("resource revision was not updated")
			}
		}
	}
	if err != nil {
		http.Error(w, "write failed", http.StatusConflict)
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE shared_workspace SET revision=revision+1,updated_at=now() WHERE id=1`); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	if err = s.appendChangeTx(r.Context(), tx, kind, id, revision+1, deleted, data); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sharedResource{ID: id, Revision: revision + 1, Deleted: deleted, Data: data})
}
