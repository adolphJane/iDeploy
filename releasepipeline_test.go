package main

import (
	"errors"
	"ideploy/internal/registry"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildArgumentsSelectManagedBuilderAndLoadImage(t *testing.T) {
	proxy := buildProxyRuntime{BuilderName: "ideploy-proxy-test"}
	if got, want := composeBuildArguments("compose.yml", proxy), []string{"compose", "-f", "compose.yml", "build", "--pull", "--builder", "ideploy-proxy-test"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected Compose build arguments: %#v", got)
	}
	if got, want := containerBuildArguments("Dockerfile", "example/api:v1", ".", "linux/arm64", proxy), []string{"buildx", "build", "--builder", "ideploy-proxy-test", "--load", "--platform", "linux/arm64", "--pull", "-f", "Dockerfile", "-t", "example/api:v1", "."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected proxied container build arguments: %#v", got)
	}
}

func TestBuildArgumentsKeepDockerBuildWhenProxyDisabled(t *testing.T) {
	if got, want := containerBuildArguments("Dockerfile", "example/api:v1", ".", "linux/amd64", buildProxyRuntime{}), []string{"build", "--platform", "linux/amd64", "--pull", "-f", "Dockerfile", "-t", "example/api:v1", "."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected default container build arguments: %#v", got)
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
