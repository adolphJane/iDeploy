package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ideploy/internal/deployment"
	"ideploy/internal/job"
	"ideploy/internal/platform/toolchain"
	"ideploy/internal/project"
	"ideploy/internal/registry"
	"ideploy/internal/server"
)

var releaseVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type preparedSource struct {
	root     string
	revision string
	cleanup  func()
}

func prepareReleaseSource(ctx context.Context, value project.Project, cacheDir string, reporter job.Reporter) (preparedSource, error) {
	if value.SourceKind == project.SourceLocal {
		info, err := os.Stat(value.LocalPath)
		if err != nil || !info.IsDir() {
			return preparedSource{}, errors.New("本地项目目录不可用，请返回项目页面检查来源")
		}
		return preparedSource{root: value.LocalPath, revision: localSourceRevision(ctx, value.LocalPath), cleanup: func() {}}, nil
	}

	git, err := toolchain.ResolveExecutable(ctx, "git")
	if err != nil {
		return preparedSource{}, err
	}
	buildCache := filepath.Join(cacheDir, "builds")
	if err := os.MkdirAll(buildCache, 0o700); err != nil {
		return preparedSource{}, fmt.Errorf("创建构建缓存: %w", err)
	}
	directory, err := os.MkdirTemp(buildCache, "source-")
	if err != nil {
		return preparedSource{}, fmt.Errorf("创建源码工作区: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	reporter.Log("正在拉取 Git 项目 " + value.RepositoryURL)
	if _, err := runLocalCommand(ctx, "", nil, git.Path, "clone", "--no-checkout", value.RepositoryURL, directory); err != nil {
		cleanup()
		return preparedSource{}, err
	}
	if _, err := runLocalCommand(ctx, "", nil, git.Path, "-C", directory, "checkout", "--detach", value.RefValue); err != nil {
		cleanup()
		return preparedSource{}, fmt.Errorf("检出 Git 引用 %s: %w", value.RefValue, err)
	}
	revision, err := runLocalCommand(ctx, "", nil, git.Path, "-C", directory, "rev-parse", "HEAD")
	if err != nil {
		cleanup()
		return preparedSource{}, err
	}
	return preparedSource{root: directory, revision: strings.TrimSpace(revision), cleanup: cleanup}, nil
}

func localSourceRevision(ctx context.Context, root string) string {
	git, err := toolchain.ResolveExecutable(ctx, "git")
	if err == nil {
		if output, commandErr := runLocalCommand(ctx, "", nil, git.Path, "-C", root, "rev-parse", "HEAD"); commandErr == nil {
			return strings.TrimSpace(output)
		}
	}
	return "local-" + time.Now().UTC().Format("20060102T150405Z")
}

func buildAndPushRelease(ctx context.Context, source preparedSource, value project.Project, target deployment.Target, registryValue registry.Connection, registryPassword, version, platform string, buildProxy buildProxyRuntime, reporter job.Reporter, stage func(deployment.ReleaseStatus, string) error) (imageRef, manifest string, err error) {
	docker, err := toolchain.ResolveExecutable(ctx, "docker")
	if err != nil {
		return "", "", err
	}
	if err := validateRegistryPushCredentials(registryValue.Username, registryPassword); err != nil {
		return "", "", err
	}
	if _, err := runLocalCommandInput(ctx, "", nil, registryPassword+"\n", docker.Path, "login", registry.DockerHost(registryValue.Endpoint), "--username", registryValue.Username, "--password-stdin"); err != nil {
		return "", "", fmt.Errorf("登录本机 Docker 镜像仓库失败，请核对用户名、密码或 Token: %w", err)
	}
	environment := append(os.Environ(), "IMAGE_TAG="+version, "DOCKER_DEFAULT_PLATFORM="+platform)
	if target.WorkloadKind == "compose" {
		if value.ComposePath == "" {
			return "", "", errors.New("Compose 发布需要在项目中配置 Compose 文件")
		}
		composeFile, err := secureProjectFile(source.root, value.ComposePath)
		if err != nil {
			return "", "", err
		}
		contents, err := os.ReadFile(composeFile)
		if err != nil {
			return "", "", fmt.Errorf("读取 Compose 文件: %w", err)
		}
		if len(contents) > 2*1024*1024 {
			return "", "", errors.New("Compose 文件超过 2 MB，已拒绝上传")
		}
		if !strings.Contains(string(contents), "${IMAGE_TAG}") && !strings.Contains(string(contents), "$IMAGE_TAG") {
			return "", "", errors.New("Compose 发布需要在 image 字段中使用 ${IMAGE_TAG}，以便生成可回滚版本")
		}
		config, err := runLocalJSONCommand(ctx, source.root, environment, docker.Path, "compose", "-f", composeFile, "config", "--format", "json")
		if err != nil {
			return "", "", err
		}
		images, err := composeBuildImagesForPlatform(config, platform)
		if err != nil {
			return "", "", err
		}
		reporter.Progress(28, "正在构建 Compose 服务镜像 · "+platform)
		buildArguments := composeBuildArguments(composeFile, buildProxy)
		if _, err := runLocalCommand(ctx, source.root, environment, docker.Path, buildArguments...); err != nil {
			return "", "", err
		}
		for _, image := range images {
			if err := verifyLocalImagePlatform(ctx, docker.Path, image, platform); err != nil {
				return "", "", err
			}
		}
		reporter.Progress(48, "正在推送 Compose 服务镜像")
		if err := stage(deployment.ReleasePushing, "正在推送 Compose 服务镜像"); err != nil {
			return "", "", err
		}
		if _, err := runLocalCommand(ctx, source.root, environment, docker.Path, "compose", "-f", composeFile, "push"); err != nil {
			return "", "", err
		}
		resolved, err := runLocalJSONCommand(ctx, source.root, environment, docker.Path, "compose", "-f", composeFile, "config", "--resolve-image-digests", "--format", "json")
		if err != nil {
			return "", "", fmt.Errorf("固定 Compose 镜像 Digest: %w", err)
		}
		pinned, err := pinnedComposeManifest(resolved)
		if err != nil {
			return "", "", err
		}
		return "compose:" + target.WorkloadName + ":" + version, pinned, nil
	}

	if value.DockerfilePath == "" || value.ImageRepository == "" {
		missing := make([]string, 0, 2)
		if value.DockerfilePath == "" {
			missing = append(missing, "Dockerfile")
		}
		if value.ImageRepository == "" {
			missing = append(missing, "目标镜像")
		}
		return "", "", fmt.Errorf("单容器发布需要在项目中配置：%s", strings.Join(missing, "、"))
	}
	contextDir, err := secureProjectFile(source.root, value.ContextPath)
	if err != nil {
		return "", "", err
	}
	dockerfile, err := secureProjectFile(source.root, value.DockerfilePath)
	if err != nil {
		return "", "", err
	}
	if strings.Contains(value.ImageRepository, "@") || strings.LastIndex(value.ImageRepository, ":") > strings.LastIndex(value.ImageRepository, "/") {
		return "", "", errors.New("项目的目标镜像只能填写仓库路径，请不要包含 Tag 或 Digest")
	}
	imageRef = value.ImageRepository + ":" + version
	if !validLocalImageReference(imageRef) {
		return "", "", errors.New("目标镜像名称无效")
	}
	if err := validateImageRepositoryForRegistry(value.ImageRepository, registryValue); err != nil {
		return "", "", err
	}
	reporter.Progress(28, "正在构建镜像 "+imageRef+" · "+platform)
	buildArguments := containerBuildArguments(dockerfile, imageRef, contextDir, platform, buildProxy)
	if _, err := runLocalCommand(ctx, source.root, nil, docker.Path, buildArguments...); err != nil {
		return "", "", explainDockerBuildError(err, dockerfile)
	}
	if err := verifyLocalImagePlatform(ctx, docker.Path, imageRef, platform); err != nil {
		return "", "", err
	}
	reporter.Progress(48, "正在推送镜像 "+imageRef)
	if err := stage(deployment.ReleasePushing, "正在推送镜像 "+imageRef); err != nil {
		return "", "", err
	}
	pushOutput, err := runLocalCommand(ctx, source.root, nil, docker.Path, "push", imageRef)
	if err != nil {
		return "", "", err
	}
	pinned, err := pinnedPushReference(value.ImageRepository, pushOutput)
	return pinned, "", err
}

func composeBuildArguments(composeFile string, buildProxy buildProxyRuntime) []string {
	arguments := []string{"compose", "-f", composeFile, "build", "--pull"}
	if buildProxy.BuilderName != "" {
		arguments = append(arguments, "--builder", buildProxy.BuilderName)
	}
	return arguments
}

func containerBuildArguments(dockerfile, imageRef, contextDir, platform string, buildProxy buildProxyRuntime) []string {
	if buildProxy.BuilderName != "" {
		return []string{"buildx", "build", "--builder", buildProxy.BuilderName, "--load", "--platform", platform, "--pull", "-f", dockerfile, "-t", imageRef, contextDir}
	}
	return []string{"build", "--platform", platform, "--pull", "-f", dockerfile, "-t", imageRef, contextDir}
}

func runtimeTaggedImageReference(pinnedImage, version string) (string, error) {
	if !pinnedImagePattern.MatchString(pinnedImage) {
		return "", errors.New("发布制品未锁定镜像 Digest，无法创建运行版本标签")
	}
	repository, _, ok := strings.Cut(pinnedImage, "@")
	if !ok || repository == "" || !releaseVersionPattern.MatchString(version) {
		return "", errors.New("无法从发布制品生成运行版本标签")
	}
	tagged := repository + ":" + version
	if !validLocalImageReference(tagged) {
		return "", errors.New("生成的运行版本标签无效")
	}
	return tagged, nil
}

func explainDockerBuildError(buildErr error, dockerfilePath string) error {
	detail := buildErr.Error()
	lower := strings.ToLower(detail)
	if !strings.Contains(lower, "failed to resolve source metadata") && !strings.Contains(lower, "load metadata for") {
		return buildErr
	}
	contents, readErr := os.ReadFile(dockerfilePath)
	if readErr != nil {
		return fmt.Errorf("无法获取 Dockerfile 基础镜像：%s", detail)
	}
	images := dockerfileBaseImages(string(contents))
	image := "Dockerfile 中的基础镜像"
	for _, candidate := range images {
		if strings.Contains(detail, candidate) || image == "Dockerfile 中的基础镜像" {
			image = candidate
		}
		if strings.Contains(detail, candidate) {
			break
		}
	}
	if (strings.Contains(strings.ToLower(image), "/proxy/") || strings.Contains(strings.ToLower(image), "/mirror/")) && strings.Contains(lower, "not found") {
		return fmt.Errorf("基础镜像 %s 在私有代理仓库中不存在或无法从上游获取。请确认 Harbor Proxy Cache 项目已绑定正确的上游并通过连接测试，或先将该基础镜像导入普通 Harbor 项目后修改 Dockerfile FROM。原始构建输出：%s", image, detail)
	}
	return fmt.Errorf("无法获取基础镜像 %s。请检查镜像名称、Docker Hub 网络或私有代理仓库配置。原始构建输出：%s", image, detail)
}

func secureProjectFile(root, relative string) (string, error) {
	root = filepath.Clean(root)
	value := filepath.Clean(filepath.Join(root, filepath.FromSlash(relative)))
	rel, err := filepath.Rel(root, value)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("构建路径超出项目目录")
	}
	return value, nil
}

func runLocalCommand(ctx context.Context, directory string, environment []string, executable string, args ...string) (string, error) {
	return runLocalCommandInput(ctx, directory, environment, "", executable, args...)
}

func runLocalCommandInput(ctx context.Context, directory string, environment []string, input, executable string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = directory
	if environment != nil {
		command.Env = environment
	}
	if input != "" {
		command.Stdin = strings.NewReader(input)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if len(detail) > 3000 {
			detail = detail[len(detail)-3000:]
		}
		if detail == "" {
			detail = err.Error()
		}
		return string(output), errors.New(detail)
	}
	return string(output), nil
}

func validLocalImageReference(value string) bool {
	if value == "" || len(value) > 512 || strings.ContainsAny(value, " \t\r\n'") || strings.Contains(value, "..") {
		return false
	}
	return true
}

func imageRepositoryMatchesRegistry(repository, endpoint string) bool {
	registryHost := strings.ToLower(registry.DockerHost(endpoint))
	first, _, hasPath := strings.Cut(strings.ToLower(repository), "/")
	hasExplicitHost := hasPath && (strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost")
	if hasExplicitHost {
		return first == registryHost
	}
	return registryHost == "docker.io" || registryHost == "registry-1.docker.io" || registryHost == "index.docker.io"
}

func validateImageRepositoryForRegistry(repository string, connection registry.Connection) error {
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	if repository == "" {
		return errors.New("目标镜像地址不能为空")
	}
	if strings.Contains(repository, "@") || strings.LastIndex(repository, ":") > strings.LastIndex(repository, "/") {
		return errors.New("项目的目标镜像只能填写仓库路径，请不要包含 Tag 或 Digest")
	}
	if !imageRepositoryMatchesRegistry(repository, connection.Endpoint) {
		return fmt.Errorf("目标镜像 %s 不属于所选仓库 %s", repository, registry.DockerHost(connection.Endpoint))
	}
	if connection.Kind != registry.KindHarbor {
		return nil
	}
	host := strings.ToLower(registry.DockerHost(connection.Endpoint))
	parts := strings.Split(repository, "/")
	if len(parts) < 3 || strings.ToLower(parts[0]) != host || strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		name := "app"
		if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) != "" {
			name = parts[len(parts)-1]
		}
		return fmt.Errorf("Harbor 目标镜像必须包含项目空间，例如 %s/library/%s；当前地址 %s 缺少项目层级", registry.DockerHost(connection.Endpoint), name, repository)
	}
	return nil
}

