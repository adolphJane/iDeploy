package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"ideploy/internal/job"
	"ideploy/internal/server"
)

type serverCertificateRequest struct {
	ServerID       string `json:"serverId"`
	FQDN           string `json:"fqdn"`
	Email          string `json:"email"`
	WebrootHost    string `json:"webrootHost"`
	NginxContainer string `json:"nginxContainer"`
}

func (s *SystemService) CheckServerCertificate(serverID, fqdn, webroot string) (map[string]any, error) {
	servers, err := s.serverStore()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	connection, err := servers.Get(ctx, serverID)
	if err != nil {
		return nil, err
	}
	data, err := server.NewRemote().AgentCertbotCheck(ctx, connection, serverID, fqdn, webroot)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *SystemService) IssueServerCertificate(serverID, fqdn, email, webroot, nginxContainer string) (JobView, error) {
	p := serverCertificateRequest{ServerID: strings.TrimSpace(serverID), FQDN: strings.ToLower(strings.TrimSpace(fqdn)), Email: strings.TrimSpace(email), WebrootHost: strings.TrimSpace(webroot), NginxContainer: strings.TrimSpace(nginxContainer)}
	if p.ServerID == "" || p.FQDN == "" || p.WebrootHost == "" || p.NginxContainer == "" {
		return JobView{}, errors.New("服务器、域名、ACME 目录和 Nginx 容器均为必填")
	}
	return s.enqueueServerJob(p.ServerID, "server.certbot.issue", p)
}

func (s *SystemService) RenewServerCertificates(serverID string) (JobView, error) {
	return s.enqueueServerJob(serverID, "server.certbot.renew", struct {
		ServerID string `json:"serverId"`
	}{ServerID: serverID})
}

func (s *SystemService) serverCertbotIssueHandler(ctx context.Context, task job.Job, reporter job.Reporter) error {
	var p serverCertificateRequest
	if err := json.Unmarshal([]byte(task.Payload), &p); err != nil {
		return err
	}
	if p.ServerID == "" || p.FQDN == "" || p.WebrootHost == "" || p.NginxContainer == "" {
		return errors.New("invalid certificate task")
	}
	servers, err := s.serverStore()
	if err != nil {
		return err
	}
	connection, err := servers.Get(ctx, p.ServerID)
	if err != nil {
		return err
	}
	reporter.Progress(10, "验证 Certbot、DNS、Nginx 挂载和 HTTP 挑战目录")
	remote := server.NewRemote()
	data, err := remote.AgentCertbotCheck(ctx, connection, p.ServerID, p.FQDN, p.WebrootHost)
	if err != nil {
		return err
	}
	var check struct {
		Existing         *server.AgentCertificateInfo `json:"existing"`
		CertbotAvailable bool                         `json:"certbotAvailable"`
		CertificateMount bool                         `json:"certificateMount"`
		ChallengeReady   bool                         `json:"challengeReady"`
		DNSReady         bool                         `json:"dnsReady"`
		NginxContainer   string                       `json:"nginxContainer"`
		Detail           string                       `json:"detail"`
	}
	if err := json.Unmarshal(data, &check); err != nil {
		return err
	}
	if check.Existing != nil {
		reporter.Log("已复用现有证书：" + check.Existing.Certificate)
		return nil
	}
	if !check.CertbotAvailable || !check.CertificateMount || !check.ChallengeReady || !check.DNSReady || check.NginxContainer != p.NginxContainer {
		return fmt.Errorf("Certbot 预检未通过：%s", check.Detail)
	}
	reporter.Progress(40, "通过 Certbot 申请证书并配置自动续期")
	info, err := remote.IssueCertificateWithAgent(ctx, connection, p.ServerID, task.ID, p.FQDN, p.Email, p.WebrootHost, p.NginxContainer)
	if err != nil {
		return err
	}
	reporter.Log("证书已可供 Nginx 使用：" + info.Certificate)
	reporter.Progress(95, "证书已签发且 Nginx 可读取，请在域名绑定中选择证书并应用入口")
	return nil
}

func (s *SystemService) serverCertbotRenewHandler(ctx context.Context, task job.Job, reporter job.Reporter) error {
	var p struct {
		ServerID string `json:"serverId"`
	}
	if err := json.Unmarshal([]byte(task.Payload), &p); err != nil || p.ServerID == "" {
		return errors.New("invalid certificate renewal task")
	}
	servers, err := s.serverStore()
	if err != nil {
		return err
	}
	connection, err := servers.Get(ctx, p.ServerID)
	if err != nil {
		return err
	}
	reporter.Progress(20, "运行 Certbot 续期；更新后的证书将触发 Nginx 安全重载")
	if err := server.NewRemote().RenewCertificatesWithAgent(ctx, connection, p.ServerID, task.ID); err != nil {
		return err
	}
	reporter.Progress(95, "Certbot 续期检查完成")
	return nil
}
