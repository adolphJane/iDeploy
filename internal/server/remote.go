package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Container struct {
	ComposeProject string `json:"composeProject"`
	Health         string `json:"health"`
	OneOff         bool   `json:"oneOff"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	Image          string `json:"image"`
	State          string `json:"state"`
	Status         string `json:"status"`
	Ports          string `json:"ports"`
	Created        string `json:"created"`
}

type ProbeResult struct {
	Status Status
	Detail string
}

type ContainerReplacement struct {
	Name        string
	BackupName  string
	HadPrevious bool
}

type Remote struct {
	command func(context.Context, string, ...string) *exec.Cmd
}

var environmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

func NewRemote() *Remote {
	return &Remote{command: exec.CommandContext}
}

func (r *Remote) Probe(ctx context.Context, connection Connection) ProbeResult {
	output, err := r.run(ctx, connection, "printf 'IDEPLY_OS='; uname -s; printf ' IDEPLY_ARCH='; uname -m; printf ' IDEPLY_DOCKER='; docker version --format '{{.Server.Version}}'")
	if err != nil {
		return probeFailure(connection, err)
	}
	return ProbeResult{Status: StatusHealthy, Detail: strings.TrimSpace(output)}
}

// DockerPlatform queries the daemon, which may differ from the SSH host.
func (r *Remote) DockerPlatform(ctx context.Context, connection Connection) (string, error) {
	output, err := r.run(ctx, connection, "docker version --format '{{.Server.Os}}/{{.Server.Arch}}'")
	if err != nil {
		return "", errors.New(connectionFailureDetail(connection, err))
	}
	return strings.TrimSpace(output), nil
}

// TestConnection verifies only SSH reachability and authentication. It uses
// OpenSSH's trust-on-first-use policy: an unknown host key is stored, while a
// changed key is rejected. It never invokes Docker. A non-empty secret is
// either a password or private-key passphrase and is never persisted here.
func (r *Remote) TestConnection(ctx context.Context, connection Connection, secret string) ProbeResult {
	output, err := r.runTimeoutWithSecret(ctx, connection, "printf 'IDEPLOY_OS='; uname -s; printf ' IDEPLOY_ARCH='; uname -m", 12*time.Second, secret, true)
	if err != nil {
		return probeFailure(connection, err)
	}
	detail := "SSH 连接成功"
	cleaned := strings.TrimSpace(output)
	if marker := strings.Index(cleaned, "IDEPLOY_OS="); marker >= 0 {
		if strings.Contains(strings.ToLower(cleaned[:marker]), "permanently added") {
			detail += " · 已自动信任首次连接主机指纹"
		}
		cleaned = strings.TrimSpace(cleaned[marker:])
	}
	if cleaned != "" {
		detail += " · " + cleaned
	}
	return ProbeResult{Status: StatusHealthy, Detail: detail}
}

func (r *Remote) ListContainers(ctx context.Context, connection Connection) ([]Container, error) {
	output, err := r.run(ctx, connection, "docker ps -a --no-trunc --format '{{json .}}'")
	if err != nil {
		return nil, err
	}
	containers := make([]Container, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	for scanner.Scan() {
		var raw struct {
			ID      string `json:"ID"`
			Names   string `json:"Names"`
			Image   string `json:"Image"`
			State   string `json:"State"`
			Status  string `json:"Status"`
			Ports   string `json:"Ports"`
			Created string `json:"CreatedAt"`
			Labels  string `json:"Labels"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			return nil, fmt.Errorf("parse remote container list: %w", err)
		}
		labels := map[string]string{}
		for _, label := range strings.Split(raw.Labels, ",") {
			key, value, ok := strings.Cut(label, "=")
			if ok {
				labels[key] = value
			}
		}
		health := "none"
		if strings.Contains(raw.Status, "(unhealthy)") {
			health = "unhealthy"
		} else if strings.Contains(raw.Status, "(healthy)") {
			health = "healthy"
		} else if strings.Contains(raw.Status, "health: starting") {
			health = "starting"
		}
		containers = append(containers, Container{ID: raw.ID, Name: raw.Names, Image: raw.Image, State: raw.State, Status: raw.Status, Ports: raw.Ports, Created: raw.Created, ComposeProject: labels["com.docker.compose.project"], OneOff: strings.EqualFold(labels["com.docker.compose.oneoff"], "true"), Health: health})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read remote container list: %w", err)
	}
	return containers, nil
}

