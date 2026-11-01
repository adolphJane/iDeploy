package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var operationIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var containerIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
var imageReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]{0,511}$`)

type OperationRequest struct {
	ID             string            `json:"id"`
	Kind           string            `json:"kind"`
	ContainerID    string            `json:"containerId"`
	Image          string            `json:"image,omitempty"`
	TargetImage    string            `json:"targetImage,omitempty"`
	RestartPolicy  string            `json:"restartPolicy,omitempty"`
	Ports          []string          `json:"ports,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	Environment    map[string]string `json:"environment,omitempty"`
	BackupName     string            `json:"backupName,omitempty"`
	HadPrevious    bool              `json:"hadPrevious,omitempty"`
	DeploymentID   string            `json:"deploymentId,omitempty"`
	ProjectName    string            `json:"projectName,omitempty"`
	Version        string            `json:"version,omitempty"`
	Manifest       string            `json:"manifest,omitempty"`
	NginxContainer string            `json:"nginxContainer,omitempty"`
	NginxConfig    string            `json:"nginxConfig,omitempty"`
	ExpectedHash   string            `json:"expectedHash,omitempty"`
	FQDN           string            `json:"fqdn,omitempty"`
	Email          string            `json:"email,omitempty"`
	WebrootHost    string            `json:"webrootHost,omitempty"`
}

