package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"ideploy/internal/sharedworkspace"
)

func TestRemoteBuildWaitingReleasesRunnerOnTerminalFailure(t *testing.T) {
	for _, status := range []string{"failed", "needs_reconcile", "canceled", "unknown"} {
		t.Run(status, func(t *testing.T) {
			_, err := waitRemoteBuild(context.Background(), func(context.Context) (sharedworkspace.RemoteReleaseJob, error) {
				return sharedworkspace.RemoteReleaseJob{Status: status}, nil
			})
			if err == nil {
				t.Fatalf("terminal status %s did not stop waiting", status)
			}
		})
	}
	_, err := waitRemoteBuild(context.Background(), func(context.Context) (sharedworkspace.RemoteReleaseJob, error) {
		return sharedworkspace.RemoteReleaseJob{}, &sharedworkspace.HTTPStatusError{Code: http.StatusUnauthorized}
	})
	if err == nil {
		t.Fatal("an unauthorized reader kept waiting")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = waitRemoteBuild(ctx, func(context.Context) (sharedworkspace.RemoteReleaseJob, error) {
		return sharedworkspace.RemoteReleaseJob{Status: "queued"}, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued task ignored the waiting deadline: %v", err)
	}
}
