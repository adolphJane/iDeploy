package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Build for the baseline ISA so amd64/v4 hosts can run the same artifact.
func releasePlatform(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "linux/amd64", "linux/x86_64", "linux/amd64/v1", "linux/amd64/v2", "linux/amd64/v3", "linux/amd64/v4":
		return "linux/amd64", nil
	case "linux/arm64", "linux/aarch64", "linux/arm64/v8":
		return "linux/arm64", nil
	default:
		return "", fmt.Errorf("无法支持或识别目标 Docker 平台 %q；当前支持 Linux AMD64 和 ARM64", value)
	}
}

func verifyLocalImagePlatform(ctx context.Context, docker, image, expected string) error {
	output, err := runLocalCommand(ctx, "", nil, docker, "image", "inspect", image, "--format", "{{.Os}}/{{.Architecture}}")
	if err != nil {
		return fmt.Errorf("检查构建镜像架构 %s: %w", image, err)
	}
	actual, err := releasePlatform(output)
	if err != nil || actual != expected {
		return fmt.Errorf("镜像 %s 的平台为 %s，目标服务器需要 %s；请检查 Dockerfile FROM --platform 或 Compose platform 设置", image, strings.TrimSpace(output), expected)
	}
	return nil
}

func composeBuildImagesForPlatform(config, expected string) ([]string, error) {
	var parsed struct {
		Services map[string]struct {
			Image    string          `json:"image"`
			Platform string          `json:"platform"`
			Build    json.RawMessage `json:"build"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(config), &parsed); err != nil {
		return nil, fmt.Errorf("解析 Compose 平台配置: %w", err)
	}
	images := []string{}
	for name, service := range parsed.Services {
		if service.Platform != "" {
			platform, err := releasePlatform(service.Platform)
			if err != nil || platform != expected {
				return nil, fmt.Errorf("Compose 服务 %s 指定平台 %s，与服务器 %s 不匹配", name, service.Platform, expected)
			}
		}
		if len(service.Build) > 0 && string(service.Build) != "null" {
			if service.Image == "" {
				return nil, fmt.Errorf("Compose 构建服务 %s 未配置 image，无法推送发布镜像", name)
			}
			images = append(images, service.Image)
		}
	}
	return images, nil
}
