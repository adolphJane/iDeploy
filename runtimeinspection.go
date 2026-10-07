package main

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"ideploy/internal/deployment"
	"ideploy/internal/server"
)

type DeploymentRuntimeView struct {
	DeploymentID string                `json:"deploymentId"`
	ServerID     string                `json:"serverId"`
	Status       string                `json:"status"`
	Detail       string                `json:"detail"`
	ObservedAt   string                `json:"observedAt"`
	Containers   []RemoteContainerView `json:"containers"`
}

type ContainerLogsInput struct {
	ServerID    string `json:"serverId"`
	ContainerID string `json:"containerId"`
	Tail        int    `json:"tail"`
}

type ContainerLogsView struct {
	ServerID    string `json:"serverId"`
	ContainerID string `json:"containerId"`
	Tail        int    `json:"tail"`
	Logs        string `json:"logs"`
	ObservedAt  string `json:"observedAt"`
}

type PortAvailabilityInput struct {
	ServerID string `json:"serverId"`
	Ports    []int  `json:"ports"`
}

type PortAvailabilityItemView struct {
	Port       int    `json:"port"`
	Available  bool   `json:"available"`
	OccupiedBy string `json:"occupiedBy"`
}

type PortAvailabilityView struct {
	ServerID       string                     `json:"serverId"`
	AllAvailable   bool                       `json:"allAvailable"`
	Ports          []PortAvailabilityItemView `json:"ports"`
	SuggestedPorts []int                      `json:"suggestedPorts"`
}

func (s *SystemService) InspectDeploymentRuntime(id string) (DeploymentRuntimeView, error) {
	deployments, err := s.deploymentStore()
	if err != nil {
		return DeploymentRuntimeView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	target, err := deployments.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return DeploymentRuntimeView{}, err
	}
	remoteView, err := s.ListRemoteContainers(target.ServerID)
	if err != nil {
		return DeploymentRuntimeView{}, err
	}
	containers := make([]RemoteContainerView, 0)
	for _, container := range remoteView.Containers {
		value := server.Container{ID: container.ID, Name: container.Name, Image: container.Image, State: container.State, Status: container.Status, Ports: container.Ports, Created: container.Created, ComposeProject: container.ComposeProject, OneOff: container.OneOff, Health: container.Health}
		if len(deployment.Match(target, []server.Container{value})) == 1 {
			containers = append(containers, container)
		}
	}
	status, detail := "missing", "远程未发现该部署的工作负载"
	if len(containers) > 0 {
		status, detail = "running", "远程工作负载运行中"
		for _, container := range containers {
			if container.State != "running" || container.Health == "unhealthy" {
				status, detail = "degraded", container.Name+": "+container.Status
				break
			}
		}
	}
	if remoteView.Stale {
		status, detail = "unknown", "远程刷新失败，返回的是缓存状态: "+remoteView.Detail
	}
	return DeploymentRuntimeView{DeploymentID: target.ID, ServerID: target.ServerID, Status: status, Detail: detail, ObservedAt: remoteView.ObservedAt, Containers: containers}, nil
}

func (s *SystemService) GetContainerLogs(input ContainerLogsInput) (ContainerLogsView, error) {
	input.ServerID = strings.TrimSpace(input.ServerID)
	input.ContainerID = strings.TrimSpace(input.ContainerID)
	if input.ServerID == "" || input.ContainerID == "" {
		return ContainerLogsView{}, errors.New("server_id and container_id are required")
	}
	if input.Tail <= 0 {
		input.Tail = 200
	}
	if input.Tail > 2000 {
		return ContainerLogsView{}, errors.New("tail cannot exceed 2000")
	}
	servers, err := s.serverStore()
	if err != nil {
		return ContainerLogsView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	host, err := servers.Get(ctx, input.ServerID)
	if err != nil {
		return ContainerLogsView{}, err
	}
	logs, err := server.NewRemote().ContainerLogs(ctx, host, input.ContainerID, input.Tail)
	if err != nil {
		return ContainerLogsView{}, err
	}
	return ContainerLogsView{ServerID: input.ServerID, ContainerID: input.ContainerID, Tail: input.Tail, Logs: logs, ObservedAt: time.Now().UTC().Format(time.RFC3339)}, nil
}

func (s *SystemService) CheckPortAvailability(input PortAvailabilityInput) (PortAvailabilityView, error) {
	input.ServerID = strings.TrimSpace(input.ServerID)
	if input.ServerID == "" || len(input.Ports) == 0 || len(input.Ports) > 32 {
		return PortAvailabilityView{}, errors.New("server_id and 1 to 32 ports are required")
	}
	requestedSet := map[int]bool{}
	for _, port := range input.Ports {
		if port <= 0 || port > 65535 {
			return PortAvailabilityView{}, errors.New("ports must be between 1 and 65535")
		}
		requestedSet[port] = true
	}
	requested := make([]int, 0, len(requestedSet))
	for port := range requestedSet {
		requested = append(requested, port)
	}
	sort.Ints(requested)
	remoteView, err := s.ListRemoteContainers(input.ServerID)
	if err != nil {
		return PortAvailabilityView{}, err
	}
	if remoteView.Stale {
		return PortAvailabilityView{}, errors.New("无法刷新远程端口状态: " + remoteView.Detail)
	}
	used := map[int]string{}
	for _, container := range remoteView.Containers {
		for _, port := range publishedHostPorts(container.Ports) {
			used[port] = container.Name
		}
	}
	view := PortAvailabilityView{ServerID: input.ServerID, AllAvailable: true, Ports: make([]PortAvailabilityItemView, 0, len(requested)), SuggestedPorts: []int{}}
	for _, port := range requested {
		owner := used[port]
		view.Ports = append(view.Ports, PortAvailabilityItemView{Port: port, Available: owner == "", OccupiedBy: owner})
		if owner != "" {
			view.AllAvailable = false
		}
	}
	if !view.AllAvailable {
		view.SuggestedPorts = suggestAvailablePorts(requested, used)
	}
	return view, nil
}
