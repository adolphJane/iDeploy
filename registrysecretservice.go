package main

import (
	"context"
	"errors"
	"ideploy/internal/registry"
	"ideploy/internal/sharedworkspace"
)

// registryPasswordForRelease reads a local Keychain entry when available. In
// shared mode, an unlocked vault can supply the same credential on another Mac.
func (s *SystemService) registryPasswordForRelease(ctx context.Context, value registry.Connection, remoteBuild bool) (string, error) {
	if !remoteBuild {
		return loadRegistryPushPassword(ctx, value)
	}
	if password, err := loadRegistryPushPassword(ctx, value); err == nil {
		return password, nil
	}
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil || status.Mode != "shared" {
		return "", errors.New("远端发布需要已解锁的共享仓库凭据")
	}
	client, err := s.sharedClient(ctx, status.ServerID)
	if err != nil {
		return "", err
	}
	items, err := client.Secrets(ctx)
	if err != nil {
		return "", err
	}
	key, err := loadSharedVaultKey(status.ServerID)
	if err != nil {
		return "", errors.New("共享凭据密钥不可用")
	}
	id := "r.registries:" + value.ID + ":credential"
	for _, item := range items {
		if item.ID == id {
			password, err := sharedworkspace.DecryptSecret(key, id, item.Envelope)
			if err != nil || password == "" {
				return "", errors.New("共享仓库凭据无法解密")
			}
			return password, nil
		}
	}
	return "", errors.New("iDeploy 服务尚未保存该镜像仓库的推送凭据")
}
