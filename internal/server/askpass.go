package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func withPasswordAskpass(password string, run func(*os.File) (string, error)) (string, error) {
	directory, err := os.MkdirTemp("", "ideploy-askpass-")
	if err != nil {
		return "", fmt.Errorf("创建 SSH 密码输入助手: %w", err)
	}
	defer os.RemoveAll(directory)

	path := filepath.Join(directory, "askpass.sh")
	contents := "#!/bin/sh\nprintf '%s\\n' " + shellQuote(password) + " 2>/dev/null || true\n"
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		return "", fmt.Errorf("写入 SSH 密码输入助手: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("打开 SSH 密码输入助手: %w", err)
	}
	defer file.Close()
	return run(file)
}

func passwordEnvironment(askpass *os.File) []string {
	return []string{
		"SSH_ASKPASS=" + askpass.Name(),
		"SSH_ASKPASS_REQUIRE=force",
		"DISPLAY=ideploy",
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
