package domain

import (
	"context"
	"database/sql"
	"errors"
)

type Route struct {
	HostID         string
	FQDN           string
	HostIP         string
	HostPort       int
	UpstreamScheme string
	Certificate    string
	CertificateKey string
}

type Ingress struct {
	ServerID   string
	ConfigHash string
	ImageRef   string
}

func (s *Store) GetIngress(ctx context.Context, serverID string) (Ingress, error) {
	var item Ingress
	err := s.db.QueryRowContext(ctx, "SELECT server_id,config_hash,image_ref FROM server_ingresses WHERE server_id=?", serverID).
		Scan(&item.ServerID, &item.ConfigHash, &item.ImageRef)
	if errors.Is(err, sql.ErrNoRows) {
		return Ingress{}, ErrNotFound
	}
	return item, err
}

func (s *Store) ListServerRoutes(ctx context.Context, serverID string) ([]Route, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT h.id,h.fqdn,b.host_ip,b.host_port,b.upstream_protocol,b.certificate_path,b.certificate_key_path
        FROM domain_bindings b JOIN domain_hosts h ON h.id=b.host_id
        JOIN deployment_targets d ON d.id=b.deployment_id
		WHERE d.server_id=? AND b.status IN ('active','removing')
		UNION ALL
		SELECT h.id,h.fqdn,e.host_ip,e.host_port,e.upstream_scheme,e.certificate_path,e.certificate_key_path
		FROM nginx_external_routes e JOIN domain_hosts h ON h.id=e.host_id
		WHERE e.server_id=? AND e.status IN ('active','removing') ORDER BY 2`, serverID, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Route{}
	for rows.Next() {
		var route Route
		if err := rows.Scan(&route.HostID, &route.FQDN, &route.HostIP, &route.HostPort, &route.UpstreamScheme, &route.Certificate, &route.CertificateKey); err != nil {
			return nil, err
		}
		result = append(result, route)
	}
	return result, rows.Err()
}

func (s *Store) SetBindingJob(ctx context.Context, hostID, jobID, status string) error {
	if status != "applying" && status != "removing" {
		return errors.New("invalid binding status")
	}
	allowed := "draft"
	if status == "removing" {
		allowed = "active"
	}
	_, err := s.db.ExecContext(ctx, "UPDATE domain_bindings SET job_id=?,status=?,detail='',updated_at=? WHERE host_id=? AND status IN (?,?)", jobID, status, now(), hostID, allowed, status)
	return err
}

func (s *Store) SetBindingFailure(ctx context.Context, hostID, status, detail string) error {
	if status != "draft" && status != "active" {
		return errors.New("invalid binding status")
	}
	_, err := s.db.ExecContext(ctx, "UPDATE domain_bindings SET status=?,detail=?,updated_at=? WHERE host_id=?", status, detail, now(), hostID)
	return err
}

func (s *Store) ReconcileFinishedJob(ctx context.Context, hostID, jobID, status, detail string) error {
	if status != "draft" && status != "active" {
		return errors.New("invalid binding status")
	}
	from := "applying"
	if status == "active" {
		from = "removing"
	}
	_, err := s.db.ExecContext(ctx, "UPDATE domain_bindings SET status=?,detail=?,updated_at=? WHERE host_id=? AND job_id=? AND status=?", status, detail, now(), hostID, jobID, from)
	return err
}

func (s *Store) MarkApplied(ctx context.Context, hostID, serverID, hostIP string, hostPort int, imageRef, hash, jobID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO server_ingresses(server_id,config_hash,image_ref,updated_at)
        VALUES(?,?,?,?) ON CONFLICT(server_id) DO UPDATE SET config_hash=excluded.config_hash,image_ref=excluded.image_ref,updated_at=excluded.updated_at`, serverID, hash, imageRef, now())
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, "UPDATE domain_bindings SET status='active',detail='入口配置已应用；请单独检查 DNS 和 HTTPS',host_ip=?,host_port=?,job_id=?,updated_at=? WHERE host_id=?", hostIP, hostPort, jobID, now(), hostID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *Store) MarkRemoved(ctx context.Context, hostID, serverID, imageRef, hash string, lastRoute bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if lastRoute {
		_, err = tx.ExecContext(ctx, "DELETE FROM server_ingresses WHERE server_id=?", serverID)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE server_ingresses SET config_hash=?,image_ref=?,updated_at=? WHERE server_id=?", hash, imageRef, now(), serverID)
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM domain_bindings WHERE host_id=?", hostID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
