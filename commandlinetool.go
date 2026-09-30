package main

type CommandLineToolStatus struct {
	Installed      bool   `json:"installed"`
	Conflict       bool   `json:"conflict"`
	CommandPath    string `json:"commandPath"`
	ExecutablePath string `json:"executablePath"`
	Detail         string `json:"detail"`
}

func (s *SystemService) GetCommandLineToolStatus() (CommandLineToolStatus, error) {
	return commandLineToolStatus()
}

func (s *SystemService) InstallCommandLineTool() (CommandLineToolStatus, error) {
	return installCommandLineTool()
}
