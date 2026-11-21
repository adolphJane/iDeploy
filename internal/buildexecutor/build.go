package buildexecutor

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
)

var secretIDPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
var imagePathPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,254}$`)
var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type RegistryAuth struct{ Host, Username, Password string }
type ContainerBuildInput struct {
	Checkout Checkout
	Version  string
	Registry RegistryAuth
	Secrets  map[string]string
}

// BuildContainer uses BuildKit secret mounts for build-time credentials and
// a private Docker config for registry authentication. It never returns raw
// command output because Dockerfiles can print secret values themselves.
func BuildContainer(ctx context.Context, input ContainerBuildInput) (string, error) {
	doc := input.Checkout.Document
	if err := doc.Validate(); err != nil || doc.WorkloadKind != "container" {
		return "", errors.New("单容器构建计划无效")
	}
	if !versionPattern.MatchString(input.Version) || !imagePathPattern.MatchString(doc.ImagePath) || strings.Contains(doc.ImagePath, "@") || strings.LastIndex(doc.ImagePath, ":") > strings.LastIndex(doc.ImagePath, "/") {
		return "", errors.New("镜像名称或版本无效")
	}
	if !imageRepositoryMatchesRegistry(doc.ImagePath, input.Registry.Host) {
		return "", errors.New("构建镜像不属于任务指定的镜像仓库")
	}
	if input.Registry.Host == "" || input.Registry.Username == "" || input.Registry.Password == "" {
		return "", errors.New("镜像仓库推送凭据不可用")
	}
	root := input.Checkout.Root
	contextPath, err := safeBuildPath(root, doc.ContextPath)
	if err != nil {
		return "", err
	}
	dockerfile, err := safeBuildPath(root, doc.Dockerfile)
	if err != nil {
		return "", err
	}
	private, err := os.MkdirTemp("", "ideploy-build-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(private)
	config := filepath.Join(private, "docker-config")
	if err := os.Mkdir(config, 0700); err != nil {
		return "", err
	}
	env := append(os.Environ(), "DOCKER_CONFIG="+config)
	login := exec.CommandContext(ctx, "docker", "login", input.Registry.Host, "--username", input.Registry.Username, "--password-stdin")
	login.Env = env
	login.Stdin = strings.NewReader(input.Registry.Password + "\n")
	if err := login.Run(); err != nil {
		return "", errors.New("构建服务器登录镜像仓库失败")
	}
	image := doc.ImagePath + ":" + input.Version
	metadata := filepath.Join(private, "metadata.json")
	args := []string{"buildx", "build", "--platform", doc.Platform, "--pull", "--push", "--metadata-file", metadata, "--build-arg", "IDEPLOY_BASE_PATH=" + doc.BasePath, "-f", dockerfile, "-t", image}
	for id, value := range input.Secrets {
		if !secretIDPattern.MatchString(id) || value == "" {
			return "", fmt.Errorf("构建秘密 %q 无效", id)
		}
		secretFile := filepath.Join(private, "secret-"+id)
		if err := os.WriteFile(secretFile, []byte(value), 0600); err != nil {
			return "", err
		}
		args = append(args, "--secret", "id="+id+",src="+secretFile)
	}
	args = append(args, contextPath)
	build := exec.CommandContext(ctx, "docker", args...)
	build.Env = env
	if err := build.Run(); err != nil {
		return "", errors.New("远端 Docker 构建或镜像推送失败")
	}
	data, err := os.ReadFile(metadata)
	if err != nil {
		return "", errors.New("构建未生成镜像摘要")
	}
	var result struct {
		Digest string `json:"containerimage.digest"`
	}
	if err := json.Unmarshal(data, &result); err != nil || !strings.HasPrefix(result.Digest, "sha256:") || len(result.Digest) != 71 {
		return "", errors.New("构建生成的镜像摘要无效")
	}
	return doc.ImagePath + "@" + result.Digest, nil
}

func imageRepositoryMatchesRegistry(repository, registryHost string) bool {
	if repository == "" || registryHost == "" || strings.Contains(repository, "..") || strings.ContainsAny(registryHost, "/@ ") {
		return false
	}
	first, _, hasPath := strings.Cut(strings.ToLower(repository), "/")
	explicit := hasPath && (strings.ContainsAny(first, ".:") || first == "localhost")
	host := strings.ToLower(registryHost)
	if explicit {
		return first == host
	}
	return host == "docker.io" || host == "registry-1.docker.io" || host == "index.docker.io"
}

func safeBuildPath(root, relative string) (string, error) {
	if root == "" || relative == "" || filepath.IsAbs(relative) {
		return "", errors.New("构建路径无效")
	}
	joined := filepath.Join(root, relative)
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", err
	}
	inside, err := filepath.Rel(realRoot, realPath)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", errors.New("构建路径超出源码目录")
	}
	return realPath, nil
}
