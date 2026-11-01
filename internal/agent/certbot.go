package agent

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var fqdnPattern = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9.-]{0,251}[a-zA-Z0-9])?$`)
var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
var letsEncryptDir = "/etc/letsencrypt"

type CertificateInfo struct {
	Certificate    string `json:"certificate"`
	CertificateKey string `json:"certificateKey"`
	ExpiresAt      string `json:"expiresAt"`
}

type CertbotCheck struct {
	Domain           string           `json:"domain"`
	CertbotAvailable bool             `json:"certbotAvailable"`
	ExistingAccount  bool             `json:"existingAccount"`
	NginxContainer   string           `json:"nginxContainer,omitempty"`
	CertificateMount bool             `json:"certificateMount"`
	ChallengeReady   bool             `json:"challengeReady"`
	DNSReady         bool             `json:"dnsReady"`
	Existing         *CertificateInfo `json:"existing,omitempty"`
	Detail           string           `json:"detail,omitempty"`
}

type dockerMount struct {
	Type        string `json:"Type"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
}

func validateFQDN(value string) bool {
	if !fqdnPattern.MatchString(value) || !strings.Contains(value, ".") || strings.Contains(value, "..") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
	}
	return true
}

func nginxMounts(ctx context.Context, container string) ([]dockerMount, error) {
	if !containerIDPattern.MatchString(container) {
		return nil, errors.New("invalid Nginx container")
	}
	data, err := docker(ctx, "inspect", "--format", "{{json .Mounts}}", container)
	if err != nil {
		return nil, err
	}
	var mounts []dockerMount
	if err := json.Unmarshal(data, &mounts); err != nil {
		return nil, err
	}
	return mounts, nil
}

func certbotMount(ctx context.Context, container string) (string, error) {
	mounts, err := nginxMounts(ctx, container)
	if err != nil {
		return "", err
	}
	for _, mount := range mounts {
		if mount.Type == "bind" && filepath.Clean(mount.Source) == letsEncryptDir && filepath.IsAbs(mount.Destination) {
			return filepath.Clean(mount.Destination), nil
		}
	}
	return "", errors.New("Nginx container does not mount the complete /etc/letsencrypt directory")
}

func webrootMounted(ctx context.Context, container, webroot string) bool {
	mounts, err := nginxMounts(ctx, container)
	if err != nil {
		return false
	}
	real, err := filepath.EvalSymlinks(webroot)
	if err != nil {
		return false
	}
	for _, mount := range mounts {
		source := filepath.Clean(mount.Source)
		if mount.Type == "bind" && source != "/" && source != "/etc" && (real == source || strings.HasPrefix(real, source+string(os.PathSeparator))) {
			return true
		}
	}
	return false
}

func discoverCertificate(ctx context.Context, container, domain string) (*CertificateInfo, error) {
	destination, err := certbotMount(ctx, container)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(letsEncryptDir, "live"))
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		name := entry.Name()
		if !containerIDPattern.MatchString(name) {
			continue
		}
		publicPath := filepath.Join(letsEncryptDir, "live", name, "fullchain.pem")
		keyPath := filepath.Join(letsEncryptDir, "live", name, "privkey.pem")
		data, err := os.ReadFile(publicPath)
		if err != nil {
			continue
		}
		block, _ := pem.Decode(data)
		if block == nil {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || cert.VerifyHostname(domain) != nil || time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter) {
			continue
		}
		if _, err := os.Stat(keyPath); err != nil {
			continue
		}
		containerPublic := filepath.Join(destination, "live", name, "fullchain.pem")
		containerKey := filepath.Join(destination, "live", name, "privkey.pem")
		if _, err := docker(ctx, "exec", container, "test", "-r", containerPublic); err != nil {
			continue
		}
		if _, err := docker(ctx, "exec", container, "test", "-r", containerKey); err != nil {
			continue
		}
		return &CertificateInfo{Certificate: containerPublic, CertificateKey: containerKey, ExpiresAt: cert.NotAfter.UTC().Format(time.RFC3339)}, nil
	}
	return nil, nil
}

func probeChallenge(ctx context.Context, domain, webroot string) error {
	if webroot == "" || !filepath.IsAbs(webroot) || filepath.Clean(webroot) != webroot {
		return errors.New("provide an absolute ACME webroot path")
	}
	info, err := os.Stat(webroot)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("ACME webroot is not a directory")
	}
	dir := filepath.Join(webroot, ".well-known", "acme-challenge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	name := "ideploy-" + hex.EncodeToString(random)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
		return err
	}
	defer os.Remove(path)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1/.well-known/acme-challenge/"+name, nil)
	if err != nil {
		return err
	}
	request.Host = domain
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 256))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK || string(data) != name {
		return errors.New("Nginx does not serve the ACME webroot for this domain on port 80")
	}
	return nil
}

