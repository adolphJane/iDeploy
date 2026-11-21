package buildexecutor

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

type Capabilities struct {
	GitInstalled    bool   `json:"gitInstalled"`
	DockerInstalled bool   `json:"dockerInstalled"`
	DockerReady     bool   `json:"dockerReady"`
	BuildxReady     bool   `json:"buildxReady"`
	Detail          string `json:"detail"`
}

func CheckReady(ctx context.Context, compose bool) error {
	status := Probe(ctx)
	if !status.BuildxReady {
		return errors.New(status.Detail)
	}
	if compose {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := exec.CommandContext(probeCtx, "docker", "compose", "version").Run(); err != nil {
			return errors.New("构建服务器的 Compose 插件不可用")
		}
	}
	return nil
}

func Probe(parent context.Context) Capabilities {
	status := Capabilities{}
	if _, err := exec.LookPath("git"); err != nil {
		status.Detail = "服务器未安装 Git"
		return status
	}
	status.GitInstalled = true
	docker, err := exec.LookPath("docker")
	if err != nil {
		status.Detail = "服务器未安装 Docker CLI"
		return status
	}
	status.DockerInstalled = true
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, docker, "info", "--format", "{{.OSType}}/{{.Architecture}}").Output()
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(output)), "linux/") {
		status.Detail = "Docker daemon 不可用或当前用户没有访问权限"
		return status
	}
	status.DockerReady = true
	if err := exec.CommandContext(ctx, docker, "buildx", "version").Run(); err != nil {
		status.Detail = "Docker 可用，但 Buildx 不可用"
		return status
	}
	status.BuildxReady = true
	status.Detail = "Git、Docker daemon 和 Buildx 均可用"
	return status
}
