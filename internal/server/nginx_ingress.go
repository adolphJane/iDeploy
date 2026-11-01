package server

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const nginxStart = "# BEGIN iDeploy managed Nginx routes"
const nginxEnd = "# END iDeploy managed Nginx routes"

type ExistingNginx struct {
	Container  string
	HostConfig string
	Config     string
	Hash       string
}

var nginxContainerName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
var nginxHTTPBlock = regexp.MustCompile(`(?m)^\s*http\s*\{`)
var nginxServerNames = regexp.MustCompile(`(?m)\bserver_name\s+[^;]*;`)
var nginxServerBlock = regexp.MustCompile(`(?m)^\s*server\s*\{`)
var nginxProxyPass = regexp.MustCompile(`\bproxy_pass\s+([^;\s]+)\s*;`)
var nginxCertificate = regexp.MustCompile(`\bssl_certificate\s+([^;\s]+)\s*;`)
var nginxCertificateKey = regexp.MustCompile(`\bssl_certificate_key\s+([^;\s]+)\s*;`)
var nginxListen = regexp.MustCompile(`\blisten\s+([^;]+);`)
var nginxACMELocation = regexp.MustCompile(`(?s)location\s+\^~\s+/\.well-known/acme-challenge/\s*\{[^{}]*\}`)
var dockerPublishedTCP = regexp.MustCompile(`(?:(\d+\.\d+\.\d+\.\d+|\[::\]|::):)?([0-9]+)->([0-9]+)/tcp`)

type NginxObservedRoute struct {
	FQDN           string `json:"fqdn"`
	Upstream       string `json:"upstream"`
	Service        string `json:"service"`
	HTTP           bool   `json:"http"`
	HTTPS          bool   `json:"https"`
	Certificate    string `json:"certificate"`
	CertificateKey string `json:"certificateKey"`
	Managed        bool   `json:"managed"`
}

type NginxScan struct {
	Container  string                 `json:"container"`
	ObservedAt string                 `json:"observedAt"`
	Routes     []NginxObservedRoute   `json:"routes"`
	Services   []NginxServiceEndpoint `json:"services"`
}

type NginxServiceEndpoint struct {
	Container     string `json:"container"`
	Service       string `json:"service"`
	HostIP        string `json:"hostIp"`
	HostPort      int    `json:"hostPort"`
	ContainerPort int    `json:"containerPort"`
}

type agentNginxSnapshot struct {
	Container  string `json:"container"`
	HostConfig string `json:"hostConfig"`
	Config     string `json:"config"`
	Hash       string `json:"hash"`
	Effective  string `json:"effective"`
	Services   string `json:"services"`
}

func (r *Remote) readAgentNginx(ctx context.Context, connection Connection) (agentNginxSnapshot, error) {
	if connection.ID == "" {
		return agentNginxSnapshot{}, errors.New("server ID unavailable")
	}
	data, err := r.AgentGET(ctx, connection, connection.ID, "/v1/nginx")
	if err != nil {
		return agentNginxSnapshot{}, err
	}
	var value agentNginxSnapshot
	if err := json.Unmarshal(data, &value); err != nil {
		return agentNginxSnapshot{}, err
	}
	if !nginxContainerName.MatchString(value.Container) || value.Config == "" || CaddyfileHash(value.Config) != value.Hash {
		return agentNginxSnapshot{}, errors.New("invalid Agent Nginx snapshot")
	}
	return value, nil
}

func parseDockerPublishedServices(output string) []NginxServiceEndpoint {
	result := []NginxServiceEndpoint{}
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.SplitN(line, "|", 4)
		if len(fields) != 4 || fields[0] == "" {
			continue
		}
		service := fields[0]
		if fields[2] != "" && fields[3] != "" {
			service = fields[2] + " / " + fields[3]
		}
		for _, match := range dockerPublishedTCP.FindAllStringSubmatch(fields[1], -1) {
			hostPort, _ := strconv.Atoi(match[2])
			containerPort, _ := strconv.Atoi(match[3])
			if hostPort < 1 || hostPort > 65535 || containerPort < 1 || containerPort > 65535 {
				continue
			}
			key := fields[0] + ":" + match[2] + ":" + match[3]
			if seen[key] {
				continue
			}
			seen[key] = true
			hostIP := match[1]
			if hostIP == "" || hostIP == "0.0.0.0" {
				hostIP = "127.0.0.1"
			}
			if hostIP == "[::]" || hostIP == "::" {
				hostIP = "::1"
			}
			result = append(result, NginxServiceEndpoint{Container: fields[0], Service: service, HostIP: hostIP, HostPort: hostPort, ContainerPort: containerPort})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Service == result[j].Service {
			return result[i].HostPort < result[j].HostPort
		}
		return result[i].Service < result[j].Service
	})
	return result
}

