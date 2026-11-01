package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// AgentGET sends HTTP through SSH's direct TCP forwarding channel (-W).
// No remote shell command is run and the Agent remains bound to loopback.
func (r *Remote) AgentGET(parent context.Context, connection Connection, serverID, path string) ([]byte, error) {
	if path != "/v1/hello" && path != "/v1/metrics" && path != "/v1/inventory" && path != "/v1/nginx" {
		return nil, errors.New("unsupported Agent endpoint")
	}
	return r.agentRequest(parent, connection, serverID, http.MethodGet, path, nil)
}

func (r *Remote) AgentOperation(parent context.Context, connection Connection, serverID string, request []byte) ([]byte, error) {
	if len(request) > 1024*1024 {
		return nil, errors.New("Agent operation request too large")
	}
	return r.agentRequest(parent, connection, serverID, http.MethodPost, "/v1/operations", request)
}

func (r *Remote) AgentOperationStatus(parent context.Context, connection Connection, serverID, id string) ([]byte, error) {
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
		return nil, errors.New("invalid Agent operation ID")
	}
	return r.agentRequest(parent, connection, serverID, http.MethodGet, "/v1/operations/"+id, nil)
}

func (r *Remote) AgentLogs(parent context.Context, connection Connection, serverID, containerID string, tail int) (string, error) {
	if !validContainerID(containerID) || tail < 1 || tail > 5000 {
		return "", errors.New("invalid Agent logs request")
	}
	data, err := r.agentRequest(parent, connection, serverID, http.MethodGet, fmt.Sprintf("/v1/logs/%s?tail=%d", containerID, tail), nil)
	if err != nil {
		return "", err
	}
	var value struct {
		Logs string `json:"logs"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return "", err
	}
	return value.Logs, nil
}

func (r *Remote) AgentCertbotCheck(parent context.Context, connection Connection, serverID, domain, webroot string) ([]byte, error) {
	if domain == "" || len(domain) > 253 || len(webroot) > 512 {
		return nil, errors.New("invalid Certbot check")
	}
	path := "/v1/certbot/check?domain=" + url.QueryEscape(domain) + "&webroot=" + url.QueryEscape(webroot)
	return r.agentRequest(parent, connection, serverID, http.MethodGet, path, nil)
}

func (r *Remote) agentRequest(parent context.Context, connection Connection, serverID, method, path string, payload []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	token, err := LoadPassword(ctx, AgentPairingCredentialRef(serverID))
	if err != nil {
		return nil, errors.New("Agent 配对凭据不可用，请重新安装 Agent")
	}
	request := fmt.Sprintf("%s %s HTTP/1.1\r\nHost: 127.0.0.1\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", method, path, token, len(payload))
	args := []string{"-o", "ConnectTimeout=8", "-o", "ConnectionAttempts=1", "-o", "StrictHostKeyChecking=yes", "-o", "ClearAllForwardings=yes"}
	if connection.AuthMethod == AuthMethodAgent || connection.AuthMethod == "" {
		args = append(args, "-o", "BatchMode=yes")
	}
	if connection.AuthMethod == AuthMethodPrivateKey {
		args = append(args, "-i", connection.PrivateKeyPath, "-o", "IdentitiesOnly=yes", "-o", "BatchMode=no")
	}
	if connection.AuthMethod == AuthMethodPassword {
		args = append(args, "-o", "BatchMode=no", "-o", "PreferredAuthentications=password,keyboard-interactive", "-o", "PubkeyAuthentication=no")
	}
	args = append(args, "-p", fmt.Sprint(connection.Port), "-W", "127.0.0.1:41827", connection.SSHUser+"@"+connection.Host)
	run := func(env []string) (string, error) {
		command := r.command(ctx, "ssh", args...)
		command.Env = env
		stdin, err := command.StdinPipe()
		if err != nil {
			return "", err
		}
		stdout, err := command.StdoutPipe()
		if err != nil {
			return "", err
		}
		var stderr bytes.Buffer
		command.Stderr = &stderr
		if err := command.Start(); err != nil {
			return "", err
		}
		if _, err := io.WriteString(stdin, request); err != nil {
			stdin.Close()
			_ = command.Wait()
			return "", err
		}
		if len(payload) > 0 {
			if _, err := stdin.Write(payload); err != nil {
				stdin.Close()
				_ = command.Wait()
				return "", err
			}
		}
		response, readErr := http.ReadResponse(bufio.NewReader(stdout), &http.Request{Method: method})
		var body []byte
		if readErr == nil {
			body, readErr = io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
			response.Body.Close()
		}
		stdin.Close()
		waitErr := command.Wait()
		if readErr != nil {
			return "", fmt.Errorf("Agent HTTP 响应无效: %w; SSH: %s", readErr, strings.TrimSpace(stderr.String()))
		}
		if waitErr != nil {
			return "", fmt.Errorf("%w: %s", waitErr, strings.TrimSpace(stderr.String()))
		}
		if len(body) > 2*1024*1024 {
			return "", errors.New("Agent response too large")
		}
		if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
			return "", fmt.Errorf("Agent 返回 HTTP %d: %.300s", response.StatusCode, body)
		}
		return string(body), nil
	}
	var responseText string
	if connection.CredentialRef != "" {
		secret, err := LoadPassword(ctx, connection.CredentialRef)
		if err != nil {
			return nil, err
		}
		responseText, err = withPasswordAskpass(secret, func(askpass *os.File) (string, error) {
			return run(append(os.Environ(), passwordEnvironment(askpass)...))
		})
		if err != nil {
			return nil, fmt.Errorf("Agent SSH 转发失败: %w", err)
		}
	} else {
		responseText, err = run(os.Environ())
		if err != nil {
			return nil, fmt.Errorf("Agent SSH 转发失败: %w", err)
		}
	}
	return []byte(responseText), nil
}
