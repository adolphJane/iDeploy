package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"ideploy/internal/buildexecutor"
	"ideploy/internal/deployment"
	"ideploy/internal/gitauth"
	"ideploy/internal/gitdelivery"
	"ideploy/internal/job"
	"ideploy/internal/project"
	"ideploy/internal/registry"
	"ideploy/internal/releaseplan"
	"ideploy/internal/sharedworkspace"
)

var remoteSourceCommitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

func (s *SystemService) buildRemoteRelease(ctx context.Context, store *deployment.Store, release deployment.Release, source preparedSource,
	projectValue project.Project, registryValue registry.Connection, workloadKind, workloadName, platform, basePath, requestID string, reporter job.Reporter) (string, string, error) {
	if projectValue.SourceKind != project.SourceGit || !remoteSourceCommitPattern.MatchString(source.revision) {
		return "", "", errors.New("远端构建需要固定的 Git 提交")
	}
	document := releaseplan.Document{SchemaVersion: 1, DeploymentID: release.DeploymentID, ReleaseID: release.ID, SourceCommit: source.revision,
		WorkloadKind: workloadKind, ContextPath: projectValue.ContextPath, Dockerfile: projectValue.DockerfilePath, ComposeFile: projectValue.ComposePath,
		ImagePath: projectValue.ImageRepository, Platform: platform, BasePath: basePath}
	if err := document.Validate(); err != nil {
		return "", "", err
	}
	reporter.Progress(18, "正在创建并推送部署文件")
	var gitEnvironment []string
	var err error
	if projectValue.CredentialRef != "" {
		credential, loadErr := gitauth.Load(ctx, projectValue.CredentialRef)
		if loadErr != nil {
			return "", "", loadErr
		}
		var cleanup func()
		gitEnvironment, cleanup, err = gitauth.Environment(credential)
		if err != nil {
			return "", "", err
		}
		defer cleanup()
	}
	delivered, err := gitdelivery.Push(ctx, projectValue.RepositoryURL, source.revision, document, s.cacheDirectory(), gitEnvironment...)
	if err != nil {
		return "", "", err
	}
	if err := store.SetReleaseDelivery(ctx, release.ID, source.revision, delivered.DeliveryRef, delivered.DeliveryCommit, delivered.Digest, ""); err != nil {
		return "", "", err
	}
	if err := s.pushSharedWorkspace(ctx); err != nil {
		return "", "", fmt.Errorf("上传远端构建所需的发布记录和凭据: %w", err)
	}
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil || status.Mode != "shared" {
		return "", "", errors.New("iDeploy 服务连接已中断")
	}
	client, err := s.sharedClient(ctx, status.ServerID)
	if err != nil {
		return "", "", err
	}
	defer func() {
		if ctx.Err() != nil {
			cancelCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			s.deliverRemoteCancellation(cancelCtx, client, requestID)
		}
	}()
	remote, err := client.CreateRemoteRelease(ctx, sharedworkspace.RemoteReleaseJob{RequestID: requestID, DeploymentID: release.DeploymentID, ReleaseID: release.ID,
		DeliveryCommit: delivered.DeliveryCommit, DeliveryDigest: delivered.Digest, RepositoryURL: projectValue.RepositoryURL,
		Version: release.Version, RegistryID: registryValue.ID, RegistryHost: registry.DockerHost(registryValue.Endpoint), RegistryUser: registryValue.Username})
	if err != nil {
		return "", "", fmt.Errorf("提交远端构建任务: %w", err)
	}
	if err := store.SetReleaseDelivery(ctx, release.ID, source.revision, delivered.DeliveryRef, delivered.DeliveryCommit, delivered.Digest, remote.ID); err != nil {
		return "", "", err
	}
	if err := store.UpdateRelease(ctx, release.ID, deployment.ReleaseBuilding, "iDeploy 服务正在构建镜像", "", source.revision, ""); err != nil {
		return "", "", err
	}
	reporter.Progress(30, "等待 iDeploy 服务构建并推送镜像")
	result, err := waitRemoteBuild(ctx, func(ctx context.Context) (sharedworkspace.RemoteReleaseJob, error) {
		return client.RemoteRelease(ctx, remote.ID)
	})
	if err != nil {
		return "", "", err
	}
	if workloadKind == "compose" {
		if result.ImageRef != "compose:"+release.DeploymentID+":"+release.Version || result.Manifest == "" || len(result.Manifest) > 900*1024 {
			return "", "", errors.New("远端 Compose 构建未返回完整制品")
		}
		if _, err := buildexecutor.PinComposeManifest(result.Manifest); err != nil {
			return "", "", fmt.Errorf("远端 Compose 快照无效: %w", err)
		}
		return "compose:" + workloadName + ":" + release.Version, result.Manifest, nil
	}
	if !pinnedImagePattern.MatchString(result.ImageRef) {
		return "", "", errors.New("远端构建未返回有效镜像摘要")
	}
	return result.ImageRef, "", nil
}

func waitRemoteBuild(ctx context.Context, lookup func(context.Context) (sharedworkspace.RemoteReleaseJob, error)) (sharedworkspace.RemoteReleaseJob, error) {
	buildCtx, stop := context.WithTimeout(ctx, time.Hour)
	defer stop()
	queueTimer := time.NewTimer(10 * time.Minute)
	defer queueTimer.Stop()
	queueTimeout := queueTimer.C
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	readFailures := 0
	for {
		readCtx, cancel := context.WithTimeout(buildCtx, 10*time.Second)
		result, err := lookup(readCtx)
		cancel()
		if err != nil {
			if buildCtx.Err() != nil {
				return result, fmt.Errorf("远端构建等待结束: %w", buildCtx.Err())
			}
			var statusError *sharedworkspace.HTTPStatusError
			readFailures++
			if readFailures == 5 || (errors.As(err, &statusError) && statusError.Code >= http.StatusBadRequest && statusError.Code < http.StatusInternalServerError && statusError.Code != http.StatusTooManyRequests) {
				return result, fmt.Errorf("无法读取远端构建状态: %w", err)
			}
		} else {
			readFailures = 0
			switch result.Status {
			case "artifact_ready":
				return result, nil
			case "queued":
			case "running", "cancel_requested":
				queueTimeout = nil
			case "canceled":
				return result, context.Canceled
			case "failed", "needs_reconcile":
				return result, fmt.Errorf("远端构建未完成，任务状态 %s；请在服务端对账", result.Status)
			default:
				return result, fmt.Errorf("远端构建任务状态 %q 无法识别", result.Status)
			}
		}
		select {
		case <-buildCtx.Done():
			return result, fmt.Errorf("远端构建等待结束: %w", buildCtx.Err())
		case <-queueTimeout:
			return result, errors.New("远端构建排队超过 10 分钟，等待已结束，请检查构建执行器")
		case <-ticker.C:
		}
	}
}
