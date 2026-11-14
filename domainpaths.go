package main

import (
	"context"
	"encoding/json"
	"errors"
	"ideploy/internal/domain"
	"ideploy/internal/job"
	"ideploy/internal/server"
	"net"
	"net/http"
	"strings"
	"time"
)

func (s *SystemService) ListSharedPathRoutes() ([]domain.PathRoute, error) {
	store, err := s.domainStore()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.ListPathRoutes(ctx)
}

func (s *SystemService) SaveSharedPathRouteDraft(input domain.PathRoute) (domain.PathRoute, error) {
	input.FQDN = strings.ToLower(strings.TrimSpace(input.FQDN))
	store, err := s.domainStore()
	if err != nil {
		return domain.PathRoute{}, err
	}
	deployments, err := s.deploymentStore()
	if err != nil {
		return domain.PathRoute{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	target, err := deployments.Get(ctx, input.DeploymentID)
	if err != nil {
		return domain.PathRoute{}, err
	}
	others, err := store.ListPathRoutes(ctx)
	if err != nil {
		return domain.PathRoute{}, err
	}
	for _, other := range others {
		if other.ID == input.ID || other.FQDN != input.FQDN {
			continue
		}
		linked, err := deployments.Get(ctx, other.DeploymentID)
		if err != nil {
			return domain.PathRoute{}, err
		}
		if linked.ServerID != target.ServerID {
			return domain.PathRoute{}, errors.New("共享域名的所有路径必须部署在同一台服务器")
		}
		if other.CertificatePath != input.CertificatePath || other.CertificateKeyPath != input.CertificateKeyPath {
			return domain.PathRoute{}, errors.New("共享域名的所有路径必须使用同一 HTTPS 证书")
		}
	}
	if input.CertificatePath != "" {
		if _, err := server.NginxManagedBlock([]server.IngressRoute{{FQDN: input.FQDN, PathPrefix: "/check/", HostIP: "127.0.0.1", HostPort: 80, Certificate: input.CertificatePath, CertificateKey: input.CertificateKeyPath}}); err != nil {
			return domain.PathRoute{}, err
		}
	}
	return store.SavePathRouteDraft(ctx, input)
}

func (s *SystemService) DeleteSharedPathRouteDraft(id string) error {
	store, err := s.domainStore()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.DeletePathRouteDraft(ctx, id)
}

type SharedPathPlan struct {
	RouteID           string `json:"routeId"`
	UpdatedAt         string `json:"updatedAt"`
	IngressConfigHash string `json:"ingressConfigHash"`
	IngressPreview    string `json:"ingressPreview"`
	Detail            string `json:"detail"`
	CanApply          bool   `json:"canApply"`
}

type SharedPathAccess struct {
	RouteID       string `json:"routeId"`
	URL           string `json:"url"`
	DNSStatus     string `json:"dnsStatus"`
	IngressStatus string `json:"ingressStatus"`
	BackendStatus string `json:"backendStatus"`
	HTTPSStatus   string `json:"httpsStatus"`
	PublicStatus  int    `json:"publicStatus"`
	Detail        string `json:"detail"`
}

func (s *SystemService) InspectSharedPathAccess(id string) (SharedPathAccess, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := s.domainStore()
	if err != nil {
		return SharedPathAccess{}, err
	}
	v, err := store.GetPathRoute(ctx, id)
	if err != nil {
		return SharedPathAccess{}, err
	}
	scheme := "https"
	if v.CertificatePath == "" {
		scheme = "http"
	}
	view := SharedPathAccess{RouteID: id, URL: scheme + "://" + v.FQDN + v.PathPrefix, DNSStatus: "unknown", IngressStatus: "unknown", BackendStatus: "unknown", HTTPSStatus: "not_configured"}
	if v.Status != "active" {
		view.Detail = "路径入口尚未应用"
		return view, nil
	}
	deployments, err := s.deploymentStore()
	if err != nil {
		return view, err
	}
	target, err := deployments.Get(ctx, v.DeploymentID)
	if err != nil {
		return view, err
	}
	servers, err := s.serverStore()
	if err != nil {
		return view, err
	}
	connection, err := servers.Get(ctx, target.ServerID)
	if err != nil {
		return view, err
	}
	ingress, err := store.GetIngress(ctx, target.ServerID)
	if err != nil {
		return view, err
	}
	remote := server.NewRemote()
	state, err := remote.InspectExistingNginx(ctx, connection)
	if err != nil {
		view.IngressStatus = "failed"
		view.Detail = err.Error()
	} else if ingress.ImageRef != "nginx:"+state.Container || ingress.ConfigHash != state.Hash {
		view.IngressStatus = "drift"
		view.Detail = "Nginx 配置与已保存版本不一致"
	} else if err = remote.ProbeNginxPathRoute(ctx, connection, server.IngressRoute{FQDN: v.FQDN, PathPrefix: v.PathPrefix, Certificate: v.CertificatePath}); err != nil {
		view.IngressStatus = "failed"
		view.Detail = err.Error()
	} else {
		view.IngressStatus = "ready"
	}
	if err = remote.ProbeIngressBackendPath(ctx, connection, v.HostIP, v.HostPort, v.FQDN, v.PathPrefix); err != nil {
		view.BackendStatus = "failed"
		if view.Detail == "" {
			view.Detail = err.Error()
		}
	} else {
		view.BackendStatus = "responding"
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, v.FQDN)
	if err != nil || len(addresses) == 0 {
		view.DNSStatus = "unresolved"
		if view.Detail == "" {
			view.Detail = "本机无法解析共享域名"
		}
		return view, nil
	}
	view.DNSStatus = "resolved"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, view.URL, nil)
	if err != nil {
		return view, err
	}
	client := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		if scheme == "https" {
			view.HTTPSStatus = "failed"
		}
		if view.Detail == "" {
			view.Detail = "公开地址访问失败: " + err.Error()
		}
		return view, nil
	}
	defer response.Body.Close()
	view.PublicStatus = response.StatusCode
	if response.Header.Get("X-iDeploy-Path") != server.IngressRouteMarker(v.FQDN+v.PathPrefix) {
		if scheme == "https" {
			view.HTTPSStatus = "wrong_target"
		}
		if view.Detail == "" {
			view.Detail = "公开地址未命中预期的路径路由"
		}
		return view, nil
	}
	if scheme == "https" {
		view.HTTPSStatus = "verified"
	}
	if response.StatusCode >= 500 {
		view.Detail = "路径路由已命中，但应用返回 HTTP " + response.Status
		return view, nil
	}
	if response.StatusCode == 404 {
		view.Detail = "路径路由已命中，但应用对该路径返回 404；API 可检查更具体的接口地址"
		return view, nil
	}
	if view.IngressStatus == "ready" && view.BackendStatus == "responding" {
		view.Detail = "DNS、入口路径及公开访问已验证；应用返回 " + response.Status
	} else if view.Detail == "" {
		view.Detail = "公开路径已响应，但入口或后端检查仍需处理"
	}
	return view, nil
}

func pathIngressRoute(v domain.PathRoute, endpoint DomainEndpoint) server.IngressRoute {
	return server.IngressRoute{FQDN: v.FQDN, PathPrefix: v.PathPrefix, HostIP: endpoint.HostIP, HostPort: endpoint.HostPort, UpstreamScheme: "http", Certificate: v.CertificatePath, CertificateKey: v.CertificateKeyPath}
}

func pathDesiredRoutes(routes []domain.Route, skipID string, addition *server.IngressRoute) []server.IngressRoute {
	desired := make([]server.IngressRoute, 0, len(routes)+1)
	for _, route := range routes {
		if route.HostID == skipID {
			continue
		}
		desired = append(desired, server.IngressRoute{FQDN: route.FQDN, PathPrefix: route.PathPrefix, HostIP: route.HostIP, HostPort: route.HostPort, UpstreamScheme: route.UpstreamScheme, Certificate: route.Certificate, CertificateKey: route.CertificateKey})
	}
	if addition != nil {
		desired = append(desired, *addition)
	}
	return desired
}

func appendInternalGatewayRoutes(ctx context.Context, store *domain.Store, serverID string, routes []server.IngressRoute) ([]server.IngressRoute, error) {
	ingresses, err := store.ListInternalIngresses(ctx)
	if err != nil {
		return nil, err
	}
	for _, ingress := range ingresses {
		if ingress.ServerID == serverID && ingress.Status == "ready" {
			routes = append(routes, server.IngressRoute{FQDN: ingress.FQDN, HostIP: "127.0.0.1", HostPort: ingress.GatewayPort, Certificate: ingress.CertificatePath, CertificateKey: ingress.CertificateKeyPath})
		}
	}
	return routes, nil
}

func (s *SystemService) PlanSharedPathRoute(id string) (SharedPathPlan, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	store, err := s.domainStore()
	if err != nil {
		return SharedPathPlan{}, err
	}
	v, err := store.GetPathRoute(ctx, id)
	if err != nil {
		return SharedPathPlan{}, err
	}
	if v.Status != "draft" {
		return SharedPathPlan{}, errors.New("请先保存路径草稿")
	}
	plan := SharedPathPlan{RouteID: id, UpdatedAt: v.UpdatedAt}
	deployments, err := s.deploymentStore()
	if err != nil {
		return plan, err
	}
	target, err := deployments.Get(ctx, v.DeploymentID)
	if err != nil {
		return plan, err
	}
	discovered, err := s.DiscoverDeploymentEndpoints(v.DeploymentID)
	if err != nil {
		return plan, err
	}
	endpoint, err := selectBindingEndpoint(domain.Binding{ServiceName: v.ServiceName, ContainerPort: v.ContainerPort, UpstreamProtocol: "http"}, discovered)
	if err != nil {
		plan.Detail = err.Error()
		return plan, nil
	}
	servers, err := s.serverStore()
	if err != nil {
		return plan, err
	}
	connection, err := servers.Get(ctx, target.ServerID)
	if err != nil {
		return plan, err
	}
	remote := server.NewRemote()
	if err = remote.ProbeIngressBackendPath(ctx, connection, endpoint.HostIP, endpoint.HostPort, v.FQDN, v.PathPrefix); err != nil {
		plan.Detail = err.Error()
		return plan, nil
	}
	state, err := remote.InspectManagedIngress(ctx, connection)
	if err != nil {
		plan.Detail = err.Error()
		return plan, nil
	}
	if state.Exists {
		plan.Detail = "服务器已有受管 Caddy，路径路由需要现有 Nginx"
		return plan, nil
	}
	ingress, ingressErr := store.GetIngress(ctx, target.ServerID)
	if ingressErr != nil && !errors.Is(ingressErr, domain.ErrNotFound) {
		return plan, ingressErr
	}
	if ingressErr == nil && !strings.HasPrefix(ingress.ImageRef, "nginx:") {
		plan.Detail = "服务器入口不是现有 Nginx"
		return plan, nil
	}
	routes, err := store.ListServerRoutes(ctx, target.ServerID)
	if err != nil {
		return plan, err
	}
	addition := pathIngressRoute(v, endpoint)
	desired := pathDesiredRoutes(routes, id, &addition)
	desired, err = appendInternalGatewayRoutes(ctx, store, target.ServerID, desired)
	if err != nil {
		return plan, err
	}
	item, _, preview, err := remote.PreviewExistingNginx(ctx, connection, desired, ingress.ConfigHash)
	if err != nil {
		plan.Detail = err.Error()
		return plan, nil
	}
	if ingressErr == nil && ingress.ImageRef != "nginx:"+item.Container {
		plan.Detail = "Nginx 容器与已保存入口不一致"
		return plan, nil
	}
	plan.IngressConfigHash = item.Hash
	plan.IngressPreview = preview
	plan.CanApply = true
	plan.Detail = "预检通过。应用时将备份、校验并安全重载 Nginx；请确认应用支持完整的 " + v.PathPrefix + " 路径。"
	return plan, nil
}

