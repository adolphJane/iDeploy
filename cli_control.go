package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"ideploy/internal/deployment"
	"ideploy/internal/job"
	"ideploy/internal/project"
	"ideploy/internal/registry"
	"ideploy/internal/server"
)

type cliRequest struct {
	Command         string                   `json:"command"`
	RequestID       string                   `json:"requestId,omitempty"`
	ID              string                   `json:"id,omitempty"`
	Project         *ProjectInput            `json:"project,omitempty"`
	ProjectPatch    *ProjectPatchInput       `json:"projectPatch,omitempty"`
	Deployment      *DeploymentInput         `json:"deployment,omitempty"`
	DeploymentPatch *DeploymentPatchInput    `json:"deploymentPatch,omitempty"`
	BuildProxy      *BuildProxySettingsInput `json:"buildProxy,omitempty"`
	RegistryID      string                   `json:"registryId,omitempty"`
	Version         string                   `json:"version,omitempty"`
	ReleaseID       string                   `json:"releaseId,omitempty"`
	DeploymentID    string                   `json:"deploymentId,omitempty"`
	ContainerID     string                   `json:"containerId,omitempty"`
	Ports           []int                    `json:"ports,omitempty"`
	Tail            int                      `json:"tail,omitempty"`
	Limit           int                      `json:"limit,omitempty"`
	Wait            bool                     `json:"wait,omitempty"`
	TimeoutMS       int64                    `json:"timeoutMs,omitempty"`
}

type cliResponse struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
	Code  string          `json:"code,omitempty"`
}

type cliCommandError struct {
	Code    string
	Message string
}

func (e *cliCommandError) Error() string { return e.Message }

type cliControlServer struct {
	listener net.Listener
	path     string
	done     chan struct{}
	wait     sync.WaitGroup
	once     sync.Once
	service  *SystemService
	ctx      context.Context
	cancel   context.CancelFunc
}

func startCLIControlServer(path string, service *SystemService) (*cliControlServer, error) {
	if runtime.GOOS == "windows" {
		return nil, nil
	}
	// The caller owns the process lock, so any remaining socket is stale.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale control socket: %w", err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on control socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("secure control socket: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	server := &cliControlServer{listener: listener, path: path, done: make(chan struct{}), service: service, ctx: ctx, cancel: cancel}
	server.wait.Add(1)
	go server.serve()
	return server, nil
}

func (s *cliControlServer) serve() {
	defer s.wait.Done()
	for {
		connection, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				continue
			}
		}
		s.wait.Add(1)
		go func() {
			defer s.wait.Done()
			defer connection.Close()
			s.handle(connection)
		}()
	}
}

func (s *cliControlServer) handle(connection net.Conn) {
	_ = connection.SetDeadline(time.Now().Add(24 * time.Hour))
	decoder := json.NewDecoder(bufio.NewReader(ioLimitReader(connection, 1024*1024)))
	var request cliRequest
	if err := decoder.Decode(&request); err != nil {
		_ = json.NewEncoder(connection).Encode(errorCLIResponse("invalid_request", "invalid control request"))
		return
	}
	ctx := s.ctx
	cancel := func() {}
	if request.TimeoutMS > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(request.TimeoutMS)*time.Millisecond)
	}
	defer cancel()
	response := executeIdempotentCLIRequest(ctx, s.service, request)
	_ = json.NewEncoder(connection).Encode(response)
}

func (s *cliControlServer) Close() error {
	if s == nil {
		return nil
	}
	var closeErr error
	s.once.Do(func() {
		close(s.done)
		s.cancel()
		closeErr = s.listener.Close()
		s.wait.Wait()
		if err := os.Remove(s.path); closeErr == nil && err != nil && !errors.Is(err, os.ErrNotExist) {
			closeErr = err
		}
	})
	return closeErr
}

// ioLimitReader is kept behind a helper so the socket protocol never accepts
// unbounded input from a local process.
func ioLimitReader(connection net.Conn, limit int64) *limitedReader {
	return &limitedReader{reader: connection, remaining: limit}
}

type limitedReader struct {
	reader    net.Conn
	remaining int64
}

