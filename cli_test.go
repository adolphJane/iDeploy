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

func TestCLIControlCreatesProjectAndDeploymentConfiguration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix socket transport is not enabled on Windows")
	}
	root, err := os.MkdirTemp("/tmp", "idp-mcp-create-")
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
	projectPath := filepath.Join(root, "orders-api")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := NewSystemService()
	if err := service.startWithPaths(ctx, true, paths); err != nil {
		t.Fatal(err)
	}
	defer service.close()

	controlPath := filepath.Join(root, "control.sock")
	proxyResponse, err := callCLIControl(ctx, controlPath, cliRequest{
		Command: "build-proxy.configure",
		BuildProxy: &BuildProxySettingsInput{
			Mode: "required", Rules: []BuildProxyRule{{Upstream: "docker.io", Mirror: "harbor.example.com/proxy-dockerhub"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proxyResponse.OK {
		t.Fatalf("configure build proxy failed: %#v", proxyResponse)
	}
	proxyReadResponse, err := callCLIControl(ctx, controlPath, cliRequest{Command: "build-proxy.get"})
	if err != nil {
		t.Fatal(err)
	}
	var proxySettings BuildProxySettingsView
	if !proxyReadResponse.OK || json.Unmarshal(proxyReadResponse.Data, &proxySettings) != nil || !proxySettings.Configured {
		t.Fatalf("unexpected build proxy settings: response=%#v settings=%#v", proxyReadResponse, proxySettings)
	}

	projectResponse, err := callCLIControl(ctx, controlPath, cliRequest{
		Command: "project.create", RequestID: "project-create-test-001",
		Project: &ProjectInput{
			Name: "Orders API", SourceKind: "local", LocalPath: projectPath,
			ContextPath: ".", ImageRepository: "registry.example.com/team/orders-api",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !projectResponse.OK {
		t.Fatalf("create project failed: %#v", projectResponse)
	}
	var createdProject ProjectView
	if err := json.Unmarshal(projectResponse.Data, &createdProject); err != nil {
		t.Fatal(err)
	}
	if createdProject.ID == "" || createdProject.DockerfilePath != "Dockerfile" {
		t.Fatalf("unexpected created project: %#v", createdProject)
	}
	repeatedProjectResponse, err := callCLIControl(ctx, controlPath, cliRequest{
		Command: "project.create", RequestID: "project-create-test-001",
		Project: &ProjectInput{
			Name: "Orders API", SourceKind: "local", LocalPath: projectPath,
			ContextPath: ".", ImageRepository: "registry.example.com/team/orders-api",
		},
	})
	if err != nil || !repeatedProjectResponse.OK || string(repeatedProjectResponse.Data) != string(projectResponse.Data) {
		t.Fatalf("idempotent project create did not return the stored result: response=%#v err=%v", repeatedProjectResponse, err)
	}
	conflictResponse, err := callCLIControl(ctx, controlPath, cliRequest{
		Command: "project.create", RequestID: "project-create-test-001",
		Project: &ProjectInput{Name: "Different", SourceKind: "local", LocalPath: projectPath, ContextPath: "."},
	})
	if err != nil || conflictResponse.OK || conflictResponse.Code != "idempotency_conflict" {
		t.Fatalf("request-id conflict was not rejected: response=%#v err=%v", conflictResponse, err)
	}
	operationResponse, err := callCLIControl(ctx, controlPath, cliRequest{Command: "operation.get", ID: "project-create-test-001"})
	if err != nil || !operationResponse.OK {
		t.Fatalf("get operation failed: response=%#v err=%v", operationResponse, err)
	}

	newRepository := "registry.example.com/team/orders-api-v2"
	projectUpdateResponse, err := callCLIControl(ctx, controlPath, cliRequest{
		Command: "project.update", ID: createdProject.ID, RequestID: "project-update-test-001",
		ProjectPatch: &ProjectPatchInput{ImageRepository: &newRepository},
	})
	if err != nil || !projectUpdateResponse.OK {
		t.Fatalf("update project failed: response=%#v err=%v", projectUpdateResponse, err)
	}
	var updatedProject ProjectView
	if json.Unmarshal(projectUpdateResponse.Data, &updatedProject) != nil || updatedProject.ImageRepository != newRepository {
		t.Fatalf("unexpected updated project: %#v", updatedProject)
	}

	createdServer, err := service.CreateServer(ServerInput{
		Name: "Deploy host", Host: "127.0.0.1", Port: 22, SSHUser: "deploy", AuthMethod: "agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	deploymentResponse, err := callCLIControl(ctx, controlPath, cliRequest{
		Command: "deployment.create",
		Deployment: &DeploymentInput{
			ProjectID: createdProject.ID, ServerID: createdServer.ID, WorkloadKind: "container",
			WorkloadName: "orders-api", ContainerPorts: []string{"8080:8080"}, RestartPolicy: "unless-stopped",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !deploymentResponse.OK {
		t.Fatalf("create deployment failed: %#v", deploymentResponse)
	}
	var createdDeployment DeploymentView
	if err := json.Unmarshal(deploymentResponse.Data, &createdDeployment); err != nil {
		t.Fatal(err)
	}
	if createdDeployment.ID == "" || createdDeployment.ProjectID != createdProject.ID || createdDeployment.ServerID != createdServer.ID {
		t.Fatalf("unexpected created deployment: %#v", createdDeployment)
	}
	updatedPorts := []string{"8081:8080"}
	deploymentUpdateResponse, err := callCLIControl(ctx, controlPath, cliRequest{
		Command: "deployment.update", ID: createdDeployment.ID, RequestID: "deployment-update-test-001",
		DeploymentPatch: &DeploymentPatchInput{ContainerPorts: &updatedPorts},
	})
	if err != nil || !deploymentUpdateResponse.OK {
		t.Fatalf("update deployment failed: response=%#v err=%v", deploymentUpdateResponse, err)
	}
	var updatedDeployment DeploymentView
	if json.Unmarshal(deploymentUpdateResponse.Data, &updatedDeployment) != nil || len(updatedDeployment.ContainerPorts) != 1 || updatedDeployment.ContainerPorts[0] != "8081:8080" {
		t.Fatalf("unexpected updated deployment: %#v", updatedDeployment)
	}
	cloneServer, err := service.CreateServer(ServerInput{
		Name: "Clone host", Host: "127.0.0.2", Port: 22, SSHUser: "deploy", AuthMethod: "agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	cloneName := "orders-api-copy"
	cloneResponse, err := callCLIControl(ctx, controlPath, cliRequest{
		Command: "deployment.clone", ID: createdDeployment.ID, RequestID: "deployment-clone-test-001",
		DeploymentPatch: &DeploymentPatchInput{ServerID: &cloneServer.ID, WorkloadName: &cloneName},
	})
	if err != nil || !cloneResponse.OK {
		t.Fatalf("clone deployment failed: response=%#v err=%v", cloneResponse, err)
	}
	var clonedDeployment DeploymentView
	if json.Unmarshal(cloneResponse.Data, &clonedDeployment) != nil || clonedDeployment.ServerID != cloneServer.ID || clonedDeployment.WorkloadName != cloneName || clonedDeployment.ProjectID != createdProject.ID {
		t.Fatalf("unexpected cloned deployment: %#v", clonedDeployment)
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
