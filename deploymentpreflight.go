package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"ideploy/internal/platform/toolchain"
	"ideploy/internal/project"
	"ideploy/internal/registry"
	"ideploy/internal/server"
)

type DeploymentPreflightInput struct {
	DeploymentID string `json:"deploymentId"`
	RegistryID   string `json:"registryId"`
}

type DeploymentPreflightCheckView struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type DeploymentPreflightView struct {
	OK             bool                           `json:"ok"`
	DeploymentID   string                         `json:"deploymentId"`
	Checks         []DeploymentPreflightCheckView `json:"checks"`
	BaseImages     []string                       `json:"baseImages"`
	SuggestedPorts []int                          `json:"suggestedPorts"`
}

var publishedPortPattern = regexp.MustCompile(`([0-9]{1,5})->`)

func (s *SystemService) PreflightDeployment(input DeploymentPreflightInput) (DeploymentPreflightView, error) {
	input.DeploymentID = strings.TrimSpace(input.DeploymentID)
	input.RegistryID = strings.TrimSpace(input.RegistryID)
	if input.DeploymentID == "" || input.RegistryID == "" {
		return DeploymentPreflightView{}, errors.New("deployment_id and registry_id are required")
	}
	deployments, err := s.deploymentStore()
	if err != nil {
		return DeploymentPreflightView{}, err
	}
	projects, err := s.projectStore()
	if err != nil {
		return DeploymentPreflightView{}, err
	}
	servers, err := s.serverStore()
	if err != nil {
		return DeploymentPreflightView{}, err
	}
	registries, err := s.registryStore()
	if err != nil {
		return DeploymentPreflightView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	target, err := deployments.Get(ctx, input.DeploymentID)
	if err != nil {
		return DeploymentPreflightView{}, err
	}
	projectValue, err := projects.Get(ctx, target.ProjectID)
	if err != nil {
		return DeploymentPreflightView{}, err
	}
	host, err := servers.Get(ctx, target.ServerID)
	if err != nil {
		return DeploymentPreflightView{}, err
	}
	registryValue, err := registries.Get(ctx, input.RegistryID)
	if err != nil {
		return DeploymentPreflightView{}, err
	}

	view := DeploymentPreflightView{OK: true, DeploymentID: target.ID, Checks: []DeploymentPreflightCheckView{}, BaseImages: []string{}, SuggestedPorts: []int{}}
	add := func(id, status, detail string) {
		view.Checks = append(view.Checks, DeploymentPreflightCheckView{ID: id, Status: status, Detail: detail})
		if status == "failed" {
			view.OK = false
		}
	}

	root := projectValue.LocalPath
	cleanup := func() {}
	if projectValue.SourceKind == project.SourceGit {
		source, prepareErr := prepareReleaseSource(ctx, projectValue, s.cacheDirectory(), discardJobReporter{})
		if prepareErr != nil {
			add("source", "failed", prepareErr.Error())
		} else {
			root, cleanup = source.root, source.cleanup
			add("source", "ok", "Git 源码已检出并可用于检查")
		}
	} else if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
		add("source", "failed", "本地项目目录不可用")
	} else {
		add("source", "ok", "本地项目目录可访问")
	}
	defer cleanup()

	if root != "" {
		if target.WorkloadKind == "compose" {
			path, pathErr := secureProjectFile(root, projectValue.ComposePath)
			if pathErr != nil || projectValue.ComposePath == "" {
				add("compose", "failed", "Compose 文件未正确配置")
			} else if _, statErr := os.Stat(path); statErr != nil {
				add("compose", "failed", "Compose 文件不存在: "+projectValue.ComposePath)
			} else {
				add("compose", "ok", "Compose 文件可读取: "+projectValue.ComposePath)
			}
		} else {
			dockerfile, pathErr := secureProjectFile(root, projectValue.DockerfilePath)
			if pathErr != nil || projectValue.DockerfilePath == "" {
				add("dockerfile", "failed", "Dockerfile 未正确配置")
			} else if contents, readErr := os.ReadFile(dockerfile); readErr != nil {
				add("dockerfile", "failed", "Dockerfile 无法读取: "+readErr.Error())
			} else {
				view.BaseImages = filterInspectableBaseImages(dockerfileBaseImages(string(contents)))
				add("dockerfile", "ok", fmt.Sprintf("Dockerfile 可读取，发现 %d 个基础镜像", len(view.BaseImages)))
			}
		}
	}

	if registryValue.Status != registry.StatusHealthy {
		add("registry_push", "failed", "发布仓库尚未通过登录测试")
	} else if _, credentialErr := loadRegistryPushPassword(ctx, registryValue); credentialErr != nil {
		add("registry_push", "failed", credentialErr.Error())
	} else {
		add("registry_push", "ok", "发布仓库连接及推送凭据可用")
	}

	proxySettings := BuildProxySettingsInput{Mode: "disabled", Rules: []BuildProxyRule{}}
	db, proxyErr := s.systemDatabase()
	if proxyErr == nil {
		proxySettings, proxyErr = loadBuildProxySettings(ctx, db)
	}
	if proxyErr != nil {
		add("build_proxy", "failed", proxyErr.Error())
	} else if proxySettings.Mode == "disabled" {
		add("build_proxy", "warning", "构建代理未启用，基础镜像将使用 Docker 默认网络")
	} else {
		add("build_proxy", "ok", fmt.Sprintf("%s 模式，%d 条代理规则", proxySettings.Mode, len(proxySettings.Rules)))
	}

	if len(view.BaseImages) > 0 {
		docker, dockerErr := toolchain.ResolveExecutable(ctx, "docker")
		if dockerErr != nil {
			add("base_images", "failed", dockerErr.Error())
		} else {
			failures := make([]string, 0)
			for _, image := range view.BaseImages {
				reference := mirroredBaseImageReference(image, proxySettings.Rules)
				inspectContext, inspectCancel := context.WithTimeout(ctx, 20*time.Second)
				_, inspectErr := runLocalCommand(inspectContext, root, nil, docker.Path, "manifest", "inspect", reference)
				inspectCancel()
				if inspectErr != nil {
					failures = append(failures, image+" via "+reference+": "+compactCommandError(inspectErr))
				}
			}
			if len(failures) > 0 {
				add("base_images", "failed", strings.Join(failures, "; "))
			} else {
				add("base_images", "ok", "所有基础镜像均可读取 manifest")
			}
		}
	}

	remote := server.NewRemote()
	probe := remote.Probe(ctx, host)
	if probe.Status != server.StatusHealthy {
		add("server_connection", "failed", probe.Detail)
		return view, nil
	}
	add("server_connection", "ok", probe.Detail)
	platform, platformErr := remote.DockerPlatform(ctx, host)
	if platformErr != nil {
		add("platform", "failed", platformErr.Error())
	} else {
		add("platform", "ok", "目标 Docker 平台: "+platform)
	}
	containers, containersErr := remote.ListContainers(ctx, host)
	if containersErr != nil {
		add("remote_runtime", "failed", containersErr.Error())
		return view, nil
	}
	add("remote_runtime", "ok", fmt.Sprintf("已读取 %d 个远程容器", len(containers)))

	if target.WorkloadKind == "container" {
		ports := target.ContainerPorts
		if root != "" {
			if resolved, resolveErr := project.ResolveContainerPorts(root, projectValue.ContextPath, projectValue.DockerfilePath, ports); resolveErr == nil {
				ports = resolved
			}
		}
		requested := requestedHostPorts(ports)
		used := map[int]string{}
		for _, container := range containers {
			if container.Name == target.WorkloadName {
				continue
			}
			for _, port := range publishedHostPorts(container.Ports) {
				used[port] = container.Name
			}
		}
		conflicts := make([]string, 0)
		for _, port := range requested {
			if owner := used[port]; owner != "" {
				conflicts = append(conflicts, fmt.Sprintf("%d 被 %s 占用", port, owner))
			}
		}
		if len(conflicts) > 0 {
			add("remote_port", "failed", strings.Join(conflicts, "；"))
			view.SuggestedPorts = suggestAvailablePorts(requested, used)
		} else {
			add("remote_port", "ok", "配置的主机端口当前可用")
		}
		add("workload_name", "ok", workloadNameDetail(target.WorkloadName, containers))
	} else {
		add("remote_port", "warning", "Compose 端口由 Compose 文件管理，当前未进行逐服务端口解析")
	}
	return view, nil
}

