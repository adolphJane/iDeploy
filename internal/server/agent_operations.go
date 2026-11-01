package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type agentOperationState struct {
	ID     string          `json:"id"`
	Status string          `json:"status"`
	Error  string          `json:"error"`
	Result json.RawMessage `json:"result"`
}

// ControlContainer uses a remote operation journal when paired. A transport
// failure after submission is ambiguous and must never trigger an SSH retry.
func (r *Remote) ControlContainer(ctx context.Context, connection Connection, serverID, operationID, action, containerID string) error {
	if !validContainerID(containerID) || action != "start" && action != "stop" && action != "restart" {
		return errors.New("invalid container control request")
	}
	request := map[string]string{"id": operationID, "kind": "container." + action, "containerId": containerID}
	_, err := r.runAgentOperation(ctx, connection, serverID, operationID, "container.control", request, func() (json.RawMessage, error) {
		return nil, r.OperateContainer(ctx, connection, action, containerID)
	})
	return err
}

func AgentStageID(jobID, stage string) string {
	sum := sha256.Sum256([]byte(jobID + ":" + stage))
	return fmt.Sprintf("%x", sum[:16])
}

func (r *Remote) PullImageWithAgent(ctx context.Context, connection Connection, serverID, jobID, image string) error {
	request := map[string]string{"id": AgentStageID(jobID, "image.pull:"+image), "kind": "image.pull", "image": image}
	_, err := r.runAgentOperation(ctx, connection, serverID, request["id"], "image.pull", request, func() (json.RawMessage, error) { return nil, r.PullImage(ctx, connection, image) })
	return err
}

func (r *Remote) TagImageWithAgent(ctx context.Context, connection Connection, serverID, jobID, source, target string) error {
	request := map[string]string{"id": AgentStageID(jobID, "image.tag:"+target), "kind": "image.tag", "image": source, "targetImage": target}
	_, err := r.runAgentOperation(ctx, connection, serverID, request["id"], "image.tag", request, func() (json.RawMessage, error) { return nil, r.TagImage(ctx, connection, source, target) })
	return err
}

func (r *Remote) DeployContainerWithAgent(ctx context.Context, connection Connection, serverID, jobID, name, image, restartPolicy string, ports []string, labels, environment map[string]string) (ContainerReplacement, error) {
	id := AgentStageID(jobID, "container.deploy:"+name)
	request := map[string]any{"id": id, "kind": "container.deploy", "containerId": name, "image": image, "restartPolicy": restartPolicy, "ports": ports, "labels": labels, "environment": environment}
	result, err := r.runAgentOperation(ctx, connection, serverID, id, "container.deploy", request, func() (json.RawMessage, error) {
		replacement, err := r.DeployContainer(ctx, connection, name, image, restartPolicy, ports, labels, environment)
		if err != nil {
			return nil, err
		}
		data, _ := json.Marshal(replacement)
		return data, nil
	})
	if err != nil {
		return ContainerReplacement{}, err
	}
	var replacement ContainerReplacement
	if err := json.Unmarshal(result, &replacement); err != nil {
		return ContainerReplacement{}, err
	}
	if replacement.Name != name || !validContainerID(replacement.BackupName) {
		return ContainerReplacement{}, errors.New("Agent returned invalid container replacement")
	}
	return replacement, nil
}

func (r *Remote) CommitContainerReplacementWithAgent(ctx context.Context, connection Connection, serverID, jobID string, replacement ContainerReplacement) error {
	id := AgentStageID(jobID, "container.commit:"+replacement.Name)
	request := map[string]any{"id": id, "kind": "container.commit", "containerId": replacement.Name, "backupName": replacement.BackupName, "hadPrevious": replacement.HadPrevious}
	_, err := r.runAgentOperation(ctx, connection, serverID, id, "container.deploy", request, func() (json.RawMessage, error) {
		return nil, r.CommitContainerReplacement(ctx, connection, replacement)
	})
	return err
}

func (r *Remote) RollbackContainerReplacementWithAgent(ctx context.Context, connection Connection, serverID, jobID string, replacement ContainerReplacement) error {
	id := AgentStageID(jobID, "container.rollback:"+replacement.Name)
	request := map[string]any{"id": id, "kind": "container.rollback", "containerId": replacement.Name, "backupName": replacement.BackupName, "hadPrevious": replacement.HadPrevious}
	_, err := r.runAgentOperation(ctx, connection, serverID, id, "container.deploy", request, func() (json.RawMessage, error) {
		return nil, r.RollbackContainerReplacement(ctx, connection, replacement)
	})
	return err
}

func (r *Remote) UploadComposeWithAgent(ctx context.Context, connection Connection, serverID, jobID, deploymentID, manifest string) error {
	id := AgentStageID(jobID, "compose.upload:"+deploymentID)
	req := map[string]any{"id": id, "kind": "compose.upload", "deploymentId": deploymentID, "manifest": manifest}
	_, err := r.runAgentOperation(ctx, connection, serverID, id, "compose.deploy", req, func() (json.RawMessage, error) { return nil, r.UploadCompose(ctx, connection, deploymentID, manifest) })
	return err
}

func (r *Remote) PullComposeWithAgent(ctx context.Context, connection Connection, serverID, jobID, deploymentID, projectName, version string) error {
	id := AgentStageID(jobID, "compose.pull:"+deploymentID)
	req := map[string]any{"id": id, "kind": "compose.pull", "deploymentId": deploymentID, "projectName": projectName, "version": version}
	_, err := r.runAgentOperation(ctx, connection, serverID, id, "compose.deploy", req, func() (json.RawMessage, error) {
		return nil, r.PullCompose(ctx, connection, deploymentID, projectName, version)
	})
	return err
}

