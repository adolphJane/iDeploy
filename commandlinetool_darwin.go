//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const commandLineToolPath = "/usr/local/bin/ideploy"

func commandLineToolStatus() (CommandLineToolStatus, error) {
	executable, err := os.Executable()
	if err != nil {
		return CommandLineToolStatus{}, fmt.Errorf("resolve iDeploy executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return CommandLineToolStatus{}, fmt.Errorf("resolve iDeploy executable links: %w", err)
	}
	status := CommandLineToolStatus{CommandPath: commandLineToolPath, ExecutablePath: executable}
	if !strings.HasSuffix(filepath.ToSlash(executable), "/iDeploy.app/Contents/MacOS/iDeploy") {
		status.Detail = "请先将 iDeploy.app 安装到本机，再安装终端命令"
		return status, nil
	}
	info, err := os.Lstat(commandLineToolPath)
	if errors.Is(err, os.ErrNotExist) {
		status.Detail = "尚未安装终端命令"
		return status, nil
	}
	if err != nil {
		return status, fmt.Errorf("检查终端命令路径: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		status.Conflict = true
		status.Detail = "命令路径已被其他文件占用，不会自动覆盖"
		return status, nil
	}
	linkTarget, err := os.Readlink(commandLineToolPath)
	if err != nil {
		return status, fmt.Errorf("读取终端命令链接: %w", err)
	}
	if !filepath.IsAbs(linkTarget) {
		linkTarget = filepath.Join(filepath.Dir(commandLineToolPath), linkTarget)
	}
	resolvedTarget, resolveErr := filepath.EvalSymlinks(commandLineToolPath)
	if resolveErr == nil && resolvedTarget == executable {
		status.Installed = true
		status.Detail = "终端命令已安装"
		return status, nil
	}
	if strings.HasSuffix(filepath.ToSlash(filepath.Clean(linkTarget)), "/iDeploy.app/Contents/MacOS/iDeploy") {
		status.Detail = "终端命令指向其他位置，可重新安装以更新"
		return status, nil
	}
	status.Conflict = true
	status.Detail = "命令路径已被其他程序占用，不会自动覆盖"
	return status, nil
}

func installCommandLineTool() (CommandLineToolStatus, error) {
	status, err := commandLineToolStatus()
	if err != nil || status.Installed {
		return status, err
	}
	if status.Conflict {
		return status, errors.New(status.Detail)
	}
	if !strings.HasSuffix(filepath.ToSlash(status.ExecutablePath), "/iDeploy.app/Contents/MacOS/iDeploy") {
		return status, errors.New("请从已安装的 iDeploy.app 中安装终端命令")
	}
	command := "/bin/mkdir -p /usr/local/bin && /bin/ln -sfn " + shellSingleQuote(status.ExecutablePath) + " " + shellSingleQuote(commandLineToolPath)
	script := "do shell script \"" + appleScriptString(command) + "\" with administrator privileges"
	output, err := exec.Command("/usr/bin/osascript", "-e", script).CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return status, fmt.Errorf("安装终端命令失败：%s", detail)
	}
	status, err = commandLineToolStatus()
	if err != nil {
		return status, err
	}
	if !status.Installed {
		return status, errors.New("终端命令安装完成后未能通过校验")
	}
	return status, nil
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func appleScriptString(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	return strings.ReplaceAll(value, "\"", "\\\"")
}
