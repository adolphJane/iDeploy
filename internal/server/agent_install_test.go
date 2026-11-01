package server

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestAgentUninstallOnlyRemovesOwnedFiles(t *testing.T) {
	var args []string
	remote := &Remote{command: func(ctx context.Context, _ string, values ...string) *exec.Cmd {
		args = append([]string(nil), values...)
		return exec.CommandContext(ctx, "sh", "-c", "true")
	}}
	if err := remote.UninstallAgent(context.Background(), Connection{Host: "example.com", Port: 22, SSHUser: "root"}); err != nil {
		t.Fatal(err)
	}
	command := strings.Join(args, " ")
	for _, expected := range []string{"ideploy-agent.service", "/opt/ideploy/agent", "/var/lib/ideploy-agent", "/etc/ideploy/agent.token", "systemctl daemon-reload"} {
		if !strings.Contains(command, expected) {
			t.Fatalf("missing uninstall step %q", expected)
		}
	}
	for _, forbidden := range []string{"docker rm", "/etc/letsencrypt/live", "/etc/nginx/nginx.conf"} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("uninstall touches user resource %q", forbidden)
		}
	}
}