func (r *Remote) ContainerLogs(ctx context.Context, connection Connection, containerID string, tail int) (string, error) {
	if !validContainerID(containerID) {
		return "", errors.New("invalid container identifier")
	}
	if tail <= 0 || tail > 2000 {
		return "", errors.New("invalid log tail")
	}
	output, err := r.runTimeout(ctx, connection, "docker logs --timestamps --tail "+fmt.Sprint(tail)+" "+shellQuote(containerID)+" 2>&1", 30*time.Second)
	if err != nil {
		return "", err
	}
	if len(output) > 2*1024*1024 {
		output = output[len(output)-2*1024*1024:]
	}
	return output, nil
}

func (r *Remote) OperateContainer(ctx context.Context, connection Connection, action, containerID string) error {
	if !validContainerID(containerID) {
		return errors.New("invalid container identifier")
	}
	var command string
	switch action {
	case "start":
		command = "docker " + action + " " + containerID
	case "stop", "restart":
		command = "docker " + action + " --time 10 " + containerID
	default:
		return fmt.Errorf("unsupported container action %q", action)
	}
	_, err := r.runTimeout(ctx, connection, command, 45*time.Second)
	return err
}

func (r *Remote) PullImage(ctx context.Context, connection Connection, image string) error {
	if err := validateImageReference(image); err != nil {
		return err
	}
	_, err := r.runTimeout(ctx, connection, "docker pull "+shellQuote(image), 10*time.Minute)
	return err
}

// TagImage creates a server-local, human-readable alias for an image that was
// pulled by digest. It verifies that the alias resolves to the exact same local
// image ID and never pushes or mutates the registry tag.
func (r *Remote) TagImage(ctx context.Context, connection Connection, source, target string) error {
	if err := validateImageReference(source); err != nil {
		return err
	}
	if err := validateImageReference(target); err != nil {
		return err
	}
	command := "source_id=$(docker image inspect --format '{{.Id}}' " + shellQuote(source) + ") || exit $?; " +
		"docker image tag \"$source_id\" " + shellQuote(target) + " || exit $?; " +
		"target_id=$(docker image inspect --format '{{.Id}}' " + shellQuote(target) + ") || exit $?; " +
		"if [ \"$source_id\" != \"$target_id\" ]; then printf 'local image tag verification failed\\n' >&2; exit 1; fi"
	_, err := r.runTimeout(ctx, connection, command, 45*time.Second)
	if err != nil {
		return fmt.Errorf("create verified local image tag: %w", err)
	}
	return nil
}

func (r *Remote) LoginRegistry(ctx context.Context, connection Connection, registryHost, username, password string) error {
	if !registryHostPattern.MatchString(registryHost) || username == "" || password == "" || strings.ContainsAny(username, "\r\n") {
		return errors.New("invalid registry credentials")
	}
	command := "docker login " + shellQuote(registryHost) + " --username " + shellQuote(username) + " --password-stdin"
	_, err := r.runTimeoutWithInput(ctx, connection, command, 45*time.Second, password+"\n")
	if err != nil {
		return errors.New(connectionFailureDetail(connection, err))
	}
	return nil
}

