package sharedserver

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, name, revoked_at IS NOT NULL FROM shared_devices ORDER BY created_at`)
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, name string
		var revoked bool
		if err := rows.Scan(&id, &name, &revoked); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		items = append(items, map[string]any{"id": id, "name": name, "revoked": revoked})
	}
	if rows.Err() != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

func (s *Server) createDevice(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input); err != nil || len(strings.TrimSpace(input.Name)) == 0 || len(input.Name) > 80 {
		http.Error(w, "invalid device name", http.StatusBadRequest)
		return
	}
	bytes := make([]byte, 48)
	if _, err := rand.Read(bytes); err != nil {
		http.Error(w, "token generation failed", http.StatusInternalServerError)
		return
	}
	token := hex.EncodeToString(bytes)
	id := hex.EncodeToString(bytes[:16])
	hash := sha256.Sum256([]byte(token))
	if _, err := s.db.ExecContext(r.Context(), s.query(`INSERT INTO shared_devices(id,name,token_hash) VALUES($1,$2,$3)`), id, strings.TrimSpace(input.Name), hash[:]); err != nil {
		http.Error(w, "device registration failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "name": strings.TrimSpace(input.Name), "token": token})
}

func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if len(id) != 32 {
		http.Error(w, "invalid device id", http.StatusBadRequest)
		return
	}
	result, err := s.db.ExecContext(r.Context(), s.query(`UPDATE shared_devices SET revoked_at=now() WHERE id=$1 AND revoked_at IS NULL`), id)
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