func (r *limitedReader) Read(buffer []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, errors.New("control request exceeds size limit")
	}
	if int64(len(buffer)) > r.remaining {
		buffer = buffer[:r.remaining]
	}
	n, err := r.reader.Read(buffer)
	r.remaining -= int64(n)
	return n, err
}

func callCLIControl(ctx context.Context, path string, request cliRequest) (cliResponse, error) {
	dialer := net.Dialer{Timeout: 750 * time.Millisecond}
	connection, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return cliResponse{}, err
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return cliResponse{}, err
	}
	var response cliResponse
	if err := json.NewDecoder(ioLimitReader(connection, 16*1024*1024)).Decode(&response); err != nil {
		return cliResponse{}, err
	}
	return response, nil
}

func executeCLIRequest(ctx context.Context, service *SystemService, request cliRequest) cliResponse {
	var value any
	var err error
	switch request.Command {
	case "version":
		value = map[string]string{"version": appVersion, "platform": runtime.GOOS, "architecture": runtime.GOARCH}
	case "doctor":
		value, err = service.GetOverview()
	case "project.list":
		value, err = service.ListProjects()
	case "operation.get":
		value, err = service.GetControlOperation(request.ID)
	case "project.create":
		if request.Project == nil {
			err = &cliCommandError{Code: "invalid_request", Message: "project configuration is required"}
		} else {
			value, err = service.CreateProject(*request.Project)
		}
	case "project.update":
		if request.ProjectPatch == nil {
			err = &cliCommandError{Code: "invalid_request", Message: "project changes are required"}
		} else {
			value, err = service.PatchProject(request.ID, *request.ProjectPatch)
		}
	case "project.inspect":
		value, err = service.InspectProject(request.ID)
	case "project.delete":
		err = service.DeleteProjectConfiguration(request.ID)
		value = map[string]string{"id": request.ID, "status": "deleted"}
	case "server.list":
		value, err = service.ListServers()
	case "server.container.list":
		value, err = service.ListRemoteContainers(request.ID)
	case "server.container.logs":
		value, err = service.GetContainerLogs(ContainerLogsInput{ServerID: request.ID, ContainerID: request.ContainerID, Tail: request.Tail})
	case "server.port.check":
		value, err = service.CheckPortAvailability(PortAvailabilityInput{ServerID: request.ID, Ports: request.Ports})
	case "registry.list":
		value, err = service.ListRegistries()
	case "build-proxy.get":
		value, err = service.GetBuildProxySettings()
	case "build-proxy.configure":
		if request.BuildProxy == nil {
			err = &cliCommandError{Code: "invalid_request", Message: "build proxy configuration is required"}
		} else {
			value, err = service.SaveBuildProxySettings(*request.BuildProxy)
		}
	case "deployment.list":
		value, err = service.ListDeployments()
	case "deployment.create":
		if request.Deployment == nil {
			err = &cliCommandError{Code: "invalid_request", Message: "deployment configuration is required"}
		} else {
			value, err = service.CreateDeployment(*request.Deployment)
		}
	case "deployment.update":
		if request.DeploymentPatch == nil {
			err = &cliCommandError{Code: "invalid_request", Message: "deployment changes are required"}
		} else {
			value, err = service.PatchDeployment(request.ID, *request.DeploymentPatch)
		}
	case "deployment.clone":
		if request.DeploymentPatch == nil {
			err = &cliCommandError{Code: "invalid_request", Message: "deployment clone overrides are required"}
		} else {
			value, err = service.CloneDeployment(request.ID, *request.DeploymentPatch)
		}
	case "deployment.delete":
		err = service.DeleteDeploymentConfiguration(request.ID)
		value = map[string]string{"id": request.ID, "status": "deleted"}
	case "deployment.get":
		value, err = findDeployment(service, request.ID)
	case "deployment.preflight":
		value, err = service.PreflightDeployment(DeploymentPreflightInput{DeploymentID: request.ID, RegistryID: request.RegistryID})
	case "deployment.runtime":
		value, err = service.InspectDeploymentRuntime(request.ID)
	case "deployment.publish":
		value, err = service.PublishDeployment(DeploymentReleaseInput{DeploymentID: request.ID, RegistryID: request.RegistryID, Version: request.Version})
		value, err = waitForCLIJobIfNeeded(ctx, service, value, err, request.Wait)
	case "deployment.rollback":
		value, err = service.RollbackDeployment(request.ID, request.ReleaseID)
		value, err = waitForCLIJobIfNeeded(ctx, service, value, err, request.Wait)
	case "deployment.start", "deployment.stop", "deployment.restart":
		action := strings.TrimPrefix(request.Command, "deployment.")
		value, err = service.OperateDeployment(request.ID, action)
		value, err = waitForCLIJobIfNeeded(ctx, service, value, err, request.Wait)
	case "release.list":
		value, err = service.ListDeploymentReleases(request.DeploymentID)
	case "release.retry":
		value, err = service.RetryRelease(request.ReleaseID)
		value, err = waitForCLIJobIfNeeded(ctx, service, value, err, request.Wait)
	case "job.list":
		var overview Overview
		overview, err = service.GetOverview()
		value = overview.Jobs
	case "job.get":
		value, err = service.GetJob(request.ID)
	case "job.wait":
		value, err = waitForCLIJob(ctx, service, request.ID)
	case "job.cancel":
		err = service.CancelJob(request.ID)
		if err == nil {
			value, err = service.GetJob(request.ID)
		}
	default:
		err = &cliCommandError{Code: "unknown_command", Message: "unknown command"}
	}
	if err != nil {
		response := cliResponse{}
		var commandErr *cliCommandError
		if errors.As(err, &commandErr) {
			response = errorCLIResponse(commandErr.Code, commandErr.Message)
		} else if errors.Is(err, job.ErrNotFound) || errors.Is(err, deployment.ErrNotFound) ||
			errors.Is(err, project.ErrNotFound) || errors.Is(err, registry.ErrNotFound) || errors.Is(err, server.ErrNotFound) {
			response = errorCLIResponse("not_found", err.Error())
		} else if errors.Is(err, registry.ErrCredentialUnavailable) || errors.Is(err, server.ErrCredentialUnavailable) {
			response = errorCLIResponse("credential_unavailable", err.Error())
		} else if errors.Is(err, context.DeadlineExceeded) {
			response = errorCLIResponse("timeout", "command timed out")
		} else if errors.Is(err, context.Canceled) {
			response = errorCLIResponse("canceled", "command canceled")
		} else {
			response = errorCLIResponse("operation_failed", err.Error())
		}
		if value != nil {
			if encoded, encodeErr := json.Marshal(value); encodeErr == nil {
				response.Data = encoded
			}
		}
		return response
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return errorCLIResponse("encoding_failed", err.Error())
	}
	return cliResponse{OK: true, Data: encoded}
}

