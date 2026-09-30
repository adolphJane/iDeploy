package main

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPCommandSelection(t *testing.T) {
	if !shouldRunMCP([]string{"mcp"}) {
		t.Fatal("expected mcp subcommand to select MCP mode")
	}
	if !shouldRunMCP([]string{"mcp", "--unused"}) {
		t.Fatal("expected mcp subcommand with trailing arguments to select MCP mode")
	}
	if shouldRunMCP([]string{"deployment", "list"}) {
		t.Fatal("did not expect a CLI command to select MCP mode")
	}
}

func TestMCPListsExpectedTools(t *testing.T) {
	client, closeSessions := connectMCPTestClient(t, func(_ context.Context, request cliRequest) cliResponse {
		return cliResponseFromValue(map[string]any{"command": request.Command})
	})
	defer closeSessions()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := client.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]bool{
		"get_version": false, "doctor": false, "list_projects": false,
		"list_servers": false, "list_registries": false, "list_deployments": false,
		"get_deployment": false, "list_releases": false, "list_jobs": false,
		"get_job": false, "wait_job": false, "publish_deployment": false,
		"rollback_deployment": false, "start_deployment": false,
		"stop_deployment": false, "restart_deployment": false, "cancel_job": false,
	}
	for _, tool := range result.Tools {
		if _, ok := expected[tool.Name]; ok {
			expected[tool.Name] = true
		}
		if tool.Name == "list_deployments" && (tool.Annotations == nil || !tool.Annotations.ReadOnlyHint) {
			t.Fatal("list_deployments must be annotated read-only")
		}
		if tool.Name == "publish_deployment" {
			if tool.Annotations == nil || tool.Annotations.ReadOnlyHint || tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
				t.Fatal("publish_deployment must be annotated as a destructive mutation")
			}
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("MCP tool %q was not registered", name)
		}
	}
}

func TestMCPPublishMapsStructuredArguments(t *testing.T) {
	var mu sync.Mutex
	var captured cliRequest
	client, closeSessions := connectMCPTestClient(t, func(_ context.Context, request cliRequest) cliResponse {
		mu.Lock()
		captured = request
		mu.Unlock()
		return cliResponseFromValue(map[string]any{"id": "job-1", "status": "succeeded"})
	})
	defer closeSessions()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{
		Name: "publish_deployment",
		Arguments: map[string]any{
			"deployment_id": "deployment-1",
			"registry_id":   "registry-1",
			"version":       "v1.2.3",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected MCP tool error: %#v", result)
	}

	mu.Lock()
	request := captured
	mu.Unlock()
	if request.Command != "deployment.publish" || request.ID != "deployment-1" || request.RegistryID != "registry-1" || request.Version != "v1.2.3" {
		t.Fatalf("unexpected request mapping: %#v", request)
	}
	if !request.Wait {
		t.Fatal("publish should wait by default")
	}
	if request.TimeoutMS != mcpOperationTimeout.Milliseconds() {
		t.Fatalf("unexpected default timeout: %d", request.TimeoutMS)
	}
}

func TestMCPBusinessFailureIsToolResult(t *testing.T) {
	client, closeSessions := connectMCPTestClient(t, func(_ context.Context, request cliRequest) cliResponse {
		return errorCLIResponse("not_found", "deployment not found")
	})
	defer closeSessions()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_deployment",
		Arguments: map[string]any{"deployment_id": "missing"},
	})
	if err != nil {
		t.Fatalf("business failure must not become a protocol error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected an MCP tool error result")
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("unexpected structured content: %#v", result.StructuredContent)
	}
	if structured["code"] != "not_found" || structured["ok"] != false {
		t.Fatalf("unexpected structured error: %#v", structured)
	}
}

func TestMCPRejectsInvalidTimeoutBeforeExecution(t *testing.T) {
	called := false
	client, closeSessions := connectMCPTestClient(t, func(_ context.Context, request cliRequest) cliResponse {
		called = true
		return cliResponseFromValue(nil)
	})
	defer closeSessions()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{
		Name: "wait_job",
		Arguments: map[string]any{
			"job_id":          "job-1",
			"timeout_seconds": 86401,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected invalid timeout to return a tool error")
	}
	if called {
		t.Fatal("executor must not run for invalid input")
	}
}

func connectMCPTestClient(t *testing.T, execute mcpRequestExecutor) (*mcp.ClientSession, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server := newIDeployMCPServer(execute, io.Discard)
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "ideploy-test", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
		cancel()
		t.Fatal(err)
	}
	return clientSession, func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
		cancel()
	}
}
