package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"flag"
	"ideploy/internal/agent"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const apiVersion = 1

func main() {
	tokenFile := flag.String("token-file", "", "path to pairing token")
	listen := flag.String("listen", "127.0.0.1:41827", "loopback listen address")
	dockerRoot := flag.String("docker-root", "/var/lib/docker", "Docker data directory")
	operationDir := flag.String("operations-dir", "/var/lib/ideploy-agent/operations", "durable operation directory")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || host != "127.0.0.1" && host != "::1" {
		log.Fatal("agent must listen on loopback")
	}
	if *tokenFile == "" {
		log.Fatal("token-file is required")
	}
	info, err := os.Lstat(*tokenFile)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		log.Fatal("pairing token must be a private regular file")
	}
	tokenBytes, err := os.ReadFile(*tokenFile)
	if err != nil {
		log.Fatal(err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if len(token) < 32 {
		log.Fatal("pairing token is too short")
	}
	executable, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		log.Fatal(err)
	}
	binarySum := sha256.Sum256(binary)
	binaryHash := hex.EncodeToString(binarySum[:])
	operations, err := agent.NewOperationStore(*operationDir)
	if err != nil {
		log.Fatal(err)
	}
	var sampler agent.Sampler
	var mu sync.RWMutex
	mu.Lock()
	sampler.Sample(*dockerRoot)
	mu.Unlock()
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			mu.Lock()
			sampler.Sample(*dockerRoot)
			mu.Unlock()
		}
	}()
	mux := http.NewServeMux()
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			next(w, r)
		}
	}
	mux.HandleFunc("/v1/hello", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": apiVersion, "binarySHA256": binaryHash, "os": runtime.GOOS, "arch": runtime.GOARCH, "capabilities": []string{"system.metrics", "system.inventory", "container.logs", "container.control", "container.deploy", "image.pull", "image.tag", "compose.deploy", "nginx.scan", "nginx.apply", "certbot.preflight", "certbot.issue", "certbot.renew"}})
	}))
	mux.HandleFunc("/v1/metrics", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		mu.RLock()
		value := sampler.SampleSnapshot()
		mu.RUnlock()
		_ = json.NewEncoder(w).Encode(value)
	}))
	mux.HandleFunc("/v1/inventory", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(agent.ReadInventory(r.Context()))
	}))
	mux.HandleFunc("/v1/nginx", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		value, err := agent.ReadNginx(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	}))
	mux.HandleFunc("/v1/certbot/check", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		value, err := agent.CheckCertbot(ctx, r.URL.Query().Get("domain"), r.URL.Query().Get("webroot"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	}))
	mux.HandleFunc("/v1/logs/", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		tail, err := strconv.Atoi(r.URL.Query().Get("tail"))
		if err != nil || tail < 1 || tail > 5000 {
			http.Error(w, "invalid tail", http.StatusBadRequest)
			return
		}
		value, err := agent.ContainerLogs(r.Context(), strings.TrimPrefix(r.URL.Path, "/v1/logs/"), tail)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"logs": value})
	}))
	mux.HandleFunc("/v1/operations", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
		var request agent.OperationRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid operation request", http.StatusBadRequest)
			return
		}
		op, err := operations.Create(request)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(op)
	}))
	mux.HandleFunc("/v1/operations/", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		op, err := operations.Get(strings.TrimPrefix(r.URL.Path, "/v1/operations/"))
		if err != nil {
			http.Error(w, "operation not found", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(op)
	}))
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	log.Fatal(server.ListenAndServe())
}
