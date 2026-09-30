package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpReadTimeout      = 30 * time.Second
	mcpOperationTimeout = 30 * time.Minute
	mcpMaximumTimeout   = 24 * time.Hour
)

const mcpInstructions = `Use list tools to resolve deployment, registry, release, and job IDs before calling mutation tools. Mutation tools change remote Docker workloads and must only be called after the user explicitly requests the exact action. Do not automatically retry a mutation after a timeout; inspect list_jobs or get_job first. Never request, expose, or return registry credentials, SSH credentials, or Keychain values. Asynchronous operations require iDeploy.app to be running; otherwise use the default wait=true behavior.`

type mcpRequestExecutor func(context.Context, cliRequest) cliResponse

type mcpService struct {
	execute mcpRequestExecutor
}

type mcpEmptyInput struct{}

type mcpDeploymentInput struct {
	DeploymentID string `json:"deployment_id" jsonschema:"stable deployment identifier returned by list_deployments"`
}

type mcpJobInput struct {
	JobID string `json:"job_id" jsonschema:"stable job identifier returned by an operation or list_jobs"`
}

type mcpJobWaitInput struct {
	JobID          string `json:"job_id" jsonschema:"stable job identifier returned by an operation or list_jobs"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"maximum time to wait in seconds; defaults to 1800 and cannot exceed 86400"`
}

type mcpDeploymentOperationInput struct {
	DeploymentID   string `json:"deployment_id" jsonschema:"stable deployment identifier returned by list_deployments"`
	Wait           *bool  `json:"wait,omitempty" jsonschema:"wait for the operation to finish; defaults to true"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"operation timeout in seconds; defaults to 1800 and cannot exceed 86400"`
}

type mcpPublishInput struct {
	DeploymentID   string `json:"deployment_id" jsonschema:"stable deployment identifier returned by list_deployments"`
	RegistryID     string `json:"registry_id" jsonschema:"stable registry identifier returned by list_registries"`
	Version        string `json:"version,omitempty" jsonschema:"optional release version; iDeploy generates one when omitted"`
	Wait           *bool  `json:"wait,omitempty" jsonschema:"wait for publishing to finish; defaults to true"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"operation timeout in seconds; defaults to 1800 and cannot exceed 86400"`
}

type mcpRollbackInput struct {
	DeploymentID   string `json:"deployment_id" jsonschema:"stable deployment identifier returned by list_deployments"`
	ReleaseID      string `json:"release_id" jsonschema:"stable release identifier returned by list_releases"`
	Wait           *bool  `json:"wait,omitempty" jsonschema:"wait for rollback to finish; defaults to true"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"operation timeout in seconds; defaults to 1800 and cannot exceed 86400"`
}

type mcpToolOutput struct {
	OK      bool   `json:"ok" jsonschema:"whether the iDeploy operation succeeded"`
	Data    any    `json:"data,omitempty" jsonschema:"structured iDeploy response data"`
	Code    string `json:"code,omitempty" jsonschema:"stable machine-readable error code"`
	Message string `json:"message" jsonschema:"short human-readable result summary"`
}

func shouldRunMCP(arguments []string) bool {
	return len(arguments) > 0 && strings.TrimSpace(arguments[0]) == "mcp"
}

func runMCPCommand(stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := newIDeployMCPServer(executeRuntimeRequest, stderr)
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(stderr, "ideploy mcp: %v\n", err)
		return 1
	}
	return 0
}

func newIDeployMCPServer(execute mcpRequestExecutor, logWriter io.Writer) *mcp.Server {
	if execute == nil {
		execute = executeRuntimeRequest
	}
	if logWriter == nil {
		logWriter = io.Discard
	}
	logger := slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: slog.LevelError}))
	server := mcp.NewServer(
		&mcp.Implementation{Name: "ideploy", Version: appVersion},
		&mcp.ServerOptions{Instructions: mcpInstructions, Logger: logger},
	)
	service := &mcpService{execute: execute}
	registerMCPReadTools(server, service)
	registerMCPMutationTools(server, service)
	return server
}

