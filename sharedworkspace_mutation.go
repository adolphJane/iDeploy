package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"ideploy/internal/sharedworkspace"
)

type pendingSharedBatch struct {
	RequestID string                             `json:"requestId"`
	Resources []sharedworkspace.ResourceMutation `json:"resources"`
	Secrets   []sharedworkspace.SecretMutation   `json:"secrets"`
}

type SharedConflictItem struct {
	Kind   string          `json:"kind"`
	ID     string          `json:"id"`
	Local  json.RawMessage `json:"local"`
	Remote json.RawMessage `json:"remote"`
}

type SharedConflictView struct {
	ID         string               `json:"id"`
	Error      string               `json:"error"`
	Items      []SharedConflictItem `json:"items"`
	HasSecrets bool                 `json:"hasSecrets"`
}

func (s *SystemService) ListSharedConflicts() ([]SharedConflictView, error) {
	if err := s.SyncSharedWorkspace(); err != nil {
		return nil, err
	}
	if err := s.pruneRedundantSharedConflicts(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	snapshot, err := sharedworkspace.Export(ctx, s.db)
	if err != nil {
		return nil, err
	}
	current := map[string]json.RawMessage{}
	for _, item := range snapshot.Records {
		current[item.Kind+"/"+item.ID] = item.Data
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,payload,last_error FROM shared_pending_batches WHERE status='conflict' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SharedConflictView{}
	for rows.Next() {
		var value SharedConflictView
		var payload string
		if err := rows.Scan(&value.ID, &payload, &value.Error); err != nil {
			return nil, err
		}
		var batch pendingSharedBatch
		if err := json.Unmarshal([]byte(payload), &batch); err != nil {
			return nil, err
		}
		value.HasSecrets = len(batch.Secrets) > 0
		for _, item := range batch.Resources {
			value.Items = append(value.Items, SharedConflictItem{Kind: item.Kind, ID: item.ID, Local: item.Data, Remote: current[item.Kind+"/"+item.ID]})
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (s *SystemService) prepareSharedWrite(name string, inputs []reflect.Value) error {
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil {
		return err
	}
	if status.Mode == "local" {
		return nil
	}
	if status.Mode != "shared" {
		return errors.New("共享工作区迁移尚未完成，当前不能修改数据")
	}
	if name == "DeleteServer" && len(inputs) > 0 && inputs[0].Kind() == reflect.String && inputs[0].String() == status.ServerID {
		return errors.New("当前共享服务的 SSH 连接不能在共享模式下删除")
	}
	if !status.Unlocked {
		return nil // Local changes remain available and are pushed after unlock.
	}
	if err := s.RetryPendingSharedChanges(); err != nil {
		return nil // An unavailable shared service must not block local work.
	}
	_ = s.SyncSharedWorkspace()
	return nil
}

func (s *SystemService) completeSharedWrite() error {
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil || status.Mode == "local" {
		return err
	}
	if !status.Unlocked {
		return nil
	}
	if err := s.PushSharedWorkspace(); err != nil {
		return nil // The local revision remains dirty and is retried on unlock/reconnect.
	}
	return nil
}

func (s *SystemService) PushSharedWorkspace() error {
	return s.pushSharedWorkspace(context.Background())
}

func (s *SystemService) pushSharedWorkspace(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil || status.Mode != "shared" {
		return errors.New("共享工作区尚未启用")
	}
	s.mu.RLock()
	key := append([]byte(nil), s.vaultKey...)
	s.mu.RUnlock()
	if len(key) != 32 {
		return errors.New("共享凭据暂不可用，请检查共享服务连接")
	}
	client, err := s.sharedClient(ctx, status.ServerID)
	if err != nil {
		return err
	}
	snapshot, err := sharedworkspace.Export(ctx, s.db)
	if err != nil {
		return err
	}
	if len(snapshot.Preview.Blockers) > 0 {
		return errors.New(strings.Join(snapshot.Preview.Blockers, "；"))
	}
	resources, err := sharedworkspace.Diff(ctx, s.db, snapshot)
	if err != nil {
		return err
	}
	remoteSecrets, err := client.Secrets(ctx)
	if err != nil {
		return err
	}
	remote := map[string]sharedworkspace.RemoteSecret{}
	for _, item := range remoteSecrets {
		remote[item.ID] = item
	}
	secrets := []sharedworkspace.SecretMutation{}
	for _, item := range snapshot.Secrets {
		value := item.Value
		if item.Ref != "" {
			value, err = loadPortableSecret(ctx, item.Ref)
			if err != nil {
				return fmt.Errorf("凭据 %s 在此设备不可用", item.ID)
			}
		}
		old := remote[item.ID]
		if old.Revision > 0 {
			oldValue, decryptErr := sharedworkspace.DecryptSecret(key, item.ID, old.Envelope)
			if decryptErr != nil {
				return decryptErr
			}
			if oldValue == value {
				delete(remote, item.ID)
				continue
			}
		}
		envelope, encryptErr := sharedworkspace.EncryptSecret(key, item.ID, value)
		if encryptErr != nil {
			return encryptErr
		}
		secrets = append(secrets, sharedworkspace.SecretMutation{ID: item.ID, ExpectedRevision: old.Revision, Envelope: envelope})
		delete(remote, item.ID)
	}
	for _, old := range remote {
		value, decryptErr := sharedworkspace.DecryptSecret(key, old.ID, old.Envelope)
		if decryptErr != nil {
			return decryptErr
		}
		if value == "" {
			continue
		}
		envelope, encryptErr := sharedworkspace.EncryptSecret(key, old.ID, "")
		if encryptErr != nil {
			return encryptErr
		}
		secrets = append(secrets, sharedworkspace.SecretMutation{ID: old.ID, ExpectedRevision: old.Revision, Envelope: envelope})
	}
	if len(resources) == 0 && len(secrets) == 0 {
		return nil
	}
	if len(resources) > 500 || len(secrets) > 500 {
		return errors.New("单次变更超过共享服务批量限制，请分批修改")
	}
	requestBytes := make([]byte, 16)
	if _, err := rand.Read(requestBytes); err != nil {
		return err
	}
	batch := pendingSharedBatch{RequestID: hex.EncodeToString(requestBytes), Resources: resources, Secrets: secrets}
	encoded, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO shared_pending_batches(id,payload,status,created_at) VALUES(?,?,'pending',?)`, batch.RequestID, string(encoded), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return s.retrySharedBatch(ctx, client, batch)
}

func (s *SystemService) ReconcileSharedWorkspace() (SharedWorkspaceStatus, error) {
	s.sharedWriteMu.Lock()
	defer s.sharedWriteMu.Unlock()
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil || status.Mode == "local" {
		return status, err
	}
	if err := s.RetryPendingSharedChanges(); err != nil {
		return status, err
	}
	if status.Unlocked {
		if err := s.PushSharedWorkspace(); err != nil {
			return status, err
		}
	} else {
		snapshot, err := sharedworkspace.Export(context.Background(), s.db)
		if err != nil {
			return status, err
		}
		changes, err := sharedworkspace.Diff(context.Background(), s.db, snapshot)
		if err != nil {
			return status, err
		}
		if len(changes) != 0 {
			return s.GetSharedWorkspaceStatus()
		}
	}
	if err := s.SyncSharedWorkspace(); err != nil {
		return status, err
	}
	return s.GetSharedWorkspaceStatus()
}

func (s *SystemService) retrySharedBatch(ctx context.Context, client *sharedworkspace.Client, batch pendingSharedBatch) error {
	err := client.WriteBatch(ctx, batch.RequestID, batch.Resources, batch.Secrets)
	if err != nil {
		state := "pending"
		if strings.Contains(err.Error(), "409") {
			state = "conflict"
		}
		message := err.Error()
		if len(message) > 500 {
			message = message[:500]
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE shared_pending_batches SET status=?,last_error=? WHERE id=?`, state, message, batch.RequestID)
		return err
	}
	if err := s.SyncSharedWorkspace(); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM shared_pending_batches WHERE id=?`, batch.RequestID)
	return err
}

func (s *SystemService) RetryPendingSharedChanges() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil || status.Mode != "shared" {
		return errors.New("共享工作区尚未启用")
	}
	client, err := s.sharedClient(ctx, status.ServerID)
	if err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM shared_pending_batches WHERE status='pending' ORDER BY created_at`)
	if err != nil {
		return err
	}
	var batches []pendingSharedBatch
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			rows.Close()
			return err
		}
		var batch pendingSharedBatch
		if err := json.Unmarshal([]byte(payload), &batch); err != nil {
			rows.Close()
			return err
		}
		batches = append(batches, batch)
	}
	rows.Close()
	for _, batch := range batches {
		if err := s.retrySharedBatch(ctx, client, batch); err != nil {
			return err
		}
	}
	if dirty, err := s.hasUnpushedSharedChanges(ctx); err != nil {
		return err
	} else if !dirty {
		if err := s.SyncSharedWorkspace(); err != nil {
			return err
		}
	}
	if err := s.pruneRedundantSharedConflicts(); err != nil {
		return err
	}
	if err := s.retryRestoredSharedConflicts(ctx, client); err != nil {
		return err
	}
	var conflicts int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shared_pending_batches WHERE status='conflict'`).Scan(&conflicts); err != nil {
		return err
	}
	if conflicts > 0 {
		return errors.New("存在共享数据冲突，请在设置中选择保留本机或远端版本")
	}
	return nil
}

func (s *SystemService) hasUnpushedSharedChanges(ctx context.Context) (bool, error) {
	snapshot, err := sharedworkspace.Export(ctx, s.db)
	if err != nil {
		return false, err
	}
	changes, err := sharedworkspace.Diff(ctx, s.db, snapshot)
	return len(changes) > 0, err
}

func (s *SystemService) pruneRedundantSharedConflicts() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil {
		return err
	}
	client, err := s.sharedClient(ctx, status.ServerID)
	if err != nil {
		return err
	}
	remoteByKind := map[string]map[string]sharedworkspace.RemoteResource{}
	rows, err := s.db.QueryContext(ctx, `SELECT id,payload FROM shared_pending_batches WHERE status='conflict'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var redundant []string
	for rows.Next() {
		var id, payload string
		if err := rows.Scan(&id, &payload); err != nil {
			return err
		}
		var batch pendingSharedBatch
		if err := json.Unmarshal([]byte(payload), &batch); err != nil {
			return err
		}
		if len(batch.Secrets) != 0 || len(batch.Resources) == 0 {
			continue
		}
		for _, item := range batch.Resources {
			if _, ok := remoteByKind[item.Kind]; !ok {
				resources, err := client.Resources(ctx, item.Kind)
				if err != nil {
					return err
				}
				remoteByKind[item.Kind] = map[string]sharedworkspace.RemoteResource{}
				for _, resource := range resources {
					remoteByKind[item.Kind][resource.ID] = resource
				}
			}
		}
		same, err := sharedConflictApplied(batch, remoteByKind)
		if err != nil {
			return err
		}
		if same {
			redundant = append(redundant, id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	for _, id := range redundant {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM shared_pending_batches WHERE id=? AND status='conflict'`, id); err != nil {
			return err
		}
	}
	return nil
}

func sharedConflictApplied(batch pendingSharedBatch, remoteByKind map[string]map[string]sharedworkspace.RemoteResource) (bool, error) {
	if len(batch.Secrets) != 0 || len(batch.Resources) == 0 {
		return false, nil
	}
	for _, item := range batch.Resources {
		remote, exists := remoteByKind[item.Kind][item.ID]
		if !exists || remote.Revision <= item.ExpectedRevision {
			return false, nil
		}
		if item.Deleted {
			if !remote.Deleted {
				return false, nil
			}
			continue
		}
		if remote.Deleted {
			return false, nil
		}
		localHash, err := sharedworkspace.CanonicalHash(item.Data)
		if err != nil {
			return false, err
		}
		remoteHash, err := sharedworkspace.CanonicalHash(remote.Data)
		if err != nil {
			return false, err
		}
		if localHash != remoteHash {
			if item.Kind != "r.job_summaries" || !newerSharedJobSummary(item.Data, remote.Data) {
				return false, nil
			}
		}
	}
	return true, nil
}

func newerSharedJobSummary(older, newer json.RawMessage) bool {
	var before, after struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		LockKey   string `json:"lock_key"`
		Progress  int    `json:"progress"`
		UpdatedAt string `json:"updated_at"`
	}
	if json.Unmarshal(older, &before) != nil || json.Unmarshal(newer, &after) != nil {
		return false
	}
	return before.ID != "" && before.ID == after.ID && before.Type == after.Type && before.LockKey == after.LockKey && after.Progress >= before.Progress && after.UpdatedAt >= before.UpdatedAt
}

// A failed write can become valid after a repaired server restores the
// materialized revision from its append-only change log. Retry only when all
// expected revisions exactly match current server heads.
func (s *SystemService) retryRestoredSharedConflicts(ctx context.Context, client *sharedworkspace.Client) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id,payload FROM shared_pending_batches WHERE status='conflict' ORDER BY created_at`)
	if err != nil {
		return err
	}
	var batches []pendingSharedBatch
	for rows.Next() {
		var id, payload string
		if err := rows.Scan(&id, &payload); err != nil {
			rows.Close()
			return err
		}
		var batch pendingSharedBatch
		if err := json.Unmarshal([]byte(payload), &batch); err != nil {
			rows.Close()
			return err
		}
		if batch.RequestID == id && len(batch.Resources) > 0 && len(batch.Secrets) == 0 {
			batches = append(batches, batch)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	remoteByKind := map[string]map[string]sharedworkspace.RemoteResource{}
	for _, batch := range batches {
		ready := true
		for _, item := range batch.Resources {
			if _, ok := remoteByKind[item.Kind]; !ok {
				resources, err := client.Resources(ctx, item.Kind)
				if err != nil {
					return err
				}
				remoteByKind[item.Kind] = map[string]sharedworkspace.RemoteResource{}
				for _, resource := range resources {
					remoteByKind[item.Kind][resource.ID] = resource
				}
			}
			if remoteByKind[item.Kind][item.ID].Revision != item.ExpectedRevision {
				ready = false
				break
			}
		}
		if !ready {
			continue
		}
		result, err := s.db.ExecContext(ctx, `UPDATE shared_pending_batches SET status='pending',last_error='' WHERE id=? AND status='conflict'`, batch.RequestID)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			continue
		}
		if err := s.retrySharedBatch(ctx, client, batch); err != nil {
			return err
		}
		for _, item := range batch.Resources {
			delete(remoteByKind, item.Kind)
		}
	}
	return nil
}

func (s *SystemService) ResolveSharedConflict(requestID, choice string) error {
	if choice != "remote" && choice != "local" {
		return errors.New("冲突处理方式无效")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var payload string
	if err := s.db.QueryRowContext(ctx, `SELECT payload FROM shared_pending_batches WHERE id=? AND status='conflict'`, requestID).Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("冲突记录不存在")
		}
		return err
	}
	if choice == "remote" {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM shared_pending_batches WHERE id=?`, requestID); err != nil {
			return err
		}
		return s.SyncSharedWorkspace()
	}
	var batch pendingSharedBatch
	if err := json.Unmarshal([]byte(payload), &batch); err != nil {
		return err
	}
	if err := s.SyncSharedWorkspace(); err != nil {
		return err
	}
	for i := range batch.Resources {
		var revision int64
		err := s.db.QueryRowContext(ctx, `SELECT revision FROM shared_resource_revisions WHERE kind=? AND resource_id=?`, batch.Resources[i].Kind, batch.Resources[i].ID).Scan(&revision)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		batch.Resources[i].ExpectedRevision = revision
	}
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil {
		return err
	}
	client, err := s.sharedClient(ctx, status.ServerID)
	if err != nil {
		return err
	}
	secretList, err := client.Secrets(ctx)
	if err != nil {
		return err
	}
	secretRevisions := map[string]int64{}
	for _, item := range secretList {
		secretRevisions[item.ID] = item.Revision
	}
	for i := range batch.Secrets {
		batch.Secrets[i].ExpectedRevision = secretRevisions[batch.Secrets[i].ID]
	}
	newID := make([]byte, 16)
	if _, err := rand.Read(newID); err != nil {
		return err
	}
	batch.RequestID = hex.EncodeToString(newID)
	encoded, _ := json.Marshal(batch)
	if _, err := s.db.ExecContext(ctx, `UPDATE shared_pending_batches SET id=?,payload=?,status='pending',last_error='' WHERE id=?`, batch.RequestID, string(encoded), requestID); err != nil {
		return err
	}
	return s.retrySharedBatch(ctx, client, batch)
}
