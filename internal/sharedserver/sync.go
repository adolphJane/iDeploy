package sharedserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ideploy/internal/sharedworkspace"
)

func ensureSyncSchema(ctx context.Context, db *sql.DB, driver string) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS shared_imports (id VARCHAR(128) PRIMARY KEY, digest VARCHAR(64) NOT NULL, item_count INTEGER NOT NULL, created_at VARCHAR(32) NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS shared_mutations (id VARCHAR(128) PRIMARY KEY, digest VARCHAR(64) NOT NULL, created_at VARCHAR(32) NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS shared_vault (id SMALLINT PRIMARY KEY, configuration TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS shared_secrets (id VARCHAR(128) PRIMARY KEY, revision BIGINT NOT NULL, envelope TEXT NOT NULL, updated_at VARCHAR(32) NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS shared_pairings (code_hash VARCHAR(64) PRIMARY KEY, expires_at VARCHAR(32) NOT NULL, used_at VARCHAR(32))`,
		`CREATE TABLE IF NOT EXISTS shared_leases (resource_id VARCHAR(128) PRIMARY KEY, request_id VARCHAR(128) NOT NULL, owner_hash VARCHAR(64) NOT NULL, expires_at VARCHAR(32) NOT NULL)`,
	}
	if driver == "mysql" {
		statements = append(statements, `CREATE TABLE IF NOT EXISTS shared_changes (sequence BIGINT AUTO_INCREMENT PRIMARY KEY, kind VARCHAR(32) NOT NULL, resource_id VARCHAR(253) NOT NULL, revision BIGINT NOT NULL, deleted BOOLEAN NOT NULL, data JSON NOT NULL)`)
		statements = append(statements, `ALTER TABLE shared_resources MODIFY id VARCHAR(253) NOT NULL`, `ALTER TABLE shared_changes MODIFY resource_id VARCHAR(253) NOT NULL`)
	} else {
		statements = append(statements, `CREATE TABLE IF NOT EXISTS shared_changes (sequence BIGSERIAL PRIMARY KEY, kind VARCHAR(32) NOT NULL, resource_id VARCHAR(253) NOT NULL, revision BIGINT NOT NULL, deleted BOOLEAN NOT NULL, data JSONB NOT NULL)`)
		statements = append(statements, `ALTER TABLE shared_changes ALTER COLUMN resource_id TYPE VARCHAR(253)`)
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

type changeItem struct {
	Sequence int64           `json:"sequence"`
	Kind     string          `json:"kind"`
	ID       string          `json:"id"`
	Revision int64           `json:"revision"`
	Deleted  bool            `json:"deleted"`
	Data     json.RawMessage `json:"data"`
}

func (s *Server) appendChangeTx(ctx context.Context, tx *sql.Tx, kind, id string, revision int64, deleted bool, data []byte) error {
	statement := `INSERT INTO shared_changes(kind,resource_id,revision,deleted,data) VALUES($1,$2,$3,$4,$5::jsonb)`
	if s.driver == "mysql" {
		statement = `INSERT INTO shared_changes(kind,resource_id,revision,deleted,data) VALUES($1,$2,$3,$4,$5)`
	}
	_, err := tx.ExecContext(ctx, s.query(statement), kind, id, revision, deleted, string(data))
	return err
}

func (s *Server) listChanges(w http.ResponseWriter, r *http.Request) {
	after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if r.URL.Query().Get("after") == "" {
		after, err = 0, nil
	}
	if err != nil || after < 0 {
		http.Error(w, "invalid cursor", http.StatusBadRequest)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), s.query(`SELECT sequence,kind,resource_id,revision,deleted,data FROM shared_changes WHERE sequence>$1 ORDER BY sequence LIMIT 501`), after)
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	defer rows.Close()
	items := []changeItem{}
	for rows.Next() {
		var item changeItem
		if err := rows.Scan(&item.Sequence, &item.Kind, &item.ID, &item.Revision, &item.Deleted, &item.Data); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	more := len(items) > 500
	if more {
		items = items[:500]
	}
	if len(items) > 0 {
		after = items[len(items)-1].Sequence
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "nextCursor": after, "hasMore": more})
}

type importRecord struct {
	Kind string          `json:"kind"`
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data"`
}

type importRequest struct {
	BatchID string         `json:"batchId"`
	Records []importRecord `json:"records"`
}

func validateImportRecord(value importRecord) error {
	if len(value.ID) == 0 || len(value.ID) > 253 || strings.ContainsAny(value.ID, "/\\\x00\r\n") || len(value.Data) == 0 || len(value.Data) > 256*1024 {
		return errors.New("invalid resource id or size")
	}
	if _, ok := sharedworkspace.Entity(value.Kind); !ok {
		return errors.New("unsupported resource kind")
	}
	var data map[string]any
	if err := json.Unmarshal(value.Data, &data); err != nil || data == nil {
		return errors.New("invalid resource data")
	}
	if err := rejectSecrets(data); err != nil {
		return err
	}
	for key := range data {
		if !sharedworkspace.AllowedField(value.Kind, key) {
			return errors.New("unsupported resource field")
		}
	}
	if value.Kind == "r.project_sources" {
		if address, _ := data["repository_url"].(string); address != "" {
			parsed, err := url.Parse(address)
			if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
				return errors.New("repository URL contains authentication or query data")
			}
		}
	}
	return nil
}

func (s *Server) importBatch(w http.ResponseWriter, r *http.Request) {
	var input importRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&input); err != nil || len(input.BatchID) < 16 || len(input.BatchID) > 128 || len(input.Records) > 5000 || strings.ContainsAny(input.BatchID, "/\\\x00\r\n") {
		http.Error(w, "invalid import batch", http.StatusBadRequest)
		return
	}
	seen := map[string]bool{}
	for _, item := range input.Records {
		if err := validateImportRecord(item); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		key := item.Kind + "/" + item.ID
		if seen[key] {
			http.Error(w, "duplicate resource", http.StatusBadRequest)
			return
		}
		seen[key] = true
	}
	canonical, _ := json.Marshal(input.Records)
	digestBytes := sha256.Sum256(canonical)
	digest := hex.EncodeToString(digestBytes[:])
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	defer tx.Rollback()
	var previous string
	err = tx.QueryRowContext(r.Context(), s.query(`SELECT digest FROM shared_imports WHERE id=$1`), input.BatchID).Scan(&previous)
	if err == nil {
		if previous != digest {
			http.Error(w, "batch id reused with different contents", http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"batchId": input.BatchID, "digest": digest, "count": len(input.Records), "replayed": true})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	var existing int
	if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM shared_resources`).Scan(&existing); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	if existing != 0 {
		http.Error(w, "workspace is not empty", http.StatusConflict)
		return
	}
	for _, item := range input.Records {
		statement := `INSERT INTO shared_resources(kind,id,revision,data,deleted) VALUES($1,$2,1,$3::jsonb,false)`
		if s.driver == "mysql" {
			statement = `INSERT INTO shared_resources(kind,id,revision,data,deleted) VALUES($1,$2,1,$3,false)`
		}
		if _, err := tx.ExecContext(r.Context(), s.query(statement), item.Kind, item.ID, string(item.Data)); err != nil {
			http.Error(w, "import failed", http.StatusConflict)
			return
		}
		if err := s.appendChangeTx(r.Context(), tx, item.Kind, item.ID, 1, false, item.Data); err != nil {
			http.Error(w, "import failed", http.StatusServiceUnavailable)
			return
		}
	}
	if _, err := tx.ExecContext(r.Context(), s.query(`INSERT INTO shared_imports(id,digest,item_count,created_at) VALUES($1,$2,$3,$4)`), input.BatchID, digest, len(input.Records), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		http.Error(w, "import failed", http.StatusConflict)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE shared_workspace SET revision=revision+1,updated_at=now() WHERE id=1`); err != nil {
		http.Error(w, "import failed", http.StatusServiceUnavailable)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "import failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"batchId": input.BatchID, "digest": digest, "count": len(input.Records), "replayed": false})
}

func (s *Server) getImport(w http.ResponseWriter, r *http.Request) {
	var digest string
	var count int
	err := s.db.QueryRowContext(r.Context(), s.query(`SELECT digest,item_count FROM shared_imports WHERE id=$1`), r.PathValue("id")).Scan(&digest, &count)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "batch not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"batchId": r.PathValue("id"), "digest": digest, "count": count})
}

func randomHex(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func (s *Server) createPairing(w http.ResponseWriter, r *http.Request) {
	code, err := randomHex(24)
	if err != nil {
		http.Error(w, "pairing unavailable", http.StatusInternalServerError)
		return
	}
	hash := sha256.Sum256([]byte(code))
	expires := time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(r.Context(), s.query(`INSERT INTO shared_pairings(code_hash,expires_at) VALUES($1,$2)`), hex.EncodeToString(hash[:]), expires); err != nil {
		http.Error(w, "pairing unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "expiresAt": expires})
}

func (s *Server) redeemPairing(w http.ResponseWriter, r *http.Request) {
	var input struct{ Code, Name string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input); err != nil || len(input.Code) != 48 || len(strings.TrimSpace(input.Name)) == 0 || len(input.Name) > 80 {
		http.Error(w, "invalid pairing request", http.StatusBadRequest)
		return
	}
	hash := sha256.Sum256([]byte(input.Code))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "pairing unavailable", http.StatusServiceUnavailable)
		return
	}
	defer tx.Rollback()
	statement := `UPDATE shared_pairings SET used_at=$1 WHERE code_hash=$2 AND used_at IS NULL AND expires_at>$1`
	args := []any{now, hex.EncodeToString(hash[:])}
	if s.driver == "mysql" {
		statement = `UPDATE shared_pairings SET used_at=? WHERE code_hash=? AND used_at IS NULL AND expires_at>?`
		args = []any{now, hex.EncodeToString(hash[:]), now}
	}
	result, err := tx.ExecContext(r.Context(), s.query(statement), args...)
	if err != nil {
		http.Error(w, "pairing unavailable", http.StatusServiceUnavailable)
		return
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		http.Error(w, "pairing code expired or already used", http.StatusForbidden)
		return
	}
	token, err := randomHex(48)
	if err != nil {
		http.Error(w, "pairing unavailable", http.StatusInternalServerError)
		return
	}
	id := token[:32]
	tokenHash := sha256.Sum256([]byte(token))
	if _, err := tx.ExecContext(r.Context(), s.query(`INSERT INTO shared_devices(id,name,token_hash) VALUES($1,$2,$3)`), id, strings.TrimSpace(input.Name), tokenHash[:]); err != nil || tx.Commit() != nil {
		http.Error(w, "pairing unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "token": token})
}

func (s *Server) getVault(w http.ResponseWriter, r *http.Request) {
	var value string
	err := s.db.QueryRowContext(r.Context(), `SELECT configuration FROM shared_vault WHERE id=1`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "vault not initialized", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, value)
}

func (s *Server) createVault(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16*1024))
	var config sharedworkspace.VaultConfig
	decodeErr := json.Unmarshal(data, &config)
	if err != nil || decodeErr != nil {
		http.Error(w, "invalid vault configuration", http.StatusBadRequest)
		return
	}
	if _, keyErr := sharedworkspace.VaultKey(config); keyErr != nil {
		http.Error(w, "invalid vault configuration", http.StatusBadRequest)
		return
	}
	_, err = s.db.ExecContext(r.Context(), s.query(`INSERT INTO shared_vault(id,configuration) VALUES(1,$1)`), string(data))
	if err != nil {
		http.Error(w, "vault already initialized", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) listSecrets(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id,revision,envelope FROM shared_secrets ORDER BY id`)
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, envelope string
		var revision int64
		if err := rows.Scan(&id, &revision, &envelope); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		items = append(items, map[string]any{"id": id, "revision": revision, "envelope": json.RawMessage(envelope)})
		if len(items) > 5000 {
			http.Error(w, "too many secrets", http.StatusRequestEntityTooLarge)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

func (s *Server) putSecret(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if len(id) == 0 || len(id) > 128 || strings.ContainsAny(id, "/\\\x00\r\n") {
		http.Error(w, "invalid secret id", http.StatusBadRequest)
		return
	}
	var input struct {
		ExpectedRevision int64           `json:"expectedRevision"`
		Envelope         json.RawMessage `json:"envelope"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024)).Decode(&input); err != nil || input.ExpectedRevision < 0 || len(input.Envelope) < 64 || !json.Valid(input.Envelope) {
		http.Error(w, "invalid encrypted secret", http.StatusBadRequest)
		return
	}
	var shape struct{ Nonce, Ciphertext string }
	if json.Unmarshal(input.Envelope, &shape) != nil || shape.Nonce == "" || shape.Ciphertext == "" {
		http.Error(w, "encrypted envelope required", http.StatusBadRequest)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	defer tx.Rollback()
	var revision int64
	err = tx.QueryRowContext(r.Context(), s.query(`SELECT revision FROM shared_secrets WHERE id=$1 FOR UPDATE`), id).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	if revision != input.ExpectedRevision {
		http.Error(w, "revision conflict", http.StatusConflict)
		return
	}
	if revision == 0 {
		_, err = tx.ExecContext(r.Context(), s.query(`INSERT INTO shared_secrets(id,revision,envelope,updated_at) VALUES($1,1,$2,$3)`), id, string(input.Envelope), time.Now().UTC().Format(time.RFC3339Nano))
	} else {
		statement := `UPDATE shared_secrets SET revision=revision+1,envelope=$2,updated_at=$3 WHERE id=$1`
		args := []any{id, string(input.Envelope), time.Now().UTC().Format(time.RFC3339Nano)}
		if s.driver == "mysql" {
			statement = `UPDATE shared_secrets SET revision=revision+1,envelope=?,updated_at=? WHERE id=? AND revision=?`
			args = []any{string(input.Envelope), args[2], id, revision}
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
	if err != nil || tx.Commit() != nil {
		http.Error(w, "secret write failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "revision": revision + 1})
}

func bearerHash(r *http.Request) string {
	hash := sha256.Sum256([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
	return hex.EncodeToString(hash[:])
}

func (s *Server) acquireLease(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" || len(id) > 128 || strings.ContainsAny(id, "/\\\x00\r\n") {
		http.Error(w, "invalid lease id", http.StatusBadRequest)
		return
	}
	var input struct {
		RequestID string `json:"requestId"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input) != nil || len(input.RequestID) < 16 || len(input.RequestID) > 128 {
		http.Error(w, "invalid request id", http.StatusBadRequest)
		return
	}
	now := time.Now().UTC()
	expires := now.Add(90 * time.Second).Format(time.RFC3339Nano)
	owner := bearerHash(r)
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "lease unavailable", http.StatusServiceUnavailable)
		return
	}
	defer tx.Rollback()
	var requestID, currentOwner, currentExpiry string
	err = tx.QueryRowContext(r.Context(), s.query(`SELECT request_id,owner_hash,expires_at FROM shared_leases WHERE resource_id=$1 FOR UPDATE`), id).Scan(&requestID, &currentOwner, &currentExpiry)
	if err == nil && currentExpiry > now.Format(time.RFC3339Nano) && (requestID != input.RequestID || currentOwner != owner) {
		http.Error(w, "operation already in progress", http.StatusConflict)
		return
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "lease unavailable", http.StatusServiceUnavailable)
		return
	}
	if err == nil {
		statement := `UPDATE shared_leases SET request_id=$2,owner_hash=$3,expires_at=$4 WHERE resource_id=$1`
		args := []any{id, input.RequestID, owner, expires}
		if s.driver == "mysql" {
			statement = `UPDATE shared_leases SET request_id=?,owner_hash=?,expires_at=? WHERE resource_id=?`
			args = []any{input.RequestID, owner, expires, id}
		}
		_, err = tx.ExecContext(r.Context(), s.query(statement), args...)
	} else {
		_, err = tx.ExecContext(r.Context(), s.query(`INSERT INTO shared_leases(resource_id,request_id,owner_hash,expires_at) VALUES($1,$2,$3,$4)`), id, input.RequestID, owner, expires)
	}
	if err != nil || tx.Commit() != nil {
		http.Error(w, "lease unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"requestId": input.RequestID, "expiresAt": expires})
}

func (s *Server) releaseLease(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	requestID := r.URL.Query().Get("requestId")
	result, err := s.db.ExecContext(r.Context(), s.query(`DELETE FROM shared_leases WHERE resource_id=$1 AND request_id=$2 AND owner_hash=$3`), id, requestID, bearerHash(r))
	if err != nil {
		http.Error(w, "lease unavailable", http.StatusServiceUnavailable)
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		http.Error(w, "lease not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
