package server

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestProbeFailureExplainsHostKeyVerification(t *testing.T) {
	result := probeFailure(Connection{Host: "example.com", Port: 2222, SSHUser: "deploy"}, errString("Host key verification failed."))
	if result.Status != StatusAuthRequired {
		t.Fatalf("expected auth-required status, got %s", result.Status)
	}
	if result.Detail == "" || !containsAll(result.Detail, "example.com:2222", "服务器页面", "主机指纹") {
		t.Fatalf("expected actionable host-key detail, got %q", result.Detail)
	}
}

func TestProbeFailureKeepsNetworkStatus(t *testing.T) {
	result := probeFailure(Connection{Host: "example.com", Port: 22, SSHUser: "deploy"}, errString("Connection timed out"))
	if result.Status != StatusUnreachable {
		t.Fatalf("expected unreachable status, got %s", result.Status)
	}
}

func TestShellQuoteProtectsPassword(t *testing.T) {
	if got := shellQuote("p'a\nss"); got != "'p'\\''a\nss'" {
		t.Fatalf("unexpected shell quote: %q", got)
	}
}

func TestConnectionDoesNotInvokeDocker(t *testing.T) {
	var arguments []string
	remote := &Remote{command: func(ctx context.Context, name string, args ...string) *exec.Cmd {
		arguments = append(arguments, args...)
		return exec.CommandContext(ctx, "sh", "-c", "printf 'IDEPLOY_OS=Darwin IDEPLOY_ARCH=arm64'")
	}}
	result := remote.TestConnection(context.Background(), Connection{Host: "example.com", Port: 22, SSHUser: "deploy"}, "")
	if result.Status != StatusHealthy {
		t.Fatalf("expected healthy SSH result, got %#v", result)
	}
	if strings.Contains(strings.Join(arguments, " "), "docker") {
		t.Fatalf("SSH connection test unexpectedly invoked Docker: %v", arguments)
	}
}

func TestConnectionReportsAutomaticFirstUseTrust(t *testing.T) {
	remote := &Remote{command: func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "printf \"Warning: Permanently added host to known hosts.\\nIDEPLOY_OS=Linux IDEPLOY_ARCH=x86_64\"")
	}}
	result := remote.TestConnection(context.Background(), Connection{Host: "example.com", Port: 22, SSHUser: "deploy"}, "")
	if result.Status != StatusHealthy || !containsAll(result.Detail, "已自动信任首次连接主机指纹", "IDEPLOY_OS=Linux") || strings.Contains(result.Detail, "Warning:") {
		t.Fatalf("unexpected automatic-trust result: %#v", result)
	}
}

func TestEnvironmentProbeKeepsStrictHostKeyChecking(t *testing.T) {
	var arguments []string
	remote := &Remote{command: func(ctx context.Context, name string, args ...string) *exec.Cmd {
		arguments = append(arguments, args...)
		return exec.CommandContext(ctx, "sh", "-c", "printf ok")
	}}
	result := remote.Probe(context.Background(), Connection{Host: "example.com", Port: 22, SSHUser: "deploy"})
	if result.Status != StatusHealthy || !strings.Contains(strings.Join(arguments, " "), "StrictHostKeyChecking=yes") {
		t.Fatalf("environment probe did not keep strict host-key checking: result=%#v args=%v", result, arguments)
	}
}

func TestPasswordConnectionUsesOnlyPasswordAuthentication(t *testing.T) {
	var arguments []string
	remote := &Remote{command: func(ctx context.Context, name string, args ...string) *exec.Cmd {
		arguments = append(arguments, args...)
		return exec.CommandContext(ctx, "sh", "-c", "printf ok")
	}}
	result := remote.TestConnection(context.Background(), Connection{
		Host: "example.com", Port: 22, SSHUser: "deploy", AuthMethod: AuthMethodPassword,
	}, "secret")
	joined := strings.Join(arguments, " ")
	if result.Status != StatusHealthy || !containsAll(joined, "StrictHostKeyChecking=accept-new", "PreferredAuthentications=password,keyboard-interactive", "PubkeyAuthentication=no", "PasswordAuthentication=yes", "KbdInteractiveAuthentication=yes", "NumberOfPasswordPrompts=1") {
		t.Fatalf("password auth options missing: result=%#v args=%v", result, arguments)
	}
}

func TestConnectionFailureDetailExplainsBannerTimeout(t *testing.T) {
	detail := connectionFailureDetail(Connection{Host: "203.0.113.10", Port: 22}, errors.New("Connection timed out during banner exchange"))
	if !strings.Contains(detail, "TCP 已连接") || !strings.Contains(detail, "sshd") || !strings.Contains(detail, "203.0.113.10:22") {
		t.Fatalf("unexpected banner timeout detail: %s", detail)
	}
}

func TestPrivateKeyConnectionUsesSelectedIdentity(t *testing.T) {
	var arguments []string
	remote := &Remote{command: func(ctx context.Context, name string, args ...string) *exec.Cmd {
		arguments = append(arguments, args...)
		return exec.CommandContext(ctx, "sh", "-c", "printf ok")
	}}
	result := remote.TestConnection(context.Background(), Connection{
		Host: "example.com", Port: 2222, SSHUser: "deploy", AuthMethod: AuthMethodPrivateKey, PrivateKeyPath: "/tmp/id_ed25519",
	}, "")
	joined := strings.Join(arguments, " ")
	if result.Status != StatusHealthy || !containsAll(joined, "-i /tmp/id_ed25519", "IdentitiesOnly=yes", "PreferredAuthentications=publickey", "-p 2222") {
		t.Fatalf("private-key auth options missing: result=%#v args=%v", result, arguments)
	}
}

func TestDeployContainerBuildsControlledDockerCommand(t *testing.T) {
	var arguments []string
	remote := &Remote{command: func(ctx context.Context, name string, args ...string) *exec.Cmd {
		arguments = append(arguments, args...)
		return exec.CommandContext(ctx, "sh", "-c", "printf ok")
	}}
	err := remote.DeployContainer(context.Background(), Connection{Host: "example.com", Port: 22, SSHUser: "deploy"}, "api", "registry.example/team/api:v1", "unless-stopped", []string{"8080:80"})
	joined := strings.Join(arguments, " ")
	if err != nil || !containsAll(joined, "docker rm -f 'api'", "--publish '8080:80'", "'registry.example/team/api:v1'") {
		t.Fatalf("unexpected deployment command: err=%v args=%v", err, arguments)
	}
	if err := remote.DeployContainer(context.Background(), Connection{}, "api; touch /tmp/pwned", "image:v1", "always", nil); err == nil {
		t.Fatal("accepted unsafe container name")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !contains(value, part) {
			return false
		}
	}
	return true
}

func contains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
