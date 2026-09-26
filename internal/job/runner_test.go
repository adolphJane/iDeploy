package job_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"ideploy/internal/job"
	"ideploy/internal/platform/database"
)

func TestRunnerPersistsSuccessfulJob(t *testing.T) {
	store, closeDatabase := newStore(t)
	defer closeDatabase()

	runner := job.NewRunner(store, nil)
	runner.Register("test.success", func(_ context.Context, _ job.Job, reporter job.Reporter) error {
		reporter.Progress(50, "half way")
		reporter.Log("test log")
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)
	defer runner.Stop()

	queued, err := runner.Enqueue(ctx, job.NewJob{Type: "test.success"})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	completed := waitForStatus(t, store, queued.ID, job.StatusSucceeded)
	if completed.Progress != 100 {
		t.Fatalf("expected completed progress, got %d", completed.Progress)
	}
}

func TestRunnerCancelsActiveJob(t *testing.T) {
	store, closeDatabase := newStore(t)
	defer closeDatabase()

	runner := job.NewRunner(store, nil)
	runner.Register("test.cancel", func(ctx context.Context, _ job.Job, _ job.Reporter) error {
		<-ctx.Done()
		return ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)
	defer runner.Stop()

	queued, err := runner.Enqueue(ctx, job.NewJob{Type: "test.cancel"})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}
	waitForStatus(t, store, queued.ID, job.StatusRunning)
	if err := runner.Cancel(ctx, queued.ID); err != nil {
		t.Fatalf("cancel job: %v", err)
	}
	waitForStatus(t, store, queued.ID, job.StatusCanceled)
}

func TestRecoverInterruptedJob(t *testing.T) {
	store, closeDatabase := newStore(t)
	defer closeDatabase()
	ctx := context.Background()

	queued, err := store.Enqueue(ctx, job.NewJob{Type: "test.interrupted"})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}
	claimed, err := store.ClaimNext(ctx, time.Minute)
	if err != nil {
		t.Fatalf("claim job: %v", err)
	}
	if claimed == nil || claimed.ID != queued.ID {
		t.Fatal("expected queued job to be claimed")
	}

	recovered, err := store.RecoverInterrupted(ctx)
	if err != nil {
		t.Fatalf("recover jobs: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("expected one recovered job, got %d", recovered)
	}
	result, err := store.Get(ctx, queued.ID)
	if err != nil {
		t.Fatalf("get recovered job: %v", err)
	}
	if result.Status != job.StatusFailed {
		t.Fatalf("expected failed status, got %s", result.Status)
	}
}

func TestEnqueueLocksResourceUntilTerminalState(t *testing.T) {
	store, closeDatabase := newStore(t)
	defer closeDatabase()
	ctx := context.Background()
	if _, err := store.Enqueue(ctx, job.NewJob{Type: "test", LockKey: "server:one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Enqueue(ctx, job.NewJob{Type: "test", LockKey: "server:one"}); err == nil {
		t.Fatal("duplicate queued mutation accepted")
	}
	if _, err := store.Enqueue(ctx, job.NewJob{Type: "test", LockKey: "server:two"}); err != nil {
		t.Fatal("unrelated resource blocked", err)
	}
	if _, err := store.ClaimNext(ctx, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Enqueue(ctx, job.NewJob{Type: "test", LockKey: "server:one"}); err != nil {
		t.Fatal("terminal job retained lock", err)
	}
}

func newStore(t *testing.T) (*job.Store, func()) {
	t.Helper()
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	return job.NewStore(db), func() {
		if err := db.Close(); err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("close test database: %v", err)
		}
	}
}

func waitForStatus(t *testing.T, store *job.Store, id string, expected job.Status) job.Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		result, err := store.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("get job: %v", err)
		}
		if result.Status == expected {
			return result
		}
		time.Sleep(20 * time.Millisecond)
	}
	result, _ := store.Get(context.Background(), id)
	t.Fatalf("timed out waiting for %s; current status is %s", expected, result.Status)
	return job.Job{}
}
