package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type PublishPath struct {
	DeploymentID  string `json:"deploymentId"`
	FQDN          string `json:"fqdn"`
	PathPrefix    string `json:"pathPrefix"`
	ServiceName   string `json:"serviceName"`
	ContainerPort int    `json:"containerPort"`
}

func (s *Store) ReservePublishPath(ctx context.Context, v PublishPath) error {
	v.FQDN = strings.ToLower(strings.TrimSpace(v.FQDN))
	path, err := NormalizeSharedPath(v.PathPrefix)
	if err != nil {
		return err
	}
	v.PathPrefix = path
	v.ServiceName = strings.TrimSpace(v.ServiceName)
	if len(v.FQDN) > 253 || !sharedHostPattern.MatchString(v.FQDN) || v.DeploymentID == "" || v.ContainerPort < 1 || v.ContainerPort > 65535 {
		return errors.New("发布域名、路径或容器端口无效")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner, routeStatus string
	err = tx.QueryRowContext(ctx, `SELECT deployment_id,status FROM shared_path_routes WHERE fqdn=? AND path_prefix=?`, v.FQDN, path).Scan(&owner, &routeStatus)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && owner != v.DeploymentID {
		return errors.New("此域名路径已由其他部署使用")
	}
	if routeStatus == "applying" || routeStatus == "removing" {
		return errors.New("此路径正在变更，请等待当前任务完成")
	}
	var otherCount int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM shared_path_routes WHERE deployment_id=? AND status IN ('active','removing') AND (fqdn<>? OR path_prefix<>?)`, v.DeploymentID, v.FQDN, path).Scan(&otherCount)
	if err != nil {
		return err
	}
	if otherCount > 0 {
		return errors.New("此部署已有生效路径；请先解除旧路径，再选择新域名或路径")
	}
	var previousHost, previousPath string
	err = tx.QueryRowContext(ctx, `SELECT fqdn,path_prefix FROM deployment_publish_paths WHERE deployment_id=?`, v.DeploymentID).Scan(&previousHost, &previousPath)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && (previousHost != v.FQDN || previousPath != path) {
		var pending int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM deployment_releases WHERE deployment_id=? AND status NOT IN ('succeeded','failed','canceled')`, v.DeploymentID).Scan(&pending); err != nil {
			return err
		}
		if pending > 0 {
			return errors.New("此部署仍有发布任务运行，请等待完成后再更换域名或路径")
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO deployment_publish_paths(deployment_id,fqdn,path_prefix,service_name,container_port,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(deployment_id) DO UPDATE SET fqdn=excluded.fqdn,path_prefix=excluded.path_prefix,service_name=excluded.service_name,container_port=excluded.container_port,updated_at=excluded.updated_at`, v.DeploymentID, v.FQDN, path, v.ServiceName, v.ContainerPort, now())
	if err != nil {
		return fmt.Errorf("发布路径已被其他部署选择: %w", err)
	}
	return tx.Commit()
}

func (s *Store) SaveReleasePathIntent(ctx context.Context, releaseID string, v PublishPath) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO release_path_intents(release_id,fqdn,path_prefix,service_name,container_port) VALUES(?,?,?,?,?)`, releaseID, v.FQDN, v.PathPrefix, v.ServiceName, v.ContainerPort)
	return err
}

func (s *Store) GetReleasePathIntent(ctx context.Context, releaseID string) (PublishPath, error) {
	var v PublishPath
	err := s.db.QueryRowContext(ctx, `SELECT r.deployment_id,p.fqdn,p.path_prefix,p.service_name,p.container_port FROM release_path_intents p JOIN deployment_releases r ON r.id=p.release_id WHERE p.release_id=?`, releaseID).Scan(&v.DeploymentID, &v.FQDN, &v.PathPrefix, &v.ServiceName, &v.ContainerPort)
	if errors.Is(err, sql.ErrNoRows) {
		return PublishPath{}, ErrNotFound
	}
	return v, err
}

func (s *Store) MarkPublishedPathApplied(ctx context.Context, v PublishPath, endpointIP string, endpointPort int, certificate, key, hash, imageRef string) error {
	if endpointIP == "" || endpointPort < 1 || hash == "" {
		return errors.New("发布路径入口状态无效")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id, owner, status string
	err = tx.QueryRowContext(ctx, `SELECT id,deployment_id,status FROM shared_path_routes WHERE fqdn=? AND path_prefix=?`, v.FQDN, v.PathPrefix).Scan(&id, &owner, &status)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && owner != v.DeploymentID {
		return errors.New("发布路径已被其他部署使用")
	}
	if status == "applying" || status == "removing" {
		return errors.New("发布路径正在变更")
	}
	if errors.Is(err, sql.ErrNoRows) {
		id, err = newID()
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO shared_path_routes(id,fqdn,path_prefix,deployment_id,service_name,container_port,host_ip,host_port,certificate_path,certificate_key_path,status,detail,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,'active','发布时自动配置',?,?)`, id, v.FQDN, v.PathPrefix, v.DeploymentID, v.ServiceName, v.ContainerPort, endpointIP, endpointPort, certificate, key, now(), now())
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE shared_path_routes SET service_name=?,container_port=?,host_ip=?,host_port=?,certificate_path=?,certificate_key_path=?,status='active',detail='发布时自动配置',updated_at=? WHERE id=?`, v.ServiceName, v.ContainerPort, endpointIP, endpointPort, certificate, key, now(), id)
	}
	if err != nil {
		return err
	}
	var serverID string
	if err = tx.QueryRowContext(ctx, `SELECT server_id FROM deployment_targets WHERE id=?`, v.DeploymentID).Scan(&serverID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO server_ingresses(server_id,config_hash,image_ref,updated_at) VALUES(?,?,?,?) ON CONFLICT(server_id) DO UPDATE SET config_hash=excluded.config_hash,image_ref=excluded.image_ref,updated_at=excluded.updated_at`, serverID, hash, imageRef, now()); err != nil {
		return err
	}
	return tx.Commit()
}
