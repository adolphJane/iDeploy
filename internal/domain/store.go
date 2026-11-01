package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

type Summary struct {
	Domain
	SubdomainCount int `json:"subdomainCount"`
	BindingCount   int `json:"bindingCount"`
}

type DeploymentDomain struct {
	FQDN          string `json:"fqdn"`
	DomainID      string `json:"domainId"`
	HostID        string `json:"hostId"`
	Status        string `json:"status"`
	ContainerPort int    `json:"containerPort"`
}

func (s *Store) ListDeploymentDomains(ctx context.Context, deploymentID string) ([]DeploymentDomain, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT h.fqdn,h.domain_id,h.id,b.status,b.container_port
        FROM domain_bindings b JOIN domain_hosts h ON h.id=b.host_id
        WHERE b.deployment_id=? ORDER BY h.fqdn`, deploymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []DeploymentDomain{}
	for rows.Next() {
		var item DeploymentDomain
		if err := rows.Scan(&item.FQDN, &item.DomainID, &item.HostID, &item.Status, &item.ContainerPort); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) CountServerBindings(ctx context.Context, serverID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM domain_bindings b
        JOIN deployment_targets d ON d.id=b.deployment_id WHERE d.server_id=?) +
        (SELECT COUNT(*) FROM nginx_external_routes WHERE server_id=?)`, serverID, serverID).Scan(&count)
	return count, err
}

func (s *Store) HasServerIngress(ctx context.Context, serverID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM server_ingresses WHERE server_id=?", serverID).Scan(&count)
	return count > 0, err
}

func (s *Store) CreateDomain(ctx context.Context, name, note, connectionID string) (Domain, error) {
	name, err := NormalizeDomain(name)
	if err != nil {
		return Domain{}, err
	}
	note, err = validateNote(note)
	if err != nil {
		return Domain{}, err
	}
	id, err := newID()
	if err != nil {
		return Domain{}, err
	}
	rootID, err := newID()
	if err != nil {
		return Domain{}, err
	}
	stamp := now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Domain{}, err
	}
	defer tx.Rollback()
	var connection any
	if connectionID != "" {
		connection = connectionID
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO domains(id,name,note,dns_connection_id,created_at,updated_at) VALUES(?,?,?,?,?,?)", id, name, note, connection, stamp, stamp); err != nil {
		return Domain{}, fmt.Errorf("保存主域名失败: %w", err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO domain_hosts(id,domain_id,label,fqdn,note,created_at,updated_at) VALUES(?,?, '@',?,'',?,?)", rootID, id, name, stamp, stamp); err != nil {
		return Domain{}, err
	}
	if err = tx.Commit(); err != nil {
		return Domain{}, err
	}
	return s.GetDomain(ctx, id)
}

func (s *Store) GetDomain(ctx context.Context, id string) (Domain, error) {
	var v Domain
	err := s.db.QueryRowContext(ctx, "SELECT id,name,note,COALESCE(dns_connection_id,''),created_at,updated_at FROM domains WHERE id=?", id).Scan(&v.ID, &v.Name, &v.Note, &v.DNSConnectionID, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Domain{}, ErrNotFound
	}
	return v, err
}

