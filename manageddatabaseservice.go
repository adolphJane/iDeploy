package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"ideploy/internal/databasebrowser"
	"ideploy/internal/job"
	"ideploy/internal/server"
)

type ManagedDatabaseInput struct {
	databasebrowser.InstanceConfig
	Password    string `json:"password"`
	AppPassword string `json:"appPassword"`
}

type managedDatabaseOperation struct {
	InstanceID string `json:"instanceId"`
	Action     string `json:"action"`
	BackupID   string `json:"backupId,omitempty"`
}

func managedDatabaseCredential(id string) string {
	return databasebrowser.PasswordCredentialRef("instance-" + id)
}

func managedDatabaseAppCredential(id string) string {
	return databasebrowser.PasswordCredentialRef("instance-" + id + "-app")
}

func (s *SystemService) ListManagedDatabases() ([]databasebrowser.Instance, error) {
	store, err := s.databaseStore()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	return store.ListInstances(ctx)
}

func (s *SystemService) CreateManagedDatabase(input ManagedDatabaseInput) (databasebrowser.Instance, error) {
	for _, password := range []string{input.Password, input.AppPassword} {
		if len(password) < 16 || len(password) > 256 || strings.ContainsAny(password, "\r\n\x00") {
			return databasebrowser.Instance{}, errors.New("实例密码须为 16–256 字节且不包含换行")
		}
		if input.Driver == databasebrowser.DriverMySQL && strings.ContainsAny(password, "'\\") {
			return databasebrowser.Instance{}, errors.New("MySQL 初始化密码不能包含单引号或反斜杠")
		}
	}
	if input.AppPassword == input.Password {
		return databasebrowser.Instance{}, errors.New("请为管理员账号和应用账号设置不同密码")
	}
	store, err := s.databaseStore()
	if err != nil {
		return databasebrowser.Instance{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	instance, err := store.CreateInstance(ctx, input.InstanceConfig)
	if err != nil {
		return databasebrowser.Instance{}, err
	}
	err = databasebrowser.SavePassword(ctx, managedDatabaseCredential(instance.ID), input.Password)
	if err == nil {
		err = databasebrowser.SavePassword(ctx, managedDatabaseAppCredential(instance.ID), input.AppPassword)
	}
	if err != nil {
		cleanup, stop := context.WithTimeout(context.Background(), 4*time.Second)
		defer stop()
		return databasebrowser.Instance{}, errors.Join(err, store.DeleteInstanceConfiguration(cleanup, instance.ID),
			databasebrowser.DeletePassword(cleanup, managedDatabaseCredential(instance.ID)))
	}
	return instance, nil
}

func (s *SystemService) DeleteManagedDatabaseConfiguration(id string) error {
	store, err := s.databaseStore()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := store.DeleteInstanceConfiguration(ctx, id); err != nil {
		return err
	}
	return errors.Join(databasebrowser.DeletePassword(ctx, managedDatabaseCredential(id)),
		databasebrowser.DeletePassword(ctx, managedDatabaseAppCredential(id)))
}

func (s *SystemService) ListManagedDatabaseBackups(id string) ([]databasebrowser.Backup, error) {
	store, err := s.databaseStore()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	return store.ListBackups(ctx, id)
}

func (s *SystemService) RunManagedDatabaseOperation(id, action, backupID string) (JobView, error) {
	if err := s.ensureSharedPublicationReady(); err != nil {
		return JobView{}, err
	}
	store, err := s.databaseStore()
	if err != nil {
		return JobView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	target, err := store.GetInstance(ctx, id)
	if err != nil {
		return JobView{}, err
	}
	operation := managedDatabaseOperation{InstanceID: id, Action: action, BackupID: backupID}
	if _, err := validateManagedDatabaseOperation(ctx, store, target, operation); err != nil {
		return JobView{}, err
	}
	return s.enqueueServerJob(target.ServerID, "database.instance", operation)
}

func validateManagedDatabaseOperation(ctx context.Context, store *databasebrowser.Store, target databasebrowser.Instance, operation managedDatabaseOperation) (databasebrowser.Backup, error) {
	switch operation.Action {
	case "deploy":
		if target.Status == "draft" || target.Status == "failed" || target.Status == "ready" {
			return databasebrowser.Backup{}, nil
		}
	case "backup":
		if target.Status == "ready" {
			return databasebrowser.Backup{}, nil
		}
	case "restore":
		if target.Status != "draft" {
			return databasebrowser.Backup{}, errors.New("备份只能恢复到尚未部署的新实例")
		}
		backup, err := store.GetBackup(ctx, operation.BackupID)
		if err != nil {
			return databasebrowser.Backup{}, errors.New("找不到指定备份")
		}
		source, err := store.GetInstance(ctx, backup.InstanceID)
		if err != nil {
			return databasebrowser.Backup{}, err
		}
		if source.ServerID != target.ServerID || source.Environment != target.Environment ||
			source.Driver != target.Driver || source.EngineVersion != target.EngineVersion ||
			source.DatabaseName != target.DatabaseName || source.AppUsername != target.AppUsername || source.ID == target.ID {
			return databasebrowser.Backup{}, errors.New("恢复目标必须是同一服务器、环境、引擎版本和库名下的新实例")
		}
		return backup, nil
	}
	return databasebrowser.Backup{}, errors.New("当前实例状态无法执行此操作")
}

func (s *SystemService) managedDatabaseHandler(ctx context.Context, task job.Job, reporter job.Reporter) (handlerErr error) {
	var operation managedDatabaseOperation
	if err := json.Unmarshal([]byte(task.Payload), &operation); err != nil {
		return err
	}
	store, err := s.databaseStore()
	if err != nil {
		return err
	}
	target, err := store.GetInstance(ctx, operation.InstanceID)
	if err != nil {
		return err
	}
	if task.LockKey != "server:"+target.ServerID {
		return errors.New("数据库任务的服务器与实例不一致")
	}
	backup, err := validateManagedDatabaseOperation(ctx, store, target, operation)
	if err != nil {
		return err
	}
	ctx, release, err := s.leaseSharedPublication(ctx, target.ServerID, task.ID)
	if err != nil {
		return err
	}
	defer release()
	servers, err := s.serverStore()
	if err != nil {
		return err
	}
	host, err := servers.Get(ctx, target.ServerID)
	if err != nil {
		return err
	}
	remote := server.NewRemote()
	if operation.Action == "backup" {
		reporter.Progress(20, "正在生成数据库逻辑备份")
		if err := remote.BackupManagedDatabase(ctx, host, target, task.ID); err != nil {
			return err
		}
		return store.RecordBackup(ctx, task.ID, target.ID)
	}

	password, err := databasebrowser.LoadPassword(ctx, managedDatabaseCredential(target.ID))
	if err != nil {
		return errors.New("本机钥匙串中的实例管理员密码不可用")
	}
	appPassword, err := databasebrowser.LoadPassword(ctx, managedDatabaseAppCredential(target.ID))
	if err != nil {
		return errors.New("本机钥匙串中的实例应用密码不可用")
	}
	runningStatus, failureStatus := "deploying", "failed"
	if operation.Action == "restore" {
		runningStatus, failureStatus = "restoring", "restore_failed"
	}
	if err := store.SetInstanceStatus(ctx, target.ID, runningStatus); err != nil {
		return err
	}
	defer func() {
		if handlerErr != nil {
			if operation.Action == "restore" {
				remote.StopManagedDatabaseAfterFailedRestore(host, target)
			}
			cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			handlerErr = errors.Join(handlerErr, store.SetInstanceStatus(cleanup, target.ID, failureStatus))
		}
	}()

	reporter.Progress(20, "正在启动持久化数据库实例并验证认证")
	if err := remote.DeployManagedDatabase(ctx, host, target, password, appPassword); err != nil {
		return err
	}
	if operation.Action == "restore" {
		reporter.Progress(60, "正在将备份导入新实例")
		if err := remote.RestoreManagedDatabase(ctx, host, backup.InstanceID, backup.ID, target); err != nil {
			return err
		}
	}
	return store.SetInstanceStatus(ctx, target.ID, "ready")
}
