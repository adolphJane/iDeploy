package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"ideploy/internal/sharedserver"
)

func TestWorkerStopsAndAcknowledgesCancellation(t *testing.T) {
	var checkoutStarted, advanced atomic.Bool
	ack := make(chan string, 1)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case http.MethodGet:
			if req.URL.Path != "/v3/worker/releases/test/git-secret" {
				advanced.Store(true)
				http.Error(w, "unexpected next stage", http.StatusConflict)
				return
			}
			checkoutStarted.Store(true)
			<-req.Context().Done()
		case http.MethodPatch:
			var update struct{ Status, Stage string }
			if err := json.NewDecoder(req.Body).Decode(&update); err != nil {
				http.Error(w, "invalid patch", http.StatusBadRequest)
				return
			}
			switch {
			case update.Status == "running" && update.Stage == "checking_out":
				w.WriteHeader(http.StatusNoContent)
			case update.Status == "canceled":
				ack <- update.Status
				w.WriteHeader(http.StatusNoContent)
			default:
				http.Error(w, "cancellation requested", http.StatusConflict)
			}
		default:
			http.Error(w, "unsupported request", http.StatusMethodNotAllowed)
		}
	}))
	defer service.Close()

	executor := worker{id: "worker", base: service.URL, client: service.Client()}
	done := make(chan struct{})
	go func() {
		defer close(done)
		executor.execute(sharedserver.RemoteReleaseJob{ID: "test", LeaseGeneration: 1})
	}()
	select {
	case <-done:
	case <-time.After(12 * time.Second):
		service.CloseClientConnections()
		t.Fatal("worker did not stop after the service rejected its heartbeat")
	}
	select {
	case <-ack:
	default:
		t.Fatal("worker did not acknowledge cancellation")
	}
	if !checkoutStarted.Load() || advanced.Load() {
		t.Fatalf("worker cancellation advanced past checkout: %t", advanced.Load())
	}
}
