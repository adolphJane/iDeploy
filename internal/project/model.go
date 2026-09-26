package project

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

type SourceKind string

const (
	SourceLocal SourceKind = "local"
	SourceGit   SourceKind = "git"
)

type RefKind string

const (
	RefBranch RefKind = "branch"
	RefTag    RefKind = "tag"
	RefCommit RefKind = "commit"
)

type Status string

const (
	StatusReady              Status = "ready"
	StatusNeedsConfiguration Status = "needs_configuration"
	StatusUnavailable        Status = "unavailable"
	StatusChecking           Status = "checking"
	StatusFailed             Status = "failed"
)

type Project struct {
	ID              string
	Name            string
	Description     string
	SourceKind      SourceKind
	LocalPath       string
	RepositoryURL   string
	RefKind         RefKind
	RefValue        string
	ResolvedCommit  string
	Status          Status
	LastCheckedAt   *time.Time
	LastError       string
	ContextPath     string
	DockerfilePath  string
	ComposePath     string
	ImageRepository string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Input struct {
	Name            string
	Description     string
	SourceKind      SourceKind
	LocalPath       string
	RepositoryURL   string
	RefKind         RefKind
	RefValue        string
	ContextPath     string
	DockerfilePath  string
	ComposePath     string
	ImageRepository string
}

func (input Input) Validate() (Input, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.LocalPath = strings.TrimSpace(input.LocalPath)
	input.RepositoryURL = strings.TrimSpace(input.RepositoryURL)
	input.RefValue = strings.TrimSpace(input.RefValue)
	input.ImageRepository = strings.TrimSpace(input.ImageRepository)

	if input.Name == "" || len([]rune(input.Name)) > 80 {
		return input, errors.New("project name must be between 1 and 80 characters")
	}
	if len([]rune(input.Description)) > 500 {
		return input, errors.New("project description must be at most 500 characters")
	}
	switch input.SourceKind {
	case SourceLocal:
		if input.LocalPath == "" || !filepath.IsAbs(input.LocalPath) {
			return input, errors.New("local project path must be an absolute path")
		}
		input.LocalPath = filepath.Clean(input.LocalPath)
		input.RepositoryURL, input.RefValue = "", ""
		input.RefKind = RefBranch
	case SourceGit:
		if !validGitURL(input.RepositoryURL) {
			return input, errors.New("git repository URL must use HTTPS, SSH, or git@host:path format")
		}
		if input.RefKind != RefBranch && input.RefKind != RefTag && input.RefKind != RefCommit {
			return input, errors.New("git reference must be a branch, tag, or commit")
		}
		if input.RefValue == "" || len(input.RefValue) > 255 || strings.ContainsAny(input.RefValue, "\n\r\t ") {
			return input, errors.New("git reference is required and cannot contain whitespace")
		}
		input.LocalPath = ""
	default:
		return input, fmt.Errorf("unsupported project source kind %q", input.SourceKind)
	}

	var err error
	if input.ContextPath, err = normalizeRelativePath(input.ContextPath, "."); err != nil {
		return input, fmt.Errorf("build context: %w", err)
	}
	if input.DockerfilePath, err = normalizeRelativePath(input.DockerfilePath, ""); err != nil {
		return input, fmt.Errorf("dockerfile: %w", err)
	}
	if input.ComposePath, err = normalizeRelativePath(input.ComposePath, ""); err != nil {
		return input, fmt.Errorf("compose file: %w", err)
	}
	return input, nil
}

func normalizeRelativePath(value, fallback string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	if filepath.IsAbs(value) {
		return "", errors.New("must be relative to the project root")
	}
	cleaned := filepath.ToSlash(filepath.Clean(value))
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("must remain inside the project root")
	}
	return cleaned, nil
}

func validGitURL(value string) bool {
	if value == "" || strings.ContainsAny(value, "\n\r\t ") {
		return false
	}
	if strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "ssh://") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Hostname() == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return false
		}
		if parsed.User != nil {
			if _, present := parsed.User.Password(); present || parsed.Scheme == "https" {
				return false
			}
		}
		return parsed.Path != "" && parsed.Path != "/"
	}
	return strings.HasPrefix(value, "git@") && strings.Contains(strings.TrimPrefix(value, "git@"), ":")
}

func newID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate project id: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func formatTime(value time.Time) string         { return value.UTC().Format(time.RFC3339Nano) }
func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }
