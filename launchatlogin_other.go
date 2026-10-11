//go:build !darwin

package main

import "errors"

func launchAtLoginStatus() (LaunchAtLoginStatus, error) {
	return LaunchAtLoginStatus{Detail: "登录启动仅支持 macOS"}, nil
}

func setLaunchAtLogin(bool) (LaunchAtLoginStatus, error) {
	status, _ := launchAtLoginStatus()
	return status, errors.New(status.Detail)
}
