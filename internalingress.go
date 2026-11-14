package main

import (
	"context"
	"encoding/json"
	"errors"
	"ideploy/internal/domain"
	"ideploy/internal/job"
	"ideploy/internal/server"
	"net"
	"strconv"
	"strings"
	"time"
)

type InternalIngressPlan struct {
	FQDN              string `json:"fqdn"`
	CanDeploy         bool   `json:"canDeploy"`
	Detail            string `json:"detail"`
	IngressPreview    string `json:"ingressPreview"`
	IngressConfigHash string `json:"ingressConfigHash"`
	NginxContainer    string `json:"nginxContainer"`
}

type InternalIngressInspection struct {
	FQDN           string `json:"fqdn"`
	Status         string `json:"status"`
	Detail         string `json:"detail"`
	CanRepair      bool   `json:"canRepair"`
	ConfigHash     string `json:"configHash"`
	NginxContainer string `json:"nginxContainer"`
}

func (s *SystemService) InspectInternalIngress(fqdn string) (InternalIngressInspection, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	return s.inspectInternalIngress(ctx, fqdn)
}

func (s *SystemService) inspectInternalIngress(ctx context.Context, fqdn string) (InternalIngressInspection, error) {
	view := InternalIngressInspection{FQDN: strings.ToLower(strings.TrimSpace(fqdn)), Status: "unknown"}
	store, err := s.domainStore()
	if err != nil {
		return view, err
	}
	entry, err := store.GetInternalIngress(ctx, view.FQDN)
	if err != nil {
		return view, err
	}
	servers, err := s.serverStore()
	if err != nil {
		return view, err
	}
	connection, err := servers.Get(ctx, entry.ServerID)
	if err != nil {
		return view, err
	}
	remote := server.NewRemote()
	if err := remote.ProbeIngressBackendPath(ctx, connection, "127.0.0.1", entry.GatewayPort, entry.FQDN, "/"); err != nil {
		view.Status = "failed"
		view.Detail = "入口服务不可达：" + err.Error()
		return view, nil
	}
	routes, err := store.ListServerRoutes(ctx, entry.ServerID)
	if err != nil {
		return view, err
	}
	desired := pathDesiredRoutes(routes, "", nil)
	desired, err = appendInternalGatewayRoutes(ctx, store, entry.ServerID, desired)
	if err != nil {
		return view, err
	}
	if entry.Status != "ready" {
		desired = append(desired, server.IngressRoute{FQDN: entry.FQDN, HostIP: "127.0.0.1", HostPort: entry.GatewayPort, Certificate: entry.CertificatePath, CertificateKey: entry.CertificateKeyPath})
	}
	item, err := remote.InspectExistingNginx(ctx, connection)
	if err != nil {
		view.Status = "failed"
		view.Detail = err.Error()
		return view, nil
	}
	expected, _, err := server.NginxConfigWithRoutes(item.Config, desired)
	if err != nil {
		view.Status = "drift"
		view.Detail = err.Error()
		return view, nil
	}
	if expected != item.Config {
		view.Status = "drift"
		view.Detail = "远端 Nginx 配置与本地入口路由不一致"
		return view, nil
	}
	if err := remote.ProbeNginxRoute(ctx, connection, entry.FQDN); err != nil {
		view.Status = "failed"
		view.Detail = err.Error()
		return view, nil
	}
	if err := remote.ProbeNginxHTTPSRoute(ctx, connection, entry.FQDN); err != nil {
		view.Status = "failed"
		view.Detail = err.Error()
		return view, nil
	}
	view.ConfigHash = item.Hash
	view.NginxContainer = item.Container
	if entry.Status == "failed" {
		view.Status = "recoverable"
		view.CanRepair = true
		view.Detail = "远端入口与配置一致，可恢复本地就绪状态"
		return view, nil
	}
	if entry.Status == "ready" {
		stored, err := store.GetIngress(ctx, entry.ServerID)
		if err != nil || stored.ConfigHash != item.Hash || stored.ImageRef != "nginx:"+item.Container {
			view.Status = "drift"
			view.Detail = "远端入口可用，但本地 Nginx 版本记录不一致"
			return view, nil
		}
		view.Status = "ready"
		view.Detail = "入口服务和 Nginx 路由均已验证"
		return view, nil
	}
	view.Status = entry.Status
	view.Detail = "远端路由已存在，入口任务仍需完成或对账"
	return view, nil
}

