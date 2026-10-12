package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// The native window uses the same SystemService API as the former web view.
// Keep this explicit: the local control socket must never become a general
// reflection endpoint for internal service methods.
var nativeMethods = map[string]bool{}

func init() {
	const names = `BindDeployment CancelJob CheckForAppUpdates CheckPortAvailability ClearRuntimeCache ClearServerData CloneDeployment ConfigureDeployment CreateDatabaseConnection CreateDeployment CreateProject CreateRegistry CreateServer DeleteDatabaseConnection DeleteDeployment DeleteDeploymentConfiguration DeleteProject DeleteProjectConfiguration DeleteRegistry DeleteServer DetectDeploymentVersion DownloadAppUpdate GenerateDockerfile GetAISettings GetAppUpdateProgress GetAppUpdateStatus GetAutomaticUpdateChecks GetBuildProxySettings GetCommandLineToolStatus GetContainerLogs GetControlOperation GetDatabaseOverview GetDatabaseTableColumns GetDatabaseTableIndexes GetDatabaseTableRows GetJob GetLaunchAtLoginStatus GetOverview InspectDeploymentRuntime InspectProject InspectSSHHostKey InspectServerHostKey InstallCommandLineTool ListDatabaseConnections ListDatabaseSchemas ListDatabaseTables ListDeploymentEnvironment ListDeploymentReleases ListDeployments ListProjects ListRegistries ListRegistryProjects ListRegistryRepositories ListRemoteContainers ListRepositoryArtifacts ListServers OperateDeployment OperateRemoteContainer PatchDeployment PatchProject PreflightDeployment ProjectGitStatus PromptDeploymentSecret PublishDeployment RetryRelease RollbackDeployment RunDiagnostic SaveBuildProxySettings SaveDeepSeekAPIKey SaveGeneratedDockerfile SetAutomaticUpdateChecks SetLaunchAtLogin TestDatabaseConnection TestRegistry TestRegistryConnection TestSavedDatabaseConnection TestSavedServerConnection TestServer TestServerConnection TrustSSHHostKey TrustServerHostKey UpdateDatabaseConnection UpdateDeploymentEnvironment UpdateProject UpdateRegistry UpdateServer UpdateServerCredentials`
	for _, name := range splitNativeNames(names) {
		nativeMethods[name] = true
	}
}

func splitNativeNames(names string) []string {
	return strings.Fields(names)
}

func executeNativeCall(service *SystemService, name string, args []json.RawMessage) (value any, err error) {
	if service == nil || !nativeMethods[name] {
		return nil, &cliCommandError{Code: "unknown_method", Message: "unknown native method"}
	}
	method := reflect.ValueOf(service).MethodByName(name)
	if !method.IsValid() || method.Type().NumIn() != len(args) {
		return nil, &cliCommandError{Code: "invalid_request", Message: "invalid native arguments"}
	}
	inputs := make([]reflect.Value, len(args))
	for index, raw := range args {
		parameter := reflect.New(method.Type().In(index))
		if decodeErr := json.Unmarshal(raw, parameter.Interface()); decodeErr != nil {
			return nil, &cliCommandError{Code: "invalid_request", Message: fmt.Sprintf("argument %d: %v", index+1, decodeErr)}
		}
		inputs[index] = parameter.Elem()
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			value = nil
			err = fmt.Errorf("native method failed: %v", recovered)
		}
	}()
	results := method.Call(inputs)
	if count := len(results); count > 0 {
		last := results[count-1]
		if last.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			if !last.IsNil() {
				return nil, last.Interface().(error)
			}
			results = results[:count-1]
		}
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].Interface(), nil
}
