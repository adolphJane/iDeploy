package deployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"ideploy/internal/server"
)

type RuntimePolicy struct {
	Revision    int64           `json:"revision"`
	Mode        string          `json:"mode"`
	Services    []string        `json:"services,omitempty"`
	Mounts      []RuntimeMount  `json:"mounts,omitempty"`
	Directories []DataDirectory `json:"directories,omitempty"`
}

const (
	RuntimeObserve = "observe"
	RuntimeEnforce = "enforce"
)

type RuntimeMount struct {
	Service     string `json:"service,omitempty"`
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"readOnly"`
}

type DataDirectory struct {
	Service string `json:"service,omitempty"`
	Path    string `json:"path"`
	Access  string `json:"access,omitempty"`
}

type RuntimeCheck struct {
	Code    string `json:"code"`
	Status  string `json:"status"`
	Service string `json:"service,omitempty"`
	Path    string `json:"path,omitempty"`
}

var policyService = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
var policyVolume = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{1,254}$`)

func (policy RuntimePolicy) Validate(kind string) error {
	if policy.Mode != RuntimeObserve && policy.Mode != RuntimeEnforce {
		return errors.New("发布策略模式必须是 observe 或 enforce")
	}
	if len(policy.Services) > 16 || len(policy.Mounts) > 16 || len(policy.Directories) > 16 {
		return errors.New("发布策略最多可登记 16 个服务、16 个挂载和 16 个数据目录")
	}
	services := map[string]bool{}
	for _, service := range policy.Services {
		if kind != "compose" || !policyService.MatchString(service) || services[service] {
			return errors.New("发布策略包含无效或重复的 Compose 服务")
		}
		services[service] = true
	}
	validService := func(service string) bool {
		if kind == "compose" {
			return services[service]
		}
		return service == ""
	}
	mounts := map[string]bool{}
	for _, mount := range policy.Mounts {
		key := mount.Service + "\x00" + mount.Destination
		validSource := mount.Type == "bind" && validMountPath(mount.Source) ||
			mount.Type == "volume" && policyVolume.MatchString(mount.Source)
		if !validSource || !validService(mount.Service) || !validMountPath(mount.Destination) || mounts[key] {
			return errors.New("发布策略包含无效或重复的挂载")
		}
		mounts[key] = true
	}
	directories := map[string]bool{}
	for _, directory := range policy.Directories {
		key := directory.Service + "\x00" + directory.Path
		if !validService(directory.Service) || !server.ValidRuntimeDirectory(directory.Path) || directories[key] ||
			(directory.Access != "" && directory.Access != "readable" && directory.Access != "writable") {
			return errors.New("发布策略包含无效或重复的数据目录")
		}
		directories[key] = true
		if policy.Mode == RuntimeEnforce {
			covered := false
			for _, mount := range policy.Mounts {
				covered = covered || mount.Service == directory.Service &&
					(directory.Path == mount.Destination || strings.HasPrefix(directory.Path, mount.Destination+"/"))
			}
			if !covered {
				return errors.New("受保护的数据目录必须位于同一服务的登记挂载中")
			}
		}
	}
	encoded, err := json.Marshal(policy)
	if err != nil || len(encoded) > 8192 {
		return errors.New("发布策略超过 8 KiB")
	}
	return nil
}

func validMountPath(value string) bool {
	return server.ValidRuntimeDirectory(value) && !strings.ContainsAny(value, ",\"")
}

var ErrRuntimePolicyConflict = errors.New("发布策略修订号已改变，请刷新后重新保存")

func (s *Store) RuntimePolicy(ctx context.Context, deploymentID string) (RuntimePolicy, error) {
	return readRuntimePolicy(s.db.QueryRowContext(ctx, `SELECT revision, policy_json FROM deployment_runtime_policies WHERE deployment_id=?`, deploymentID))
}

func (s *Store) ReleaseRuntimePolicy(ctx context.Context, releaseID string) (RuntimePolicy, error) {
	return readRuntimePolicy(s.db.QueryRowContext(ctx, `SELECT revision, policy_json FROM release_runtime_policies WHERE release_id=?`, releaseID))
}

func readRuntimePolicy(row scanner) (RuntimePolicy, error) {
	var revision int64
	var document string
	err := row.Scan(&revision, &document)
	if errors.Is(err, sql.ErrNoRows) {
		return RuntimePolicy{Mode: RuntimeObserve}, nil
	}
	if err != nil {
		return RuntimePolicy{}, err
	}
	var policy RuntimePolicy
	if err := json.Unmarshal([]byte(document), &policy); err != nil {
		return RuntimePolicy{}, fmt.Errorf("读取发布策略: %w", err)
	}
	policy.Revision = revision
	return policy, nil
}

