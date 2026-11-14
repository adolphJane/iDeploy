package domain

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type InternalIngress struct {
	FQDN               string `json:"fqdn"`
	ServerID           string `json:"serverId"`
	GatewayPort        int    `json:"gatewayPort"`
	CertificatePath    string `json:"certificatePath"`
	CertificateKeyPath string `json:"certificateKeyPath"`
	Webroot            string `json:"webroot"`
	Status             string `json:"status"`
	Detail             string `json:"detail"`
	JobID              string `json:"jobId"`
	UpdatedAt          string `json:"updatedAt"`
}

func (s *Store) ListInternalIngresses(ctx context.Context) ([]InternalIngress, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT fqdn,server_id,gateway_port,certificate_path,certificate_key_path,webroot,status,detail,job_id,updated_at FROM internal_ingresses ORDER BY fqdn`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []InternalIngress{}
	for rows.Next() {
		var v InternalIngress
		if err := rows.Scan(&v.FQDN, &v.ServerID, &v.GatewayPort, &v.CertificatePath, &v.CertificateKeyPath, &v.Webroot, &v.Status, &v.Detail, &v.JobID, &v.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) GetInternalIngress(ctx context.Context, fqdn string) (InternalIngress, error) {
	var v InternalIngress
	err := s.db.QueryRowContext(ctx, `SELECT fqdn,server_id,gateway_port,certificate_path,certificate_key_path,webroot,status,detail,job_id,updated_at FROM internal_ingresses WHERE fqdn=?`, strings.ToLower(strings.TrimSpace(fqdn))).
		Scan(&v.FQDN, &v.ServerID, &v.GatewayPort, &v.CertificatePath, &v.CertificateKeyPath, &v.Webroot, &v.Status, &v.Detail, &v.JobID, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return InternalIngress{}, ErrNotFound
	}
	return v, err
}

func (s *Store) SaveInternalIngressDraft(ctx context.Context, v InternalIngress) (InternalIngress, error) {
	v.FQDN = strings.ToLower(strings.TrimSpace(v.FQDN))
	if v.GatewayPort == 0 {
		v.GatewayPort = 8766
	}
	if len(v.FQDN) > 253 || !sharedHostPattern.MatchString(v.FQDN) || v.ServerID == "" || v.GatewayPort < 1 || v.GatewayPort > 65535 || (v.CertificatePath == "") != (v.CertificateKeyPath == "") {
		return InternalIngress{}, errors.New("内部入口的域名、服务器或证书路径无效")
	}
	old, err := s.GetInternalIngress(ctx, v.FQDN)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return InternalIngress{}, err
	}
	if err == nil && old.Status != "draft" && old.Status != "failed" {
		return InternalIngress{}, errors.New("已应用的入口不能直接修改；请先使用入口迁移流程")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO internal_ingresses(fqdn,server_id,gateway_port,certificate_path,certificate_key_path,webroot,status,detail,updated_at) VALUES(?,?,?,?,?,?,'draft','',?) ON CONFLICT(fqdn) DO UPDATE SET server_id=excluded.server_id,gateway_port=excluded.gateway_port,certificate_path=excluded.certificate_path,certificate_key_path=excluded.certificate_key_path,webroot=excluded.webroot,status='draft',detail='',job_id='',updated_at=excluded.updated_at`, v.FQDN, v.ServerID, v.GatewayPort, v.CertificatePath, v.CertificateKeyPath, v.Webroot, now())
	if err != nil {
		return InternalIngress{}, err
	}
	return s.GetInternalIngress(ctx, v.FQDN)
}

func (s *Store) SetInternalIngressJob(ctx context.Context, fqdn, jobID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE internal_ingresses SET status='applying',job_id=?,detail='',updated_at=? WHERE fqdn=? AND status IN ('draft','failed')`, jobID, now(), fqdn)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return errors.New("入口状态已变化")
	}
	return nil
}

func (s *Store) SetInternalIngressFailure(ctx context.Context, fqdn, jobID, detail string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE internal_ingresses SET status='failed',detail=?,updated_at=? WHERE fqdn=? AND job_id=? AND status='applying'`, detail, now(), fqdn, jobID)
	return err
}

func (s *Store) ReconcileInterruptedInternalIngresses(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE internal_ingresses SET status='failed',detail='入口任务中断；请核对远端 Nginx 和入口容器',updated_at=? WHERE status='applying' AND job_id IN (SELECT id FROM jobs WHERE status IN ('failed','canceled'))`, now())
	return err
}

func (s *Store) DeleteInternalIngressDraft(ctx context.Context, fqdn string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM internal_ingresses WHERE fqdn=? AND status='draft' AND job_id=''`, strings.ToLower(strings.TrimSpace(fqdn)))
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return errors.New("只有未应用的入口可以删除")
	}
	return nil
}

func (s *Store) MarkInternalIngressReady(ctx context.Context, fqdn, serverID, jobID, hash, container string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO server_ingresses(server_id,config_hash,image_ref,updated_at) VALUES(?,?,?,?) ON CONFLICT(server_id) DO UPDATE SET config_hash=excluded.config_hash,image_ref=excluded.image_ref,updated_at=excluded.updated_at`, serverID, hash, "nginx:"+container, now()); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE internal_ingresses SET status='ready',detail='HTTPS 入口已就绪',updated_at=? WHERE fqdn=? AND server_id=? AND job_id=? AND status='applying'`, now(), fqdn, serverID, jobID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return errors.New("入口配置已变化")
	}
	return tx.Commit()
}

func (s *Store) ReconcileInternalIngressReady(ctx context.Context, fqdn, serverID, hash, container string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE internal_ingresses SET status='ready',detail='已与远端 HTTPS 入口对账',updated_at=? WHERE fqdn=? AND server_id=? AND status='failed'`, now(), fqdn, serverID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return errors.New("入口状态已变化，请重新检查")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO server_ingresses(server_id,config_hash,image_ref,updated_at) VALUES(?,?,?,?) ON CONFLICT(server_id) DO UPDATE SET config_hash=excluded.config_hash,image_ref=excluded.image_ref,updated_at=excluded.updated_at`, serverID, hash, "nginx:"+container, now()); err != nil {
		return err
	}
	return tx.Commit()
}
