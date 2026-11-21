package main

import (
	"context"
	"errors"
	"ideploy/internal/project"
	"ideploy/internal/registry"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPrepareReleaseSourceResolvesConfiguredGitRef(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	git("init", "-b", "main")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.test")
	git("commit", "--allow-empty", "-m", "tagged")
	tagCommit := git("rev-parse", "HEAD")
	git("tag", "compose")
	git("switch", "-c", "compose")
	git("commit", "--allow-empty", "-m", "branch tip")
	branchCommit := git("rev-parse", "HEAD")
	git("switch", "main")
	for _, test := range []struct {
		kind project.RefKind
		ref  string
		want string
	}{
		{project.RefBranch, "compose", branchCommit},
		{project.RefBranch, "refs/heads/compose", branchCommit},
		{project.RefTag, "compose", tagCommit},
		{project.RefCommit, branchCommit, branchCommit},
	} {
		t.Run(string(test.kind)+"/"+test.ref, func(t *testing.T) {
			source, err := prepareReleaseSource(context.Background(), project.Project{
				SourceKind: project.SourceGit, RepositoryURL: root, RefKind: test.kind, RefValue: test.ref,
			}, t.TempDir(), discardJobReporter{})
			if err != nil {
				t.Fatal(err)
			}
			defer source.cleanup()
			if source.revision != test.want {
				t.Fatalf("got commit %s, want %s", source.revision, test.want)
			}
		})
	}
}

func TestBuildArgumentsSelectManagedBuilderAndLoadImage(t *testing.T) {
	proxy := buildProxyRuntime{BuilderName: "ideploy-proxy-test"}
	if got, want := composeBuildArguments("compose.yml", proxy), []string{"compose", "-f", "compose.yml", "build", "--pull", "--builder", "ideploy-proxy-test"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected Compose build arguments: %#v", got)
	}
	if got, want := containerBuildArguments("Dockerfile", "example/api:v1", ".", "linux/arm64", "/demo/", proxy), []string{"buildx", "build", "--builder", "ideploy-proxy-test", "--load", "--platform", "linux/arm64", "--pull", "--build-arg", "IDEPLOY_BASE_PATH=/demo/", "-f", "Dockerfile", "-t", "example/api:v1", "."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected proxied container build arguments: %#v", got)
	}
}

func TestBuildArgumentsKeepDockerBuildWhenProxyDisabled(t *testing.T) {
	if got, want := containerBuildArguments("Dockerfile", "example/api:v1", ".", "linux/amd64", "/draw/", buildProxyRuntime{}), []string{"build", "--platform", "linux/amd64", "--pull", "--build-arg", "IDEPLOY_BASE_PATH=/draw/", "-f", "Dockerfile", "-t", "example/api:v1", "."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected default container build arguments: %#v", got)
	}
}

func TestRuntimeTaggedImageReferenceKeepsRepositoryAndVersion(t *testing.T) {
	pinned := "harbor.example.com/library/api@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	got, err := runtimeTaggedImageReference(pinned, "v1.2.3")
	if err != nil || got != "harbor.example.com/library/api:v1.2.3" {
		t.Fatalf("unexpected runtime tag: %q err=%v", got, err)
	}
	for _, invalid := range []struct{ image, version string }{
		{"harbor.example.com/library/api:v1.2.3", "v1.2.3"},
		{pinned, "bad version"},
	} {
		if _, err := runtimeTaggedImageReference(invalid.image, invalid.version); err == nil {
			t.Fatalf("accepted invalid runtime tag input: %#v", invalid)
		}
	}
}

func TestSecureProjectFileRejectsEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := secureProjectFile(root, "../secret"); err == nil {
		t.Fatal("accepted a path outside the project root")
	}
	path, err := secureProjectFile(root, "deploy/compose.yml")
	if err != nil || path != filepath.Join(root, "deploy", "compose.yml") {
		t.Fatalf("unexpected safe path: %q %v", path, err)
	}
}

func TestExplainDockerBuildErrorIdentifiesMissingProxyBaseImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Dockerfile")
	if err := os.WriteFile(path, []byte("FROM harbor.example.com/proxy/node:22-alpine AS build\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := explainDockerBuildError(errors.New("failed to resolve source metadata for harbor.example.com/proxy/node:22-alpine: not found"), path)
	if !strings.Contains(err.Error(), "Harbor Proxy Cache") || !strings.Contains(err.Error(), "harbor.example.com/proxy/node:22-alpine") {
		t.Fatalf("unexpected build diagnosis: %v", err)
	}
}

func TestImageRepositoryMatchesSelectedRegistry(t *testing.T) {
	for _, test := range []struct {
		repository string
		endpoint   string
		want       bool
	}{
		{"harbor.example.com/team/api", "https://harbor.example.com", true},
		{"other.example.com/team/api", "https://harbor.example.com", false},
		{"team/api", "https://registry-1.docker.io", true},
		{"team/api", "https://harbor.example.com", false},
	} {
		if got := imageRepositoryMatchesRegistry(test.repository, test.endpoint); got != test.want {
			t.Fatalf("match %q with %q = %v, want %v", test.repository, test.endpoint, got, test.want)
		}
	}
}

func TestValidateHarborImageRepositoryRequiresProjectNamespace(t *testing.T) {
	connection := registry.Connection{Kind: registry.KindHarbor, Endpoint: "https://harbor.example.com"}
	if err := validateImageRepositoryForRegistry("harbor.example.com/api", connection); err == nil || !strings.Contains(err.Error(), "library/api") {
		t.Fatalf("expected missing Harbor project error, got %v", err)
	}
	if err := validateImageRepositoryForRegistry("harbor.example.com/library/api", connection); err != nil {
		t.Fatalf("valid Harbor repository rejected: %v", err)
	}
}

func TestValidateRegistryPushCredentials(t *testing.T) {
	if err := validateRegistryPushCredentials("", ""); err == nil || !strings.Contains(err.Error(), "推送凭据") {
		t.Fatalf("expected missing credential error, got %v", err)
	}
	if err := validateRegistryPushCredentials("robot$ideploy", "token"); err != nil {
		t.Fatalf("valid push credentials rejected: %v", err)
	}
}
