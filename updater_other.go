//go:build !darwin

package main

func startAppUpdater() {}

func (s *SystemService) CheckForAppUpdates() bool                   { return false }
func (s *SystemService) GetAutomaticUpdateChecks() bool             { return false }
func (s *SystemService) SetAutomaticUpdateChecks(enabled bool) bool { return false }

func sparkleUpdateProgress() (AppUpdateProgress, error) {
	return AppUpdateProgress{Phase: "idle", Percent: -1}, nil
}
