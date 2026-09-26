package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Keep Compose warnings on stderr out of the JSON document.
func runLocalJSONCommand(ctx context.Context, directory string, environment []string, executable string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = directory
	cmd.Env = environment
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("读取 Compose 配置失败: %w", err)
	}
	return string(out), nil
}

var pushedDigestPattern = regexp.MustCompile(`digest:\s+(sha256:[a-f0-9]{64})\b`)
var pinnedImagePattern = regexp.MustCompile(`^.+@sha256:[a-f0-9]{64}$`)

func pinnedPushReference(repository, output string) (string, error) {
	matches := pushedDigestPattern.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return "", errors.New("镜像已推送，但未读取到仓库 Digest；已停止部署，避免记录可变 Tag")
	}
	return repository + "@" + matches[len(matches)-1][1], nil
}

// Compose accepts JSON. Remove build definitions so the remote cannot rebuild
// a saved release; escape dollars to preserve already-resolved literal values.
func pinnedComposeManifest(config string) (string, error) {
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
			return "", fmt.Errorf("无效 Compose 服务 %s", name)
		}
		image, _ := service["image"].(string)
		if !pinnedImagePattern.MatchString(image) {
			return "", fmt.Errorf("Compose 服务 %s 未锁定镜像 Digest，无法安全发布或回滚", name)
		}
		delete(service, "build")
	}
	output, err := json.Marshal(document)
	return strings.ReplaceAll(string(output), "$", "$$"), err
}

func validateRollbackArtifact(image, manifest string) error {
	if strings.HasPrefix(image, "compose:") {
		_, err := pinnedComposeManifest(manifest)
		return err
	}
	if !pinnedImagePattern.MatchString(image) {
		return errors.New("该历史版本未保存镜像 Digest，无法保证恢复原制品；请重新发布生成固定制品后再使用回滚")
	}
	return nil
}
