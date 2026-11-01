package domain

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"golang.org/x/net/idna"

	alidns "github.com/alibabacloud-go/alidns-20150109/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	"github.com/alibabacloud-go/tea/dara"
)

type DNSRecord struct {
	ID                    string `json:"id"`
	RR                    string `json:"rr"`
	Type                  string `json:"type"`
	Value                 string `json:"value"`
	TTL                   int64  `json:"ttl"`
	Line                  string `json:"line"`
	Status                string `json:"status"`
	Locked                bool   `json:"locked"`
	UpdatedAt             int64  `json:"updatedAt"`
	FQDN                  string `json:"fqdn,omitempty"`
	BindingEligible       bool   `json:"bindingEligible"`
	BindingDisabledReason string `json:"bindingDisabledReason,omitempty"`
}

type DNSRecordInput struct {
	RR    string `json:"rr"`
	Type  string `json:"type"`
	Value string `json:"value"`
	TTL   int64  `json:"ttl"`
	Line  string `json:"line"`
}

func (v DNSRecordInput) Validate() (DNSRecordInput, error) {
	v.RR = strings.ToLower(strings.TrimSpace(v.RR))
	v.Type = strings.ToUpper(strings.TrimSpace(v.Type))
	v.Value = strings.TrimSpace(v.Value)
	v.Line = strings.TrimSpace(v.Line)
	if v.RR != "@" {
		if v.Type == "TXT" && strings.HasPrefix(v.RR, "_") {
			if len(v.RR) > 63 || !validDNSLabel(v.RR) {
				return v, errors.New("TXT 主机记录格式无效")
			}
		} else {
			label, err := NormalizeLabel(v.RR)
			if err != nil {
				return v, err
			}
			v.RR = label
		}
	}
	switch v.Type {
	case "A":
		if ip := net.ParseIP(v.Value); ip == nil || ip.To4() == nil {
			return v, errors.New("A 记录必须是 IPv4 地址，不包含端口")
		}
	case "AAAA":
		if ip := net.ParseIP(v.Value); ip == nil || ip.To4() != nil {
			return v, errors.New("AAAA 记录必须是 IPv6 地址")
		}
	case "CNAME":
		value, err := idna.Lookup.ToASCII(strings.TrimSuffix(strings.ToLower(v.Value), "."))
		if err != nil || len(value) > 253 || !validHostname(value) {
			return v, errors.New("CNAME 必须是目标域名")
		}
		v.Value = value
	case "TXT":
		if v.Value == "" || len(v.Value) > 255 || strings.ContainsAny(v.Value, "\r\n\x00") {
			return v, errors.New("TXT 记录内容须在 1 到 255 字节之间")
		}
	default:
		return v, errors.New("当前只支持编辑 A、AAAA、CNAME、TXT 记录")
	}
	if v.TTL < 0 || v.TTL > 86400 {
		return v, errors.New("TTL 不在支持的范围内")
	}
	if v.Line == "" {
		v.Line = "default"
	}
	if len(v.Line) > 80 || strings.ContainsAny(v.Line, " \r\n\t") {
		return v, errors.New("解析线路格式无效")
	}
	return v, nil
}

func validHostname(value string) bool {
	labels := strings.Split(value, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) > 63 || !validLabel(label) {
			return false
		}
	}
	return true
}

func validDNSLabel(value string) bool {
	if value == "" || value[len(value)-1] == '-' {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}

func NewAliDNSClient(accessKeyID, secret string) (*alidns.Client, error) {
	if accessKeyID == "" || secret == "" {
		return nil, errors.New("阿里云凭据不可用")
	}
	return alidns.NewClient(&openapi.Config{
		AccessKeyId: dara.String(accessKeyID), AccessKeySecret: dara.String(secret),
		Endpoint: dara.String("alidns.aliyuncs.com"), Protocol: dara.String("HTTPS"),
	})
}

func aliDNSRuntime() *dara.RuntimeOptions {
	connectTimeout, readTimeout, maxAttempts, retry := 5000, 10000, 1, false
	return &dara.RuntimeOptions{ConnectTimeout: &connectTimeout, ReadTimeout: &readTimeout,
		MaxAttempts: &maxAttempts, Autoretry: &retry}
}

func normalizeDNSRecordStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "ENABLE":
		return "Enable"
	case "DISABLE":
		return "Disable"
	default:
		return status
	}
}

