package project

import "testing"

func TestInputValidate(t *testing.T) {
	value, err := (Input{Name: "  API  ", SourceKind: SourceLocal, LocalPath: "/tmp/api", ContextPath: "./services/api", DockerfilePath: "Dockerfile"}).Validate()
	if err != nil || value.Name != "API" || value.ContextPath != "services/api" {
		t.Fatalf("unexpected valid input: %#v, %v", value, err)
	}
	if _, err := (Input{Name: "bad", SourceKind: SourceLocal, LocalPath: "relative"}).Validate(); err == nil {
		t.Fatal("expected relative local path to fail")
	}
	if _, err := (Input{Name: "bad", SourceKind: SourceGit, RepositoryURL: "https://example.com/api.git", RefKind: RefBranch, RefValue: "main", DockerfilePath: "../Dockerfile"}).Validate(); err == nil {
		t.Fatal("expected escaping Dockerfile path to fail")
	}
	if _, err := (Input{Name: "bad", SourceKind: SourceGit, RepositoryURL: "http://example.com/api.git", RefKind: RefBranch, RefValue: "main"}).Validate(); err == nil {
		t.Fatal("expected insecure Git URL to fail")
	}
	if _, err := (Input{Name: "bad", SourceKind: SourceGit, RepositoryURL: "https://token@example.com/api.git", RefKind: RefBranch, RefValue: "main"}).Validate(); err == nil {
		t.Fatal("expected credential-bearing Git URL to fail")
	}
}
