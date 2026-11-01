package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ContainerReplacement struct {
	Name        string `json:"name"`
	BackupName  string `json:"backupName"`
	HadPrevious bool   `json:"hadPrevious"`
}

func docker(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
}

func deployContainer(ctx context.Context, req OperationRequest) (ContainerReplacement, error) {
	if err := validateOperation(req); err != nil {
		return ContainerReplacement{}, err
	}
	name := req.ContainerID
	suffix := ".ideploy-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	backupBase := name
	if len(backupBase)+len(suffix) > 128 {
		backupBase = backupBase[:128-len(suffix)]
	}
	backup := backupBase + suffix
	replacement := ContainerReplacement{Name: name, BackupName: backup}
	args := []string{"run", "-d", "--restart", req.RestartPolicy, "--name", name}
	if len(req.Environment) > 0 {
		if err := os.MkdirAll("/run/ideploy-agent", 0o700); err != nil {
			return ContainerReplacement{}, err
		}
		file, err := os.CreateTemp("/run/ideploy-agent", "env-*")
		if err != nil {
			return ContainerReplacement{}, err
		}
		defer os.Remove(file.Name())
		if err := file.Chmod(0o600); err != nil {
			file.Close()
			return ContainerReplacement{}, err
		}
		keys := make([]string, 0, len(req.Environment))
		for key := range req.Environment {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if _, err := fmt.Fprintf(file, "%s=%s\n", key, req.Environment[key]); err != nil {
				file.Close()
				return ContainerReplacement{}, err
			}
		}
		if err := file.Close(); err != nil {
			return ContainerReplacement{}, err
		}
		args = append(args, "--env-file", file.Name())
	}
	keys := make([]string, 0, len(req.Labels))
	for key := range req.Labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--label", key+"="+req.Labels[key])
	}
	for _, port := range req.Ports {
		args = append(args, "--publish", port)
	}
	args = append(args, req.Image)
	inspect, inspectErr := docker(ctx, "container", "inspect", name)
	if inspectErr == nil {
		replacement.HadPrevious = true
		if _, err := docker(ctx, "rename", name, backup); err != nil {
			return ContainerReplacement{}, fmt.Errorf("rename existing container: %w", err)
		}
		if _, err := docker(ctx, "stop", "--time", "10", backup); err != nil {
			_, restoreErr := docker(ctx, "rename", backup, name)
			return ContainerReplacement{}, fmt.Errorf("stop existing container: %w; restore: %v", err, restoreErr)
		}
	} else if !strings.Contains(string(inspect), "No such object") {
		return ContainerReplacement{}, fmt.Errorf("inspect existing container: %w", inspectErr)
	}
	if _, err := docker(ctx, args...); err != nil {
		_, _ = docker(ctx, "rm", "-f", name)
		if replacement.HadPrevious {
			if _, restoreErr := docker(ctx, "rename", backup, name); restoreErr != nil {
				return ContainerReplacement{}, fmt.Errorf("start new container failed: %w; restore rename failed: %v", err, restoreErr)
			}
			if _, restoreErr := docker(ctx, "start", name); restoreErr != nil {
				return ContainerReplacement{}, fmt.Errorf("start new container failed: %w; restore start failed: %v", err, restoreErr)
			}
		}
		return ContainerReplacement{}, fmt.Errorf("start new container: %w", err)
	}
	return replacement, nil
}

func commitContainer(ctx context.Context, req OperationRequest) error {
	if !req.HadPrevious {
		return nil
	}
	_, err := docker(ctx, "rm", "-f", req.BackupName)
	return err
}

func rollbackContainer(ctx context.Context, req OperationRequest) error {
	if _, err := docker(ctx, "rm", "-f", req.ContainerID); err != nil {
		if !strings.Contains(err.Error(), "exit status 1") {
			return err
		}
	}
	if !req.HadPrevious {
		return nil
	}
	if _, err := docker(ctx, "rename", req.BackupName, req.ContainerID); err != nil {
		return err
	}
	_, err := docker(ctx, "start", req.ContainerID)
	return err
}
