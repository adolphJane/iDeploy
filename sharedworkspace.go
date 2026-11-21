package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"ideploy/internal/databasebrowser"
	"ideploy/internal/deployment"
	"ideploy/internal/domain"
	"ideploy/internal/gitauth"
	"ideploy/internal/registry"
	"ideploy/internal/server"
	"ideploy/internal/sharedworkspace"
)

type SharedWorkspaceStatus struct {
	Mode       string `json:"mode"`
	ServerID   string `json:"serverId"`
	DeviceID   string `json:"deviceId"`
	Cursor     int64  `json:"cursor"`
	BatchID    string `json:"-"`
	LastSyncAt string `json:"lastSyncAt"`
	Unlocked   bool   `json:"unlocked"`
	Pending    int    `json:"pending"`
	Conflicts  int    `json:"conflicts"`
}

type SharedSyncHealth struct {
	Mode       string `json:"mode"`
	UpToDate   bool   `json:"upToDate"`
	LastSyncAt string `json:"lastSyncAt"`
	Pending    int    `json:"pending"`
	Conflicts  int    `json:"conflicts"`
	Detail     string `json:"detail"`
}

func (s *SystemService) GetSharedSyncHealth() (SharedSyncHealth, error) {
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil {
		return SharedSyncHealth{}, err
	}
	result := SharedSyncHealth{Mode: status.Mode, LastSyncAt: status.LastSyncAt, Pending: status.Pending, Conflicts: status.Conflicts}
	if status.Mode != "shared" {
		return result, nil
	}
	if status.Pending > 0 || status.Conflicts > 0 {
		result.Detail = "本机有待处理的共享变更"
		return result, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	client, err := s.sharedClient(ctx, status.ServerID)
	if err != nil {
		return result, err
	}
	page, err := client.Changes(ctx, status.Cursor)
	if err != nil {
		return result, err
	}
	if len(page.Items) > 0 || page.HasMore {
		result.Detail = "服务端有尚未拉取的更新"
		return result, nil
	}
	snapshot, err := sharedworkspace.Export(ctx, s.db)
	if err != nil {
		return result, err
	}
	changes, err := sharedworkspace.Diff(ctx, s.db, snapshot)
	if err != nil {
		return result, err
	}
	if len(changes) > 0 {
		s.sharedWriteMu.Lock()
		staleHead, rebaseErr := s.rebaseLegacyProbeTimestamps(ctx, client, snapshot, changes)
		s.sharedWriteMu.Unlock()
		if rebaseErr != nil {
			return result, rebaseErr
		}
		if staleHead {
			result.Detail = "服务端记录落后于同步日志，请升级共享服务后刷新"
			return result, nil
		}
		changes, err = sharedworkspace.Diff(ctx, s.db, snapshot)
		if err != nil {
			return result, err
		}
	}
	if len(changes) > 0 {
		result.Detail = "本机配置尚未上传"
		return result, nil
	}
	result.UpToDate = true
	result.Detail = "配置数据已与服务端同步"
	return result, nil
}

// Older snapshots included updated_at for connection records. Rebase their
// local hash only when every other field and the server revision still match.
func (s *SystemService) rebaseLegacyProbeTimestamps(ctx context.Context, client *sharedworkspace.Client, snapshot sharedworkspace.Snapshot, changes []sharedworkspace.ResourceMutation) (bool, error) {
	staleHead := false
	local := map[string]json.RawMessage{}
	for _, item := range snapshot.Records {
		local[item.Kind+"/"+item.ID] = item.Data
	}
	remoteByKind := map[string]map[string]sharedworkspace.RemoteResource{}
	for _, change := range changes {
		if change.Deleted || (change.Kind != "r.servers" && change.Kind != "r.registries" && change.Kind != "r.databases") {
			continue
		}
		if _, ok := remoteByKind[change.Kind]; !ok {
			items, err := client.Resources(ctx, change.Kind)
			if err != nil {
				return false, err
			}
			remoteByKind[change.Kind] = map[string]sharedworkspace.RemoteResource{}
			for _, item := range items {
				remoteByKind[change.Kind][item.ID] = item
			}
		}
		remote, ok := remoteByKind[change.Kind][change.ID]
		if !ok || remote.Deleted {
			continue
		}
		var oldData, newData map[string]any
		if json.Unmarshal(remote.Data, &oldData) != nil || json.Unmarshal(local[change.Kind+"/"+change.ID], &newData) != nil {
			continue
		}
		delete(oldData, "updated_at")
		oldBytes, _ := json.Marshal(oldData)
		newBytes, _ := json.Marshal(newData)
		if string(oldBytes) != string(newBytes) {
			continue
		}
		if remote.Revision < change.ExpectedRevision {
			staleHead = true
			continue
		}
		if remote.Revision != change.ExpectedRevision {
			continue
		}
		hash, err := sharedworkspace.CanonicalHash(local[change.Kind+"/"+change.ID])
		if err != nil {
			return false, err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE shared_resource_revisions SET content_hash=? WHERE kind=? AND resource_id=? AND revision=?`, hash, change.Kind, change.ID, remote.Revision); err != nil {
			return false, err
		}
	}
	return staleHead, nil
}

type SharedMigrationResult struct {
	Status SharedWorkspaceStatus `json:"status"`
	Count  int                   `json:"count"`
	Digest string                `json:"digest"`
}

type SharedPairingCode struct {
	Code      string `json:"code"`
	ExpiresAt string `json:"expiresAt"`
}

func (s *SystemService) ListSharedDevices() ([]sharedworkspace.Device, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil || status.Mode != "shared" {
		return nil, errors.New("此设备尚未加入共享工作区")
	}
	client, err := s.sharedClient(ctx, status.ServerID)
	if err != nil {
		return nil, err
	}
	return client.Devices(ctx)
}

func (s *SystemService) RevokeSharedDevice(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil || status.Mode != "shared" {
		return errors.New("此设备尚未加入共享工作区")
	}
	if id == status.DeviceID {
		return errors.New("不能在当前设备上撤销自身接入")
	}
	client, err := s.sharedClient(ctx, status.ServerID)
	if err != nil {
		return err
	}
	return client.RevokeDevice(ctx, id)
}

func (s *SystemService) NewSharedPairingCode() (SharedPairingCode, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil || status.Mode != "shared" {
		return SharedPairingCode{}, errors.New("此设备尚未加入共享工作区")
	}
	client, err := s.sharedClient(ctx, status.ServerID)
	if err != nil {
		return SharedPairingCode{}, err
	}
	code, expiry, err := client.NewPairing(ctx)
	return SharedPairingCode{Code: code, ExpiresAt: expiry}, err
}

func (s *SystemService) JoinSharedWorkspace(bootstrapServerID, pairingCode, deviceName string) (SharedMigrationResult, error) {
	return s.joinSharedWorkspace(bootstrapServerID, pairingCode, deviceName)
}

func (s *SystemService) joinSharedWorkspace(bootstrapServerID, accessCode, deviceName string) (SharedMigrationResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil || status.Mode != "local" {
		return SharedMigrationResult{}, errors.New("此设备已有共享工作区状态")
	}
	snapshot, err := sharedworkspace.Export(ctx, s.db)
	if err != nil {
		return SharedMigrationResult{}, err
	}
	if len(snapshot.Records) != 1 || snapshot.Records[0].Kind != "r.servers" || snapshot.Records[0].ID != bootstrapServerID {
		return SharedMigrationResult{}, errors.New("加入共享工作区前，此设备只能保存用于建立 SSH 隧道的一条服务器连接")
	}
	servers, err := s.serverStore()
	if err != nil {
		return SharedMigrationResult{}, err
	}
	bootstrap, err := servers.Get(ctx, bootstrapServerID)
	if err != nil {
		return SharedMigrationResult{}, err
	}
	bootstrapClient := sharedworkspace.NewPairingClient(bootstrap)
	var deviceID, token string
	deviceID, token, err = bootstrapClient.RedeemPairing(ctx, accessCode, deviceName)
	if err != nil {
		return SharedMigrationResult{}, err
	}
	client, err := sharedworkspace.NewClient(bootstrap, token)
	if err != nil {
		return SharedMigrationResult{}, err
	}
	if _, err := client.Workspace(ctx); err != nil {
		return SharedMigrationResult{}, err
	}
	config, err := client.GetVault(ctx)
	if err != nil {
		_ = client.RevokeDevice(ctx, deviceID)
		return SharedMigrationResult{}, err
	}
	key, err := sharedworkspace.VaultKey(config)
	if err != nil {
		_ = client.RevokeDevice(ctx, deviceID)
		return SharedMigrationResult{}, err
	}
	changes := []sharedworkspace.Change{}
	cursor := int64(0)
	for {
		page, err := client.Changes(ctx, cursor)
		if err != nil {
			return SharedMigrationResult{}, err
		}
		changes = append(changes, page.Items...)
		if len(changes) > 5000 {
			return SharedMigrationResult{}, errors.New("共享工作区资源过多，当前客户端无法安全初始化")
		}
		cursor = page.NextCursor
		if !page.HasMore {
			break
		}
	}
	canonicalID := ""
	for _, change := range changes {
		if change.Kind != "r.servers" || change.Deleted {
			continue
		}
		var data struct {
			Host    string `json:"host"`
			Port    int    `json:"port"`
			SSHUser string `json:"ssh_user"`
		}
		if json.Unmarshal(change.Data, &data) == nil && data.Host == bootstrap.Host && data.Port == bootstrap.Port && data.SSHUser == bootstrap.SSHUser {
			canonicalID = change.ID
			break
		}
	}
	if canonicalID == "" {
		return SharedMigrationResult{}, errors.New("共享数据中找不到与 SSH 隧道匹配的服务器")
	}
	if err := server.SavePassword(ctx, server.SharedServerTokenCredentialRef(canonicalID), token); err != nil {
		return SharedMigrationResult{}, err
	}
	credentialRef := ""
	if bootstrap.CredentialRef != "" {
		secret, loadErr := server.LoadPassword(ctx, bootstrap.CredentialRef)
		if loadErr != nil {
			return SharedMigrationResult{}, loadErr
		}
		credentialRef = server.PasswordCredentialRef(canonicalID)
		if bootstrap.AuthMethod == server.AuthMethodPrivateKey {
			credentialRef = server.PrivateKeyPassphraseCredentialRef(canonicalID)
		}
		if err := server.SavePassword(ctx, credentialRef, secret); err != nil {
			return SharedMigrationResult{}, err
		}
	}
	if err := sharedworkspace.JoinChanges(ctx, s.db, changes, cursor, bootstrapServerID, canonicalID, deviceID); err != nil {
		return SharedMigrationResult{}, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE servers SET private_key_path=?,credential_ref=? WHERE id=?`, bootstrap.PrivateKeyPath, credentialRef, canonicalID); err != nil {
		return SharedMigrationResult{}, err
	}
	if err := rememberSharedVaultKey(ctx, canonicalID, key); err != nil {
		return SharedMigrationResult{}, err
	}
	s.mu.Lock()
	s.vaultKey = key
	s.mu.Unlock()
	joined, err := s.GetSharedWorkspaceStatus()
	return SharedMigrationResult{Status: joined, Count: len(changes)}, err
}

func (s *SystemService) sharedClient(ctx context.Context, serverID string) (*sharedworkspace.Client, error) {
	servers, err := s.serverStore()
	if err != nil {
		return nil, err
	}
	connection, err := servers.Get(ctx, serverID)
	if err != nil {
		return nil, err
	}
	token, err := server.LoadPassword(ctx, server.SharedServerTokenCredentialRef(serverID))
	if err != nil {
		return nil, errors.New("此设备没有共享服务令牌，请先部署服务或完成设备配对")
	}
	return sharedworkspace.NewClient(connection, token)
}

func rememberSharedVaultKey(ctx context.Context, serverID string, key []byte) error {
	if len(key) != 32 {
		return errors.New("共享凭据密钥无效")
	}
	return server.SavePassword(ctx, server.SharedVaultKeyCredentialRef(serverID), base64.StdEncoding.EncodeToString(key))
}

func loadSharedVaultKey(serverID string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	value, err := server.LoadPassword(ctx, server.SharedVaultKeyCredentialRef(serverID))
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(key) != 32 {
		return nil, errors.New("本机 Keychain 中的共享凭据密钥无效")
	}
	return key, nil
}

func (s *SystemService) GetSharedWorkspaceStatus() (SharedWorkspaceStatus, error) {
	s.mu.RLock()
	db := s.db
	unlocked := len(s.vaultKey) == 32
	s.mu.RUnlock()
	if db == nil {
		return SharedWorkspaceStatus{}, errors.New("本机数据库未连接")
	}
	var value SharedWorkspaceStatus
	err := db.QueryRow(`SELECT mode,server_id,device_id,cursor,batch_id,last_sync_at FROM shared_sync_state WHERE id=1`).Scan(&value.Mode, &value.ServerID, &value.DeviceID, &value.Cursor, &value.BatchID, &value.LastSyncAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SharedWorkspaceStatus{Mode: "local"}, nil
	}
	if err == nil && value.Mode == "shared" && !unlocked {
		key, loadErr := loadSharedVaultKey(value.ServerID)
		if loadErr != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			if client, clientErr := s.sharedClient(ctx, value.ServerID); clientErr == nil {
				if config, configErr := client.GetVault(ctx); configErr == nil {
					key, loadErr = sharedworkspace.VaultKey(config)
					if loadErr == nil {
						_ = rememberSharedVaultKey(ctx, value.ServerID, key)
					}
				}
			}
			cancel()
		}
		if loadErr == nil {
			s.mu.Lock()
			s.vaultKey = key
			s.mu.Unlock()
			unlocked = true
		}
	}
	value.Unlocked = unlocked
	if err == nil {
		_ = db.QueryRow(`SELECT COUNT(*) FROM shared_pending_batches WHERE status='pending'`).Scan(&value.Pending)
		_ = db.QueryRow(`SELECT COUNT(*) FROM shared_pending_batches WHERE status='conflict'`).Scan(&value.Conflicts)
	}
	return value, err
}

func (s *SystemService) PreviewSharedWorkspaceMigration(serverID string) (sharedworkspace.Preview, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	client, err := s.sharedClient(ctx, serverID)
	if err != nil {
		return sharedworkspace.Preview{}, err
	}
	if _, err := client.Workspace(ctx); err != nil {
		return sharedworkspace.Preview{}, err
	}
	snapshot, err := sharedworkspace.Export(ctx, s.db)
	if err != nil {
		return sharedworkspace.Preview{}, err
	}
	return snapshot.Preview, nil
}

func (s *SystemService) StartSharedWorkspaceMigration(serverID string) (SharedMigrationResult, error) {
	s.sharedWriteMu.Lock()
	defer s.sharedWriteMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil {
		return SharedMigrationResult{}, err
	}
	if status.Mode == "shared" {
		return SharedMigrationResult{}, errors.New("此设备已启用共享工作区")
	}
	client, err := s.sharedClient(ctx, serverID)
	if err != nil {
		return SharedMigrationResult{}, err
	}
	if _, err := client.Workspace(ctx); err != nil {
		return SharedMigrationResult{}, err
	}
	page, err := client.Changes(ctx, 0)
	if err != nil {
		return SharedMigrationResult{}, err
	}
	if len(page.Items) != 0 && !(status.Mode == "preparing" && status.BatchID != "") {
		return SharedMigrationResult{}, errors.New("共享工作区已有数据；首次迁移只允许导入空工作区")
	}
	snapshot, err := sharedworkspace.Export(ctx, s.db)
	if err != nil {
		return SharedMigrationResult{}, err
	}
	if len(snapshot.Preview.Blockers) != 0 {
		return SharedMigrationResult{}, errors.New(strings.Join(snapshot.Preview.Blockers, "；"))
	}
	config, err := client.GetVault(ctx)
	var key []byte
	if err == nil {
		key, err = sharedworkspace.VaultKey(config)
		if err != nil {
			return SharedMigrationResult{}, errors.New("共享保险库已存在但本机缺少密钥；请先重置旧共享数据库")
		}
	} else {
		if !strings.Contains(err.Error(), "404") {
			return SharedMigrationResult{}, err
		}
		config, key, err = sharedworkspace.CreateVault()
		if err != nil {
			return SharedMigrationResult{}, err
		}
		if err := client.CreateVault(ctx, config); err != nil {
			return SharedMigrationResult{}, err
		}
	}
	if err := rememberSharedVaultKey(ctx, serverID, key); err != nil {
		return SharedMigrationResult{}, err
	}
	existing, err := client.Secrets(ctx)
	if err != nil {
		return SharedMigrationResult{}, err
	}
	revisions := map[string]int64{}
	for _, value := range existing {
		revisions[value.ID] = value.Revision
	}
	for _, item := range snapshot.Secrets {
		value := item.Value
		if item.Ref != "" {
			value, err = loadPortableSecret(ctx, item.Ref)
			if err != nil {
				return SharedMigrationResult{}, fmt.Errorf("凭据 %s 在本机不可用，迁移未完成", item.ID)
			}
		}
		envelope, encryptErr := sharedworkspace.EncryptSecret(key, item.ID, value)
		if encryptErr != nil {
			return SharedMigrationResult{}, encryptErr
		}
		if err := client.PutSecret(ctx, item.ID, envelope, revisions[item.ID]); err != nil {
			return SharedMigrationResult{}, err
		}
	}
	batchID := status.BatchID
	if status.Mode != "preparing" || batchID == "" {
		bytes := make([]byte, 16)
		if _, err := rand.Read(bytes); err != nil {
			return SharedMigrationResult{}, err
		}
		batchID = hex.EncodeToString(bytes)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO shared_sync_state(id,server_id,mode,batch_id) VALUES(1,?,'preparing',?) ON CONFLICT(id) DO UPDATE SET server_id=excluded.server_id,mode='preparing',batch_id=excluded.batch_id`, serverID, batchID); err != nil {
		return SharedMigrationResult{}, err
	}
	receipt, err := client.Import(ctx, batchID, snapshot.Records)
	if err != nil {
		return SharedMigrationResult{}, err
	}
	encoded, _ := json.Marshal(snapshot.Records)
	digest := sha256.Sum256(encoded)
	if receipt.Count != len(snapshot.Records) || receipt.Digest != hex.EncodeToString(digest[:]) {
		return SharedMigrationResult{}, errors.New("服务端迁移回执与本地快照不一致，未启用共享模式")
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE shared_sync_state SET mode='shared',server_id=?,batch_id=? WHERE id=1`, serverID, batchID); err != nil {
		return SharedMigrationResult{}, err
	}
	s.mu.Lock()
	s.vaultKey = key
	s.mu.Unlock()
	if err := s.SyncSharedWorkspace(); err != nil {
		return SharedMigrationResult{}, err
	}
	newStatus, err := s.GetSharedWorkspaceStatus()
	return SharedMigrationResult{Status: newStatus, Count: receipt.Count, Digest: receipt.Digest}, err
}

func loadPortableSecret(ctx context.Context, ref string) (string, error) {
	switch {
	case strings.HasPrefix(ref, "keychain:ssh-") || strings.HasPrefix(ref, "keychain:agent-") || strings.HasPrefix(ref, "keychain:shared-server-"):
		return server.LoadPassword(ctx, ref)
	case strings.HasPrefix(ref, "keychain:registry-"):
		return registry.LoadPassword(ctx, ref)
	case strings.HasPrefix(ref, "keychain:database-"):
		return databasebrowser.LoadPassword(ctx, ref)
	case strings.HasPrefix(ref, "keychain:alidns-") || strings.HasPrefix(ref, "keychain:cloudflare-"):
		return domain.LoadDNSSecret(ctx, ref)
	case strings.HasPrefix(ref, "keychain:deployment-environment:"):
		return deployment.LoadEnvironmentSecret(ctx, ref)
	case strings.HasPrefix(ref, "keychain:git-project:"):
		value, err := gitauth.Load(ctx, ref)
		if err != nil {
			return "", err
		}
		return gitauth.Encode(value)
	default:
		return "", errors.New("unsupported credential reference")
	}
}

func (s *SystemService) SyncSharedWorkspace() error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	status, err := s.GetSharedWorkspaceStatus()
	if err != nil || status.Mode != "shared" {
		return errors.New("共享工作区尚未启用")
	}
	if !status.Unlocked {
		dirty, err := s.hasUnpushedSharedChanges(ctx)
		if err != nil {
			return err
		}
		if dirty {
			return nil
		} // Keep local edits intact until the vault is unlocked.
	}
	client, err := s.sharedClient(ctx, status.ServerID)
	if err != nil {
		return err
	}
	if _, err := client.Workspace(ctx); err != nil {
		return err
	}
	cursor := status.Cursor
	for {
		page, err := client.Changes(ctx, cursor)
		if err != nil {
			return err
		}
		if page.NextCursor < cursor {
			return errors.New("共享服务返回了倒退的同步游标")
		}
		if err := sharedworkspace.ApplyChanges(ctx, s.db, page.Items, page.NextCursor); err != nil {
			return err
		}
		cursor = page.NextCursor
		if !page.HasMore {
			return s.restoreSharedSecrets(ctx, client)
		}
	}
}

