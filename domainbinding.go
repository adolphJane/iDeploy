package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"ideploy/internal/domain"
	"ideploy/internal/job"
	"ideploy/internal/server"
	"net"
	"strings"
	"time"
)

type DomainBindingPlan struct {
	HostID            string `json:"hostId"`
	FQDN              string `json:"fqdn"`
	DeploymentID      string `json:"deploymentId"`
	ServerID          string `json:"serverId"`
	ContainerName     string `json:"containerName"`
	ContainerPort     int    `json:"containerPort"`
	HostIP            string `json:"hostIp"`
	HostPort          int    `json:"hostPort"`
	BindingUpdatedAt  string `json:"bindingUpdatedAt"`
	ObservedAt        string `json:"observedAt"`
	IngressStatus     string `json:"ingressStatus"`
	IngressMode       string `json:"ingressMode"`
	IngressPreview    string `json:"ingressPreview"`
	IngressConfigHash string `json:"ingressConfigHash"`
	Detail            string `json:"detail"`
	CanApply          bool   `json:"canApply"`
}

func selectBindingEndpoint(binding domain.Binding, discovered DomainEndpointDiscovery) (DomainEndpoint, error) {
	if binding.UpstreamProtocol != "http" {
		return DomainEndpoint{}, errors.New("首期受管入口仅支持 HTTP 后端；请修改绑定草稿")
	}
	var candidates []DomainEndpoint
	for _, item := range discovered.Endpoints {
		if item.ContainerPort != binding.ContainerPort || net.ParseIP(item.HostIP) == nil {
			continue
		}
		if binding.ServiceName != "" && item.ContainerName != binding.ServiceName && !strings.Contains(item.ContainerName, "-"+binding.ServiceName+"-") {
			continue
		}
		candidates = append(candidates, item)
	}
	if len(candidates) == 0 {
		return DomainEndpoint{}, errors.New("所选容器端口没有实际发布的 TCP 映射，请先发布或调整部署")
	}
	first := candidates[0]
	for _, item := range candidates[1:] {
		if item.ContainerName != first.ContainerName || item.HostPort != first.HostPort {
			return DomainEndpoint{}, errors.New("匹配到多个后端，请填写 Compose 服务名或选择唯一容器端口")
		}
		if net.ParseIP(item.HostIP).To4() != nil {
			first = item
		}
	}
	return first, nil
}

func deploymentIngressRoute(fqdn string, endpoint DomainEndpoint, binding domain.Binding) server.IngressRoute {
	return server.IngressRoute{FQDN: fqdn, HostIP: endpoint.HostIP, HostPort: endpoint.HostPort,
		UpstreamScheme: "http", Certificate: binding.CertificatePath, CertificateKey: binding.CertificateKeyPath}
}