func (s *SystemService) ApplySharedPathRoute(id, updatedAt, hash string) (JobView, error) {
	if len(hash) != 64 {
		return JobView{}, errors.New("请重新预检 Nginx 配置")
	}
	store, err := s.domainStore()
	if err != nil {
		return JobView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, err := store.GetPathRoute(ctx, id)
	if err != nil {
		return JobView{}, err
	}
	if v.Status != "draft" || v.UpdatedAt != updatedAt {
		return JobView{}, errors.New("路径草稿已变化，请重新预检")
	}
	deployments, err := s.deploymentStore()
	if err != nil {
		return JobView{}, err
	}
	target, err := deployments.Get(ctx, v.DeploymentID)
	if err != nil {
		return JobView{}, err
	}
	queued, err := s.enqueueServerJob(target.ServerID, "domain.path.apply", struct {
		ID   string `json:"id"`
		Hash string `json:"hash"`
	}{id, hash})
	if err != nil {
		return JobView{}, err
	}
	if err = store.SetPathRouteJob(ctx, id, queued.ID, "applying"); err != nil {
		return JobView{}, err
	}
	return queued, nil
}

func (s *SystemService) RemoveSharedPathRoute(id string) (JobView, error) {
	store, err := s.domainStore()
	if err != nil {
		return JobView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, err := store.GetPathRoute(ctx, id)
	if err != nil {
		return JobView{}, err
	}
	if v.Status != "active" {
		return JobView{}, errors.New("只有已应用的路径可以解除")
	}
	deployments, err := s.deploymentStore()
	if err != nil {
		return JobView{}, err
	}
	target, err := deployments.Get(ctx, v.DeploymentID)
	if err != nil {
		return JobView{}, err
	}
	queued, err := s.enqueueServerJob(target.ServerID, "domain.path.remove", struct {
		ID string `json:"id"`
	}{id})
	if err != nil {
		return JobView{}, err
	}
	if err = store.SetPathRouteJob(ctx, id, queued.ID, "removing"); err != nil {
		return JobView{}, err
	}
	return queued, nil
}

func (s *SystemService) sharedPathHandler(ctx context.Context, task job.Job, reporter job.Reporter) (handlerErr error) {
	var p struct {
		ID   string `json:"id"`
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal([]byte(task.Payload), &p); err != nil {
		return err
	}
	store, err := s.domainStore()
	if err != nil {
		return err
	}
	var v domain.PathRoute
	for attempt := 0; attempt < 40; attempt++ {
		v, err = store.GetPathRoute(ctx, p.ID)
		if err != nil {
			return err
		}
		if v.JobID == task.ID {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	if v.JobID != task.ID {
		return errors.New("路径任务已变化")
	}
	removing := task.Type == "domain.path.remove"
	if !removing && v.Status == "active" {
		return nil
	}
	defer func() {
		if handlerErr != nil {
			fallback := "draft"
			if removing {
				fallback = "active"
			}
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = store.SetPathRouteFailure(cleanup, p.ID, task.ID, fallback, handlerErr.Error())
		}
	}()
	deployments, err := s.deploymentStore()
	if err != nil {
		return err
	}
	target, err := deployments.Get(ctx, v.DeploymentID)
	if err != nil {
		return err
	}
	servers, err := s.serverStore()
	if err != nil {
		return err
	}
	connection, err := servers.Get(ctx, target.ServerID)
	if err != nil {
		return err
	}
	remote := server.NewRemote()
	reporter.Progress(10, "检查应用服务和 Nginx")
	var addition *server.IngressRoute
	var endpoint DomainEndpoint
	if !removing {
		discovered, err := s.DiscoverDeploymentEndpoints(v.DeploymentID)
		if err != nil {
			return err
		}
		endpoint, err = selectBindingEndpoint(domain.Binding{ServiceName: v.ServiceName, ContainerPort: v.ContainerPort, UpstreamProtocol: "http"}, discovered)
		if err != nil {
			return err
		}
		if err = remote.ProbeIngressBackendPath(ctx, connection, endpoint.HostIP, endpoint.HostPort, v.FQDN, v.PathPrefix); err != nil {
			return err
		}
		route := pathIngressRoute(v, endpoint)
		addition = &route
	}
	ingress, ingressErr := store.GetIngress(ctx, target.ServerID)
	if ingressErr != nil && !errors.Is(ingressErr, domain.ErrNotFound) {
		return ingressErr
	}
	if ingressErr == nil && !strings.HasPrefix(ingress.ImageRef, "nginx:") {
		return errors.New("服务器入口不是现有 Nginx")
	}
	routes, err := store.ListServerRoutes(ctx, target.ServerID)
	if err != nil {
		return err
	}
	desired := pathDesiredRoutes(routes, p.ID, addition)
	desired, err = appendInternalGatewayRoutes(ctx, store, target.ServerID, desired)
	if err != nil {
		return err
	}
	item, err := remote.InspectExistingNginx(ctx, connection)
	if err != nil {
		return err
	}
	if !removing && item.Hash != p.Hash {
		return errors.New("Nginx 配置与预览时不同，请重新预检")
	}
	if ingressErr == nil && ingress.ImageRef != "nginx:"+item.Container {
		return errors.New("Nginx 容器与已保存入口不一致")
	}
	reporter.Progress(40, "备份、校验并应用路径路由")
	hash, err := remote.ApplyExistingNginxWithAgent(ctx, connection, target.ServerID, task.ID, desired, ingress.ConfigHash, item.Container)
	if err != nil {
		return err
	}
	reporter.Progress(85, "保存入口状态")
	commit, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if removing {
		err = store.MarkPathRouteRemoved(commit, p.ID, target.ServerID, task.ID, hash, "nginx:"+item.Container, len(desired) == 0)
	} else {
		err = store.MarkPathRouteApplied(commit, p.ID, target.ServerID, task.ID, endpoint.HostIP, endpoint.HostPort, hash, "nginx:"+item.Container)
	}
	if err != nil {
		return err
	}
	reporter.Progress(100, "共享域名路径入口已更新")
	return nil
}