func parseNginxRoutes(config string) []NginxObservedRoute {
	byName := map[string]*NginxObservedRoute{}
	for _, span := range nginxServerBlock.FindAllStringIndex(config, -1) {
		end, err := nginxBlockEnd(config, span[1]-1)
		if err != nil {
			continue
		}
		block := config[span[0] : end+1]
		var names []string
		for _, directive := range nginxServerNames.FindAllString(block, -1) {
			names = append(names, strings.Fields(strings.TrimSuffix(strings.TrimPrefix(directive, "server_name"), ";"))...)
		}
		var http, https bool
		for _, match := range nginxListen.FindAllStringSubmatch(block, -1) {
			fields := strings.Fields(match[1])
			if len(fields) == 0 {
				continue
			}
			port := fields[0]
			if port == "80" || strings.HasSuffix(port, ":80") {
				http = true
			}
			if port == "443" || strings.HasSuffix(port, ":443") {
				https = true
			}
		}
		upstream, cert, key := "", "", ""
		if match := nginxProxyPass.FindStringSubmatch(block); len(match) == 2 {
			upstream = match[1]
		}
		if match := nginxCertificate.FindStringSubmatch(block); len(match) == 2 {
			cert = match[1]
		}
		if match := nginxCertificateKey.FindStringSubmatch(block); len(match) == 2 {
			key = match[1]
		}
		for _, name := range names {
			if !validIngressFQDN(name) {
				continue
			}
			item := byName[name]
			if item == nil {
				item = &NginxObservedRoute{FQDN: name}
				byName[name] = item
			}
			item.HTTP = item.HTTP || http
			item.HTTPS = item.HTTPS || https
			if upstream != "" {
				item.Upstream = upstream
			}
			if cert != "" {
				item.Certificate = cert
			}
			if key != "" {
				item.CertificateKey = key
			}
			item.Managed = item.Managed || strings.Contains(block, "X-iDeploy-Route")
		}
	}
	result := make([]NginxObservedRoute, 0, len(byName))
	for _, item := range byName {
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].FQDN < result[j].FQDN })
	return result
}

func (r *Remote) ScanExistingNginx(ctx context.Context, connection Connection) (NginxScan, error) {
	if snapshot, err := r.readAgentNginx(ctx, connection); err == nil {
		return buildNginxScan(snapshot.Container, snapshot.Effective, snapshot.Services), nil
	}
	item, err := r.InspectExistingNginx(ctx, connection)
	if err != nil {
		return NginxScan{}, err
	}
	all, err := r.runTimeout(ctx, connection, "docker exec "+shellQuote(item.Container)+" nginx -T 2>/dev/null", 25*time.Second)
	if err != nil {
		return NginxScan{}, fmt.Errorf("读取 Nginx 有效配置失败: %w", err)
	}
	containers, listErr := r.runTimeout(ctx, connection, `docker ps --format '{{.Names}}|{{.Ports}}|{{.Label "com.docker.compose.project"}}|{{.Label "com.docker.compose.service"}}'`, 15*time.Second)
	if listErr != nil {
		containers = ""
	}
	return buildNginxScan(item.Container, all, containers), nil
}

func buildNginxScan(container, all, containers string) NginxScan {
	routes := parseNginxRoutes(all)
	services := parseDockerPublishedServices(containers)
	if containers != "" {
		for i := range routes {
			upstream, parseErr := url.Parse(routes[i].Upstream)
			if parseErr != nil || upstream.Port() == "" {
				continue
			}
			needle := ":" + upstream.Port() + "->"
			for _, line := range strings.Split(containers, "\n") {
				fields := strings.SplitN(line, "|", 4)
				if len(fields) != 4 || !strings.Contains(fields[1], needle) {
					continue
				}
				routes[i].Service = fields[0]
				if fields[2] != "" {
					routes[i].Service = fields[2] + " / " + fields[3]
				}
				break
			}
		}
	}
	return NginxScan{Container: container, ObservedAt: time.Now().UTC().Format(time.RFC3339), Routes: routes, Services: services}
}

