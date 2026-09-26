package registry

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

const registryPasswordService = "com.ideploy.registry.password"

var ErrCredentialUnavailable = errors.New("镜像仓库密码凭据不可用")

func PasswordCredentialRef(registryID string) string {
	return "keychain:registry-password:" + registryID
}

func credentialAccount(ref string) (string, bool) {
	const prefix = "keychain:registry-password:"
	if !strings.HasPrefix(ref, prefix) {
		return "", false
	}
	account := strings.TrimPrefix(ref, prefix)
	return account, account != ""
}

func SavePassword(ctx context.Context, ref, password string) error {
	account, ok := credentialAccount(ref)
	if !ok || password == "" {
		return errors.New("镜像仓库密码不能为空")
	}
	if runtime.GOOS != "darwin" {
		return errors.New("当前平台暂不支持系统钥匙串密码存储")
	}
	command := exec.CommandContext(ctx, "security", "add-generic-password", "-a", account, "-s", registryPasswordService, "-w", password, "-U")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("保存镜像仓库密码到系统钥匙串: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func LoadPassword(ctx context.Context, ref string) (string, error) {
	account, ok := credentialAccount(ref)
	if !ok || runtime.GOOS != "darwin" {
		return "", ErrCredentialUnavailable
	}
	output, err := exec.CommandContext(ctx, "security", "find-generic-password", "-a", account, "-s", registryPasswordService, "-w").Output()
	if err != nil {
		return "", ErrCredentialUnavailable
	}
	password := strings.TrimSuffix(string(output), "\n")
	if password == "" {
		return "", ErrCredentialUnavailable
	}
	return password, nil
}

func DeletePassword(ctx context.Context, ref string) error {
	account, ok := credentialAccount(ref)
	if !ok || runtime.GOOS != "darwin" {
		return nil
	}
	output, err := exec.CommandContext(ctx, "security", "delete-generic-password", "-a", account, "-s", registryPasswordService).CombinedOutput()
	if err != nil && !strings.Contains(string(output), "could not be found") {
		return fmt.Errorf("从系统钥匙串删除镜像仓库密码: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
