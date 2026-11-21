package buildexecutor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ideploy/internal/gitdelivery"
	"ideploy/internal/releaseplan"
)

func TestPrepareVerifiesPushedCommitAndDigest(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	work := filepath.Join(base, "work")
	if err := runGit(ctx, "", "init", "--bare", origin); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, "", "clone", origin, work); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "Dockerfile"), []byte("FROM scratch\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, work, "add", "Dockerfile"); err != nil {
		t.Fatal(err)
	}
	cmd := func(args ...string) error {
		command := exec.CommandContext(ctx, "git", args...)
		command.Dir = work
		command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@localhost", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@localhost")
		return command.Run()
	}
	if err := cmd("commit", "-m", "source"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, work, "push", "origin", "HEAD:refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	source, err := gitOutput(ctx, work, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	id, release := strings.Repeat("a", 32), strings.Repeat("b", 32)
	doc := releaseplan.Document{SchemaVersion: 1, DeploymentID: id, ReleaseID: release, SourceCommit: source, WorkloadKind: "container", ContextPath: ".", Dockerfile: "Dockerfile", ImagePath: "registry.example/team/app", Platform: "linux/amd64"}
	delivered, err := gitdelivery.Push(ctx, origin, source, doc, filepath.Join(base, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	input := CheckoutInput{RepositoryURL: origin, DeliveryCommit: delivered.DeliveryCommit, DeliveryDigest: delivered.Digest, DeploymentID: id, ReleaseID: release, WorkRoot: filepath.Join(base, "builds")}
	checked, err := Prepare(ctx, input)
	if err != nil || checked.Document.SourceCommit != source {
		t.Fatalf("prepare: %+v %v", checked, err)
	}
	input.ReleaseID = strings.Repeat("c", 32)
	if _, err := Prepare(ctx, input); err == nil {
		t.Fatal("accepted wrong release identifier")
	}
}