// TestAliDNSAccess makes one read-only request. A domain name checks permissions
// for that zone; an empty name checks whether the account can list DNS domains.
func TestAliDNSAccess(client *alidns.Client, domainName string) error {
	if domainName != "" {
		response, err := client.DescribeDomainRecordsWithOptions(&alidns.DescribeDomainRecordsRequest{
			DomainName: dara.String(domainName), PageNumber: dara.Int64(1), PageSize: dara.Int64(1),
		}, aliDNSRuntime())
		if err != nil {
			return fmt.Errorf("读取阿里云域名解析记录失败: %w", err)
		}
		if response == nil || response.Body == nil {
			return errors.New("阿里云返回了空的解析结果")
		}
		return nil
	}
	response, err := client.DescribeDomainsWithOptions(&alidns.DescribeDomainsRequest{
		PageNumber: dara.Int64(1), PageSize: dara.Int64(1),
	}, aliDNSRuntime())
	if err != nil {
		return fmt.Errorf("查询阿里云 DNS 域名失败: %w", err)
	}
	if response == nil || response.Body == nil {
		return errors.New("阿里云返回了空的域名列表")
	}
	return nil
}

func ListAliDNSRecords(client *alidns.Client, domainName string) ([]DNSRecord, error) {
	result := []DNSRecord{}
	for page := int64(1); page <= 20; page++ {
		response, err := client.DescribeDomainRecordsWithOptions(&alidns.DescribeDomainRecordsRequest{
			DomainName: dara.String(domainName), PageNumber: dara.Int64(page), PageSize: dara.Int64(500),
		}, aliDNSRuntime())
		if err != nil {
			return nil, fmt.Errorf("读取阿里云解析记录失败: %w", err)
		}
		if response == nil || response.Body == nil {
			return nil, errors.New("阿里云返回了空的解析结果")
		}
		if response.Body.DomainRecords != nil {
			for _, record := range response.Body.DomainRecords.Record {
				if record == nil {
					continue
				}
				result = append(result, DNSRecord{ID: dara.StringValue(record.RecordId), RR: dara.StringValue(record.RR), Type: dara.StringValue(record.Type), Value: dara.StringValue(record.Value), TTL: dara.Int64Value(record.TTL), Line: dara.StringValue(record.Line), Status: normalizeDNSRecordStatus(dara.StringValue(record.Status)), Locked: dara.BoolValue(record.Locked), UpdatedAt: dara.Int64Value(record.UpdateTimestamp)})
			}
		}
		if int64(len(result)) >= dara.Int64Value(response.Body.TotalCount) {
			return result, nil
		}
	}
	return nil, errors.New("解析记录超过 10000 条，请缩小管理范围")
}

func AddAliDNSRecord(client *alidns.Client, domainName string, input DNSRecordInput) (string, error) {
	value, err := input.Validate()
	if err != nil {
		return "", err
	}
	existing, err := ListAliDNSRecords(client, domainName)
	if err != nil {
		return "", err
	}
	for _, item := range existing {
		if item.RR == value.RR && item.Type == value.Type && item.Line == value.Line {
			if item.Value == value.Value {
				return item.ID, nil
			}
			return "", errors.New("相同主机记录、类型和线路已存在不同值，请先核对阿里云记录")
		}
	}
	request := &alidns.AddDomainRecordRequest{DomainName: dara.String(domainName), RR: dara.String(value.RR), Type: dara.String(value.Type), Value: dara.String(value.Value), Line: dara.String(value.Line)}
	if value.TTL > 0 {
		request.TTL = dara.Int64(value.TTL)
	}
	response, err := client.AddDomainRecordWithOptions(request, aliDNSRuntime())
	if err != nil {
		return "", fmt.Errorf("提交阿里云解析记录失败，请刷新后检查是否已写入: %w", err)
	}
	if response == nil || response.Body == nil {
		return "", errors.New("阿里云返回空结果，请刷新记录核对")
	}
	return dara.StringValue(response.Body.RecordId), nil
}

