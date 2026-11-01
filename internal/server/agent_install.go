package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func packagedAgentPath(arch string) (string, error) {
	if arch != "amd64" && arch != "arm64" {
		return "", errors.New("暂不支持此服务器架构")
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(executable), "..", "Resources", "agents", "ideploy-agent-linux-"+arch)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("应用包缺少 Linux %s Agent: %w", arch, err)
	}
	return path, nil
}

func BundledAgentHash(arch string) (string, error) {
	path, err := packagedAgentPath(arch)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (r *Remote) InstallAgent(ctx context.Context, connection Connection, serverID string) error {
	platform, err := r.runTimeout(ctx, connection, "uname -s; uname -m; id -u; command -v systemctl", 15*time.Second)
	if err != nil {
		return fmt.Errorf("检查 Agent 安装环境: %w", err)
	}
	fields := strings.Fields(platform)
	if len(fields) < 4 || fields[0] != "Linux" || fields[2] != "0" {
		return errors.New("Agent 首期仅支持具有 root SSH 权限的 systemd Linux 服务器")
	}
	arch := fields[1]
	if arch == "x86_64" {
		arch = "amd64"
	}
	if arch == "aarch64" {
		arch = "arm64"
	}
	path, err := packagedAgentPath(arch)
	if err != nil {
		return err
	}
	binary, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(binary)
	hash := hex.EncodeToString(sum[:])
	ref := AgentPairingCredentialRef(serverID)
	token, err := LoadPassword(ctx, ref)
	if err == nil {
		if data, probeErr := r.AgentGET(ctx, connection, serverID, "/v1/hello"); probeErr == nil {
			var greeting struct {
				BinarySHA256 string `json:"binarySHA256"`
			}
			if json.Unmarshal(data, &greeting) == nil && greeting.BinarySHA256 == hash {
				return nil
			}
		}
	}
	if err != nil {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return err
		}
		token = hex.EncodeToString(secret)
		if err := SavePassword(ctx, ref, token); err != nil {
			return err
		}
	}
	const root = "/opt/ideploy/agent"
	stage := root + "/versions/" + hash
	upload := "umask 077; mkdir -p " + shellQuote(stage) + "; cat > " + shellQuote(stage+"/ideploy-agent.tmp") + "; test \"$(sha256sum " + shellQuote(stage+"/ideploy-agent.tmp") + " | cut -d ' ' -f 1)\" = " + shellQuote(hash) + " && mv " + shellQuote(stage+"/ideploy-agent.tmp") + " " + shellQuote(stage+"/ideploy-agent") + " && chmod 700 " + shellQuote(stage+"/ideploy-agent")
	if _, err := r.runTimeoutWithInput(ctx, connection, upload, 2*time.Minute, string(binary)); err != nil {
		return fmt.Errorf("上传并校验 Agent: %w", err)
	}
	if _, err := r.runTimeoutWithInput(ctx, connection, "umask 077; mkdir -p /etc/ideploy; cat > /etc/ideploy/agent.token.tmp && chmod 600 /etc/ideploy/agent.token.tmp && mv /etc/ideploy/agent.token.tmp /etc/ideploy/agent.token", 15*time.Second, token+"\n"); err != nil {
		return fmt.Errorf("写入 Agent 配对凭据: %w", err)
	}
	unit := "[Unit]\nDescription=iDeploy server agent\nAfter=network.target\n[Service]\nType=simple\nExecStart=/opt/ideploy/agent/current/ideploy-agent --token-file /etc/ideploy/agent.token\nRestart=on-failure\nRestartSec=3\nNoNewPrivileges=true\n[Install]\nWantedBy=multi-user.target\n"
	if _, err := r.runTimeoutWithInput(ctx, connection, "umask 077; cat > /etc/systemd/system/ideploy-agent.service.tmp && mv /etc/systemd/system/ideploy-agent.service.tmp /etc/systemd/system/ideploy-agent.service", 15*time.Second, unit); err != nil {
		return fmt.Errorf("安装 Agent 服务定义: %w", err)
	}
	activate := "set -e; old=$(readlink " + shellQuote(root+"/current") + " || true); ln -sfn " + shellQuote(stage) + " " + shellQuote(root+"/current.next") + "; mv -Tf " + shellQuote(root+"/current.next") + " " + shellQuote(root+"/current") + "; systemctl daemon-reload; systemctl enable ideploy-agent.service >/dev/null; ready=0; if systemctl restart ideploy-agent.service; then for attempt in 1 2 3 4 5 6 7 8 9 10; do if systemctl is-active --quiet ideploy-agent.service && test \"$(curl --noproxy '*' -s -o /dev/null -w '%{http_code}' --max-time 2 http://127.0.0.1:41827/v1/hello)\" = 401; then ready=1; break; fi; sleep 1; done; fi; if test \"$ready\" != 1; then if test -n \"$old\"; then ln -sfn \"$old\" " + shellQuote(root+"/current.next") + "; mv -Tf " + shellQuote(root+"/current.next") + " " + shellQuote(root+"/current") + "; systemctl restart ideploy-agent.service; fi; exit 1; fi"
	if _, err := r.runTimeout(ctx, connection, activate, 30*time.Second); err != nil {
		diagnostic, _ := r.runTimeout(ctx, connection, "systemctl status ideploy-agent.service --no-pager --lines=8 2>&1; journalctl -u ideploy-agent.service -n 8 --no-pager 2>&1", 12*time.Second)
		if len(diagnostic) > 3000 {
			diagnostic = diagnostic[len(diagnostic)-3000:]
		}
		return fmt.Errorf("启动 Agent 失败，已尝试回退: %w; systemd: %s", err, strings.TrimSpace(diagnostic))
	}
	return nil
}

// UninstallAgent removes only iDeploy Agent-owned service, credentials, journal,
// backups and renewal integration. It leaves Docker, Nginx and certificates intact.
func (r *Remote) UninstallAgent(ctx context.Context, connection Connection) error {
	const command = `set -e
if systemctl is-active --quiet ideploy-agent.service && test -d /var/lib/ideploy-agent/operations && grep -l '"status":"running"' /var/lib/ideploy-agent/operations/*.json >/dev/null 2>&1; then
  echo 'Agent still has a running operation' >&2
  exit 1
fi
systemctl disable --now ideploy-agent.service >/dev/null 2>&1 || true
rm -f /etc/systemd/system/ideploy-agent.service
systemctl disable --now ideploy-certbot-renew.timer >/dev/null 2>&1 || true
rm -f /etc/systemd/system/ideploy-certbot-renew.timer /etc/systemd/system/ideploy-certbot-renew.service
rm -f /etc/letsencrypt/renewal-hooks/deploy/ideploy-nginx-*
rm -rf /opt/ideploy/agent /var/lib/ideploy-agent
rm -f /etc/ideploy/agent.token
rmdir /etc/ideploy >/dev/null 2>&1 || true
systemctl daemon-reload`
	if _, err := r.runTimeout(ctx, connection, command, 45*time.Second); err != nil {
		return fmt.Errorf("卸载服务端 Agent: %w", err)
	}
	return nil
}