func (s *SystemService) restoreSharedSecrets(ctx context.Context, client *sharedworkspace.Client) error {
	s.mu.RLock()
	key := append([]byte(nil), s.vaultKey...)
	s.mu.RUnlock()
	if len(key) != 32 {
		return nil // Metadata remains readable while the vault is locked.
	}
	items, err := client.Secrets(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		value, err := sharedworkspace.DecryptSecret(key, item.ID, item.Envelope)
		if err != nil {
			return err
		}
		if err := s.savePortableSecret(ctx, item.ID, value); err != nil {
			return err
		}
	}
	return nil
}

func (s *SystemService) savePortableSecret(ctx context.Context, secretID, value string) error {
	parts := strings.Split(secretID, ":")
	if len(parts) != 3 || parts[1] == "" {
		return errors.New("共享凭据标识无效")
	}
	kind, id, field := parts[0], parts[1], parts[2]
	if field != "credential" {
		column := ""
		table := ""
		switch kind + ":" + field {
		case "r.build_configs:build_args_json":
			table, column = "build_configs", "build_args_json"
		case "r.releases:manifest":
			store, err := s.deploymentStore()
			if err != nil {
				return err
			}
			return store.SetManifest(ctx, id, value)
		case "r.env:plain_value":
			table, column = "deployment_environment_variables", "plain_value"
		case "r.dns_connections:access_key_id":
			table, column = "dns_connections", "access_key_id"
		default:
			return errors.New("共享凭据字段不受支持")
		}
		spec, _ := sharedworkspace.Entity(kind)
		_, err := s.db.ExecContext(ctx, "UPDATE "+table+" SET "+column+"=? WHERE "+spec.ID+"=?", value, id)
		return err
	}
	switch kind {
	case "r.project_sources":
		ref := gitauth.Ref(id)
		if value == "" {
			_ = gitauth.Delete(ctx, ref)
			_, err := s.db.ExecContext(ctx, `UPDATE project_sources SET credential_ref='' WHERE project_id=?`, id)
			return err
		}
		credential, err := gitauth.Decode(value)
		if err != nil {
			return err
		}
		if err := gitauth.Save(ctx, ref, credential); err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx, `UPDATE project_sources SET credential_ref=? WHERE project_id=?`, ref, id)
		return err
	case "r.servers":
		var method string
		if err := s.db.QueryRowContext(ctx, `SELECT auth_method FROM servers WHERE id=?`, id).Scan(&method); err != nil && !(errors.Is(err, sql.ErrNoRows) && value == "") {
			return err
		}
		ref := server.PasswordCredentialRef(id)
		if method == server.AuthMethodPrivateKey {
			ref = server.PrivateKeyPassphraseCredentialRef(id)
		}
		if value == "" {
			_ = server.DeletePassword(ctx, ref)
			_ = server.DeletePassword(ctx, server.PrivateKeyPassphraseCredentialRef(id))
			_, err := s.db.ExecContext(ctx, `UPDATE servers SET credential_ref='' WHERE id=?`, id)
			return err
		}
		if err := server.SavePassword(ctx, ref, value); err != nil {
			return err
		}
		_, err := s.db.ExecContext(ctx, `UPDATE servers SET credential_ref=? WHERE id=?`, ref, id)
		return err
	case "r.registries":
		ref := registry.PasswordCredentialRef(id)
		if value == "" {
			_ = registry.DeletePassword(ctx, ref)
			_, err := s.db.ExecContext(ctx, `UPDATE registry_connections SET credential_ref='' WHERE id=?`, id)
			return err
		}
		if err := registry.SavePassword(ctx, ref, value); err != nil {
			return err
		}
		_, err := s.db.ExecContext(ctx, `UPDATE registry_connections SET credential_ref=? WHERE id=?`, ref, id)
		return err
	case "r.databases":
		ref := databasebrowser.PasswordCredentialRef(id)
		if value == "" {
			_ = databasebrowser.DeletePassword(ctx, ref)
			_, err := s.db.ExecContext(ctx, `UPDATE database_connections SET credential_ref='' WHERE id=?`, id)
			return err
		}
		if err := databasebrowser.SavePassword(ctx, ref, value); err != nil {
			return err
		}
		_, err := s.db.ExecContext(ctx, `UPDATE database_connections SET credential_ref=? WHERE id=?`, ref, id)
		return err
	case "r.dns_connections":
		var provider string
		if err := s.db.QueryRowContext(ctx, `SELECT provider FROM dns_connections WHERE id=?`, id).Scan(&provider); err != nil && !(errors.Is(err, sql.ErrNoRows) && value == "") {
			return err
		}
		ref := domain.DNSCredentialRefForProvider(provider, id)
		if value == "" {
			_ = domain.DeleteDNSSecret(ctx, ref)
			_ = domain.DeleteDNSSecret(ctx, domain.DNSCredentialRefForProvider("cloudflare", id))
			return nil
		}
		if err := domain.SaveDNSSecret(ctx, ref, value); err != nil {
			return err
		}
		_, err := s.db.ExecContext(ctx, `UPDATE dns_connections SET credential_ref=? WHERE id=?`, ref, id)
		return err
	case "r.env":
		ref := deployment.EnvironmentSecretCredentialRef(id)
		if value == "" {
			return deployment.DeleteEnvironmentSecret(ctx, ref)
		}
		return deployment.SaveEnvironmentSecret(ctx, ref, value)
	default:
		return errors.New("共享凭据类型不受支持")
	}
}
