package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ideploy/internal/buildexecutor"
	"ideploy/internal/deployment"
	"ideploy/internal/sharedworkspace"
)

// A desktop interruption can happen after the worker pushed an immutable image
// but before the local release recorded its digest. Reconcile before retrying
// so the already-built artifact is reused instead of rebuilt.
func (s *SystemService) recoverRemoteReleaseArtifact(ctx context.Context, store *deployment.Store, source deployment.Release) (deployment.Release, error) {
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil || status.Mode != "shared" {
		return source, errors.New("恢复远端制品需要连接 iDeploy 服务")
	}
	client, err := s.sharedClient(ctx, status.ServerID)
	if err != nil {
		return source, err
	}
	remote, source, err := locateRemoteRelease(ctx, store, source, client)
	if err != nil {
		var statusError *sharedworkspace.HTTPStatusError
		if source.RemoteJobID == "" && errors.As(err, &statusError) && statusError.Code == http.StatusNotFound {
			return source, nil
		}
		return source, fmt.Errorf("查询远端构建任务失败: %w", err)
	}
	switch remote.Status {
	case "artifact_ready":
		return saveRecoveredRemoteArtifact(ctx, store, source, remote)
	case "queued", "running", "cancel_requested":
		return source, errors.New("远端构建仍在进行，请等待完成后再恢复发布")
	case "needs_reconcile":
		return source, errors.New("远端构建任务需要对账，暂不能安全地重新构建")
	case "failed", "canceled":
		return source, nil
	default:
		return source, fmt.Errorf("远端构建任务状态 %q 无法用于恢复", remote.Status)
	}
}

// Use the same artifact checks for manual retry and automatic recovery. The
// remote reference uses the deployment ID; the saved release uses the project name.
func saveRecoveredRemoteArtifact(ctx context.Context, store *deployment.Store, source deployment.Release, remote sharedworkspace.RemoteReleaseJob) (deployment.Release, error) {
	if remote.Status != "artifact_ready" {
		return source, errors.New("远端发布尚无可恢复制品")
	}
	imageRef, manifest := remote.ImageRef, remote.Manifest
	if strings.HasPrefix(imageRef, "compose:") {
		if imageRef != "compose:"+source.DeploymentID+":"+source.ArtifactTag() || manifest == "" || len(manifest) > 900*1024 {
			return source, errors.New("远端 Compose 制品不完整")
		}
		if _, err := buildexecutor.PinComposeManifest(manifest); err != nil {
			return source, fmt.Errorf("远端 Compose 快照无效: %w", err)
		}
		target, err := store.Get(ctx, source.DeploymentID)
		if err != nil || target.WorkloadKind != "compose" {
			return source, errors.New("远端 Compose 制品与部署类型不符")
		}
		imageRef = "compose:" + target.WorkloadName + ":" + source.Version
	} else if !pinnedImagePattern.MatchString(imageRef) || manifest != "" {
		return source, errors.New("远端构建制品没有有效镜像摘要")
	}
	if err := store.UpdateRelease(ctx, source.ID, source.Status, "已从 iDeploy 服务恢复镜像制品", imageRef, source.SourceRevision, manifest); err != nil {
		return source, err
	}
	return store.GetRelease(ctx, source.ID)
}

func locateRemoteRelease(ctx context.Context, store *deployment.Store, source deployment.Release, client *sharedworkspace.Client) (sharedworkspace.RemoteReleaseJob, deployment.Release, error) {
	var remote sharedworkspace.RemoteReleaseJob
	var err error
	if source.RemoteJobID != "" {
		remote, err = client.RemoteRelease(ctx, source.RemoteJobID)
	} else if source.JobID != "" && source.DeliveryCommit != "" {
		remote, err = client.RemoteReleaseByRequest(ctx, source.JobID)
	} else {
		return remote, source, errors.New("本地发布缺少远端任务标识")
	}
	if err != nil {
		return remote, source, err
	}
	if remote.ReleaseID != source.ID || remote.DeploymentID != source.DeploymentID || remote.DeliveryCommit != source.DeliveryCommit || remote.DeliveryDigest != source.DeliveryDigest || remote.RequestID != source.JobID {
		return remote, source, errors.New("远端构建任务与本地发布记录不一致")
	}
	if source.RemoteJobID == "" {
		if err := store.SetReleaseDelivery(ctx, source.ID, source.SourceCommit, source.DeliveryRef, source.DeliveryCommit, source.DeliveryDigest, remote.ID); err != nil {
			return remote, source, err
		}
		source.RemoteJobID = remote.ID
	}
	return remote, source, nil
}

