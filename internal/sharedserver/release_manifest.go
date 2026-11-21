package sharedserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"ideploy/internal/buildexecutor"
	"ideploy/internal/sharedworkspace"
)

func (s *Server) releaseVaultKey(ctx context.Context) ([]byte, error) {
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT configuration FROM shared_vault WHERE id=1`).Scan(&raw); err != nil {
		return nil, err
	}
	var config sharedworkspace.VaultConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return nil, err
	}
	return sharedworkspace.VaultKey(config)
}

func (s *Server) decryptReleaseManifest(ctx context.Context, jobID, raw string) (string, error) {
	key, err := s.releaseVaultKey(ctx)
	if err != nil {
		return "", err
	}
	var envelope sharedworkspace.Envelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return "", err
	}
	return sharedworkspace.DecryptSecret(key, "release-manifest:"+jobID, envelope)
}

func (s *Server) updateComposeArtifact(ctx context.Context, id, workerID string, generation int64, status, stage, imageRef, manifest string) error {
	if !releaseIDPattern.MatchString(id) || !releaseIDPattern.MatchString(workerID) || generation < 1 || status != "artifact_ready" || stage != "artifact_ready" || !strings.HasPrefix(imageRef, "compose:") || len(imageRef) > 512 || len(manifest) > 900*1024 {
		return errors.New("Compose 制品无效")
	}
	if _, err := buildexecutor.PinComposeManifest(manifest); err != nil {
		return err
	}
	key, err := s.releaseVaultKey(ctx)
	if err != nil {
		return err
	}
	envelope, err := sharedworkspace.EncryptSecret(key, "release-manifest:"+id, manifest)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, s.query(`UPDATE release_jobs SET status='artifact_ready',stage='artifact_ready',image_ref=$1,manifest_envelope=$2,lease_until=$3,updated_at=$4
		WHERE id=$5 AND lease_owner=$6 AND lease_generation=$7 AND status IN ('running','cancel_requested') AND lease_until>$8`), imageRef, string(raw), releaseTime(now.Add(time.Minute)), releaseTime(now), id, workerID, generation, releaseTime(now))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	return nil
}
