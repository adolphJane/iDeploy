package project_test

import (
	"context"
	"path/filepath"
	"testing"

	"ideploy/internal/platform/database"
	"ideploy/internal/project"
)

func TestStoreCreatesUpdatesAndSoftDeletesProject(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "projects.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	store := project.NewStore(db)
	created, err := store.Create(ctx, project.Input{Name: "API", SourceKind: project.SourceLocal, LocalPath: "/tmp/api"}, project.Inspection{Status: project.StatusNeedsConfiguration, Detail: "Dockerfile not found"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if created.Status != project.StatusNeedsConfiguration {
		t.Fatalf("unexpected project: %#v", created)
	}
	updated, err := store.Update(ctx, created.ID, project.Input{Name: "API Service", SourceKind: project.SourceLocal, LocalPath: "/tmp/api", DockerfilePath: "Dockerfile"}, project.Inspection{Status: project.StatusReady, Detail: "Dockerfile found"})
	if err != nil || updated.Name != "API Service" || updated.DockerfilePath != "Dockerfile" {
		t.Fatalf("update project: %#v, %v", updated, err)
	}
	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	values, err := store.List(ctx)
	if err != nil || len(values) != 0 {
		t.Fatalf("list soft-deleted projects: %#v, %v", values, err)
	}
}
