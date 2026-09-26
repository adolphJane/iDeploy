package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"ideploy/internal/deployment"
	"ideploy/internal/job"
	"ideploy/internal/project"
	"ideploy/internal/registry"
	"ideploy/internal/server"
	"strings"
	"time"
)

func toRemoteContainerView(c server.Container) RemoteContainerView {
	return RemoteContainerView{ID: c.ID, Name: c.Name, Image: c.Image, State: c.State, Status: c.Status, Ports: c.Ports, Created: c.Created, ComposeProject: c.ComposeProject, Health: c.Health, OneOff: c.OneOff}
}

func (s *SystemService) GetJob(id string) (JobView, error) {
	store, _, _, err := s.readyState()
	if err != nil {
		return JobView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	value, err := store.Get(ctx, id)
	return toJobView(value), err
}

func (s *SystemService) BindDeployment(id, kind, name string) error {
	store, err := s.deploymentStore()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return store.Bind(ctx, id, kind, name)
}

func (s *SystemService) ConfigureDeployment(id string, input DeploymentInput) error {
	store, err := s.deploymentStore()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return store.Configure(ctx, id, deployment.Input{
		WorkloadKind: input.WorkloadKind, WorkloadName: input.WorkloadName,
		ContainerPorts: input.ContainerPorts, RestartPolicy: input.RestartPolicy,
	})
}

func (s *SystemService) ListDeploymentReleases(deploymentID string) ([]DeploymentReleaseView, error) {
	store, err := s.deploymentStore()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if _, err := store.Get(ctx, deploymentID); err != nil {
		return nil, err
	}
	values, err := store.ListReleases(ctx, deploymentID, 50)
	if err != nil {
		return nil, err
	}
	result := make([]DeploymentReleaseView, 0, len(values))
	for _, value := range values {
		result = append(result, toDeploymentReleaseView(value))
	}
	return result, nil
}

// DetectDeploymentVersion returns an advisory version from the local build
// context. Git projects intentionally return an empty suggestion until their
// source is materialized locally for the release job.
func (s *SystemService) DetectDeploymentVersion(deploymentID string) (DeploymentVersionSuggestionView, error) {
	deployments, err := s.deploymentStore()
	if err != nil {
		return DeploymentVersionSuggestionView{}, err
	}
	projects, err := s.projectStore()
	if err != nil {
		return DeploymentVersionSuggestionView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	target, err := deployments.Get(ctx, strings.TrimSpace(deploymentID))
	if err != nil {
		return DeploymentVersionSuggestionView{}, err
	}
	workspaceProject, err := projects.Get(ctx, target.ProjectID)
	if err != nil {
		return DeploymentVersionSuggestionView{}, err
	}
	sourceVersion, sourceName := "", "默认初始版本"
	if workspaceProject.SourceKind != project.SourceLocal {
		if workspaceProject.RefKind == project.RefTag && releaseVersionPattern.MatchString(strings.TrimSpace(workspaceProject.RefValue)) {
			sourceVersion, sourceName = strings.TrimSpace(workspaceProject.RefValue), "Git tag"
		}
	} else {
		suggestion, err := project.DetectLocalVersion(workspaceProject.LocalPath, workspaceProject.ContextPath)
		if err != nil {
			return DeploymentVersionSuggestionView{}, err
		}
		sourceVersion, sourceName = suggestion.Version, suggestion.Source
	}
	history, err := deployments.VersionHistory(ctx, target.ID)
	if err != nil {
		return DeploymentVersionSuggestionView{}, err
	}
	view := DeploymentVersionSuggestionView{Version: deployment.SuggestVersion(sourceVersion, history), Source: sourceName, Detail: "根据项目版本与全部发布记录自动建议下一可用版本；不修改源码版本", UsedVersions: []string{}}
	for _, r := range history {
		view.UsedVersions = append(view.UsedVersions, r.Version)
		if view.CurrentVersion == "" && r.Status == deployment.ReleaseSucceeded {
			view.CurrentVersion = r.Version
		}
	}
	return view, nil
}

func (s *SystemService) PublishDeployment(input DeploymentReleaseInput) (JobView, error) {
	store, err := s.deploymentStore()
	if err != nil {
		return JobView{}, err
	}
	registries, err := s.registryStore()
	if err != nil {
		return JobView{}, err
	}
	s.mu.RLock()
	runner := s.runner
	s.mu.RUnlock()
	if runner == nil {
		return JobView{}, errors.New("job runner is not ready")
	}
	input.Version = strings.TrimSpace(input.Version)
	if input.Version == "" {
		input.Version = time.Now().UTC().Format("20060102-150405")
	}
	if !releaseVersionPattern.MatchString(input.Version) {
		return JobView{}, errors.New("版本只能包含字母、数字、点、短横线和下划线")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	target, err := store.Get(ctx, input.DeploymentID)
	if err != nil {
		return JobView{}, err
	}
	if target.WorkloadName == "" {
		return JobView{}, errors.New("请先完成部署目标配置")
	}
	history, err := store.VersionHistory(ctx, target.ID)
	if err != nil {
		return JobView{}, err
	}
	if err := deployment.ValidateNewVersion(input.Version, history); err != nil {
		return JobView{}, err
	}
	servers, err := s.serverStore()
	if err != nil {
		return JobView{}, err
	}
	host, err := servers.Get(ctx, target.ServerID)
	if err != nil {
		return JobView{}, err
	}
	probeContext, probeCancel := context.WithTimeout(ctx, 15*time.Second)
	_, err = s.verifyDeploymentServer(probeContext, host)
	probeCancel()
	if err != nil {
		return JobView{}, err
	}
	if strings.TrimSpace(input.RegistryID) == "" {
		return JobView{}, errors.New("请选择镜像仓库")
	}
	registryValue, err := registries.Get(ctx, input.RegistryID)
	if err != nil {
		return JobView{}, err
	}
	if registryValue.Status != registry.StatusHealthy {
		return JobView{}, errors.New("镜像仓库尚未通过 Docker 登录测试")
	}
	if _, err := loadRegistryPushPassword(ctx, registryValue); err != nil {
		return JobView{}, err
	}
	if target.WorkloadKind == "container" {
		projects, err := s.projectStore()
		if err != nil {
			return JobView{}, err
		}
		projectValue, err := projects.Get(ctx, target.ProjectID)
		if err != nil {
			return JobView{}, err
		}
		if err := validateImageRepositoryForRegistry(projectValue.ImageRepository, registryValue); err != nil {
			return JobView{}, err
		}
	}
	release, err := store.CreateRelease(ctx, deployment.ReleaseInput{
		DeploymentID: target.ID, RegistryID: input.RegistryID, Action: "deploy", Version: input.Version,
	})
	if err != nil {
		return JobView{}, err
	}
	payload, err := json.Marshal(struct {
		ReleaseID string `json:"releaseId"`
	}{ReleaseID: release.ID})
	if err != nil {
		return JobView{}, err
	}
	queued, err := runner.Enqueue(ctx, job.NewJob{Type: "deployment.release", LockKey: "server:" + target.ServerID, Payload: string(payload)})
	if err != nil {
		_ = store.UpdateRelease(ctx, release.ID, deployment.ReleaseFailed, err.Error(), "", "", "")
		return JobView{}, err
	}
	if err := store.AttachReleaseJob(ctx, release.ID, queued.ID); err != nil {
		return JobView{}, err
	}
	return toJobView(queued), nil
}

func (s *SystemService) verifyDeploymentServer(ctx context.Context, connection server.Connection) (server.Connection, error) {
	result := server.NewRemote().Probe(ctx, connection)
	servers, err := s.serverStore()
	if err != nil {
		return connection, err
	}
	updateContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	updated, err := servers.UpdateProbe(updateContext, connection.ID, result.Status, result.Detail)
	if err != nil {
		return connection, err
	}
	s.emitServerView(toServerView(updated))
	if result.Status != server.StatusHealthy {
		return updated, fmt.Errorf("目标服务器发布前检查失败：%s", result.Detail)
	}
	return updated, nil
}

func (s *SystemService) RollbackDeployment(deploymentID, sourceReleaseID string) (JobView, error) {
	store, err := s.deploymentStore()
	if err != nil {
		return JobView{}, err
	}
	s.mu.RLock()
	runner := s.runner
	s.mu.RUnlock()
	if runner == nil {
		return JobView{}, errors.New("job runner is not ready")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	target, err := store.Get(ctx, deploymentID)
	if err != nil {
		return JobView{}, err
	}
	source, err := store.GetRelease(ctx, sourceReleaseID)
	if err != nil {
		return JobView{}, err
	}
	if source.DeploymentID != deploymentID || source.Status != deployment.ReleaseSucceeded {
		return JobView{}, errors.New("只能回滚到当前部署目标的成功版本")
	}
	if err := validateRollbackArtifact(source.ImageRef, source.Manifest); err != nil {
		return JobView{}, err
	}
	release, err := store.CreateRelease(ctx, deployment.ReleaseInput{
		DeploymentID: deploymentID, RegistryID: source.RegistryID, Action: "rollback", Version: source.Version,
		ImageRef: source.ImageRef, Manifest: source.Manifest, RollbackToID: source.ID,
	})
	if err != nil {
		return JobView{}, err
	}
	payload, err := json.Marshal(struct {
		ReleaseID string `json:"releaseId"`
		SourceID  string `json:"sourceId"`
	}{ReleaseID: release.ID, SourceID: source.ID})
	if err != nil {
		return JobView{}, err
	}
	queued, err := runner.Enqueue(ctx, job.NewJob{Type: "deployment.rollback", LockKey: "server:" + target.ServerID, Payload: string(payload)})
	if err != nil {
		_ = store.UpdateRelease(ctx, release.ID, deployment.ReleaseFailed, err.Error(), "", "", "")
		return JobView{}, err
	}
	if err := store.AttachReleaseJob(ctx, release.ID, queued.ID); err != nil {
		return JobView{}, err
	}
	return toJobView(queued), nil
}

func (s *SystemService) OperateDeployment(id, action string) (JobView, error) {
	if action != "start" && action != "stop" && action != "restart" {
		return JobView{}, errors.New("不支持的部署操作")
	}
	store, err := s.deploymentStore()
	if err != nil {
		return JobView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	target, err := store.Get(ctx, id)
	if err != nil {
		return JobView{}, err
	}
	if target.WorkloadName == "" {
		return JobView{}, errors.New("请先绑定明确的远端工作负载")
	}
	return s.enqueueServerJob(target.ServerID, "deployment.operation", struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}{id, action})
}

func (s *SystemService) deploymentOperationHandler(ctx context.Context, task job.Job, reporter job.Reporter) error {
	var p struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(task.Payload), &p); err != nil {
		return err
	}
	if p.Action != "start" && p.Action != "stop" && p.Action != "restart" {
		return errors.New("invalid operation")
	}
	store, err := s.deploymentStore()
	if err != nil {
		return err
	}
	target, err := store.Get(ctx, p.ID)
	if err != nil {
		return err
	}
	hosts, err := s.serverStore()
	if err != nil {
		return err
	}
	host, err := hosts.Get(ctx, target.ServerID)
	if err != nil {
		return err
	}
	remote := server.NewRemote()
	reporter.Progress(10, "正在重新核对远端工作负载")
	all, err := remote.ListContainers(ctx, host)
	if err != nil {
		return err
	}
	selected := deployment.Match(target, all)
	if len(selected) == 0 {
		return errors.New("远端工作负载不存在，请刷新并检查绑定")
	}
	if p.Action != "restart" {
		desired := "running"
		if p.Action == "stop" {
			desired = "stopped"
		}
		if err := store.SetDesired(ctx, target.ID, desired); err != nil {
			return err
		}
	}
	var failures []error
	for i, c := range selected {
		if ctx.Err() != nil {
			failures = append(failures, ctx.Err())
			break
		}
		reporter.Progress(15+i*65/len(selected), fmt.Sprintf("%s · %s", p.Action, c.Name))
		if err := remote.OperateContainer(ctx, host, p.Action, c.ID); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", c.Name, err))
		}
	}
	// Reconcile even after cancellation; interrupted SSH is not proof of rollback.
	verify, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	observed, err := remote.ListContainers(verify, host)
	if err != nil {
		failures = append(failures, fmt.Errorf("操作后状态核对失败: %w", err))
	} else {
		if err := store.SaveSnapshot(verify, host.ID, observed); err != nil {
			failures = append(failures, err)
		}
		actual := deployment.Match(target, observed)
		if len(actual) != len(selected) {
			failures = append(failures, errors.New("操作期间工作负载成员发生变化，请重新检查"))
		}
		for _, c := range actual {
			if p.Action == "stop" && c.State != "exited" && c.State != "created" || p.Action != "stop" && (c.State != "running" || c.Health == "unhealthy") {
				failures = append(failures, fmt.Errorf("%s 未达到目标状态（%s / %s）", c.Name, c.State, c.Health))
			}
		}
	}
	reporter.Progress(95, "已核对远端状态；未配置健康检查的服务仅验证运行状态")
	return errors.Join(failures...)
}
