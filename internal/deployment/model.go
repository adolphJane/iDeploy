package deployment

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type DesiredState string

const (
	DesiredRunning DesiredState = "running"
	DesiredStopped DesiredState = "stopped"
)

type Target struct {
	WorkloadKind   string
	WorkloadName   string
	ID             string
	ProjectID      string
	ServerID       string
	DesiredState   DesiredState
	ContainerPorts []string
	RestartPolicy  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Input struct {
	WorkloadKind   string
	WorkloadName   string
	ProjectID      string
	ServerID       string
	ContainerPorts []string
	RestartPolicy  string
}

type ReleaseStatus string

const (
	ReleaseQueued    ReleaseStatus = "queued"
	ReleasePreparing ReleaseStatus = "preparing"
	ReleaseBuilding  ReleaseStatus = "building"
	ReleasePushing   ReleaseStatus = "pushing"
	ReleasePulling   ReleaseStatus = "pulling"
	ReleaseDeploying ReleaseStatus = "deploying"
	ReleaseChecking  ReleaseStatus = "checking"
	ReleaseSucceeded ReleaseStatus = "succeeded"
	ReleaseFailed    ReleaseStatus = "failed"
	ReleaseCanceled  ReleaseStatus = "canceled"
)

type Release struct {
	ID             string
	DeploymentID   string
	RegistryID     string
	JobID          string
	Action         string
	Version        string
	ImageRef       string
	SourceRevision string
	Manifest       string
	RollbackToID   string
	Status         ReleaseStatus
	Detail         string
	CreatedAt      time.Time
	FinishedAt     *time.Time
}

type ReleaseInput struct {
	DeploymentID string
	RegistryID   string
	Action       string
	Version      string
	ImageRef     string
	Manifest     string
	RollbackToID string
}

func (input Input) Validate() (Input, error) {
	input.WorkloadName = strings.TrimSpace(input.WorkloadName)
	if input.WorkloadKind != "container" && input.WorkloadKind != "compose" {
		return input, errors.New("请选择容器或 Compose 项目")
	}
	if input.WorkloadName == "" || len(input.WorkloadName) > 128 || strings.ContainsAny(input.WorkloadName, "\r\n\t") {
		return input, errors.New("请填写明确的远端工作负载名称")
	}
	if input.WorkloadKind == "container" && !containerNamePattern.MatchString(input.WorkloadName) {
		return input, errors.New("容器名称只能包含字母、数字、点、下划线和短横线")
	}
	if input.WorkloadKind == "compose" && !composeNamePattern.MatchString(input.WorkloadName) {
		return input, errors.New("Compose 项目名称必须以小写字母或数字开头，且只能包含小写字母、数字、短横线和下划线")
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.ServerID = strings.TrimSpace(input.ServerID)
	if input.ProjectID == "" || input.ServerID == "" {
		return input, errors.New("project and server are required")
	}
	if len(input.ProjectID) > 128 || len(input.ServerID) > 128 {
		return input, errors.New("project or server identifier is too long")
	}
	if input.RestartPolicy == "" {
		input.RestartPolicy = "unless-stopped"
	}
	switch input.RestartPolicy {
	case "no", "always", "unless-stopped", "on-failure":
	default:
		return input, errors.New("不支持的容器重启策略")
	}
	if input.WorkloadKind == "compose" {
		input.ContainerPorts = nil
	}
	if len(input.ContainerPorts) > 32 {
		return input, errors.New("端口映射不能超过 32 条")
	}
	for index, port := range input.ContainerPorts {
		port = strings.TrimSpace(port)
		if !portPattern.MatchString(port) {
			return input, fmt.Errorf("端口映射 %q 格式无效，请使用外部端口 8080，或完整映射 8080:80 / 127.0.0.1:8080:80", port)
		}
		input.ContainerPorts[index] = port
	}
	return input, nil
}

var portPattern = regexp.MustCompile(`^(?:(?:(?:\d{1,3}\.){3}\d{1,3}:)?\d{1,5}:\d{1,5}|\d{1,5})(?:/(?:tcp|udp))?$`)
var containerNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
var composeNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)

func newID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate deployment target id: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func formatTime(value time.Time) string         { return value.UTC().Format(time.RFC3339Nano) }
func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }
