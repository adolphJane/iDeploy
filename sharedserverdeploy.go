package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"ideploy/internal/databasebrowser"
	"ideploy/internal/job"
	"ideploy/internal/platform/paths"
	"ideploy/internal/server"
	"path/filepath"
	"strings"
	"time"
)

type SharedServerDeploymentPlan struct {
	ServerID      string `json:"serverId"`
	DatabaseID    string `json:"databaseId"`
	DatabaseName  string `json:"databaseName"`
	Architecture  string `json:"architecture"`
	CanDeploy     bool   `json:"canDeploy"`
	Repair        bool   `json:"repair"`
	Upgrade       bool   `json:"upgrade"`
	SkipTLSVerify bool   `json:"skipTLSVerify"`
	Detail        string `json:"detail"`
}

func (s *SystemService) GetSharedServerStatus(serverID string) (server.SharedServerState, error) {
	servers, err := s.serverStore()
	if err != nil {
		return server.SharedServerState{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	connection, err := servers.Get(ctx, serverID)
	if err != nil {
		return server.SharedServerState{}, err
	}
	return server.NewRemote().PreflightSharedServer(ctx, connection)
}

func (s *SystemService) PlanSharedServerDeployment(serverID, databaseID, databaseName string, skipTLSVerify bool) (SharedServerDeploymentPlan, error) {
	plan := SharedServerDeploymentPlan{ServerID: serverID, DatabaseID: databaseID, SkipTLSVerify: skipTLSVerify}
	servers, err := s.serverStore()
	if err != nil {
		return plan, err
	}
	databases, err := s.databaseStore()
	if err != nil {
		return plan, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	connection, err := servers.Get(ctx, serverID)
	if err != nil {
		return plan, err
	}
	database, err := databases.Get(ctx, databaseID)
	if err != nil {
		return plan, err
	}
	if database.Driver != databasebrowser.DriverMySQL {
		plan.Detail = "共享服务当前只支持选择 MySQL 连接"
		return plan, nil
	}
	databaseName = strings.TrimSpace(databaseName)
	if databaseName == "" {
		databaseName = database.DatabaseName
	}
	if databaseName == "" {
		plan.Detail = "请选择或填写共享服务使用的 MySQL 数据库名"
		return plan, nil
	}
	if len(databaseName) > 128 || strings.ContainsAny(databaseName, "\r\n\x00") {
		plan.Detail = "MySQL 数据库名无效"
		return plan, nil
	}
	plan.DatabaseName = databaseName
	password, err := databasebrowser.LoadPassword(ctx, database.CredentialRef)
	if err != nil {
		plan.Detail = "此设备的 MySQL 密码不可用，请编辑并保存连接"
		return plan, nil
	}
	probeMode := "require"
	if skipTLSVerify {
		probeMode = "skip-verify"
	}
	probe := databasebrowser.NewConnection{Name: "共享服务预检", Driver: database.Driver, Host: database.Host, Port: database.Port, DatabaseName: databaseName, Username: database.Username, SSLMode: probeMode}
	if testErr := databasebrowser.Test(ctx, probe, password); testErr != nil && (strings.Contains(testErr.Error(), "x509:") || strings.Contains(testErr.Error(), "certificate")) {
		plan.Detail = "MySQL TLS 证书校验失败：" + strings.ReplaceAll(testErr.Error(), password, "[已隐藏]") + "。" + sharedServerTLSAdvice(testErr.Error())
		return plan, nil
	}
	state, err := server.NewRemote().PreflightSharedServer(ctx, connection)
	if err != nil {
		plan.Detail = err.Error()
		return plan, nil
	}
	plan.Architecture = state.Architecture
	plan.Repair = state.Installed && !state.Healthy
	plan.Upgrade = state.Installed && state.Healthy && state.CanInstall
	plan.Detail = state.Detail + "。共享服务仅在服务器本机绑定 127.0.0.1:8765；数据库密码与管理员令牌通过受限文件挂载。"
	if state.Installed {
		plan.Detail += " 升级前会先将共享 MySQL 表备份到本机 iDeploy/backups，并保留旧容器至新版本健康检查通过。"
	}
	if skipTLSVerify {
		plan.Detail += " MySQL 连接将使用 TLS 加密，但不会验证服务器证书或身份。"
	}
	plan.CanDeploy = state.CanInstall
	return plan, nil
}

func sharedServerTLSAdvice(message string) string {
	switch {
	case strings.Contains(message, "not standards compliant"):
		return "当前 MySQL 5.7 自动生成的证书不符合客户端校验要求；更换连接域名不能修复。请在 MySQL 服务端配置符合 X.509 标准、包含访问域名或 IP 的 SAN 的服务器证书及可信 CA，重启 MySQL 后重新预检。"
	case strings.Contains(message, "cannot validate certificate for") || strings.Contains(message, "doesn't contain any IP SANs"):
		return "证书未覆盖当前连接地址；请使用证书 SAN 中的域名连接，或配置包含当前地址的服务器证书。"
	default:
		return "请检查 MySQL 服务器证书的有效期、证书链、SAN 和客户端信任的 CA，修复后重新预检。"
	}
}

func (s *SystemService) DeploySharedServer(serverID, databaseID, databaseName string, skipTLSVerify bool) (JobView, error) {
	plan, err := s.PlanSharedServerDeployment(serverID, databaseID, databaseName, skipTLSVerify)
	if err != nil {
		return JobView{}, err
	}
	if !plan.CanDeploy {
		return JobView{}, errors.New(plan.Detail)
	}
	return s.enqueueServerJob(serverID, "shared.server.deploy", struct {
		ServerID      string `json:"serverId"`
		DatabaseID    string `json:"databaseId"`
		Architecture  string `json:"architecture"`
		DatabaseName  string `json:"databaseName"`
		SkipTLSVerify bool   `json:"skipTLSVerify"`
	}{serverID, databaseID, plan.Architecture, plan.DatabaseName, skipTLSVerify})
}

func (s *SystemService) sharedServerDeployHandler(ctx context.Context, task job.Job, reporter job.Reporter) error {
	var p struct {
		ServerID      string `json:"serverId"`
		DatabaseID    string `json:"databaseId"`
		Architecture  string `json:"architecture"`
		DatabaseName  string `json:"databaseName"`
		SkipTLSVerify bool   `json:"skipTLSVerify"`
	}
	if err := json.Unmarshal([]byte(task.Payload), &p); err != nil {
		return err
	}
	reporter.Progress(10, "读取服务器与 MySQL 连接")
	servers, err := s.serverStore()
	if err != nil {
		return err
	}
	databases, err := s.databaseStore()
	if err != nil {
		return err
	}
	connection, err := servers.Get(ctx, p.ServerID)
	if err != nil {
		return err
	}
	database, err := databases.Get(ctx, p.DatabaseID)
	if err != nil {
		return err
	}
	if database.Driver != databasebrowser.DriverMySQL || p.DatabaseName == "" || len(p.DatabaseName) > 128 || strings.ContainsAny(p.DatabaseName, "\r\n\x00") {
		return errors.New("请选择有效的 MySQL 连接")
	}
	password, err := databasebrowser.LoadPassword(ctx, database.CredentialRef)
	if err != nil {
		return errors.New("MySQL 密码不可用")
	}
	ref := server.SharedServerTokenCredentialRef(p.ServerID)
	token, err := server.LoadPassword(ctx, ref)
	if err != nil {
		data := make([]byte, 32)
		if _, err = rand.Read(data); err != nil {
			return err
		}
		token = hex.EncodeToString(data)
		if err = server.SavePassword(ctx, ref, token); err != nil {
			return err
		}
	}
	remote := server.NewRemote()
	state, err := remote.PreflightSharedServer(ctx, connection)
	if err != nil {
		return err
	}
	if state.Installed {
		reporter.Progress(25, "备份共享服务 MySQL 数据")
		backupConnection := database
		backupConnection.DatabaseName = p.DatabaseName
		backupConnection.SSLMode = "require"
		if p.SkipTLSVerify {
			backupConnection.SSLMode = "skip-verify"
		}
		appPaths, err := paths.Resolve()
		if err != nil {
			return err
		}
		backupPath, err := databasebrowser.BackupSharedTables(ctx, backupConnection, password, filepath.Join(appPaths.DataDir, "backups"))
		if err != nil {
			return fmt.Errorf("升级前备份 MySQL 数据失败: %w", err)
		}
		reporter.Log("已保存共享服务数据库备份：" + backupPath)
	}
	reporter.Progress(30, "上传服务程序并构建镜像")
	if err = remote.DeploySharedServer(ctx, connection, p.Architecture, server.SharedServerDatabase{Host: database.Host, Port: database.Port, Name: p.DatabaseName, User: database.Username, Password: password, SkipTLSVerify: p.SkipTLSVerify}, token); err != nil {
		return err
	}
	reporter.Progress(100, "共享服务已部署，远程 MySQL 健康检查通过")
	return nil
}
