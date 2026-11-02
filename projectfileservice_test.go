package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectFileEditDetectsExternalChanges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Dockerfile")
	if err := os.WriteFile(path, []byte("FROM alpine:3.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := readProjectFileAtRoot(root, "Dockerfile", "dockerfile")
	if err != nil || loaded.Content != "FROM alpine:3.21\n" {
		t.Fatalf("read file: %#v, %v", loaded, err)
	}
	if err := os.WriteFile(path, []byte("FROM alpine:3.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := saveProjectFileAtRoot(root, "Dockerfile", "dockerfile", "FROM alpine:3.23\n", loaded.Hash); err == nil || !strings.Contains(err.Error(), "其他程序修改") {
		t.Fatalf("expected conflict error, got %v", err)
	}
	current, err := readProjectFileAtRoot(root, "Dockerfile", "dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := saveProjectFileAtRoot(root, "Dockerfile", "dockerfile", "FROM alpine:3.23\n", current.Hash)
	if err != nil || updated.Content != "FROM alpine:3.23\n" || updated.Hash == current.Hash {
		t.Fatalf("save file: %#v, %v", updated, err)
	}
}

func TestProjectFileEditRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "Dockerfile")
	if err := os.WriteFile(outside, []byte("FROM alpine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "Dockerfile")); err != nil {
		t.Fatal(err)
	}
	if _, err := readProjectFileAtRoot(root, "Dockerfile", "dockerfile"); err == nil || !strings.Contains(err.Error(), "超出项目目录") {
		t.Fatalf("expected path error, got %v", err)
	}
}

func TestProjectFileEditRejectsInvalidDockerfileWithoutWriting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Dockerfile")
	if err := os.WriteFile(path, []byte("FROM alpine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := readProjectFileAtRoot(root, "Dockerfile", "dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saveProjectFileAtRoot(root, "Dockerfile", "dockerfile", "RUN echo hello\n", loaded.Hash); err == nil {
		t.Fatal("expected Dockerfile validation error")
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != loaded.Content {
		t.Fatalf("invalid edit changed original file: %q, %v", content, err)
	}
}

func TestProjectFileEditSavesComposeText(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "docker-compose.yml")
	if err := os.WriteFile(path, []byte("services:\n  app:\n    image: alpine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := readProjectFileAtRoot(root, "docker-compose.yml", "compose")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := saveProjectFileAtRoot(root, "docker-compose.yml", "compose", "services:\n  app:\n    image: nginx\n", loaded.Hash)
	if err != nil || !strings.Contains(updated.Content, "image: nginx") {
		t.Fatalf("save compose: %#v, %v", updated, err)
	}
}
