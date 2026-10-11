//go:build !darwin

package main

import "errors"

func startAppUpdater() {}

func (s *SystemService) CheckForAppUpdates() error {
	return errors.New("应用内检查更新仅支持 macOS")
}
