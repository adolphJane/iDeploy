package domain

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
)

// SharedAppsFQDN is retained for existing drafts and migration tests.
const SharedAppsFQDN = "apps.longxutec.top"

var sharedPathPattern = regexp.MustCompile(`^/[a-z0-9][a-z0-9_-]{0,62}/$`)
var sharedHostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

type PathRoute struct {
	ID                 string `json:"id"`
	FQDN               string `json:"fqdn"`
	PathPrefix         string `json:"pathPrefix"`
	DeploymentID       string `json:"deploymentId"`
	ServiceName        string `json:"serviceName"`
	ContainerPort      int    `json:"containerPort"`
	HostIP             string `json:"hostIp"`
	HostPort           int    `json:"hostPort"`
	CertificatePath    string `json:"certificatePath"`
	CertificateKeyPath string `json:"certificateKeyPath"`
	Status             string `json:"status"`
	Detail             string `json:"detail"`
	JobID              string `json:"jobId"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt"`
}

func NormalizeSharedPath(value string) (string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	if !strings.HasSuffix(value, "/") {
		value += "/"
	}
	if !sharedPathPattern.MatchString(value) {
		return "", errors.New("访问路径只能使用一段小写字母、数字、下划线或连字符")
	}
	return value, nil
}

func (s *Store) ListPathRoutes(ctx context.Context) ([]PathRoute, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,fqdn,path_prefix,deployment_id,service_name,container_port,host_ip,host_port,certificate_path,certificate_key_path,status,detail,job_id,created_at,updated_at FROM shared_path_routes ORDER BY path_prefix`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PathRoute{}
	for rows.Next() {
		var v PathRoute
		if err := rows.Scan(&v.ID, &v.FQDN, &v.PathPrefix, &v.DeploymentID, &v.ServiceName, &v.ContainerPort, &v.HostIP, &v.HostPort, &v.CertificatePath, &v.CertificateKeyPath, &v.Status, &v.Detail, &v.JobID, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) GetPathRoute(ctx context.Context, id string) (PathRoute, error) {
	var v PathRoute
	err := s.db.QueryRowContext(ctx, `SELECT id,fqdn,path_prefix,deployment_id,service_name,container_port,host_ip,host_port,certificate_path,certificate_key_path,status,detail,job_id,created_at,updated_at FROM shared_path_routes WHERE id=?`, id).
		Scan(&v.ID, &v.FQDN, &v.PathPrefix, &v.DeploymentID, &v.ServiceName, &v.ContainerPort, &v.HostIP, &v.HostPort, &v.CertificatePath, &v.CertificateKeyPath, &v.Status, &v.Detail, &v.JobID, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PathRoute{}, ErrNotFound
	}
	return v, err
}

func (s *Store) SavePathRouteDraft(ctx context.Context, v PathRoute) (PathRoute, error) {
	path, err := NormalizeSharedPath(v.PathPrefix)
	if err != nil {
		return PathRoute{}, err
	}
	v.FQDN = strings.ToLower(strings.TrimSpace(v.FQDN))
	if len(v.FQDN) > 253 || !sharedHostPattern.MatchString(v.FQDN) || v.DeploymentID == "" || v.ContainerPort < 1 || v.ContainerPort > 65535 || (v.CertificatePath == "") != (v.CertificateKeyPath == "") {
		return PathRoute{}, errors.New("共享域名、部署、端口或证书配置无效")
	}
	var reservedFor string
	err = s.db.QueryRowContext(ctx, `SELECT deployment_id FROM deployment_publish_paths WHERE fqdn=? AND path_prefix=?`, v.FQDN, path).Scan(&reservedFor)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return PathRoute{}, err
	}
	if reservedFor != "" && reservedFor != v.DeploymentID {
		return PathRoute{}, errors.New("此路径已被其他部署的发布配置占用")
	}
	if v.ID != "" {
		old, err := s.GetPathRoute(ctx, v.ID)
		if err != nil {
			return PathRoute{}, err
		}
		if old.Status != "draft" {
			return PathRoute{}, errors.New("已应用的路径请先解除，再修改目标")
		}
		_, err = s.db.ExecContext(ctx, `UPDATE shared_path_routes SET fqdn=?,path_prefix=?,deployment_id=?,service_name=?,container_port=?,certificate_path=?,certificate_key_path=?,detail='',updated_at=? WHERE id=? AND status='draft'`, v.FQDN, path, v.DeploymentID, v.ServiceName, v.ContainerPort, v.CertificatePath, v.CertificateKeyPath, now(), v.ID)
		if err != nil {
			return PathRoute{}, err
		}
		return s.GetPathRoute(ctx, v.ID)
	}
	id, err := newID()
	if err != nil {
		return PathRoute{}, err
	}
	stamp := now()
	_, err = s.db.ExecContext(ctx, `INSERT INTO shared_path_routes(id,fqdn,path_prefix,deployment_id,service_name,container_port,certificate_path,certificate_key_path,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, v.FQDN, path, v.DeploymentID, v.ServiceName, v.ContainerPort, v.CertificatePath, v.CertificateKeyPath, stamp, stamp)
	if err != nil {
		return PathRoute{}, err
	}
	return s.GetPathRoute(ctx, id)
}

