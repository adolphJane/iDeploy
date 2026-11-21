package gitauth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const service = "com.ideploy.project.git"
const prefix = "keychain:git-project:"

type Credential struct {
	Username string `json:"username"`
	Token    string `json:"token"`
}

func Ref(projectID string) string { return prefix + projectID }

func Save(ctx context.Context, ref string, value Credential) error {
	if runtime.GOOS != "darwin" || !strings.HasPrefix(ref, prefix) || value.Username == "" || value.Token == "" || strings.ContainsAny(value.Username, "\r\n") {
		return errors.New("Git 凭据无效")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "security", "add-generic-password", "-a", strings.TrimPrefix(ref, prefix), "-s", service, "-w", string(encoded), "-U")
	if err := command.Run(); err != nil {
		return errors.New("保存 Git 凭据到系统钥匙串失败")
	}
	return nil
}

func Load(ctx context.Context, ref string) (Credential, error) {
	if runtime.GOOS != "darwin" || !strings.HasPrefix(ref, prefix) {
		return Credential{}, errors.New("Git 凭据不可用")
	}
	output, err := exec.CommandContext(ctx, "security", "find-generic-password", "-a", strings.TrimPrefix(ref, prefix), "-s", service, "-w").Output()
	if err != nil {
		return Credential{}, errors.New("Git 凭据不可用")
	}
	var value Credential
	if json.Unmarshal(output, &value) != nil || value.Username == "" || value.Token == "" {
		return Credential{}, errors.New("Git 凭据无效")
	}
	return value, nil
}

func Delete(ctx context.Context, ref string) error {
	if runtime.GOOS != "darwin" || !strings.HasPrefix(ref, prefix) {
		return nil
	}
	_ = exec.CommandContext(ctx, "security", "delete-generic-password", "-a", strings.TrimPrefix(ref, prefix), "-s", service).Run()
	return nil
}

// Environment passes credentials to Git's askpass hook only for the lifetime
// of a command. The token does not appear in the URL, arguments or Git logs.
func Environment(value Credential) ([]string, func(), error) {
	if value.Username == "" || value.Token == "" {
		return nil, nil, errors.New("Git 凭据无效")
	}
	dir, err := os.MkdirTemp("", "ideploy-git-auth-")
	if err != nil {
		return nil, nil, err
	}
	path := filepath.Join(dir, "askpass")
	if err := os.WriteFile(path, []byte("#!/bin/sh\ncase \"$1\" in\n*Username*) printf '%s\\n' \"$IDEPLOY_GIT_USER\" ;;\n*Password*) printf '%s\\n' \"$IDEPLOY_GIT_TOKEN\" ;;\nesac\n"), 0700); err != nil {
		os.RemoveAll(dir)
		return nil, nil, err
	}
	env := append(os.Environ(), "GIT_ASKPASS="+path, "GIT_TERMINAL_PROMPT=0", "IDEPLOY_GIT_USER="+value.Username, "IDEPLOY_GIT_TOKEN="+value.Token,
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.followRedirects", "GIT_CONFIG_VALUE_0=false")
	return env, func() { _ = os.RemoveAll(dir) }, nil
}

func Encode(value Credential) (string, error) {
	data, err := json.Marshal(value)
	return string(data), err
}

func Decode(raw string) (Credential, error) {
	var value Credential
	if err := json.Unmarshal([]byte(raw), &value); err != nil || value.Username == "" || value.Token == "" {
		return Credential{}, errors.New("Git 凭据无效")
	}
	return value, nil
}
