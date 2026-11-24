package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"ideploy/internal/deployment"
	"ideploy/internal/job"
)

func TestComposePolicyResolvesNamedVolumesAndRejectsAnUnregisteredMount(t *testing.T) {
	target := deployment.Target{WorkloadKind: "compose", WorkloadName: "app"}
	policy := deployment.RuntimePolicy{
		Mode: deployment.RuntimeEnforce, Services: []string{"api"},
		Mounts: []deployment.RuntimeMount{{Service: "api", Type: "volume", Source: "app_data", Destination: "/data"}},
	}
	manifest := `{"services":{"api":{"volumes":[{"type":"volume","source":"data","target":"/data"}]}},"volumes":{"data":{"name":"app_data"}}}`
	checks, err := validateComposeRuntimePlan(target, manifest, policy)
	if err != nil || runtimeCheckFailure(checks) != nil {
		t.Fatalf("approved Compose plan: %v %v", checks, err)
	}
	for _, changed := range []string{
		strings.Replace(manifest, "app_data", "new_storage", 1),
		strings.Replace(manifest, `"target":"/data"`, `"target":"/data","read_only":true`, 1),
		strings.Replace(manifest, `"target":"/data"`, `"target":"/data","volume":{"subpath":"other"}`, 1),
		strings.Replace(manifest, `"api"`, `"worker"`, 1),
	} {
		checks, err := validateComposeRuntimePlan(target, changed, policy)
		if err != nil || runtimeCheckFailure(checks) == nil {
			t.Fatalf("changed Compose plan should be blocked: %v %v", checks, err)
		}
	}
}

func TestPolicyRevisionAndInterruptedPublishKeepTheOriginalJob(t *testing.T) {
	ctx := context.Background()
	service, store, target := newComposeTestStore(t, t.TempDir())
	service.store = job.NewStore(service.db)
	policy := deployment.RuntimePolicy{Mode: deployment.RuntimeEnforce, Services: []string{"api"}}
	saved, err := store.SaveRuntimePolicy(ctx, target.ID, 0, policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveRuntimePolicy(ctx, target.ID, 0, policy); !errors.Is(err, deployment.ErrRuntimePolicyConflict) {
		t.Fatalf("stale policy edit: %v", err)
	}
	zero := int64(0)
	if _, err := store.CreateRelease(ctx, deployment.ReleaseInput{
		DeploymentID: target.ID, Version: "1.0.0", ExpectedPolicyRevision: &zero,
	}); !errors.Is(err, deployment.ErrRuntimePolicyConflict) {
		t.Fatalf("publish accepted mismatched preflight revision: %v", err)
	}
	source, err := store.CreateRelease(ctx, deployment.ReleaseInput{
		DeploymentID: target.ID, Version: "1.0.0", ExpectedPolicyRevision: &saved.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	policy.Services = []string{"replacement"}
	if _, err := store.SaveRuntimePolicy(ctx, target.ID, saved.Revision, policy); err != nil {
		t.Fatal(err)
	}
	frozen, err := store.ReleaseRuntimePolicy(ctx, source.ID)
	if err != nil || frozen.Revision != saved.Revision || len(frozen.Services) != 1 || frozen.Services[0] != "api" {
		t.Fatalf("release lost its original policy: %+v %v", frozen, err)
	}
	if _, err := publicationRuntimePolicy(ctx, store, target, source); err == nil {
		t.Fatal("enforced release continued under changed policy")
	}

	request := cliRequest{Command: "deployment.publish", RequestID: "publish-request-1", ID: target.ID}
	hash, err := controlRequestHash(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := service.claimControlOperation(ctx, request.RequestID, request.Command, hash); err != nil || !claimed {
		t.Fatalf("reserve publish request: %v %v", claimed, err)
	}
	payload, _ := json.Marshal(map[string]string{"releaseId": source.ID})
	queued, err := service.store.Enqueue(ctx, job.NewJob{
		Type: "deployment.release", Payload: string(payload), LockKey: "server:" + target.ServerID,
		ReleaseID: source.ID, ControlRequestID: request.RequestID,
	})
	if err != nil {
		t.Fatal(err)
	}
	attached, err := store.GetRelease(ctx, source.ID)
	if err != nil || attached.JobID != queued.ID {
		t.Fatalf("job was visible without a linked release: %#v %v", attached, err)
	}
	if err := recoverInterruptedControlOperations(ctx, service.db); err != nil {
		t.Fatal(err)
	}
	result := executeIdempotentCLIRequest(ctx, service, request)
	var returned JobView
	if !result.OK || json.Unmarshal(result.Data, &returned) != nil || returned.ID != queued.ID {
		t.Fatalf("a reconnect did not return the existing job: %#v", result)
	}
	if err := service.store.RequestCancel(ctx, queued.ID); err != nil {
		t.Fatal(err)
	}
	retry, err := store.CreateRelease(ctx, deployment.ReleaseInput{
		DeploymentID: target.ID, Version: source.Version, AllowRetry: true, PolicySourceID: source.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	retryPolicy, err := store.ReleaseRuntimePolicy(ctx, retry.ID)
	if err != nil || retryPolicy.Revision != frozen.Revision {
		t.Fatalf("cancel and retry lost frozen verification policy: %#v %v", retryPolicy, err)
	}
}

func TestLegacyRollbackUsesCurrentStorageRules(t *testing.T) {
	ctx := context.Background()
	_, store, target := newComposeTestStore(t, t.TempDir())
	legacy, err := store.CreateRelease(ctx, deployment.ReleaseInput{DeploymentID: target.ID, Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.SaveRuntimePolicy(ctx, target.ID, 0, deployment.RuntimePolicy{
		Mode: deployment.RuntimeEnforce, Services: []string{"api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	rollback, err := store.CreateRelease(ctx, deployment.ReleaseInput{
		DeploymentID: target.ID, Version: legacy.Version, Action: "rollback", RollbackToID: legacy.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := publicationRuntimePolicy(ctx, store, target, rollback)
	if err != nil || policy.Revision != saved.Revision || policy.Mode != deployment.RuntimeEnforce {
		t.Fatalf("legacy rollback skipped current data protection: %#v %v", policy, err)
	}
}

func TestVerifiedVersionRequiresEveryExpectedService(t *testing.T) {
	references := map[string]string{"api": "api@sha", "queue": "queue@sha"}
	checks := []deployment.RuntimeCheck{{Code: "image_digest", Service: "api", Status: "ok"}}
	if allReleaseImagesVerified(checks, references, nil) || allReleaseImagesVerified(checks, references, []string{"api", "queue"}) {
		t.Fatal("a partial Compose deployment was reported as a verified release")
	}
	checks = append(checks, deployment.RuntimeCheck{Code: "image_digest", Service: "queue", Status: "ok"})
	if !allReleaseImagesVerified(checks, references, nil) {
		t.Fatal("all published images should verify the version")
	}
}
