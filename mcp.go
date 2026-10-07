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

const mcpInstructions = `Use list tools to resolve project, server, deployment, registry, release, and job IDs before referencing existing resources. Project, deployment, and build-proxy configuration tools only save local iDeploy configuration; they do not publish or change remote workloads. Supply a stable request_id for configuration writes and use get_operation after operation_pending or an uncertain local-control timeout. Run preflight_deployment before publishing. Publishing automatically uses the global build proxy policy returned by get_build_proxy_settings and blocks known host-port conflicts before building. Remote workload mutation tools must only be called after the user explicitly requests the exact action. retry_release safely rebuilds failures without an artifact and directly resumes deployment when an immutable pushed artifact was retained. Do not automatically retry other mutations after a timeout; inspect list_jobs or get_job first. Never request, expose, or return registry credentials, SSH credentials, or Keychain values. Asynchronous operations require iDeploy.app to be running; otherwise use the default wait=true behavior.`

type mcpRequestExecutor func(context.Context, cliRequest) cliResponse

type mcpService struct {
	execute mcpRequestExecutor
}

type mcpEmptyInput struct{}

type mcpOperationInput struct {
	OperationID string `json:"operation_id" jsonschema:"request_id used by an idempotent configuration write"`
}

type mcpServerInput struct {
	ServerID string `json:"server_id" jsonschema:"stable server identifier returned by list_servers"`
}

type mcpContainerLogsInput struct {
	ServerID    string `json:"server_id" jsonschema:"stable server identifier returned by list_servers"`
	ContainerID string `json:"container_id" jsonschema:"container ID or name returned by list_remote_containers"`
	Tail        int    `json:"tail,omitempty" jsonschema:"number of recent log lines, defaults to 200 and cannot exceed 2000"`
}

type mcpPortAvailabilityInput struct {
	ServerID string `json:"server_id" jsonschema:"stable server identifier returned by list_servers"`
	Ports    []int  `json:"ports" jsonschema:"host ports to check, between 1 and 65535"`
}

type mcpCreateProjectInput struct {
	RequestID       string `json:"request_id,omitempty" jsonschema:"optional caller-generated idempotency key; reuse it when retrying an uncertain request"`
	Name            string `json:"name" jsonschema:"project name shown in iDeploy; must be unique and 1 to 80 characters"`
	Description     string `json:"description,omitempty" jsonschema:"optional project description; at most 500 characters"`
	SourceKind      string `json:"source_kind" jsonschema:"source type: local for a directory on the iDeploy machine, or git for a repository URL"`
	LocalPath       string `json:"local_path,omitempty" jsonschema:"required when source_kind is local: absolute project directory path on the machine running iDeploy"`
	RepositoryURL   string `json:"repository_url,omitempty" jsonschema:"required when source_kind is git: non-secret HTTPS, SSH, or git@host:path repository URL"`
	RefKind         string `json:"ref_kind,omitempty" jsonschema:"git reference type: branch, tag, or commit; defaults to branch and is ignored for local projects"`
	RefValue        string `json:"ref_value,omitempty" jsonschema:"required when source_kind is git: branch name, tag, or commit to check out"`
	ContextPath     string `json:"context_path,omitempty" jsonschema:"Docker build context relative to the project root; defaults to ."`
	DockerfilePath  string `json:"dockerfile_path,omitempty" jsonschema:"Dockerfile path relative to the project root; local projects auto-detect it when omitted; required for container publishing if detection is unavailable"`
	ComposePath     string `json:"compose_path,omitempty" jsonschema:"Compose file path relative to the project root; local projects auto-detect it when omitted; required for compose publishing"`
	ImageRepository string `json:"image_repository,omitempty" jsonschema:"container image repository without tag or digest, for example registry.example.com/team/api; required for container publishing and must match the selected registry"`
}

type mcpProjectInput struct {
	ProjectID string `json:"project_id" jsonschema:"stable project identifier returned by create_project or list_projects"`
}

