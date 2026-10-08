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
		"get_version": false, "doctor": false, "list_projects": false, "validate_project": false, "create_project": false, "update_project": false, "delete_project": false,
		"list_servers": false, "list_remote_containers": false, "get_container_logs": false, "check_port_availability": false,
		"list_registries": false, "get_build_proxy_settings": false, "configure_build_proxy": false,
		"get_operation": false, "list_deployments": false, "list_deployment_environment": false,
		"create_deployment": false, "update_deployment": false, "clone_deployment": false, "delete_deployment": false, "get_deployment": false, "preflight_deployment": false, "inspect_deployment_runtime": false, "list_releases": false, "list_jobs": false,
		"update_deployment_environment": false, "request_deployment_secret_input": false,
		"get_job": false, "wait_job": false, "publish_deployment": false,
		"rollback_deployment": false, "retry_release": false, "start_deployment": false,
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
		if tool.Name == "create_project" || tool.Name == "update_project" || tool.Name == "validate_project" || tool.Name == "create_deployment" || tool.Name == "update_deployment" || tool.Name == "update_deployment_environment" || tool.Name == "request_deployment_secret_input" || tool.Name == "clone_deployment" || tool.Name == "configure_build_proxy" {
			if tool.Annotations == nil || tool.Annotations.ReadOnlyHint || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
				t.Fatalf("%s must be annotated as a non-destructive configuration write", tool.Name)
			}
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("MCP tool %q was not registered", name)
		}
	}
}

func TestMCPDeploymentEnvironmentToolsMapSanitizedRequests(t *testing.T) {
	requests := make(chan cliRequest, 3)
	client, closeSessions := connectMCPTestClient(t, func(_ context.Context, request cliRequest) cliResponse {
		requests <- request
		return cliResponseFromValue(map[string]any{"configured": true})
	})
	defer closeSessions()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "list_deployment_environment", Arguments: map[string]any{"deployment_id": "deployment-1"}}); err != nil || result.IsError {
		t.Fatalf("list environment failed: result=%#v err=%v", result, err)
	}
	if result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "update_deployment_environment", Arguments: map[string]any{
		"request_id": "deployment-env-001", "deployment_id": "deployment-1", "variables": map[string]any{"DB_MODE": "mysql"}, "remove": []any{"OLD_KEY"},
	}}); err != nil || result.IsError {
		t.Fatalf("update environment failed: result=%#v err=%v", result, err)
	}
	if result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "request_deployment_secret_input", Arguments: map[string]any{
		"request_id": "deployment-secret-001", "deployment_id": "deployment-1", "name": "DATABASE_PASSWORD",
	}}); err != nil || result.IsError {
		t.Fatalf("secret prompt failed: result=%#v err=%v", result, err)
	}
	listed, updated, prompted := <-requests, <-requests, <-requests
	if listed.Command != "deployment.environment.list" || listed.ID != "deployment-1" {
		t.Fatalf("unexpected environment list mapping: %#v", listed)
	}
	if updated.Command != "deployment.environment.update" || updated.RequestID != "deployment-env-001" || updated.DeploymentEnvironment == nil || len(updated.DeploymentEnvironment.Variables) != 1 || updated.DeploymentEnvironment.Variables[0].Value != "mysql" {
		t.Fatalf("unexpected environment update mapping: %#v", updated)
	}
	if prompted.Command != "deployment.environment.secret.prompt" || prompted.DeploymentSecretName != "DATABASE_PASSWORD" || prompted.DeploymentEnvironment != nil {
		t.Fatalf("unexpected secret prompt mapping: %#v", prompted)
	}
}

