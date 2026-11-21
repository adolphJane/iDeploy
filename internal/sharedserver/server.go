package sharedserver

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const APIVersion = 2

type Server struct {
	db              *sql.DB
	driver          string
	tokenHash       [32]byte
	workerTokenHash [32]byte
	workerEnabled   bool
}

func (s *Server) SetWorkerToken(token string) error {
	if len(token) < 32 {
		return errors.New("worker token must be at least 32 characters")
	}
	s.workerTokenHash = sha256.Sum256([]byte(token))
	s.workerEnabled = true
	return nil
}

func Open(ctx context.Context, dsn, token string) (*Server, error) {
	return OpenWithDriver(ctx, "postgres", dsn, token)
}

func OpenWithDriver(ctx context.Context, driver, dsn, token string) (*Server, error) {
	if strings.TrimSpace(dsn) == "" || len(token) < 32 {
		return nil, errors.New("database URL and an access token of at least 32 characters are required")
	}
	if driver != "postgres" && driver != "mysql" {
		return nil, errors.New("unsupported shared database driver")
	}
	goDriver := "pgx"
	if driver == "mysql" {
		if err := validateMySQLDSN(dsn); err != nil {
			return nil, err
		}
		goDriver = "mysql"
	}
	db, err := sql.Open(goDriver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	schema := []string{`CREATE TABLE IF NOT EXISTS shared_workspace (
 id SMALLINT PRIMARY KEY CHECK (id = 1),
 name TEXT NOT NULL,
 revision BIGINT NOT NULL DEFAULT 1,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
 )`, `CREATE TABLE IF NOT EXISTS shared_resources (
 kind TEXT NOT NULL, id TEXT NOT NULL, revision BIGINT NOT NULL,
 data JSONB NOT NULL DEFAULT '{}'::jsonb, deleted BOOLEAN NOT NULL DEFAULT false,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY (kind, id)
 )`, `CREATE TABLE IF NOT EXISTS shared_devices (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, token_hash BYTEA NOT NULL UNIQUE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), revoked_at TIMESTAMPTZ
 )`}
	insertWorkspace := `INSERT INTO shared_workspace (id, name) VALUES (1, 'iDeploy') ON CONFLICT (id) DO NOTHING`
	if driver == "mysql" {
		schema = []string{`CREATE TABLE IF NOT EXISTS shared_workspace (
 id SMALLINT PRIMARY KEY, name VARCHAR(255) NOT NULL, revision BIGINT NOT NULL DEFAULT 1,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6))`,
			`CREATE TABLE IF NOT EXISTS shared_resources (
 kind VARCHAR(32) NOT NULL, id VARCHAR(253) NOT NULL, revision BIGINT NOT NULL,
 data JSON NOT NULL, deleted BOOLEAN NOT NULL DEFAULT false,
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), PRIMARY KEY(kind,id))`,
			`CREATE TABLE IF NOT EXISTS shared_devices (
 id VARCHAR(32) PRIMARY KEY, name VARCHAR(80) NOT NULL, token_hash BINARY(32) NOT NULL UNIQUE,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), revoked_at DATETIME(6))`}
		insertWorkspace = `INSERT IGNORE INTO shared_workspace (id,name) VALUES (1,'iDeploy')`
	}
	for _, statement := range schema {
		if _, err = db.ExecContext(ctx, statement); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := ensureSyncSchema(ctx, db, driver); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureReleaseJobSchema(ctx, db, driver); err != nil {
		db.Close()
		return nil, err
	}
	if err := reconcileResourceHeads(ctx, db, driver); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.ExecContext(ctx, insertWorkspace); err != nil {
		db.Close()
		return nil, err
	}
	return &Server{db: db, driver: driver, tokenHash: sha256.Sum256([]byte(token))}, nil
}

func validateMySQLDSN(dsn string) error {
	config, err := mysql.ParseDSN(dsn)
	if err != nil || config.DBName == "" || (config.TLSConfig != "true" && config.TLSConfig != "skip-verify") {
		return errors.New("MySQL requires a valid DSN with a database name and encrypted TLS (true or skip-verify)")
	}
	return nil
}

var parameterPattern = regexp.MustCompile(`\$[1-9][0-9]*`)

func (s *Server) query(statement string) string {
	if s.driver == "mysql" {
		return parameterPattern.ReplaceAllString(statement, "?")
	}
	return statement
}

func (s *Server) Close() error { return s.db.Close() }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.db.PingContext(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	mux.HandleFunc("GET /v1/workspace", s.authorize(func(w http.ResponseWriter, r *http.Request) {
		var name string
		var revision int64
		if err := s.db.QueryRowContext(r.Context(), "SELECT name, revision FROM shared_workspace WHERE id=1").Scan(&name, &revision); err != nil {
			http.Error(w, "workspace unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			APIVersion int    `json:"apiVersion"`
			Name       string `json:"name"`
			Revision   int64  `json:"revision"`
		}{APIVersion, name, revision})
	}))
	mux.HandleFunc("GET /v1/resources/{kind}", s.authorize(s.listResources))
	mux.HandleFunc("PUT /v1/resources/{kind}/{id}", s.authorize(s.putResource))
	mux.HandleFunc("DELETE /v1/resources/{kind}/{id}", s.authorize(s.deleteResource))
	mux.HandleFunc("GET /v1/devices", s.authorize(s.listDevices))
	mux.HandleFunc("POST /v1/devices", s.authorizeAdmin(s.createDevice))
	mux.HandleFunc("DELETE /v1/devices/{id}", s.authorize(s.revokeDevice))
	mux.HandleFunc("GET /v2/changes", s.authorize(s.listChanges))
	mux.HandleFunc("POST /v2/imports", s.authorizeAdmin(s.importBatch))
	mux.HandleFunc("POST /v2/batches", s.authorize(s.writeBatch))
	mux.HandleFunc("GET /v2/imports/{id}", s.authorizeAdmin(s.getImport))
	mux.HandleFunc("POST /v2/pairings", s.authorize(s.createPairing))
	mux.HandleFunc("POST /v2/pairings/redeem", s.redeemPairing)
	mux.HandleFunc("GET /v2/vault", s.authorize(s.getVault))
	mux.HandleFunc("POST /v2/vault", s.authorizeAdmin(s.createVault))
	mux.HandleFunc("GET /v2/secrets", s.authorize(s.listSecrets))
	mux.HandleFunc("PUT /v2/secrets/{id}", s.authorize(s.putSecret))
	mux.HandleFunc("POST /v2/leases/{id}", s.authorize(s.acquireLease))
	mux.HandleFunc("DELETE /v2/leases/{id}", s.authorize(s.releaseLease))
	mux.HandleFunc("POST /v3/worker/releases/claim", s.authorizeWorker(s.claimReleaseJobHTTP))
	mux.HandleFunc("PATCH /v3/worker/releases/{id}", s.authorizeWorker(s.updateReleaseJobHTTP))
	mux.HandleFunc("GET /v3/worker/releases/{id}/registry-secret", s.authorizeWorker(s.releaseRegistrySecretHTTP))
	mux.HandleFunc("GET /v3/worker/releases/{id}/git-secret", s.authorizeWorker(s.releaseGitSecretHTTP))
	mux.HandleFunc("GET /v3/worker/releases/{id}/build-secrets", s.authorizeWorker(s.releaseBuildSecretsHTTP))
	mux.HandleFunc("POST /v3/releases", s.authorize(s.createReleaseJobHTTP))
	mux.HandleFunc("GET /v3/releases/by-request/{id}", s.authorize(s.getReleaseJobByRequestHTTP))
	mux.HandleFunc("DELETE /v3/releases/by-request/{id}", s.authorize(s.cancelReleaseJobHTTP))
	mux.HandleFunc("GET /v3/releases/{id}", s.authorize(s.getReleaseJobHTTP))
	return mux
}

func (s *Server) authorizeWorker(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.workerEnabled {
			http.Error(w, "worker unavailable", http.StatusServiceUnavailable)
			return
		}
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		hash := sha256.Sum256([]byte(auth))
		if auth == "" || subtle.ConstantTimeCompare(hash[:], s.workerTokenHash[:]) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		hash := sha256.Sum256([]byte(auth))
		if auth == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if subtle.ConstantTimeCompare(hash[:], s.tokenHash[:]) != 1 {
			var exists bool
			err := s.db.QueryRowContext(r.Context(), s.query(`SELECT EXISTS(SELECT 1 FROM shared_devices WHERE token_hash=$1 AND revoked_at IS NULL)`), hash[:]).Scan(&exists)
			if err != nil || !exists {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

func (s *Server) authorizeAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		hash := sha256.Sum256([]byte(auth))
		if auth == "" || subtle.ConstantTimeCompare(hash[:], s.tokenHash[:]) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
