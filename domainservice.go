package main

import (
	"context"
	"errors"
	"fmt"
	"ideploy/internal/domain"
	"ideploy/internal/server"
	"strings"
	"time"

	alidns "github.com/alibabacloud-go/alidns-20150109/v5/client"
)

type DomainInput struct {
	Name            string `json:"name"`
	Note            string `json:"note"`
	DNSConnectionID string `json:"dnsConnectionId"`
}

type DomainHostInput struct {
	DomainID string `json:"domainId"`
	Label    string `json:"label"`
	Note     string `json:"note"`
}

type AliDNSConnectionInput struct {
	Name            string `json:"name"`
	AccessKeyID     string `json:"accessKeyId"`
	AccessKeySecret string `json:"accessKeySecret"`
}

type DNSConnectionInput struct {
	Provider        string `json:"provider"`
	Name            string `json:"name"`
	AccessKeyID     string `json:"accessKeyId"`
	AccessKeySecret string `json:"accessKeySecret"`
	APIToken        string `json:"apiToken"`
}

type DNSConnectionTestInput struct {
	Provider        string `json:"provider"`
	AccessKeyID     string `json:"accessKeyId"`
	AccessKeySecret string `json:"accessKeySecret"`
	APIToken        string `json:"apiToken"`
	DomainName      string `json:"domainName"`
}

type SavedDNSConnectionTestInput struct {
	ID         string `json:"id"`
	DomainName string `json:"domainName"`
}

type DomainBindingDraftInput struct {
	HostID             string `json:"hostId"`
	DeploymentID       string `json:"deploymentId"`
	ServiceName        string `json:"serviceName"`
	ContainerPort      int    `json:"containerPort"`
	UpstreamProtocol   string `json:"upstreamProtocol"`
	CertificatePath    string `json:"certificatePath"`
	CertificateKeyPath string `json:"certificateKeyPath"`
	IngressMode        string `json:"ingressMode"`
}

type AliDNSRecordChange struct {
	DomainID          string                `json:"domainId"`
	RecordID          string                `json:"recordId"`
	ExpectedUpdatedAt int64                 `json:"expectedUpdatedAt"`
	Record            domain.DNSRecordInput `json:"record"`
}

func (s *SystemService) domainStore() (*domain.Store, error) {
	s.mu.RLock()
	db := s.db
	s.mu.RUnlock()
	if db == nil {
		return nil, errors.New("域名服务尚未就绪")
	}
	return domain.NewStore(db), nil
}

func (s *SystemService) ListDomains() ([]domain.Summary, error) {
	store, err := s.domainStore()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.ListDomains(ctx)
}