func (s *SystemService) planDomainBinding(ctx context.Context, hostID, mode string) (DomainBindingPlan, error) {
	store, err := s.domainStore()
	if err != nil {
		return DomainBindingPlan{}, err
	}
	host, err := store.GetHost(ctx, hostID)
	if err != nil {
		return DomainBindingPlan{}, err
	}
	if host.Binding == nil {
		return DomainBindingPlan{}, errors.New("请先保存绑定草稿")
	}
	b := *host.Binding
	if b.IngressMode != mode {
		return DomainBindingPlan{}, errors.New("入口方式已变化，请保存草稿后重新预检")
	}
	deployments, err := s.deploymentStore()
	if err != nil {
		return DomainBindingPlan{}, err
	}
	target, err := deployments.Get(ctx, b.DeploymentID)
	if err != nil {
		return DomainBindingPlan{}, err
	}
	discovered, err := s.DiscoverDeploymentEndpoints(b.DeploymentID)
	if err != nil {
		return DomainBindingPlan{}, err
	}
	endpoint, err := selectBindingEndpoint(b, discovered)
	if err != nil {
		return DomainBindingPlan{}, err
	}
	plan := DomainBindingPlan{HostID: hostID, FQDN: host.FQDN, DeploymentID: b.DeploymentID,
		ServerID: target.ServerID, ContainerName: endpoint.ContainerName, ContainerPort: endpoint.ContainerPort,
		HostIP: endpoint.HostIP, HostPort: endpoint.HostPort, BindingUpdatedAt: b.UpdatedAt, ObservedAt: discovered.ObservedAt}
	servers, err := s.serverStore()
	if err != nil {
		return plan, err
	}
	connection, err := servers.Get(ctx, target.ServerID)
	if err != nil {
		return plan, err
	}
	remote := server.NewRemote()
	if err := remote.ProbeIngressBackend(ctx, connection, endpoint.HostIP, endpoint.HostPort, host.FQDN); err != nil {
		plan.Detail = err.Error()
		return plan, nil
	}
	state, err := remote.InspectManagedIngress(ctx, connection)
	if err != nil {
		plan.Detail = err.Error()
		return plan, nil
	}
	ingress, err := store.GetIngress(ctx, target.ServerID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return plan, err
	}
	if mode == "nginx" {
		plan.IngressMode = "nginx"
		if state.Exists {
			plan.Detail = "服务器已有 iDeploy 受管 Caddy 容器，不能同时接入现有 Nginx"
			return plan, nil
		}
		if err == nil && !strings.HasPrefix(ingress.ImageRef, "nginx:") {
			plan.Detail = "此服务器已有 iDeploy 受管 Caddy，不能混用 Nginx 入口"
			return plan, nil
		}
		routes, routeErr := store.ListServerRoutes(ctx, target.ServerID)
		if routeErr != nil {
			return plan, routeErr
		}
		desired := make([]server.IngressRoute, 0, len(routes)+1)
		for _, route := range routes {
			if route.HostID != hostID {
				desired = append(desired, server.IngressRoute{FQDN: route.FQDN, HostIP: route.HostIP, HostPort: route.HostPort, UpstreamScheme: route.UpstreamScheme, Certificate: route.Certificate, CertificateKey: route.CertificateKey})
			}
		}
		desired = append(desired, deploymentIngressRoute(host.FQDN, endpoint, b))
		item, _, preview, previewErr := remote.PreviewExistingNginx(ctx, connection, desired, ingress.ConfigHash)
		if previewErr != nil {
			plan.Detail = previewErr.Error()
			return plan, nil
		}
		if err == nil && ingress.ImageRef != "nginx:"+item.Container {
			plan.Detail = "Nginx 容器与本地入口记录不一致，请先核对"
			return plan, nil
		}
		plan.IngressStatus = "existing_nginx"
		plan.IngressPreview = preview
		plan.IngressConfigHash = item.Hash
		plan.Detail = "检测到现有 Nginx（" + item.Container + "）。应用时会备份主配置、写入预览路由、执行 nginx -t 并安全重载。"
		if b.CertificatePath == "" {
			plan.Detail += "当前仅配置 HTTP；如需域名 HTTPS，请先填写匹配证书的容器内路径。"
		} else {
			plan.Detail += "域名 HTTPS 证书已验证；应用后将检查 HTTPS 路由。"
		}
		plan.CanApply = true
		return plan, nil
	}
	if err == nil && strings.HasPrefix(ingress.ImageRef, "nginx:") {
		plan.Detail = "此服务器已使用现有 Nginx 入口，请选择相同模式"
		return plan, nil
	}
	if b.CertificatePath != "" {
		plan.Detail = "已填写现有 Nginx 的证书路径；请选择「使用现有入口代理（Nginx）」进行预检"
		return plan, nil
	}
	if state.Exists && (errors.Is(err, domain.ErrNotFound) || !state.Running || state.Hash != ingress.ConfigHash) {
		routes, routeErr := store.ListServerRoutes(ctx, target.ServerID)
		if routeErr != nil {
			return plan, routeErr
		}
		desired := []server.IngressRoute{}
		for _, route := range routes {
			if route.HostID != hostID {
				desired = append(desired, server.IngressRoute{FQDN: route.FQDN, HostIP: route.HostIP, HostPort: route.HostPort, UpstreamScheme: route.UpstreamScheme, Certificate: route.Certificate, CertificateKey: route.CertificateKey})
			}
		}
		desired = append(desired, deploymentIngressRoute(host.FQDN, endpoint, b))
		config, configErr := server.ManagedCaddyfile(desired)
		if configErr != nil {
			return plan, configErr
		}
		if !state.Running || state.Hash != server.CaddyfileHash(config) {
			plan.Detail = "受管入口与本地记录不一致，请先核对远端配置"
			return plan, nil
		}
		plan.Detail = "发现已应用的目标入口配置；重试将对账本地状态。"
	}
	if !state.Exists && !errors.Is(err, domain.ErrNotFound) {
		plan.Detail = "本地记录了受管入口，但远端容器不存在，请先核对"
		return plan, nil
	}
	if state.Exists {
		plan.IngressStatus = "managed"
	} else {
		plan.IngressStatus = "new"
		if err := remote.CheckIngressPortsFree(ctx, connection); err != nil {
			plan.Detail = err.Error()
			return plan, nil
		}
	}
	plan.CanApply = true
	if plan.Detail == "" {
		plan.Detail = "已确认 HTTP 后端；应用时将重新检查，并配置 iDeploy 受管 Caddy。DNS 与证书状态需单独验证。"
	}
	return plan, nil
}

