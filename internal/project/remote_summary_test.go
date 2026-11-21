package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolveGitSummaryUsesWorktreeBranchUpstream(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"symbolic-ref", "HEAD", "refs/heads/local"},
		{"remote", "add", "origin", "https://example.invalid/origin.git"},
		{"remote", "add", "team", "https://example.invalid/team.git"},
		{"config", "branch.local.remote", "team"},
		{"config", "branch.local.merge", "refs/heads/main"},
	} {
		if output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	subdir := filepath.Join(root, "app")
	if err := os.Mkdir(subdir, 0700); err != nil {
		t.Fatal(err)
	}
	summary, err := ResolveGitSummarySource(context.Background(), Project{SourceKind: SourceLocal, LocalPath: subdir})
	if err != nil || summary.RepositoryURL != "https://example.invalid/team.git" || summary.Ref != "refs/heads/main" {
		t.Fatalf("unexpected upstream: %+v, %v", summary, err)
	}
}