func TestMCPUpdateConfigurationMapsPartialFieldsAndRequestID(t *testing.T) {
	requests := make(chan cliRequest, 2)
	client, closeSessions := connectMCPTestClient(t, func(_ context.Context, request cliRequest) cliResponse {
		requests <- request
		return cliResponseFromValue(map[string]any{"id": request.ID})
	})
	defer closeSessions()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "update_project", Arguments: map[string]any{
		"request_id": "project-update-001", "project_id": "project-1", "dockerfile_path": "deploy/Dockerfile",
	}}); err != nil || result.IsError {
		t.Fatalf("update_project failed: result=%#v err=%v", result, err)
	}
	if result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "update_deployment", Arguments: map[string]any{
		"request_id": "deployment-update-001", "deployment_id": "deployment-1", "container_ports": []any{"8081:3000"},
	}}); err != nil || result.IsError {
		t.Fatalf("update_deployment failed: result=%#v err=%v", result, err)
	}
	projectRequest, deploymentRequest := <-requests, <-requests
	if projectRequest.Command != "project.update" || projectRequest.RequestID != "project-update-001" || projectRequest.ProjectPatch == nil || projectRequest.ProjectPatch.DockerfilePath == nil || *projectRequest.ProjectPatch.DockerfilePath != "deploy/Dockerfile" {
		t.Fatalf("unexpected project update mapping: %#v", projectRequest)
	}
	if deploymentRequest.Command != "deployment.update" || deploymentRequest.RequestID != "deployment-update-001" || deploymentRequest.DeploymentPatch == nil || deploymentRequest.DeploymentPatch.ContainerPorts == nil || (*deploymentRequest.DeploymentPatch.ContainerPorts)[0] != "8081:3000" {
		t.Fatalf("unexpected deployment update mapping: %#v", deploymentRequest)
	}
}

func TestMCPPreflightAndRetryMapArguments(t *testing.T) {
	requests := make(chan cliRequest, 2)
	client, closeSessions := connectMCPTestClient(t, func(_ context.Context, request cliRequest) cliResponse {
		requests <- request
		return cliResponseFromValue(map[string]any{"ok": true})
	})
	defer closeSessions()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "preflight_deployment", Arguments: map[string]any{"deployment_id": "deployment-1", "registry_id": "registry-1"}}); err != nil || result.IsError {
		t.Fatalf("preflight failed: result=%#v err=%v", result, err)
	}
	if result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "retry_release", Arguments: map[string]any{"release_id": "release-1", "wait": false}}); err != nil || result.IsError {
		t.Fatalf("retry failed: result=%#v err=%v", result, err)
	}
	preflight, retry := <-requests, <-requests
	if preflight.Command != "deployment.preflight" || preflight.ID != "deployment-1" || preflight.RegistryID != "registry-1" {
		t.Fatalf("unexpected preflight mapping: %#v", preflight)
	}
	if retry.Command != "release.retry" || retry.ReleaseID != "release-1" || retry.Wait {
		t.Fatalf("unexpected retry mapping: %#v", retry)
	}
}

