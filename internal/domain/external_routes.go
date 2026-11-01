package domain

import (
	"context"
	"database/sql"
	"errors"
)

// ExternalRoute maps a DNS hostname to a service already running on a server.
// Certificate paths refer to files inside the existing Nginx container.
type ExternalRoute struct {
	HostID             string `json:"hostId"`
	ServerID           string `json:"serverId"`
	HostIP             string `json:"hostIp"`
	HostPort           int    `json:"hostPort"`
	UpstreamScheme     string `json:"upstreamScheme"`
	CertificatePath    string `json:"certificatePath"`
	CertificateKeyPath string `json:"certificateKeyPath"`
	Status             string `json:"status"`
	Detail             string `json:"detail"`
	JobID              string `json:"jobId"`
	UpdatedAt          string `json:"updatedAt"`
}

func (s *Store) GetExternalRoute(ctx context.Context, hostID string) (ExternalRoute, error) {
	var v ExternalRoute
	err := s.db.QueryRowContext(ctx, `SELECT host_id,server_id,host_ip,host_port,upstream_scheme,certificate_path,certificate_key_path,status,detail,job_id,updated_at FROM nginx_external_routes WHERE host_id=?`, hostID).
		Scan(&v.HostID, &v.ServerID, &v.HostIP, &v.HostPort, &v.UpstreamScheme, &v.CertificatePath, &v.CertificateKeyPath, &v.Status, &v.Detail, &v.JobID, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ExternalRoute{}, ErrNotFound
	}
	return v, err
}

func (s *Store) SaveExternalRouteDraft(ctx context.Context, v ExternalRoute) (ExternalRoute, error) {
	host, err := s.GetHost(ctx, v.HostID)
	if err != nil {
		return ExternalRoute{}, err
	}
	if host.Binding != nil {
		return ExternalRoute{}, errors.New("此域名已有部署绑定，请先解除")
	}
	existing, err := s.GetExternalRoute(ctx, v.HostID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return ExternalRoute{}, err
	}
	if err == nil && existing.Status != "draft" {
		return ExternalRoute{}, errors.New("已应用的入口请先解除，再修改目标")
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO nginx_external_routes(host_id,server_id,host_ip,host_port,upstream_scheme,certificate_path,certificate_key_path,status,detail,updated_at)
        VALUES(?,?,?,?,?,?,?,'draft','',?) ON CONFLICT(host_id) DO UPDATE SET server_id=excluded.server_id,host_ip=excluded.host_ip,host_port=excluded.host_port,upstream_scheme=excluded.upstream_scheme,certificate_path=excluded.certificate_path,certificate_key_path=excluded.certificate_key_path,detail='',updated_at=excluded.updated_at`,
		v.HostID, v.ServerID, v.HostIP, v.HostPort, v.UpstreamScheme, v.CertificatePath, v.CertificateKeyPath, now()); err != nil {
		return ExternalRoute{}, err
	}
	return s.GetExternalRoute(ctx, v.HostID)
}

func (s *Store) SetExternalRouteJob(ctx context.Context, hostID, jobID, status string) error {
	from := "draft"
	if status == "removing" {
		from = "active"
	} else if status != "applying" {
		return errors.New("invalid route status")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE nginx_external_routes SET status=?,job_id=?,detail='',updated_at=? WHERE host_id=? AND status=?`, status, jobID, now(), hostID, from)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("入口状态已变化")
	}
	return nil
}

func (s *Store) DeleteExternalRouteDraft(ctx context.Context, hostID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM nginx_external_routes WHERE host_id=? AND status='draft'`, hostID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("只有草稿可以删除")
	}
	return nil
}

func (s *Store) ReconcileInterruptedExternalRoutes(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE nginx_external_routes SET
        status=CASE WHEN status='applying' THEN 'draft' ELSE 'active' END,
        detail='任务中断或失败；请重新预检并核对远端 Nginx 配置',updated_at=?
        WHERE status IN ('applying','removing') AND job_id IN
        (SELECT id FROM jobs WHERE status IN ('failed','canceled'))`, now())
	return err
}

func (s *Store) SetExternalRouteFailure(ctx context.Context, hostID, jobID, status, detail string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE nginx_external_routes SET status=?,detail=?,updated_at=? WHERE host_id=? AND job_id=? AND status IN ('applying','removing')`, status, detail, now(), hostID, jobID)
	return err
}

func (s *Store) MarkExternalRouteApplied(ctx context.Context, hostID, serverID, hash, imageRef, jobID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO server_ingresses(server_id,config_hash,image_ref,updated_at) VALUES(?,?,?,?) ON CONFLICT(server_id) DO UPDATE SET config_hash=excluded.config_hash,image_ref=excluded.image_ref,updated_at=excluded.updated_at`, serverID, hash, imageRef, now()); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE nginx_external_routes SET status='active',detail='Nginx 路由已应用',updated_at=? WHERE host_id=? AND job_id=? AND status='applying'`, now(), hostID, jobID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("入口任务状态已变化")
	}
	return tx.Commit()
}

func (s *Store) MarkExternalRouteRemoved(ctx context.Context, hostID, serverID, hash, imageRef string, last bool) error {
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
	if _, err = tx.ExecContext(ctx, `DELETE FROM nginx_external_routes WHERE host_id=? AND status='removing'`, hostID); err != nil {
		return err
	}
	return tx.Commit()
}