// DeployContainer stages a replacement while preserving the previous
// container under a temporary backup name. Call CommitContainerReplacement
// only after health checks pass, or RollbackContainerReplacement on failure.
func (r *Remote) DeployContainer(ctx context.Context, connection Connection, name, image, restartPolicy string, ports []string, labels map[string]string, environment map[string]string) (ContainerReplacement, error) {
	if !validContainerID(name) {
		return ContainerReplacement{}, errors.New("invalid container name")
	}
	if err := validateImageReference(image); err != nil {
		return ContainerReplacement{}, err
	}
	switch restartPolicy {
	case "no", "always", "unless-stopped", "on-failure":
	default:
		return ContainerReplacement{}, errors.New("invalid restart policy")
	}
	suffix := fmt.Sprintf(".ideploy-%d", time.Now().UnixNano())
	backupBase := name
	if len(backupBase)+len(suffix) > 128 {
		backupBase = backupBase[:128-len(suffix)]
	}
	backupName := backupBase + suffix
	args := []string{"docker run -d", "--restart " + shellQuote(restartPolicy), "--name " + shellQuote(name)}
	environmentInput, err := dockerEnvironmentFile(environment)
	if err != nil {
		return ContainerReplacement{}, err
	}
	if len(environment) > 0 {
		args = append(args, `--env-file "$env_file"`)
	}
	labelKeys := make([]string, 0, len(labels))
	for key, value := range labels {
		if !dockerLabelKeyPattern.MatchString(key) || len(key) > 128 || value == "" || len(value) > 1024 || strings.ContainsAny(value, "\r\n") {
			return ContainerReplacement{}, fmt.Errorf("invalid container label %q", key)
		}
		labelKeys = append(labelKeys, key)
	}
	sort.Strings(labelKeys)
	for _, key := range labelKeys {
		args = append(args, "--label "+shellQuote(key+"="+labels[key]))
	}
	for _, port := range ports {
		if !deploymentPortPattern.MatchString(port) {
			return ContainerReplacement{}, fmt.Errorf("invalid port mapping %q", port)
		}
		args = append(args, "--publish "+shellQuote(port))
	}
	args = append(args, shellQuote(image))
	command := "had_previous=0; "
	if len(environment) > 0 {
		command = `umask 077; env_file=$(mktemp "${TMPDIR:-/tmp}/ideploy-env.XXXXXX") || exit $?; trap 'rm -f "$env_file"' EXIT HUP INT TERM; cat > "$env_file" || exit $?; ` + command
	}
	command +=
		"if docker container inspect " + shellQuote(name) + " >/dev/null 2>&1; then " +
			"docker rename " + shellQuote(name) + " " + shellQuote(backupName) + " || exit $?; had_previous=1; " +
			"if ! docker stop --time 10 " + shellQuote(backupName) + " >/dev/null; then " +
			"docker rename " + shellQuote(backupName) + " " + shellQuote(name) + " >/dev/null 2>&1 || true; exit 1; fi; fi; " +
			"if output=$(" + strings.Join(args, " ") + "); then " +
			"printf '%s\\nIDEPLOY_PREVIOUS=%s\\n' \"$output\" \"$had_previous\"; " +
			"else status=$?; docker rm -f " + shellQuote(name) + " >/dev/null 2>&1 || true; " +
			"if [ \"$had_previous\" = 1 ]; then docker rename " + shellQuote(backupName) + " " + shellQuote(name) + " && docker start " + shellQuote(name) + " >/dev/null; fi; exit \"$status\"; fi"
	var output string
	if len(environment) > 0 {
		output, err = r.runTimeoutWithInput(ctx, connection, command, 3*time.Minute, environmentInput)
	} else {
		output, err = r.runTimeout(ctx, connection, command, 3*time.Minute)
	}
	if err != nil {
		return ContainerReplacement{}, err
	}
	return ContainerReplacement{Name: name, BackupName: backupName, HadPrevious: strings.Contains(output, "IDEPLOY_PREVIOUS=1")}, nil
}

