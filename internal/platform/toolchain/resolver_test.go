package toolchain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveExecutableFromProcessPath(t *testing.T) {
	resolved, err := ResolveExecutable(context.Background(), "sh")
	if err != nil {
		t.Fatalf("resolve sh: %v", err)
	}
	if resolved.Path == "" {
		t.Fatal("expected an executable path")
	}
	if resolved.Source != SourceProcessPath {
		t.Fatalf("expected process PATH source, got %q", resolved.Source)
	}
}

func TestResolveExecutableRejectsShellSyntax(t *testing.T) {
	if _, err := ResolveExecutable(context.Background(), "docker;whoami"); err == nil {
		t.Fatal("expected unsafe executable name to be rejected")
	}
}

func TestResolveExecutableLoadsZshrcOnMacOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("zsh startup fallback is macOS-specific")
	}

	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("create fake bin directory: %v", err)
	}
	executablePath := filepath.Join(binDir, "ideploy-test-tool")
	if err := os.WriteFile(executablePath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("create fake executable: %v", err)
	}
	zshrc := fmt.Sprintf("export PATH=%q:$PATH\n", binDir)
	if err := os.WriteFile(filepath.Join(root, ".zshrc"), []byte(zshrc), 0o600); err != nil {
		t.Fatalf("create test .zshrc: %v", err)
	}

	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("ZDOTDIR", root)
	resolved, err := ResolveExecutable(context.Background(), "ideploy-test-tool")
	if err != nil {
		t.Fatalf("resolve executable from .zshrc: %v", err)
	}
	if resolved.Path != executablePath {
		t.Fatalf("expected %q, got %q", executablePath, resolved.Path)
	}
	if resolved.Source != SourceZshStartup {
		t.Fatalf("expected zsh startup source, got %q", resolved.Source)
	}
}

func TestParseMarkedPathIgnoresShellPluginOutput(t *testing.T) {
	output := "plugin warning\n__IDEPLOY_PATH__=/usr/local/bin/docker\n"
	if actual := parseMarkedPath(output); actual != "/usr/local/bin/docker" {
		t.Fatalf("unexpected parsed path %q", actual)
	}
}
