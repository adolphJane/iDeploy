package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectLocalFindsDockerConfiguration(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "service"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "service", "Dockerfile"), []byte("FROM scratch"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "compose.yml"), []byte("services: {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := InspectLocal(root)
	if err != nil {
		t.Fatalf("inspect local project: %v", err)
	}
	if result.Status != StatusReady || result.DockerfilePath != "service/Dockerfile" || result.ComposePath != "compose.yml" {
		t.Fatalf("unexpected inspection: %#v", result)
	}
}