func nginxDeclaresName(config, fqdn string) bool {
	for _, directive := range nginxServerNames.FindAllString(config, -1) {
		for _, name := range strings.Fields(strings.TrimSuffix(strings.TrimPrefix(directive, "server_name"), ";")) {
			if name == fqdn {
				return true
			}
		}
	}
	return false
}

// InspectExistingNginx accepts only one running host-network Nginx container
// whose main configuration is bind-mounted from a writable host file.
func (r *Remote) InspectExistingNginx(ctx context.Context, connection Connection) (ExistingNginx, error) {
	if snapshot, err := r.readAgentNginx(ctx, connection); err == nil {
		return ExistingNginx{Container: snapshot.Container, HostConfig: snapshot.HostConfig, Config: snapshot.Config, Hash: snapshot.Hash}, nil
	}
	names, err := r.runTimeout(ctx, connection, `docker ps --format '{{.Names}}'`, 15*time.Second)
	if err != nil {
		return ExistingNginx{}, err
	}
	var found []ExistingNginx
	for _, name := range strings.Fields(names) {
		if !nginxContainerName.MatchString(name) {
			continue
		}
		version, err := r.runTimeout(ctx, connection, "docker exec "+shellQuote(name)+" nginx -v 2>&1", 12*time.Second)
		if err != nil || !strings.Contains(version, "nginx version:") {
			continue
		}
		metadata, err := r.runTimeout(ctx, connection, "docker inspect --format '{{.HostConfig.NetworkMode}}|{{range .Mounts}}{{if eq .Destination \"/etc/nginx/nginx.conf\"}}{{.Type}}:{{.Source}}{{end}}{{end}}' "+shellQuote(name), 12*time.Second)
		if err != nil {
			return ExistingNginx{}, err
		}
		mode, mount, ok := strings.Cut(strings.TrimSpace(metadata), "|")
		if !ok || mode != "host" || !strings.HasPrefix(mount, "bind:/") {
			continue
		}
		path := strings.TrimPrefix(mount, "bind:")
		if strings.ContainsAny(path, "\r\n") {
			continue
		}
		found = append(found, ExistingNginx{Container: name, HostConfig: path})
	}
	if len(found) != 1 {
		return ExistingNginx{}, fmt.Errorf("需要恰好一个使用 host 网络、挂载主配置文件的 Nginx 容器；当前找到 %d 个", len(found))
	}
	item := found[0]
	content, err := r.runTimeout(ctx, connection, "cat "+shellQuote(item.HostConfig), 12*time.Second)
	if err != nil {
		return ExistingNginx{}, fmt.Errorf("读取 Nginx 主配置失败: %w", err)
	}
	if len(content) > 256*1024 {
		return ExistingNginx{}, errors.New("Nginx 主配置过大，停止自动编辑")
	}
	item.Config = content
	item.Hash = CaddyfileHash(content)
	return item, nil
}

var nginxCertPath = regexp.MustCompile(`^/[A-Za-z0-9_./-]+$`)

