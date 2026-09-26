package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type HostKeyStatus string

const (
	HostKeyUnknown HostKeyStatus = "unknown"
	HostKeyTrusted HostKeyStatus = "trusted"
	HostKeyChanged HostKeyStatus = "changed"
)

// HostKey is deliberately a display-oriented result. Raw key material stays
// inside the server package and is only appended after an explicit trust call.
type HostKey struct {
	Status      HostKeyStatus
	Host        string
	Fingerprint string
	KeyType     string
	Detail      string
	rawLine     string
}

func InspectHostKey(ctx context.Context, connection Connection) (HostKey, error) {
	rawLine, keyType, fingerprint, err := scanHostKey(ctx, connection)
	if err != nil {
		return HostKey{}, err
	}
	value := HostKey{Status: HostKeyUnknown, Host: connection.Host, Fingerprint: fingerprint, KeyType: keyType, Detail: "尚未写入本机 SSH known_hosts", rawLine: rawLine}
	knownPath, err := knownHostsPath()
	if err != nil {
		return value, err
	}
	knownLines, err := lookupKnownHost(ctx, hostLabel(connection), knownPath)
	if err != nil {
		return value, err
	}
	for _, line := range knownLines {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[1] != keyType {
			continue
		}
		if fields[2] == strings.Fields(rawLine)[2] {
			value.Status = HostKeyTrusted
			value.Detail = "本机 SSH known_hosts 已信任此指纹"
		} else {
			value.Status = HostKeyChanged
			value.Detail = "本机已存在同类型但不同的主机密钥，请先人工核对变更原因"
		}
		return value, nil
	}
	return value, nil
}

func TrustHostKey(ctx context.Context, connection Connection, fingerprint string) (HostKey, error) {
	value, err := InspectHostKey(ctx, connection)
	if err != nil {
		return HostKey{}, err
	}
	if value.Fingerprint != fingerprint {
		return HostKey{}, errors.New("主机指纹在确认前发生变化，请重新获取并核对")
	}
	if value.Status == HostKeyChanged {
		return value, errors.New(value.Detail)
	}
	if value.Status == HostKeyTrusted {
		return value, nil
	}
	knownPath, err := knownHostsPath()
	if err != nil {
		return HostKey{}, err
	}
	sshDir := filepath.Dir(knownPath)
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return HostKey{}, fmt.Errorf("create SSH config directory: %w", err)
	}
	file, err := os.OpenFile(knownPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return HostKey{}, fmt.Errorf("open SSH known_hosts: %w", err)
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return HostKey{}, fmt.Errorf("secure SSH known_hosts: %w", err)
	}
	if _, err := file.WriteString(strings.TrimSpace(value.rawLine) + "\n"); err != nil {
		return HostKey{}, fmt.Errorf("save SSH host key: %w", err)
	}
	value.Status = HostKeyTrusted
	value.Detail = "已写入本机 SSH known_hosts"
	return value, nil
}

func scanHostKey(ctx context.Context, connection Connection) (string, string, string, error) {
	for _, keyType := range []string{"ed25519", "ecdsa", "rsa"} {
		command := exec.CommandContext(ctx, "ssh-keyscan", "-T", "5", "-p", fmt.Sprint(connection.Port), "-t", keyType, connection.Host)
		output, err := command.Output()
		if err != nil || strings.TrimSpace(string(output)) == "" {
			continue
		}
		line := ""
		for _, candidate := range strings.Split(string(output), "\n") {
			candidate = strings.TrimSpace(candidate)
			if candidate != "" && !strings.HasPrefix(candidate, "#") {
				line = candidate
				break
			}
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		fingerprint, err := fingerprintForKey(ctx, line)
		if err != nil {
			return "", "", "", err
		}
		return line, fields[1], fingerprint, nil
	}
	return "", "", "", fmt.Errorf("无法获取 %s:%d 的 SSH 主机指纹，请检查端口和网络", connection.Host, connection.Port)
}

func fingerprintForKey(ctx context.Context, line string) (string, error) {
	command := exec.CommandContext(ctx, "ssh-keygen", "-lf", "-")
	command.Stdin = bytes.NewBufferString(line + "\n")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("计算 SSH 主机指纹: %w", err)
	}
	fields := strings.Fields(string(output))
	if len(fields) < 2 {
		return "", errors.New("SSH 主机指纹格式无效")
	}
	return fields[1], nil
}

func knownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".ssh", "known_hosts"), nil
}

func hostLabel(connection Connection) string {
	if connection.Port == 22 {
		return connection.Host
	}
	return fmt.Sprintf("[%s]:%d", connection.Host, connection.Port)
}

func lookupKnownHost(ctx context.Context, host, path string) ([]string, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	command := exec.CommandContext(ctx, "ssh-keygen", "-F", host, "-f", path)
	output, err := command.Output()
	if err != nil {
		// ssh-keygen exits 1 when the host is simply not present.
		if len(output) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 SSH known_hosts: %w", err)
	}
	lines := make([]string, 0)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, nil
}
