package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"ideploy/internal/deployment"
	"ideploy/internal/job"
	"ideploy/internal/server"
)

func (s *SystemService) deployAndCheckCompose(ctx context.Context, store *deployment.Store, release deployment.Release, target deployment.Target, host server.Connection, jobID string, reporter job.Reporter, remote *server.Remote, image, revision, manifest, detail string) (result error) {
	if !strings.HasPrefix(image, "compose:") || manifest == "" {
		return errors.New("发布制品缺少 Compose 快照")
	}
	previous, err := store.LatestSucceededRelease(ctx, target.ID)
	if err != nil && !errors.Is(err, deployment.ErrNotFound) {
		return err
	}
	containers, err := remote.ListContainers(ctx, host)
	if err != nil {
		return fmt.Errorf("检查 Compose 项目的当前状态: %w", err)
	}
	before := deployment.Match(target, containers)
	if previous.ID != "" && previous.ImageRef != "compose:"+target.WorkloadName+":"+previous.Version {
		previous = deployment.Release{}
	}
	if previous.ID != "" {
		if err := validateRollbackArtifact(previous.ImageRef, previous.Manifest); err != nil {
			return fmt.Errorf("无法验证上一版本的 Compose 快照: %w", err)
		}
	} else if len(before) != 0 {
		return errors.New("目标 Compose 项目已有容器，但没有可恢复的 iDeploy 发布快照；请使用未占用的 Compose 项目名")
	}
	stage := "upload"
	upAttempted := false
	ingressApplied := false
	var ingressBefore server.ExistingNginx
	defer func() {
		if result == nil {
			return
		}
		var ingressStateErr appliedIngressStateError
		if ingressApplied || errors.As(result, &ingressStateErr) {
			result = fmt.Errorf("%w；入口已更新，保留当前 Compose 服务以维持访问，请核对发布记录", result)
			return
		}
		reporter.Log("发布未完成，正在恢复 Compose 工作负载")
		restoreCtx, cancel := context.WithTimeout(context.Background(), 17*time.Minute)
		defer cancel()
		restoreCtx, releaseLease, err := s.leaseSharedPublication(restoreCtx, target.ServerID, jobID)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("无法取得 Compose 恢复租约: %w", err))
			return
		}
		defer releaseLease()
		if err := remote.WaitComposeStage(restoreCtx, host, target.ServerID, jobID, target.ID, stage); err != nil {
			result = errors.Join(result, err)
			return
		}
		if ingressBefore.Container != "" {
			if err := verifyComposeIngressUnchanged(restoreCtx, remote, host, target.ServerID, jobID, ingressBefore); err != nil {
				result = errors.Join(result, fmt.Errorf("%w；保留当前 Compose 服务以维持访问", err))
				return
			}
		}
		if err := restoreComposeWorkload(restoreCtx, store, remote, host, target, previous, before, jobID, release.Version, upAttempted); err != nil {
			result = errors.Join(result, fmt.Errorf("恢复 Compose 工作负载失败: %w", err))
		} else if previous.ID != "" {
			result = fmt.Errorf("%w；已恢复上一版 Compose 配置", result)
		} else if upAttempted {
			result = fmt.Errorf("%w；已清理本次发布创建的 Compose 服务", result)
		}
	}()

	if err := remote.UploadComposeWithAgent(ctx, host, target.ServerID, jobID, target.ID, manifest); err != nil {
		return err
	}
	stage = "pull"
	if err := remote.PullComposeWithAgent(ctx, host, target.ServerID, jobID, target.ID, target.WorkloadName, release.Version); err != nil {
		return err
	}
	if err := store.UpdateRelease(ctx, release.ID, deployment.ReleaseDeploying, detail, image, revision, manifest); err != nil {
		return err
	}
	stage, upAttempted = "up", true
	if err := remote.UpComposeWithAgent(ctx, host, target.ServerID, jobID, target.ID, target.WorkloadName, release.Version); err != nil {
		return err
	}
	return s.finishReleaseHealthCheck(ctx, store, release, target, host, reporter, image, revision, manifest, func() error {
		if err := s.applyReleasePathObservingIngress(ctx, release.ID, jobID, host, func(before server.ExistingNginx) {
			ingressBefore = before
		}); err != nil {
			return err
		}
		ingressApplied = ingressBefore.Container != ""
		return nil
	})
}

func verifyComposeIngressUnchanged(ctx context.Context, remote *server.Remote, host server.Connection, serverID, jobID string, before server.ExistingNginx) error {
	if err := remote.WaitNginxApply(ctx, host, serverID, jobID, before.Container); err != nil {
		return err
	}
	current, err := remote.InspectExistingNginx(ctx, host)
	if err != nil {
		return fmt.Errorf("无法确认当前入口配置: %w", err)
	}
	if current.Container != before.Container || current.Hash != before.Hash {
		return errors.New("入口已变化，无法安全恢复上一版 Compose 服务")
	}
	return nil
}

func restoreComposeWorkload(ctx context.Context, store *deployment.Store, remote *server.Remote, host server.Connection, target deployment.Target, previous deployment.Release, before []server.Container, jobID, version string, upAttempted bool) error {
	if len(before) == 0 && upAttempted {
		if err := remote.DownCompose(ctx, host, target.ID, target.WorkloadName, version); err != nil {
			return err
		}
	}
	if previous.ID != "" {
		restoreID := server.AgentStageID(jobID, "restore-compose")
		if err := remote.UploadComposeWithAgent(ctx, host, target.ServerID, restoreID, target.ID, previous.Manifest); err != nil {
			return err
		}
		if upAttempted && len(before) != 0 {
			wasRunning := false
			for _, container := range before {
				wasRunning = wasRunning || container.State == "running" || container.State == "restarting"
			}
			if !wasRunning {
				if err := remote.RestoreStoppedCompose(ctx, host, target.ID, target.WorkloadName, previous.Version); err != nil {
					return err
				}
			} else if err := remote.UpComposeWithAgent(ctx, host, target.ServerID, restoreID, target.ID, target.WorkloadName, previous.Version); err != nil {
				return err
			}
		}
	}
	if upAttempted && releaseHealthIssue(before) == "" {
		containers, _, err := waitReleaseHealth(ctx, remote, host, target)
		if err != nil {
			return err
		}
		return store.SaveSnapshot(ctx, host.ID, containers)
	}
	containers, err := remote.ListContainers(ctx, host)
	if err != nil {
		return err
	}
	return store.SaveSnapshot(ctx, host.ID, containers)
}