func validateRegistryPushCredentials(username, password string) error {
	if strings.TrimSpace(username) == "" || password == "" {
		return errors.New("镜像仓库未配置推送凭据，请填写用户名和密码或 Token，并通过 Docker 登录测试")
	}
	return nil
}

func loadRegistryPushPassword(ctx context.Context, connection registry.Connection) (string, error) {
	if strings.TrimSpace(connection.Username) == "" || strings.TrimSpace(connection.CredentialRef) == "" {
		return "", errors.New("镜像仓库未配置推送凭据，请在镜像仓库页面填写用户名和密码或 Token")
	}
	password, err := registry.LoadPassword(ctx, connection.CredentialRef)
	if err != nil {
		return "", errors.New("无法从系统钥匙串读取镜像仓库凭据，请重新保存用户名和密码或 Token")
	}
	if err := validateRegistryPushCredentials(connection.Username, password); err != nil {
		return "", err
	}
	return password, nil
}

func (s *SystemService) deploymentReleaseHandler(ctx context.Context, task job.Job, reporter job.Reporter) (handlerErr error) {
	var payload struct {
		ReleaseID string `json:"releaseId"`
	}
	if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil || payload.ReleaseID == "" {
		return errors.New("invalid deployment release payload")
	}
	deployments, err := s.deploymentStore()
	if err != nil {
		return err
	}
	defer func() {
		finishContext, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if handlerErr != nil {
			status := deployment.ReleaseFailed
			if errors.Is(handlerErr, context.Canceled) {
				status = deployment.ReleaseCanceled
			}
			_ = deployments.UpdateRelease(finishContext, payload.ReleaseID, status, handlerErr.Error(), "", "", "")
		}
	}()

	release, err := deployments.GetRelease(ctx, payload.ReleaseID)
	if err != nil {
		return err
	}
	target, err := deployments.Get(ctx, release.DeploymentID)
	if err != nil {
		return err
	}
	projects, err := s.projectStore()
	if err != nil {
		return err
	}
	projectValue, err := projects.Get(ctx, target.ProjectID)
	if err != nil {
		return err
	}
	servers, err := s.serverStore()
	if err != nil {
		return err
	}
	host, err := servers.Get(ctx, target.ServerID)
	if err != nil {
		return err
	}
	host, err = s.verifyDeploymentServer(ctx, host)
	if err != nil {
		return err
	}
	platformOutput, err := server.NewRemote().DockerPlatform(ctx, host)
	if err != nil {
		return fmt.Errorf("读取目标服务器 Docker 架构: %w", err)
	}
	platform, err := releasePlatform(platformOutput)
	if err != nil {
		return err
	}
	reporter.Log("目标服务器镜像平台：" + platform)
	registries, err := s.registryStore()
	if err != nil {
		return err
	}
	registryValue, err := registries.Get(ctx, release.RegistryID)
	if err != nil {
		return err
	}
	if registryValue.Status != registry.StatusHealthy {
		return errors.New("镜像仓库尚未通过连接检测")
	}
	registryPassword, err := loadRegistryPushPassword(ctx, registryValue)
	if err != nil {
		return err
	}
	docker, err := toolchain.ResolveExecutable(ctx, "docker")
	if err != nil {
		return err
	}
	buildProxy, err := s.prepareBuildProxy(ctx, docker.Path, registryValue, reporter)
	if err != nil {
		return err
	}

	if err := deployments.UpdateRelease(ctx, release.ID, deployment.ReleasePreparing, "正在准备源码", "", "", ""); err != nil {
		return err
	}
	reporter.Progress(8, "正在准备项目源码")
	source, err := prepareReleaseSource(ctx, projectValue, s.cacheDirectory(), reporter)
	if err != nil {
		return err
	}
	defer source.cleanup()
	if target.WorkloadKind == "container" {
		resolvedPorts, err := project.ResolveContainerPorts(source.root, projectValue.ContextPath, projectValue.DockerfilePath, target.ContainerPorts)
		if err != nil {
			return err
		}
		if !sameStrings(resolvedPorts, target.ContainerPorts) {
			if err := deployments.UpdateContainerPorts(ctx, target.ID, resolvedPorts); err != nil {
				return err
			}
			target.ContainerPorts = resolvedPorts
		}
	}
	if err := deployments.UpdateRelease(ctx, release.ID, deployment.ReleaseBuilding, "正在构建镜像", "", source.revision, ""); err != nil {
		return err
	}
	imageRef, manifest, err := buildAndPushRelease(ctx, source, projectValue, target, registryValue, registryPassword, release.Version, platform, buildProxy, reporter, func(status deployment.ReleaseStatus, detail string) error {
		return deployments.UpdateRelease(ctx, release.ID, status, detail, "", source.revision, "")
	})
	if err != nil {
		return err
	}
	if err := deployments.UpdateRelease(ctx, release.ID, deployment.ReleasePulling, "服务器正在拉取镜像", imageRef, source.revision, manifest); err != nil {
		return err
	}
	reporter.Progress(62, "目标服务器正在拉取发布制品")
	remote := server.NewRemote()
	if registryPassword != "" {
		if err := remote.LoginRegistry(ctx, host, registry.DockerHost(registryValue.Endpoint), registryValue.Username, registryPassword); err != nil {
			return fmt.Errorf("登录服务器 Docker 镜像仓库: %w", err)
		}
	}
	if target.WorkloadKind == "compose" {
		if err := remote.UploadCompose(ctx, host, target.ID, manifest); err != nil {
			return err
		}
		if err := remote.PullCompose(ctx, host, target.ID, target.WorkloadName, release.Version); err != nil {
			return err
		}
		if err := deployments.UpdateRelease(ctx, release.ID, deployment.ReleaseDeploying, "正在更新 Compose 项目", imageRef, source.revision, manifest); err != nil {
			return err
		}
		if err := remote.UpCompose(ctx, host, target.ID, target.WorkloadName, release.Version); err != nil {
			return err
		}
	} else {
		if err := remote.PullImage(ctx, host, imageRef); err != nil {
			return err
		}
		runtimeImageRef, err := runtimeTaggedImageReference(imageRef, release.Version)
		if err != nil {
			return err
		}
		if err := remote.TagImage(ctx, host, imageRef, runtimeImageRef); err != nil {
			return fmt.Errorf("创建远端运行版本标签: %w", err)
		}
		reporter.Log("远端运行镜像标签：" + runtimeImageRef + "（制品仍按 Digest 锁定）")
		if err := deployments.UpdateRelease(ctx, release.ID, deployment.ReleaseDeploying, "正在替换远端容器", imageRef, source.revision, manifest); err != nil {
			return err
		}
		reporter.Progress(74, "正在创建或更新远端容器")
		return s.deployAndCheckContainer(ctx, deployments, release, target, host, reporter, remote, imageRef, runtimeImageRef, source.revision, manifest)
	}
	return s.finishReleaseHealthCheck(ctx, deployments, release, target, host, reporter, imageRef, source.revision, manifest, nil)
}

