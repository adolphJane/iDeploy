package main

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type DomainEndpoint struct {
	ContainerName string   `json:"containerName"`
	HostIP        string   `json:"hostIp"`
	BindAddresses []string `json:"bindAddresses"`
	HostPort      int      `json:"hostPort"`
	ContainerPort int      `json:"containerPort"`
	Protocol      string   `json:"protocol"`
	Status        string   `json:"status"`
	Reason        string   `json:"reason"`
}

type DomainEndpointDiscovery struct {
	DeploymentID string           `json:"deploymentId"`
	ObservedAt   string           `json:"observedAt"`
	Status       string           `json:"status"`
	Detail       string           `json:"detail"`
	Endpoints    []DomainEndpoint `json:"endpoints"`
}

var domainPortPattern = regexp.MustCompile(`(?:\[([^]]+)\]|([0-9.]+)|(::)):(\d+)->(\d+)/(tcp|udp)`)

func parseDomainEndpoints(container RemoteContainerView) []DomainEndpoint {
	result := []DomainEndpoint{}
	seen := map[string]bool{}
	wildcardIndices := map[string]int{}
	for _, match := range domainPortPattern.FindAllStringSubmatch(container.Ports, -1) {
		if match[6] != "tcp" {
			continue
		}
		hostPort, err1 := strconv.Atoi(match[4])
		containerPort, err2 := strconv.Atoi(match[5])
		if err1 != nil || err2 != nil || hostPort < 1 || hostPort > 65535 || containerPort < 1 || containerPort > 65535 {
			continue
		}
		hostIP := match[1]
		if hostIP == "" {
			hostIP = match[2]
		}
		if hostIP == "" {
			hostIP = match[3]
		}
		key := container.Name + ":" + hostIP + ":" + match[4] + ":" + match[5]
		if seen[key] {
			continue
		}
		seen[key] = true
		if hostIP == "0.0.0.0" || hostIP == "::" {
			mappingKey := container.Name + ":" + match[4] + ":" + match[5]
			if index, exists := wildcardIndices[mappingKey]; exists {
				result[index].BindAddresses = append(result[index].BindAddresses, hostIP)
				if hostIP == "0.0.0.0" {
					result[index].HostIP = hostIP
				}
				continue
			}
			wildcardIndices[mappingKey] = len(result)
		}
		result = append(result, DomainEndpoint{ContainerName: container.Name, HostIP: hostIP,
			BindAddresses: []string{hostIP},
			HostPort:      hostPort, ContainerPort: containerPort, Protocol: "tcp",
			Status: "unverified", Reason: "发现 Docker 实际端口映射；尚未验证 HTTP 服务"})
	}
	return result
}

func (s *SystemService) DiscoverDeploymentEndpoints(id string) (DomainEndpointDiscovery, error) {
	runtime, err := s.InspectDeploymentRuntime(id)
	if err != nil {
		return DomainEndpointDiscovery{}, err
	}
	if runtime.Status == "unknown" {
		return DomainEndpointDiscovery{}, errors.New("无法确认远端容器状态，请检查服务器连接")
	}
	view := DomainEndpointDiscovery{DeploymentID: id, ObservedAt: runtime.ObservedAt,
		Status: runtime.Status, Detail: runtime.Detail, Endpoints: []DomainEndpoint{}}
	for _, container := range runtime.Containers {
		if !strings.EqualFold(container.State, "running") {
			continue
		}
		view.Endpoints = append(view.Endpoints, parseDomainEndpoints(container)...)
	}
	sort.Slice(view.Endpoints, func(i, j int) bool {
		if view.Endpoints[i].ContainerName != view.Endpoints[j].ContainerName {
			return view.Endpoints[i].ContainerName < view.Endpoints[j].ContainerName
		}
		return view.Endpoints[i].ContainerPort < view.Endpoints[j].ContainerPort
	})
	if len(view.Endpoints) == 0 {
		view.Detail = "运行中的工作负载没有已发布的 TCP 端口映射"
	}
	return view, nil
}
