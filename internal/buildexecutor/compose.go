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

var pinnedComposeImage = regexp.MustCompile(`^.+@sha256:[a-f0-9]{64}$`)

type ComposeBuildInput struct {
	Checkout Checkout
	Version  string
	Registry RegistryAuth
	Secrets  map[string]string
}

func BuildCompose(ctx context.Context, input ComposeBuildInput) (string, error) {
	doc := input.Checkout.Document
	if doc.Validate() != nil || doc.WorkloadKind != "compose" || !versionPattern.MatchString(input.Version) {
		return "", errors.New("Compose 构建计划无效")
	}
	if input.Registry.Host == "" || input.Registry.Username == "" || input.Registry.Password == "" {
		return "", errors.New("镜像仓库凭据不可用")
	}
	file, err := safeBuildPath(input.Checkout.Root, doc.ComposeFile)
	if err != nil {
		return "", err
	}
	composeSource, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	if len(composeSource) > 2*1024*1024 || !strings.Contains(string(composeSource), "${IMAGE_TAG}") && !strings.Contains(string(composeSource), "$IMAGE_TAG") {
		return "", errors.New("Compose 文件需在镜像标签中引用 IMAGE_TAG，且不能超过 2 MB")
	}
	private, err := os.MkdirTemp("", "ideploy-compose-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(private)
	configDir := filepath.Join(private, "docker-config")
	if err := os.Mkdir(configDir, 0700); err != nil {
		return "", err
	}
	env := append(os.Environ(), "DOCKER_CONFIG="+configDir, "IMAGE_TAG="+input.Version, "DOCKER_DEFAULT_PLATFORM="+doc.Platform, "IDEPLOY_BASE_PATH="+doc.BasePath)
	for name, value := range input.Secrets {
		if !composeSecretName.MatchString(name) || value == "" || strings.ContainsRune(value, '\x00') {
			return "", errors.New("Compose 构建秘密名称或内容无效")
		}
		env = append(env, name+"="+value)
	}
	login := exec.CommandContext(ctx, "docker", "login", input.Registry.Host, "--username", input.Registry.Username, "--password-stdin")
	login.Env = env
	login.Stdin = strings.NewReader(input.Registry.Password + "\n")
	if login.Run() != nil {
		return "", errors.New("构建服务器登录镜像仓库失败")
	}
	compose := func(args ...string) ([]byte, error) {
		command := exec.CommandContext(ctx, "docker", append([]string{"compose", "-f", file}, args...)...)
		command.Dir = input.Checkout.Root
		command.Env = env
		return command.Output()
	}
	raw, err := compose("config", "--format", "json")
	if err != nil {
		return "", errors.New("服务端无法解析 Compose 配置")
	}
	if _, err := ComposeBuildImages(string(raw), doc.Platform, input.Registry.Host); err != nil {
		return "", err
	}
	if err := validateComposeBuildPaths(raw, input.Checkout.Root); err != nil {
		return "", err
	}
	if err := validateComposeBuildSecrets(raw, input.Secrets); err != nil {
		return "", err
	}
	if err := ensureComposeSecretsNotExpanded(raw, input.Secrets); err != nil {
		return "", err
	}
	if _, err := compose("build", "--pull", "--push"); err != nil {
		return "", errors.New("远端 Compose 构建或推送失败")
	}
	resolved, err := compose("config", "--resolve-image-digests", "--format", "json")
	if err != nil {
		return "", errors.New("无法解析 Compose 镜像摘要")
	}
	if err := ensureComposeSecretsNotExpanded(resolved, input.Secrets); err != nil {
		return "", err
	}
	clean, err := stripComposeBuildSecrets(resolved)
	if err != nil {
		return "", err
	}
	manifest, err := PinComposeManifest(string(clean))
	if err != nil {
		return "", err
	}
	if len(manifest) > 900*1024 {
		return "", errors.New("Compose 发布快照超过目标 Agent 的 900 KB 限制")
	}
	return manifest, nil
}

var composeSecretName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

func validateComposeBuildSecrets(raw []byte, secrets map[string]string) error {
	var config struct {
		Secrets map[string]struct {
			Environment string `json:"environment"`
			File        string `json:"file"`
		} `json:"secrets"`
		Services map[string]struct {
			Build struct {
				Secrets []struct {
					Source string `json:"source"`
				} `json:"secrets"`
			} `json:"build"`
		} `json:"services"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return err
	}
	for name, service := range config.Services {
		for _, item := range service.Build.Secrets {
			if _, ok := secrets[item.Source]; !ok {
				return fmt.Errorf("Compose 服务 %s 引用了未配置的构建秘密 %s", name, item.Source)
			}
			declaration, ok := config.Secrets[item.Source]
			if !ok || declaration.Environment != item.Source || declaration.File != "" {
				return fmt.Errorf("Compose 构建秘密 %s 需声明 environment: %s", item.Source, item.Source)
			}
		}
	}
	return nil
}

func ensureComposeSecretsNotExpanded(raw []byte, secrets map[string]string) error {
	if len(secrets) == 0 {
		return nil
	}
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		return err
	}
	var walk func(any) bool
	walk = func(value any) bool {
		switch item := value.(type) {
		case string:
			for _, secret := range secrets {
				if item == secret || len(secret) >= 8 && strings.Contains(item, secret) {
					return true
				}
			}
		case map[string]any:
			for _, nested := range item {
				if walk(nested) {
					return true
				}
			}
		case []any:
			for _, nested := range item {
				if walk(nested) {
					return true
				}
			}
		}
		return false
	}
	if walk(document) {
		return errors.New("Compose 构建秘密被展开到配置中，请仅通过 BuildKit secret 引用")
	}
	return nil
}

func stripComposeBuildSecrets(raw []byte) ([]byte, error) {
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	services, _ := config["services"].(map[string]any)
	for name, rawService := range services {
		service, _ := rawService.(map[string]any)
		if value, ok := service["secrets"].([]any); ok && len(value) > 0 {
			return nil, fmt.Errorf("Compose 服务 %s 的运行时 secrets 尚未配置安全交付", name)
		}
	}
	delete(config, "secrets")
	return json.Marshal(config)
}

func validateComposeBuildPaths(raw []byte, root string) error {
	var config struct {
		Services map[string]struct {
			Build struct {
				Context            string          `json:"context"`
				Dockerfile         string          `json:"dockerfile"`
				AdditionalContexts json.RawMessage `json:"additional_contexts"`
			} `json:"build"`
		} `json:"services"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	inside := func(path string) bool {
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return false
		}
		rel, err := filepath.Rel(realRoot, real)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	for name, service := range config.Services {
		build := service.Build
		if build.Context == "" {
			continue
		}
		if !filepath.IsAbs(build.Context) || !inside(build.Context) {
			return fmt.Errorf("Compose 服务 %s 的构建上下文超出源码目录", name)
		}
		if len(build.AdditionalContexts) > 0 && string(build.AdditionalContexts) != "null" && string(build.AdditionalContexts) != "{}" {
			return fmt.Errorf("Compose 服务 %s 暂不支持附加构建上下文", name)
		}
		if build.Dockerfile != "" {
			file := build.Dockerfile
			if !filepath.IsAbs(file) {
				file = filepath.Join(build.Context, file)
			}
			if !inside(file) {
				return fmt.Errorf("Compose 服务 %s 的 Dockerfile 超出源码目录", name)
			}
		}
	}
	return nil
}

// ComposeBuildImages validates the resolved Compose model before a worker
// builds it. Every service with a build section must push to the chosen
// registry, and its platform must match the deployment target.
func ComposeBuildImages(config, platform, registryHost string) ([]string, error) {
	var document struct {
		Services map[string]struct {
			Image    string          `json:"image"`
			Platform string          `json:"platform"`
			Build    json.RawMessage `json:"build"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(config), &document); err != nil {
		return nil, err
	}
	if len(document.Services) == 0 {
		return nil, errors.New("Compose 配置没有服务")
	}
	images := []string{}
	for name, service := range document.Services {
		if service.Platform != "" && service.Platform != platform {
			return nil, fmt.Errorf("Compose 服务 %s 的平台与目标服务器不一致", name)
		}
		if len(service.Build) == 0 || string(service.Build) == "null" {
			continue
		}
		repository := service.Image
		if colon := strings.LastIndex(repository, ":"); colon > strings.LastIndex(repository, "/") {
			repository = repository[:colon]
		}
		if service.Image == "" || !imageRepositoryMatchesRegistry(repository, registryHost) {
			return nil, fmt.Errorf("Compose 构建服务 %s 的镜像不属于指定仓库", name)
		}
		images = append(images, service.Image)
	}
	if len(images) == 0 {
		return nil, errors.New("Compose 配置没有可构建的服务")
	}
	return images, nil
}

// PinComposeManifest strips all build instructions and requires immutable
// digests for every service. The result must be encrypted before persistence
// because Compose environment fields may contain sensitive values.
func PinComposeManifest(config string) (string, error) {
	var document map[string]any
	if err := json.Unmarshal([]byte(config), &document); err != nil {
		return "", err
	}
	services, ok := document["services"].(map[string]any)
	if !ok || len(services) == 0 {
		return "", errors.New("Compose 快照缺少服务")
	}
	for name, raw := range services {
		service, ok := raw.(map[string]any)
		if !ok {
			return "", fmt.Errorf("Compose 服务 %s 无效", name)
		}
		image, _ := service["image"].(string)
		if !pinnedComposeImage.MatchString(image) {
			return "", fmt.Errorf("Compose 服务 %s 未固定镜像摘要", name)
		}
		delete(service, "build")
	}
	data, err := json.Marshal(document)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(data), "$", "$$"), nil
}