func TestMCPConfigureBuildProxyMapsStructuredArguments(t *testing.T) {
	var mu sync.Mutex
	var captured cliRequest
	client, closeSessions := connectMCPTestClient(t, func(_ context.Context, request cliRequest) cliResponse {
		mu.Lock()
		captured = request
		mu.Unlock()
		return cliResponseFromValue(BuildProxySettingsView{Mode: "required", Configured: true})
	})
	defer closeSessions()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{
		Name: "configure_build_proxy",
		Arguments: map[string]any{
			"mode":  "required",
			"rules": []any{map[string]any{"upstream": "docker.io", "mirror": "harbor.example.com/proxy-dockerhub"}},
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
	if request.Command != "build-proxy.configure" || request.BuildProxy == nil || request.BuildProxy.Mode != "required" {
		t.Fatalf("unexpected request mapping: %#v", request)
	}
	if len(request.BuildProxy.Rules) != 1 || request.BuildProxy.Rules[0].Mirror != "harbor.example.com/proxy-dockerhub" {
		t.Fatalf("unexpected build proxy mapping: %#v", request.BuildProxy)
	}
}

func TestMCPCreateProjectMapsStructuredArguments(t *testing.T) {
	var mu sync.Mutex
	var captured cliRequest
	client, closeSessions := connectMCPTestClient(t, func(_ context.Context, request cliRequest) cliResponse {
		mu.Lock()
		captured = request
		mu.Unlock()
		return cliResponseFromValue(map[string]any{"id": "project-1"})
	})
	defer closeSessions()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{
		Name: "create_project",
		Arguments: map[string]any{
			"name": "API", "source_kind": "git", "repository_url": "https://example.com/api.git",
			"ref_value": "main", "dockerfile_path": "Dockerfile", "image_repository": "registry.example.com/team/api",
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
	if request.Command != "project.create" || request.Project == nil {
		t.Fatalf("unexpected request mapping: %#v", request)
	}
	if request.Project.Name != "API" || request.Project.SourceKind != "git" || request.Project.RefKind != "branch" || request.Project.RefValue != "main" {
		t.Fatalf("unexpected project mapping: %#v", request.Project)
	}
}

func TestMCPCreateDeploymentMapsStructuredArguments(t *testing.T) {
	var mu sync.Mutex
	var captured cliRequest
	client, closeSessions := connectMCPTestClient(t, func(_ context.Context, request cliRequest) cliResponse {
		mu.Lock()
		captured = request
		mu.Unlock()
		return cliResponseFromValue(map[string]any{"id": "deployment-1"})
	})
	defer closeSessions()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{
		Name: "create_deployment",
		Arguments: map[string]any{
			"project_id": "project-1", "server_id": "server-1", "workload_kind": "container",
			"workload_name": "api", "container_ports": []any{"8080:80"},
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
	if request.Command != "deployment.create" || request.Deployment == nil {
		t.Fatalf("unexpected request mapping: %#v", request)
	}
	if request.Deployment.ProjectID != "project-1" || request.Deployment.ServerID != "server-1" || request.Deployment.RestartPolicy != "unless-stopped" {
		t.Fatalf("unexpected deployment mapping: %#v", request.Deployment)
	}
}

func TestMCPCloneDeploymentMapsOverrides(t *testing.T) {
	var captured cliRequest
	client, closeSessions := connectMCPTestClient(t, func(_ context.Context, request cliRequest) cliResponse {
		captured = request
		return cliResponseFromValue(map[string]any{"id": "deployment-2"})
	})
	defer closeSessions()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "clone_deployment", Arguments: map[string]any{
		"request_id": "deployment-clone-001", "deployment_id": "deployment-1",
		"server_id": "server-2", "workload_name": "api-copy", "container_ports": []any{"8081:80"},
	}})
	if err != nil || result.IsError {
		t.Fatalf("clone failed: result=%#v err=%v", result, err)
	}
	if captured.Command != "deployment.clone" || captured.ID != "deployment-1" || captured.RequestID != "deployment-clone-001" || captured.DeploymentPatch == nil {
		t.Fatalf("unexpected clone mapping: %#v", captured)
	}
	if captured.DeploymentPatch.ServerID == nil || *captured.DeploymentPatch.ServerID != "server-2" || captured.DeploymentPatch.WorkloadName == nil || *captured.DeploymentPatch.WorkloadName != "api-copy" {
		t.Fatalf("unexpected clone overrides: %#v", captured.DeploymentPatch)
	}
}

func TestMCPCreateProjectRejectsIncompleteSource(t *testing.T) {
	called := false
	client, closeSessions := connectMCPTestClient(t, func(_ context.Context, request cliRequest) cliResponse {
		called = true
		return cliResponseFromValue(nil)
	})
	defer closeSessions()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{
		Name: "create_project", Arguments: map[string]any{"name": "API", "source_kind": "git"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || called {
		t.Fatalf("expected validation failure before execution: result=%#v called=%v", result, called)
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
