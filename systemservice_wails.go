//go:build !native

package main

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func (s *SystemService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	return s.start(ctx, true)
}

func (s *SystemService) ChooseProjectDirectory() (string, error) {
	return application.Get().Dialog.OpenFile().
		CanChooseDirectories(true).
		CanChooseFiles(false).
		SetTitle("选择项目目录").
		PromptForSingleSelection()
}

func (s *SystemService) ChooseSSHPrivateKey() (string, error) {
	return application.Get().Dialog.OpenFile().
		CanChooseDirectories(false).
		CanChooseFiles(true).
		SetTitle("选择 SSH 私钥").
		PromptForSingleSelection()
}
