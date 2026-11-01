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

type ExternalRoutePlan struct {
	HostID            string `json:"hostId"`
	FQDN              string `json:"fqdn"`
	UpdatedAt         string `json:"updatedAt"`
	IngressConfigHash string `json:"ingressConfigHash"`
	IngressPreview    string `json:"ingressPreview"`
	Detail            string `json:"detail"`
	CanApply          bool   `json:"canApply"`
}

func externalIngressRoute(fqdn string, v domain.ExternalRoute) server.IngressRoute {
	return server.IngressRoute{FQDN: fqdn, HostIP: v.HostIP, HostPort: v.HostPort, UpstreamScheme: v.UpstreamScheme, Certificate: v.CertificatePath, CertificateKey: v.CertificateKeyPath}
}

func (s *SystemService) SaveExternalRouteDraft(v domain.ExternalRoute) (domain.ExternalRoute, error) {
	if v.HostID == "" || v.ServerID == "" || net.ParseIP(v.HostIP) == nil || v.HostPort < 1 || v.HostPort > 65535 || v.UpstreamScheme != "http" {
		return domain.ExternalRoute{}, errors.New("请选择服务器、HTTP 服务及有效的后端 IP 和端口")
	}
	if (v.CertificatePath == "") != (v.CertificateKeyPath == "") {
		return domain.ExternalRoute{}, errors.New("HTTPS 证书与私钥路径必须同时填写")
	}
	if v.CertificatePath != "" {
		if _, err := server.NginxManagedBlock([]server.IngressRoute{externalIngressRoute("check.example.com", v)}); err != nil {
			return domain.ExternalRoute{}, err
		}
	}
	servers, err := s.serverStore()
	if err != nil {
		return domain.ExternalRoute{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err = servers.Get(ctx, v.ServerID); err != nil {
		return domain.ExternalRoute{}, err
	}
	store, err := s.domainStore()
	if err != nil {
		return domain.ExternalRoute{}, err
	}
	return store.SaveExternalRouteDraft(ctx, v)
}

func (s *SystemService) PlanExternalRoute(hostID string) (ExternalRoutePlan, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	store, err := s.domainStore()
	if err != nil {
		return ExternalRoutePlan{}, err
	}
	host, err := store.GetHost(ctx, hostID)
	if err != nil {
		return ExternalRoutePlan{}, err
	}
	if host.Binding != nil {
		return ExternalRoutePlan{}, errors.New("此域名已有部署绑定")
	}
	if host.ExternalRoute == nil || host.ExternalRoute.Status != "draft" {
		return ExternalRoutePlan{}, errors.New("请先保存服务器服务草稿")
	}
	v := *host.ExternalRoute
	plan := ExternalRoutePlan{HostID: hostID, FQDN: host.FQDN, UpdatedAt: v.UpdatedAt}
	servers, err := s.serverStore()
	if err != nil {
		return plan, err
	}
	connection, err := servers.Get(ctx, v.ServerID)
	if err != nil {
		return plan, err
	}
	remote := server.NewRemote()
	state, err := remote.InspectManagedIngress(ctx, connection)
	if err != nil {
		return plan, err
	}
	if state.Exists {
		plan.Detail = "此服务器已有 iDeploy 受管 Caddy，不能混用 Nginx"
		return plan, nil
	}
	ingress, ingressErr := store.GetIngress(ctx, v.ServerID)
	if ingressErr != nil && !errors.Is(ingressErr, domain.ErrNotFound) {
		return plan, ingressErr
	}
	if ingressErr == nil && !strings.HasPrefix(ingress.ImageRef, "nginx:") {
		plan.Detail = "此服务器使用受管 Caddy，不能混用 Nginx"
		return plan, nil
	}
	if err := remote.ProbeIngressBackend(ctx, connection, v.HostIP, v.HostPort, host.FQDN); err != nil {
		plan.Detail = err.Error()
		return plan, nil
	}
	routes, err := store.ListServerRoutes(ctx, v.ServerID)
	if err != nil {
		return plan, err
	}
	desired := make([]server.IngressRoute, 0, len(routes)+1)
	for _, route := range routes {
		if route.HostID != hostID {
			desired = append(desired, server.IngressRoute{FQDN: route.FQDN, HostIP: route.HostIP, HostPort: route.HostPort, UpstreamScheme: route.UpstreamScheme, Certificate: route.Certificate, CertificateKey: route.CertificateKey})
		}
	}
	desired = append(desired, externalIngressRoute(host.FQDN, v))
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
	if v.CertificatePath == "" {
		plan.Detail = "HTTP 路由可应用。尚无覆盖该域名的证书；配置证书路径后可启用 HTTPS。"
	} else {
		plan.Detail = "HTTP 将跳转 HTTPS；现有证书已通过域名和有效期校验。应用时会备份、校验并安全重载 Nginx。"
	}
	return plan, nil
}

func (s *SystemService) ApplyExternalRoute(hostID, expectedUpdatedAt, expectedHash string) (JobView, error) {
	if len(expectedHash) != 64 {
		return JobView{}, errors.New("请重新预检 Nginx 配置")
	}
	store, err := s.domainStore()
	if err != nil {
		return JobView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	route, err := store.GetExternalRoute(ctx, hostID)
	if err != nil {
		return JobView{}, err
	}
	if route.Status != "draft" || route.UpdatedAt != expectedUpdatedAt {
		return JobView{}, errors.New("服务草稿已变化，请重新预检")
	}
	queued, err := s.enqueueServerJob(route.ServerID, "domain.external.apply", struct {
		HostID string `json:"hostId"`
		Hash   string `json:"hash"`
	}{hostID, expectedHash})
	if err != nil {
		return JobView{}, err
	}
	if err = store.SetExternalRouteJob(ctx, hostID, queued.ID, "applying"); err != nil {
		return JobView{}, err
	}
	return queued, nil
}

func (s *SystemService) RemoveExternalRoute(hostID string) (JobView, error) {
	store, err := s.domainStore()
	if err != nil {
		return JobView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	route, err := store.GetExternalRoute(ctx, hostID)
	if err != nil {
		return JobView{}, err
	}
	if route.Status != "active" {
		return JobView{}, errors.New("只有已应用的路由可以解除")
	}
	queued, err := s.enqueueServerJob(route.ServerID, "domain.external.remove", struct {
		HostID string `json:"hostId"`
	}{hostID})
	if err != nil {
		return JobView{}, err
	}
	if err = store.SetExternalRouteJob(ctx, hostID, queued.ID, "removing"); err != nil {
		return JobView{}, err
	}
	return queued, nil
}

func (s *SystemService) DeleteExternalRouteDraft(hostID string) error {
	store, err := s.domainStore()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	route, err := store.GetExternalRoute(ctx, hostID)
	if err != nil {
		return err
	}
	if route.Status != "draft" {
		return errors.New("只有草稿可以删除")
	}
	return store.DeleteExternalRouteDraft(ctx, hostID)
}

func (s *SystemService) externalRouteHandler(ctx context.Context, task job.Job, reporter job.Reporter) (handlerErr error) {
	var p struct {
		HostID string `json:"hostId"`
		Hash   string `json:"hash"`
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
	// The runner may claim a job just before the enqueue caller stores its job ID.
	for attempt := 0; attempt < 40 && host.ExternalRoute != nil && host.ExternalRoute.JobID != task.ID; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
		host, err = store.GetHost(ctx, p.HostID)
		if err != nil {
			return err
		}
	}
	if host.ExternalRoute == nil {
		return errors.New("路由已不存在")
	}
	v := *host.ExternalRoute
	removing := task.Type == "domain.external.remove"
	if !removing && v.Status == "active" && v.JobID == task.ID {
		return nil
	}
	if v.JobID != task.ID {
		return errors.New("路由任务已变化")
	}
	defer func() {
		if handlerErr != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			fallback := "draft"
			if removing {
				fallback = "active"
			}
			_ = store.SetExternalRouteFailure(cleanup, p.HostID, task.ID, fallback, handlerErr.Error())
		}
	}()
	servers, err := s.serverStore()
	if err != nil {
		return err
	}
	connection, err := servers.Get(ctx, v.ServerID)
	if err != nil {
		return err
	}
	remote := server.NewRemote()
	reporter.Progress(10, "重新核对 Nginx 和服务")
	if !removing {
		if err := remote.ProbeIngressBackend(ctx, connection, v.HostIP, v.HostPort, host.FQDN); err != nil {
			return err
		}
	}
	ingress, err := store.GetIngress(ctx, v.ServerID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if err == nil && !strings.HasPrefix(ingress.ImageRef, "nginx:") {
		return errors.New("服务器入口不是现有 Nginx")
	}
	routes, err := store.ListServerRoutes(ctx, v.ServerID)
	if err != nil {
		return err
	}
	desired := make([]server.IngressRoute, 0, len(routes)+1)
	for _, route := range routes {
		if route.HostID != p.HostID {
			desired = append(desired, server.IngressRoute{FQDN: route.FQDN, HostIP: route.HostIP, HostPort: route.HostPort, UpstreamScheme: route.UpstreamScheme, Certificate: route.Certificate, CertificateKey: route.CertificateKey})
		}
	}
	if !removing {
		desired = append(desired, externalIngressRoute(host.FQDN, v))
	}
	item, inspectErr := remote.InspectExistingNginx(ctx, connection)
	if inspectErr != nil {
		return inspectErr
	}
	if !removing && item.Hash != p.Hash {
		return errors.New("Nginx 配置与预览时不同，请重新预检")
	}
	if err == nil && ingress.ImageRef != "nginx:"+item.Container {
		return errors.New("Nginx 容器与已保存入口不一致")
	}
	reporter.Progress(40, "备份、校验并应用 Nginx 路由")
	hash, err := remote.ApplyExistingNginxWithAgent(ctx, connection, v.ServerID, task.ID, desired, ingress.ConfigHash, item.Container)
	if err != nil {
		return err
	}
	reporter.Progress(85, "保存入口配置版本")
	commit, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if removing {
		err = store.MarkExternalRouteRemoved(commit, p.HostID, v.ServerID, hash, "nginx:"+item.Container, len(desired) == 0)
	} else {
		err = store.MarkExternalRouteApplied(commit, p.HostID, v.ServerID, hash, "nginx:"+item.Container, task.ID)
	}
	if err != nil {
		return fmt.Errorf("入口已更新，本地状态待恢复: %w", err)
	}
	if !removing {
		if access, checkErr := s.InspectDomainAccess(p.HostID); checkErr == nil {
			reporter.Progress(100, fmt.Sprintf("入口已应用；DNS：%s，HTTPS：%s，后端：%s。%s", access.DNSStatus, access.HTTPSStatus, access.BackendStatus, access.Detail))
		} else {
			reporter.Progress(100, "入口已应用；访问检查暂未完成："+checkErr.Error())
		}
	} else {
		reporter.Progress(100, "入口路由已解除")
	}
	return nil
}