func (s *SystemService) PlanDomainBinding(hostID string) (DomainBindingPlan, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return s.planDomainBinding(ctx, hostID, "caddy")
}

func (s *SystemService) PlanExistingNginxBinding(hostID string) (DomainBindingPlan, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return s.planDomainBinding(ctx, hostID, "nginx")
}

func (s *SystemService) ApplyDomainBinding(hostID, expectedUpdatedAt string) (JobView, error) {
	return s.applyDomainBinding(hostID, expectedUpdatedAt, "caddy", "")
}

func (s *SystemService) ApplyExistingNginxBinding(hostID, expectedUpdatedAt, expectedIngressHash string) (JobView, error) {
	if len(expectedIngressHash) != 64 {
		return JobView{}, errors.New("Nginx 入口预览已失效，请重新预检")
	}
	return s.applyDomainBinding(hostID, expectedUpdatedAt, "nginx", expectedIngressHash)
}

func (s *SystemService) applyDomainBinding(hostID, expectedUpdatedAt, mode, expectedIngressHash string) (JobView, error) {
	store, err := s.domainStore()
	if err != nil {
		return JobView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := store.GetBindingForHost(ctx, hostID)
	if err != nil {
		return JobView{}, err
	}
	if b.Status != "draft" || b.UpdatedAt != expectedUpdatedAt {
		return JobView{}, errors.New("绑定草稿已变化，请重新预检")
	}
	if b.IngressMode != mode {
		return JobView{}, errors.New("入口方式已变化，请重新预检")
	}
	if mode == "caddy" && b.CertificatePath != "" {
		return JobView{}, errors.New("已填写现有 Nginx 证书，请选择 Nginx 入口模式")
	}
	deployments, err := s.deploymentStore()
	if err != nil {
		return JobView{}, err
	}
	target, err := deployments.Get(ctx, b.DeploymentID)
	if err != nil {
		return JobView{}, err
	}
	queued, err := s.enqueueServerJob(target.ServerID, "domain.binding.apply", struct {
		HostID              string `json:"hostId"`
		Mode                string `json:"mode"`
		ExpectedIngressHash string `json:"expectedIngressHash"`
	}{hostID, mode, expectedIngressHash})
	if err != nil {
		return JobView{}, err
	}
	if err := store.SetBindingJob(ctx, hostID, queued.ID, "applying"); err != nil {
		return JobView{}, err
	}
	s.reconcileFinishedDomainJob(store, hostID, queued.ID, "draft")
	return queued, nil
}

func (s *SystemService) RemoveDomainBinding(hostID string) (JobView, error) {
	store, err := s.domainStore()
	if err != nil {
		return JobView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := store.GetBindingForHost(ctx, hostID)
	if err != nil {
		return JobView{}, err
	}
	if b.Status != "active" {
		return JobView{}, errors.New("只有已应用的绑定才能远端解绑")
	}
	deployments, err := s.deploymentStore()
	if err != nil {
		return JobView{}, err
	}
	target, err := deployments.Get(ctx, b.DeploymentID)
	if err != nil {
		return JobView{}, err
	}
	queued, err := s.enqueueServerJob(target.ServerID, "domain.binding.remove", struct {
		HostID string `json:"hostId"`
	}{hostID})
	if err != nil {
		return JobView{}, err
	}
	if err := store.SetBindingJob(ctx, hostID, queued.ID, "removing"); err != nil {
		return JobView{}, err
	}
	s.reconcileFinishedDomainJob(store, hostID, queued.ID, "active")
	return queued, nil
}

func (s *SystemService) reconcileFinishedDomainJob(store *domain.Store, hostID, jobID, fallback string) {
	latest, err := s.GetJob(jobID)
	if err != nil || latest.Status != "failed" && latest.Status != "canceled" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = store.ReconcileFinishedJob(ctx, hostID, jobID, fallback, latest.Error)
}

func (s *SystemService) domainBindingHandler(ctx context.Context, task job.Job, reporter job.Reporter) (handlerErr error) {
	var p struct {
		HostID              string `json:"hostId"`
		Mode                string `json:"mode"`
		ExpectedIngressHash string `json:"expectedIngressHash"`
	}
	if err := json.Unmarshal([]byte(task.Payload), &p); err != nil {
		return err
	}
	store, err := s.domainStore()
	if err != nil {
		return err
	}
	host, err := store.GetHost(ctx, p.HostID)
	if err != nil {
		return err
	}
	removing := task.Type == "domain.binding.remove"
	if host.Binding == nil {
		if removing {
			return nil
		}
		return errors.New("绑定已不存在")
	}
	b := *host.Binding
	if task.Type == "domain.binding.apply" && b.IngressMode != p.Mode {
		return errors.New("绑定入口方式与任务不一致")
	}
	if p.Mode != "nginx" && b.CertificatePath != "" && task.Type == "domain.binding.apply" {
		return errors.New("已填写 Nginx 证书，不能应用受管 Caddy 模式")
	}
	if !removing && b.Status == "active" && b.JobID == task.ID {
		return nil
	}
	defer func() {
		if handlerErr == nil {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		status := "draft"
		if removing {
			status = "active"
		}
		_ = store.SetBindingFailure(cleanupCtx, p.HostID, status, handlerErr.Error())
	}()
	deployments, err := s.deploymentStore()
	if err != nil {
		return err
	}
	target, err := deployments.Get(ctx, b.DeploymentID)
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
	if !removing && task.Type != "domain.binding.apply" {
		return errors.New("invalid domain job")
	}
	reporter.Progress(10, "重新核对部署与入口")
	var endpoint DomainEndpoint
	if !removing {
		discovered, err := s.DiscoverDeploymentEndpoints(b.DeploymentID)
		if err != nil {
			return err
		}
		endpoint, err = selectBindingEndpoint(b, discovered)
		if err != nil {
			return err
		}
		if err := remote.ProbeIngressBackend(ctx, connection, endpoint.HostIP, endpoint.HostPort, host.FQDN); err != nil {
			return err
		}
	}
	routes, err := store.ListServerRoutes(ctx, target.ServerID)
	if err != nil {
		return err
	}
	var desired []server.IngressRoute
	for _, route := range routes {
		if route.HostID == p.HostID {
			continue
		}
		desired = append(desired, server.IngressRoute{FQDN: route.FQDN, HostIP: route.HostIP, HostPort: route.HostPort, UpstreamScheme: route.UpstreamScheme, Certificate: route.Certificate, CertificateKey: route.CertificateKey})
	}
	if !removing {
		desired = append(desired, deploymentIngressRoute(host.FQDN, endpoint, b))
	}
	ingress, err := store.GetIngress(ctx, target.ServerID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	reporter.Progress(40, "校验并应用受管入口配置")
	hash := ""
	imageRef := server.ManagedCaddyImage
	if removing && strings.HasPrefix(ingress.ImageRef, "nginx:") {
		p.Mode = "nginx"
	}
	if p.Mode == "nginx" {
		state, inspectErr := remote.InspectManagedIngress(ctx, connection)
		if inspectErr != nil {
			return inspectErr
		}
		if state.Exists {
			return errors.New("服务器已有 iDeploy 受管 Caddy 容器，不能混用 Nginx")
		}
		if !removing {
			item, inspectErr := remote.InspectExistingNginx(ctx, connection)
			if inspectErr != nil {
				return inspectErr
			}
			if item.Hash != p.ExpectedIngressHash {
				return errors.New("Nginx 主配置与预览时不同，请重新预检")
			}
		}
		if err == nil && !strings.HasPrefix(ingress.ImageRef, "nginx:") {
			return errors.New("此服务器已有受管 Caddy，不能混用 Nginx 入口")
		}
		container := strings.TrimPrefix(ingress.ImageRef, "nginx:")
		if errors.Is(err, domain.ErrNotFound) {
			container = ""
		}
		hash, err = remote.ApplyExistingNginxWithAgent(ctx, connection, target.ServerID, task.ID, desired, ingress.ConfigHash, container)
		if err != nil {
			return err
		}
		if container == "" {
			item, inspectErr := remote.InspectExistingNginx(ctx, connection)
			if inspectErr != nil {
				return inspectErr
			}
			container = item.Container
		}
		imageRef = "nginx:" + container
	} else if removing && len(desired) == 0 {
		if err := remote.RemoveManagedIngress(ctx, connection, ingress.ConfigHash); err != nil {
			return err
		}
	} else {
		if err == nil && strings.HasPrefix(ingress.ImageRef, "nginx:") {
			return errors.New("此服务器已使用 Nginx 入口")
		}
		config, err := server.ManagedCaddyfile(desired)
		if err != nil {
			return err
		}
		hash, err = remote.ApplyManagedIngress(ctx, connection, config, ingress.ConfigHash)
		if err != nil {
			return err
		}
	}
	reporter.Progress(85, "记录入口配置版本")
	commitCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if removing {
		if err := store.MarkRemoved(commitCtx, p.HostID, target.ServerID, imageRef, hash, len(desired) == 0); err != nil {
			return fmt.Errorf("入口已更新，本地解绑待恢复: %w", err)
		}
	} else {
		if err := store.MarkApplied(commitCtx, p.HostID, target.ServerID, endpoint.HostIP, endpoint.HostPort, imageRef, hash, task.ID); err != nil {
			return fmt.Errorf("入口已更新，本地状态待恢复: %w", err)
		}
	}
	if !removing {
		access, checkErr := s.InspectDomainAccess(p.HostID)
		if checkErr == nil {
			reporter.Progress(100, fmt.Sprintf("入口已应用；DNS：%s，HTTPS：%s，后端：%s。%s", access.DNSStatus, access.HTTPSStatus, access.BackendStatus, access.Detail))
		} else {
			reporter.Progress(100, "入口已应用；访问检查暂未完成："+checkErr.Error())
		}
	} else {
		reporter.Progress(100, "入口路由已解除")
	}
	return nil
}