func (s *Store) ListDomains(ctx context.Context) ([]Summary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id,d.name,d.note,COALESCE(d.dns_connection_id,''),d.created_at,d.updated_at,
    (SELECT COUNT(*) FROM domain_hosts h WHERE h.domain_id=d.id AND h.label<>'@'),
    (SELECT COUNT(*) FROM domain_hosts h JOIN domain_bindings b ON b.host_id=h.id WHERE h.domain_id=d.id)
    FROM domains d ORDER BY d.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Summary{}
	for rows.Next() {
		var v Summary
		if err := rows.Scan(&v.ID, &v.Name, &v.Note, &v.DNSConnectionID, &v.CreatedAt, &v.UpdatedAt, &v.SubdomainCount, &v.BindingCount); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func (s *Store) UpdateDomain(ctx context.Context, id, note, connectionID string) (Domain, error) {
	note, err := validateNote(note)
	if err != nil {
		return Domain{}, err
	}
	var connection any
	if connectionID != "" {
		connection = connectionID
	}
	res, err := s.db.ExecContext(ctx, "UPDATE domains SET note=?,dns_connection_id=?,updated_at=? WHERE id=?", note, connection, now(), id)
	if err != nil {
		return Domain{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Domain{}, ErrNotFound
	}
	return s.GetDomain(ctx, id)
}

func (s *Store) DeleteDomain(ctx context.Context, id string) error {
	var children, bindings int
	err := s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM domain_hosts WHERE domain_id=? AND label<>'@'),
        (SELECT COUNT(*) FROM domain_bindings b JOIN domain_hosts h ON h.id=b.host_id WHERE h.domain_id=?)`, id, id).Scan(&children, &bindings)
	if err != nil {
		return err
	}
	if children > 0 || bindings > 0 {
		return errors.New("请先移除子域名并解绑访问，再移除主域名")
	}
	res, err := s.db.ExecContext(ctx, "DELETE FROM domains WHERE id=?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateHost(ctx context.Context, domainID, label, note string) (Host, error) {
	d, err := s.GetDomain(ctx, domainID)
	if err != nil {
		return Host{}, err
	}
	label, err = NormalizeLabel(label)
	if err != nil {
		return Host{}, err
	}
	note, err = validateNote(note)
	if err != nil {
		return Host{}, err
	}
	id, err := newID()
	if err != nil {
		return Host{}, err
	}
	stamp := now()
	fqdn := label + "." + d.Name
	_, err = s.db.ExecContext(ctx, "INSERT INTO domain_hosts(id,domain_id,label,fqdn,note,created_at,updated_at) VALUES(?,?,?,?,?,?,?)", id, domainID, label, fqdn, note, stamp, stamp)
	if err != nil {
		return Host{}, fmt.Errorf("保存子域名失败: %w", err)
	}
	return s.GetHost(ctx, id)
}

func (s *Store) GetHost(ctx context.Context, id string) (Host, error) {
	var h Host
	err := s.db.QueryRowContext(ctx, "SELECT id,domain_id,label,fqdn,note,created_at,updated_at FROM domain_hosts WHERE id=?", id).Scan(&h.ID, &h.DomainID, &h.Label, &h.FQDN, &h.Note, &h.CreatedAt, &h.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Host{}, ErrNotFound
	}
	if err != nil {
		return Host{}, err
	}
	b, err := s.GetBindingForHost(ctx, id)
	if err == nil {
		h.Binding = &b
	} else if !errors.Is(err, ErrNotFound) {
		return Host{}, err
	}
	if route, routeErr := s.GetExternalRoute(ctx, id); routeErr == nil {
		h.ExternalRoute = &route
	} else if !errors.Is(routeErr, ErrNotFound) {
		return Host{}, routeErr
	}
	return h, nil
}

func (s *Store) ListHosts(ctx context.Context, domainID string) ([]Host, error) {
	if _, err := s.GetDomain(ctx, domainID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM domain_hosts WHERE domain_id=? ORDER BY CASE WHEN label='@' THEN 0 ELSE 1 END,label COLLATE NOCASE`, domainID)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	result := make([]Host, 0, len(ids))
	for _, id := range ids {
		h, err := s.GetHost(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, h)
	}
	return result, nil
}

func (s *Store) UpdateHost(ctx context.Context, id, note string) (Host, error) {
	note, err := validateNote(note)
	if err != nil {
		return Host{}, err
	}
	res, err := s.db.ExecContext(ctx, "UPDATE domain_hosts SET note=?,updated_at=? WHERE id=?", note, now(), id)
	if err != nil {
		return Host{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Host{}, ErrNotFound
	}
	return s.GetHost(ctx, id)
}

func (s *Store) DeleteHost(ctx context.Context, id string) error {
	h, err := s.GetHost(ctx, id)
	if err != nil {
		return err
	}
	if h.Label == "@" {
		return errors.New("根域名访问项不能单独移除")
	}
	if h.Binding != nil {
		return errors.New("请先解绑部署，再移除子域名")
	}
	res, err := s.db.ExecContext(ctx, "DELETE FROM domain_hosts WHERE id=?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateBindingDraft(ctx context.Context, hostID, deploymentID, service string, port int, protocol, certificatePath, certificateKeyPath, ingressMode string) (Binding, error) {
	host, err := s.GetHost(ctx, hostID)
	if err != nil {
		return Binding{}, err
	}
	if host.ExternalRoute != nil {
		return Binding{}, errors.New("此域名已有服务器服务路由，请先解除")
	}
	if strings.TrimSpace(deploymentID) == "" || port <= 0 || port > 65535 || protocol != "http" || (certificatePath == "") != (certificateKeyPath == "") || ingressMode != "caddy" && ingressMode != "nginx" || certificatePath != "" && ingressMode != "nginx" {
		return Binding{}, errors.New("绑定目标或端口无效")
	}
	current, currentErr := s.GetBindingForHost(ctx, hostID)
	if currentErr == nil {
		if current.Status != "draft" {
			return Binding{}, errors.New("已应用的绑定须通过迁移流程修改")
		}
		_, err := s.db.ExecContext(ctx, `UPDATE domain_bindings SET deployment_id=?,service_name=?,container_port=?,upstream_protocol=?,certificate_path=?,certificate_key_path=?,ingress_mode=?,updated_at=? WHERE id=?`, deploymentID, strings.TrimSpace(service), port, protocol, certificatePath, certificateKeyPath, ingressMode, now(), current.ID)
		if err != nil {
			return Binding{}, err
		}
		return s.GetBindingForHost(ctx, hostID)
	}
	if !errors.Is(currentErr, ErrNotFound) {
		return Binding{}, currentErr
	}
	id, err := newID()
	if err != nil {
		return Binding{}, err
	}
	stamp := now()
	_, err = s.db.ExecContext(ctx, `INSERT INTO domain_bindings(id,host_id,deployment_id,service_name,container_port,upstream_protocol,certificate_path,certificate_key_path,ingress_mode,status,created_at,updated_at)
        VALUES(?,?,?,?,?,?,?,?,?,'draft',?,?)`, id, hostID, deploymentID, strings.TrimSpace(service), port, protocol, certificatePath, certificateKeyPath, ingressMode, stamp, stamp)
	if err != nil {
		return Binding{}, fmt.Errorf("保存绑定草稿失败: %w", err)
	}
	return s.GetBindingForHost(ctx, hostID)
}

func (s *Store) GetBindingForHost(ctx context.Context, hostID string) (Binding, error) {
	var b Binding
	err := s.db.QueryRowContext(ctx, `SELECT id,host_id,deployment_id,service_name,container_port,upstream_protocol,certificate_path,certificate_key_path,ingress_mode,status,detail,host_ip,host_port,job_id,created_at,updated_at FROM domain_bindings WHERE host_id=?`, hostID).Scan(&b.ID, &b.HostID, &b.DeploymentID, &b.ServiceName, &b.ContainerPort, &b.UpstreamProtocol, &b.CertificatePath, &b.CertificateKeyPath, &b.IngressMode, &b.Status, &b.Detail, &b.HostIP, &b.HostPort, &b.JobID, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Binding{}, ErrNotFound
	}
	return b, err
}

func (s *Store) DeleteBindingDraft(ctx context.Context, hostID string) error {
	b, err := s.GetBindingForHost(ctx, hostID)
	if err != nil {
		return err
	}
	if b.Status != "draft" {
		return errors.New("已应用的绑定必须通过远端解绑任务移除")
	}
	_, err = s.db.ExecContext(ctx, "DELETE FROM domain_bindings WHERE host_id=?", hostID)
	return err
}

func validateDNSConnectionFields(provider, name, accessKeyID string) (string, string, error) {
	name = strings.TrimSpace(name)
	accessKeyID = strings.TrimSpace(accessKeyID)
	if provider != "aliyun" && provider != "cloudflare" {
		return "", "", errors.New("不支持的 DNS 服务商")
	}
	if name == "" || len(name) > 100 || len(accessKeyID) > 200 || provider == "aliyun" && accessKeyID == "" {
		return "", "", errors.New("请填写连接名称及服务商所需凭据")
	}
	if provider == "cloudflare" {
		accessKeyID = ""
	}
	return name, accessKeyID, nil
}

func (s *Store) CreateDNSConnection(ctx context.Context, provider, name, accessKeyID string) (DNSConnection, error) {
	name, accessKeyID, err := validateDNSConnectionFields(provider, name, accessKeyID)
	if err != nil {
		return DNSConnection{}, err
	}
	id, err := newID()
	if err != nil {
		return DNSConnection{}, err
	}
	stamp := now()
	_, err = s.db.ExecContext(ctx, "INSERT INTO dns_connections(id,name,access_key_id,credential_ref,provider,created_at,updated_at) VALUES(?,?,?,?,?,?,?)", id, name, accessKeyID, DNSCredentialRefForProvider(provider, id), provider, stamp, stamp)
	if err != nil {
		return DNSConnection{}, fmt.Errorf("保存 DNS 连接失败: %w", err)
	}
	return s.GetDNSConnection(ctx, id)
}

func (s *Store) UpdateDNSConnection(ctx context.Context, id, name, accessKeyID string) (DNSConnection, error) {
	current, err := s.GetDNSConnection(ctx, id)
	if err != nil {
		return DNSConnection{}, err
	}
	name, accessKeyID, err = validateDNSConnectionFields(current.Provider, name, accessKeyID)
	if err != nil {
		return DNSConnection{}, err
	}
	result, err := s.db.ExecContext(ctx,
		"UPDATE dns_connections SET name=?, access_key_id=?, updated_at=? WHERE id=?",
		name, accessKeyID, now(), id)
	if err != nil {
		return DNSConnection{}, fmt.Errorf("更新 DNS 连接失败: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return DNSConnection{}, ErrNotFound
	}
	return s.GetDNSConnection(ctx, id)
}

func (s *Store) GetDNSConnection(ctx context.Context, id string) (DNSConnection, error) {
	var v DNSConnection
	err := s.db.QueryRowContext(ctx, "SELECT id,name,access_key_id,credential_ref,provider,created_at,updated_at FROM dns_connections WHERE id=?", id).Scan(&v.ID, &v.Name, &v.AccessKeyID, &v.CredentialRef, &v.Provider, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return DNSConnection{}, ErrNotFound
	}
	if err != nil {
		return DNSConnection{}, err
	}
	v.CredentialConfigured = v.CredentialRef != ""
	return v, nil
}

func (s *Store) ListDNSConnections(ctx context.Context) ([]DNSConnection, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM dns_connections ORDER BY name COLLATE NOCASE")
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	result := make([]DNSConnection, 0, len(ids))
	for _, id := range ids {
		v, err := s.GetDNSConnection(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, nil
}

func (s *Store) DeleteDNSConnection(ctx context.Context, id string) error {
	var references int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM domains WHERE dns_connection_id=?", id).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return errors.New("请先从使用此连接的主域名移除关联")
	}
	res, err := s.db.ExecContext(ctx, "DELETE FROM dns_connections WHERE id=?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
