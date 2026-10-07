package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"ideploy/internal/job"
	"ideploy/internal/registry"
)

const buildProxySettingsKey = "build_proxy_settings"

type BuildProxyRule struct {
	Upstream string `json:"upstream"`
	Mirror   string `json:"mirror"`
}

type BuildProxySettingsInput struct {
	Mode  string           `json:"mode"`
	Rules []BuildProxyRule `json:"rules"`
}

type BuildProxySettingsView struct {
	Mode        string           `json:"mode"`
	Rules       []BuildProxyRule `json:"rules"`
	Configured  bool             `json:"configured"`
	BuilderName string           `json:"builderName"`
	Detail      string           `json:"detail"`
}

type buildProxyRuntime struct {
	Mode        string
	BuilderName string
	Rules       []BuildProxyRule
}

var (
	buildProxyUpstreamPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?(?::[0-9]{1,5})?$`)
	buildProxyMirrorPattern   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?(?::[0-9]{1,5})?(?:/[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?)+$`)
)

func validateBuildProxySettings(input BuildProxySettingsInput) (BuildProxySettingsInput, error) {
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	if input.Mode == "" {
		input.Mode = "disabled"
	}
	switch input.Mode {
	case "disabled", "preferred", "required":
	default:
		return input, errors.New("构建代理策略必须是 disabled、preferred 或 required")
	}
	if len(input.Rules) > 16 {
		return input, errors.New("构建代理规则不能超过 16 条")
	}

	seen := make(map[string]bool, len(input.Rules))
	rules := make([]BuildProxyRule, 0, len(input.Rules))
	for _, rule := range input.Rules {
		upstream := strings.ToLower(strings.Trim(strings.TrimSpace(rule.Upstream), "/"))
		mirror := strings.ToLower(strings.Trim(strings.TrimSpace(rule.Mirror), "/"))
		if !buildProxyUpstreamPattern.MatchString(upstream) {
			return input, fmt.Errorf("上游仓库 %q 格式无效，请填写主机名和可选端口", rule.Upstream)
		}
		if !buildProxyMirrorPattern.MatchString(mirror) {
			return input, fmt.Errorf("代理地址 %q 格式无效，请填写 host/proxy-project，不要包含协议或 Tag", rule.Mirror)
		}
		if seen[upstream] {
			return input, fmt.Errorf("上游仓库 %s 配置了重复规则", upstream)
		}
		seen[upstream] = true
		rules = append(rules, BuildProxyRule{Upstream: upstream, Mirror: mirror})
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Upstream < rules[j].Upstream })
	input.Rules = rules
	if input.Mode != "disabled" && len(input.Rules) == 0 {
		return input, errors.New("启用构建代理时至少需要一条上游映射")
	}
	return input, nil
}

func buildProxyConfig(settings BuildProxySettingsInput) string {
	var builder strings.Builder
	builder.WriteString("# Managed by iDeploy. Changes are replaced when proxy settings change.\n")
	for _, rule := range settings.Rules {
		fmt.Fprintf(&builder, "\n[registry.%q]\n  mirrors = [%q]\n", rule.Upstream, rule.Mirror)
	}
	return builder.String()
}

func buildProxyBuilderName(config string) string {
	digest := sha256.Sum256([]byte(config))
	return "ideploy-proxy-" + hex.EncodeToString(digest[:6])
}

func buildProxyView(settings BuildProxySettingsInput) BuildProxySettingsView {
	view := BuildProxySettingsView{Mode: settings.Mode, Rules: settings.Rules}
	if view.Rules == nil {
		view.Rules = []BuildProxyRule{}
	}
	view.Configured = settings.Mode != "disabled" && len(settings.Rules) > 0
	if view.Configured {
		view.BuilderName = buildProxyBuilderName(buildProxyConfig(settings))
		view.Detail = fmt.Sprintf("%s 模式 · %d 条代理规则", settings.Mode, len(settings.Rules))
	} else {
		view.Detail = "构建代理未启用，基础镜像按 Docker 当前配置拉取"
	}
	return view
}

func (s *SystemService) GetBuildProxySettings() (BuildProxySettingsView, error) {
	db, err := s.systemDatabase()
	if err != nil {
		return BuildProxySettingsView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	settings, err := loadBuildProxySettings(ctx, db)
	if err != nil {
		return BuildProxySettingsView{}, err
	}
	return buildProxyView(settings), nil
}

func (s *SystemService) SaveBuildProxySettings(input BuildProxySettingsInput) (BuildProxySettingsView, error) {
	settings, err := validateBuildProxySettings(input)
	if err != nil {
		return BuildProxySettingsView{}, err
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return BuildProxySettingsView{}, fmt.Errorf("编码构建代理配置: %w", err)
	}
	db, err := s.systemDatabase()
	if err != nil {
		return BuildProxySettingsView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, `
		INSERT INTO app_settings(key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at
	`, buildProxySettingsKey, string(encoded), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return BuildProxySettingsView{}, fmt.Errorf("保存构建代理配置: %w", err)
	}
	return buildProxyView(settings), nil
}

func (s *SystemService) systemDatabase() (*sql.DB, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, errors.New("system service is not ready")
	}
	return s.db, nil
}

