package job

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Handler func(context.Context, Job, Reporter) error

type Reporter interface {
	Progress(percent int, message string)
	Log(message string)
}

type Runner struct {
	store    *Store
	handlers map[string]Handler
	emit     func(Job)
	wake     chan struct{}

	mu      sync.Mutex
	active  map[string]context.CancelFunc
	cancel  context.CancelFunc
	wait    sync.WaitGroup
	started bool
}

func NewRunner(store *Store, emit func(Job)) *Runner {
	if emit == nil {
		emit = func(Job) {}
	}
	return &Runner{
		store:    store,
		handlers: make(map[string]Handler),
		emit:     emit,
		wake:     make(chan struct{}, 1),
		active:   make(map[string]context.CancelFunc),
	}
}

func (r *Runner) Register(jobType string, handler Handler) {
	r.handlers[jobType] = handler
}

func (r *Runner) Start(parent context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	r.started = true
	r.wait.Add(1)
	go r.loop(ctx)
}

func (r *Runner) Stop() {
	r.mu.Lock()
	if !r.started {
		r.mu.Unlock()
		return
	}
	r.cancel()
	for _, cancel := range r.active {
		cancel()
	}
	r.mu.Unlock()
	r.wait.Wait()

	r.mu.Lock()
	r.started = false
	r.mu.Unlock()
}

func (r *Runner) Enqueue(ctx context.Context, input NewJob) (Job, error) {
	queued, err := r.store.Enqueue(ctx, input)
	if err != nil {
		return Job{}, err
	}
	r.emit(queued)
	select {
	case r.wake <- struct{}{}:
	default:
	}
	return queued, nil
}

func (r *Runner) Cancel(ctx context.Context, id string) error {
	if err := r.store.RequestCancel(ctx, id); err != nil {
		return err
	}
	r.mu.Lock()
	activeCancel := r.active[id]
	r.mu.Unlock()
	if activeCancel != nil {
		activeCancel()
	}
	updated, err := r.store.Get(ctx, id)
	if err == nil {
		r.emit(updated)
	}
	return nil
}

func (r *Runner) loop(ctx context.Context) {
	defer r.wait.Done()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := r.runNext(ctx); err != nil && !errors.Is(err, context.Canceled) {
			// The next poll retries queue access. Individual job failures are
			// persisted by runNext and do not escape this method.
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-r.wake:
		}
	}
}

func (r *Runner) runNext(ctx context.Context) error {
	claimed, err := r.store.ClaimNext(ctx, 30*time.Second)
	if err != nil || claimed == nil {
		return err
	}
	r.emit(*claimed)

	handler, ok := r.handlers[claimed.Type]
	if !ok {
		err := fmt.Errorf("no handler registered for job type %q", claimed.Type)
		_ = r.store.Finish(context.Background(), claimed.ID, StatusFailed, "Unsupported job", err.Error())
		r.emitLatest(claimed.ID)
		return nil
	}

	jobContext, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.active[claimed.ID] = cancel
	r.mu.Unlock()

	reporter := &storeReporter{store: r.store, jobID: claimed.ID, emit: r.emitLatest}
	handlerErr := handler(jobContext, *claimed, reporter)
	cancel()

	r.mu.Lock()
	delete(r.active, claimed.ID)
	r.mu.Unlock()

	finishContext, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()

	latest, getErr := r.store.Get(finishContext, claimed.ID)
	if getErr != nil {
		return getErr
	}
	if latest.CancelRequested || errors.Is(handlerErr, context.Canceled) {
		if err := r.store.Finish(finishContext, claimed.ID, StatusCanceled, "Canceled", ""); err != nil {
			return err
		}
	} else if handlerErr != nil {
		if err := r.store.Finish(finishContext, claimed.ID, StatusFailed, "Failed", handlerErr.Error()); err != nil {
			return err
		}
	} else if err := r.store.Finish(finishContext, claimed.ID, StatusSucceeded, "Completed", ""); err != nil {
		return err
	}
	r.emitLatest(claimed.ID)
	return nil
}

func (r *Runner) emitLatest(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	latest, err := r.store.Get(ctx, id)
	if err == nil {
		r.emit(latest)
	}
}

type storeReporter struct {
	store *Store
	jobID string
	emit  func(string)
}

func (r *storeReporter) Progress(percent int, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if r.store.UpdateProgress(ctx, r.jobID, percent, message) == nil {
		r.emit(r.jobID)
	}
}

func (r *storeReporter) Log(message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = r.store.AppendLog(ctx, r.jobID, "info", message)
}
