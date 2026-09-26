package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

type Status string

const (
	AuthMethodAgent      = "agent"
	AuthMethodPassword   = "password"
	AuthMethodPrivateKey = "private_key"
)

const (
	StatusUnknown      Status = "unknown"
	StatusChecking     Status = "checking"
	StatusHealthy      Status = "healthy"
	StatusAuthRequired Status = "auth_required"
	StatusDegraded     Status = "degraded"
	StatusUnreachable  Status = "unreachable"
)

type Connection struct {
	ID             string
	Name           string
	Host           string
	Port           int
	SSHUser        string
	AuthMethod     string
	PrivateKeyPath string
	CredentialRef  string
	Status         Status
	LastCheckedAt  *time.Time
	LastMessage    string
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type NewConnection struct {
	Name           string
	Host           string
	Port           int
	SSHUser        string
	AuthMethod     string
	PrivateKeyPath string
	CredentialRef  string
}

func ValidateAuthMethod(method, password, privateKeyPath string) error {
	if method == "" {
		method = AuthMethodAgent
	}
	switch method {
	case AuthMethodAgent:
		return nil
	case AuthMethodPassword:
		if password == "" {
			return errors.New("SSH 密码不能为空")
		}
		return nil
	case AuthMethodPrivateKey:
		privateKeyPath = strings.TrimSpace(privateKeyPath)
		if privateKeyPath == "" {
			return errors.New("请选择 SSH 私钥文件")
		}
		if strings.ContainsAny(privateKeyPath, "\r\n\x00") {
			return errors.New("SSH 私钥路径无效")
		}
		return nil
	default:
		return fmt.Errorf("unsupported SSH authentication method %q", method)
	}
}

func (input NewConnection) Validate() (NewConnection, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Host = strings.TrimSpace(input.Host)
	input.SSHUser = strings.TrimSpace(input.SSHUser)
	input.PrivateKeyPath = strings.TrimSpace(input.PrivateKeyPath)
	if input.AuthMethod == "" {
		input.AuthMethod = AuthMethodAgent
	}
	if input.Name == "" || len([]rune(input.Name)) > 80 {
		return input, errors.New("server name must be between 1 and 80 characters")
	}
	if input.Host == "" || len(input.Host) > 253 || strings.ContainsAny(input.Host, " \t\r\n/@:") {
		return input, errors.New("server host must be a hostname or IP address without a scheme")
	}
	if input.Port == 0 {
		input.Port = 22
	}
	if input.Port < 1 || input.Port > 65535 {
		return input, errors.New("SSH port must be between 1 and 65535")
	}
	if input.SSHUser == "" || strings.HasPrefix(input.SSHUser, "-") || len(input.SSHUser) > 64 || strings.ContainsAny(input.SSHUser, " \t\r\n@/:;\\") {
		return input, errors.New("SSH user contains unsupported characters")
	}
	if net.ParseIP(input.Host) == nil && strings.HasPrefix(input.Host, "-") {
		return input, errors.New("server host cannot start with a dash")
	}
	return input, nil
}

func newID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate server id: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
