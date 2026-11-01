package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const nginxMountTemplate = `{{.HostConfig.NetworkMode}}|{{range .Mounts}}{{if eq .Destination "/etc/nginx/nginx.conf"}}{{.Type}}:{{.Source}}{{end}}{{end}}`

var nginxBackupDir = "/var/lib/ideploy-agent/nginx/backups"

type NginxSnapshot struct {
	Container  string `json:"container"`
	HostConfig string `json:"hostConfig"`
	Config     string `json:"config"`
	Hash       string `json:"hash"`
	Effective  string `json:"effective"`
	Services   string `json:"services"`
}

func ReadNginx(ctx context.Context) (NginxSnapshot, error) {
	names, err := docker(ctx, "ps", "--format", "{{.Names}}")
	if err != nil {
		return NginxSnapshot{}, err
	}
	var found []NginxSnapshot
	for _, name := range strings.Fields(string(names)) {
		if !containerIDPattern.MatchString(name) {
			continue
		}
		path, err := nginxHostConfig(ctx, name)
		if err != nil {
			continue
		}
		found = append(found, NginxSnapshot{Container: name, HostConfig: path})
	}
	if len(found) != 1 {
		return NginxSnapshot{}, fmt.Errorf("expected one host-network Nginx with bind-mounted main config, found %d", len(found))
	}
	value := found[0]
	config, err := os.ReadFile(value.HostConfig)
	if err != nil {
		return NginxSnapshot{}, err
	}
	if len(config) > 256*1024 {
		return NginxSnapshot{}, errors.New("Nginx config too large")
	}
	value.Config = string(config)
	sum := sha256.Sum256(config)
	value.Hash = hex.EncodeToString(sum[:])
	effective, err := docker(ctx, "exec", value.Container, "nginx", "-T")
	if err != nil {
		return NginxSnapshot{}, fmt.Errorf("read effective Nginx config: %w", err)
	}
	if len(effective) > 1024*1024 {
		return NginxSnapshot{}, errors.New("effective Nginx config too large")
	}
	value.Effective = string(effective)
	services, _ := docker(ctx, "ps", "--format", `{{.Names}}|{{.Ports}}|{{.Label "com.docker.compose.project"}}|{{.Label "com.docker.compose.service"}}`)
	value.Services = string(services)
	return value, nil
}

func nginxHostConfig(ctx context.Context, container string) (string, error) {
	if !containerIDPattern.MatchString(container) {
		return "", errors.New("invalid Nginx container")
	}
	version, err := docker(ctx, "exec", container, "nginx", "-v")
	if err != nil || !strings.Contains(string(version), "nginx version:") {
		return "", errors.New("container is not a running Nginx instance")
	}
	metadata, err := docker(ctx, "inspect", "--format", nginxMountTemplate, container)
	if err != nil {
		return "", fmt.Errorf("inspect Nginx mount: %w", err)
	}
	mode, mount, ok := strings.Cut(strings.TrimSpace(string(metadata)), "|")
	if !ok || mode != "host" || !strings.HasPrefix(mount, "bind:/") {
		return "", errors.New("Nginx must use host network and bind-mounted main config")
	}
	path := strings.TrimPrefix(mount, "bind:")
	if strings.ContainsAny(path, "\r\n") || !filepath.IsAbs(path) {
		return "", errors.New("invalid Nginx config mount")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 256*1024 {
		return "", errors.New("Nginx config is not a regular file or is too large")
	}
	return path, nil
}

func applyNginx(ctx context.Context, req OperationRequest) (string, error) {
	path, err := nginxHostConfig(ctx, req.NginxContainer)
	if err != nil {
		return "", err
	}
	previous, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(previous) > 256*1024 {
		return "", errors.New("Nginx config too large")
	}
	sum := sha256.Sum256(previous)
	if hex.EncodeToString(sum[:]) != req.ExpectedHash {
		return "", errors.New("Nginx config changed since preview")
	}
	newHash := sha256.Sum256([]byte(req.NginxConfig))
	if string(previous) == req.NginxConfig {
		return hex.EncodeToString(newHash[:]), nil
	}
	if err := os.MkdirAll(nginxBackupDir, 0o700); err != nil {
		return "", err
	}
	backup := filepath.Join(nginxBackupDir, req.ID+".conf")
	if err := os.WriteFile(backup, previous, 0o600); err != nil {
		return "", err
	}
	restore := func() error {
		restoreCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := writeNginxConfig(path, previous); err != nil {
			return err
		}
		if _, err := docker(restoreCtx, "exec", req.NginxContainer, "nginx", "-t"); err != nil {
			return err
		}
		_, err := docker(restoreCtx, "exec", req.NginxContainer, "nginx", "-s", "reload")
		return err
	}
	if err := writeNginxConfig(path, []byte(req.NginxConfig)); err != nil {
		return "", fmt.Errorf("write Nginx config: %w; restore: %v", err, restore())
	}
	if output, err := docker(ctx, "exec", req.NginxContainer, "nginx", "-t"); err != nil {
		return "", fmt.Errorf("Nginx validation failed: %v: %.200s; restore: %v", err, output, restore())
	}
	if output, err := docker(ctx, "exec", req.NginxContainer, "nginx", "-s", "reload"); err != nil {
		return "", fmt.Errorf("Nginx reload failed: %v: %.200s; restore: %v", err, output, restore())
	}
	return hex.EncodeToString(newHash[:]), nil
}

func writeNginxConfig(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
