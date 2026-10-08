package deployment

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

const environmentSecretService = "com.ideploy.deployment.environment"
const environmentSecretPrefix = "keychain:deployment-environment:"

var ErrEnvironmentSecretUnavailable = errors.New("部署环境秘密凭据不可用")

func EnvironmentSecretCredentialRef(variableID string) string {
	return environmentSecretPrefix + variableID
}

func environmentSecretAccount(ref string) (string, bool) {
	if !strings.HasPrefix(ref, environmentSecretPrefix) {
		return "", false
	}
	account := strings.TrimPrefix(ref, environmentSecretPrefix)
	return account, account != ""
}

func SaveEnvironmentSecret(ctx context.Context, ref, secret string) error {
	account, ok := environmentSecretAccount(ref)
	if !ok || secret == "" {
		return errors.New("部署环境秘密不能为空")
	}
	if strings.ContainsAny(secret, "\r\n") {
		return errors.New("部署环境秘密暂不支持换行")
	}
	if runtime.GOOS != "darwin" {
		return errors.New("当前平台暂不支持系统钥匙串秘密存储")
	}
	output, err := exec.CommandContext(ctx, "security", "add-generic-password", "-a", account, "-s", environmentSecretService, "-w", secret, "-U").CombinedOutput()
	if err != nil {
		return fmt.Errorf("保存部署环境秘密到系统钥匙串: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func LoadEnvironmentSecret(ctx context.Context, ref string) (string, error) {
	account, ok := environmentSecretAccount(ref)
	if !ok || runtime.GOOS != "darwin" {
		return "", ErrEnvironmentSecretUnavailable
	}
	output, err := exec.CommandContext(ctx, "security", "find-generic-password", "-a", account, "-s", environmentSecretService, "-w").Output()
	if err != nil {
		return "", ErrEnvironmentSecretUnavailable
	}
	secret := strings.TrimSuffix(string(output), "\n")
	if secret == "" {
		return "", ErrEnvironmentSecretUnavailable
	}
	return secret, nil
}

func DeleteEnvironmentSecret(ctx context.Context, ref string) error {
	account, ok := environmentSecretAccount(ref)
	if !ok || runtime.GOOS != "darwin" {
		return nil
	}
	output, err := exec.CommandContext(ctx, "security", "delete-generic-password", "-a", account, "-s", environmentSecretService).CombinedOutput()
	if err != nil && !strings.Contains(string(output), "could not be found") {
		return fmt.Errorf("从系统钥匙串删除部署环境秘密: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
