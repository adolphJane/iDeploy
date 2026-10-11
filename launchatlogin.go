package main

type LaunchAtLoginStatus struct {
	Enabled          bool   `json:"enabled"`
	RequiresApproval bool   `json:"requiresApproval"`
	Available        bool   `json:"available"`
	Detail           string `json:"detail"`
}

func (s *SystemService) GetLaunchAtLoginStatus() (LaunchAtLoginStatus, error) {
	return launchAtLoginStatus()
}

func (s *SystemService) SetLaunchAtLogin(enabled bool) (LaunchAtLoginStatus, error) {
	return setLaunchAtLogin(enabled)
}