func registerMCPReadTools(server *mcp.Server, service *mcpService) {
	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "get_version", Description: "Return the installed iDeploy version and platform.",
		Annotations: mcpReadAnnotations("Get iDeploy version", false),
	}, mcpReadTimeout, func(mcpEmptyInput) (cliRequest, error) {
		return cliRequest{Command: "version"}, nil
	}, "Returned the installed iDeploy version.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "doctor", Description: "Inspect the local iDeploy, Docker, database, and runtime status without changing them.",
		Annotations: mcpReadAnnotations("Inspect iDeploy status", true),
	}, mcpReadTimeout, func(mcpEmptyInput) (cliRequest, error) {
		return cliRequest{Command: "doctor"}, nil
	}, "Inspected the iDeploy runtime.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "list_projects", Description: "List projects configured in iDeploy and return their stable IDs.",
		Annotations: mcpReadAnnotations("List projects", false),
	}, mcpReadTimeout, listRequest("project.list"), "Listed iDeploy projects.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "list_servers", Description: "List deployment servers configured in iDeploy. Credentials are never returned.",
		Annotations: mcpReadAnnotations("List servers", false),
	}, mcpReadTimeout, listRequest("server.list"), "Listed iDeploy servers.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "list_registries", Description: "List container registries configured in iDeploy. Credentials are never returned.",
		Annotations: mcpReadAnnotations("List registries", false),
	}, mcpReadTimeout, listRequest("registry.list"), "Listed iDeploy registries.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "list_deployments", Description: "List deployment configurations and return the stable deployment IDs needed by operation tools.",
		Annotations: mcpReadAnnotations("List deployments", false),
	}, mcpReadTimeout, listRequest("deployment.list"), "Listed iDeploy deployments.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "get_deployment", Description: "Get one deployment configuration by its stable ID.",
		Annotations: mcpReadAnnotations("Get deployment", false),
	}, mcpReadTimeout, func(input mcpDeploymentInput) (cliRequest, error) {
		if err := requireMCPValue("deployment_id", input.DeploymentID); err != nil {
			return cliRequest{}, err
		}
		return cliRequest{Command: "deployment.get", ID: strings.TrimSpace(input.DeploymentID)}, nil
	}, "Returned the deployment configuration.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "list_releases", Description: "List recorded releases for one deployment. Use the returned release IDs for rollback.",
		Annotations: mcpReadAnnotations("List deployment releases", false),
	}, mcpReadTimeout, func(input mcpDeploymentInput) (cliRequest, error) {
		if err := requireMCPValue("deployment_id", input.DeploymentID); err != nil {
			return cliRequest{}, err
		}
		return cliRequest{Command: "release.list", DeploymentID: strings.TrimSpace(input.DeploymentID)}, nil
	}, "Listed deployment releases.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "list_jobs", Description: "List recent iDeploy jobs and their current status.",
		Annotations: mcpReadAnnotations("List jobs", false),
	}, mcpReadTimeout, listRequest("job.list"), "Listed recent iDeploy jobs.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "get_job", Description: "Get the current state and diagnostic message for one iDeploy job.",
		Annotations: mcpReadAnnotations("Get job", false),
	}, mcpReadTimeout, func(input mcpJobInput) (cliRequest, error) {
		if err := requireMCPValue("job_id", input.JobID); err != nil {
			return cliRequest{}, err
		}
		return cliRequest{Command: "job.get", ID: strings.TrimSpace(input.JobID)}, nil
	}, "Returned the job state.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "wait_job", Description: "Wait until an iDeploy job succeeds, fails, is canceled, or reaches the timeout.",
		Annotations: mcpReadAnnotations("Wait for job", false),
	}, mcpOperationTimeout, func(input mcpJobWaitInput) (cliRequest, error) {
		if err := requireMCPValue("job_id", input.JobID); err != nil {
			return cliRequest{}, err
		}
		timeout, err := mcpTimeout(input.TimeoutSeconds, mcpOperationTimeout)
		if err != nil {
			return cliRequest{}, err
		}
		return cliRequest{Command: "job.wait", ID: strings.TrimSpace(input.JobID), TimeoutMS: timeout.Milliseconds()}, nil
	}, "The job reached a terminal state.")
}

func registerMCPMutationTools(server *mcp.Server, service *mcpService) {
	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "publish_deployment", Description: "Build and publish a deployment, push its image through an existing registry, and update the configured remote workload. This changes external systems.",
		Annotations: mcpMutationAnnotations("Publish deployment"),
	}, mcpOperationTimeout, func(input mcpPublishInput) (cliRequest, error) {
		if err := requireMCPValue("deployment_id", input.DeploymentID); err != nil {
			return cliRequest{}, err
		}
		if err := requireMCPValue("registry_id", input.RegistryID); err != nil {
			return cliRequest{}, err
		}
		timeout, err := mcpTimeout(input.TimeoutSeconds, mcpOperationTimeout)
		if err != nil {
			return cliRequest{}, err
		}
		return cliRequest{
			Command: "deployment.publish", ID: strings.TrimSpace(input.DeploymentID),
			RegistryID: strings.TrimSpace(input.RegistryID), Version: strings.TrimSpace(input.Version),
			Wait: mcpWait(input.Wait), TimeoutMS: timeout.Milliseconds(),
		}, nil
	}, "Deployment publishing completed or was queued.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "rollback_deployment", Description: "Roll a deployment back to a recorded release. This changes the remote workload and may replace running containers.",
		Annotations: mcpMutationAnnotations("Rollback deployment"),
	}, mcpOperationTimeout, func(input mcpRollbackInput) (cliRequest, error) {
		if err := requireMCPValue("deployment_id", input.DeploymentID); err != nil {
			return cliRequest{}, err
		}
		if err := requireMCPValue("release_id", input.ReleaseID); err != nil {
			return cliRequest{}, err
		}
		timeout, err := mcpTimeout(input.TimeoutSeconds, mcpOperationTimeout)
		if err != nil {
			return cliRequest{}, err
		}
		return cliRequest{
			Command: "deployment.rollback", ID: strings.TrimSpace(input.DeploymentID),
			ReleaseID: strings.TrimSpace(input.ReleaseID), Wait: mcpWait(input.Wait), TimeoutMS: timeout.Milliseconds(),
		}, nil
	}, "Deployment rollback completed or was queued.")

	registerDeploymentOperationTool(server, service, "start_deployment", "Start deployment", "start")
	registerDeploymentOperationTool(server, service, "stop_deployment", "Stop deployment", "stop")
	registerDeploymentOperationTool(server, service, "restart_deployment", "Restart deployment", "restart")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "cancel_job", Description: "Request cancellation of an iDeploy job. Cancellation may leave an external operation partially completed.",
		Annotations: mcpMutationAnnotations("Cancel job"),
	}, mcpReadTimeout, func(input mcpJobInput) (cliRequest, error) {
		if err := requireMCPValue("job_id", input.JobID); err != nil {
			return cliRequest{}, err
		}
		return cliRequest{Command: "job.cancel", ID: strings.TrimSpace(input.JobID)}, nil
	}, "Requested job cancellation.")
}