func FindAliDNSRecord(client *alidns.Client, domainName, recordID string) (DNSRecord, error) {
	if recordID == "" {
		return DNSRecord{}, errors.New("解析记录 ID 不能为空")
	}
	values, err := ListAliDNSRecords(client, domainName)
	if err != nil {
		return DNSRecord{}, err
	}
	for _, v := range values {
		if v.ID == recordID {
			return v, nil
		}
	}
	return DNSRecord{}, ErrNotFound
}

func UpdateAliDNSRecord(client *alidns.Client, domainName, recordID string, input DNSRecordInput, expectedUpdatedAt int64) (DNSRecord, error) {
	v, err := input.Validate()
	if err != nil {
		return DNSRecord{}, err
	}
	current, err := FindAliDNSRecord(client, domainName, recordID)
	if err != nil {
		return DNSRecord{}, err
	}
	if current.Locked {
		return DNSRecord{}, errors.New("该解析记录已锁定")
	}
	if expectedUpdatedAt > 0 && current.UpdatedAt != expectedUpdatedAt {
		return DNSRecord{}, errors.New("解析记录已在阿里云发生变化，请刷新后重试")
	}
	request := &alidns.UpdateDomainRecordRequest{RecordId: dara.String(recordID), RR: dara.String(v.RR), Type: dara.String(v.Type), Value: dara.String(v.Value), Line: dara.String(v.Line)}
	if v.TTL > 0 {
		request.TTL = dara.Int64(v.TTL)
	}
	if _, err := client.UpdateDomainRecordWithOptions(request, aliDNSRuntime()); err != nil {
		return DNSRecord{}, fmt.Errorf("更新阿里云解析记录失败: %w", err)
	}
	updated, err := FindAliDNSRecord(client, domainName, recordID)
	if err != nil {
		return DNSRecord{}, fmt.Errorf("阿里云已接受修改，但无法回读记录，请刷新后核对: %w", err)
	}
	return updated, nil
}

func SetAliDNSRecordStatus(client *alidns.Client, domainName, recordID, status string, expectedUpdatedAt int64) (DNSRecord, error) {
	if status != "Enable" && status != "Disable" {
		return DNSRecord{}, errors.New("解析记录状态无效")
	}
	current, err := FindAliDNSRecord(client, domainName, recordID)
	if err != nil {
		return DNSRecord{}, err
	}
	if current.Locked {
		return DNSRecord{}, errors.New("该解析记录已锁定")
	}
	if expectedUpdatedAt > 0 && current.UpdatedAt != expectedUpdatedAt {
		return DNSRecord{}, errors.New("解析记录已在阿里云发生变化，请刷新后重试")
	}
	if current.Status == status {
		return current, nil
	}
	_, err = client.SetDomainRecordStatusWithOptions(&alidns.SetDomainRecordStatusRequest{RecordId: dara.String(recordID), Status: dara.String(status)}, aliDNSRuntime())
	if err != nil {
		return DNSRecord{}, fmt.Errorf("更改阿里云解析记录状态失败: %w", err)
	}
	return FindAliDNSRecord(client, domainName, recordID)
}

func DeleteAliDNSRecord(client *alidns.Client, domainName, recordID string, expectedUpdatedAt int64) error {
	current, err := FindAliDNSRecord(client, domainName, recordID)
	if err != nil {
		return err
	}
	if current.Locked {
		return errors.New("该解析记录已锁定")
	}
	if expectedUpdatedAt > 0 && current.UpdatedAt != expectedUpdatedAt {
		return errors.New("解析记录已在阿里云发生变化，请刷新后重试")
	}
	_, err = client.DeleteDomainRecordWithOptions(&alidns.DeleteDomainRecordRequest{RecordId: dara.String(recordID)}, aliDNSRuntime())
	if err != nil {
		return fmt.Errorf("删除阿里云解析记录失败: %w", err)
	}
	return nil
}