func (s *SystemService) startRemoteReleaseRecovery(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	s.mu.Lock()
	s.remoteRecoveryCancel = cancel
	s.remoteRecoveryDone = done
	s.mu.Unlock()
	go func() {
		defer close(done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			s.reconcileRemoteCancellations(ctx)
			s.reconcileInterruptedRemoteReleases(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *SystemService) deliverRemoteCancellation(ctx context.Context, client *sharedworkspace.Client, requestID string) {
	jobs, _, _, err := s.readyState()
	if err != nil {
		return
	}
	task, err := jobs.Get(ctx, requestID)
	if err != nil || !task.CancelRequested {
		return
	}
	if err := client.CancelRemoteRelease(ctx, requestID); err != nil {
		return
	}
	if store, err := s.deploymentStore(); err == nil {
		_ = store.MarkRemoteCancellationDelivered(ctx, requestID)
	}
}

func (s *SystemService) reconcileRemoteCancellations(ctx context.Context) {
	store, err := s.deploymentStore()
	if err != nil {
		return
	}
	scanCtx, stop := context.WithTimeout(ctx, 3*time.Second)
	ids, err := store.PendingRemoteCancellations(scanCtx)
	stop()
	if err != nil || len(ids) == 0 {
		return
	}
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil || status.Mode != "shared" {
		return
	}
	client, err := s.sharedClient(ctx, status.ServerID)
	if err != nil {
		return
	}
	for _, id := range ids {
		requestCtx, stop := context.WithTimeout(ctx, 8*time.Second)
		s.deliverRemoteCancellation(requestCtx, client, id)
		stop()
	}
}

func (s *SystemService) reconcileInterruptedRemoteReleases(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	store, err := s.deploymentStore()
	if err != nil {
		return
	}
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	values, err := store.InterruptedRemoteReleases(readCtx)
	cancel()
	if err != nil || len(values) == 0 {
		return
	}
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil || status.Mode != "shared" {
		return
	}
	client, err := s.sharedClient(ctx, status.ServerID)
	if err != nil {
		return
	}
	for _, source := range values {
		if ctx.Err() != nil {
			return
		}
		if source.ImageRef == "" {
			probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			remote, located, readErr := locateRemoteRelease(probeCtx, store, source, client)
			if readErr == nil && remote.Status == "artifact_ready" {
				_, readErr = saveRecoveredRemoteArtifact(probeCtx, store, located, remote)
			}
			cancel()
			var statusError *sharedworkspace.HTTPStatusError
			noSubmission := source.RemoteJobID == "" && errors.As(readErr, &statusError) && statusError.Code == http.StatusNotFound
			if !noSubmission && (readErr != nil || remote.Status != "artifact_ready") {
				continue
			}
		}
		if err := releaseInterruptedPublicationLease(ctx, store, client, source); err != nil {
			continue
		}
		_, _ = s.RetryRelease(source.ID)
	}
}

func releaseInterruptedPublicationLease(ctx context.Context, store *deployment.Store, client *sharedworkspace.Client, source deployment.Release) error {
	if source.JobID == "" {
		return nil
	}
	leaseCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	target, err := store.Get(leaseCtx, source.DeploymentID)
	if err != nil {
		return err
	}
	// The service only deletes the lease if both the previous job ID and the
	// authenticated owner match, so another device's release is unaffected.
	err = client.ReleaseLease(leaseCtx, "server:"+target.ServerID, source.JobID)
	var statusError *sharedworkspace.HTTPStatusError
	if errors.As(err, &statusError) && statusError.Code == http.StatusNotFound {
		return nil
	}
	return err
}
