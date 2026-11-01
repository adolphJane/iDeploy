package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

func composeFile(deploymentID string) (string, error) {
	if !containerIDPattern.MatchString(deploymentID) {
		return "", errors.New("invalid deployment ID")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ideploy", "deployments", deploymentID, "compose.yml"), nil
}

func uploadCompose(req OperationRequest) error {
	path, err := composeFile(req.DeploymentID)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "compose-*.candidate")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.WriteString(req.Manifest); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func runCompose(ctx context.Context, req OperationRequest) ([]byte, error) {
	path, err := composeFile(req.DeploymentID)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	args := []string{"compose", "--project-name", req.ProjectName, "-f", path}
	if req.Kind == "compose.pull" {
		args = append(args, "pull")
	} else {
		args = append(args, "up", "-d", "--remove-orphans")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = append(os.Environ(), "IMAGE_TAG="+req.Version)
	return cmd.CombinedOutput()
}