func findDeployment(service *SystemService, id string) (DeploymentView, error) {
	values, err := service.ListDeployments()
	if err != nil {
		return DeploymentView{}, err
	}
	for _, value := range values {
		if value.ID == id {
			return value, nil
		}
	}
	return DeploymentView{}, &cliCommandError{Code: "not_found", Message: "deployment not found"}
}

func waitForCLIJobIfNeeded(ctx context.Context, service *SystemService, value any, err error, wait bool) (any, error) {
	if err != nil || !wait {
		return value, err
	}
	queued, ok := value.(JobView)
	if !ok {
		return value, nil
	}
	return waitForCLIJob(ctx, service, queued.ID)
}

func waitForCLIJob(ctx context.Context, service *SystemService, id string) (JobView, error) {
	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := service.GetJob(id)
		if err != nil {
			return JobView{}, err
		}
		switch current.Status {
		case "succeeded":
			return current, nil
		case "failed", "canceled":
			message := current.Error
			if strings.TrimSpace(message) == "" {
				message = current.Message
			}
			return current, &cliCommandError{Code: "job_" + current.Status, Message: message}
		}
		select {
		case <-ctx.Done():
			return JobView{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func errorCLIResponse(code, message string) cliResponse {
	return cliResponse{OK: false, Error: message, Code: code}
}
