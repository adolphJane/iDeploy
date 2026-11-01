package server

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ListeningPort describes a socket bound to a non-loopback address. It is an
// observation on the server, not a firewall or public reachability test.
type ListeningPort struct {
	Port          int      `json:"port"`
	Protocol      string   `json:"protocol"`
	BindAddresses []string `json:"bindAddresses"`
	Service       string   `json:"service"`
	Container     string   `json:"container"`
	Process       string   `json:"process"`
}

var ssProcessName = regexp.MustCompile(`users:\(\("([^"]+)"`)

func parseListeningPorts(ssOutput, dockerOutput, hostNetworkOutput string) []ListeningPort {
	published := parseDockerPublishedServices(dockerOutput)
	byPort := map[int]NginxServiceEndpoint{}
	for _, item := range published {
		byPort[item.HostPort] = item
	}
	hostNginx := ""
	for _, line := range strings.Split(hostNetworkOutput, "\n") {
		fields := strings.SplitN(line, "|", 2)
		if len(fields) == 2 && strings.Contains(strings.ToLower(fields[1]), "nginx") {
			if hostNginx != "" {
				hostNginx = ""
				break
			}
			hostNginx = fields[0]
		}
	}
	found := map[int]*ListeningPort{}
	for _, line := range strings.Split(ssOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "LISTEN" {
			continue
		}
		local := fields[3]
		colon := strings.LastIndexByte(local, ':')
		if colon < 0 {
			continue
		}
		address := strings.Trim(local[:colon], "[]")
		port, err := strconv.Atoi(local[colon+1:])
		if err != nil || port < 1 || port > 65535 || address == "localhost" || address == "::1" || strings.HasPrefix(address, "127.") {
			continue
		}
		item := found[port]
		if item == nil {
			process := ""
			if match := ssProcessName.FindStringSubmatch(line); len(match) > 1 {
				process = match[1]
			}
			item = &ListeningPort{Port: port, Protocol: "tcp", BindAddresses: []string{}, Process: process, Service: process}
			if mapped, ok := byPort[port]; ok {
				item.Service = mapped.Service
				item.Container = mapped.Container
			}
			if process == "nginx" && hostNginx != "" {
				item.Service = hostNginx
				item.Container = hostNginx
			}
			found[port] = item
		}
		present := false
		for _, existing := range item.BindAddresses {
			if existing == address {
				present = true
				break
			}
		}
		if !present {
			item.BindAddresses = append(item.BindAddresses, address)
		}
	}
	result := make([]ListeningPort, 0, len(found))
	for _, item := range found {
		sort.Strings(item.BindAddresses)
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Port < result[j].Port })
	return result
}

func ParseListeningPorts(ssOutput, dockerOutput, hostNetworkOutput string) []ListeningPort {
	return parseListeningPorts(ssOutput, dockerOutput, hostNetworkOutput)
}

func (r *Remote) ListListeningPorts(ctx context.Context, connection Connection) ([]ListeningPort, error) {
	sockets, err := r.runTimeout(ctx, connection, "ss -H -ltnp", 12*time.Second)
	if err != nil {
		return nil, fmt.Errorf("读取服务器监听端口失败: %w", err)
	}
	containers, _ := r.runTimeout(ctx, connection, `docker ps --format '{{.Names}}|{{.Ports}}|{{.Label "com.docker.compose.project"}}|{{.Label "com.docker.compose.service"}}'`, 12*time.Second)
	hostNetwork, _ := r.runTimeout(ctx, connection, `docker ps --filter network=host --format '{{.Names}}|{{.Image}}'`, 12*time.Second)
	return parseListeningPorts(sockets, containers, hostNetwork), nil
}
