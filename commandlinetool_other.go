//go:build !darwin

package main

import "errors"

func commandLineToolStatus() (CommandLineToolStatus, error) {
	return CommandLineToolStatus{Detail: "当前仅支持在 macOS 安装终端命令"}, nil
}

func installCommandLineTool() (CommandLineToolStatus, error) {
	return CommandLineToolStatus{}, errors.New("当前仅支持在 macOS 安装终端命令")
}
