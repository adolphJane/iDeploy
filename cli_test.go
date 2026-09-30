package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"ideploy/internal/platform/instance"
	appPaths "ideploy/internal/platform/paths"
)

func TestLowercaseExecutableSelectsCLIWithoutArguments(t *testing.T) {
	original := os.Args
	os.Args = []string{"/usr/local/bin/ideploy"}
	t.Cleanup(func() { os.Args = original })
	if !shouldRunCLI(nil) {
		t.Fatal("expected lowercase ideploy executable to select CLI mode")
	}
}

func TestParseCLIPublishArguments(t *testing.T) {
	request, options, special, err := parseCLIArguments([]string{
		"deployment", "publish", "deployment-1", "--registry", "registry-1",
		"--version", "v1.2.3", "--json", "--timeout", "45s",
	})
	if err != nil {
		t.Fatal(err)
	}
	if special != "" || request.Command != "deployment.publish" || request.ID != "deployment-1" || request.RegistryID != "registry-1" || request.Version != "v1.2.3" {
		t.Fatalf("unexpected request: %#v, special=%q", request, special)
	}
	if !options.JSON || !options.Wait || options.Timeout != 45*time.Second {
		t.Fatalf("unexpected options: %#v", options)
	}
}

func TestParseCLINoWaitAfterSubcommand(t *testing.T) {
	request, options, _, err := parseCLIArguments([]string{
		"deployment", "restart", "deployment-1", "--no-wait", "--json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.Wait || options.Wait {
		t.Fatalf("expected no-wait request, got request=%#v options=%#v", request, options)
	}
}

func TestRunCLIVersionJSON(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := runCLI([]string{"--version", "--json"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("exit code %d, stderr=%s", exitCode, stderr.String())
	}
	var response cliResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v (%s)", err, stdout.String())
	}
	if !response.OK || len(response.Data) == 0 {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestCLIControlRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix socket transport is not enabled on Windows")
	}
	path := filepath.Join(t.TempDir(), "control.sock")
	server, err := startCLIControlServer(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, err := callCLIControl(ctx, path, cliRequest{Command: "version"})
	if err != nil {
		t.Fatal(err)
	}
	if !response.OK {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestDesktopRuntimeOwnsQueueAndServesCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix socket transport is not enabled on Windows")
	}
	root, err := os.MkdirTemp("/tmp", "idp-runtime-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	paths := appPaths.AppPaths{
		DataDir:      root,
		CacheDir:     filepath.Join(root, "cache"),
		LogDir:       filepath.Join(root, "logs"),
		DatabasePath: filepath.Join(root, "ideploy.db"),
	}
	if err := appPaths.Ensure(paths); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := NewSystemService()
	if err := owner.startWithPaths(ctx, true, paths); err != nil {
		t.Fatal(err)
	}
	defer owner.close()

	second := NewSystemService()
	if err := second.startWithPaths(ctx, false, paths); !errors.Is(err, instance.ErrLocked) {
		t.Fatalf("expected second runtime to be locked out, got %v", err)
	}
	response, err := callCLIControl(ctx, filepath.Join(root, "control.sock"), cliRequest{Command: "version"})
	if err != nil {
		t.Fatal(err)
	}
	if !response.OK {
		t.Fatalf("unexpected response: %#v", response)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if err := second.startWithPaths(ctx, false, paths); err != nil {
		t.Fatalf("runtime lock was not released: %v", err)
	}
	if err := second.close(); err != nil {
		t.Fatal(err)
	}
}

func TestCLIUsageErrorIsMachineReadable(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := runCLI([]string{"deployment", "publish", "deployment-1", "--json"}, &stdout, &stderr); exitCode != 2 {
		t.Fatalf("exit code %d", exitCode)
	}
	var response cliResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.OK || response.Code != "usage" || stderr.Len() != 0 {
		t.Fatalf("unexpected response=%#v stderr=%q", response, stderr.String())
	}
}
