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

func TestGitCredentialRefClearsWhenRepositoryChanges(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "projects.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := project.NewStore(db)
	input := project.Input{Name: "Private", SourceKind: project.SourceGit, RepositoryURL: "https://example.com/first.git", RefKind: project.RefBranch, RefValue: "main"}
	created, err := store.Create(ctx, input, project.Inspection{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetCredentialRef(ctx, created.ID, "keychain:git-project:"+created.ID); err != nil {
		t.Fatal(err)
	}
	unchanged, err := store.Update(ctx, created.ID, input, project.Inspection{})
	if err != nil || unchanged.CredentialRef == "" {
		t.Fatalf("credential lost on same URL: %#v %v", unchanged, err)
	}
	input.RepositoryURL = "https://example.com/second.git"
	changed, err := store.Update(ctx, created.ID, input, project.Inspection{})
	if err != nil || changed.CredentialRef != "" {
		t.Fatalf("credential carried to another URL: %#v %v", changed, err)
	}
}