func (s *SystemService) deploymentRollbackHandler(ctx context.Context, task job.Job, reporter job.Reporter) error {
	return s.deploymentArtifactHandler(ctx, task, reporter, true)
}

func (s *SystemService) deploymentResumeHandler(ctx context.Context, task job.Job, reporter job.Reporter) error {
	return s.deploymentArtifactHandler(ctx, task, reporter, false)
}

func (s *SystemService) deploymentArtifactHandler(ctx context.Context, task job.Job, reporter job.Reporter, rollback bool) (handlerErr error) {
	var payload struct {
		ReleaseID string `json:"releaseId"`
		SourceID  string `json:"sourceId"`
	}
	if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil || payload.ReleaseID == "" || payload.SourceID == "" {
		return errors.New("invalid deployment artifact payload")
	}
	deployments, err := s.deploymentStore()
	if err != nil {
		return err
	}
	defer func() {
		finishContext, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if handlerErr != nil {
			status := deployment.ReleaseFailed
			if errors.Is(handlerErr, context.Canceled) {
				status = deployment.ReleaseCanceled
			}
			_ = deployments.UpdateRelease(finishContext, payload.ReleaseID, status, handlerErr.Error(), "", "", "")
		}
	}()
	release, err := deployments.GetRelease(ctx, payload.ReleaseID)
	if err != nil {
		return err
	}
	source, err := deployments.GetRelease(ctx, payload.SourceID)
	if err != nil {
		return err
	}
	if source.DeploymentID != release.DeploymentID {
		return errors.New("制品来源不属于当前部署目标")
	}
	if rollback && source.Status != deployment.ReleaseSucceeded {
		return errors.New("只能回滚到当前部署目标的成功版本")
	}
	if !rollback && (source.Action != "deploy" || (source.Status != deployment.ReleaseFailed && source.Status != deployment.ReleaseCanceled)) {
		return errors.New("只能恢复当前部署目标失败或取消的普通发布")
	}
	if err := validateRollbackArtifact(source.ImageRef, source.Manifest); err != nil {
		return err
	}
	target, err := deployments.Get(ctx, release.DeploymentID)
	if err != nil {
		return err
	}
	servers, err := s.serverStore()
	if err != nil {
		return err
	}
	host, err := servers.Get(ctx, target.ServerID)
	if err != nil {
		return err
	}
	if host.Status != server.StatusHealthy {
		return errors.New("目标服务器当前不可用，无法部署已有制品")
	}
	projects, err := s.projectStore()
	if err != nil {
		return err
	}
	projectValue, err := projects.Get(ctx, target.ProjectID)
	if err != nil {
		return err
	}
	if target.WorkloadKind == "container" && projectValue.SourceKind == project.SourceLocal {
		resolvedPorts, err := project.ResolveContainerPorts(projectValue.LocalPath, projectValue.ContextPath, projectValue.DockerfilePath, target.ContainerPorts)
		if err != nil {
			return err
		}
		if !sameStrings(resolvedPorts, target.ContainerPorts) {
			if err := deployments.UpdateContainerPorts(ctx, target.ID, resolvedPorts); err != nil {
				return err
			}
			target.ContainerPorts = resolvedPorts
		}
	}
	remote := server.NewRemote()
	registries, err := s.registryStore()
	if err != nil {
		return err
	}
	registryValue, err := registries.Get(ctx, source.RegistryID)
	if err != nil {
		return err
	}
	if registryValue.CredentialRef != "" {
		password, err := registry.LoadPassword(ctx, registryValue.CredentialRef)
		if err != nil {
			return err
		}
		if err := remote.LoginRegistry(ctx, host, registry.DockerHost(registryValue.Endpoint), registryValue.Username, password); err != nil {
			return fmt.Errorf("登录服务器 Docker 镜像仓库: %w", err)
		}
	}
	detail := "正在拉取失败发布的已有制品"
	progress := "正在拉取已推送发布制品"
	if rollback {
		detail = "正在拉取历史版本"
		progress = "正在拉取历史发布制品"
	}
	if err := deployments.UpdateRelease(ctx, release.ID, deployment.ReleasePulling, detail, source.ImageRef, source.SourceRevision, source.Manifest); err != nil {
		return err
	}
	reporter.Progress(30, progress)
	if target.WorkloadKind == "compose" {
		if source.Manifest == "" {
			return errors.New("已有制品缺少 Compose 快照，无法部署")
		}
		if err := remote.UploadCompose(ctx, host, target.ID, source.Manifest); err != nil {
			return err
		}
		if err := remote.PullCompose(ctx, host, target.ID, target.WorkloadName, source.Version); err != nil {
			return err
		}
		deployDetail := "正在使用已有制品更新 Compose 项目"
		if rollback {
			deployDetail = "正在恢复历史 Compose 项目"
		}
		if err := deployments.UpdateRelease(ctx, release.ID, deployment.ReleaseDeploying, deployDetail, source.ImageRef, source.SourceRevision, source.Manifest); err != nil {
			return err
		}
		if err := remote.UpCompose(ctx, host, target.ID, target.WorkloadName, source.Version); err != nil {
			return err
		}
	} else {
		if err := remote.PullImage(ctx, host, source.ImageRef); err != nil {
			return err
		}
		runtimeImageRef, err := runtimeTaggedImageReference(source.ImageRef, source.Version)
		if err != nil {
			return err
		}
		if err := remote.TagImage(ctx, host, source.ImageRef, runtimeImageRef); err != nil {
			return fmt.Errorf("创建远端运行版本标签: %w", err)
		}
		reporter.Log("远端运行镜像标签：" + runtimeImageRef + "（制品仍按 Digest 锁定）")
		deployDetail := "正在使用已有制品替换远端容器"
		if rollback {
			deployDetail = "正在恢复历史容器"
		}
		if err := deployments.UpdateRelease(ctx, release.ID, deployment.ReleaseDeploying, deployDetail, source.ImageRef, source.SourceRevision, source.Manifest); err != nil {
			return err
		}
		return s.deployAndCheckContainer(ctx, deployments, release, target, host, reporter, remote, source.ImageRef, runtimeImageRef, source.SourceRevision, source.Manifest)
	}
	return s.finishReleaseHealthCheck(ctx, deployments, release, target, host, reporter, source.ImageRef, source.SourceRevision, source.Manifest, nil)
}

