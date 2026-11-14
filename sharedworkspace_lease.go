package main

import (
	"context"
	"errors"
	"time"
)

// A shared publication holds the server-wide lease for the entire job. The
// request ID is the durable local job ID, so restart/retry can renew it.
func (s *SystemService) leaseSharedPublication(ctx context.Context, serverID, requestID string) (context.Context, func(), error) {
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil || status.Mode == "local" {
		return ctx, func() {}, err
	}
	if status.Mode != "shared" {
		return nil, nil, errors.New("共享工作区迁移尚未完成")
	}
	client, err := s.sharedClient(ctx, status.ServerID)
	if err != nil {
		return nil, nil, err
	}
	leaseID := "server:" + serverID
	if err := client.AcquireLease(ctx, leaseID, requestID); err != nil {
		return nil, nil, err
	}
	jobCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-jobCtx.Done():
				return
			case <-ticker.C:
				renewCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
				err := client.AcquireLease(renewCtx, leaseID, requestID)
				stop()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	release := func() {
		cancel()
		<-done
		releaseCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		_ = client.ReleaseLease(releaseCtx, leaseID, requestID)
	}
	return jobCtx, release, nil
}

func (s *SystemService) ensureSharedPublicationReady() error {
	s.sharedWriteMu.Lock()
	defer s.sharedWriteMu.Unlock()
	return s.prepareSharedWrite("", nil)
}
