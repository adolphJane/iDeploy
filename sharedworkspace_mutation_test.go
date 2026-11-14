package main

import (
	"encoding/json"
	"testing"

	"ideploy/internal/sharedworkspace"
)

func TestSharedConflictAppliedRequiresConfirmedRemoteRevisionAndContent(t *testing.T) {
	data := json.RawMessage(`{"id":"db","name":"Local"}`)
	batch := pendingSharedBatch{Resources: []sharedworkspace.ResourceMutation{{Kind: "r.databases", ID: "db", ExpectedRevision: 2, Data: data}}}
	remote := map[string]map[string]sharedworkspace.RemoteResource{
		"r.databases": {"db": {ID: "db", Revision: 2, Data: data}},
	}
	if applied, err := sharedConflictApplied(batch, remote); err != nil || applied {
		t.Fatalf("current revision must not discard unsent local data: applied=%v err=%v", applied, err)
	}
	remote["r.databases"]["db"] = sharedworkspace.RemoteResource{ID: "db", Revision: 3, Data: json.RawMessage(`{"id":"db","name":"Other"}`)}
	if applied, err := sharedConflictApplied(batch, remote); err != nil || applied {
		t.Fatalf("different remote data must remain a conflict: applied=%v err=%v", applied, err)
	}
	remote["r.databases"]["db"] = sharedworkspace.RemoteResource{ID: "db", Revision: 3, Data: data}
	if applied, err := sharedConflictApplied(batch, remote); err != nil || !applied {
		t.Fatalf("confirmed matching remote write can be cleared: applied=%v err=%v", applied, err)
	}
}

func TestSharedConflictAppliedAcceptsNewerJobSummary(t *testing.T) {
	batch := pendingSharedBatch{Resources: []sharedworkspace.ResourceMutation{{Kind: "r.job_summaries", ID: "job", ExpectedRevision: 0, Data: json.RawMessage(`{"id":"job","type":"deploy","lock_key":"server","progress":30,"updated_at":"2026-01-01T01:00:00Z"}`)}}}
	remote := map[string]map[string]sharedworkspace.RemoteResource{"r.job_summaries": {"job": {ID: "job", Revision: 1, Data: json.RawMessage(`{"id":"job","type":"deploy","lock_key":"server","progress":100,"updated_at":"2026-01-01T01:01:00Z"}`)}}}
	if applied, err := sharedConflictApplied(batch, remote); err != nil || !applied {
		t.Fatalf("newer completed job should supersede earlier summary: %v %v", applied, err)
	}
}