type mcpUpdateProjectInput struct {
	RequestID       string  `json:"request_id,omitempty" jsonschema:"optional caller-generated idempotency key; reuse it when retrying an uncertain request"`
	ProjectID       string  `json:"project_id" jsonschema:"stable project identifier returned by create_project or list_projects"`
	Name            *string `json:"name,omitempty" jsonschema:"new project name when changing it"`
	Description     *string `json:"description,omitempty" jsonschema:"new project description; use an empty string to clear it"`
	SourceKind      *string `json:"source_kind,omitempty" jsonschema:"new source type: local or git"`
	LocalPath       *string `json:"local_path,omitempty" jsonschema:"absolute local project path; use with source_kind local"`
	RepositoryURL   *string `json:"repository_url,omitempty" jsonschema:"Git repository URL; use with source_kind git"`
	RefKind         *string `json:"ref_kind,omitempty" jsonschema:"Git reference type: branch, tag, or commit"`
	RefValue        *string `json:"ref_value,omitempty" jsonschema:"Git branch, tag, or commit value"`
	ContextPath     *string `json:"context_path,omitempty" jsonschema:"Docker build context relative to the project root"`
	DockerfilePath  *string `json:"dockerfile_path,omitempty" jsonschema:"Dockerfile path relative to the project root; empty clears it"`
	ComposePath     *string `json:"compose_path,omitempty" jsonschema:"Compose file path relative to the project root; empty clears it"`
	ImageRepository *string `json:"image_repository,omitempty" jsonschema:"container image repository without tag or digest"`
}

type mcpCreateDeploymentInput struct {
	RequestID      string   `json:"request_id,omitempty" jsonschema:"optional caller-generated idempotency key; reuse it when retrying an uncertain request"`
	ProjectID      string   `json:"project_id" jsonschema:"stable project identifier returned by create_project or list_projects"`
	ServerID       string   `json:"server_id" jsonschema:"stable server identifier returned by list_servers"`
	WorkloadKind   string   `json:"workload_kind" jsonschema:"remote workload type: container or compose"`
	WorkloadName   string   `json:"workload_name" jsonschema:"remote container name, or lowercase Compose project name"`
	ContainerPorts []string `json:"container_ports,omitempty" jsonschema:"container-only port mappings, for example 8080, 8080:80, 127.0.0.1:8080:80, optionally ending in /tcp or /udp; ignored for compose"`
	RestartPolicy  string   `json:"restart_policy,omitempty" jsonschema:"container restart policy: no, always, unless-stopped, or on-failure; defaults to unless-stopped"`
}

type mcpUpdateDeploymentInput struct {
	RequestID      string    `json:"request_id,omitempty" jsonschema:"optional caller-generated idempotency key; reuse it when retrying an uncertain request"`
	DeploymentID   string    `json:"deployment_id" jsonschema:"stable deployment identifier returned by create_deployment or list_deployments"`
	ProjectID      *string   `json:"project_id,omitempty" jsonschema:"new project identifier"`
	ServerID       *string   `json:"server_id,omitempty" jsonschema:"new target server identifier"`
	WorkloadKind   *string   `json:"workload_kind,omitempty" jsonschema:"new workload type: container or compose"`
	WorkloadName   *string   `json:"workload_name,omitempty" jsonschema:"new container or Compose project name"`
	ContainerPorts *[]string `json:"container_ports,omitempty" jsonschema:"replacement container port mappings; an empty array clears them"`
	RestartPolicy  *string   `json:"restart_policy,omitempty" jsonschema:"new container restart policy"`
}

