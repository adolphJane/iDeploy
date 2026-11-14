package sharedworkspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Change struct {
	Sequence int64           `json:"sequence"`
	Kind     string          `json:"kind"`
	ID       string          `json:"id"`
	Revision int64           `json:"revision"`
	Deleted  bool            `json:"deleted"`
	Data     json.RawMessage `json:"data"`
}

// ApplyChanges materializes a server page and its cursor atomically. The
// portable field allowlist keeps local paths and Keychain references intact.
func ApplyChanges(ctx context.Context, db *sql.DB, changes []Change, cursor int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := applyChangesTx(ctx, tx, changes, cursor); err != nil {
		return err
	}
	return tx.Commit()
}

// JoinChanges replaces a temporary SSH profile with its canonical shared ID
// and applies the first snapshot in the same local transaction.
func JoinChanges(ctx context.Context, db *sql.DB, changes []Change, cursor int64, bootstrapID, serverID, deviceID string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var privateKeyPath string
	if err := tx.QueryRowContext(ctx, `SELECT private_key_path FROM servers WHERE id=?`, bootstrapID).Scan(&privateKeyPath); err != nil {
		return err
	}
	if bootstrapID != serverID {
		if _, err := tx.ExecContext(ctx, `DELETE FROM servers WHERE id=?`, bootstrapID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO shared_sync_state(id,server_id,device_id,mode,cursor) VALUES(1,?,?,'shared',0)`, serverID, deviceID); err != nil {
		return err
	}
	if err := applyChangesTx(ctx, tx, changes, cursor); err != nil {
		return err
	}
	if privateKeyPath != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE servers SET private_key_path=? WHERE id=?`, privateKeyPath, serverID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func applyChangesTx(ctx context.Context, tx *sql.Tx, changes []Change, cursor int64) error {
	for _, item := range changes {
		spec, ok := Entity(item.Kind)
		if !ok || item.ID == "" || item.Revision < 1 {
			return errors.New("共享服务返回了不支持的资源")
		}
		var previous int64
		err := tx.QueryRowContext(ctx, `SELECT revision FROM shared_resource_revisions WHERE kind=? AND resource_id=?`, item.Kind, item.ID).Scan(&previous)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if item.Revision <= previous {
			continue
		}
		if item.Deleted {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+spec.Table+" WHERE "+spec.ID+"=?", item.ID); err != nil {
				return fmt.Errorf("删除 %s: %w", spec.Table, err)
			}
		} else if err := applyRecord(ctx, tx, spec, item); err != nil {
			return err
		}
		hash := ""
		if !item.Deleted {
			hash, err = CanonicalHash(item.Data)
			if err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO shared_resource_revisions(kind,resource_id,revision,content_hash) VALUES(?,?,?,?) ON CONFLICT(kind,resource_id) DO UPDATE SET revision=excluded.revision,content_hash=excluded.content_hash`, item.Kind, item.ID, item.Revision, hash); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE shared_sync_state SET cursor=?,last_sync_at=? WHERE id=1 AND mode='shared'`, cursor, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return nil
}

func CanonicalHash(data []byte) (string, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func applyRecord(ctx context.Context, tx *sql.Tx, spec EntitySpec, item Change) error {
	var data map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(item.Data)))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil || data == nil || data[spec.ID] != item.ID {
		return errors.New("共享资源 ID 与内容不一致")
	}
	columns := []string{}
	values := []any{}
	for _, column := range spec.Columns {
		if value, present := data[column]; present {
			columns = append(columns, column)
			values = append(values, value)
		}
	}
	if len(columns) == 0 {
		return errors.New("共享资源缺少字段")
	}
	if spec.Table == "servers" || spec.Table == "registry_connections" || spec.Table == "database_connections" {
		updatedAt, _ := data["created_at"].(string)
		if updatedAt == "" {
			updatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
		columns = append(columns, "updated_at")
		values = append(values, updatedAt)
	}
	// These columns have no SQLite default, while their values live in the
	// encrypted vault and the local Keychain rather than the shared record.
	if spec.Table == "dns_connections" {
		prefix := "keychain:alidns-access-key-secret:"
		if data["provider"] == "cloudflare" {
			prefix = "keychain:cloudflare-api-token:"
		}
		columns = append(columns, "access_key_id", "credential_ref")
		values = append(values, "", prefix+item.ID)
	}
	if spec.Table == "deployment_environment_variables" && data["kind"] == "secret" {
		columns = append(columns, "credential_ref")
		values = append(values, "keychain:deployment-environment:"+item.ID)
	}
	placeholders := make([]string, len(columns))
	updates := []string{}
	for i, column := range columns {
		placeholders[i] = "?"
		deviceProbeTime := column == "updated_at" && (spec.Table == "servers" || spec.Table == "registry_connections" || spec.Table == "database_connections")
		if column != spec.ID && column != "credential_ref" && column != "access_key_id" && !deviceProbeTime {
			updates = append(updates, column+"=excluded."+column)
		}
	}
	statement := "INSERT INTO " + spec.Table + "(" + strings.Join(columns, ",") + ") VALUES(" + strings.Join(placeholders, ",") + ") ON CONFLICT(" + spec.ID + ") DO UPDATE SET " + strings.Join(updates, ",")
	if _, err := tx.ExecContext(ctx, statement, values...); err != nil {
		return fmt.Errorf("恢复 %s: %w", spec.Table, err)
	}
	return nil
}
