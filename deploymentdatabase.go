package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	"ideploy/internal/databasebrowser"
	"ideploy/internal/deployment"
	"ideploy/internal/server"
)

func (s *SystemService) deploymentDatabase(ctx context.Context, target deployment.Target) (databasebrowser.Connection, error) {
	store, err := s.databaseStore()
	if err != nil {
		return databasebrowser.Connection{}, err
	}
	database, err := store.Get(ctx, target.DatabaseConnectionID)
	if err != nil {
		return databasebrowser.Connection{}, err
	}
	if target.Environment != database.Environment {
		return databasebrowser.Connection{}, errors.New("数据库必须与部署目标属于同一环境")
	}
	return database, nil
}

func (s *SystemService) checkDeploymentDatabase(ctx context.Context, target deployment.Target, host server.Connection) error {
	if target.DatabaseConnectionID == "" {
		return nil
	}
	database, err := s.deploymentDatabase(ctx, target)
	if err != nil {
		return err
	}
	if err := server.NewRemote().ProbeTCP(ctx, host, database.Host, database.Port); err != nil {
		return fmt.Errorf("应用服务器无法访问数据库 %s: %w", net.JoinHostPort(database.Host, strconv.Itoa(database.Port)), err)
	}
	return nil
}

func (s *SystemService) addDeploymentDatabaseVariables(ctx context.Context, target deployment.Target, values map[string]string) error {
	if target.DatabaseConnectionID == "" || target.WorkloadKind != "container" {
		return nil
	}
	database, err := s.deploymentDatabase(ctx, target)
	if err != nil {
		return err
	}
	for name, value := range map[string]string{
		"IDEPLOY_DATABASE_HOST": database.Host,
		"IDEPLOY_DATABASE_PORT": strconv.Itoa(database.Port),
		"IDEPLOY_DATABASE_NAME": database.DatabaseName,
	} {
		if existing, present := values[name]; present && existing != value {
			return fmt.Errorf("环境变量 %s 与已绑定数据库冲突，请移除该变量或更新绑定", name)
		}
		values[name] = value
	}
	return nil
}