func loadBuildProxySettings(ctx context.Context, db *sql.DB) (BuildProxySettingsInput, error) {
	settings := BuildProxySettingsInput{Mode: "disabled", Rules: []BuildProxyRule{}}
	var encoded string
	err := db.QueryRowContext(ctx, "SELECT value FROM app_settings WHERE key=?", buildProxySettingsKey).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return settings, nil
	}
	if err != nil {
		return settings, fmt.Errorf("读取构建代理配置: %w", err)
	}
	if err := json.Unmarshal([]byte(encoded), &settings); err != nil {
		return settings, fmt.Errorf("解析构建代理配置: %w", err)
	}
	settings, err = validateBuildProxySettings(settings)
	if err != nil {
		return settings, fmt.Errorf("构建代理配置无效: %w", err)
	}
	return settings, nil
}

func (s *SystemService) prepareBuildProxy(ctx context.Context, dockerPath string, authenticatedRegistry registry.Connection, reporter job.Reporter) (buildProxyRuntime, error) {
	db, err := s.systemDatabase()
	if err != nil {
		return buildProxyRuntime{}, err
	}
	settings, err := loadBuildProxySettings(ctx, db)
	if err != nil {
		return buildProxyRuntime{}, err
	}
	if settings.Mode == "disabled" {
		return buildProxyRuntime{Mode: settings.Mode}, nil
	}

	fail := func(cause error) (buildProxyRuntime, error) {
		wrapped := fmt.Errorf("准备 iDeploy 构建代理: %w", cause)
		if settings.Mode == "required" {
			return buildProxyRuntime{}, wrapped
		}
		reporter.Log("构建代理不可用，preferred 模式回退到 Docker 默认构建器：" + cause.Error())
		return buildProxyRuntime{Mode: settings.Mode}, nil
	}

	if _, err := runLocalCommand(ctx, "", nil, dockerPath, "buildx", "version"); err != nil {
		return fail(errors.New("Docker Buildx 不可用: " + err.Error()))
	}
	if err := s.loginBuildProxyRegistries(ctx, dockerPath, authenticatedRegistry, settings.Rules, reporter); err != nil {
		return fail(err)
	}

	config := buildProxyConfig(settings)
	name := buildProxyBuilderName(config)
	directory := filepath.Join(s.cacheDirectory(), "buildkit")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fail(fmt.Errorf("创建 BuildKit 配置目录: %w", err))
	}
	configPath := filepath.Join(directory, name+".toml")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		return fail(fmt.Errorf("写入 BuildKit 配置: %w", err))
	}
	if err := os.Chmod(configPath, 0o600); err != nil {
		return fail(fmt.Errorf("保护 BuildKit 配置权限: %w", err))
	}

	if _, err := runLocalCommand(ctx, "", nil, dockerPath, "buildx", "inspect", name); err != nil {
		if _, createErr := runLocalCommand(ctx, "", nil, dockerPath, "buildx", "create",
			"--name", name, "--driver", "docker-container", "--driver-opt", "default-load=true",
			"--buildkitd-config", configPath); createErr != nil {
			return fail(fmt.Errorf("创建专用 BuildKit Builder: %w", createErr))
		}
	}
	if _, err := runLocalCommand(ctx, "", nil, dockerPath, "buildx", "inspect", "--bootstrap", name); err != nil {
		return fail(fmt.Errorf("启动专用 BuildKit Builder: %w", err))
	}

	reporter.Log(fmt.Sprintf("构建代理已启用：%s · %d 条映射 · Builder %s", settings.Mode, len(settings.Rules), name))
	return buildProxyRuntime{Mode: settings.Mode, BuilderName: name, Rules: settings.Rules}, nil
}

func (s *SystemService) loginBuildProxyRegistries(ctx context.Context, dockerPath string, authenticatedRegistry registry.Connection, rules []BuildProxyRule, reporter job.Reporter) error {
	stores, err := s.registryStore()
	if err != nil {
		return err
	}
	connections, err := stores.List(ctx)
	if err != nil {
		return err
	}
	byHost := make(map[string]registry.Connection, len(connections))
	for _, connection := range connections {
		byHost[strings.ToLower(registry.DockerHost(connection.Endpoint))] = connection
	}
	already := map[string]bool{strings.ToLower(registry.DockerHost(authenticatedRegistry.Endpoint)): true}
	for _, rule := range rules {
		host := strings.SplitN(rule.Mirror, "/", 2)[0]
		if already[host] {
			continue
		}
		already[host] = true
		connection, ok := byHost[host]
		if !ok || strings.TrimSpace(connection.Username) == "" || strings.TrimSpace(connection.CredentialRef) == "" {
			reporter.Log("构建代理 " + host + " 未关联可用的 iDeploy Registry 凭据，将尝试匿名拉取")
			continue
		}
		password, err := registry.LoadPassword(ctx, connection.CredentialRef)
		if err != nil {
			return fmt.Errorf("读取构建代理 %s 的 Registry 凭据: %w", host, err)
		}
		if _, err := runLocalCommandInput(ctx, "", nil, password+"\n", dockerPath, "login", host, "--username", connection.Username, "--password-stdin"); err != nil {
			return fmt.Errorf("登录构建代理 %s: %w", host, err)
		}
	}
	return nil
}