func (s *SystemService) RepairInternalIngress(fqdn string) (domain.InternalIngress, error) {
	inspection, err := s.InspectInternalIngress(fqdn)
	if err != nil {
		return domain.InternalIngress{}, err
	}
	if !inspection.CanRepair {
		return domain.InternalIngress{}, errors.New("远端入口尚未满足安全对账条件")
	}
	store, err := s.domainStore()
	if err != nil {
		return domain.InternalIngress{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	entry, err := store.GetInternalIngress(ctx, inspection.FQDN)
	if err != nil {
		return domain.InternalIngress{}, err
	}
	if err := store.ReconcileInternalIngressReady(ctx, entry.FQDN, entry.ServerID, inspection.ConfigHash, inspection.NginxContainer); err != nil {
		return domain.InternalIngress{}, err
	}
	return store.GetInternalIngress(ctx, entry.FQDN)
}

func (s *SystemService) ListInternalIngresses() ([]domain.InternalIngress, error) {
	store, err := s.domainStore()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.ListInternalIngresses(ctx)
}

func (s *SystemService) DeleteInternalIngressDraft(fqdn string) error {
	store, err := s.domainStore()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.DeleteInternalIngressDraft(ctx, fqdn)
}

func (s *SystemService) SaveInternalIngressDraft(input domain.InternalIngress) (domain.InternalIngress, error) {
	input.FQDN = strings.ToLower(strings.TrimSpace(input.FQDN))
	input.ServerID = strings.TrimSpace(input.ServerID)
	input.CertificatePath = strings.TrimSpace(input.CertificatePath)
	input.CertificateKeyPath = strings.TrimSpace(input.CertificateKeyPath)
	input.Webroot = strings.TrimSpace(input.Webroot)
	if input.ServerID == "" {
		return domain.InternalIngress{}, errors.New("请选择入口服务器")
	}
	servers, err := s.serverStore()
	if err != nil {
		return domain.InternalIngress{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := servers.Get(ctx, input.ServerID); err != nil {
		return domain.InternalIngress{}, err
	}
	store, err := s.domainStore()
	if err != nil {
		return domain.InternalIngress{}, err
	}
	if input.GatewayPort == 0 {
		input.GatewayPort = 8766
	}
	others, err := store.ListInternalIngresses(ctx)
	if err != nil {
		return domain.InternalIngress{}, err
	}
	for _, other := range others {
		if other.FQDN != input.FQDN && other.ServerID == input.ServerID && other.GatewayPort == input.GatewayPort {
			return domain.InternalIngress{}, errors.New("此服务器的内部入口端口已被其他域名使用")
		}
	}
	return store.SaveInternalIngressDraft(ctx, input)
}

func (s *SystemService) PlanInternalIngress(fqdn string) (InternalIngressPlan, error) {
	plan := InternalIngressPlan{FQDN: strings.ToLower(strings.TrimSpace(fqdn))}
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	store, err := s.domainStore()
	if err != nil {
		return plan, err
	}
	entry, err := store.GetInternalIngress(ctx, plan.FQDN)
	if err != nil {
		return plan, err
	}
	if entry.CertificatePath == "" || entry.CertificateKeyPath == "" {
		plan.Detail = "请先为域名申请证书并填写 Nginx 容器内的证书和私钥路径"
		return plan, nil
	}
	if addresses, lookupErr := net.DefaultResolver.LookupHost(ctx, entry.FQDN); lookupErr != nil || len(addresses) == 0 {
		plan.Detail = "域名 DNS 尚未解析"
		return plan, nil
	}
	servers, err := s.serverStore()
	if err != nil {
		return plan, err
	}
	connection, err := servers.Get(ctx, entry.ServerID)
	if err != nil {
		return plan, err
	}
	name := "ideploy-apps-gateway-" + strings.ReplaceAll(entry.FQDN, ".", "-")
	if len(name) > 100 {
		plan.Detail = "内部入口容器名称过长"
		return plan, nil
	}
	if err := server.NewRemote().ValidateGatewayOwnership(ctx, connection, name, entry.FQDN); err != nil {
		plan.Detail = err.Error()
		return plan, nil
	}
	routes, err := store.ListServerRoutes(ctx, entry.ServerID)
	if err != nil {
		return plan, err
	}
	desired := pathDesiredRoutes(routes, "", nil)
	desired, err = appendInternalGatewayRoutes(ctx, store, entry.ServerID, desired)
	if err != nil {
		return plan, err
	}
	for _, route := range desired {
		if route.FQDN == entry.FQDN && route.PathPrefix == "" {
			plan.Detail = "此域名已有入口，请先检查或迁移现有入口"
			return plan, nil
		}
	}
	desired = append(desired, server.IngressRoute{FQDN: entry.FQDN, HostIP: "127.0.0.1", HostPort: entry.GatewayPort, Certificate: entry.CertificatePath, CertificateKey: entry.CertificateKeyPath})
	ingress, ingressErr := store.GetIngress(ctx, entry.ServerID)
	if ingressErr != nil && !errors.Is(ingressErr, domain.ErrNotFound) {
		return plan, ingressErr
	}
	if ingressErr == nil && !strings.HasPrefix(ingress.ImageRef, "nginx:") {
		plan.Detail = "服务器入口不是现有 Nginx"
		return plan, nil
	}
	expectedHash := ""
	if ingressErr == nil {
		expectedHash = ingress.ConfigHash
	}
	item, _, preview, err := server.NewRemote().PreviewExistingNginx(ctx, connection, desired, expectedHash)
	if err != nil {
		plan.Detail = err.Error()
		return plan, nil
	}
	if ingressErr == nil && ingress.ImageRef != "nginx:"+item.Container {
		plan.Detail = "Nginx 容器与已保存入口不一致"
		return plan, nil
	}
	plan.CanDeploy = true
	plan.IngressPreview = preview
	plan.IngressConfigHash = item.Hash
	plan.NginxContainer = item.Container
	plan.Detail = "预检通过。将部署内部网关并为此域名应用 HTTPS 入口。"
	return plan, nil
}

func (s *SystemService) DeployInternalIngress(fqdn, expectedHash string) (JobView, error) {
	plan, err := s.PlanInternalIngress(fqdn)
	if err != nil {
		return JobView{}, err
	}
	if !plan.CanDeploy || plan.IngressConfigHash != expectedHash {
		return JobView{}, errors.New("入口预检已变化，请重新预检")
	}
	store, err := s.domainStore()
	if err != nil {
		return JobView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entry, err := store.GetInternalIngress(ctx, plan.FQDN)
	if err != nil {
		return JobView{}, err
	}
	if entry.Status != "draft" && entry.Status != "failed" {
		return JobView{}, errors.New("入口正在部署或已就绪")
	}
	queued, err := s.enqueueServerJob(entry.ServerID, "internal.ingress.deploy", struct {
		FQDN string `json:"fqdn"`
		Hash string `json:"hash"`
	}{plan.FQDN, expectedHash})
	if err != nil {
		return JobView{}, err
	}
	if err := store.SetInternalIngressJob(ctx, plan.FQDN, queued.ID); err != nil {
		return JobView{}, err
	}
	return queued, nil
}

func (s *SystemService) internalIngressDeployHandler(ctx context.Context, task job.Job, reporter job.Reporter) (handlerErr error) {
	var request struct {
		FQDN string `json:"fqdn"`
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal([]byte(task.Payload), &request); err != nil || request.FQDN == "" {
		return errors.New("invalid internal ingress task")
	}
	store, err := s.domainStore()
	if err != nil {
		return err
	}
	defer func() {
		if handlerErr != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = store.SetInternalIngressFailure(cleanup, request.FQDN, task.ID, handlerErr.Error())
		}
	}()
	var entry domain.InternalIngress
	for attempt := 0; attempt < 40; attempt++ {
		entry, err = store.GetInternalIngress(ctx, request.FQDN)
		if err != nil {
			return err
		}
		if entry.JobID == task.ID && entry.Status == "applying" {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	if entry.JobID != task.ID || entry.Status != "applying" {
		return errors.New("入口任务状态已变化")
	}
	ctx, releaseLease, err := s.leaseSharedPublication(ctx, entry.ServerID, task.ID)
	if err != nil {
		return err
	}
	defer releaseLease()
	plan, err := s.PlanInternalIngress(request.FQDN)
	if err != nil {
		return err
	}
	if !plan.CanDeploy || plan.IngressConfigHash != request.Hash {
		return errors.New("入口配置已变化，请重新预检")
	}
	servers, err := s.serverStore()
	if err != nil {
		return err
	}
	connection, err := servers.Get(ctx, entry.ServerID)
	if err != nil {
		return err
	}
	remote := server.NewRemote()
	const image = "nginx:stable-alpine"
	reporter.Progress(15, "拉取内部入口服务镜像")
	if err := remote.PullImageWithAgent(ctx, connection, entry.ServerID, task.ID, image); err != nil {
		return err
	}
	name := "ideploy-apps-gateway-" + strings.ReplaceAll(entry.FQDN, ".", "-")
	if len(name) > 100 {
		return errors.New("内部入口容器名称过长")
	}
	if err := remote.ValidateGatewayOwnership(ctx, connection, name, entry.FQDN); err != nil {
		return err
	}
	reporter.Progress(35, "启动仅本机可访问的内部入口服务")
	replacement, err := remote.DeployContainerWithAgent(ctx, connection, entry.ServerID, task.ID, name, image, "unless-stopped", []string{"127.0.0.1:" + strconv.Itoa(entry.GatewayPort) + ":80"}, map[string]string{"com.ideploy.role": "apps-gateway", "com.ideploy.fqdn": entry.FQDN}, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = remote.RollbackContainerReplacementWithAgent(cleanupCtx, connection, entry.ServerID, task.ID, replacement)
		}
	}()
	if err := remote.ProbeIngressBackendPath(ctx, connection, "127.0.0.1", entry.GatewayPort, entry.FQDN, "/"); err != nil {
		return err
	}
	routes, err := store.ListServerRoutes(ctx, entry.ServerID)
	if err != nil {
		return err
	}
	desired := pathDesiredRoutes(routes, "", nil)
	desired, err = appendInternalGatewayRoutes(ctx, store, entry.ServerID, desired)
	if err != nil {
		return err
	}
	desired = append(desired, server.IngressRoute{FQDN: entry.FQDN, HostIP: "127.0.0.1", HostPort: entry.GatewayPort, Certificate: entry.CertificatePath, CertificateKey: entry.CertificateKeyPath})
	ingress, ingressErr := store.GetIngress(ctx, entry.ServerID)
	if ingressErr != nil && !errors.Is(ingressErr, domain.ErrNotFound) {
		return ingressErr
	}
	expected := ""
	if ingressErr == nil {
		expected = ingress.ConfigHash
	}
	reporter.Progress(65, "应用域名 HTTPS 入口")
	hash, err := remote.ApplyExistingNginxWithAgent(ctx, connection, entry.ServerID, task.ID, desired, expected, plan.NginxContainer)
	if err != nil {
		return err
	}
	// Keep the gateway running once Nginx points at it, even if bookkeeping fails.
	committed = true
	if err := remote.CommitContainerReplacementWithAgent(ctx, connection, entry.ServerID, task.ID, replacement); err != nil {
		return err
	}
	if err := store.MarkInternalIngressReady(ctx, entry.FQDN, entry.ServerID, task.ID, hash, plan.NginxContainer); err != nil {
		return err
	}
	reporter.Progress(100, "内部入口与 HTTPS 已就绪")
	return nil
}
