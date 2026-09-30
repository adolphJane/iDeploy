package databasebrowser

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

const passwordService = "com.ideploy.database.password"

var ErrCredentialUnavailable = errors.New("数据库密码凭据不可用")

func PasswordCredentialRef(connectionID string) string {
	return "keychain:database-password:" + connectionID
}

func credentialAccount(ref string) (string, bool) {
	account := strings.TrimPrefix(ref, "keychain:database-password:")
	return account, account != "" && account != ref
}

func SavePassword(ctx context.Context, ref, password string) error {
	if password == "" {
		return errors.New("数据库密码不能为空")
	}
	if runtime.GOOS != "darwin" {
		return errors.New("当前平台暂不支持系统钥匙串密码存储")
	}
	account, ok := credentialAccount(ref)
	if !ok {
		return errors.New("数据库密码凭据引用无效")
	}
	output, err := exec.CommandContext(ctx, "security", "add-generic-password", "-a", account, "-s", passwordService, "-w", password, "-U").CombinedOutput()
	if err != nil {
		return fmt.Errorf("保存数据库密码到系统钥匙串: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func LoadPassword(ctx context.Context, ref string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", ErrCredentialUnavailable
	}
	account, ok := credentialAccount(ref)
	if !ok {
		return "", ErrCredentialUnavailable
	}
	output, err := exec.CommandContext(ctx, "security", "find-generic-password", "-a", account, "-s", passwordService, "-w").Output()
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
	if runtime.GOOS != "darwin" || ref == "" {
		return nil
	}
	account, ok := credentialAccount(ref)
	if !ok {
		return ErrCredentialUnavailable
	}
	output, err := exec.CommandContext(ctx, "security", "delete-generic-password", "-a", account, "-s", passwordService).CombinedOutput()
	if err != nil && !strings.Contains(string(output), "could not be found") {
		return fmt.Errorf("从系统钥匙串删除数据库密码: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