func (s *SystemService) CreateDomain(input DomainInput) (domain.Domain, error) {
	store, err := s.domainStore()
	if err != nil {
		return domain.Domain{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if input.DNSConnectionID != "" {
		if _, err = store.GetDNSConnection(ctx, input.DNSConnectionID); err != nil {
			return domain.Domain{}, err
		}
	}
	return store.CreateDomain(ctx, input.Name, input.Note, input.DNSConnectionID)
}

func (s *SystemService) UpdateDomain(id string, input DomainInput) (domain.Domain, error) {
	store, err := s.domainStore()
	if err != nil {
		return domain.Domain{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if input.DNSConnectionID != "" {
		if _, err = store.GetDNSConnection(ctx, input.DNSConnectionID); err != nil {
			return domain.Domain{}, err
		}
	}
	return store.UpdateDomain(ctx, id, input.Note, input.DNSConnectionID)
}

func (s *SystemService) DeleteDomain(id string) error {
	store, err := s.domainStore()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.DeleteDomain(ctx, id)
}

func (s *SystemService) ListDomainHosts(domainID string) ([]domain.Host, error) {
	store, err := s.domainStore()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.ListHosts(ctx, domainID)
}

func (s *SystemService) ListDeploymentDomains(deploymentID string) ([]domain.DeploymentDomain, error) {
	store, err := s.domainStore()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.ListDeploymentDomains(ctx, deploymentID)
}

func (s *SystemService) CreateDomainHost(input DomainHostInput) (domain.Host, error) {
	store, err := s.domainStore()
	if err != nil {
		return domain.Host{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.CreateHost(ctx, input.DomainID, input.Label, input.Note)
}

func (s *SystemService) UpdateDomainHost(id, note string) (domain.Host, error) {
	store, err := s.domainStore()
	if err != nil {
		return domain.Host{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.UpdateHost(ctx, id, note)
}

func (s *SystemService) DeleteDomainHost(id string) error {
	store, err := s.domainStore()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.DeleteHost(ctx, id)
}

func (s *SystemService) SaveDomainBindingDraft(input DomainBindingDraftInput) (domain.Binding, error) {
	store, err := s.domainStore()
	if err != nil {
		return domain.Binding{}, err
	}
	deployments, err := s.deploymentStore()
	if err != nil {
		return domain.Binding{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err = deployments.Get(ctx, input.DeploymentID); err != nil {
		return domain.Binding{}, err
	}
	if (input.CertificatePath == "") != (input.CertificateKeyPath == "") {
		return domain.Binding{}, errors.New("HTTPS 证书和私钥路径需要同时填写")
	}
	if input.CertificatePath != "" {
		if _, err := server.NginxManagedBlock([]server.IngressRoute{{FQDN: "check.example.com", HostIP: "127.0.0.1", HostPort: 80,
			Certificate: input.CertificatePath, CertificateKey: input.CertificateKeyPath}}); err != nil {
			return domain.Binding{}, err
		}
	}
	if input.IngressMode == "" {
		input.IngressMode = "caddy"
	}
	return store.CreateBindingDraft(ctx, input.HostID, input.DeploymentID, input.ServiceName, input.ContainerPort, input.UpstreamProtocol, input.CertificatePath, input.CertificateKeyPath, input.IngressMode)
}

func (s *SystemService) DeleteDomainBindingDraft(hostID string) error {
	store, err := s.domainStore()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.DeleteBindingDraft(ctx, hostID)
}

func (s *SystemService) ListAliDNSConnections() ([]domain.DNSConnection, error) {
	return s.ListDNSConnections()
}

func (s *SystemService) ListDNSConnections() ([]domain.DNSConnection, error) {
	store, err := s.domainStore()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.ListDNSConnections(ctx)
}

func (s *SystemService) CreateAliDNSConnection(input AliDNSConnectionInput) (domain.DNSConnection, error) {
	return s.CreateDNSConnection(DNSConnectionInput{Provider: "aliyun", Name: input.Name, AccessKeyID: input.AccessKeyID, AccessKeySecret: input.AccessKeySecret})
}

func (s *SystemService) CreateDNSConnection(input DNSConnectionInput) (domain.DNSConnection, error) {
	secret := input.AccessKeySecret
	if input.Provider == "cloudflare" {
		secret = input.APIToken
		if _, err := domain.NewCloudflareDNSClient(secret); err != nil {
			return domain.DNSConnection{}, err
		}
	} else if input.Provider == "aliyun" {
		if strings.TrimSpace(secret) == "" {
			return domain.DNSConnection{}, errors.New("请输入 AccessKey Secret")
		}
	} else {
		return domain.DNSConnection{}, errors.New("不支持的 DNS 服务商")
	}
	store, err := s.domainStore()
	if err != nil {
		return domain.DNSConnection{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	value, err := store.CreateDNSConnection(ctx, input.Provider, input.Name, input.AccessKeyID)
	if err != nil {
		return domain.DNSConnection{}, err
	}
	if err = domain.SaveDNSSecret(ctx, value.CredentialRef, secret); err != nil {
		_ = store.DeleteDNSConnection(ctx, value.ID)
		return domain.DNSConnection{}, err
	}
	return value, nil
}

func (s *SystemService) UpdateDNSConnection(id string, input DNSConnectionInput) (domain.DNSConnection, error) {
	store, err := s.domainStore()
	if err != nil {
		return domain.DNSConnection{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	current, err := store.GetDNSConnection(ctx, id)
	if err != nil {
		return domain.DNSConnection{}, err
	}
	if input.Provider != current.Provider {
		return domain.DNSConnection{}, errors.New("DNS 服务商类型不可修改，请新增连接")
	}
	accessKeyID := strings.TrimSpace(input.AccessKeyID)
	secret := input.AccessKeySecret
	if current.Provider == "cloudflare" {
		accessKeyID = ""
		secret = input.APIToken
	}
	if current.Provider == "aliyun" && accessKeyID != current.AccessKeyID && strings.TrimSpace(secret) == "" {
		return domain.DNSConnection{}, errors.New("更换 AccessKey ID 时请同时填写 AccessKey Secret")
	}
	if strings.TrimSpace(secret) == "" {
		return store.UpdateDNSConnection(ctx, id, input.Name, accessKeyID)
	}
	var previousSecret string
	previousSecret, previousError := domain.LoadDNSSecret(ctx, current.CredentialRef)
	if err := domain.SaveDNSSecret(ctx, current.CredentialRef, secret); err != nil {
		return domain.DNSConnection{}, err
	}
	updated, err := store.UpdateDNSConnection(ctx, id, input.Name, accessKeyID)
	if err == nil {
		return updated, nil
	}
	var rollbackErr error
	if previousError == nil {
		rollbackErr = domain.SaveDNSSecret(ctx, current.CredentialRef, previousSecret)
	} else {
		rollbackErr = domain.DeleteDNSSecret(ctx, current.CredentialRef)
	}
	if rollbackErr != nil {
		return domain.DNSConnection{}, fmt.Errorf("%w；恢复原 DNS 凭据失败: %v", err, rollbackErr)
	}
	return domain.DNSConnection{}, err
}

func (s *SystemService) TestDNSConnection(input DNSConnectionTestInput) (string, error) {
	name := strings.TrimSpace(input.DomainName)
	if name != "" {
		var err error
		name, err = domain.NormalizeDomain(name)
		if err != nil {
			return "", err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch input.Provider {
	case "aliyun":
		client, err := domain.NewAliDNSClient(strings.TrimSpace(input.AccessKeyID), strings.TrimSpace(input.AccessKeySecret))
		if err != nil {
			return "", err
		}
		if err := domain.TestAliDNSAccess(client, name); err != nil {
			return "", err
		}
	case "cloudflare":
		client, err := domain.NewCloudflareDNSClient(input.APIToken)
		if err != nil {
			return "", err
		}
		if err := client.TestAccess(ctx, name); err != nil {
			return "", err
		}
	default:
		return "", errors.New("不支持的 DNS 服务商")
	}
	if name == "" {
		return "连接成功：可访问服务商的 DNS 域名列表；尚未验证具体域名的解析权限。", nil
	}
	return fmt.Sprintf("连接成功：可读取 %s 的解析记录。", name), nil
}

func (s *SystemService) TestSavedDNSConnection(input SavedDNSConnectionTestInput) (string, error) {
	store, err := s.domainStore()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connection, err := store.GetDNSConnection(ctx, input.ID)
	if err != nil {
		return "", err
	}
	secret, err := domain.LoadDNSSecret(ctx, connection.CredentialRef)
	if err != nil {
		return "", err
	}
	test := DNSConnectionTestInput{Provider: connection.Provider, AccessKeyID: connection.AccessKeyID, DomainName: input.DomainName}
	if connection.Provider == "cloudflare" {
		test.APIToken = secret
	} else {
		test.AccessKeySecret = secret
	}
	return s.TestDNSConnection(test)
}

func (s *SystemService) DeleteAliDNSConnection(id string) error {
	return s.DeleteDNSConnection(id)
}

func (s *SystemService) DeleteDNSConnection(id string) error {
	store, err := s.domainStore()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	value, err := store.GetDNSConnection(ctx, id)
	if err != nil {
		return err
	}
	if err = store.DeleteDNSConnection(ctx, id); err != nil {
		return err
	}
	return domain.DeleteDNSSecret(ctx, value.CredentialRef)
}

func (s *SystemService) aliClientForDomain(ctx context.Context, domainID string) (*alidns.Client, string, error) {
	store, err := s.domainStore()
	if err != nil {
		return nil, "", err
	}
	value, err := store.GetDomain(ctx, domainID)
	if err != nil {
		return nil, "", err
	}
	if value.DNSConnectionID == "" {
		return nil, "", errors.New("请先在主域名上选择阿里云 DNS 连接")
	}
	connection, err := store.GetDNSConnection(ctx, value.DNSConnectionID)
	if err != nil {
		return nil, "", err
	}
	if connection.Provider != "aliyun" {
		return nil, "", errors.New("该主域名未选择阿里云 DNS 连接")
	}
	secret, err := domain.LoadDNSSecret(ctx, connection.CredentialRef)
	if err != nil {
		return nil, "", err
	}
	client, err := domain.NewAliDNSClient(connection.AccessKeyID, secret)
	return client, value.Name, err
}

func (s *SystemService) dnsConnectionForDomain(ctx context.Context, domainID string) (domain.Domain, domain.DNSConnection, string, error) {
	store, err := s.domainStore()
	if err != nil {
		return domain.Domain{}, domain.DNSConnection{}, "", err
	}
	value, err := store.GetDomain(ctx, domainID)
	if err != nil {
		return domain.Domain{}, domain.DNSConnection{}, "", err
	}
	if value.DNSConnectionID == "" {
		return value, domain.DNSConnection{}, "", errors.New("请先为主域名选择 DNS 服务商连接")
	}
	connection, err := store.GetDNSConnection(ctx, value.DNSConnectionID)
	if err != nil {
		return value, domain.DNSConnection{}, "", err
	}
	secret, err := domain.LoadDNSSecret(ctx, connection.CredentialRef)
	return value, connection, secret, err
}

func (s *SystemService) ListDNSRecords(domainID string) ([]domain.DNSRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	value, connection, secret, err := s.dnsConnectionForDomain(ctx, domainID)
	if err != nil {
		return nil, err
	}
	switch connection.Provider {
	case "aliyun":
		records, err := s.ListAliDNSRecords(domainID)
		return domain.WithDNSBindingInfo(value.Name, records), err
	case "cloudflare":
		client, err := domain.NewCloudflareDNSClient(secret)
		if err != nil {
			return nil, err
		}
		records, err := client.ListRecords(ctx, value.Name)
		return domain.WithDNSBindingInfo(value.Name, records), err
	}
	return nil, errors.New("不支持的 DNS 服务商")
}

// PrepareDNSRecordBinding re-reads the provider record before resolving its local
// binding identity. No remote DNS or server configuration is changed here.
func (s *SystemService) PrepareDNSRecordBinding(domainID, recordID string) (domain.Host, error) {
	records, err := s.ListDNSRecords(domainID)
	if err != nil {
		return domain.Host{}, err
	}
	for _, record := range records {
		if record.ID != recordID {
			continue
		}
		store, err := s.domainStore()
		if err != nil {
			return domain.Host{}, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return store.EnsureDNSRecordHost(ctx, domainID, record)
	}
	return domain.Host{}, errors.New("该 DNS 记录已不存在，请刷新解析记录后重试")
}

func (s *SystemService) AddDNSRecord(input AliDNSRecordChange) (domain.DNSRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	value, connection, secret, err := s.dnsConnectionForDomain(ctx, input.DomainID)
	if err != nil {
		return domain.DNSRecord{}, err
	}
	switch connection.Provider {
	case "aliyun":
		return s.AddAliDNSRecord(input)
	case "cloudflare":
		client, err := domain.NewCloudflareDNSClient(secret)
		if err != nil {
			return domain.DNSRecord{}, err
		}
		return client.AddRecord(ctx, value.Name, input.Record)
	}
	return domain.DNSRecord{}, errors.New("不支持的 DNS 服务商")
}

func (s *SystemService) UpdateDNSRecord(input AliDNSRecordChange) (domain.DNSRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	value, connection, secret, err := s.dnsConnectionForDomain(ctx, input.DomainID)
	if err != nil {
		return domain.DNSRecord{}, err
	}
	switch connection.Provider {
	case "aliyun":
		return s.UpdateAliDNSRecord(input)
	case "cloudflare":
		client, err := domain.NewCloudflareDNSClient(secret)
		if err != nil {
			return domain.DNSRecord{}, err
		}
		return client.UpdateRecord(ctx, value.Name, input.RecordID, input.Record, input.ExpectedUpdatedAt)
	}
	return domain.DNSRecord{}, errors.New("不支持的 DNS 服务商")
}

func (s *SystemService) SetDNSRecordStatus(domainID, recordID, status string, expectedUpdatedAt int64) (domain.DNSRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, connection, _, err := s.dnsConnectionForDomain(ctx, domainID)
	if err != nil {
		return domain.DNSRecord{}, err
	}
	if connection.Provider != "aliyun" {
		return domain.DNSRecord{}, errors.New("此服务商暂不支持启停解析记录")
	}
	return s.SetAliDNSRecordStatus(domainID, recordID, status, expectedUpdatedAt)
}

func (s *SystemService) DeleteDNSRecord(domainID, recordID string, expectedUpdatedAt int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	value, connection, secret, err := s.dnsConnectionForDomain(ctx, domainID)
	if err != nil {
		return err
	}
	switch connection.Provider {
	case "aliyun":
		return s.DeleteAliDNSRecord(domainID, recordID, expectedUpdatedAt)
	case "cloudflare":
		client, err := domain.NewCloudflareDNSClient(secret)
		if err != nil {
			return err
		}
		return client.DeleteRecord(ctx, value.Name, recordID, expectedUpdatedAt)
	}
	return errors.New("不支持的 DNS 服务商")
}

func (s *SystemService) ListAliDNSRecords(domainID string) ([]domain.DNSRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, name, err := s.aliClientForDomain(ctx, domainID)
	if err != nil {
		return nil, err
	}
	return domain.ListAliDNSRecords(client, name)
}

func (s *SystemService) AddAliDNSRecord(input AliDNSRecordChange) (domain.DNSRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, name, err := s.aliClientForDomain(ctx, input.DomainID)
	if err != nil {
		return domain.DNSRecord{}, err
	}
	id, err := domain.AddAliDNSRecord(client, name, input.Record)
	if err != nil {
		return domain.DNSRecord{}, err
	}
	return domain.FindAliDNSRecord(client, name, id)
}

func (s *SystemService) UpdateAliDNSRecord(input AliDNSRecordChange) (domain.DNSRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, name, err := s.aliClientForDomain(ctx, input.DomainID)
	if err != nil {
		return domain.DNSRecord{}, err
	}
	return domain.UpdateAliDNSRecord(client, name, input.RecordID, input.Record, input.ExpectedUpdatedAt)
}

func (s *SystemService) SetAliDNSRecordStatus(domainID, recordID, status string, expectedUpdatedAt int64) (domain.DNSRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, name, err := s.aliClientForDomain(ctx, domainID)
	if err != nil {
		return domain.DNSRecord{}, err
	}
	return domain.SetAliDNSRecordStatus(client, name, recordID, status, expectedUpdatedAt)
}

func (s *SystemService) DeleteAliDNSRecord(domainID, recordID string, expectedUpdatedAt int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, name, err := s.aliClientForDomain(ctx, domainID)
	if err != nil {
		return err
	}
	if err = domain.DeleteAliDNSRecord(client, name, recordID, expectedUpdatedAt); err != nil {
		return err
	}
	return nil
}

func (s *SystemService) TestAliDNSDomain(domainID string) (string, error) {
	values, err := s.ListAliDNSRecords(domainID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("阿里云 DNS 查询成功，读取到 %d 条解析记录", len(values)), nil
}