func dockerEnvironmentFile(environment map[string]string) (string, error) {
	if len(environment) > 128 {
		return "", errors.New("too many container environment variables")
	}
	keys := make([]string, 0, len(environment))
	for key, value := range environment {
		if !environmentNamePattern.MatchString(key) {
			return "", fmt.Errorf("invalid environment variable name %q", key)
		}
		if len(value) > 65536 || strings.ContainsAny(value, "\x00\r\n") {
			return "", fmt.Errorf("invalid environment variable value for %s", key)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var content strings.Builder
	for _, key := range keys {
		content.WriteString(key)
		content.WriteByte('=')
		content.WriteString(environment[key])
		content.WriteByte('\n')
	}
	return content.String(), nil
}

func (r *Remote) CommitContainerReplacement(ctx context.Context, connection Connection, replacement ContainerReplacement) error {
	if !validContainerID(replacement.Name) || !validContainerID(replacement.BackupName) {
		return errors.New("invalid container replacement")
	}
	if !replacement.HadPrevious {
		return nil
	}
	_, err := r.runTimeout(ctx, connection, "docker rm -f "+shellQuote(replacement.BackupName)+" >/dev/null", 45*time.Second)
	return err
}

func (r *Remote) RollbackContainerReplacement(ctx context.Context, connection Connection, replacement ContainerReplacement) error {
	if !validContainerID(replacement.Name) || !validContainerID(replacement.BackupName) {
		return errors.New("invalid container replacement")
	}
	command := "docker rm -f " + shellQuote(replacement.Name) + " >/dev/null 2>&1 || true"
	if replacement.HadPrevious {
		command += "; docker rename " + shellQuote(replacement.BackupName) + " " + shellQuote(replacement.Name) + " && docker start " + shellQuote(replacement.Name) + " >/dev/null"
	}
	_, err := r.runTimeout(ctx, connection, command, 60*time.Second)
	return err
}

func (r *Remote) DeployCompose(ctx context.Context, connection Connection, deploymentID, projectName, version, manifest string) error {
	if err := r.UploadCompose(ctx, connection, deploymentID, manifest); err != nil {
		return err
	}
	if err := r.PullCompose(ctx, connection, deploymentID, projectName, version); err != nil {
		return err
	}
	return r.UpCompose(ctx, connection, deploymentID, projectName, version)
}

func (r *Remote) UploadCompose(ctx context.Context, connection Connection, deploymentID, manifest string) error {
	if !validContainerID(deploymentID) {
		return errors.New("invalid compose deployment identifier")
	}
	remoteDir := ".ideploy/deployments/" + deploymentID
	remoteFile := remoteDir + "/compose.yml"
	if _, err := r.runTimeout(ctx, connection, "mkdir -p "+shellQuote(remoteDir), 30*time.Second); err != nil {
		return err
	}
	if _, err := r.runTimeoutWithInput(ctx, connection, "umask 077; cat > "+shellQuote(remoteFile), 30*time.Second, manifest); err != nil {
		return fmt.Errorf("upload compose manifest: %w", err)
	}
	return nil
}

func (r *Remote) PullCompose(ctx context.Context, connection Connection, deploymentID, projectName, version string) error {
	command, err := composeCommand(deploymentID, projectName, version)
	if err != nil {
		return err
	}
	_, err = r.runTimeout(ctx, connection, command+" pull", 10*time.Minute)
	return err
}

func (r *Remote) UpCompose(ctx context.Context, connection Connection, deploymentID, projectName, version string) error {
	command, err := composeCommand(deploymentID, projectName, version)
	if err != nil {
		return err
	}
	_, err = r.runTimeout(ctx, connection, command+" up -d --remove-orphans", 5*time.Minute)
	return err
}

func composeCommand(deploymentID, projectName, version string) (string, error) {
	if !validContainerID(deploymentID) || !validContainerID(projectName) || !safeVersionPattern.MatchString(version) {
		return "", errors.New("invalid compose deployment identifier")
	}
	remoteFile := ".ideploy/deployments/" + deploymentID + "/compose.yml"
	prefix := "IMAGE_TAG=" + shellQuote(version) + " docker compose --project-name " + shellQuote(projectName) + " -f " + shellQuote(remoteFile)
	return prefix, nil
}

func (r *Remote) run(ctx context.Context, connection Connection, remoteCommand string) (string, error) {
	return r.runTimeout(ctx, connection, remoteCommand, 12*time.Second)
}

func (r *Remote) runTimeout(ctx context.Context, connection Connection, remoteCommand string, timeout time.Duration) (string, error) {
	return r.runTimeoutWithSecret(ctx, connection, remoteCommand, timeout, "", false)
}

func (r *Remote) runTimeoutWithSecret(ctx context.Context, connection Connection, remoteCommand string, timeout time.Duration, secretOverride string, trustOnFirstUse bool) (string, error) {
	return r.runTimeoutWithSecretAndInput(ctx, connection, remoteCommand, timeout, secretOverride, trustOnFirstUse, "")
}

func (r *Remote) runTimeoutWithInput(ctx context.Context, connection Connection, remoteCommand string, timeout time.Duration, input string) (string, error) {
	return r.runTimeoutWithSecretAndInput(ctx, connection, remoteCommand, timeout, "", false, input)
}

func (r *Remote) runTimeoutWithSecretAndInput(ctx context.Context, connection Connection, remoteCommand string, timeout time.Duration, secretOverride string, trustOnFirstUse bool, input string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	authMethod := connection.AuthMethod
	if authMethod == "" {
		if connection.CredentialRef != "" {
			authMethod = AuthMethodPassword
		} else {
			authMethod = AuthMethodAgent
		}
	}
	hostKeyPolicy := "yes"
	if trustOnFirstUse {
		hostKeyPolicy = "accept-new"
	}
	args := []string{"-o", "ConnectTimeout=8", "-o", "ConnectionAttempts=1", "-o", "StrictHostKeyChecking=" + hostKeyPolicy}
	switch authMethod {
	case AuthMethodAgent:
		args = append(args, "-o", "BatchMode=yes")
	case AuthMethodPassword:
		args = append(args,
			"-o", "BatchMode=no",
			"-o", "PreferredAuthentications=password,keyboard-interactive",
			"-o", "PubkeyAuthentication=no",
			"-o", "PasswordAuthentication=yes",
			"-o", "KbdInteractiveAuthentication=yes",
			"-o", "NumberOfPasswordPrompts=1",
		)
	case AuthMethodPrivateKey:
		args = append(args,
			"-i", connection.PrivateKeyPath,
			"-o", "IdentitiesOnly=yes",
			"-o", "PreferredAuthentications=publickey",
			"-o", "NumberOfPasswordPrompts=1",
		)
		if secretOverride == "" && connection.CredentialRef == "" {
			args = append(args, "-o", "BatchMode=yes")
		} else {
			args = append(args, "-o", "BatchMode=no")
		}
	default:
		return "", fmt.Errorf("unsupported SSH authentication method %q", authMethod)
	}
	args = append(args, "-p", fmt.Sprint(connection.Port), connection.SSHUser+"@"+connection.Host, remoteCommand)
	runCommand := func(env []string) (string, error) {
		command := r.command(ctx, "ssh", args...)
		command.Env = env
		if input != "" {
			command.Stdin = strings.NewReader(input)
		}
		output, err := command.CombinedOutput()
		return string(output), err
	}
	var output string
	var err error
	if secretOverride != "" {
		output, err = withPasswordAskpass(secretOverride, func(askpass *os.File) (string, error) {
			return runCommand(append(os.Environ(), passwordEnvironment(askpass)...))
		})
	} else if connection.CredentialRef == "" {
		output, err = runCommand(os.Environ())
	} else {
		var secret string
		secret, err = LoadPassword(ctx, connection.CredentialRef)
		if err == nil {
			output, err = withPasswordAskpass(secret, func(askpass *os.File) (string, error) {
				return runCommand(append(os.Environ(), passwordEnvironment(askpass)...))
			})
		}
	}
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", errors.New("SSH 连接超时，请检查地址、端口和服务器网络")
		}
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return "", errors.New(detail)
	}
	return string(output), nil
}

