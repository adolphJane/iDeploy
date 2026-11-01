package domain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CloudflareDNSClient uses a zone-scoped API token. It never logs the token.
type CloudflareDNSClient struct {
	token   string
	baseURL string
	http    *http.Client
}

func NewCloudflareDNSClient(token string) (*CloudflareDNSClient, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("Cloudflare API Token 不能为空")
	}
	return &CloudflareDNSClient{token: strings.TrimSpace(token), baseURL: "https://api.cloudflare.com/client/v4", http: &http.Client{Timeout: 15 * time.Second}}, nil
}

type cloudflareEnvelope struct {
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	ResultInfo struct {
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

func (c *CloudflareDNSClient) call(ctx context.Context, method, path string, body any, out any) (cloudflareEnvelope, error) {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return cloudflareEnvelope{}, err
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, payload)
	if err != nil {
		return cloudflareEnvelope{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return cloudflareEnvelope{}, fmt.Errorf("Cloudflare 请求失败: %w", err)
	}
	defer response.Body.Close()
	var envelope cloudflareEnvelope
	if err = json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&envelope); err != nil {
		return envelope, fmt.Errorf("Cloudflare 响应无效（HTTP %d）", response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !envelope.Success {
		message := "请求未成功"
		if len(envelope.Errors) > 0 {
			message = envelope.Errors[0].Message
		}
		return envelope, fmt.Errorf("Cloudflare API 错误（HTTP %d）: %s", response.StatusCode, message)
	}
	if out != nil {
		if err = json.Unmarshal(envelope.Result, out); err != nil {
			return envelope, fmt.Errorf("Cloudflare 记录格式无效: %w", err)
		}
	}
	return envelope, nil
}

func (c *CloudflareDNSClient) zoneID(ctx context.Context, name string) (string, error) {
	var zones []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	_, err := c.call(ctx, "GET", "/zones?name="+url.QueryEscape(name)+"&per_page=50", nil, &zones)
	if err != nil {
		return "", err
	}
	for _, zone := range zones {
		if strings.EqualFold(zone.Name, name) {
			return zone.ID, nil
		}
	}
	return "", errors.New("Cloudflare 中没有此主域名的 Zone；请检查 Token 的 Zone Read 权限")
}

// TestAccess checks the token with read-only requests. If a zone is supplied,
// it also verifies access to that zone's DNS records.
func (c *CloudflareDNSClient) TestAccess(ctx context.Context, zone string) error {
	if zone == "" {
		var zones []struct {
			ID string `json:"id"`
		}
		_, err := c.call(ctx, "GET", "/zones?per_page=1", nil, &zones)
		return err
	}
	id, err := c.zoneID(ctx, zone)
	if err != nil {
		return err
	}
	var records []cloudflareRecord
	_, err = c.call(ctx, "GET", "/zones/"+url.PathEscape(id)+"/dns_records?page=1&per_page=1", nil, &records)
	return err
}

type cloudflareRecord struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Content    string `json:"content"`
	TTL        int64  `json:"ttl"`
	ModifiedOn string `json:"modified_on"`
}

func convertCloudflareRecord(v cloudflareRecord, zone string) DNSRecord {
	rr := strings.TrimSuffix(strings.ToLower(v.Name), ".")
	if rr == zone {
		rr = "@"
	} else {
		rr = strings.TrimSuffix(rr, "."+zone)
	}
	modified, _ := time.Parse(time.RFC3339Nano, v.ModifiedOn)
	var updatedAt int64
	if !modified.IsZero() {
		updatedAt = modified.UnixMilli()
	}
	return DNSRecord{ID: v.ID, RR: rr, Type: v.Type, Value: v.Content, TTL: v.TTL,
		Line: "default", Status: "Enable", Locked: !editableRecordType(v.Type), UpdatedAt: updatedAt}
}

func editableRecordType(kind string) bool {
	switch kind {
	case "A", "AAAA", "CNAME", "TXT":
		return true
	}
	return false
}

func (c *CloudflareDNSClient) list(ctx context.Context, zoneID, zone string) ([]DNSRecord, error) {
	records := []DNSRecord{}
	for page := 1; page <= 20; page++ {
		var items []cloudflareRecord
		info, err := c.call(ctx, "GET", fmt.Sprintf("/zones/%s/dns_records?page=%d&per_page=100", url.PathEscape(zoneID), page), nil, &items)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			records = append(records, convertCloudflareRecord(item, zone))
		}
		if info.ResultInfo.TotalPages <= page {
			return records, nil
		}
	}
	return nil, errors.New("Cloudflare 解析记录超过 2000 条，请缩小管理范围")
}

func (c *CloudflareDNSClient) ListRecords(ctx context.Context, zone string) ([]DNSRecord, error) {
	id, err := c.zoneID(ctx, zone)
	if err != nil {
		return nil, err
	}
	return c.list(ctx, id, zone)
}

func (c *CloudflareDNSClient) recordContext(ctx context.Context, zone, recordID string, expected int64) (string, DNSRecord, error) {
	id, err := c.zoneID(ctx, zone)
	if err != nil {
		return "", DNSRecord{}, err
	}
	records, err := c.list(ctx, id, zone)
	if err != nil {
		return "", DNSRecord{}, err
	}
	for _, record := range records {
		if record.ID != recordID {
			continue
		}
		if record.Locked {
			return "", DNSRecord{}, errors.New("此类型的记录暂不支持编辑")
		}
		if expected > 0 && record.UpdatedAt != expected {
			return "", DNSRecord{}, errors.New("解析记录已在 Cloudflare 发生变化，请刷新后重试")
		}
		return id, record, nil
	}
	return "", DNSRecord{}, ErrNotFound
}

func cloudflareRecordBody(zone string, v DNSRecordInput) map[string]any {
	name := zone
	if v.RR != "@" {
		name = v.RR + "." + zone
	}
	ttl := v.TTL
	if ttl == 0 {
		ttl = 1
	}
	return map[string]any{"name": name, "type": v.Type, "content": v.Value, "ttl": ttl}
}

func (c *CloudflareDNSClient) AddRecord(ctx context.Context, zone string, input DNSRecordInput) (DNSRecord, error) {
	v, err := input.Validate()
	if err != nil {
		return DNSRecord{}, err
	}
	if v.Line != "default" {
		return DNSRecord{}, errors.New("Cloudflare 仅支持默认解析线路")
	}
	if v.TTL != 0 && v.TTL != 1 && v.TTL < 60 {
		return DNSRecord{}, errors.New("Cloudflare TTL 须为自动（1）或至少 60 秒")
	}
	id, err := c.zoneID(ctx, zone)
	if err != nil {
		return DNSRecord{}, err
	}
	existing, err := c.list(ctx, id, zone)
	if err != nil {
		return DNSRecord{}, err
	}
	for _, item := range existing {
		if item.RR == v.RR && item.Type == v.Type {
			if item.Value == v.Value {
				return item, nil
			}
			return DNSRecord{}, errors.New("相同主机记录和类型已有不同值，请先核对 Cloudflare 记录")
		}
	}
	var created cloudflareRecord
	_, err = c.call(ctx, "POST", "/zones/"+url.PathEscape(id)+"/dns_records", cloudflareRecordBody(zone, v), &created)
	if err != nil {
		return DNSRecord{}, fmt.Errorf("提交 Cloudflare 解析记录失败，请刷新后核对: %w", err)
	}
	return convertCloudflareRecord(created, zone), nil
}

func (c *CloudflareDNSClient) UpdateRecord(ctx context.Context, zone, recordID string, input DNSRecordInput, expected int64) (DNSRecord, error) {
	v, err := input.Validate()
	if err != nil {
		return DNSRecord{}, err
	}
	if v.Line != "default" {
		return DNSRecord{}, errors.New("Cloudflare 仅支持默认解析线路")
	}
	if v.TTL != 0 && v.TTL != 1 && v.TTL < 60 {
		return DNSRecord{}, errors.New("Cloudflare TTL 须为自动（1）或至少 60 秒")
	}
	id, _, err := c.recordContext(ctx, zone, recordID, expected)
	if err != nil {
		return DNSRecord{}, err
	}
	var updated cloudflareRecord
	_, err = c.call(ctx, "PATCH", "/zones/"+url.PathEscape(id)+"/dns_records/"+url.PathEscape(recordID), cloudflareRecordBody(zone, v), &updated)
	if err != nil {
		return DNSRecord{}, err
	}
	return convertCloudflareRecord(updated, zone), nil
}

func (c *CloudflareDNSClient) DeleteRecord(ctx context.Context, zone, recordID string, expected int64) error {
	id, _, err := c.recordContext(ctx, zone, recordID, expected)
	if err != nil {
		return err
	}
	_, err = c.call(ctx, "DELETE", "/zones/"+url.PathEscape(id)+"/dns_records/"+url.PathEscape(recordID), nil, nil)
	return err
}
