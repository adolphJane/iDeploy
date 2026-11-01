package main

import (
	"context"
	"ideploy/internal/server"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

type DomainAccessView struct {
	HostID        string   `json:"hostId"`
	FQDN          string   `json:"fqdn"`
	Status        string   `json:"status"`
	DNSStatus     string   `json:"dnsStatus"`
	DNSAddresses  []string `json:"dnsAddresses"`
	IngressStatus string   `json:"ingressStatus"`
	BackendStatus string   `json:"backendStatus"`
	HTTPSStatus   string   `json:"httpsStatus"`
	Detail        string   `json:"detail"`
	ObservedAt    string   `json:"observedAt"`
}

func (s *SystemService) InspectDomainAccess(hostID string) (DomainAccessView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := s.domainStore()
	if err != nil {
		return DomainAccessView{}, err
	}
	host, err := store.GetHost(ctx, hostID)
	if err != nil {
		return DomainAccessView{}, err
	}
	view := DomainAccessView{HostID: hostID, FQDN: host.FQDN, Status: "unknown", DNSStatus: "unknown",
		IngressStatus: "unknown", BackendStatus: "unknown", HTTPSStatus: "unknown", DNSAddresses: []string{},
		ObservedAt: time.Now().UTC().Format(time.RFC3339)}
	deploymentRoute := host.Binding != nil && host.Binding.Status == "active"
	externalRoute := host.ExternalRoute != nil && host.ExternalRoute.Status == "active"
	if !deploymentRoute && !externalRoute {
		view.Detail = "入口尚未应用"
		return view, nil
	}
	serverID, backendIP, backendPort := "", "", 0
	if deploymentRoute {
		b := *host.Binding
		deployments, err := s.deploymentStore()
		if err != nil {
			return view, err
		}
		target, err := deployments.Get(ctx, b.DeploymentID)
		if err != nil {
			return view, err
		}
		serverID, backendIP, backendPort = target.ServerID, b.HostIP, b.HostPort
	} else {
		route := *host.ExternalRoute
		serverID, backendIP, backendPort = route.ServerID, route.HostIP, route.HostPort
	}
	servers, err := s.serverStore()
	if err != nil {
		return view, err
	}
	connection, err := servers.Get(ctx, serverID)
	if err != nil {
		return view, err
	}
	ingress, err := store.GetIngress(ctx, serverID)
	if err != nil {
		return view, err
	}
	remote := server.NewRemote()
	if strings.HasPrefix(ingress.ImageRef, "nginx:") {
		state, inspectErr := remote.InspectExistingNginx(ctx, connection)
		if inspectErr != nil {
			view.Detail = inspectErr.Error()
		} else if "nginx:"+state.Container != ingress.ImageRef || state.Hash != ingress.ConfigHash {
			view.IngressStatus = "drift"
			view.Detail = "现有 Nginx 配置与上次应用记录不一致"
		} else if routeErr := remote.ProbeNginxRoute(ctx, connection, host.FQDN); routeErr != nil {
			view.IngressStatus = "drift"
			view.Detail = routeErr.Error()
		} else {
			view.IngressStatus = "ready"
		}
	} else {
		state, inspectErr := remote.InspectManagedIngress(ctx, connection)
		if inspectErr != nil {
			view.Detail = inspectErr.Error()
		} else if !state.Exists || !state.Running || state.Hash != ingress.ConfigHash {
			view.IngressStatus = "drift"
			view.Detail = "受管入口未运行或配置与本地记录不一致"
		} else {
			view.IngressStatus = "ready"
		}
	}
	if err := remote.ProbeIngressBackend(ctx, connection, backendIP, backendPort, host.FQDN); err != nil {
		view.BackendStatus = "failed"
		if view.Detail == "" {
			view.Detail = err.Error()
		}
	} else {
		view.BackendStatus = "responding"
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host.FQDN)
	if err != nil || len(addresses) == 0 {
		view.DNSStatus = "unresolved"
		if view.Detail == "" {
			view.Detail = "本机无法解析此域名"
		}
	} else {
		seen := map[string]bool{}
		for _, address := range addresses {
			ip := address.IP.String()
			if !seen[ip] {
				seen[ip] = true
				view.DNSAddresses = append(view.DNSAddresses, ip)
			}
		}
		sort.Strings(view.DNSAddresses)
		view.DNSStatus = "resolved"
	}
	httpOnly := strings.HasPrefix(ingress.ImageRef, "nginx:") &&
		(deploymentRoute && host.Binding.CertificatePath == "" || externalRoute && host.ExternalRoute.CertificatePath == "")
	if httpOnly {
		view.HTTPSStatus = "not_configured"
	}
	publicRouteVerified := false
	if view.DNSStatus == "resolved" {
		scheme := "https"
		if httpOnly {
			scheme = "http"
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodHead, scheme+"://"+host.FQDN+"/", nil)
		if err != nil {
			return view, err
		}
		client := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: nil},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, err := client.Do(request)
		if err != nil {
			if !httpOnly {
				view.HTTPSStatus = "failed"
			}
			if view.Detail == "" {
				view.Detail = strings.ToUpper(scheme) + " 校验失败: " + err.Error()
			}
		} else {
			response.Body.Close()
			if response.Header.Get("X-iDeploy-Route") == server.IngressRouteMarker(host.FQDN) {
				publicRouteVerified = true
				if !httpOnly {
					view.HTTPSStatus = "verified"
				}
			} else {
				if !httpOnly {
					view.HTTPSStatus = "wrong_target"
				}
				if view.Detail == "" {
					view.Detail = strings.ToUpper(scheme) + " 已响应，但未命中本域名的 iDeploy 路由"
				}
			}
		}
	}
	if view.IngressStatus == "ready" && view.BackendStatus == "responding" && view.DNSStatus == "resolved" && publicRouteVerified {
		view.Status = "reachable"
		protocol := "HTTPS"
		if httpOnly {
			protocol = "HTTP；HTTPS 尚未配置"
		}
		view.Detail = "已从本机验证 DNS、" + protocol + "、入口路由与 HTTP 后端；业务健康需按应用自身检查。"
	} else if view.Detail == "" {
		view.Detail = "访问条件尚未全部就绪"
	}
	return view, nil
}
