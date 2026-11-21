package sharedworkspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"ideploy/internal/deployment"
)

type Record struct {
	Kind string          `json:"kind"`
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data"`
}

// PortableSecret never leaves this process unencrypted. Ref addresses the
// source device's Keychain; Value is for fields that lived in local SQLite.
type PortableSecret struct {
	ID    string `json:"-"`
	Ref   string `json:"-"`
	Value string `json:"-"`
}

type Preview struct {
	Counts       map[string]int `json:"counts"`
	SecretCount  int            `json:"secretCount"`
	LocalPaths   []string       `json:"localPaths"`
	Blockers     []string       `json:"blockers"`
	TotalRecords int            `json:"totalRecords"`
}

type Snapshot struct {
	Records []Record
	Secrets []PortableSecret
	Preview Preview
}

var extraColumns = map[string][]string{
	"project_sources":                  {"local_path", "credential_ref"},
	"build_configs":                    {"build_args_json"},
	"servers":                          {"private_key_path", "credential_ref"},
	"registry_connections":             {"credential_ref"},
	"database_connections":             {"credential_ref"},
	"deployment_environment_variables": {"plain_value", "credential_ref"},
	"deployment_releases":              {"manifest", "manifest_envelope"},
	"dns_connections":                  {"access_key_id", "credential_ref"},
}

func Export(ctx context.Context, db *sql.DB) (Snapshot, error) {
	result := Snapshot{Records: []Record{}, Secrets: []PortableSecret{}, Preview: Preview{Counts: map[string]int{}, LocalPaths: []string{}, Blockers: []string{}}}
	for _, spec := range Entities {
		columns := append(append([]string{}, spec.Columns...), extraColumns[spec.Table]...)
		rows, err := db.QueryContext(ctx, "SELECT "+strings.Join(columns, ",")+" FROM "+spec.Table+" ORDER BY "+spec.ID)
		if err != nil {
			return Snapshot{}, fmt.Errorf("读取 %s: %w", spec.Table, err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(values))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				rows.Close()
				return Snapshot{}, err
			}
			data := make(map[string]any, len(spec.Columns))
			extra := map[string]string{}
			for i, column := range columns {
				value := values[i]
				if binary, ok := value.([]byte); ok {
					value = string(binary)
				}
				if i < len(spec.Columns) {
					data[column] = value
				} else if value != nil {
					extra[column] = fmt.Sprint(value)
				}
			}
			id, ok := data[spec.ID].(string)
			if !ok || id == "" {
				rows.Close()
				return Snapshot{}, errors.New("迁移数据缺少稳定 ID")
			}
			if spec.Table == "project_sources" {
				if path := extra["local_path"]; path != "" {
					result.Preview.LocalPaths = append(result.Preview.LocalPaths, "项目 "+id+" 的源码目录需在其他设备重新选择")
				}
				if ref := extra["credential_ref"]; ref != "" && ref != "keychain:git-project:"+id {
					result.Preview.Blockers = append(result.Preview.Blockers, "项目 "+id+" 的 Git 凭据引用无法识别")
				}
			}
			if spec.Table == "deployment_releases" && extra["manifest_envelope"] != "" {
				manifest, err := deployment.OpenManifestEnvelope(ctx, extra["manifest_envelope"])
				if err != nil {
					rows.Close()
					return Snapshot{}, fmt.Errorf("读取加密发布快照: %w", err)
				}
				extra["manifest"] = manifest
			}
			if spec.Table == "servers" && extra["private_key_path"] != "" {
				result.Preview.LocalPaths = append(result.Preview.LocalPaths, "服务器 "+id+" 的 SSH 私钥需在其他设备重新选择")
			}
			if spec.Table == "project_sources" {
				if address, _ := data["repository_url"].(string); address != "" {
					parsed, parseErr := url.Parse(address)
					if parseErr != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
						result.Preview.Blockers = append(result.Preview.Blockers, "项目 "+id+" 的仓库 URL 可能包含凭据，请先移除 URL 中的认证或查询参数")
					}
				}
			}
			for _, field := range []string{"build_args_json", "plain_value", "manifest", "access_key_id"} {
				if value := extra[field]; value != "" && value != "{}" {
					result.Secrets = append(result.Secrets, PortableSecret{ID: spec.Kind + ":" + id + ":" + field, Value: value})
				}
			}
			if ref := extra["credential_ref"]; ref != "" {
				result.Secrets = append(result.Secrets, PortableSecret{ID: spec.Kind + ":" + id + ":credential", Ref: ref})
			}
			encoded, err := json.Marshal(data)
			if err != nil {
				rows.Close()
				return Snapshot{}, err
			}
			result.Records = append(result.Records, Record{Kind: spec.Kind, ID: id, Data: encoded})
			result.Preview.Counts[spec.Table]++
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return Snapshot{}, err
		}
		rows.Close()
	}
	result.Preview.TotalRecords = len(result.Records)
	result.Preview.SecretCount = len(result.Secrets)
	return result, nil
}
