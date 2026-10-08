package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"ideploy/internal/deployment"
)

type DeploymentEnvironmentVariableInput struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
}

type DeploymentEnvironmentUpdateInput struct {
	DeploymentID string                               `json:"deploymentId"`
	Variables    []DeploymentEnvironmentVariableInput `json:"variables"`
	Remove       []string                             `json:"remove"`
}

type DeploymentSecretPromptInput struct {
	DeploymentID string `json:"deploymentId"`
	Name         string `json:"name"`
}

type DeploymentEnvironmentVariableView struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Value      string `json:"value,omitempty"`
	Configured bool   `json:"configured"`
}

type DeploymentEnvironmentView struct {
	DeploymentID    string                              `json:"deploymentId"`
	Revision        int64                               `json:"revision"`
	AppliedRevision int64                               `json:"appliedRevision"`
	PendingApply    bool                                `json:"pendingApply"`
	Variables       []DeploymentEnvironmentVariableView `json:"variables"`
}

func (s *SystemService) ListDeploymentEnvironment(deploymentID string) (DeploymentEnvironmentView, error) {
	store, err := s.deploymentStore()
	if err != nil {
		return DeploymentEnvironmentView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	target, err := store.Get(ctx, strings.TrimSpace(deploymentID))
	if err != nil {
		return DeploymentEnvironmentView{}, err
	}
	values, err := store.ListEnvironment(ctx, target.ID)
	if err != nil {
		return DeploymentEnvironmentView{}, err
	}
	return deploymentEnvironmentView(target, values), nil
}

func (s *SystemService) UpdateDeploymentEnvironment(input DeploymentEnvironmentUpdateInput) (DeploymentEnvironmentView, error) {
	store, err := s.deploymentStore()
	if err != nil {
		return DeploymentEnvironmentView{}, err
	}
	input.DeploymentID = strings.TrimSpace(input.DeploymentID)
	if input.DeploymentID == "" {
		return DeploymentEnvironmentView{}, errors.New("deployment_id is required")
	}
	if len(input.Variables) > 128 || len(input.Remove) > 128 {
		return DeploymentEnvironmentView{}, errors.New("单个部署最多配置 128 个环境变量")
	}
	removeNames := make(map[string]struct{}, len(input.Remove))
	for _, rawName := range input.Remove {
		name, nameErr := deployment.ValidateEnvironmentName(rawName)
		if nameErr != nil {
			return DeploymentEnvironmentView{}, nameErr
		}
		removeNames[name] = struct{}{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	target, err := store.Get(ctx, input.DeploymentID)
	if err != nil {
		return DeploymentEnvironmentView{}, err
	}
	if target.WorkloadKind != "container" {
		return DeploymentEnvironmentView{}, errors.New("当前版本只支持单容器部署环境变量")
	}
	existing, err := store.ListEnvironment(ctx, target.ID)
	if err != nil {
		return DeploymentEnvironmentView{}, err
	}
	byName := make(map[string]deployment.EnvironmentVariable, len(existing))
	for _, value := range existing {
		byName[value.Name] = value
	}

	changes := make([]deployment.EnvironmentChange, 0, len(input.Variables))
	newSecretRefs := make([]string, 0)
	for _, variable := range input.Variables {
		name, err := deployment.ValidateEnvironmentName(variable.Name)
		if err != nil {
			cleanupEnvironmentSecrets(ctx, newSecretRefs)
			return DeploymentEnvironmentView{}, fmt.Errorf("%s: %w", variable.Name, err)
		}
		if _, removing := removeNames[name]; removing {
			cleanupEnvironmentSecrets(ctx, newSecretRefs)
			return DeploymentEnvironmentView{}, fmt.Errorf("环境变量 %s 不能同时更新和删除", name)
		}
		kind := strings.TrimSpace(variable.Kind)
		if kind == deployment.EnvironmentKindPlain {
			changes = append(changes, deployment.EnvironmentChange{Name: name, Kind: kind, PlainValue: variable.Value})
			continue
		}
		if kind != deployment.EnvironmentKindSecret {
			cleanupEnvironmentSecrets(ctx, newSecretRefs)
			return DeploymentEnvironmentView{}, fmt.Errorf("环境变量 %s 类型无效", name)
		}
		if variable.Value == "" {
			current, ok := byName[name]
			if !ok || current.Kind != deployment.EnvironmentKindSecret || current.CredentialRef == "" {
				cleanupEnvironmentSecrets(ctx, newSecretRefs)
				return DeploymentEnvironmentView{}, fmt.Errorf("秘密变量 %s 尚未填写", name)
			}
			changes = append(changes, deployment.EnvironmentChange{ID: current.ID, Name: name, Kind: kind, CredentialRef: current.CredentialRef})
			continue
		}
		id, err := deployment.NewEnvironmentVariableID()
		if err != nil {
			cleanupEnvironmentSecrets(ctx, newSecretRefs)
			return DeploymentEnvironmentView{}, err
		}
		ref := deployment.EnvironmentSecretCredentialRef(id)
		if err := deployment.SaveEnvironmentSecret(ctx, ref, variable.Value); err != nil {
			cleanupEnvironmentSecrets(ctx, newSecretRefs)
			return DeploymentEnvironmentView{}, fmt.Errorf("秘密变量 %s 保存失败: %w", name, err)
		}
		newSecretRefs = append(newSecretRefs, ref)
		changes = append(changes, deployment.EnvironmentChange{ID: id, Name: name, Kind: kind, CredentialRef: ref})
	}

	values, staleRefs, err := store.ApplyEnvironmentChanges(ctx, target.ID, changes, input.Remove)
	if err != nil {
		cleanupEnvironmentSecrets(ctx, newSecretRefs)
		return DeploymentEnvironmentView{}, err
	}
	cleanupEnvironmentSecrets(ctx, staleRefs)
	target, err = store.Get(ctx, target.ID)
	if err != nil {
		return DeploymentEnvironmentView{}, err
	}
	view := deploymentEnvironmentView(target, values)
	if deploymentView, viewErr := s.deploymentViewByID(ctx, target.ID); viewErr == nil {
		s.emitDeploymentView(deploymentView)
	}
	return view, nil
}

// PromptDeploymentSecret opens a local macOS secure text prompt. The secret is
// returned only to this process, saved immediately to Keychain, and never sent
// through MCP or included in the returned view.
func (s *SystemService) PromptDeploymentSecret(input DeploymentSecretPromptInput) (DeploymentEnvironmentView, error) {
	if runtime.GOOS != "darwin" {
		return DeploymentEnvironmentView{}, errors.New("安全秘密输入目前仅支持 macOS")
	}
	input.DeploymentID = strings.TrimSpace(input.DeploymentID)
	name, err := deployment.ValidateEnvironmentName(input.Name)
	if err != nil {
		return DeploymentEnvironmentView{}, err
	}
	if input.DeploymentID == "" {
		return DeploymentEnvironmentView{}, errors.New("deployment_id is required")
	}
	const script = `on run argv
set variableName to item 1 of argv
set answer to display dialog ("为部署变量 “" & variableName & "” 输入秘密值。该值只会保存到 macOS 钥匙串。") default answer "" with hidden answer buttons {"取消", "保存"} default button "保存" cancel button "取消" with title "iDeploy 安全输入"
return text returned of answer
end run`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "osascript", "-e", script, name).Output()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return DeploymentEnvironmentView{}, errors.New("安全输入等待超时")
		}
		return DeploymentEnvironmentView{}, errors.New("用户取消了秘密输入")
	}
	secret := strings.TrimSuffix(string(output), "\n")
	if secret == "" {
		return DeploymentEnvironmentView{}, errors.New("秘密值不能为空")
	}
	return s.UpdateDeploymentEnvironment(DeploymentEnvironmentUpdateInput{
		DeploymentID: input.DeploymentID,
		Variables:    []DeploymentEnvironmentVariableInput{{Name: name, Kind: deployment.EnvironmentKindSecret, Value: secret}},
	})
}

