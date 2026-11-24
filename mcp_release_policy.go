package main

import (
	"context"
	_ "embed"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"ideploy/internal/deployment"
)

//go:embed .agents/skills/ideploy-release/SKILL.md
var releaseSkill string

type mcpRuntimePolicyInput struct {
	DeploymentID     string                     `json:"deployment_id" jsonschema:"stable deployment identifier"`
	RequestID        string                     `json:"request_id,omitempty" jsonschema:"stable idempotency key for this policy update"`
	ExpectedRevision int64                      `json:"expected_revision" jsonschema:"current policy revision; use 0 to create the first policy"`
	Mode             string                     `json:"mode" jsonschema:"observe records evidence; enforce blocks failed verification"`
	Services         []string                   `json:"services,omitempty" jsonschema:"required service names for a Compose workload"`
	Mounts           []deployment.RuntimeMount  `json:"mounts,omitempty" jsonschema:"expected bind and named volume mounts; use actual Docker volume names"`
	Directories      []deployment.DataDirectory `json:"directories,omitempty" jsonschema:"registered container data directories"`
}

type mcpReleaseVerificationInput struct {
	ReleaseID string `json:"release_id" jsonschema:"stable release identifier returned by list_releases"`
}

func registerReleasePolicyTools(server *mcp.Server, service *mcpService) {
	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "get_deployment_runtime_policy", Description: "Read the registered storage policy and its revision for an iDeploy deployment.",
		Annotations: mcpReadAnnotations("Get deployment runtime policy", false),
	}, mcpReadTimeout, func(input mcpDeploymentInput) (cliRequest, error) {
		return cliRequest{Command: "deployment.policy.get", ID: strings.TrimSpace(input.DeploymentID)}, requireMCPValue("deployment_id", input.DeploymentID)
	}, "Returned the publication policy.")
	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "update_deployment_runtime_policy", Description: "Save a new policy revision for an explicitly requested deployment configuration change. This does not publish or change remote files.",
		Annotations: mcpMutationAnnotations("Update deployment runtime policy"),
	}, mcpReadTimeout, func(input mcpRuntimePolicyInput) (cliRequest, error) {
		if err := requireMCPValue("deployment_id", input.DeploymentID); err != nil {
			return cliRequest{}, err
		}
		return cliRequest{
			Command: "deployment.policy.update", RequestID: strings.TrimSpace(input.RequestID),
			DeploymentRuntimePolicy: &DeploymentRuntimePolicyInput{
				DeploymentID: strings.TrimSpace(input.DeploymentID), ExpectedRevision: input.ExpectedRevision,
				Policy: deployment.RuntimePolicy{Mode: input.Mode, Services: input.Services, Mounts: input.Mounts, Directories: input.Directories},
			},
		}, nil
	}, "Saved the publication policy without starting a release.")
	registerMCPCommandTool(server, service, &mcp.Tool{
		Name: "get_release_runtime_verification", Description: "Read the policy revision and saved storage and image checks for one release. An empty stage has no saved checks.",
		Annotations: mcpReadAnnotations("Read release verification", false),
	}, mcpReadTimeout, func(input mcpReleaseVerificationInput) (cliRequest, error) {
		return cliRequest{Command: "release.runtime_verification", ID: strings.TrimSpace(input.ReleaseID)}, requireMCPValue("release_id", input.ReleaseID)
	}, "Returned the release verification.")
}

func registerReleasePrompt(server *mcp.Server) {
	sections := strings.SplitN(releaseSkill, "---\n", 3)
	server.AddPrompt(&mcp.Prompt{
		Name: "ideploy_release", Title: "Release with iDeploy",
		Description: "Plan, publish, and verify a release using the iDeploy MCP tools.",
	}, func(context.Context, *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		if len(sections) != 3 {
			return nil, errors.New("invalid embedded iDeploy release guide")
		}
		return &mcp.GetPromptResult{Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: strings.TrimSpace(sections[2])}},
		}}, nil
	})
}