type discardJobReporter struct{}

func (discardJobReporter) Progress(int, string) {}
func (discardJobReporter) Log(string)           {}

func filterInspectableBaseImages(images []string) []string {
	result := make([]string, 0, len(images))
	for _, image := range images {
		if image == "" || image == "scratch" || strings.Contains(image, "$") {
			continue
		}
		result = append(result, image)
		if len(result) == 8 {
			break
		}
	}
	return result
}

func mirroredBaseImageReference(image string, rules []BuildProxyRule) string {
	registryHost, path := "docker.io", image
	first, remainder, hasSlash := strings.Cut(image, "/")
	if hasSlash && (strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost") {
		registryHost, path = first, remainder
	} else if !hasSlash {
		path = "library/" + image
	}
	for _, rule := range rules {
		if rule.Upstream == registryHost {
			return strings.TrimSuffix(rule.Mirror, "/") + "/" + path
		}
	}
	return image
}

func requestedHostPorts(mappings []string) []int {
	set := map[int]bool{}
	for _, mapping := range mappings {
		value := strings.SplitN(mapping, "/", 2)[0]
		parts := strings.Split(value, ":")
		index := 0
		if len(parts) == 2 {
			index = 0
		} else if len(parts) >= 3 {
			index = len(parts) - 2
		}
		port, err := strconv.Atoi(parts[index])
		if err == nil && port > 0 && port <= 65535 {
			set[port] = true
		}
	}
	result := make([]int, 0, len(set))
	for port := range set {
		result = append(result, port)
	}
	sort.Ints(result)
	return result
}

func publishedHostPorts(value string) []int {
	set := map[int]bool{}
	for _, match := range publishedPortPattern.FindAllStringSubmatch(value, -1) {
		port, err := strconv.Atoi(match[1])
		if err == nil {
			set[port] = true
		}
	}
	result := make([]int, 0, len(set))
	for port := range set {
		result = append(result, port)
	}
	sort.Ints(result)
	return result
}

func suggestAvailablePorts(requested []int, used map[int]string) []int {
	start := 8080
	if len(requested) > 0 {
		start = requested[0] + 1
	}
	result := make([]int, 0, 3)
	for port := start; port <= 65535 && len(result) < 3; port++ {
		if used[port] == "" {
			result = append(result, port)
		}
	}
	return result
}

func workloadNameDetail(name string, containers []server.Container) string {
	for _, container := range containers {
		if container.Name == name {
			return fmt.Sprintf("将替换现有容器 %s（%s）", name, container.State)
		}
	}
	return "容器名称当前未被占用"
}

func compactCommandError(err error) string {
	detail := strings.Join(strings.Fields(err.Error()), " ")
	if len(detail) > 280 {
		return detail[:280] + "…"
	}
	return detail
}
