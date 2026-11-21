package main

import (
	"context"
	"errors"
	"fmt"
	"ideploy/internal/deployment"
	"ideploy/internal/domain"
	"ideploy/internal/server"
	"strconv"
	"strings"
)

type appliedIngressStateError struct{ cause error }

// releaseBasePath reads the immutable path selected for this release. Rollbacks
// reuse the path of their source artifact so the image and runtime agree.
func (s *SystemService) releaseBasePath(ctx context.Context, release deployment.Release) (string, error) {
	store, err := s.domainStore()
	if err != nil {
		return "", err
	}
	intent, err := store.GetReleasePathIntent(ctx, release.ID)
	if errors.Is(err, domain.ErrNotFound) && release.RollbackToID != "" {
		intent, err = store.GetReleasePathIntent(ctx, release.RollbackToID)
	}
	if errors.Is(err, domain.ErrNotFound) {
		return "", nil // releases created before path publication have no path snapshot
	}
	if err != nil {
		return "", fmt.Errorf("读取发布路径: %w", err)
	}
	return domain.NormalizeSharedPath(intent.PathPrefix)
}

func (s *SystemService) validateRollbackBasePath(ctx context.Context, source deployment.Release) error {
	basePath, err := s.releaseBasePath(ctx, source)
	if err != nil || basePath == "" {
		return err
	}
	store, err := s.domainStore()
	if err != nil {
		return err
	}
	routes, err := store.ListPathRoutes(ctx)
	if err != nil {
		return err
	}
	for _, route := range routes {
		if route.DeploymentID == source.DeploymentID && route.Status == "active" && route.PathPrefix != basePath {
			return fmt.Errorf("历史版本使用路径 %s，当前入口使用 %s；请发布适配当前路径的新版本", basePath, route.PathPrefix)
		}
	}
	return nil
}

func (e appliedIngressStateError) Error() string {
	return "Nginx 路径已生效，但本地状态保存失败: " + e.cause.Error()
}
func (e appliedIngressStateError) Unwrap() error { return e.cause }

func configuredContainerPortPublished(mappings []string, port int) bool {
	for _, mapping := range mappings {
		protocol := "tcp"
		if parts := strings.SplitN(mapping, "/", 2); len(parts) == 2 {
			mapping, protocol = parts[0], parts[1]
		}
		if protocol != "tcp" {
			continue
		}
		parts := strings.Split(mapping, ":")
		if len(parts) < 2 {
			continue
		}
		candidate, err := strconv.Atoi(parts[len(parts)-1])
		if err == nil && candidate == port {
			return true
		}
	}
	return false
}

func (s *SystemService) validateReleaseContainerPort(ctx context.Context, releaseID string, mappings []string) error {
	store, err := s.domainStore()
	if err != nil {
		return err
	}
	intent, err := store.GetReleasePathIntent(ctx, releaseID)
	if err != nil {
		return err
	}
	if !configuredContainerPortPublished(mappings, intent.ContainerPort) {
		return errors.New("发布路径选择的容器 HTTP 端口未在部署端口映射中发布")
	}
	return nil
}

// applyReleasePath runs inside the server-locked release job, after the new workload is healthy.
func (s *SystemService) applyReleasePath(ctx context.Context, releaseID, jobID string, targetServer server.Connection) error {
	return s.applyReleasePathObservingIngress(ctx, releaseID, jobID, targetServer, nil)
}

func (s *SystemService) applyReleasePathObservingIngress(ctx context.Context, releaseID, jobID string, targetServer server.Connection, onApply func(server.ExistingNginx)) error {
	store, err := s.domainStore()
	if err != nil {
		return err
	}
	intent, err := store.GetReleasePathIntent(ctx, releaseID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	} // historical rollbacks have no new path choice
	if err != nil {
		return err
	}
	ingress, err := store.GetInternalIngress(ctx, intent.FQDN)
	if err != nil {
		return err
	}
	if ingress.Status != "ready" || ingress.ServerID != targetServer.ID {
		return errors.New("内部 HTTPS 入口已变化，请重新配置后重试发布")
	}
	discovered, err := s.DiscoverDeploymentEndpoints(intent.DeploymentID)
	if err != nil {
		return err
	}
	endpoint, err := selectBindingEndpoint(domain.Binding{ServiceName: intent.ServiceName, ContainerPort: intent.ContainerPort, UpstreamProtocol: "http"}, discovered)
	if err != nil {
		return err
	}
	remote := server.NewRemote()
	if err := remote.ProbeIngressBackendPath(ctx, targetServer, endpoint.HostIP, endpoint.HostPort, intent.FQDN, intent.PathPrefix); err != nil {
		return fmt.Errorf("发布路径后端检查失败: %w", err)
	}
	routes, err := store.ListServerRoutes(ctx, targetServer.ID)
	if err != nil {
		return err
	}
	var oldID string
	for _, route := range routes {
		if route.FQDN == intent.FQDN && route.PathPrefix == intent.PathPrefix {
			oldID = route.HostID
		}
	}
	addition := server.IngressRoute{FQDN: intent.FQDN, PathPrefix: intent.PathPrefix, HostIP: endpoint.HostIP, HostPort: endpoint.HostPort, UpstreamScheme: "http", Certificate: ingress.CertificatePath, CertificateKey: ingress.CertificateKeyPath}
	desired := pathDesiredRoutes(routes, oldID, &addition)
	desired, err = appendInternalGatewayRoutes(ctx, store, targetServer.ID, desired)
	if err != nil {
		return err
	}
	current, err := store.GetIngress(ctx, targetServer.ID)
	if err != nil {
		return err
	}
	if current.ImageRef == "" {
		return errors.New("Nginx 入口状态尚未就绪")
	}
	item, err := remote.InspectExistingNginx(ctx, targetServer)
	if err != nil {
		return err
	}
	if current.ImageRef != "nginx:"+item.Container || current.ConfigHash != item.Hash {
		return errors.New("Nginx 配置与已保存版本不一致，请先修复入口")
	}
	if onApply != nil {
		onApply(item)
	}
	hash, err := remote.ApplyExistingNginxWithAgent(ctx, targetServer, targetServer.ID, jobID, desired, current.ConfigHash, item.Container)
	if err != nil {
		return fmt.Errorf("应用发布路径失败: %w", err)
	}
	if err := store.MarkPublishedPathApplied(ctx, intent, endpoint.HostIP, endpoint.HostPort, ingress.CertificatePath, ingress.CertificateKeyPath, hash, "nginx:"+item.Container); err != nil {
		return appliedIngressStateError{err}
	}
	return nil
}

func (s *SystemService) validateReleasePathReady(ctx context.Context, releaseID, serverID string) error {
	store, err := s.domainStore()
	if err != nil {
		return err
	}
	intent, err := store.GetReleasePathIntent(ctx, releaseID)
	if err != nil {
		return fmt.Errorf("发布记录缺少应用路径，请重新创建发布: %w", err)
	}
	ingress, err := store.GetInternalIngress(ctx, intent.FQDN)
	if err != nil || ingress.Status != "ready" || ingress.ServerID != serverID {
		return errors.New("内部 HTTPS 入口尚未就绪，请在设置中完成配置后重试")
	}
	observed, err := s.inspectInternalIngress(ctx, intent.FQDN)
	if err != nil {
		return err
	}
	if observed.Status != "ready" {
		return fmt.Errorf("内部 HTTPS 入口实际状态异常：%s", observed.Detail)
	}
	return nil
}
