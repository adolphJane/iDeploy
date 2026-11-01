package domain

import (
	"context"
	"errors"
	"strings"

	"golang.org/x/net/idna"
)

// DNSRecordHost identifies the hostname served by a record, not its target value.
// Multiple addresses and provider routing lines for a name share one binding.
func DNSRecordHost(domainName string, record DNSRecord) (label, fqdn string, err error) {
	switch strings.ToUpper(record.Type) {
	case "A", "AAAA", "CNAME":
	default:
		return "", "", errors.New("仅 A、AAAA、CNAME 记录可关联网站服务")
	}
	label = strings.ToLower(strings.TrimSpace(record.RR))
	if strings.Contains(label, "*") {
		return "", "", errors.New("通配符记录暂不支持服务绑定，请选择具体域名")
	}
	if label == "@" {
		return label, domainName, nil
	}
	label, err = idna.Lookup.ToASCII(label)
	if err != nil {
		return "", "", errors.New("解析记录的主机名格式无效")
	}
	for _, part := range strings.Split(label, ".") {
		if len(part) > 63 || !validLabel(part) {
			return "", "", errors.New("解析记录的主机名不能用于网站服务绑定")
		}
	}
	fqdn = label + "." + domainName
	if len(fqdn) > 253 {
		return "", "", errors.New("解析记录的完整域名过长")
	}
	return label, fqdn, nil
}

func WithDNSBindingInfo(domainName string, records []DNSRecord) []DNSRecord {
	for i := range records {
		_, fqdn, err := DNSRecordHost(domainName, records[i])
		records[i].FQDN = fqdn
		records[i].BindingEligible = err == nil
		records[i].BindingDisabledReason = ""
		if err != nil {
			records[i].BindingDisabledReason = err.Error()
		}
	}
	return records
}

// EnsureDNSRecordHost is idempotent and never updates an existing host or binding.
// DNS reads alone do not persist hosts; the binding action creates one as needed.
func (s *Store) EnsureDNSRecordHost(ctx context.Context, domainID string, record DNSRecord) (Host, error) {
	d, err := s.GetDomain(ctx, domainID)
	if err != nil {
		return Host{}, err
	}
	label, fqdn, err := DNSRecordHost(d.Name, record)
	if err != nil {
		return Host{}, err
	}
	id, err := newID()
	if err != nil {
		return Host{}, err
	}
	stamp := now()
	_, err = s.db.ExecContext(ctx, `INSERT INTO domain_hosts(id,domain_id,label,fqdn,note,created_at,updated_at)
		VALUES(?,?,?,?,'',?,?) ON CONFLICT(domain_id,label) DO NOTHING`, id, domainID, label, fqdn, stamp, stamp)
	if err != nil {
		return Host{}, err
	}
	if err = s.db.QueryRowContext(ctx, "SELECT id FROM domain_hosts WHERE domain_id=? AND label=?", domainID, label).Scan(&id); err != nil {
		return Host{}, err
	}
	return s.GetHost(ctx, id)
}
