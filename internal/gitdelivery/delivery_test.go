package gitdelivery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ideploy/internal/releaseplan"
)

func TestPushCreatesIsolatedReleaseRef(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	work := filepath.Join(root, "source")
	if _, err := git(ctx, "", nil, "init", "--bare", bare); err != nil {
		t.Fatal(err)
	}
	if _, err := git(ctx, "", nil, "clone", bare, work); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "Dockerfile"), []byte("FROM scratch\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := git(ctx, work, nil, "add", "Dockerfile"); err != nil {
		t.Fatal(err)
	}
	identity := []string{"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@localhost", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@localhost"}
	if _, err := git(ctx, work, identity, "commit", "-m", "source"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(ctx, work, nil, "push", "origin", "HEAD:refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	commit, err := git(ctx, work, nil, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	d := releaseplan.Document{SchemaVersion: 1, DeploymentID: strings.Repeat("a", 32), ReleaseID: strings.Repeat("b", 32), SourceCommit: commit, WorkloadKind: "container", ContextPath: ".", Dockerfile: "Dockerfile", ImagePath: "registry.example/team/app", Platform: "linux/amd64"}
	result, err := Push(ctx, bare, commit, d, filepath.Join(root, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceCommit != commit || result.DeliveryCommit == commit || result.Digest == "" {
		t.Fatalf("invalid result: %+v", result)
	}
	if _, err := git(ctx, work, nil, "fetch", "origin", result.DeliveryRef); err != nil {
		t.Fatal(err)
	}
	parent, err := git(ctx, work, nil, "rev-parse", "FETCH_HEAD^")
	if err != nil || parent != commit {
		t.Fatalf("release parent = %q: %v", parent, err)
	}
	if _, err := git(ctx, work, nil, "show", "FETCH_HEAD:"+result.DocumentPath); err != nil {
		t.Fatal(err)
	}
	if _, err := Push(ctx, bare, commit, d, filepath.Join(root, "cache")); err == nil {
		t.Fatal("duplicate release ref was overwritten")
	}
}
