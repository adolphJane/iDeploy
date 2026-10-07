package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInspectGitLocalChangesAndUpstream(t *testing.T) {
	root := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	runGit("init", "-q")
	runGit("config", "user.name", "Test")
	runGit("config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit("add", "tracked.txt")
	runGit("commit", "-qm", "initial")
	runGit("branch", "-M", "main")
	runGit("branch", "upstream")
	runGit("branch", "--set-upstream-to=upstream", "main")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	status := InspectGit(context.Background(), root, false)
	if !status.Available || status.Branch != "main" || status.ChangedFiles != 1 || status.Untracked != 1 || status.Ahead != 0 || status.Behind != 0 || status.RemoteFresh {
		t.Fatalf("unexpected status: %+v", status)
	}
	runGit("add", "tracked.txt")
	runGit("commit", "-qm", "second")
	status = InspectGit(context.Background(), root, false)
	if status.Ahead != 1 || status.Behind != 0 || status.Untracked != 1 {
		t.Fatalf("unexpected divergence: %+v", status)
	}
	runGit("switch", "-q", "upstream")
	if err := os.WriteFile(filepath.Join(root, "remote.txt"), []byte("remote"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit("add", "remote.txt")
	runGit("commit", "-qm", "upstream change")
	runGit("switch", "-q", "main")
	status = InspectGit(context.Background(), root, false)
	if status.Ahead != 1 || status.Behind != 1 {
		t.Fatalf("expected diverged branches: %+v", status)
	}
}

func TestInspectGitNotARepository(t *testing.T) {
	status := InspectGit(context.Background(), t.TempDir(), false)
	if status.Available || status.Detail == "" {
		t.Fatalf("unexpected status: %+v", status)
	}
}
