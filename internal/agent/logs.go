package agent

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

func ContainerLogs(parent context.Context, containerID string, tail int) (string, error) {
	if !containerIDPattern.MatchString(containerID) || tail < 1 || tail > 5000 {
		return "", errors.New("invalid container logs request")
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "docker", "logs", "--timestamps", "--tail", fmt.Sprint(tail), containerID).CombinedOutput()
	if err != nil {
		return "", err
	}
	if len(output) > 1024*1024 {
		output = output[len(output)-1024*1024:]
	}
	return string(output), nil
}