func probeFailure(connection Connection, err error) ProbeResult {
	detail := connectionFailureDetail(connection, err)
	lower := strings.ToLower(detail)
	if strings.Contains(lower, "remote host identification has changed") {
		return ProbeResult{Status: StatusAuthRequired, Detail: fmt.Sprintf("SSH 主机指纹与本机记录不一致，应用不会自动覆盖。请先从可信渠道核对 %s:%d 的指纹并处理旧记录。原始错误: %s", connection.Host, connection.Port, detail)}
	}
	if strings.Contains(lower, "host key verification failed") {
		return ProbeResult{Status: StatusAuthRequired, Detail: fmt.Sprintf("SSH 主机指纹未确认或已变化。请在服务器页面获取并核对 %s:%d 的主机指纹，确认信任后再重试。原始错误: %s", connection.Host, connection.Port, detail)}
	}
	if errors.Is(err, ErrCredentialUnavailable) || strings.Contains(lower, "permission denied") || strings.Contains(lower, "authentication failed") {
		return ProbeResult{Status: StatusAuthRequired, Detail: "SSH 认证失败，请确认用户名和所选登录方式的凭据；原始错误: " + detail}
	}
	if strings.Contains(lower, "ssh 连接超时") || strings.Contains(lower, "no route to host") || strings.Contains(lower, "connection timed out") || strings.Contains(lower, "could not resolve hostname") || strings.Contains(lower, "connection refused") {
		return ProbeResult{Status: StatusUnreachable, Detail: detail}
	}
	return ProbeResult{Status: StatusDegraded, Detail: detail}
}

func connectionFailureDetail(connection Connection, err error) string {
	detail := err.Error()
	if strings.Contains(strings.ToLower(detail), "timed out during banner exchange") {
		return fmt.Sprintf("TCP 已连接到 %s:%d，但 SSH 服务未及时返回握手信息。请检查 sshd 是否正常监听、端口是否映射到 SSH，以及防火墙、Fail2ban 或 MaxStartups 限制。原始错误: %s", connection.Host, connection.Port, detail)
	}
	return detail
}

var containerIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
var deploymentPortPattern = regexp.MustCompile(`^(?:(?:\d{1,3}\.){3}\d{1,3}:)?\d{1,5}:\d{1,5}(?:/(?:tcp|udp))?$`)
var imageReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]{0,511}$`)
var dockerLabelKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var safeVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
var registryHostPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:-]{0,254}$`)

func validContainerID(value string) bool { return containerIDPattern.MatchString(value) }

func validateImageReference(value string) error {
	if !imageReferencePattern.MatchString(value) || strings.Contains(value, "..") {
		return errors.New("invalid image reference")
	}
	return nil
}