func (s *SystemService) deploymentViewByID(ctx context.Context, id string) (DeploymentView, error) {
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
	target, err := deployments.Get(ctx, id)
	if err != nil {
		return DeploymentView{}, err
	}
	return deploymentViewFor(ctx, projects, servers, target)
}

func deploymentEnvironmentView(target deployment.Target, values []deployment.EnvironmentVariable) DeploymentEnvironmentView {
	result := DeploymentEnvironmentView{
		DeploymentID: target.ID, Revision: target.EnvironmentRevision,
		AppliedRevision: target.AppliedEnvironmentRevision,
		PendingApply:    target.EnvironmentRevision != target.AppliedEnvironmentRevision,
		Variables:       make([]DeploymentEnvironmentVariableView, 0, len(values)),
	}
	for _, value := range values {
		view := DeploymentEnvironmentVariableView{Name: value.Name, Kind: value.Kind, Configured: value.Kind == deployment.EnvironmentKindPlain || value.CredentialRef != ""}
		if value.Kind == deployment.EnvironmentKindPlain {
			view.Value = value.PlainValue
		}
		result.Variables = append(result.Variables, view)
	}
	return result
}

func (s *SystemService) resolveDeploymentEnvironment(ctx context.Context, deploymentID string) (map[string]string, error) {
	store, err := s.deploymentStore()
	if err != nil {
		return nil, err
	}
	values, err := store.ListEnvironment(ctx, deploymentID)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(values))
	for _, value := range values {
		if value.Kind == deployment.EnvironmentKindPlain {
			result[value.Name] = value.PlainValue
			continue
		}
		secret, err := deployment.LoadEnvironmentSecret(ctx, value.CredentialRef)
		if err != nil {
			return nil, fmt.Errorf("秘密变量 %s 未配置或不可用", value.Name)
		}
		result[value.Name] = secret
	}
	return result, nil
}

func (s *SystemService) resolveDeploymentEnvironmentRevision(ctx context.Context, store *deployment.Store, deploymentID string) (deployment.Target, map[string]string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		before, err := store.Get(ctx, deploymentID)
		if err != nil {
			return deployment.Target{}, nil, err
		}
		environment, err := s.resolveDeploymentEnvironment(ctx, deploymentID)
		if err != nil {
			return deployment.Target{}, nil, err
		}
		after, err := store.Get(ctx, deploymentID)
		if err != nil {
			return deployment.Target{}, nil, err
		}
		if before.EnvironmentRevision == after.EnvironmentRevision {
			return after, environment, nil
		}
	}
	return deployment.Target{}, nil, errors.New("运行配置正在被修改，请稍后重试")
}

func cleanupEnvironmentSecrets(ctx context.Context, refs []string) {
	for _, ref := range refs {
		_ = deployment.DeleteEnvironmentSecret(ctx, ref)
	}
}
