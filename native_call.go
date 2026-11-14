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
	const names = `AddAliDNSRecord AddDNSRecord ApplyDomainBinding ApplyExistingNginxBinding ApplyExternalRoute ApplySharedPathRoute BindDeployment CancelJob CheckForAppUpdates CheckPortAvailability ClearRuntimeCache ClearServerData CloneDeployment ConfigureDeployment CreateAliDNSConnection CreateDNSConnection CreateDatabaseConnection CreateDeployment CreateDomain CreateDomainHost CreateProject CreateRegistry CreateServer DeleteAliDNSConnection DeleteAliDNSRecord DeleteDNSConnection DeleteDNSRecord DeleteDatabaseConnection DeleteDeployment DeleteDeploymentConfiguration DeleteDomain DeleteDomainBindingDraft DeleteExternalRouteDraft DeleteSharedPathRouteDraft DeleteDomainHost DeleteProject DeleteProjectConfiguration DeleteRegistry DeleteServer DeploySharedServer DetectDeploymentVersion DiscoverDeploymentEndpoints DownloadAppUpdate GenerateDockerfile GetAISettings GetAppUpdateProgress GetAppUpdateStatus GetAutomaticUpdateChecks GetBuildProxySettings GetCommandLineToolStatus GetContainerLogs GetControlOperation GetDatabaseOverview GetDatabaseTableColumns GetDatabaseTableIndexes GetDatabaseTableRows GetJob GetLaunchAtLoginStatus GetOverview InspectDeploymentRuntime InspectProject InspectSSHHostKey InspectServerHostKey InstallCommandLineTool InstallServerAgent InspectDomainAccess InspectSharedPathAccess ListAliDNSConnections ListAliDNSRecords ListDNSConnections ListDNSRecords ListDatabaseConnections ListDatabaseSchemas ListDatabaseTables ListDeploymentEnvironment ListDeploymentReleases ListDeployments ListDeploymentDomains ListDomainHosts ListDomains ListProjects ListRegistries ListRegistryProjects ListRegistryRepositories ListRemoteContainers ListSharedPathRoutes ListRepositoryArtifacts ListServers OperateDeployment OperateRemoteContainer PatchDeployment PatchProject PlanDomainBinding PlanExistingNginxBinding PlanExternalRoute PlanSharedPathRoute PlanSharedServerDeployment PrepareDNSRecordBinding PreflightDeployment ProjectGitStatus PromptDeploymentSecret PublishDeployment RemoveDomainBinding RemoveExternalRoute RemoveSharedPathRoute RetryRelease RollbackDeployment RunDiagnostic SaveBuildProxySettings SaveDeepSeekAPIKey SaveDomainBindingDraft SaveExternalRouteDraft SaveSharedPathRouteDraft SaveGeneratedDockerfile ScanServerNginxRoutes SetAliDNSRecordStatus SetDNSRecordStatus SetAutomaticUpdateChecks SetLaunchAtLogin TestAliDNSDomain TestDNSConnection TestDatabaseConnection TestRegistry TestRegistryConnection TestSavedDNSConnection TestSavedDatabaseConnection TestSavedServerConnection TestServer TestServerConnection TrustSSHHostKey TrustServerHostKey UpdateAliDNSRecord UpdateDNSConnection UpdateDNSRecord UpdateDatabaseConnection UpdateDeploymentEnvironment UpdateDomain UpdateDomainHost UpdateProject UpdateRegistry UpdateServer UpdateServerCredentials`
	for _, name := range splitNativeNames(names) {
		nativeMethods[name] = true
	}
	nativeMethods["GetServerAgentStatus"] = true
	nativeMethods["GetSharedServerStatus"] = true
	nativeMethods["ListInternalIngresses"] = true
	nativeMethods["SaveInternalIngressDraft"] = true
	nativeMethods["PlanInternalIngress"] = true
	nativeMethods["DeployInternalIngress"] = true
	nativeMethods["DeleteInternalIngressDraft"] = true
	nativeMethods["InspectInternalIngress"] = true
	nativeMethods["RepairInternalIngress"] = true
	nativeMethods["GetPreflightProgress"] = true
	nativeMethods["ReadProjectFile"] = true
	nativeMethods["SaveProjectFile"] = true
	nativeMethods["CheckServerCertificate"] = true
	nativeMethods["IssueServerCertificate"] = true
	nativeMethods["RenewServerCertificates"] = true
	nativeMethods["UpdateServerAgent"] = true
	nativeMethods["UninstallServerAgent"] = true
	nativeMethods["CreateRemoteDatabase"] = true
	for _, name := range splitNativeNames("GetSharedWorkspaceStatus GetSharedSyncHealth PreviewSharedWorkspaceMigration StartSharedWorkspaceMigration SyncSharedWorkspace PushSharedWorkspace RetryPendingSharedChanges ReconcileSharedWorkspace NewSharedPairingCode JoinSharedWorkspace ListSharedConflicts ResolveSharedConflict ListSharedDevices RevokeSharedDevice") {
		nativeMethods[name] = true
	}
}

