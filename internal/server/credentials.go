package server

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

const (
	sshPasswordService      = "com.ideploy.ssh.password"
	sshKeyPassphraseService = "com.ideploy.ssh.key-passphrase"
)

var ErrCredentialUnavailable = errors.New("SSH 密码凭据不可用")

func PasswordCredentialRef(serverID string) string {
	return "keychain:ssh-password:" + serverID
}

func PrivateKeyPassphraseCredentialRef(serverID string) string {
	return "keychain:ssh-key-passphrase:" + serverID
}

func credentialLocation(ref string) (service, account string, ok bool) {
	for _, value := range []struct {
		prefix  string
		service string
	}{
		{prefix: "keychain:ssh-password:", service: sshPasswordService},
		{prefix: "keychain:ssh-key-passphrase:", service: sshKeyPassphraseService},
	} {
		if strings.HasPrefix(ref, value.prefix) {
			account = strings.TrimPrefix(ref, value.prefix)
			return value.service, account, account != ""
		}
	}
	return "", "", false
}

func SavePassword(ctx context.Context, ref, password string) error {
	if ref == "" || password == "" {
		return errors.New("SSH 密码不能为空")
	}
	if runtime.GOOS != "darwin" {
		return errors.New("当前平台暂不支持系统钥匙串密码存储")
	}
	service, account, ok := credentialLocation(ref)
	if !ok {
		return errors.New("SSH 密码凭据引用无效")
	}
	command := exec.CommandContext(ctx, "security", "add-generic-password", "-a", account, "-s", service, "-w", password, "-U")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("保存 SSH 密码到系统钥匙串: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func LoadPassword(ctx context.Context, ref string) (string, error) {
	if ref == "" || runtime.GOOS != "darwin" {
		return "", ErrCredentialUnavailable
	}
	service, account, ok := credentialLocation(ref)
	if !ok {
		return "", ErrCredentialUnavailable
	}
	command := exec.CommandContext(ctx, "security", "find-generic-password", "-a", account, "-s", service, "-w")
	output, err := command.Output()
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
	if ref == "" || runtime.GOOS != "darwin" {
		return nil
	}
	service, account, ok := credentialLocation(ref)
	if !ok {
		return ErrCredentialUnavailable
	}
	command := exec.CommandContext(ctx, "security", "delete-generic-password", "-a", account, "-s", service)
	if output, err := command.CombinedOutput(); err != nil && !strings.Contains(string(output), "could not be found") {
		return fmt.Errorf("从系统钥匙串删除 SSH 密码: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
