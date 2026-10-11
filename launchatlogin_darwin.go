//go:build darwin

package main

/*
#cgo LDFLAGS: -framework Foundation -framework ServiceManagement
#include <stdlib.h>
int ideploy_login_status(void);
int ideploy_set_login(int enabled, char **error_message);
*/
import "C"

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"
)

func bundledAppExecutable() bool {
	path, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.HasSuffix(filepath.ToSlash(path), "/iDeploy.app/Contents/MacOS/iDeploy")
}

func launchAtLoginStatus() (LaunchAtLoginStatus, error) {
	if !bundledAppExecutable() {
		return LaunchAtLoginStatus{Detail: "请从已安装的 iDeploy.app 设置登录启动"}, nil
	}
	status := LaunchAtLoginStatus{Available: true}
	switch int(C.ideploy_login_status()) {
	case 1:
		status.Enabled = true
		status.Detail = "登录后自动启动 iDeploy，菜单栏和本机发布能力随即就绪"
	case 2:
		status.RequiresApproval = true
		status.Detail = "请在 macOS 系统设置的登录项中允许 iDeploy"
	default:
		status.Detail = "登录后不会自动启动 iDeploy"
	}
	return status, nil
}

func setLaunchAtLogin(enabled bool) (LaunchAtLoginStatus, error) {
	status, err := launchAtLoginStatus()
	if err != nil || !status.Available {
		if err == nil {
			err = errors.New(status.Detail)
		}
		return status, err
	}
	var message *C.char
	requested := C.int(0)
	if enabled {
		requested = 1
	}
	if C.ideploy_set_login(requested, &message) == 0 {
		defer C.free(unsafe.Pointer(message))
		return status, errors.New(C.GoString(message))
	}
	return launchAtLoginStatus()
}
