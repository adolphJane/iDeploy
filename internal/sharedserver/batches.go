package sharedserver

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"ideploy/internal/sharedworkspace"
)

type batchResource struct {
	Kind             string          `json:"kind"`
	ID               string          `json:"id"`
	ExpectedRevision int64           `json:"expectedRevision"`
	Deleted          bool            `json:"deleted"`
	Data             json.RawMessage `json:"data"`
}

type batchSecret struct {
	ID               string          `json:"id"`
	ExpectedRevision int64           `json:"expectedRevision"`
	Envelope         json.RawMessage `json:"envelope"`
}

type batchRequest struct {
	RequestID string          `json:"requestId"`
	Resources []batchResource `json:"resources"`
	Secrets   []batchSecret   `json:"secrets"`
}

func (s *Server) writeBatch(w http.ResponseWriter, r *http.Request) {
	var input batchRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&input); err != nil || len(input.RequestID) < 16 || len(input.RequestID) > 128 || len(input.Resources) > 500 || len(input.Secrets) > 500 {
		http.Error(w, "invalid mutation batch", http.StatusBadRequest)
		return
	}
	seen := map[string]bool{}
	for _, item := range input.Resources {
		key := item.Kind + "/" + item.ID
		_, portable := sharedworkspace.Entity(item.Kind)
		if seen[key] || item.ExpectedRevision < 0 || (!portable && !resourceKinds[item.Kind]) || item.ID == "" || len(item.ID) > 253 {
			http.Error(w, "duplicate or invalid resource", http.StatusBadRequest)
			return
		}
		seen[key] = true
		if !item.Deleted {
			if err := validateImportRecord(importRecord{Kind: item.Kind, ID: item.ID, Data: item.Data}); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		} else if item.ExpectedRevision == 0 {
			http.Error(w, "deletion requires a revision", http.StatusBadRequest)
			return
		}
	}
	for _, item := range input.Secrets {
		if item.ID == "" || len(item.ID) > 128 || item.ExpectedRevision < 0 || seen["secret/"+item.ID] || len(item.Envelope) > 256*1024 {
			http.Error(w, "invalid secret", http.StatusBadRequest)
			return
		}
		seen["secret/"+item.ID] = true
		var shape sharedworkspace.Envelope
		if json.Unmarshal(item.Envelope, &shape) != nil || shape.Nonce == "" || shape.Ciphertext == "" {
			http.Error(w, "encrypted envelope required", http.StatusBadRequest)
			return
		}
	}
	encoded, _ := json.Marshal(input)
	sum := sha256.Sum256(encoded)
	digest := hex.EncodeToString(sum[:])
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	defer tx.Rollback()
	var previous string
	err = tx.QueryRowContext(r.Context(), s.query(`SELECT digest FROM shared_mutations WHERE id=$1`), input.RequestID).Scan(&previous)
	if err == nil {
		if previous != digest {
			http.Error(w, "request id reused", http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"requestId": input.RequestID, "replayed": true})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	for _, item := range input.Resources {
		var revision int64
		err = tx.QueryRowContext(r.Context(), s.query(`SELECT revision FROM shared_resources WHERE kind=$1 AND id=$2 FOR UPDATE`), item.Kind, item.ID).Scan(&revision)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		if revision != item.ExpectedRevision {
			http.Error(w, "revision conflict: "+item.Kind+"/"+item.ID, http.StatusConflict)
			return
		}
		data := item.Data
		if item.Deleted {
			data = []byte(`{}`)
		}
		if revision == 0 {
			statement := `INSERT INTO shared_resources(kind,id,revision,data,deleted) VALUES($1,$2,1,$3::jsonb,$4)`
			if s.driver == "mysql" {
				statement = `INSERT INTO shared_resources(kind,id,revision,data,deleted) VALUES($1,$2,1,$3,$4)`
			}
			_, err = tx.ExecContext(r.Context(), s.query(statement), item.Kind, item.ID, string(data), item.Deleted)
		} else {
			statement := `UPDATE shared_resources SET revision=revision+1,data=$3::jsonb,deleted=$4,updated_at=now() WHERE kind=$1 AND id=$2`
			args := []any{item.Kind, item.ID, string(data), item.Deleted}
			if s.driver == "mysql" {
				statement = `UPDATE shared_resources SET revision=revision+1,data=?,deleted=?,updated_at=now() WHERE kind=? AND id=? AND revision=?`
				args = []any{string(data), item.Deleted, item.Kind, item.ID, revision}
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
		if err != nil || s.appendChangeTx(r.Context(), tx, item.Kind, item.ID, revision+1, item.Deleted, data) != nil {
			http.Error(w, "resource write failed", http.StatusServiceUnavailable)
			return
		}
	}
	for _, item := range input.Secrets {
		var revision int64
		err = tx.QueryRowContext(r.Context(), s.query(`SELECT revision FROM shared_secrets WHERE id=$1 FOR UPDATE`), item.ID).Scan(&revision)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		if revision != item.ExpectedRevision {
			http.Error(w, "secret revision conflict", http.StatusConflict)
			return
		}
		if revision == 0 {
			_, err = tx.ExecContext(r.Context(), s.query(`INSERT INTO shared_secrets(id,revision,envelope,updated_at) VALUES($1,1,$2,$3)`), item.ID, string(item.Envelope), time.Now().UTC().Format(time.RFC3339Nano))
		} else {
			statement := `UPDATE shared_secrets SET revision=revision+1,envelope=$2,updated_at=$3 WHERE id=$1`
			args := []any{item.ID, string(item.Envelope), time.Now().UTC().Format(time.RFC3339Nano)}
			if s.driver == "mysql" {
				statement = `UPDATE shared_secrets SET revision=revision+1,envelope=?,updated_at=? WHERE id=? AND revision=?`
				args = []any{string(item.Envelope), args[2], item.ID, revision}
			}
			var result sql.Result
			result, err = tx.ExecContext(r.Context(), s.query(statement), args...)
			if err == nil {
				var count int64
				count, err = result.RowsAffected()
				if err == nil && count != 1 {
					err = errors.New("secret revision was not updated")
				}
			}
		}
		if err != nil {
			http.Error(w, "secret write failed", http.StatusServiceUnavailable)
			return
		}
	}
	if _, err := tx.ExecContext(r.Context(), s.query(`INSERT INTO shared_mutations(id,digest,created_at) VALUES($1,$2,$3)`), input.RequestID, digest, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		http.Error(w, "mutation write failed", http.StatusServiceUnavailable)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE shared_workspace SET revision=revision+1,updated_at=now() WHERE id=1`); err != nil || tx.Commit() != nil {
		http.Error(w, "mutation write failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"requestId": input.RequestID, "replayed": false})
}