type Operation struct {
	OperationRequest
	RequestHash string          `json:"requestHash"`
	Status      string          `json:"status"`
	Error       string          `json:"error,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	CreatedAt   string          `json:"createdAt"`
	UpdatedAt   string          `json:"updatedAt"`
}

type OperationStore struct {
	dir    string
	mu     sync.Mutex
	active map[string]string
}

func NewOperationStore(dir string) (*OperationStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	return &OperationStore{dir: dir, active: map[string]string{}}, nil
}

func validateOperation(req OperationRequest) error {
	if !operationIDPattern.MatchString(req.ID) {
		return errors.New("invalid operation identifier")
	}
	switch req.Kind {
	case "container.start", "container.stop", "container.restart":
		if !containerIDPattern.MatchString(req.ContainerID) || req.Image != "" || req.TargetImage != "" {
			return errors.New("invalid container control request")
		}
	case "image.pull":
		if !validImage(req.Image) || req.ContainerID != "" || req.TargetImage != "" {
			return errors.New("invalid image pull request")
		}
	case "image.tag":
		if !validImage(req.Image) || !validImage(req.TargetImage) || req.ContainerID != "" {
			return errors.New("invalid image tag request")
		}
	case "container.deploy":
		if !containerIDPattern.MatchString(req.ContainerID) || !validImage(req.Image) || !validRestart(req.RestartPolicy) || len(req.Ports) > 64 || len(req.Labels) > 128 || len(req.Environment) > 128 {
			return errors.New("invalid container deployment request")
		}
		for _, port := range req.Ports {
			if !deploymentPortPattern.MatchString(port) {
				return errors.New("invalid port mapping")
			}
		}
		for key, value := range req.Labels {
			if !labelKeyPattern.MatchString(key) || len(key) > 128 || value == "" || len(value) > 1024 || strings.ContainsAny(value, "\r\n") {
				return errors.New("invalid container label")
			}
		}
		for key, value := range req.Environment {
			if !environmentNamePattern.MatchString(key) || len(value) > 65536 || strings.ContainsAny(value, "\x00\r\n") {
				return errors.New("invalid container environment")
			}
		}
	case "container.commit", "container.rollback":
		if !containerIDPattern.MatchString(req.ContainerID) || !containerIDPattern.MatchString(req.BackupName) {
			return errors.New("invalid container replacement")
		}
	case "compose.upload":
		if !containerIDPattern.MatchString(req.DeploymentID) || req.Manifest == "" || len(req.Manifest) > 900*1024 {
			return errors.New("invalid compose manifest")
		}
	case "compose.pull", "compose.up":
		if !containerIDPattern.MatchString(req.DeploymentID) || !containerIDPattern.MatchString(req.ProjectName) || !containerIDPattern.MatchString(req.Version) {
			return errors.New("invalid compose operation")
		}
	case "nginx.apply":
		if !containerIDPattern.MatchString(req.NginxContainer) || len(req.NginxConfig) > 256*1024 || len(req.NginxConfig) == 0 || !sha256HexPattern.MatchString(req.ExpectedHash) {
			return errors.New("invalid Nginx apply request")
		}
	case "certbot.issue":
		if !validateFQDN(req.FQDN) || req.Email != "" && !emailPattern.MatchString(req.Email) || !containerIDPattern.MatchString(req.NginxContainer) || !filepath.IsAbs(req.WebrootHost) || len(req.WebrootHost) > 512 {
			return errors.New("invalid Certbot issue request")
		}
	case "certbot.renew":
		if req.FQDN != "" || req.Email != "" || req.WebrootHost != "" {
			return errors.New("invalid Certbot renew request")
		}
	default:
		return errors.New("unsupported operation")
	}
	return nil
}

func validImage(value string) bool {
	return imageReferencePattern.MatchString(value) && !strings.Contains(value, "..")
}

var deploymentPortPattern = regexp.MustCompile(`^(?:(?:\d{1,3}\.){3}\d{1,3}:)?\d{1,5}:\d{1,5}(?:/(?:tcp|udp))?$`)
var labelKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var environmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
var sha256HexPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validRestart(value string) bool {
	return value == "no" || value == "always" || value == "unless-stopped" || value == "on-failure"
}

func resourceKey(req OperationRequest) string {
	if req.Kind == "certbot.issue" || req.Kind == "certbot.renew" {
		return "certbot"
	}
	if req.NginxContainer != "" {
		return "nginx:" + req.NginxContainer
	}
	if req.DeploymentID != "" {
		return "compose:" + req.DeploymentID
	}
	if req.ContainerID != "" {
		return "container:" + req.ContainerID
	}
	if req.TargetImage != "" {
		return "image:" + req.TargetImage
	}
	return "image:" + req.Image
}

func (s *OperationStore) path(id string) string { return filepath.Join(s.dir, id+".json") }

func (s *OperationStore) read(id string) (Operation, error) {
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		return Operation{}, err
	}
	var op Operation
	if err := json.Unmarshal(data, &op); err != nil {
		return Operation{}, err
	}
	return op, nil
}

func (s *OperationStore) write(op Operation) error {
	data, err := json.Marshal(op)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".operation-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path(op.ID))
}

func (s *OperationStore) Get(id string) (Operation, error) {
	if !operationIDPattern.MatchString(id) {
		return Operation{}, errors.New("invalid operation identifier")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	op, err := s.read(id)
	if err != nil {
		return Operation{}, err
	}
	// A process restart cannot safely replay a possibly completed Docker command.
	if op.Status == "running" && s.active[resourceKey(op.OperationRequest)] != op.ID {
		op.Status, op.Error = "uncertain", "Agent restarted while operation was running; inspect container state before retrying"
		op.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.write(op); err != nil {
			return Operation{}, err
		}
	}
	return op, nil
}

func (s *OperationStore) Create(req OperationRequest) (Operation, error) {
	if err := validateOperation(req); err != nil {
		return Operation{}, err
	}
	requestData, _ := json.Marshal(req)
	hash := sha256.Sum256(requestData)
	requestHash := hex.EncodeToString(hash[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, err := s.read(req.ID); err == nil {
		if old.RequestHash != requestHash {
			return Operation{}, errors.New("operation ID reused with different request")
		}
		if old.Status == "running" && s.active[resourceKey(old.OperationRequest)] != old.ID {
			old.Status, old.Error = "uncertain", "Agent restarted while operation was running; inspect container state before retrying"
			old.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			if err := s.write(old); err != nil {
				return Operation{}, err
			}
		}
		return old, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Operation{}, err
	}
	if current := s.active[resourceKey(req)]; current != "" {
		return Operation{}, fmt.Errorf("resource has active operation %s", current)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	sanitized := req
	sanitized.Environment = nil
	sanitized.Manifest = ""
	sanitized.NginxConfig = ""
	op := Operation{OperationRequest: sanitized, RequestHash: requestHash, Status: "running", CreatedAt: now, UpdatedAt: now}
	if err := s.write(op); err != nil {
		return Operation{}, err
	}
	s.active[resourceKey(req)] = req.ID
	go s.execute(req, op)
	return op, nil
}

func (s *OperationStore) execute(req OperationRequest, op Operation) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	var output []byte
	var err error
	switch op.Kind {
	case "container.start", "container.stop", "container.restart":
		action := op.Kind[len("container."):]
		args := []string{action}
		if action == "stop" || action == "restart" {
			args = append(args, "--time", "10")
		}
		args = append(args, op.ContainerID)
		output, err = exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	case "image.pull":
		output, err = exec.CommandContext(ctx, "docker", "pull", op.Image).CombinedOutput()
	case "image.tag":
		var source, target []byte
		source, err = exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", op.Image).Output()
		if err == nil {
			output, err = exec.CommandContext(ctx, "docker", "image", "tag", strings.TrimSpace(string(source)), op.TargetImage).CombinedOutput()
		}
		if err == nil {
			target, err = exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", op.TargetImage).Output()
		}
		if err == nil && strings.TrimSpace(string(source)) != strings.TrimSpace(string(target)) {
			err = errors.New("local image tag verification failed")
		}
	case "container.deploy":
		var replacement ContainerReplacement
		replacement, err = deployContainer(ctx, req)
		if err == nil {
			op.Result, _ = json.Marshal(replacement)
		}
	case "container.commit":
		err = commitContainer(ctx, req)
	case "container.rollback":
		err = rollbackContainer(ctx, req)
	case "compose.upload":
		err = uploadCompose(req)
	case "compose.pull", "compose.up":
		output, err = runCompose(ctx, req)
	case "nginx.apply":
		var hash string
		hash, err = applyNginx(ctx, req)
		if err == nil {
			op.Result, _ = json.Marshal(hash)
		}
	case "certbot.issue":
		var cert CertificateInfo
		cert, err = issueCertificate(ctx, req)
		if err == nil {
			op.Result, _ = json.Marshal(cert)
		}
	case "certbot.renew":
		_, err = exec.CommandContext(ctx, "certbot", "renew", "--non-interactive", "--quiet").CombinedOutput()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.active, resourceKey(op.OperationRequest))
	current, readErr := s.read(op.ID)
	if readErr != nil {
		return
	}
	if err != nil {
		current.Status = "failed"
		if op.Kind == "container.deploy" || op.Kind == "compose.upload" || op.Kind == "nginx.apply" || strings.HasPrefix(op.Kind, "certbot.") {
			current.Error = fmt.Sprintf("%s failed: %v", op.Kind, err)
		} else {
			current.Error = fmt.Sprintf("%s failed: %v: %.300s", op.Kind, err, output)
		}
	} else {
		current.Status = "succeeded"
		current.Result = op.Result
	}
	current.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_ = s.write(current)
}