func splitNativeNames(names string) []string {
	return strings.Fields(names)
}

var sharedResourceMutations = map[string]bool{
	"CreateProject": true, "UpdateProject": true, "PatchProject": true, "DeleteProject": true,
	"CreateServer": true, "UpdateServer": true, "UpdateServerCredentials": true, "DeleteServer": true,
	"CreateRegistry": true, "UpdateRegistry": true, "DeleteRegistry": true,
	"CreateDatabaseConnection": true, "UpdateDatabaseConnection": true, "DeleteDatabaseConnection": true,
	"CreateDeployment": true, "CloneDeployment": true, "ConfigureDeployment": true, "PatchDeployment": true,
	"BindDeployment": true, "DeleteDeployment": true, "DeleteDeploymentConfiguration": true,
	"UpdateDeploymentEnvironment": true,
	"PromptDeploymentSecret":      true,
	"CreateDNSConnection":         true, "UpdateDNSConnection": true, "DeleteDNSConnection": true,
	"CreateAliDNSConnection": true, "DeleteAliDNSConnection": true,
	"CreateDomain": true, "UpdateDomain": true, "DeleteDomain": true,
	"CreateDomainHost": true, "UpdateDomainHost": true, "DeleteDomainHost": true,
	"SaveDomainBindingDraft": true, "DeleteDomainBindingDraft": true,
	"SaveExternalRouteDraft": true, "DeleteExternalRouteDraft": true,
	"SaveSharedPathRouteDraft": true, "DeleteSharedPathRouteDraft": true,
	"SaveInternalIngressDraft": true, "DeleteInternalIngressDraft": true, "RepairInternalIngress": true,
}

var sharedOperationMutations = map[string]bool{
	"AddAliDNSRecord": true, "AddDNSRecord": true, "UpdateAliDNSRecord": true,
	"UpdateDNSRecord": true, "DeleteAliDNSRecord": true, "DeleteDNSRecord": true,
	"SetAliDNSRecordStatus": true, "SetDNSRecordStatus": true,
	"ApplyDomainBinding": true, "ApplyExistingNginxBinding": true,
	"ApplyExternalRoute": true, "ApplySharedPathRoute": true,
	"DeployInternalIngress": true,
	"RemoveDomainBinding":   true, "RemoveExternalRoute": true, "RemoveSharedPathRoute": true,
	"OperateDeployment": true, "OperateRemoteContainer": true,
	"InstallServerAgent": true, "UpdateServerAgent": true, "UninstallServerAgent": true,
	"IssueServerCertificate": true, "RenewServerCertificates": true,
	"CreateRemoteDatabase": true,
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
	sharedWrite := sharedResourceMutations[name]
	sharedOperation := sharedWrite || sharedOperationMutations[name]
	if sharedOperation {
		service.sharedWriteMu.Lock()
		defer service.sharedWriteMu.Unlock()
		if err := service.prepareSharedWrite(name, inputs); err != nil {
			return nil, err
		}
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
	if sharedWrite {
		if err := service.completeSharedWrite(); err != nil {
			return nil, err
		}
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].Interface(), nil
}
