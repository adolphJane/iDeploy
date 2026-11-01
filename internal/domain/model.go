package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

var ErrNotFound = errors.New("域名不存在")

type Domain struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Note            string `json:"note"`
	DNSConnectionID string `json:"dnsConnectionId"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type Host struct {
	ID            string         `json:"id"`
	DomainID      string         `json:"domainId"`
	Label         string         `json:"label"`
	FQDN          string         `json:"fqdn"`
	Note          string         `json:"note"`
	Binding       *Binding       `json:"binding,omitempty"`
	ExternalRoute *ExternalRoute `json:"externalRoute,omitempty"`
	CreatedAt     string         `json:"createdAt"`
	UpdatedAt     string         `json:"updatedAt"`
}

type Binding struct {
	ID                 string `json:"id"`
	HostID             string `json:"hostId"`
	DeploymentID       string `json:"deploymentId"`
	ServiceName        string `json:"serviceName"`
	ContainerPort      int    `json:"containerPort"`
	UpstreamProtocol   string `json:"upstreamProtocol"`
	CertificatePath    string `json:"certificatePath"`
	CertificateKeyPath string `json:"certificateKeyPath"`
	IngressMode        string `json:"ingressMode"`
	Status             string `json:"status"`
	Detail             string `json:"detail"`
	HostIP             string `json:"hostIp"`
	HostPort           int    `json:"hostPort"`
	JobID              string `json:"jobId"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt"`
}

type DNSConnection struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Provider             string `json:"provider"`
	AccessKeyID          string `json:"accessKeyId"`
	CredentialRef        string `json:"-"`
	CredentialConfigured bool   `json:"credentialConfigured"`
	CreatedAt            string `json:"createdAt"`
	UpdatedAt            string `json:"updatedAt"`
}

func NormalizeDomain(input string) (string, error) {
	value := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(input)), ".")
	if value == "" || strings.ContainsAny(value, "/:@* \r\n\t") {
		return "", errors.New("请填写主域名，不要包含协议、端口、路径或通配符")
	}
	ascii, err := idna.Lookup.ToASCII(value)
	if err != nil || len(ascii) > 253 {
		return "", errors.New("主域名格式无效")
	}
	base, err := publicsuffix.EffectiveTLDPlusOne(ascii)
	if err != nil || base != ascii {
		return "", fmt.Errorf("请填写可注册的主域名，如 example.com；%s 是子域名或公共后缀", value)
	}
	return ascii, nil
}

func NormalizeLabel(input string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(input))
	if value == "@" {
		return "", errors.New("根域名已存在，请选择 @ 行")
	}
	if value == "" || strings.ContainsAny(value, ".:/@* \r\n\t") {
		return "", errors.New("只填写一段子域名前缀，例如 api")
	}
	ascii, err := idna.Lookup.ToASCII(value)
	if err != nil || len(ascii) > 63 || !validLabel(ascii) {
		return "", errors.New("子域名前缀格式无效")
	}
	return ascii, nil
}

func validLabel(value string) bool {
	if value == "" || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
			return false
		}
	}
	return true
}

func validateNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if len(note) > 500 || strings.ContainsAny(note, "\x00") {
		return "", errors.New("备注不能超过 500 字节")
	}
	return note, nil
}

func newID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
