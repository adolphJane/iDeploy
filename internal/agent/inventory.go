package agent

import (
	"context"
	"ideploy/internal/server"
	"os/exec"
	"strings"
	"time"
)

type Inventory struct {
	ObservedAt      string                 `json:"observedAt"`
	Containers      []server.Container     `json:"containers"`
	ListeningPorts  []server.ListeningPort `json:"listeningPorts"`
	ContainersError string                 `json:"containersError,omitempty"`
	PortsError      string                 `json:"portsError,omitempty"`
}

func ReadInventory(parent context.Context) Inventory {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	result := Inventory{ObservedAt: time.Now().UTC().Format(time.RFC3339), Containers: []server.Container{}, ListeningPorts: []server.ListeningPort{}}
	ps, err := exec.CommandContext(ctx, "docker", "ps", "-a", "--no-trunc", "--format", "{{json .}}").Output()
	if err != nil {
		result.ContainersError = err.Error()
	} else if result.Containers, err = server.ParseDockerContainers(string(ps)); err != nil {
		result.ContainersError = err.Error()
	}
	ss, err := exec.CommandContext(ctx, "ss", "-H", "-ltnp").Output()
	if err != nil {
		result.PortsError = err.Error()
		return result
	}
	services, _ := exec.CommandContext(ctx, "docker", "ps", "--format", "{{.Names}}|{{.Ports}}|{{.Label \"com.docker.compose.project\"}}|{{.Label \"com.docker.compose.service\"}}").Output()
	hostNetwork, _ := exec.CommandContext(ctx, "docker", "ps", "--filter", "network=host", "--format", "{{.Names}}|{{.Image}}").Output()
	result.ListeningPorts = server.ParseListeningPorts(string(ss), strings.TrimSpace(string(services)), strings.TrimSpace(string(hostNetwork)))
	return result
}
