package main

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"ideploy/internal/job"
)

const remoteJobSummaryMessage = "由其他设备发起；详细日志保留在发起设备"

func (s *SystemService) sharedJobSummary(ctx context.Context, id string) (JobView, error) {
	var item JobView
	err := s.db.QueryRowContext(ctx, `SELECT id,type,lock_key,status,progress,created_at,updated_at FROM shared_job_summaries WHERE id=?`, id).
		Scan(&item.ID, &item.Type, &item.LockKey, &item.Status, &item.Progress, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return JobView{}, job.ErrNotFound
	}
	item.Message = remoteJobSummaryMessage
	return item, err
}

func (s *SystemService) mergeSharedJobSummaries(ctx context.Context, local []JobView) ([]JobView, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,type,lock_key,status,progress,created_at,updated_at FROM shared_job_summaries ORDER BY updated_at DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := append([]JobView{}, local...)
	seen := map[string]bool{}
	for _, item := range local {
		seen[item.ID] = true
	}
	for rows.Next() {
		var item JobView
		if err := rows.Scan(&item.ID, &item.Type, &item.LockKey, &item.Status, &item.Progress, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if seen[item.ID] {
			continue
		}
		item.Message = remoteJobSummaryMessage
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].UpdatedAt > result[j].UpdatedAt })
	if len(result) > 20 {
		result = result[:20]
	}
	return result, nil
}
