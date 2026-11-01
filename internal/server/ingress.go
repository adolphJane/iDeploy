package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const ManagedCaddyImage = "caddy:2.11.7-alpine"
const managedCaddyName = "ideploy-ingress"

type IngressRoute struct {
	FQDN           string
	HostIP         string
	HostPort       int
	UpstreamScheme string
	Certificate    string
	CertificateKey string
}

func ManagedCaddyfile(routes []IngressRoute) (string, error) {
	items := append([]IngressRoute(nil), routes...)
	sort.Slice(items, func(i, j int) bool { return items[i].FQDN < items[j].FQDN })
	var out strings.Builder
	out.WriteString("# Managed by iDeploy. Changes outside iDeploy block later updates.\n{\n    admin localhost:2019\n}\n")
	seen := map[string]bool{}
	for _, route := range items {
		if !validIngressFQDN(route.FQDN) || seen[route.FQDN] || route.HostPort < 1 || route.HostPort > 65535 {
			return "", errors.New("invalid or duplicate ingress route")
		}
		seen[route.FQDN] = true
		ip := net.ParseIP(route.HostIP)
		if ip == nil {
			return "", errors.New("invalid upstream address")
		}
		if ip.IsUnspecified() {
			if ip.To4() != nil {
				ip = net.ParseIP("127.0.0.1")
			} else {
				ip = net.ParseIP("::1")
			}
		}
		out.WriteString("\n" + route.FQDN + " {\n    header X-iDeploy-Route " + IngressRouteMarker(route.FQDN) + "\n    reverse_proxy " + net.JoinHostPort(ip.String(), strconv.Itoa(route.HostPort)) + "\n}\n")
	}
	return out.String(), nil
}