// NginxManagedBlock enables TLS only with an existing certificate/key pair.
func NginxManagedBlock(routes []IngressRoute, acmeLocation ...string) (string, error) {
	items := append([]IngressRoute(nil), routes...)
	sort.Slice(items, func(i, j int) bool { return items[i].FQDN < items[j].FQDN })
	seen := map[string]bool{}
	var out strings.Builder
	out.WriteString("    " + nginxStart + "\n")
	for _, route := range items {
		if !validIngressFQDN(route.FQDN) || seen[route.FQDN] || route.HostPort < 1 || route.HostPort > 65535 {
			return "", errors.New("Nginx 路由域名或端口无效")
		}
		seen[route.FQDN] = true
		ip := net.ParseIP(route.HostIP)
		if ip == nil {
			return "", errors.New("Nginx 后端地址无效")
		}
		if ip.IsUnspecified() {
			if ip.To4() != nil {
				ip = net.ParseIP("127.0.0.1")
			} else {
				ip = net.ParseIP("::1")
			}
		}
		scheme := route.UpstreamScheme
		if scheme == "" {
			scheme = "http"
		}
		if scheme != "http" && scheme != "https" {
			return "", errors.New("Nginx 后端协议无效")
		}
		tls := route.Certificate != "" || route.CertificateKey != ""
		if tls && (!nginxCertPath.MatchString(route.Certificate) || !nginxCertPath.MatchString(route.CertificateKey)) {
			return "", errors.New("HTTPS 需要填写有效的容器内证书和私钥路径")
		}
		upstream := net.JoinHostPort(ip.String(), strconv.Itoa(route.HostPort))
		out.WriteString("    server {\n        listen 80;\n        server_name " + route.FQDN + ";\n")
		if tls {
			out.WriteString("        add_header X-iDeploy-Route " + IngressRouteMarker(route.FQDN) + " always;\n")
			if len(acmeLocation) > 0 && acmeLocation[0] != "" {
				out.WriteString("        " + strings.TrimSpace(acmeLocation[0]) + "\n")
			}
			out.WriteString("        location / { return 301 https://$host$request_uri; }\n    }\n")
			out.WriteString("    server {\n        listen 443 ssl;\n        server_name " + route.FQDN + ";\n")
			out.WriteString("        ssl_certificate " + route.Certificate + ";\n        ssl_certificate_key " + route.CertificateKey + ";\n")
		}
		out.WriteString("        location / {\n")
		out.WriteString("            add_header X-iDeploy-Route " + IngressRouteMarker(route.FQDN) + " always;\n")
		out.WriteString("            proxy_set_header Host $host;\n            proxy_set_header X-Real-IP $remote_addr;\n")
		out.WriteString("            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n            proxy_set_header X-Forwarded-Proto $scheme;\n")
		out.WriteString("            proxy_set_header Upgrade $http_upgrade;\n            proxy_set_header Connection \"upgrade\";\n")
		out.WriteString("            proxy_read_timeout 3600s;\n            proxy_pass " + scheme + "://" + upstream + ";\n        }\n    }\n")
	}
	out.WriteString("    " + nginxEnd + "\n")
	return out.String(), nil
}

func NginxConfigWithRoutes(config string, routes []IngressRoute) (string, string, error) {
	start, end := strings.Index(config, nginxStart), strings.Index(config, nginxEnd)
	if (start < 0) != (end < 0) || start >= 0 && (strings.Count(config, nginxStart) != 1 || strings.Count(config, nginxEnd) != 1 || end < start) {
		return "", "", errors.New("Nginx 受管区块标记不完整，停止覆盖")
	}
	if start >= 0 {
		begin := strings.LastIndex(config[:start], "\n") + 1
		finish := end + len(nginxEnd)
		if finish < len(config) && config[finish] == '\n' {
			finish++
		}
		config = config[:begin] + config[finish:]
	}
	challenge := nginxACMELocation.FindString(config)
	block, err := NginxManagedBlock(routes, challenge)
	if err != nil {
		return "", "", err
	}
	if len(routes) == 0 {
		block = ""
	}
	if len(nginxHTTPBlock.FindAllStringIndex(config, -1)) != 1 {
		return "", "", errors.New("Nginx 主配置必须包含唯一的 http 区块")
	}
	opening := nginxHTTPBlock.FindStringIndex(config)[1] - 1
	closing, err := nginxBlockEnd(config, opening)
	if err != nil {
		return "", "", err
	}
	for _, route := range routes {
		if nginxDeclaresName(config, route.FQDN) {
			return "", "", fmt.Errorf("现有 Nginx 配置已声明 %s，停止覆盖", route.FQDN)
		}
	}
	return config[:closing] + block + config[closing:], block, nil
}