type mcpCloneDeploymentInput struct {
	RequestID      string    `json:"request_id,omitempty" jsonschema:"optional caller-generated idempotency key; reuse it when retrying an uncertain request"`
	DeploymentID   string    `json:"deployment_id" jsonschema:"source deployment configuration identifier"`
	ServerID       string    `json:"server_id" jsonschema:"target server identifier; normally different from the source server"`
	WorkloadName   string    `json:"workload_name" jsonschema:"new remote container or Compose project name"`
	ProjectID      *string   `json:"project_id,omitempty" jsonschema:"optional replacement project identifier; defaults to the source project"`
	WorkloadKind   *string   `json:"workload_kind,omitempty" jsonschema:"optional replacement workload type: container or compose"`
	ContainerPorts *[]string `json:"container_ports,omitempty" jsonschema:"optional replacement port mappings; omitted inherits the source"`
	RestartPolicy  *string   `json:"restart_policy,omitempty" jsonschema:"optional replacement restart policy"`
}

type mcpDeleteProjectInput struct {
	RequestID string `json:"request_id,omitempty" jsonschema:"optional caller-generated idempotency key"`
	ProjectID string `json:"project_id" jsonschema:"stable project identifier to delete"`
	Confirm   bool   `json:"confirm" jsonschema:"must be true after the user explicitly confirms local configuration deletion"`
}

type mcpDeleteDeploymentInput struct {
	RequestID    string `json:"request_id,omitempty" jsonschema:"optional caller-generated idempotency key"`
	DeploymentID string `json:"deployment_id" jsonschema:"stable deployment identifier to delete"`
	Confirm      bool   `json:"confirm" jsonschema:"must be true after the user explicitly confirms local configuration deletion"`
}

type mcpBuildProxyRule struct {
	Upstream string `json:"upstream" jsonschema:"upstream registry host and optional port, for example docker.io or ghcr.io; do not include a URL scheme"`
	Mirror   string `json:"mirror" jsonschema:"iDeploy proxy cache path in host/project form, for example harbor.example.com/proxy-dockerhub; do not include a URL scheme, tag, or digest"`
}

type mcpConfigureBuildProxyInput struct {
	RequestID string              `json:"request_id,omitempty" jsonschema:"optional caller-generated idempotency key; reuse it when retrying an uncertain request"`
	Mode      string              `json:"mode" jsonschema:"build proxy policy: disabled, preferred, or required; required stops publishing when the managed proxy builder cannot be prepared"`
	Rules     []mcpBuildProxyRule `json:"rules" jsonschema:"upstream-to-proxy mappings; at least one rule is required unless mode is disabled"`
}

type mcpDeploymentInput struct {
	DeploymentID string `json:"deployment_id" jsonschema:"stable deployment identifier returned by list_deployments"`
}

