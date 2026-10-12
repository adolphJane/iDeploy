package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	appPaths "ideploy/internal/platform/paths"
)

func TestNativeControlBridge(t *testing.T) {
	root := t.TempDir()
	paths := appPaths.AppPaths{
		DataDir:      root,
		CacheDir:     filepath.Join(root, "cache"),
		LogDir:       filepath.Join(root, "logs"),
		DatabasePath: filepath.Join(root, "ideploy.db"),
	}
	if err := appPaths.Ensure(paths); err != nil {
		t.Fatal(err)
	}
	service := NewSystemService()
	if err := service.startWithPaths(context.Background(), true, paths); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.close() })
	socket := filepath.Join(root, "control.sock")

	response, err := callCLIControl(context.Background(), socket, cliRequest{
		Command: "native.call", NativeMethod: "ListProjects",
	})
	if err != nil || !response.OK || string(response.Data) != "[]" {
		t.Fatalf("ListProjects: response=%+v err=%v", response, err)
	}
	response, err = callCLIControl(context.Background(), socket, cliRequest{
		Command: "native.call", NativeMethod: "CreateProject",
		NativeArgs: []json.RawMessage{json.RawMessage(`{"name":42}`)},
	})
	if err != nil || response.OK || response.Code != "invalid_request" {
		t.Fatalf("invalid argument: response=%+v err=%v", response, err)
	}
	response, err = callCLIControl(context.Background(), socket, cliRequest{
		Command: "native.call", NativeMethod: "ServiceShutdown",
	})
	if err != nil || response.OK || response.Code != "unknown_method" {
		t.Fatalf("method whitelist: response=%+v err=%v", response, err)
	}
}
