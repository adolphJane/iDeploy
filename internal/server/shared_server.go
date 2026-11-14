package server

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const sharedServerContainer = "ideploy-shared-server"

type SharedServerState struct {
	Installed       bool   `json:"installed"`
	Running         bool   `json:"running"`
	Healthy         bool   `json:"healthy"`
	CanInstall      bool   `json:"canInstall"`
	Architecture    string `json:"architecture"`
	ImageTag        string `json:"imageTag"`
	LatestImageTag  string `json:"latestImageTag"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Detail          string `json:"detail"`
}

type SharedServerDatabase struct {
	Host          string
	Port          int
	Name          string
	User          string
	Password      string
	SkipTLSVerify bool
}

func bundledSharedServerPath(arch string) (string, error) {
	if arch != "amd64" && arch != "arm64" {
		return "", errors.New("共享服务仅支持 Linux amd64 和 arm64")
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(executable), "..", "Resources", "shared-server", "ideploy-server-linux-"+arch)
	if _, err = os.Stat(path); err != nil {
		return "", fmt.Errorf("应用包缺少 Linux %s 共享服务程序: %w", arch, err)
	}
	return path, nil
}

func (r *Remote) PreflightSharedServer(ctx context.Context, connection Connection) (SharedServerState, error) {
	output, err := r.runTimeout(ctx, connection, "set -e; uname -s; uname -m; docker info --format '{{.OSType}}/{{.Architecture}}'", 25*time.Second)
	if err != nil {
		return SharedServerState{}, fmt.Errorf("检查服务器 Docker: %w", err)
	}
	lines := strings.Fields(output)
	if len(lines) < 3 || lines[0] != "Linux" {
		return SharedServerState{}, errors.New("共享服务需要 Linux Docker 服务器")
	}
	arch := lines[1]
	if arch == "x86_64" {
		arch = "amd64"
	}
	if arch == "aarch64" {
		arch = "arm64"
	}
	dockerArch := strings.TrimPrefix(lines[2], "linux/")
	if dockerArch == "x86_64" {
		dockerArch = "amd64"
	}
	if dockerArch == "aarch64" {
		dockerArch = "arm64"
	}
	if !strings.HasPrefix(lines[2], "linux/") || dockerArch != arch {
		return SharedServerState{}, errors.New("Docker 引擎架构与服务器架构不一致")
	}
	path, err := bundledSharedServerPath(arch)
	if err != nil {
		return SharedServerState{}, err
	}
	state, err := r.InspectSharedServer(ctx, connection)
	if err != nil {
		return SharedServerState{}, err
	}
	state.Architecture = arch
	if state.Installed {
		binary, readErr := os.ReadFile(path)
		if readErr != nil {
			return SharedServerState{}, readErr
		}
		currentTag, _, buildErr := sharedServerBuildContext(binary)
		if buildErr != nil {
			return SharedServerState{}, buildErr
		}
		state.LatestImageTag = currentTag
		state.UpdateAvailable = state.ImageTag != currentTag
		if !state.Healthy {
			state.CanInstall = true
			state.Detail = "已有共享服务容器未通过健康检查；重新部署时会保留旧容器，验证失败后自动恢复"
			return state, nil
		}
		if state.UpdateAvailable {
			state.CanInstall = true
			state.Detail = "共享服务已有新版本；更新前会保留旧容器，验证失败后自动恢复"
			return state, nil
		}
		state.Detail = "服务器已有运行正常的 iDeploy 共享服务容器"
		return state, nil
	}
	ports, err := r.runTimeout(ctx, connection, "docker ps --format '{{.Ports}}'", 15*time.Second)
	if err != nil {
		return SharedServerState{}, err
	}
	if strings.Contains(ports, "8765->") {
		state.Detail = "服务器端口 8765 已被容器占用"
		return state, nil
	}
	state.Detail = "Linux Docker 可用；应用包包含匹配架构的服务程序，容器名与端口可用"
	state.CanInstall = true
	return state, nil
}

func (r *Remote) InspectSharedServer(ctx context.Context, connection Connection) (SharedServerState, error) {
	output, err := r.runTimeout(ctx, connection, "if docker container inspect ideploy-shared-server >/dev/null 2>&1; then docker container inspect ideploy-shared-server --format '{{index .Config.Labels \"com.ideploy.role\"}}|{{.State.Running}}|{{.Config.Image}}'; else echo ABSENT; fi", 15*time.Second)
	if err != nil {
		return SharedServerState{}, err
	}
	value := strings.TrimSpace(output)
	if value == "ABSENT" {
		return SharedServerState{Detail: "尚未部署"}, nil
	}
	parts := strings.Split(value, "|")
	if len(parts) != 3 || parts[0] != "shared-server" {
		return SharedServerState{}, errors.New("同名容器不属于 iDeploy，停止接管")
	}
	state := SharedServerState{Installed: true, Running: parts[1] == "true", ImageTag: parts[2], Detail: "容器已安装"}
	if !state.Running {
		state.Detail = "共享服务容器未运行"
		return state, nil
	}
	health, err := r.runTimeout(ctx, connection, "curl --noproxy '*' --connect-timeout 2 --max-time 5 -fsS http://127.0.0.1:8765/healthz", 8*time.Second)
	if err == nil && strings.Contains(health, "\"status\":\"ready\"") {
		state.Healthy = true
		state.Detail = "共享服务与远程数据库连接正常"
	} else {
		state.Detail = "容器正在运行，但健康检查未通过"
	}
	return state, nil
}

func sharedServerBuildContext(binary []byte) (string, string, error) {
	sum := sha256.Sum256(binary)
	tag := "ideploy/shared-server:" + hex.EncodeToString(sum[:8])
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	dockerfile := []byte("FROM alpine:3.21\nRUN apk add --no-cache ca-certificates\nCOPY ideploy-server /usr/local/bin/ideploy-server\nUSER 65532:65532\nEXPOSE 8765\nENTRYPOINT [\"/usr/local/bin/ideploy-server\"]\n")
	for _, item := range []struct {
		name string
		data []byte
		mode int64
	}{{"Dockerfile", dockerfile, 0644}, {"ideploy-server", binary, 0755}} {
		if err := writer.WriteHeader(&tar.Header{Name: item.name, Mode: item.mode, Size: int64(len(item.data)), Typeflag: tar.TypeReg}); err != nil {
			return "", "", err
		}
		if _, err := writer.Write(item.data); err != nil {
			return "", "", err
		}
	}
	if err := writer.Close(); err != nil {
		return "", "", err
	}
	return tag, buffer.String(), nil
}

func (r *Remote) DeploySharedServer(ctx context.Context, connection Connection, architecture string, database SharedServerDatabase, token string) error {
	if database.Host == "" || database.Port < 1 || database.Port > 65535 || database.Name == "" || database.User == "" || database.Password == "" || len(token) < 32 || strings.ContainsAny(database.Host+database.Name+database.User, "\r\n\x00") {
		return errors.New("共享服务数据库配置无效")
	}
	path, err := bundledSharedServerPath(architecture)
	if err != nil {
		return err
	}
	binary, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	tag, archive, err := sharedServerBuildContext(binary)
	if err != nil {
		return err
	}
	state, err := r.PreflightSharedServer(ctx, connection)
	if err != nil {
		return err
	}
	if !state.CanInstall {
		return errors.New(state.Detail)
	}
	if state.Architecture != architecture {
		return errors.New("服务器架构已变化，请重新预检")
	}
	home, err := r.runTimeout(ctx, connection, "printf '%s' \"$HOME\"", 10*time.Second)
	if err != nil {
		return err
	}
	base := filepath.Join(strings.TrimSpace(home), ".ideploy", "shared-server")
	if !filepath.IsAbs(base) {
		return errors.New("服务器用户目录无效")
	}
	if _, err = r.runTimeout(ctx, connection, "install -d -m 700 "+shellQuote(base), 12*time.Second); err != nil {
		return err
	}
	secretSuffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	passwordPath := filepath.Join(base, "mysql_password_"+secretSuffix)
	tokenPath := filepath.Join(base, "server_token_"+secretSuffix)
	if err = r.uploadSharedSecret(ctx, connection, passwordPath, database.Password); err != nil {
		return err
	}
	if err = r.uploadSharedSecret(ctx, connection, tokenPath, token); err != nil {
		return err
	}
	if _, err = r.runTimeoutWithInput(ctx, connection, "docker build -q -t "+shellQuote(tag)+" -", 5*time.Minute, archive); err != nil {
		return fmt.Errorf("构建共享服务镜像: %w", err)
	}
	uid, err := r.runTimeout(ctx, connection, "id -u; id -g", 8*time.Second)
	if err != nil {
		return err
	}
	ids := strings.Fields(uid)
	if len(ids) != 2 {
		return errors.New("无法读取服务器用户 ID")
	}
	if _, err = strconv.Atoi(ids[0]); err != nil {
		return err
	}
	if _, err = strconv.Atoi(ids[1]); err != nil {
		return err
	}
	backupName := sharedServerContainer + "-backup-" + secretSuffix
	if state.Installed {
		if _, err = r.runTimeout(ctx, connection, "docker stop "+shellQuote(sharedServerContainer)+" && docker rename "+shellQuote(sharedServerContainer)+" "+shellQuote(backupName), 35*time.Second); err != nil {
			_, _ = r.runTimeout(ctx, connection, "docker start "+shellQuote(sharedServerContainer), 15*time.Second)
			return fmt.Errorf("备份原共享服务容器失败: %w", err)
		}
	}
	restore := func() {
		if !state.Installed {
			return
		}
		restoreCtx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		_, _ = r.runTimeout(restoreCtx, connection, "docker rm -f "+shellQuote(sharedServerContainer)+" >/dev/null 2>&1 || true; docker rename "+shellQuote(backupName)+" "+shellQuote(sharedServerContainer)+" && docker start "+shellQuote(sharedServerContainer), 35*time.Second)
	}
	tlsMode := "verify"
	if database.SkipTLSVerify {
		tlsMode = "skip-verify"
	}
	args := []string{"docker run -d --name " + sharedServerContainer, "--label com.ideploy.role=shared-server", "--restart unless-stopped", "--user " + ids[0] + ":" + ids[1], "-p 127.0.0.1:8765:8765", "-e IDEPLOY_DATABASE_DRIVER=mysql", "-e IDEPLOY_DB_HOST=" + shellQuote(database.Host), "-e IDEPLOY_DB_PORT=" + strconv.Itoa(database.Port), "-e IDEPLOY_DB_NAME=" + shellQuote(database.Name), "-e IDEPLOY_DB_USER=" + shellQuote(database.User), "-e IDEPLOY_DB_TLS_MODE=" + tlsMode, "-e IDEPLOY_DB_PASSWORD_FILE=/run/secrets/mysql_password", "-e IDEPLOY_SERVER_TOKEN_FILE=/run/secrets/server_token", "-e IDEPLOY_SERVER_LISTEN=0.0.0.0:8765", "-v " + shellQuote(passwordPath+":/run/secrets/mysql_password:ro"), "-v " + shellQuote(tokenPath+":/run/secrets/server_token:ro"), shellQuote(tag)}
	if _, err = r.runTimeout(ctx, connection, strings.Join(args, " "), 45*time.Second); err != nil {
		restore()
		return fmt.Errorf("启动共享服务容器: %w", err)
	}
	for attempt := 0; attempt < 12; attempt++ {
		current, checkErr := r.InspectSharedServer(ctx, connection)
		if checkErr == nil && current.Healthy {
			if state.Installed {
				_, _ = r.runTimeout(ctx, connection, "docker rm "+shellQuote(backupName), 15*time.Second)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			restore()
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	logs, _ := r.runTimeout(ctx, connection, "docker logs --tail 12 "+shellQuote(sharedServerContainer)+" 2>&1", 12*time.Second)
	logs = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(logs, database.Password, "[已隐藏]"), token, "[已隐藏]"))
	if len(logs) > 1200 {
		logs = logs[len(logs)-1200:]
	}
	restore()
	if strings.Contains(logs, "not standards compliant") {
		return errors.New("共享服务无法连接 MySQL：服务器使用的自动生成证书不符合 X.509 校验要求。请为 MySQL 配置标准服务器证书和受信任 CA 后重新预检。容器日志：" + logs)
	}
	if strings.Contains(logs, "cannot validate certificate") || strings.Contains(logs, "x509:") {
		return errors.New("共享服务无法连接 MySQL：TLS 证书校验失败。请检查证书链、有效期及连接地址是否在证书 SAN 中，然后重新预检。容器日志：" + logs)
	}
	return errors.New("共享服务健康检查未通过；请检查数据库权限及容器日志：" + logs)
}

func (r *Remote) uploadSharedSecret(ctx context.Context, connection Connection, path, value string) error {
	if strings.ContainsAny(value, "\x00") {
		return errors.New("凭据包含无效字符")
	}
	command := "umask 077; cat > " + shellQuote(path+".tmp") + " && mv " + shellQuote(path+".tmp") + " " + shellQuote(path) + " && chmod 600 " + shellQuote(path)
	_, err := r.runTimeoutWithInput(ctx, connection, command, 15*time.Second, value)
	if err != nil {
		return errors.New("上传共享服务凭据失败")
	}
	return nil
}