func CheckCertbot(ctx context.Context, domain, webroot string) (CertbotCheck, error) {
	if !validateFQDN(domain) {
		return CertbotCheck{}, errors.New("invalid domain")
	}
	value := CertbotCheck{Domain: domain}
	if _, err := exec.LookPath("certbot"); err == nil {
		value.CertbotAvailable = true
	}
	if entries, err := os.ReadDir(filepath.Join(letsEncryptDir, "accounts", "acme-v02.api.letsencrypt.org", "directory")); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				value.ExistingAccount = true
				break
			}
		}
	}
	snapshot, err := ReadNginx(ctx)
	if err != nil {
		value.Detail = err.Error()
		return value, nil
	}
	value.NginxContainer = snapshot.Container
	if _, err := certbotMount(ctx, snapshot.Container); err == nil {
		value.CertificateMount = true
	} else {
		value.Detail = err.Error()
	}
	if value.CertificateMount {
		value.Existing, _ = discoverCertificate(ctx, snapshot.Container, domain)
	}
	if webroot != "" {
		if !webrootMounted(ctx, snapshot.Container, webroot) {
			value.Detail = "ACME webroot is not bind-mounted into the Nginx container"
		} else if err := probeChallenge(ctx, domain, webroot); err == nil {
			value.ChallengeReady = true
		} else {
			value.Detail = err.Error()
		}
	}
	if addresses, err := net.LookupHost(domain); err != nil || len(addresses) == 0 {
		value.Detail = "domain DNS does not resolve"
	} else {
		value.DNSReady = true
	}
	return value, nil
}

func issueCertificate(ctx context.Context, req OperationRequest) (CertificateInfo, error) {
	if !validateFQDN(req.FQDN) || req.Email != "" && !emailPattern.MatchString(req.Email) {
		return CertificateInfo{}, errors.New("invalid certificate request")
	}
	check, err := CheckCertbot(ctx, req.FQDN, req.WebrootHost)
	if err != nil {
		return CertificateInfo{}, err
	}
	if check.Existing != nil {
		return *check.Existing, nil
	}
	if !check.CertbotAvailable || !check.CertificateMount || !check.ChallengeReady || !check.DNSReady {
		return CertificateInfo{}, fmt.Errorf("Certbot preflight failed: %s", check.Detail)
	}
	if req.Email == "" && !check.ExistingAccount {
		return CertificateInfo{}, errors.New("no existing Certbot account; provide a notification email")
	}
	if check.NginxContainer != req.NginxContainer {
		return CertificateInfo{}, errors.New("Nginx container changed since preflight")
	}
	args := []string{"certonly", "--webroot", "-w", req.WebrootHost, "-d", req.FQDN, "--non-interactive", "--agree-tos", "--keep-until-expiring"}
	if req.Email != "" {
		args = append(args, "--email", req.Email)
	}
	command := exec.CommandContext(ctx, "certbot", args...)
	if err := command.Run(); err != nil {
		return CertificateInfo{}, fmt.Errorf("Certbot issuance failed: %w", err)
	}
	cert, err := discoverCertificate(ctx, req.NginxContainer, req.FQDN)
	if err != nil || cert == nil {
		return CertificateInfo{}, errors.New("certificate was issued but is not readable by Nginx; check certificate mount")
	}
	if err := ensureCertbotRenewal(ctx, req.NginxContainer); err != nil {
		return CertificateInfo{}, fmt.Errorf("certificate issued but automatic renewal setup failed: %w", err)
	}
	return *cert, nil
}

func ensureCertbotRenewal(ctx context.Context, container string) error {
	if !containerIDPattern.MatchString(container) {
		return errors.New("invalid Nginx container")
	}
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return err
	}
	certbotPath, err := exec.LookPath("certbot")
	if err != nil {
		return err
	}
	if !regexp.MustCompile(`^/[A-Za-z0-9_./-]+$`).MatchString(dockerPath) || !regexp.MustCompile(`^/[A-Za-z0-9_./-]+$`).MatchString(certbotPath) {
		return errors.New("unexpected Docker or Certbot executable path")
	}
	hookDir := filepath.Join(letsEncryptDir, "renewal-hooks", "deploy")
	if err := os.MkdirAll(hookDir, 0o700); err != nil {
		return err
	}
	hook := filepath.Join(hookDir, "ideploy-nginx-"+container)
	content := fmt.Sprintf("#!/bin/sh\n%s exec %s nginx -t && %s exec %s nginx -s reload\n", dockerPath, container, dockerPath, container)
	if err := os.WriteFile(hook, []byte(content), 0o700); err != nil {
		return err
	}
	if _, err := exec.CommandContext(ctx, "systemctl", "is-enabled", "--quiet", "certbot.timer").Output(); err == nil {
		return nil
	}
	if _, err := exec.CommandContext(ctx, "systemctl", "is-enabled", "--quiet", "snap.certbot.renew.timer").Output(); err == nil {
		return nil
	}
	service := "[Unit]\nDescription=iDeploy Certbot renewal\n[Service]\nType=oneshot\nExecStart=" + certbotPath + " renew --quiet\n"
	timer := "[Unit]\nDescription=iDeploy Certbot renewal schedule\n[Timer]\nOnCalendar=daily\nRandomizedDelaySec=12h\nPersistent=true\n[Install]\nWantedBy=timers.target\n"
	if err := os.WriteFile("/etc/systemd/system/ideploy-certbot-renew.service", []byte(service), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile("/etc/systemd/system/ideploy-certbot-renew.timer", []byte(timer), 0o644); err != nil {
		return err
	}
	if err := exec.CommandContext(ctx, "systemctl", "daemon-reload").Run(); err != nil {
		return err
	}
	return exec.CommandContext(ctx, "systemctl", "enable", "--now", "ideploy-certbot-renew.timer").Run()
}
