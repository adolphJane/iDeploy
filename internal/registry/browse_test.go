package registry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBrowserListsHarborProjects(t *testing.T) {
	browser := &Browser{client: &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/v2.0/projects":
			return jsonResponse(`[{"name":"team","repo_count":1}]`), nil
		default:
			t.Fatalf("unexpected path %q", request.URL.Path)
			return nil, nil
		}
	})}}

	projects, err := browser.ListProjects(context.Background(), Connection{Kind: KindHarbor, Endpoint: "https://harbor.example.com"})
	if err != nil {
		t.Fatalf("list Harbor projects: %v", err)
	}
	if len(projects) != 1 || projects[0].Name != "team" || projects[0].RepositoryCount != 1 {
		t.Fatalf("unexpected projects: %#v", projects)
	}
}

func TestBrowserListsHarborRepositoriesInProject(t *testing.T) {
	browser := &Browser{client: &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/v2.0/projects/team/repositories" {
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
		return jsonResponse(`[{"name":"team/api","artifact_count":3,"pull_count":9,"update_time":"2026-09-04T00:00:00Z"}]`), nil
	})}}

	repositories, err := browser.ListRepositories(context.Background(), Connection{Kind: KindHarbor, Endpoint: "https://harbor.example.com"}, "team")
	if err != nil {
		t.Fatalf("list Harbor repositories: %v", err)
	}
	if len(repositories) != 1 || repositories[0].Name != "team/api" || repositories[0].ArtifactCount != 3 {
		t.Fatalf("unexpected repositories: %#v", repositories)
	}
}

func TestBrowserListsHarborArtifacts(t *testing.T) {
	browser := &Browser{client: &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/v2.0/projects/team/repositories/api/artifacts" {
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
		return jsonResponse(`[{"digest":"sha256:abc","size":1048576,"push_time":"2026-09-04T00:00:00Z","tags":[{"name":"v1"},{"name":"latest"}]}]`), nil
	})}}

	artifacts, err := browser.ListArtifacts(context.Background(), Connection{Kind: KindHarbor, Endpoint: "https://harbor.example.com"}, "team/api")
	if err != nil {
		t.Fatalf("list Harbor artifacts: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].Digest != "sha256:abc" || strings.Join(artifacts[0].Tags, ",") != "latest,v1" {
		t.Fatalf("unexpected artifacts: %#v", artifacts)
	}
}

func TestBrowserOCIAuthRequired(t *testing.T) {
	browser := &Browser{client: &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: http.NoBody}, nil
	})}}

	_, err := browser.ListRepositories(context.Background(), Connection{Kind: KindOCI, Endpoint: "https://registry.example.com"}, "")
	var browseError *BrowseError
	if !errors.As(err, &browseError) || browseError.Status != StatusAuthRequired {
		t.Fatalf("expected authentication error, got %v", err)
	}
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