func (s *Store) SaveRuntimePolicy(ctx context.Context, deploymentID string, expectedRevision int64, policy RuntimePolicy) (RuntimePolicy, error) {
	if expectedRevision < 0 || expectedRevision == int64(^uint64(0)>>1) {
		return RuntimePolicy{}, errors.New("无效的发布策略修订号")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RuntimePolicy{}, err
	}
	defer tx.Rollback()
	var kind, serverID string
	if err := tx.QueryRowContext(ctx, `SELECT workload_kind,server_id FROM deployment_targets WHERE id=?`, deploymentID).Scan(&kind, &serverID); err != nil {
		return RuntimePolicy{}, err
	}
	policy.Revision = expectedRevision + 1
	if err := policy.Validate(kind); err != nil {
		return RuntimePolicy{}, err
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE lock_key=? AND status IN ('queued','running','canceling')`, "server:"+serverID).Scan(&active); err != nil {
		return RuntimePolicy{}, err
	}
	if active != 0 {
		return RuntimePolicy{}, errors.New("服务器存在未完成任务，暂不能修改发布策略")
	}
	document, err := json.Marshal(policy)
	if err != nil {
		return RuntimePolicy{}, err
	}
	var result sql.Result
	if expectedRevision == 0 {
		result, err = tx.ExecContext(ctx, `INSERT INTO deployment_runtime_policies(deployment_id,revision,policy_json) VALUES(?,1,?) ON CONFLICT(deployment_id) DO NOTHING`, deploymentID, string(document))
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE deployment_runtime_policies SET revision=?,policy_json=? WHERE deployment_id=? AND revision=?`, policy.Revision, string(document), deploymentID, expectedRevision)
	}
	if err != nil {
		return RuntimePolicy{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return RuntimePolicy{}, err
	}
	if changed != 1 {
		return RuntimePolicy{}, ErrRuntimePolicyConflict
	}
	return policy, tx.Commit()
}

func snapshotRuntimePolicy(ctx context.Context, tx *sql.Tx, releaseID, deploymentID, sourceID string, legacyRollback bool) error {
	if sourceID != "" {
		result, err := tx.ExecContext(ctx, `INSERT INTO release_runtime_policies(release_id,revision,policy_json)
			SELECT ?,p.revision,p.policy_json FROM release_runtime_policies p JOIN deployment_releases r ON r.id=p.release_id
			WHERE r.id=? AND r.deployment_id=?`, releaseID, sourceID, deploymentID)
		if err != nil || !legacyRollback {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil || count != 0 {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO release_runtime_policies(release_id,revision,policy_json)
		SELECT ?,revision,policy_json FROM deployment_runtime_policies WHERE deployment_id=?`, releaseID, deploymentID)
	return err
}

func (s *Store) RecordRuntimeChecks(ctx context.Context, releaseID string, after bool, checks []RuntimeCheck) error {
	report, err := runtimeCheckSummary(checks)
	if err != nil {
		return err
	}
	statement := `UPDATE release_runtime_policies SET before_json=? WHERE release_id=?`
	if after {
		statement = `UPDATE release_runtime_policies SET after_json=? WHERE release_id=?`
	}
	_, err = s.db.ExecContext(ctx, statement, string(report), releaseID)
	return err
}

func runtimeCheckSummary(checks []RuntimeCheck) ([]byte, error) {
	// Keep the shared release record bounded and retain failed observations
	// before passing ones when a deployment has many replicas.
	ordered := make([]RuntimeCheck, 0, len(checks))
	for _, check := range checks {
		if check.Status != "ok" {
			ordered = append(ordered, check)
		}
	}
	for _, check := range checks {
		if check.Status == "ok" {
			ordered = append(ordered, check)
		}
	}
	for i := len(ordered); ; i /= 2 {
		summary := append([]RuntimeCheck{}, ordered[:i]...)
		if i != len(ordered) {
			summary = append(summary, RuntimeCheck{Code: "report_truncated", Status: "info"})
		}
		data, err := json.Marshal(summary)
		if err != nil || len(data) <= 24*1024 {
			return data, err
		}
	}
}

type ReleaseRuntimeVerification struct {
	ReleaseID      string         `json:"releaseId"`
	PolicyRevision int64          `json:"policyRevision"`
	Before         []RuntimeCheck `json:"before"`
	After          []RuntimeCheck `json:"after"`
}

func (s *Store) ReleaseRuntimeVerification(ctx context.Context, releaseID string) (ReleaseRuntimeVerification, error) {
	view := ReleaseRuntimeVerification{ReleaseID: releaseID, Before: []RuntimeCheck{}, After: []RuntimeCheck{}}
	var before, after string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(p.revision,0),COALESCE(p.before_json,'[]'),COALESCE(p.after_json,'[]')
		FROM deployment_releases r LEFT JOIN release_runtime_policies p ON p.release_id=r.id WHERE r.id=?`, releaseID).Scan(&view.PolicyRevision, &before, &after)
	if err != nil {
		return view, err
	}
	if err := json.Unmarshal([]byte(before), &view.Before); err != nil {
		return view, err
	}
	return view, json.Unmarshal([]byte(after), &view.After)
}
