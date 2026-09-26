package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateDockerfileForNode(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"start":"node server.js"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	suggestion, err := GenerateDockerfile(root, ".")
	if err != nil || suggestion.Technology != "Node.js" || suggestion.Source != "package.json" {
		t.Fatalf("unexpected suggestion: %#v, %v", suggestion, err)
	}
	if suggestion.Content == "" {
		t.Fatal("expected Dockerfile content")
	}
}

func TestWriteGeneratedDockerfileDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteGeneratedDockerfile(root, ".", "FROM alpine", false); err == nil {
		t.Fatal("expected existing Dockerfile to be protected")
	}
	if _, err := WriteGeneratedDockerfile(root, ".", "FROM alpine", true); err != nil {
		t.Fatalf("overwrite confirmed Dockerfile: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "Dockerfile"))
	if err != nil || string(content) != "FROM alpine\n" {
		t.Fatalf("confirmed Dockerfile overwrite was not persisted: %q, %v", content, err)
	}
}