func (r *Remote) UpComposeWithAgent(ctx context.Context, connection Connection, serverID, jobID, deploymentID, projectName, version string) error {
	id := AgentStageID(jobID, "compose.up:"+deploymentID)
	req := map[string]any{"id": id, "kind": "compose.up", "deploymentId": deploymentID, "projectName": projectName, "version": version}
	_, err := r.runAgentOperation(ctx, connection, serverID, id, "compose.deploy", req, func() (json.RawMessage, error) {
		return nil, r.UpCompose(ctx, connection, deploymentID, projectName, version)
	})
	return err
}

func (r *Remote) ApplyExistingNginxWithAgent(ctx context.Context, connection Connection, serverID, jobID string, routes []IngressRoute, expectedHash, expectedContainer string) (string, error) {
	item, config, _, err := r.PreviewExistingNginx(ctx, connection, routes, expectedHash)
	if err != nil {
		return "", err
	}
	if expectedContainer != "" && item.Container != expectedContainer {
		return "", errors.New("Nginx container changed since preview")
	}
	if config == item.Config {
		return item.Hash, nil
	}
	id := AgentStageID(jobID, "nginx.apply:"+item.Container)
	req := map[string]any{"id": id, "kind": "nginx.apply", "nginxContainer": item.Container, "nginxConfig": config, "expectedHash": item.Hash}
	result, err := r.runAgentOperation(ctx, connection, serverID, id, "nginx.apply", req, func() (json.RawMessage, error) {
		hash, err := r.ApplyExistingNginx(ctx, connection, routes, expectedHash, expectedContainer)
		if err != nil {
			return nil, err
		}
		data, _ := json.Marshal(hash)
		return data, nil
	})
	if err != nil {
		return "", err
	}
	var hash string
	if err := json.Unmarshal(result, &hash); err != nil {
		return "", err
	}
	if len(hash) != 64 {
		return "", errors.New("Agent returned invalid Nginx config hash")
	}
	return hash, nil
}

type AgentCertificateInfo struct {
	Certificate    string `json:"certificate"`
	CertificateKey string `json:"certificateKey"`
	ExpiresAt      string `json:"expiresAt"`
}

func (r *Remote) IssueCertificateWithAgent(ctx context.Context, connection Connection, serverID, jobID, domain, email, webroot, nginxContainer string) (AgentCertificateInfo, error) {
	id := AgentStageID(jobID, "certbot.issue:"+domain)
	req := map[string]string{"id": id, "kind": "certbot.issue", "fqdn": domain, "email": email, "webrootHost": webroot, "nginxContainer": nginxContainer}
	result, err := r.runAgentOperation(ctx, connection, serverID, id, "certbot.issue", req, func() (json.RawMessage, error) {
		return nil, errors.New("Certbot issuance requires an installed Agent")
	})
	if err != nil {
		return AgentCertificateInfo{}, err
	}
	var info AgentCertificateInfo
	if err := json.Unmarshal(result, &info); err != nil {
		return AgentCertificateInfo{}, err
	}
	if info.Certificate == "" || info.CertificateKey == "" {
		return AgentCertificateInfo{}, errors.New("Agent did not return certificate paths")
	}
	return info, nil
}

func (r *Remote) RenewCertificatesWithAgent(ctx context.Context, connection Connection, serverID, jobID string) error {
	id := AgentStageID(jobID, "certbot.renew")
	req := map[string]string{"id": id, "kind": "certbot.renew"}
	_, err := r.runAgentOperation(ctx, connection, serverID, id, "certbot.renew", req, func() (json.RawMessage, error) { return nil, errors.New("Certbot renewal requires an installed Agent") })
	return err
}

func (r *Remote) runAgentOperation(ctx context.Context, connection Connection, serverID, operationID, capability string, request any, legacy func() (json.RawMessage, error)) (json.RawMessage, error) {
	if _, err := LoadPassword(ctx, AgentPairingCredentialRef(serverID)); err != nil {
		return legacy()
	}
	helloBytes, err := r.AgentGET(ctx, connection, serverID, "/v1/hello")
	if err != nil {
		return nil, fmt.Errorf("Agent 不可用，未执行操作: %w", err)
	}
	var hello struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(helloBytes, &hello); err != nil {
		return nil, err
	}
	supported := false
	for _, item := range hello.Capabilities {
		if item == capability {
			supported = true
			break
		}
	}
	if !supported {
		return legacy()
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if _, err := r.AgentOperation(ctx, connection, serverID, body); err != nil {
		if _, statusErr := r.AgentOperationStatus(ctx, connection, serverID, operationID); statusErr != nil {
			return nil, fmt.Errorf("Agent 操作提交结果不确定，请按操作 ID %s 查询，不会自动重试: %w", operationID, err)
		}
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		data, err := r.AgentOperationStatus(ctx, connection, serverID, operationID)
		if err != nil {
			return nil, fmt.Errorf("Agent 已接收操作 %s，但状态读取失败；请刷新后核对: %w", operationID, err)
		}
		var state agentOperationState
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, err
		}
		switch state.Status {
		case "succeeded":
			return state.Result, nil
		case "failed", "uncertain":
			return nil, fmt.Errorf("Agent 操作 %s: %s", state.Status, state.Error)
		case "running":
		default:
			return nil, fmt.Errorf("Agent 返回未知操作状态 %q", state.Status)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("Agent 操作 %s 仍可能运行: %w", operationID, ctx.Err())
		case <-ticker.C:
		}
	}
}