func (s *Store) DeletePathRouteDraft(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM shared_path_routes WHERE id=? AND status='draft'`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("只有草稿路径可以删除")
	}
	return nil
}

func (s *Store) SetPathRouteJob(ctx context.Context, id, jobID, status string) error {
	from := "draft"
	if status == "removing" {
		from = "active"
	} else if status != "applying" {
		return errors.New("路径任务状态无效")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE shared_path_routes SET status=?,job_id=?,detail='',updated_at=? WHERE id=? AND status=?`, status, jobID, now(), id, from)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("路径状态已变化")
	}
	return nil
}

func (s *Store) SetPathRouteFailure(ctx context.Context, id, jobID, status, detail string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE shared_path_routes SET status=?,detail=?,updated_at=? WHERE id=? AND job_id=? AND status IN ('applying','removing')`, status, detail, now(), id, jobID)
	return err
}

func (s *Store) ReconcileInterruptedPathRoutes(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE shared_path_routes SET status=CASE WHEN status='applying' THEN 'draft' ELSE 'active' END,detail='任务中断；请重新预检并核对远端 Nginx 配置',updated_at=? WHERE status IN ('applying','removing') AND job_id IN (SELECT id FROM jobs WHERE status IN ('failed','canceled'))`, now())
	return err
}

func (s *Store) MarkPathRouteApplied(ctx context.Context, id, serverID, jobID, hostIP string, hostPort int, hash, imageRef string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO server_ingresses(server_id,config_hash,image_ref,updated_at) VALUES(?,?,?,?) ON CONFLICT(server_id) DO UPDATE SET config_hash=excluded.config_hash,image_ref=excluded.image_ref,updated_at=excluded.updated_at`, serverID, hash, imageRef, now()); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE shared_path_routes SET status='active',host_ip=?,host_port=?,detail='入口配置已应用',updated_at=? WHERE id=? AND job_id=? AND status='applying'`, hostIP, hostPort, now(), id, jobID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("路径任务状态已变化")
	}
	return tx.Commit()
}

func (s *Store) MarkPathRouteRemoved(ctx context.Context, id, serverID, jobID, hash, imageRef string, last bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if last {
		_, err = tx.ExecContext(ctx, `DELETE FROM server_ingresses WHERE server_id=?`, serverID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE server_ingresses SET config_hash=?,image_ref=?,updated_at=? WHERE server_id=?`, hash, imageRef, now(), serverID)
	}
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM shared_path_routes WHERE id=? AND job_id=? AND status='removing'`, id, jobID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("路径任务状态已变化")
	}
	return tx.Commit()
}
