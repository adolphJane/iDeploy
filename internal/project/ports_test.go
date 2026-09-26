package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveContainerPortsFromExpose(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM alpine\nEXPOSE 8080/tcp\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ports, err := ResolveContainerPorts(root, ".", "Dockerfile", []string{"80"})
	if err != nil || len(ports) != 1 || ports[0] != "80:8080/tcp" {
		t.Fatalf("unexpected resolved ports: %#v, %v", ports, err)
	}
}

func TestResolveContainerPortsRequiresUniqueMatch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM alpine\nEXPOSE 8080 9090\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveContainerPorts(root, ".", "Dockerfile", []string{"80"}); err == nil {
		t.Fatal("expected ambiguous port mapping to fail")
	}
}
