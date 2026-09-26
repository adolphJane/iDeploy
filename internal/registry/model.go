package registry

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type Kind string

const (
	KindHarbor Kind = "harbor"
	KindOCI    Kind = "oci"
)

type Status string

const (
	StatusUnknown      Status = "unknown"
	StatusChecking     Status = "checking"
	StatusHealthy      Status = "healthy"
	StatusAuthRequired Status = "auth_required"
	StatusDegraded     Status = "degraded"
	StatusUnreachable  Status = "unreachable"
)

type Connection struct {
	ID            string
	Name          string
	Kind          Kind
	Endpoint      string
	CredentialRef string
	Username      string
	Password      string
	Status        Status
	LastCheckedAt *time.Time
	LastMessage   string
	LastError     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type NewConnection struct {
	Name     string
	Kind     Kind
	Endpoint string
	Username string
}

func (input NewConnection) Validate() (NewConnection, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Username = strings.TrimSpace(input.Username)
	if input.Name == "" {
		return input, errors.New("registry name is required")
	}
	if len([]rune(input.Name)) > 80 {
		return input, errors.New("registry name must be at most 80 characters")
	}
	if len([]rune(input.Username)) > 255 || strings.ContainsAny(input.Username, "\r\n") {
		return input, errors.New("registry username is invalid")
	}
	if input.Kind != KindHarbor && input.Kind != KindOCI {
		return input, fmt.Errorf("unsupported registry kind %q", input.Kind)
	}

	endpoint, err := NormalizeEndpoint(input.Endpoint)
	if err != nil {
		return input, err
	}
	input.Endpoint = endpoint
	return input, nil
}

func DockerHost(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	}
	return parsed.Host
}

// NormalizeEndpoint accepts a registry base URL without credentials or paths.
// A scheme is optional and defaults to HTTPS, which is the safe default for a
// registry connection. HTTP remains available for local development registries.
func NormalizeEndpoint(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("registry endpoint is required")
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("parse registry endpoint: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", errors.New("registry endpoint must use http or https")
	}
	if parsed.Hostname() == "" {
		return "", errors.New("registry endpoint must include a host")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("registry endpoint cannot include credentials, query, or fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("registry endpoint must not include a path")
	}

	parsed.Path = ""
	parsed.RawPath = ""
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func newID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate registry id: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