type mcpPreflightDeploymentInput struct {
	DeploymentID string `json:"deployment_id" jsonschema:"stable deployment identifier returned by list_deployments"`
	RegistryID   string `json:"registry_id" jsonschema:"registry identifier intended for the next publish"`
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

type mcpRetryReleaseInput struct {
	ReleaseID      string `json:"release_id" jsonschema:"failed or canceled release identifier returned by list_releases"`
	Wait           *bool  `json:"wait,omitempty" jsonschema:"wait for the retry to finish; defaults to true"`
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
		Name: "validate_project", Description: "Rescan an existing local project source and refresh its Dockerfile, Compose, and readiness status. Git project validation is performed as part of deployment preflight.",
		Annotations: mcpConfigurationAnnotations("Validate project configuration"),
	}, mcpReadTimeout, func(input mcpProjectInput) (cliRequest, error) {
		if err := requireMCPValue("project_id", input.ProjectID); err != nil {
			return cliRequest{}, err
		}
		return cliRequest{Command: "project.inspect", ID: strings.TrimSpace(input.ProjectID)}, nil
	}, "Validated the iDeploy project configuration.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "list_servers", Description: "List deployment servers configured in iDeploy. Credentials are never returned.",
		Annotations: mcpReadAnnotations("List servers", false),
	}, mcpReadTimeout, listRequest("server.list"), "Listed iDeploy servers.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "list_remote_containers", Description: "Refresh and list Docker containers on an existing iDeploy server. Credentials are never returned.",
		Annotations: mcpReadAnnotations("List remote containers", true),
	}, mcpReadTimeout, func(input mcpServerInput) (cliRequest, error) {
		if err := requireMCPValue("server_id", input.ServerID); err != nil {
			return cliRequest{}, err
		}
		return cliRequest{Command: "server.container.list", ID: strings.TrimSpace(input.ServerID)}, nil
	}, "Listed remote Docker containers.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "get_container_logs", Description: "Return a bounded tail of logs from one remote container. Secret values present in application logs are not automatically redacted.",
		Annotations: mcpReadAnnotations("Get remote container logs", true),
	}, mcpReadTimeout, func(input mcpContainerLogsInput) (cliRequest, error) {
		if err := requireMCPValue("server_id", input.ServerID); err != nil {
			return cliRequest{}, err
		}
		if err := requireMCPValue("container_id", input.ContainerID); err != nil {
			return cliRequest{}, err
		}
		if input.Tail < 0 || input.Tail > 2000 {
			return cliRequest{}, errors.New("tail must be between 0 and 2000")
		}
		return cliRequest{Command: "server.container.logs", ID: strings.TrimSpace(input.ServerID), ContainerID: strings.TrimSpace(input.ContainerID), Tail: input.Tail}, nil
	}, "Returned remote container logs.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "check_port_availability", Description: "Check whether host ports are already published by containers on an existing server and suggest alternatives when occupied.",
		Annotations: mcpReadAnnotations("Check remote ports", true),
	}, mcpReadTimeout, func(input mcpPortAvailabilityInput) (cliRequest, error) {
		if err := requireMCPValue("server_id", input.ServerID); err != nil {
			return cliRequest{}, err
		}
		if len(input.Ports) == 0 || len(input.Ports) > 32 {
			return cliRequest{}, errors.New("ports must contain 1 to 32 values")
		}
		return cliRequest{Command: "server.port.check", ID: strings.TrimSpace(input.ServerID), Ports: input.Ports}, nil
	}, "Checked remote host-port availability.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "list_registries", Description: "List container registries configured in iDeploy. Credentials are never returned.",
		Annotations: mcpReadAnnotations("List registries", false),
	}, mcpReadTimeout, listRequest("registry.list"), "Listed iDeploy registries.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "get_build_proxy_settings", Description: "Return the global iDeploy BuildKit proxy policy and upstream mappings. Credentials are never returned.",
		Annotations: mcpReadAnnotations("Get build proxy settings", false),
	}, mcpReadTimeout, listRequest("build-proxy.get"), "Returned the iDeploy build proxy settings.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "get_operation", Description: "Return the final result of an idempotent configuration write by its request_id. Use this after operation_pending or an uncertain local control timeout.",
		Annotations: mcpReadAnnotations("Get configuration operation", false),
	}, mcpReadTimeout, func(input mcpOperationInput) (cliRequest, error) {
		if err := requireMCPValue("operation_id", input.OperationID); err != nil {
			return cliRequest{}, err
		}
		return cliRequest{Command: "operation.get", ID: strings.TrimSpace(input.OperationID)}, nil
	}, "Returned the configuration operation status.")

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
		Name: "preflight_deployment", Description: "Validate source files, base-image manifests through the configured build proxy, registry credentials, target Docker connectivity and architecture, remote workload name, and host-port availability before publishing. This reads external systems but does not deploy anything.",
		Annotations: mcpReadAnnotations("Preflight deployment", true),
	}, 2*time.Minute, func(input mcpPreflightDeploymentInput) (cliRequest, error) {
		if err := requireMCPValue("deployment_id", input.DeploymentID); err != nil {
			return cliRequest{}, err
		}
		if err := requireMCPValue("registry_id", input.RegistryID); err != nil {
			return cliRequest{}, err
		}
		return cliRequest{Command: "deployment.preflight", ID: strings.TrimSpace(input.DeploymentID), RegistryID: strings.TrimSpace(input.RegistryID)}, nil
	}, "Completed deployment preflight checks.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "inspect_deployment_runtime", Description: "Refresh and return the remote containers, image, ports, state, and health matched to one deployment configuration.",
		Annotations: mcpReadAnnotations("Inspect deployment runtime", true),
	}, mcpReadTimeout, func(input mcpDeploymentInput) (cliRequest, error) {
		if err := requireMCPValue("deployment_id", input.DeploymentID); err != nil {
			return cliRequest{}, err
		}
		return cliRequest{Command: "deployment.runtime", ID: strings.TrimSpace(input.DeploymentID)}, nil
	}, "Inspected the remote deployment runtime.")

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
		Name: "configure_build_proxy", Description: "Configure the global iDeploy BuildKit proxy policy used automatically by future MCP and desktop publishing. Rules map upstream registry hosts to proxy cache paths. This writes local configuration and does not start a build or change a remote workload.",
		Annotations: mcpConfigurationAnnotations("Configure build proxy"),
	}, mcpReadTimeout, func(input mcpConfigureBuildProxyInput) (cliRequest, error) {
		if err := requireMCPValue("mode", input.Mode); err != nil {
			return cliRequest{}, err
		}
		rules := make([]BuildProxyRule, 0, len(input.Rules))
		for _, rule := range input.Rules {
			rules = append(rules, BuildProxyRule{Upstream: rule.Upstream, Mirror: rule.Mirror})
		}
		settings, err := validateBuildProxySettings(BuildProxySettingsInput{Mode: input.Mode, Rules: rules})
		if err != nil {
			return cliRequest{}, err
		}
		return cliRequest{Command: "build-proxy.configure", RequestID: strings.TrimSpace(input.RequestID), BuildProxy: &settings}, nil
	}, "Configured the iDeploy build proxy policy.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "create_project", Description: "Create a project configuration in iDeploy and return its stable project ID. For a local source, provide an absolute path on the iDeploy machine. For a Git source, provide the repository and ref. This only writes local configuration and does not publish anything.",
		Annotations: mcpConfigurationAnnotations("Create project configuration"),
	}, mcpReadTimeout, func(input mcpCreateProjectInput) (cliRequest, error) {
		if err := requireMCPValue("name", input.Name); err != nil {
			return cliRequest{}, err
		}
		sourceKind := strings.TrimSpace(input.SourceKind)
		switch sourceKind {
		case "local":
			if err := requireMCPValue("local_path", input.LocalPath); err != nil {
				return cliRequest{}, err
			}
		case "git":
			if err := requireMCPValue("repository_url", input.RepositoryURL); err != nil {
				return cliRequest{}, err
			}
			if err := requireMCPValue("ref_value", input.RefValue); err != nil {
				return cliRequest{}, err
			}
			if strings.TrimSpace(input.RefKind) == "" {
				input.RefKind = "branch"
			}
		default:
			return cliRequest{}, errors.New("source_kind must be local or git")
		}
		return cliRequest{Command: "project.create", RequestID: strings.TrimSpace(input.RequestID), Project: &ProjectInput{
			Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description), SourceKind: sourceKind,
			LocalPath: strings.TrimSpace(input.LocalPath), RepositoryURL: strings.TrimSpace(input.RepositoryURL),
			RefKind: strings.TrimSpace(input.RefKind), RefValue: strings.TrimSpace(input.RefValue),
			ContextPath: strings.TrimSpace(input.ContextPath), DockerfilePath: strings.TrimSpace(input.DockerfilePath),
			ComposePath: strings.TrimSpace(input.ComposePath), ImageRepository: strings.TrimSpace(input.ImageRepository),
		}}, nil
	}, "Created the iDeploy project configuration.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "update_project", Description: "Update selected fields of an existing iDeploy project configuration. Omitted fields are preserved. This does not publish or change remote workloads.",
		Annotations: mcpConfigurationAnnotations("Update project configuration"),
	}, mcpReadTimeout, func(input mcpUpdateProjectInput) (cliRequest, error) {
		if err := requireMCPValue("project_id", input.ProjectID); err != nil {
			return cliRequest{}, err
		}
		return cliRequest{Command: "project.update", ID: strings.TrimSpace(input.ProjectID), RequestID: strings.TrimSpace(input.RequestID), ProjectPatch: &ProjectPatchInput{
			Name: input.Name, Description: input.Description, SourceKind: input.SourceKind,
			LocalPath: input.LocalPath, RepositoryURL: input.RepositoryURL, RefKind: input.RefKind,
			RefValue: input.RefValue, ContextPath: input.ContextPath, DockerfilePath: input.DockerfilePath,
			ComposePath: input.ComposePath, ImageRepository: input.ImageRepository,
		}}, nil
	}, "Updated the iDeploy project configuration.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "create_deployment", Description: "Create an iDeploy deployment configuration linking an existing project to an existing server, and return its stable deployment ID. This only writes local configuration; call publish_deployment separately to build and deploy it.",
		Annotations: mcpConfigurationAnnotations("Create deployment configuration"),
	}, mcpReadTimeout, func(input mcpCreateDeploymentInput) (cliRequest, error) {
		for _, field := range []struct{ name, value string }{
			{"project_id", input.ProjectID}, {"server_id", input.ServerID},
			{"workload_kind", input.WorkloadKind}, {"workload_name", input.WorkloadName},
		} {
			if err := requireMCPValue(field.name, field.value); err != nil {
				return cliRequest{}, err
			}
		}
		workloadKind := strings.TrimSpace(input.WorkloadKind)
		if workloadKind != "container" && workloadKind != "compose" {
			return cliRequest{}, errors.New("workload_kind must be container or compose")
		}
		restartPolicy := strings.TrimSpace(input.RestartPolicy)
		if restartPolicy == "" {
			restartPolicy = "unless-stopped"
		}
		return cliRequest{Command: "deployment.create", RequestID: strings.TrimSpace(input.RequestID), Deployment: &DeploymentInput{
			ProjectID: strings.TrimSpace(input.ProjectID), ServerID: strings.TrimSpace(input.ServerID),
			WorkloadKind: workloadKind, WorkloadName: strings.TrimSpace(input.WorkloadName),
			ContainerPorts: input.ContainerPorts, RestartPolicy: restartPolicy,
		}}, nil
	}, "Created the iDeploy deployment configuration.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "update_deployment", Description: "Update selected fields of an existing iDeploy deployment configuration, including ports, name, restart policy, project, server, or workload type. Omitted fields are preserved. This does not publish the change.",
		Annotations: mcpConfigurationAnnotations("Update deployment configuration"),
	}, mcpReadTimeout, func(input mcpUpdateDeploymentInput) (cliRequest, error) {
		if err := requireMCPValue("deployment_id", input.DeploymentID); err != nil {
			return cliRequest{}, err
		}
		return cliRequest{Command: "deployment.update", ID: strings.TrimSpace(input.DeploymentID), RequestID: strings.TrimSpace(input.RequestID), DeploymentPatch: &DeploymentPatchInput{
			ProjectID: input.ProjectID, ServerID: input.ServerID, WorkloadKind: input.WorkloadKind,
			WorkloadName: input.WorkloadName, ContainerPorts: input.ContainerPorts, RestartPolicy: input.RestartPolicy,
		}}, nil
	}, "Updated the iDeploy deployment configuration.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "clone_deployment", Description: "Clone an existing local deployment configuration for another server or workload name. The clone has no release history and does not change any remote workload.",
		Annotations: mcpConfigurationAnnotations("Clone deployment configuration"),
	}, mcpReadTimeout, func(input mcpCloneDeploymentInput) (cliRequest, error) {
		if err := requireMCPValue("deployment_id", input.DeploymentID); err != nil {
			return cliRequest{}, err
		}
		if err := requireMCPValue("server_id", input.ServerID); err != nil {
			return cliRequest{}, err
		}
		if err := requireMCPValue("workload_name", input.WorkloadName); err != nil {
			return cliRequest{}, err
		}
		serverID := strings.TrimSpace(input.ServerID)
		workloadName := strings.TrimSpace(input.WorkloadName)
		return cliRequest{Command: "deployment.clone", ID: strings.TrimSpace(input.DeploymentID), RequestID: strings.TrimSpace(input.RequestID), DeploymentPatch: &DeploymentPatchInput{
			ProjectID: input.ProjectID, ServerID: &serverID, WorkloadKind: input.WorkloadKind,
			WorkloadName: &workloadName, ContainerPorts: input.ContainerPorts, RestartPolicy: input.RestartPolicy,
		}}, nil
	}, "Cloned the iDeploy deployment configuration.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "delete_project", Description: "Delete only an iDeploy project configuration after explicit confirmation. Source files, images, and remote workloads are never deleted.",
		Annotations: mcpLocalDeleteAnnotations("Delete project configuration"),
	}, mcpReadTimeout, func(input mcpDeleteProjectInput) (cliRequest, error) {
		if err := requireMCPValue("project_id", input.ProjectID); err != nil {
			return cliRequest{}, err
		}
		if !input.Confirm {
			return cliRequest{}, errors.New("confirm must be true")
		}
		return cliRequest{Command: "project.delete", ID: strings.TrimSpace(input.ProjectID), RequestID: strings.TrimSpace(input.RequestID)}, nil
	}, "Deleted the local iDeploy project configuration.")

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "delete_deployment", Description: "Delete only an iDeploy deployment configuration after explicit confirmation. Remote containers and images are never deleted.",
		Annotations: mcpLocalDeleteAnnotations("Delete deployment configuration"),
	}, mcpReadTimeout, func(input mcpDeleteDeploymentInput) (cliRequest, error) {
		if err := requireMCPValue("deployment_id", input.DeploymentID); err != nil {
			return cliRequest{}, err
		}
		if !input.Confirm {
			return cliRequest{}, errors.New("confirm must be true")
		}
		return cliRequest{Command: "deployment.delete", ID: strings.TrimSpace(input.DeploymentID), RequestID: strings.TrimSpace(input.RequestID)}, nil
	}, "Deleted the local iDeploy deployment configuration.")

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

	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "retry_release", Description: "Retry a failed or canceled release with the same version. Failures before push are rebuilt; failures with a retained immutable image Digest or Compose snapshot resume remote deployment without rebuilding or overwriting the tag.",
		Annotations: mcpMutationAnnotations("Retry failed release"),
	}, mcpOperationTimeout, func(input mcpRetryReleaseInput) (cliRequest, error) {
		if err := requireMCPValue("release_id", input.ReleaseID); err != nil {
			return cliRequest{}, err
		}
		timeout, err := mcpTimeout(input.TimeoutSeconds, mcpOperationTimeout)
		if err != nil {
			return cliRequest{}, err
		}
		return cliRequest{Command: "release.retry", ReleaseID: strings.TrimSpace(input.ReleaseID), Wait: mcpWait(input.Wait), TimeoutMS: timeout.Milliseconds()}, nil
	}, "Release retry completed or was queued.")

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

func mcpConfigurationAnnotations(title string) *mcp.ToolAnnotations {
	destructive, openWorld := false, false
	return &mcp.ToolAnnotations{
		Title: title, ReadOnlyHint: false, IdempotentHint: false,
		DestructiveHint: &destructive, OpenWorldHint: &openWorld,
	}
}

func mcpLocalDeleteAnnotations(title string) *mcp.ToolAnnotations {
	destructive, openWorld := true, false
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
