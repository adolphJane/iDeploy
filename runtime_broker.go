package main

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"ideploy/internal/platform/instance"
	appPaths "ideploy/internal/platform/paths"
)

// executeRuntimeRequest routes a structured command through the running
// desktop application when possible, and otherwise owns a temporary headless
// runtime for the duration of the command. CLI and MCP both use this path so
// there is only one implementation of runtime arbitration.
func executeRuntimeRequest(ctx context.Context, request cliRequest) cliResponse {
	if request.Command == "version" {
		return executeCLIRequest(ctx, nil, request)
	}

	paths, err := appPaths.Resolve()
	if err != nil {
		return errorCLIResponse("runtime_unavailable", err.Error())
	}
	if err := appPaths.Ensure(paths); err != nil {
		return errorCLIResponse("runtime_unavailable", err.Error())
	}

	socketPath := filepath.Join(paths.DataDir, "control.sock")
	response, controlErr := callCLIControl(ctx, socketPath, request)
	if controlErr == nil {
		return response
	}
	if !request.Wait && cliRequestStartsJob(request) {
		return errorCLIResponse("background_unavailable", "asynchronous operations require iDeploy.app to be running")
	}

	service := NewSystemService()
	if err := service.startWithPaths(ctx, false, paths); err != nil {
		if errors.Is(err, instance.ErrLocked) {
			// The desktop may be between acquiring its runtime lock and opening
			// the control socket. Give that short startup window a chance to end.
			for attempt := 0; attempt < 20; attempt++ {
				select {
				case <-ctx.Done():
					return errorCLIResponse("runtime_busy", "iDeploy is running but its local control channel is unavailable")
				case <-time.After(100 * time.Millisecond):
				}
				response, controlErr = callCLIControl(ctx, socketPath, request)
				if controlErr == nil {
					return response
				}
			}
			return errorCLIResponse("runtime_busy", "iDeploy is running but its local control channel is unavailable")
		}
		return errorCLIResponse("runtime_unavailable", err.Error())
	}
	defer service.close()
	return executeCLIRequest(ctx, service, request)
}