func nginxBlockEnd(config string, opening int) (int, error) {
	depth, quote, comment, escape := 0, byte(0), false, false
	for i := opening; i < len(config); i++ {
		ch := config[i]
		if comment {
			if ch == '\n' {
				comment = false
			}
			continue
		}
		if escape {
			escape = false
			continue
		}
		if ch == '\\' {
			escape = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '#' {
			comment = true
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == '{' {
			depth++
		}
		if ch == '}' {
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, errors.New("Nginx http 区块未闭合")
}

func (r *Remote) PreviewExistingNginx(ctx context.Context, connection Connection, routes []IngressRoute, expectedHash string) (ExistingNginx, string, string, error) {
	item, err := r.InspectExistingNginx(ctx, connection)
	if err != nil {
		return item, "", "", err
	}
	if _, err := r.runTimeout(ctx, connection, "test -w "+shellQuote(item.HostConfig), 8*time.Second); err != nil {
		return item, "", "", errors.New("当前 SSH 用户不能写入 Nginx 主配置文件")
	}
	if expectedHash == "" {
		if strings.Contains(item.Config, nginxStart) {
			return item, "", "", errors.New("Nginx 已有 iDeploy 受管区块，但本地没有配置记录，请先核对")
		}
	} else if item.Hash != expectedHash {
		return item, "", "", errors.New("Nginx 主配置在上次应用后发生变化，请先核对配置漂移")
	}
	config, block, err := NginxConfigWithRoutes(item.Config, routes)
	if err != nil {
		return item, "", "", err
	}
	for _, route := range routes {
		if route.Certificate == "" {
			continue
		}
		if _, checkErr := r.runTimeout(ctx, connection, "docker exec "+shellQuote(item.Container)+" test -r "+shellQuote(route.CertificateKey), 8*time.Second); checkErr != nil {
			return item, "", "", fmt.Errorf("Nginx 容器无法读取 %s 的证书私钥", route.FQDN)
		}
		publicPEM, readErr := r.runTimeout(ctx, connection, "docker exec "+shellQuote(item.Container)+" cat "+shellQuote(route.Certificate), 8*time.Second)
		if readErr != nil {
			return item, "", "", fmt.Errorf("Nginx 容器无法读取 %s 的证书", route.FQDN)
		}
		pemBlock, _ := pem.Decode([]byte(publicPEM))
		if pemBlock == nil {
			return item, "", "", errors.New("证书文件不是 PEM 格式")
		}
		certificate, parseErr := x509.ParseCertificate(pemBlock.Bytes)
		if parseErr != nil || certificate.VerifyHostname(route.FQDN) != nil || time.Now().Before(certificate.NotBefore) || time.Now().After(certificate.NotAfter) {
			return item, "", "", fmt.Errorf("现有证书不覆盖 %s 或已过期，请先配置匹配证书", route.FQDN)
		}
	}
	// Included files may declare the same hostname. Reject these before editing.
	all, err := r.runTimeout(ctx, connection, "docker exec "+shellQuote(item.Container)+" nginx -T 2>/dev/null", 20*time.Second)
	if err != nil {
		return item, "", "", fmt.Errorf("读取 Nginx 有效配置失败: %w", err)
	}
	if start := strings.Index(all, nginxStart); start >= 0 {
		if end := strings.Index(all[start:], nginxEnd); end >= 0 {
			all = all[:start] + all[start+end+len(nginxEnd):]
		}
	}
	for _, route := range routes {
		if nginxDeclaresName(all, route.FQDN) {
			return item, "", "", fmt.Errorf("Nginx 有效配置已声明 %s，停止覆盖", route.FQDN)
		}
	}
	return item, config, block, nil
}

// ApplyExistingNginx keeps the original inode for file bind mounts. It backs
// up before writing, validates in the container, and restores on failure.
func (r *Remote) ApplyExistingNginx(ctx context.Context, connection Connection, routes []IngressRoute, expectedHash, expectedContainer string) (string, error) {
	item, config, _, err := r.PreviewExistingNginx(ctx, connection, routes, expectedHash)
	if err != nil {
		return "", err
	}
	if expectedContainer != "" && item.Container != expectedContainer {
		return "", errors.New("Nginx 容器已变化，请重新预检")
	}
	if config == item.Config {
		return item.Hash, nil
	}
	candidate := `"$HOME/.ideploy/nginx/config.candidate"`
	if _, err = r.runTimeoutWithInput(ctx, connection, `umask 077; mkdir -p "$HOME/.ideploy/nginx"; cat > `+candidate, 20*time.Second, config); err != nil {
		return "", err
	}
	path := shellQuote(item.HostConfig)
	backup := `"$HOME/.ideploy/nginx/config-` + strconv.FormatInt(time.Now().UnixNano(), 10) + `.backup"`
	commit := "set -e; test \"$(sha256sum " + path + " | cut -d ' ' -f 1)\" = " + shellQuote(item.Hash) +
		" || { echo 'Nginx 配置已变化' >&2; exit 1; }; cp -p " + path + " " + backup + "; cat " + candidate + " > " + path
	rollback := "set -e; cat " + backup + " > " + path + "; docker exec " + shellQuote(item.Container) + " nginx -t >/dev/null 2>&1; docker exec " + shellQuote(item.Container) + " nginx -s reload >/dev/null 2>&1"
	restore := func() error {
		restoreCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, restoreErr := r.runTimeout(restoreCtx, connection, rollback, 25*time.Second)
		return restoreErr
	}
	if _, err = r.runTimeout(ctx, connection, commit, 20*time.Second); err != nil {
		restoreCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, _ = r.runTimeout(restoreCtx, connection, "if test -f "+backup+"; then "+rollback+"; fi", 25*time.Second)
		cancel()
		return "", err
	}
	check := "docker exec " + shellQuote(item.Container) + " nginx -t"
	if _, err = r.runTimeout(ctx, connection, check, 20*time.Second); err != nil {
		restoreErr := restore()
		return "", fmt.Errorf("Nginx 配置校验失败，已尝试恢复备份（恢复结果：%v）: %w", restoreErr, err)
	}
	if _, err = r.runTimeout(ctx, connection, "docker exec "+shellQuote(item.Container)+" nginx -s reload", 20*time.Second); err != nil {
		restoreErr := restore()
		return "", fmt.Errorf("Nginx 重载失败，已尝试恢复备份（恢复结果：%v）: %w", restoreErr, err)
	}
	for _, route := range routes {
		var probeErr error
		for attempt := 0; attempt < 5; attempt++ {
			probeErr = r.ProbeNginxRoute(ctx, connection, route.FQDN)
			if probeErr == nil {
				break
			}
			if ctx.Err() != nil {
				break
			}
			time.Sleep(time.Second)
		}
		if probeErr != nil {
			restoreErr := restore()
			return "", fmt.Errorf("Nginx 重载后路由校验失败，已尝试恢复备份（恢复结果：%v）: %w", restoreErr, probeErr)
		}
		if route.Certificate != "" {
			if tlsErr := r.ProbeNginxHTTPSRoute(ctx, connection, route.FQDN); tlsErr != nil {
				restoreErr := restore()
				return "", fmt.Errorf("Nginx HTTPS 校验失败，已尝试恢复备份（恢复结果：%v）: %w", restoreErr, tlsErr)
			}
		}
	}
	hash := sha256.Sum256([]byte(config))
	return hex.EncodeToString(hash[:]), nil
}

func (r *Remote) ProbeNginxHTTPSRoute(ctx context.Context, connection Connection, fqdn string) error {
	if !validIngressFQDN(fqdn) {
		return errors.New("域名无效")
	}
	command := "curl --noproxy '*' --connect-timeout 3 --max-time 8 -sSI --resolve " + shellQuote(fqdn+":443:127.0.0.1") + " " + shellQuote("https://"+fqdn+"/")
	output, err := r.runTimeout(ctx, connection, command, 12*time.Second)
	if err != nil {
		return fmt.Errorf("Nginx HTTPS 或证书校验失败: %w", err)
	}
	if !strings.Contains(strings.ToLower(output), "x-ideploy-route: "+IngressRouteMarker(fqdn)) {
		return errors.New("HTTPS 已响应，但未命中 iDeploy 域名路由")
	}
	return nil
}

func (r *Remote) ProbeNginxRoute(ctx context.Context, connection Connection, fqdn string) error {
	if !validIngressFQDN(fqdn) {
		return errors.New("域名无效")
	}
	command := "curl --noproxy '*' --connect-timeout 3 --max-time 8 -sSI -H " + shellQuote("Host: "+fqdn) + " http://127.0.0.1/"
	output, err := r.runTimeout(ctx, connection, command, 12*time.Second)
	if err != nil {
		return fmt.Errorf("Nginx HTTP 路由不可达: %w", err)
	}
	if !strings.Contains(strings.ToLower(output), "x-ideploy-route: "+IngressRouteMarker(fqdn)) {
		return errors.New("Nginx 已响应，但未命中 iDeploy 域名路由")
	}
	return nil
}