func (s *SystemService) deployAndCheckContainer(ctx context.Context, deployments *deployment.Store, release deployment.Release, target deployment.Target, host server.Connection, reporter job.Reporter, remote *server.Remote, imageRef, runtimeImageRef, revision, manifest string) error {
	currentTarget, environment, err := s.resolveDeploymentEnvironmentRevision(ctx, deployments, target.ID)
	if err != nil {
		return err
	}
	target = currentTarget
	if err := deployments.SetReleaseEnvironmentRevision(ctx, release.ID, target.EnvironmentRevision); err != nil {
		return err
	}
	if len(environment) > 0 {
		reporter.Log(fmt.Sprintf("正在注入 %d 个运行环境变量", len(environment)))
	}
	replacement, err := remote.DeployContainer(ctx, host, target.WorkloadName, runtimeImageRef, target.RestartPolicy, target.ContainerPorts, map[string]string{
		"com.ideploy.pinned-image":         imageRef,
		"com.ideploy.release-id":           release.ID,
		"com.ideploy.version":              release.Version,
		"com.ideploy.environment-revision": fmt.Sprint(target.EnvironmentRevision),
	}, environment)
	if err != nil {
		return err
	}
	committed := false
	err = s.finishReleaseHealthCheck(ctx, deployments, release, target, host, reporter, imageRef, revision, manifest, func() error {
		commitErr := remote.CommitContainerReplacement(ctx, host, replacement)
		if commitErr == nil {
			committed = true
		}
		return commitErr
	})
	if err == nil {
		if applyErr := deployments.MarkEnvironmentApplied(ctx, target.ID, target.EnvironmentRevision); applyErr != nil {
			return fmt.Errorf("容器已更新，但记录环境配置应用状态失败: %w", applyErr)
		}
		return nil
	}
	if committed {
		return fmt.Errorf("%w；新容器已通过健康检查并完成替换，但发布记录收尾失败", err)
	}
	restoreContext, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	if restoreErr := remote.RollbackContainerReplacement(restoreContext, host, replacement); restoreErr != nil {
		return errors.Join(err, fmt.Errorf("恢复上一容器失败: %w", restoreErr))
	}
	return fmt.Errorf("%w；已恢复上一容器", err)
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (s *SystemService) finishReleaseHealthCheck(ctx context.Context, deployments *deployment.Store, release deployment.Release, target deployment.Target, host server.Connection, reporter job.Reporter, imageRef, revision, manifest string, onHealthy func() error) error {
	if err := deployments.UpdateRelease(ctx, release.ID, deployment.ReleaseChecking, "正在执行健康检查", imageRef, revision, manifest); err != nil {
		return err
	}
	reporter.Progress(86, "正在检查远端服务健康状态")
	remote := server.NewRemote()
	deadline := time.Now().Add(90 * time.Second)
	var lastDetail string
	for {
		containers, err := remote.ListContainers(ctx, host)
		if err != nil {
			return fmt.Errorf("健康检查无法读取容器: %w", err)
		}
		matched := deployment.Match(target, containers)
		if len(matched) > 0 {
			healthy := true
			for _, container := range matched {
				if container.State != "running" || container.Health == "unhealthy" {
					healthy = false
					lastDetail = fmt.Sprintf("%s: %s / %s", container.Name, container.State, container.Health)
					break
				}
				if container.Health == "starting" {
					healthy = false
					lastDetail = container.Name + ": 健康检查启动中"
					break
				}
			}
			if healthy {
				if onHealthy != nil {
					if err := onHealthy(); err != nil {
						return fmt.Errorf("清理上一容器失败: %w", err)
					}
				}
				if err := deployments.SaveSnapshot(ctx, host.ID, containers); err != nil {
					return err
				}
				detail := fmt.Sprintf("发布成功，%d 个容器运行正常", len(matched))
				if err := deployments.UpdateRelease(ctx, release.ID, deployment.ReleaseSucceeded, detail, imageRef, revision, manifest); err != nil {
					return err
				}
				reporter.Progress(98, detail)
				return nil
			}
		} else {
			lastDetail = "尚未发现发布后的容器"
		}
		if time.Now().After(deadline) {
			return errors.New("健康检查超时: " + lastDetail)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func (s *SystemService) cacheDirectory() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.paths.CacheDir
}
