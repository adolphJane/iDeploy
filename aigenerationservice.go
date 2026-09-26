package main

import (
	"context"
	"errors"
	"fmt"
	"ideploy/internal/ai"
	"ideploy/internal/project"
	"regexp"
	"strings"
	"time"
)

type AISettingsView struct {
	DeepSeekConfigured bool `json:"deepSeekConfigured"`
}

type DockerfileGenerationInput struct {
	ProjectID string `json:"projectId"`
	Mode      string `json:"mode"` // auto or deepseek
}

type DockerfileGenerationView struct {
	Content        string   `json:"content"`
	Technology     string   `json:"technology"`
	Source         string   `json:"source"`
	GenerationMode string   `json:"generationMode"`
	Detail         string   `json:"detail"`
	Warnings       []string `json:"warnings"`
}

type DockerfileSaveInput struct {
	ProjectID string `json:"projectId"`
	Content   string `json:"content"`
	Overwrite bool   `json:"overwrite"`
}

func (s *SystemService) GetAISettings() (AISettingsView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := ai.LoadAPIKey(ctx)
	return AISettingsView{DeepSeekConfigured: err == nil}, nil
}

func (s *SystemService) SaveDeepSeekAPIKey(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if strings.TrimSpace(key) == "" {
		return ai.DeleteAPIKey(ctx)
	}
	return ai.SaveAPIKey(ctx, key)
}

func (s *SystemService) GenerateDockerfile(input DockerfileGenerationInput) (DockerfileGenerationView, error) {
	projects, err := s.projectStore()
	if err != nil {
		return DockerfileGenerationView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	value, err := projects.Get(ctx, strings.TrimSpace(input.ProjectID))
	if err != nil {
		return DockerfileGenerationView{}, err
	}
	if value.SourceKind != project.SourceLocal {
		return DockerfileGenerationView{}, errors.New("Git 项目需要先拉取到本地后才能生成 Dockerfile")
	}
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode == "" {
		mode = "auto"
	}
	if mode != "auto" && mode != "deepseek" {
		return DockerfileGenerationView{}, errors.New("不支持的 Dockerfile 生成模式")
	}
	if mode == "auto" {
		suggestion, err := project.GenerateDockerfile(value.LocalPath, value.ContextPath)
		if err != nil {
			return DockerfileGenerationView{}, err
		}
		if suggestion.Content == "" {
			return DockerfileGenerationView{
				Technology: suggestion.Technology, Source: suggestion.Source, GenerationMode: "template",
				Detail: "内置模板未识别到项目技术栈，可尝试 DeepSeek 生成。", Warnings: suggestion.Warnings,
			}, nil
		}
		return DockerfileGenerationView{
			Content: suggestion.Content, Technology: suggestion.Technology, Source: suggestion.Source,
			GenerationMode: "template", Detail: "已使用内置模板生成，可编辑后确认写入。", Warnings: suggestion.Warnings,
		}, nil
	}
	apiKey, err := ai.LoadAPIKey(ctx)
	if err != nil {
		return DockerfileGenerationView{}, err
	}
	summary, err := project.AIProjectSummary(value.LocalPath, value.ContextPath)
	if err != nil {
		return DockerfileGenerationView{}, err
	}
	content, err := ai.GenerateDockerfile(ctx, apiKey, summary)
	if err != nil {
		return DockerfileGenerationView{}, err
	}
	warnings := validateGeneratedDockerfile(content)
	return DockerfileGenerationView{
		Content: content, Technology: "DeepSeek 推断", Source: "项目清单摘要",
		GenerationMode: "deepseek", Detail: "DeepSeek 已生成草案，请核对启动命令、端口和依赖后再写入。", Warnings: warnings,
	}, nil
}

func (s *SystemService) SaveGeneratedDockerfile(input DockerfileSaveInput) (ProjectView, error) {
	projects, err := s.projectStore()
	if err != nil {
		return ProjectView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	value, err := projects.Get(ctx, strings.TrimSpace(input.ProjectID))
	if err != nil {
		return ProjectView{}, err
	}
	if value.SourceKind != project.SourceLocal {
		return ProjectView{}, errors.New("Git 项目不能直接写入本地 Dockerfile")
	}
	content := strings.TrimSpace(input.Content)
	if err := validateDockerfileContent(content); err != nil {
		return ProjectView{}, err
	}
	path, err := project.WriteGeneratedDockerfile(value.LocalPath, value.ContextPath, content, input.Overwrite)
	if err != nil {
		return ProjectView{}, err
	}
	updated, err := projects.Update(ctx, value.ID, project.Input{
		Name: value.Name, Description: value.Description, SourceKind: value.SourceKind,
		LocalPath: value.LocalPath, RepositoryURL: value.RepositoryURL, RefKind: value.RefKind,
		RefValue: value.RefValue, ContextPath: value.ContextPath, DockerfilePath: path,
		ComposePath: value.ComposePath, ImageRepository: value.ImageRepository,
	}, project.Inspection{Status: project.StatusReady, Detail: "已生成 Dockerfile", DockerfilePath: path, ComposePath: value.ComposePath})
	if err != nil {
		return ProjectView{}, err
	}
	s.emitProjectView(toProjectView(updated))
	return toProjectView(updated), nil
}

func validateDockerfileContent(content string) error {
	if content == "" || len(content) > 128*1024 || !strings.Contains(strings.ToUpper(content), "FROM ") {
		return errors.New("Dockerfile 内容无效：必须包含 FROM 指令且不超过 128 KB")
	}
	if strings.Contains(strings.ToLower(content), "docker.sock") || strings.Contains(strings.ToLower(content), "--privileged") {
		return errors.New("Dockerfile 包含高风险 Docker 权限配置，请手动移除后再保存")
	}
	return nil
}

func validateGeneratedDockerfile(content string) []string {
	warnings := make([]string, 0)
	if err := validateDockerfileContent(content); err != nil {
		warnings = append(warnings, err.Error())
	}
	lower := strings.ToLower(content)
	if strings.Contains(lower, "curl | sh") || strings.Contains(lower, "wget | sh") {
		warnings = append(warnings, "检测到远程脚本管道执行，请确认来源可信后再使用。")
	}
	if strings.Contains(content, "TODO") {
		warnings = append(warnings, "生成结果包含 TODO，请补充启动命令或端口。")
	}
	for _, image := range dockerfileBaseImages(content) {
		lowerImage := strings.ToLower(image)
		if strings.Contains(lowerImage, "/proxy/") || strings.Contains(lowerImage, "/mirror/") {
			warnings = append(warnings, fmt.Sprintf("基础镜像 %s 使用了私有代理路径；写入前请确认该仓库已配置 Proxy Cache，且能访问对应上游镜像。", image))
		}
	}
	if len(warnings) == 0 {
		warnings = append(warnings, "请在写入前确认基础镜像、启动命令和端口与项目实际情况一致。")
	}
	return warnings
}

var dockerfileFromPattern = regexp.MustCompile(`(?im)^\s*FROM(?:\s+--platform=\S+)?\s+(\S+)`)

func dockerfileBaseImages(content string) []string {
	matches := dockerfileFromPattern.FindAllStringSubmatch(content, -1)
	images := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) == 2 && !strings.EqualFold(match[1], "scratch") {
			images = append(images, match[1])
		}
	}
	return images
}
