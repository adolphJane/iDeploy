package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"ideploy/internal/deployment"
)

type ProjectPatchInput struct {
	Name            *string `json:"name,omitempty"`
	Description     *string `json:"description,omitempty"`
	SourceKind      *string `json:"sourceKind,omitempty"`
	LocalPath       *string `json:"localPath,omitempty"`
	RepositoryURL   *string `json:"repositoryUrl,omitempty"`
	RefKind         *string `json:"refKind,omitempty"`
	RefValue        *string `json:"refValue,omitempty"`
	ContextPath     *string `json:"contextPath,omitempty"`
	DockerfilePath  *string `json:"dockerfilePath,omitempty"`
	ComposePath     *string `json:"composePath,omitempty"`
	ImageRepository *string `json:"imageRepository,omitempty"`
}

type DeploymentPatchInput struct {
	ProjectID      *string   `json:"projectId,omitempty"`
	ServerID       *string   `json:"serverId,omitempty"`
	WorkloadKind   *string   `json:"workloadKind,omitempty"`
	WorkloadName   *string   `json:"workloadName,omitempty"`
	ContainerPorts *[]string `json:"containerPorts,omitempty"`
	RestartPolicy  *string   `json:"restartPolicy,omitempty"`
}

func (s *SystemService) PatchProject(id string, patch ProjectPatchInput) (ProjectView, error) {
	projects, err := s.projectStore()
	if err != nil {
		return ProjectView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	current, err := projects.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return ProjectView{}, err
	}
	input := ProjectInput{
		Name: current.Name, Description: current.Description, SourceKind: string(current.SourceKind),
		LocalPath: current.LocalPath, RepositoryURL: current.RepositoryURL, RefKind: string(current.RefKind),
		RefValue: current.RefValue, ContextPath: current.ContextPath, DockerfilePath: current.DockerfilePath,
		ComposePath: current.ComposePath, ImageRepository: current.ImageRepository,
	}
	applyStringPatch(&input.Name, patch.Name)
	applyStringPatch(&input.Description, patch.Description)
	applyStringPatch(&input.SourceKind, patch.SourceKind)
	applyStringPatch(&input.LocalPath, patch.LocalPath)
	applyStringPatch(&input.RepositoryURL, patch.RepositoryURL)
	applyStringPatch(&input.RefKind, patch.RefKind)
	applyStringPatch(&input.RefValue, patch.RefValue)
	applyStringPatch(&input.ContextPath, patch.ContextPath)
	applyStringPatch(&input.DockerfilePath, patch.DockerfilePath)
	applyStringPatch(&input.ComposePath, patch.ComposePath)
	applyStringPatch(&input.ImageRepository, patch.ImageRepository)
	return s.UpdateProject(current.ID, input)
}

func (s *SystemService) PatchDeployment(id string, patch DeploymentPatchInput) (DeploymentView, error) {
	deployments, err := s.deploymentStore()
	if err != nil {
		return DeploymentView{}, err
	}
	projects, err := s.projectStore()
	if err != nil {
		return DeploymentView{}, err
	}
	servers, err := s.serverStore()
	if err != nil {
		return DeploymentView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	current, err := deployments.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return DeploymentView{}, err
	}
	input := deployment.Input{
		ProjectID: current.ProjectID, ServerID: current.ServerID, WorkloadKind: current.WorkloadKind,
		WorkloadName: current.WorkloadName, ContainerPorts: append([]string(nil), current.ContainerPorts...),
		RestartPolicy: current.RestartPolicy,
	}
	applyStringPatch(&input.ProjectID, patch.ProjectID)
	applyStringPatch(&input.ServerID, patch.ServerID)
	applyStringPatch(&input.WorkloadKind, patch.WorkloadKind)
	applyStringPatch(&input.WorkloadName, patch.WorkloadName)
	applyStringPatch(&input.RestartPolicy, patch.RestartPolicy)
	if patch.ContainerPorts != nil {
		input.ContainerPorts = append([]string(nil), (*patch.ContainerPorts)...)
	}
	if _, err := projects.Get(ctx, input.ProjectID); err != nil {
		return DeploymentView{}, err
	}
	if _, err := servers.Get(ctx, input.ServerID); err != nil {
		return DeploymentView{}, err
	}
	updated, err := deployments.Update(ctx, current.ID, input)
	if err != nil {
		return DeploymentView{}, err
	}
	view, err := deploymentViewFor(ctx, projects, servers, updated)
	if err != nil {
		return DeploymentView{}, err
	}
	s.emitDeploymentView(view)
	return view, nil
}

// CloneDeployment creates a new local deployment configuration from an
// existing one. Callers normally override the server and workload name because
// both project/server and server/workload pairs are unique.
func (s *SystemService) CloneDeployment(id string, patch DeploymentPatchInput) (DeploymentView, error) {
	deployments, err := s.deploymentStore()
	if err != nil {
		return DeploymentView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	current, err := deployments.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return DeploymentView{}, err
	}
	input := DeploymentInput{
		ProjectID: current.ProjectID, ServerID: current.ServerID, WorkloadKind: current.WorkloadKind,
		WorkloadName: current.WorkloadName, ContainerPorts: append([]string(nil), current.ContainerPorts...),
		RestartPolicy: current.RestartPolicy,
	}
	applyStringPatch(&input.ProjectID, patch.ProjectID)
	applyStringPatch(&input.ServerID, patch.ServerID)
	applyStringPatch(&input.WorkloadKind, patch.WorkloadKind)
	applyStringPatch(&input.WorkloadName, patch.WorkloadName)
	applyStringPatch(&input.RestartPolicy, patch.RestartPolicy)
	if patch.ContainerPorts != nil {
		input.ContainerPorts = append([]string(nil), (*patch.ContainerPorts)...)
	}
	return s.CreateDeployment(input)
}

func applyStringPatch(target *string, value *string) {
	if value != nil {
		*target = *value
	}
}

func (s *SystemService) DeleteProjectConfiguration(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("project_id is required")
	}
	return s.DeleteProject(id)
}

func (s *SystemService) DeleteDeploymentConfiguration(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("deployment_id is required")
	}
	return s.DeleteDeployment(id)
}