func validIngressFQDN(value string) bool {
	if value == "" || len(value) > 253 || strings.ToLower(value) != value || !strings.Contains(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return true
}

func CaddyfileHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func IngressRouteMarker(fqdn string) string {
	sum := sha256.Sum256([]byte(fqdn))
	return hex.EncodeToString(sum[:8])
}

type ManagedIngressState struct {
	Exists  bool
	Running bool
	Hash    string
}

func (r *Remote) InspectManagedIngress(ctx context.Context, connection Connection) (ManagedIngressState, error) {
	command := `if [ "$(uname -s)" != Linux ] || [ "$(docker info --format '{{.OSType}}' 2>/dev/null)" != linux ]; then echo UNSUPPORTED; exit 0; fi
if docker container inspect ideploy-ingress >/dev/null 2>&1; then
  docker container inspect ideploy-ingress --format '{{index .Config.Labels "com.ideploy.managed"}}|{{.State.Running}}'
else echo ABSENT; fi
if [ -f "$HOME/.ideploy/ingress/config/Caddyfile" ]; then sha256sum "$HOME/.ideploy/ingress/config/Caddyfile" | cut -d ' ' -f 1; else echo NOFILE; fi`
	output, err := r.runTimeout(ctx, connection, command, 20*time.Second)
	if err != nil {
		return ManagedIngressState{}, err
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if lines[0] == "UNSUPPORTED" {
		return ManagedIngressState{}, errors.New("受管入口需要 Linux Docker 主机")
	}
	if len(lines) < 2 {
		return ManagedIngressState{}, errors.New("入口状态响应不完整")
	}
	state := ManagedIngressState{}
	if lines[0] != "ABSENT" {
		parts := strings.Split(lines[0], "|")
		if len(parts) != 2 || parts[0] != "ingress" {
			return ManagedIngressState{}, errors.New("同名容器不属于 iDeploy，停止自动接管")
		}
		state.Exists = true
		state.Running = parts[1] == "true"
	}
	if lines[1] != "NOFILE" {
		state.Hash = lines[1]
	}
	return state, nil
}

func (r *Remote) CheckIngressPortsFree(ctx context.Context, connection Connection) error {
	command := `if ! command -v ss >/dev/null 2>&1; then echo NO_SS; exit 0; fi
ss -H -ltnp '( sport = :80 )' | sed 's/^/80\/TCP|/'
ss -H -ltnp '( sport = :443 )' | sed 's/^/443\/TCP|/'
ss -H -ltnp '( sport = :2019 )' | sed 's/^/2019\/TCP|/'
ss -H -lunp '( sport = :443 )' | sed 's/^/443\/UDP|/'`
	output, err := r.runTimeout(ctx, connection, command, 15*time.Second)
	if err != nil {
		return err
	}
	if strings.TrimSpace(output) == "NO_SS" {
		return errors.New("服务器缺少 ss，无法确认入口端口 80/TCP、443/TCP、2019/TCP 和 443/UDP 是否空闲")
	}
	if detail := ingressPortConflictDetail(output); detail != "" {
		return errors.New(detail)
	}
	return nil
}

var ingressProcessName = regexp.MustCompile(`users:\(\("([^"]+)"`)

func ingressPortConflictDetail(output string) string {
	var conflicts []string
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 2)
		if len(parts) != 2 || parts[1] == "" {
			continue
		}
		item := parts[0]
		if match := ingressProcessName.FindStringSubmatch(parts[1]); len(match) == 2 {
			item += "（" + match[1] + "）"
		}
		if !containsString(conflicts, item) {
			conflicts = append(conflicts, item)
		}
	}
	if len(conflicts) == 0 {
		return ""
	}
	return "入口端口已被占用：" + strings.Join(conflicts, "、") + "。iDeploy 受管入口需要这些端口空闲；请先核对现有代理配置，不会自动停止或接管它。应用后端端口不受此冲突影响。"
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func (r *Remote) ProbeIngressBackend(ctx context.Context, connection Connection, hostIP string, hostPort int, fqdn string) error {
	ip := net.ParseIP(hostIP)
	if ip == nil || hostPort < 1 || hostPort > 65535 || !validIngressFQDN(fqdn) {
		return errors.New("后端地址无效")
	}
	if ip.IsUnspecified() {
		if ip.To4() != nil {
			ip = net.ParseIP("127.0.0.1")
		} else {
			ip = net.ParseIP("::1")
		}
	}
	url := "http://" + net.JoinHostPort(ip.String(), strconv.Itoa(hostPort)) + "/"
	command := "curl --noproxy '*' --connect-timeout 3 --max-time 6 -sS -o /dev/null -w '%{http_code}' -H " + shellQuote("Host: "+fqdn) + " " + shellQuote(url)
	output, err := r.runTimeout(ctx, connection, command, 12*time.Second)
	if err != nil {
		return fmt.Errorf("远端 HTTP 后端不可达: %w", err)
	}
	status, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil || status < 100 || status > 599 {
		return errors.New("后端未返回有效 HTTP 响应")
	}
	if status >= 500 {
		return fmt.Errorf("后端返回 HTTP %d，请先检查应用服务", status)
	}
	return nil
}

func (r *Remote) ApplyManagedIngress(ctx context.Context, connection Connection, caddyfile, expectedHash string) (string, error) {
	desired := CaddyfileHash(caddyfile)
	state, err := r.InspectManagedIngress(ctx, connection)
	if err != nil {
		return "", err
	}
	if state.Exists && state.Running && state.Hash == desired {
		if _, err := r.runTimeout(ctx, connection, `docker exec ideploy-ingress caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile`, 45*time.Second); err != nil {
			return "", err
		}
		if err := r.verifyManagedIngress(ctx, connection); err != nil {
			return "", err
		}
		return desired, nil
	}
	if expectedHash == "" {
		if state.Exists && state.Hash != desired {
			return "", errors.New("服务器已有受管入口，但本地没有其配置记录；请先核对")
		}
		if !state.Exists && state.Hash != "" && state.Hash != desired {
			return "", errors.New("服务器已有入口配置文件，停止自动覆盖")
		}
	} else if state.Hash != expectedHash && state.Hash != desired {
		return "", errors.New("服务器受管入口配置已被外部修改，请先核对配置漂移")
	}
	if !state.Exists {
		if expectedHash != "" {
			return "", errors.New("受管入口容器已丢失，请先核对远端状态")
		}
		if err := r.CheckIngressPortsFree(ctx, connection); err != nil {
			return "", err
		}
	}
	if _, err := r.runTimeout(ctx, connection, `umask 077; mkdir -p "$HOME/.ideploy/ingress/config" "$HOME/.ideploy/ingress/data" "$HOME/.ideploy/ingress/caddy-config"`, 20*time.Second); err != nil {
		return "", err
	}
	if _, err := r.runTimeout(ctx, connection, "docker pull "+shellQuote(ManagedCaddyImage), 3*time.Minute); err != nil {
		return "", err
	}
	if _, err := r.runTimeoutWithInput(ctx, connection, `umask 077; cat > "$HOME/.ideploy/ingress/config/Caddyfile.candidate"`, 20*time.Second, caddyfile); err != nil {
		return "", err
	}
	validate := `docker run --rm -v "$HOME/.ideploy/ingress/config:/etc/caddy:ro" ` + shellQuote(ManagedCaddyImage) + ` caddy validate --config /etc/caddy/Caddyfile.candidate --adapter caddyfile`
	if _, err := r.runTimeout(ctx, connection, validate, 45*time.Second); err != nil {
		return "", fmt.Errorf("入口配置校验失败: %w", err)
	}
	commit := `set -e; if [ -f "$HOME/.ideploy/ingress/config/Caddyfile" ]; then cp "$HOME/.ideploy/ingress/config/Caddyfile" "$HOME/.ideploy/ingress/config/Caddyfile.prev"; fi; mv "$HOME/.ideploy/ingress/config/Caddyfile.candidate" "$HOME/.ideploy/ingress/config/Caddyfile"`
	if _, err := r.runTimeout(ctx, connection, commit, 20*time.Second); err != nil {
		return "", err
	}
	if !state.Exists {
		start := `docker run -d --name ideploy-ingress --label com.ideploy.managed=ingress --restart unless-stopped --network host ` +
			`-v "$HOME/.ideploy/ingress/config:/etc/caddy:ro" -v "$HOME/.ideploy/ingress/data:/data" -v "$HOME/.ideploy/ingress/caddy-config:/config" ` + shellQuote(ManagedCaddyImage)
		_, err = r.runTimeout(ctx, connection, start, 60*time.Second)
	} else if !state.Running {
		_, err = r.runTimeout(ctx, connection, "docker start ideploy-ingress", 45*time.Second)
	} else {
		_, err = r.runTimeout(ctx, connection, `docker exec ideploy-ingress caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile`, 45*time.Second)
	}
	if err != nil {
		_, _ = r.runTimeout(context.Background(), connection, `if [ -f "$HOME/.ideploy/ingress/config/Caddyfile.prev" ]; then cp "$HOME/.ideploy/ingress/config/Caddyfile.prev" "$HOME/.ideploy/ingress/config/Caddyfile"; docker exec ideploy-ingress caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1 || true; fi`, 30*time.Second)
		return "", fmt.Errorf("应用入口配置失败: %w", err)
	}
	verified, err := r.InspectManagedIngress(ctx, connection)
	if err != nil {
		return "", err
	}
	if !verified.Running || verified.Hash != desired {
		return "", errors.New("入口配置提交后未能确认运行状态，请重试核对")
	}
	if err := r.verifyManagedIngress(ctx, connection); err != nil {
		return "", err
	}
	return desired, nil
}

func (r *Remote) verifyManagedIngress(ctx context.Context, connection Connection) error {
	command := `i=0; while [ "$i" -lt 5 ]; do if curl --noproxy '*' -fsS --max-time 2 http://127.0.0.1:2019/config/ >/dev/null 2>&1; then exit 0; fi; i=$((i+1)); sleep 1; done; exit 1`
	if _, err := r.runTimeout(ctx, connection, command, 20*time.Second); err != nil {
		return errors.New("受管入口运行中，但管理接口未就绪，请检查远端容器日志")
	}
	return nil
}

// RemoveManagedIngress removes only the container carrying iDeploy's ownership label.
// Certificate data is retained so a later binding can reuse existing ACME state.
func (r *Remote) RemoveManagedIngress(ctx context.Context, connection Connection, expectedHash string) error {
	state, err := r.InspectManagedIngress(ctx, connection)
	if err != nil {
		return err
	}
	if !state.Exists && state.Hash == "" {
		return nil
	}
	if expectedHash == "" || state.Hash != expectedHash {
		return errors.New("入口配置版本不匹配，停止自动清理")
	}
	if state.Exists {
		if _, err := r.runTimeout(ctx, connection, "docker rm -f ideploy-ingress", 45*time.Second); err != nil {
			return err
		}
	}
	_, err = r.runTimeout(ctx, connection, `rm -f "$HOME/.ideploy/ingress/config/Caddyfile" "$HOME/.ideploy/ingress/config/Caddyfile.candidate"`, 20*time.Second)
	return err
}
