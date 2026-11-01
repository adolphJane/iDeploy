package domain

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

const dnsSecretService = "com.ideploy.alidns.access-key-secret"
const dnsSecretPrefix = "keychain:alidns-access-key-secret:"
const cloudflareSecretService = "com.ideploy.dns.cloudflare-api-token"
const cloudflareSecretPrefix = "keychain:cloudflare-api-token:"

func DNSCredentialRef(id string) string { return dnsSecretPrefix + id }
func DNSCredentialRefForProvider(provider, id string) string {
	if provider == "cloudflare" {
		return cloudflareSecretPrefix + id
	}
	return DNSCredentialRef(id)
}

func credentialAccount(ref string) (string, string, bool) {
	service, prefix := dnsSecretService, dnsSecretPrefix
	if strings.HasPrefix(ref, cloudflareSecretPrefix) {
		service, prefix = cloudflareSecretService, cloudflareSecretPrefix
	} else if !strings.HasPrefix(ref, dnsSecretPrefix) {
		return "", "", false
	}
	account := strings.TrimPrefix(ref, prefix)
	return account, service, account != "" && !strings.ContainsAny(account, ":/\r\n")
}

func SaveDNSSecret(ctx context.Context, ref, secret string) error {
	account, service, ok := credentialAccount(ref)
	if !ok || secret == "" {
		return errors.New("DNS 连接凭据不能为空")
	}
	if runtime.GOOS != "darwin" {
		return errors.New("当前平台暂不支持系统钥匙串存储")
	}
	cmd := exec.CommandContext(ctx, "security", "add-generic-password", "-a", account, "-s", service, "-w", secret, "-U")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("保存 DNS 凭据到钥匙串失败: %w", err)
	}
	return nil
}

func LoadDNSSecret(ctx context.Context, ref string) (string, error) {
	account, service, ok := credentialAccount(ref)
	if !ok || runtime.GOOS != "darwin" {
		return "", errors.New("DNS 连接凭据不可用")
	}
	out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-a", account, "-s", service, "-w").Output()
	if err != nil {
		return "", errors.New("DNS 连接凭据不可用")
	}
	secret := strings.TrimSuffix(string(out), "\n")
	if secret == "" {
		return "", errors.New("DNS 连接凭据不可用")
	}
	return secret, nil
}

func DeleteDNSSecret(ctx context.Context, ref string) error {
	account, service, ok := credentialAccount(ref)
	if !ok || runtime.GOOS != "darwin" {
		return nil
	}
	out, err := exec.CommandContext(ctx, "security", "delete-generic-password", "-a", account, "-s", service).CombinedOutput()
	if err != nil && !strings.Contains(string(out), "could not be found") {
		return fmt.Errorf("删除 DNS 钥匙串凭据失败: %w", err)
	}
	return nil
}