func registerDeploymentOperationTool(server *mcp.Server, service *mcpService, name, title, action string) {
	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: name, Description: title + " on its configured remote server. This changes the remote workload.",
		Annotations: mcpMutationAnnotations(title),
	}, mcpOperationTimeout, func(input mcpDeploymentOperationInput) (cliRequest, error) {
		if err := requireMCPValue("deployment_id", input.DeploymentID); err != nil {
			return cliRequest{}, err
		}
		timeout, err := mcpTimeout(input.TimeoutSeconds, mcpOperationTimeout)
		if err != nil {
			return cliRequest{}, err
		}
		return cliRequest{
			Command: "deployment." + action, ID: strings.TrimSpace(input.DeploymentID),
			Wait: mcpWait(input.Wait), TimeoutMS: timeout.Milliseconds(),
		}, nil
	}, title+" completed or was queued.")
}

func registerMCPCommandTool[Input any](
	server *mcp.Server,
	service *mcpService,
	tool *mcp.Tool,
	defaultTimeout time.Duration,
	build func(Input) (cliRequest, error),
	successMessage string,
) {
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, input Input) (*mcp.CallToolResult, mcpToolOutput, error) {
		request, err := build(input)
		if err != nil {
			return mcpFailure("invalid_arguments", err.Error())
		}
		timeout := defaultTimeout
		if request.TimeoutMS > 0 {
			timeout = time.Duration(request.TimeoutMS) * time.Millisecond
		} else {
			request.TimeoutMS = timeout.Milliseconds()
		}
		callContext, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return mcpResult(service.execute(callContext, request), successMessage)
	})
}

func listRequest(command string) func(mcpEmptyInput) (cliRequest, error) {
	return func(mcpEmptyInput) (cliRequest, error) {
		return cliRequest{Command: command}, nil
	}
}

func mcpResult(response cliResponse, successMessage string) (*mcp.CallToolResult, mcpToolOutput, error) {
	var data any
	if len(response.Data) > 0 {
		if err := json.Unmarshal(response.Data, &data); err != nil {
			return mcpFailure("encoding_failed", "iDeploy returned invalid structured data")
		}
	}
	if !response.OK {
		message := strings.TrimSpace(response.Error)
		if message == "" {
			message = "iDeploy operation failed"
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: message}},
			IsError: true,
		}, mcpToolOutput{OK: false, Data: data, Code: response.Code, Message: message}, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: successMessage}},
	}, mcpToolOutput{OK: true, Data: data, Message: successMessage}, nil
}

func mcpFailure(code, message string) (*mcp.CallToolResult, mcpToolOutput, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: message}},
		IsError: true,
	}, mcpToolOutput{OK: false, Code: code, Message: message}, nil
}

func mcpReadAnnotations(title string, openWorld bool) *mcp.ToolAnnotations {
	destructive := false
	return &mcp.ToolAnnotations{
		Title: title, ReadOnlyHint: true, IdempotentHint: true,
		DestructiveHint: &destructive, OpenWorldHint: &openWorld,
	}
}

func mcpMutationAnnotations(title string) *mcp.ToolAnnotations {
	destructive, openWorld := true, true
	return &mcp.ToolAnnotations{
		Title: title, ReadOnlyHint: false, IdempotentHint: false,
		DestructiveHint: &destructive, OpenWorldHint: &openWorld,
	}
}

func requireMCPValue(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

func mcpWait(value *bool) bool {
	return value == nil || *value
}

func mcpTimeout(seconds int, fallback time.Duration) (time.Duration, error) {
	if seconds == 0 {
		return fallback, nil
	}
	duration := time.Duration(seconds) * time.Second
	if seconds < 0 || duration <= 0 || duration > mcpMaximumTimeout {
		return 0, errors.New("timeout_seconds must be between 1 and 86400")
	}
	return duration, nil
}
