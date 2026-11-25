package main

import (
	"context"
	"encoding/json"

	"ideploy/internal/job"
)

func (s *SystemService) describeJobTarget(ctx context.Context, task job.Job, view *JobView) {
	switch task.Type {
	case "database.instance":
		var input managedDatabaseOperation
		if json.Unmarshal([]byte(task.Payload), &input) == nil {
			if store, err := s.databaseStore(); err == nil {
				if target, err := store.GetInstance(ctx, input.InstanceID); err == nil {
					view.TargetName, view.Environment = target.Name, target.Environment
				}
			}
		}
	case "deployment.operation", "deployment.release", "deployment.rollback", "deployment.resume":
		var input struct {
			ID        string `json:"id"`
			ReleaseID string `json:"releaseId"`
		}
		if json.Unmarshal([]byte(task.Payload), &input) != nil {
			return
		}
		db, err := s.systemDatabase()
		if err != nil {
			return
		}
		targetID := input.ID
		if input.ReleaseID != "" {
			if err := db.QueryRowContext(ctx, "SELECT deployment_id FROM deployment_releases WHERE id=?", input.ReleaseID).Scan(&targetID); err != nil {
				return
			}
		}
		_ = db.QueryRowContext(ctx, `SELECT p.name,d.environment FROM deployment_targets d
			JOIN projects p ON p.id=d.project_id WHERE d.id=?`, targetID).Scan(&view.TargetName, &view.Environment)
	}
}
