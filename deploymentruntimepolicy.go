package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"ideploy/internal/deployment"
	"ideploy/internal/server"
	"ideploy/internal/sharedworkspace"
)

type DeploymentRuntimePolicyInput struct {
	DeploymentID     string                   `json:"deploymentId"`
	ExpectedRevision int64                    `json:"expectedRevision"`
	Policy           deployment.RuntimePolicy `json:"policy"`
}

func (s *SystemService) GetDeploymentRuntimePolicy(id string) (deployment.RuntimePolicy, error) {
	store, err := s.deploymentStore()
	if err != nil {
		return deployment.RuntimePolicy{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	id = strings.TrimSpace(id)
	if _, err := store.Get(ctx, id); err != nil {
		return deployment.RuntimePolicy{}, err
	}
	return store.RuntimePolicy(ctx, id)
}

func (s *SystemService) UpdateDeploymentRuntimePolicy(input DeploymentRuntimePolicyInput) (deployment.RuntimePolicy, error) {
	s.sharedWriteMu.Lock()
	defer s.sharedWriteMu.Unlock()
	store, err := s.deploymentStore()
	if err != nil {
		return deployment.RuntimePolicy{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()
	target, err := store.Get(ctx, strings.TrimSpace(input.DeploymentID))
	if err != nil {
		return deployment.RuntimePolicy{}, err
	}
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil {
		return deployment.RuntimePolicy{}, err
	}
	if status.Mode == "local" {
		return store.SaveRuntimePolicy(ctx, target.ID, input.ExpectedRevision, input.Policy)
	}
	if status.Mode != "shared" || !status.Unlocked {
		return deployment.RuntimePolicy{}, errors.New("请先解锁共享工作区，再修改发布策略")
	}
	client, err := s.sharedClient(ctx, status.ServerID)
	if err != nil {
		return deployment.RuntimePolicy{}, err
	}
	info, err := client.Workspace(ctx)
	if err != nil {
		return deployment.RuntimePolicy{}, err
	}
	if info.APIVersion < sharedworkspace.RuntimePolicyAPIVersion {
		return deployment.RuntimePolicy{}, errors.New("请先升级 iDeploy 共享服务以支持发布策略")
	}
	ctx, releaseLease, err := s.leaseSharedPublication(ctx, target.ServerID, rand.Text())
	if err != nil {
		return deployment.RuntimePolicy{}, err
	}
	defer releaseLease()
	if err := s.SyncSharedWorkspace(); err != nil {
		return deployment.RuntimePolicy{}, err
	}
	saved, err := store.SaveRuntimePolicy(ctx, target.ID, input.ExpectedRevision, input.Policy)
	if err != nil {
		return saved, err
	}
	if err := s.pushSharedWorkspace(ctx); err != nil {
		return saved, fmt.Errorf("策略修订 %d 已保存在本机，共享同步失败: %w", saved.Revision, err)
	}
	return saved, nil
}

func (s *SystemService) verifySharedRuntimePolicy(ctx context.Context, target deployment.Target, local deployment.RuntimePolicy) error {
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil || status.Mode == "local" {
		return err
	}
	if status.Mode != "shared" {
		return errors.New("共享工作区尚未就绪")
	}
	client, err := s.sharedClient(ctx, status.ServerID)
	if err != nil {
		return err
	}
	info, err := client.Workspace(ctx)
	if err != nil {
		return err
	}
	if info.APIVersion < sharedworkspace.RuntimePolicyAPIVersion {
		if local.Revision != 0 {
			return errors.New("请先升级 iDeploy 共享服务以支持发布策略")
		}
		return nil
	}
	shared, err := client.RuntimePolicy(ctx, target.ID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(local, shared) {
		return errors.New("本机与共享服务的发布策略不一致，请同步工作区后重试")
	}
	return nil
}

func (s *SystemService) GetReleaseRuntimeVerification(id string) (deployment.ReleaseRuntimeVerification, error) {
	store, err := s.deploymentStore()
	if err != nil {
		return deployment.ReleaseRuntimeVerification{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	return store.ReleaseRuntimeVerification(ctx, strings.TrimSpace(id))
}

func inspectRuntimePolicy(ctx context.Context, remote *server.Remote, host server.Connection, policy deployment.RuntimePolicy, details []server.ContainerRuntimeDetails) []deployment.RuntimeCheck {
	checks := []deployment.RuntimeCheck{}
	seen := map[string]bool{}
	for _, detail := range details {
		seen[detail.Service] = true
		for _, rule := range policy.Mounts {
			if rule.Service != detail.Service {
				continue
			}
			result := deployment.RuntimeCheck{Code: "mount", Service: rule.Service, Path: rule.Destination, Status: "mismatch"}
			for _, mount := range detail.Mounts {
				if matchesRuntimeMount(rule, mount) {
					result.Status = "ok"
					break
				}
			}
			checks = append(checks, result)
		}
		if policy.Mode == deployment.RuntimeEnforce {
			for _, mount := range detail.Mounts {
				if mount.Type != "volume" && mount.Type != "bind" {
					continue
				}
				if !registeredMount(policy, detail.Service, mount) {
					checks = append(checks, deployment.RuntimeCheck{Code: "unexpected_mount", Status: "mismatch", Service: detail.Service, Path: mount.Destination})
				}
			}
		}
		for _, directory := range policy.Directories {
			if directory.Service != detail.Service {
				continue
			}
			result := deployment.RuntimeCheck{Code: "data_directory", Service: directory.Service, Path: directory.Path, Status: "unmounted"}
			var backing server.RuntimeMount
			for _, mount := range detail.Mounts {
				if len(mount.Destination) > len(backing.Destination) &&
					(directory.Path == mount.Destination || strings.HasPrefix(directory.Path, mount.Destination+"/")) {
					backing = mount
				}
			}
			if backing.Type == "bind" || backing.Type == "volume" {
				result.Status = "unknown"
				if directory.Access == "writable" && !backing.RW {
					result.Status = "inaccessible"
				} else if state, err := remote.CheckContainerDirectory(ctx, host, detail.ID, directory.Path, directory.Access); err == nil {
					result.Status = state
					if state == "directory" {
						result.Status = "ok"
					}
				}
			}
			checks = append(checks, result)
		}
	}
	for _, service := range policy.Services {
		if !seen[service] {
			checks = append(checks, deployment.RuntimeCheck{Code: "service", Status: "missing", Service: service})
		}
	}
	return checks
}

func matchesRuntimeMount(rule deployment.RuntimeMount, observed server.RuntimeMount) bool {
	source := observed.Source
	if observed.Type == "volume" {
		source = observed.Name
	}
	return observed.Type == rule.Type && source == rule.Source && observed.Destination == rule.Destination && observed.RW != rule.ReadOnly
}

func registeredMount(policy deployment.RuntimePolicy, service string, observed server.RuntimeMount) bool {
	for _, rule := range policy.Mounts {
		if rule.Service == service && matchesRuntimeMount(rule, observed) {
			return true
		}
	}
	return false
}

func mountSpec(rule deployment.RuntimeMount) server.ContainerMountSpec {
	return server.ContainerMountSpec{Type: rule.Type, Source: rule.Source, Destination: rule.Destination, ReadOnly: rule.ReadOnly}
}

func singleContainerMounts(ctx context.Context, remote *server.Remote, host server.Connection, target deployment.Target, policy deployment.RuntimePolicy, containers []server.Container) ([]server.ContainerMountSpec, error) {
	var mounts []server.ContainerMountSpec
	if policy.Mode == deployment.RuntimeEnforce {
		for _, rule := range policy.Mounts {
			mounts = append(mounts, mountSpec(rule))
		}
		return mounts, nil
	}
	details, err := inspectBoundContainers(ctx, remote, host, target, containers)
	if err != nil {
		return nil, err
	}
	for _, detail := range details {
		for _, mount := range detail.Mounts {
			source := mount.Source
			if mount.Type == "volume" {
				source = mount.Name
			}
			spec := server.ContainerMountSpec{Type: mount.Type, Source: source, Destination: mount.Destination, ReadOnly: !mount.RW}
			if _, err := spec.Argument(); err != nil {
				return nil, fmt.Errorf("无法安全保留现有挂载 %s: %w", mount.Destination, err)
			}
			mounts = append(mounts, spec)
		}
	}
	return mounts, nil
}

func inspectBoundContainers(ctx context.Context, remote *server.Remote, host server.Connection, target deployment.Target, containers []server.Container) ([]server.ContainerRuntimeDetails, error) {
	matched := deployment.Match(target, containers)
	ids := make([]string, 0, len(matched))
	for _, container := range matched {
		ids = append(ids, container.ID)
	}
	return remote.InspectContainerRuntime(ctx, host, ids)
}

type composeRuntimeSnapshot struct {
	Services map[string]struct {
		Image   string `json:"image"`
		Volumes []struct {
			Type     string `json:"type"`
			Source   string `json:"source"`
			Target   string `json:"target"`
			ReadOnly bool   `json:"read_only"`
			Volume   struct {
				Subpath string `json:"subpath"`
			} `json:"volume"`
		} `json:"volumes"`
	} `json:"services"`
	Volumes map[string]struct {
		Name     string `json:"name"`
		External bool   `json:"external"`
	} `json:"volumes"`
}

func releaseImageReferences(target deployment.Target, image, manifest string) (map[string]string, error) {
	if target.WorkloadKind != "compose" {
		if !pinnedImagePattern.MatchString(image) {
			return nil, errors.New("发布镜像缺少 Digest")
		}
		return map[string]string{"": image}, nil
	}
	var snapshot composeRuntimeSnapshot
	if err := json.Unmarshal([]byte(manifest), &snapshot); err != nil || len(snapshot.Services) == 0 {
		return nil, errors.New("发布制品缺少可校验的 Compose 服务快照")
	}
	references := make(map[string]string, len(snapshot.Services))
	for name, service := range snapshot.Services {
		if !pinnedImagePattern.MatchString(service.Image) {
			return nil, fmt.Errorf("Compose 服务 %s 缺少镜像 Digest", name)
		}
		references[name] = service.Image
	}
	return references, nil
}

func inspectReleaseImages(ctx context.Context, remote *server.Remote, host server.Connection, details []server.ContainerRuntimeDetails, references map[string]string) []deployment.RuntimeCheck {
	checks := make([]deployment.RuntimeCheck, 0, len(details))
	verified := map[string]string{}
	for _, detail := range details {
		check := deployment.RuntimeCheck{Code: "image_digest", Service: detail.Service, Status: "unknown"}
		reference := references[detail.Service]
		if reference != "" {
			id, read := verified[reference]
			if !read {
				id, _ = remote.InspectLocalImageID(ctx, host, reference)
				verified[reference] = id
			}
			if id == detail.ImageID && id != "" {
				check.Status = "ok"
			} else if id != "" {
				check.Status = "mismatch"
			}
		}
		checks = append(checks, check)
	}
	return checks
}

func validateComposeRuntimePlan(target deployment.Target, manifest string, policy deployment.RuntimePolicy) ([]deployment.RuntimeCheck, error) {
	var snapshot composeRuntimeSnapshot
	if err := json.Unmarshal([]byte(manifest), &snapshot); err != nil {
		return nil, errors.New("无法校验 Compose 快照的挂载配置")
	}
	for _, service := range policy.Services {
		if _, ok := snapshot.Services[service]; !ok {
			return []deployment.RuntimeCheck{{Code: "compose_service", Status: "missing", Service: service}}, nil
		}
	}
	checks := []deployment.RuntimeCheck{}
	names := make([]string, 0, len(snapshot.Services))
	for name := range snapshot.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var mounts []server.RuntimeMount
		for _, mount := range snapshot.Services[name].Volumes {
			if mount.Type != "bind" && mount.Type != "volume" {
				continue
			}
			source := strings.ReplaceAll(mount.Source, "$$", "$")
			destination := strings.ReplaceAll(mount.Target, "$$", "$")
			var volume string
			if mount.Type == "volume" && mount.Volume.Subpath == "" && source != "" {
				spec, exists := snapshot.Volumes[source]
				if exists {
					volume = strings.ReplaceAll(spec.Name, "$$", "$")
					if volume == "" {
						volume = target.WorkloadName + "_" + source
						if spec.External {
							volume = source
						}
					}
				}
			}
			mounts = append(mounts, server.RuntimeMount{Type: mount.Type, Source: source, Name: volume, Destination: destination, RW: !mount.ReadOnly})
		}
		for _, mount := range mounts {
			if !registeredMount(policy, name, mount) {
				checks = append(checks, deployment.RuntimeCheck{Code: "compose_unregistered_mount", Status: "mismatch", Service: name, Path: mount.Destination})
			}
		}
		for _, rule := range policy.Mounts {
			if rule.Service != name {
				continue
			}
			found := false
			for _, mount := range mounts {
				found = found || matchesRuntimeMount(rule, mount)
			}
			if !found {
				checks = append(checks, deployment.RuntimeCheck{Code: "compose_mount", Status: "mismatch", Service: name, Path: rule.Destination})
			}
		}
	}
	return checks, nil
}

func runtimeCheckFailure(checks []deployment.RuntimeCheck) error {
	for _, check := range checks {
		if check.Status != "ok" {
			location := strings.TrimSpace(check.Service + " " + check.Path)
			return fmt.Errorf("发布策略校验失败（%s，%s）%s", check.Code, check.Status, location)
		}
	}
	return nil
}

func publicationRuntimePolicy(ctx context.Context, store *deployment.Store, target deployment.Target, release deployment.Release) (deployment.RuntimePolicy, error) {
	policy, err := store.ReleaseRuntimePolicy(ctx, release.ID)
	if err != nil {
		return policy, err
	}
	current, err := store.RuntimePolicy(ctx, target.ID)
	if err != nil {
		return policy, err
	}
	if current.Revision != policy.Revision && (current.Mode == deployment.RuntimeEnforce || policy.Mode == deployment.RuntimeEnforce) {
		return policy, errors.New("发布策略已改变，此历史制品需按当前策略重新发布")
	}
	if err := policy.Validate(target.WorkloadKind); err != nil {
		return policy, err
	}
	return policy, nil
}

func checkPublicationBefore(ctx context.Context, store *deployment.Store, remote *server.Remote, host server.Connection, target deployment.Target, release deployment.Release, policy deployment.RuntimePolicy, containers []server.Container, manifest string) error {
	if policy.Revision == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	checks := checkRuntimeBaseline(ctx, remote, host, target, policy, containers)
	if target.WorkloadKind == "compose" {
		planned, err := validateComposeRuntimePlan(target, manifest, policy)
		if err != nil {
			checks = append(checks, deployment.RuntimeCheck{Code: "compose_plan", Status: "unknown"})
		} else {
			checks = append(checks, planned...)
		}
	}
	if err := recordPublicationChecks(store, release.ID, false, checks); err != nil {
		return err
	}
	if policy.Mode == deployment.RuntimeEnforce {
		return runtimeCheckFailure(checks)
	}
	return nil
}

func checkRuntimeBaseline(ctx context.Context, remote *server.Remote, host server.Connection, target deployment.Target, policy deployment.RuntimePolicy, containers []server.Container) []deployment.RuntimeCheck {
	checks := []deployment.RuntimeCheck{}
	details, err := inspectBoundContainers(ctx, remote, host, target, containers)
	if err != nil {
		checks = append(checks, deployment.RuntimeCheck{Code: "inspect_runtime", Status: "unknown"})
	} else if len(details) != 0 {
		checks = append(checks, inspectRuntimePolicy(ctx, remote, host, policy, details)...)
	}
	for _, rule := range policy.Mounts {
		check := deployment.RuntimeCheck{Code: "storage_source", Service: rule.Service, Path: rule.Destination, Status: "ok"}
		if err := remote.CheckMountSource(ctx, host, mountSpec(rule)); err != nil {
			check.Status = "unknown"
		}
		checks = append(checks, check)
	}
	return checks
}

func checkPublicationAfter(ctx context.Context, store *deployment.Store, remote *server.Remote, host server.Connection, target deployment.Target, release deployment.Release, containers []server.Container, image, manifest string) error {
	policy, err := publicationRuntimePolicy(ctx, store, target, release)
	if err != nil || policy.Revision == 0 {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	checks := []deployment.RuntimeCheck{}
	details, err := inspectBoundContainers(ctx, remote, host, target, containers)
	if err != nil || len(details) == 0 {
		checks = append(checks, deployment.RuntimeCheck{Code: "inspect_runtime", Status: "unknown"})
	} else {
		checks = append(checks, inspectRuntimePolicy(ctx, remote, host, policy, details)...)
		references, err := releaseImageReferences(target, image, manifest)
		if err != nil {
			checks = append(checks, deployment.RuntimeCheck{Code: "image_digest", Status: "unknown"})
		} else {
			checks = append(checks, inspectReleaseImages(ctx, remote, host, details, references)...)
		}
	}
	if err := recordPublicationChecks(store, release.ID, true, checks); err != nil {
		return err
	}
	if policy.Mode == deployment.RuntimeEnforce {
		return runtimeCheckFailure(checks)
	}
	return nil
}

func recordPublicationChecks(store *deployment.Store, releaseID string, after bool, checks []deployment.RuntimeCheck) error {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	return store.RecordRuntimeChecks(ctx, releaseID, after, checks)
}
