package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"ideploy/internal/server"
	"time"
)

// Match requires an explicitly selected workload; an image is not an identity.
func Match(target Target, containers []server.Container) []server.Container {
	result := make([]server.Container, 0)
	if target.WorkloadName == "" {
		return result
	}
	for _, c := range containers {
		if target.WorkloadKind == "container" && c.Name == target.WorkloadName || target.WorkloadKind == "compose" && c.ComposeProject == target.WorkloadName && !c.OneOff {
			result = append(result, c)
		}
	}
	return result
}

func (s *Store) Bind(ctx context.Context, id, kind, name string) error {
	target, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	input, err := (Input{ProjectID: target.ProjectID, ServerID: target.ServerID, WorkloadKind: kind, WorkloadName: name}).Validate()
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, "UPDATE deployment_targets SET workload_kind=?, workload_name=?, updated_at=? WHERE id=? AND NOT EXISTS (SELECT 1 FROM jobs WHERE lock_key='server:' || deployment_targets.server_id AND status IN ('queued','running','canceling'))", input.WorkloadKind, input.WorkloadName, formatTime(time.Now()), id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return errors.New("服务器存在未完成任务，暂不能修改绑定")
	}
	return nil
}

func (s *Store) SetDesired(ctx context.Context, id, state string) error {
	if state != "running" && state != "stopped" {
		return errors.New("invalid desired state")
	}
	result, err := s.db.ExecContext(ctx, "UPDATE deployment_targets SET desired_state=?, updated_at=? WHERE id=?", state, formatTime(time.Now()), id)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) SaveSnapshot(ctx context.Context, serverID string, containers []server.Container) error {
	data, err := json.Marshal(containers)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO remote_snapshots(server_id,containers_json,observed_at) VALUES(?,?,?) ON CONFLICT(server_id) DO UPDATE SET containers_json=excluded.containers_json, observed_at=excluded.observed_at`, serverID, string(data), formatTime(time.Now()))
	return err
}

func (s *Store) Snapshot(ctx context.Context, serverID string) ([]server.Container, string, error) {
	var data, observed string
	err := s.db.QueryRowContext(ctx, "SELECT containers_json,observed_at FROM remote_snapshots WHERE server_id=?", serverID).Scan(&data, &observed)
	if err != nil {
		return nil, "", err
	}
	var containers []server.Container
	err = json.Unmarshal([]byte(data), &containers)
	return containers, observed, err
}

func (s *Store) ClearSnapshots(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, "DELETE FROM remote_snapshots")
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
