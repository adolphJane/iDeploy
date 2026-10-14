package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"ideploy/internal/databasebrowser"
	"ideploy/internal/deployment"
	"ideploy/internal/job"
	"ideploy/internal/platform/database"
	"ideploy/internal/platform/instance"
	appPaths "ideploy/internal/platform/paths"
	"ideploy/internal/platform/toolchain"
	"ideploy/internal/project"
	"ideploy/internal/registry"
	"ideploy/internal/server"
)

// Release builds inject the Git tag through -ldflags -X.
var appVersion = "0.2.0-dev"

type ToolStatus struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Path      string `json:"path"`
	Source    string `json:"source"`
	Detail    string `json:"detail"`
}

type JobView struct {
	LockKey   string `json:"lockKey"`
	ID        string `json:"id"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Progress  int    `json:"progress"`
	Message   string `json:"message"`
	Error     string `json:"error"`
	Attempt   int    `json:"attempt"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type Overview struct {
	Version       string       `json:"version"`
	Platform      string       `json:"platform"`
	Architecture  string       `json:"architecture"`
	DatabasePath  string       `json:"databasePath"`
	CachePath     string       `json:"cachePath"`
	LogPath       string       `json:"logPath"`
	RecoveredJobs int64        `json:"recoveredJobs"`
	ActiveJobs    int          `json:"activeJobs"`
	Tools         []ToolStatus `json:"tools"`
	Jobs          []JobView    `json:"jobs"`
}

type CleanupView struct {
	Servers          int64  `json:"servers"`
	Deployments      int64  `json:"deployments"`
	Snapshots        int64  `json:"snapshots"`
	Jobs             int64  `json:"jobs"`
	Credentials      int64  `json:"credentials"`
	CredentialErrors int64  `json:"credentialErrors"`
	CacheItems       int64  `json:"cacheItems"`
	Detail           string `json:"detail"`
}

type RegistryInput struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Endpoint string `json:"endpoint"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type DatabaseConnectionInput struct {
	Name         string `json:"name"`
	Driver       string `json:"driver"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	DatabaseName string `json:"databaseName"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	SSLMode      string `json:"sslMode"`
}

type DatabaseConnectionView struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Driver               string `json:"driver"`
	Host                 string `json:"host"`
	Port                 int    `json:"port"`
	DatabaseName         string `json:"databaseName"`
	Username             string `json:"username"`
	SSLMode              string `json:"sslMode"`
	CredentialConfigured bool   `json:"credentialConfigured"`
	CreatedAt            string `json:"createdAt"`
	UpdatedAt            string `json:"updatedAt"`
	LastConnectedAt      string `json:"lastConnectedAt"`
	LastError            string `json:"lastError"`
}

type DatabaseSchemaView struct {
	Name string `json:"name"`
}
type DatabaseOverviewView struct {
	Version           string `json:"version"`
	DatabaseSizeBytes *int64 `json:"databaseSizeBytes"`
	SizeDetail        string `json:"sizeDetail"`
}
type DatabaseTableView struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Type   string `json:"type"`
}
type DatabaseColumnView struct {
	Name         string `json:"name"`
	DataType     string `json:"dataType"`
	Nullable     bool   `json:"nullable"`
	DefaultValue string `json:"defaultValue"`
	PrimaryKey   bool   `json:"primaryKey"`
	Comment      string `json:"comment"`
}
type DatabaseIndexView struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
}
type DatabaseTableQuery struct {
	Page       int    `json:"page"`
	PageSize   int    `json:"pageSize"`
	SortColumn string `json:"sortColumn"`
	SortDesc   bool   `json:"sortDesc"`
}
type DatabaseTablePageView struct {
	Columns    []DatabaseColumnView `json:"columns"`
	Rows       []map[string]string  `json:"rows"`
	Page       int                  `json:"page"`
	PageSize   int                  `json:"pageSize"`
	HasNext    bool                 `json:"hasNext"`
	SortColumn string               `json:"sortColumn"`
	SortDesc   bool                 `json:"sortDesc"`
}

type RegistryView struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Kind                 string `json:"kind"`
	Endpoint             string `json:"endpoint"`
	Username             string `json:"username"`
	Status               string `json:"status"`
	LastCheckedAt        string `json:"lastCheckedAt"`
	LastMessage          string `json:"lastMessage"`
	LastError            string `json:"lastError"`
	CredentialConfigured bool   `json:"credentialConfigured"`
}

// RegistryProbeView is the result of testing an unsaved registry form. It is
// intentionally separate from RegistryView: no connection record or secret is
// written until the user chooses to save it.
type RegistryProbeView struct {
	Endpoint string `json:"endpoint"`
	Status   string `json:"status"`
	Detail   string `json:"detail"`
}

type RegistryRepositoryView struct {
	Name          string `json:"name"`
	ArtifactCount int    `json:"artifactCount"`
	PullCount     int64  `json:"pullCount"`
	UpdatedAt     string `json:"updatedAt"`
}

type RegistryProjectView struct {
	Name            string `json:"name"`
	RepositoryCount int    `json:"repositoryCount"`
}

type RegistryProjectsView struct {
	RegistryID string                `json:"registryId"`
	Status     string                `json:"status"`
	Detail     string                `json:"detail"`
	Projects   []RegistryProjectView `json:"projects"`
}

type RegistryRepositoriesView struct {
	RegistryID   string                   `json:"registryId"`
	Status       string                   `json:"status"`
	Detail       string                   `json:"detail"`
	Repositories []RegistryRepositoryView `json:"repositories"`
}

type RegistryArtifactView struct {
	Digest    string   `json:"digest"`
	Tags      []string `json:"tags"`
	Size      int64    `json:"size"`
	CreatedAt string   `json:"createdAt"`
	MediaType string   `json:"mediaType"`
}

type RegistryArtifactsView struct {
	RegistryID string                 `json:"registryId"`
	Repository string                 `json:"repository"`
	Status     string                 `json:"status"`
	Detail     string                 `json:"detail"`
	Artifacts  []RegistryArtifactView `json:"artifacts"`
}

type ServerInput struct {
	Name                 string `json:"name"`
	Host                 string `json:"host"`
	Port                 int    `json:"port"`
	SSHUser              string `json:"sshUser"`
	AuthMethod           string `json:"authMethod"`
	Password             string `json:"password"`
	PrivateKeyPath       string `json:"privateKeyPath"`
	PrivateKeyPassphrase string `json:"privateKeyPassphrase"`
}

type ServerConnectionInput struct {
	ServerID             string `json:"serverId"`
	Host                 string `json:"host"`
	Port                 int    `json:"port"`
	SSHUser              string `json:"sshUser"`
	AuthMethod           string `json:"authMethod"`
	Password             string `json:"password"`
	PrivateKeyPath       string `json:"privateKeyPath"`
	PrivateKeyPassphrase string `json:"privateKeyPassphrase"`
}

type ServerConnectionTestView struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type SSHHostKeyInput struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	SSHUser string `json:"sshUser"`
}

type ServerView struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Host                 string `json:"host"`
	Port                 int    `json:"port"`
	SSHUser              string `json:"sshUser"`
	AuthMethod           string `json:"authMethod"`
	PrivateKeyPath       string `json:"privateKeyPath"`
	CredentialConfigured bool   `json:"credentialConfigured"`
	Status               string `json:"status"`
	LastCheckedAt        string `json:"lastCheckedAt"`
	LastMessage          string `json:"lastMessage"`
	LastError            string `json:"lastError"`
}

type HostKeyView struct {
	Status      string `json:"status"`
	Host        string `json:"host"`
	Fingerprint string `json:"fingerprint"`
	KeyType     string `json:"keyType"`
	Detail      string `json:"detail"`
}

type RemoteContainerView struct {
	ComposeProject string `json:"composeProject"`
	Health         string `json:"health"`
	OneOff         bool   `json:"oneOff"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	Image          string `json:"image"`
	State          string `json:"state"`
	Status         string `json:"status"`
	Ports          string `json:"ports"`
	Created        string `json:"created"`
}

type RemoteContainersView struct {
	ObservedAt string                `json:"observedAt"`
	Stale      bool                  `json:"stale"`
	ServerID   string                `json:"serverId"`
	Status     string                `json:"status"`
	Detail     string                `json:"detail"`
	Containers []RemoteContainerView `json:"containers"`
}

type ProjectInput struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	SourceKind      string `json:"sourceKind"`
	LocalPath       string `json:"localPath"`
	RepositoryURL   string `json:"repositoryUrl"`
	RefKind         string `json:"refKind"`
	RefValue        string `json:"refValue"`
	ContextPath     string `json:"contextPath"`
	DockerfilePath  string `json:"dockerfilePath"`
	ComposePath     string `json:"composePath"`
	ImageRepository string `json:"imageRepository"`
}

type ProjectView struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	SourceKind      string `json:"sourceKind"`
	LocalPath       string `json:"localPath"`
	RepositoryURL   string `json:"repositoryUrl"`
	RefKind         string `json:"refKind"`
	RefValue        string `json:"refValue"`
	ResolvedCommit  string `json:"resolvedCommit"`
	Status          string `json:"status"`
	LastCheckedAt   string `json:"lastCheckedAt"`
	LastError       string `json:"lastError"`
	ContextPath     string `json:"contextPath"`
	DockerfilePath  string `json:"dockerfilePath"`
	ComposePath     string `json:"composePath"`
	ImageRepository string `json:"imageRepository"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type DeploymentInput struct {
	WorkloadKind   string   `json:"workloadKind"`
	WorkloadName   string   `json:"workloadName"`
	ProjectID      string   `json:"projectId"`
	ServerID       string   `json:"serverId"`
	ContainerPorts []string `json:"containerPorts"`
	RestartPolicy  string   `json:"restartPolicy"`
}

type DeploymentReleaseInput struct {
	DeploymentID string `json:"deploymentId"`
	RegistryID   string `json:"registryId"`
	Version      string `json:"version"`
}

type DeploymentVersionSuggestionView struct {
	CurrentVersion string   `json:"currentVersion"`
	UsedVersions   []string `json:"usedVersions"`
	Version        string   `json:"version"`
	Source         string   `json:"source"`
	Detail         string   `json:"detail"`
}

type DeploymentReleaseView struct {
	ArtifactPinned      bool   `json:"artifactPinned"`
	ID                  string `json:"id"`
	DeploymentID        string `json:"deploymentId"`
	RegistryID          string `json:"registryId"`
	JobID               string `json:"jobId"`
	Action              string `json:"action"`
	Version             string `json:"version"`
	ImageRef            string `json:"imageRef"`
	SourceRevision      string `json:"sourceRevision"`
	RollbackToID        string `json:"rollbackToId"`
	Status              string `json:"status"`
	Detail              string `json:"detail"`
	EnvironmentRevision int64  `json:"environmentRevision"`
	CreatedAt           string `json:"createdAt"`
	FinishedAt          string `json:"finishedAt"`
}

// DeploymentView binds a local project to a remote server. Runtime state is
// deliberately not persisted here: the desktop queries Docker when rendering
// the deployment page so an old local record is never presented as live.
type DeploymentView struct {
	WorkloadKind               string   `json:"workloadKind"`
	WorkloadName               string   `json:"workloadName"`
	ID                         string   `json:"id"`
	ProjectID                  string   `json:"projectId"`
	ProjectName                string   `json:"projectName"`
	ProjectStatus              string   `json:"projectStatus"`
	ImageRepository            string   `json:"imageRepository"`
	ComposePath                string   `json:"composePath"`
	ServerID                   string   `json:"serverId"`
	ServerName                 string   `json:"serverName"`
	ServerStatus               string   `json:"serverStatus"`
	DesiredState               string   `json:"desiredState"`
	ContainerPorts             []string `json:"containerPorts"`
	RestartPolicy              string   `json:"restartPolicy"`
	EnvironmentRevision        int64    `json:"environmentRevision"`
	AppliedEnvironmentRevision int64    `json:"appliedEnvironmentRevision"`
	EnvironmentPendingApply    bool     `json:"environmentPendingApply"`
	CreatedAt                  string   `json:"createdAt"`
	UpdatedAt                  string   `json:"updatedAt"`
}

// SystemService owns Phase 0 application infrastructure and exposes a narrow,
// typed interface to the native UI, CLI, and MCP.
type SystemService struct {
	mu sync.RWMutex

	db             *sql.DB
	store          *job.Store
	registries     *registry.Store
	databases      *databasebrowser.Store
	servers        *server.Store
	projects       *project.Store
	deployments    *deployment.Store
	runner         *job.Runner
	runtimeLock    *instance.Lock
	controlServer  *cliControlServer
	paths          appPaths.AppPaths
	recoveredJobs  int64
	emit           func(JobView)
	emitRegistry   func(RegistryView)
	emitServer     func(ServerView)
	emitProject    func(ProjectView)
	emitDeployment func(DeploymentView)
}

func NewSystemService() *SystemService {
	return &SystemService{
		emit:           func(JobView) {},
		emitRegistry:   func(RegistryView) {},
		emitServer:     func(ServerView) {},
		emitProject:    func(ProjectView) {},
		emitDeployment: func(DeploymentView) {},
	}
}

func (s *SystemService) setEmitter(emit func(JobView)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if emit != nil {
		s.emit = emit
	}
}

func (s *SystemService) setRegistryEmitter(emit func(RegistryView)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if emit != nil {
		s.emitRegistry = emit
	}
}

func (s *SystemService) setServerEmitter(emit func(ServerView)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if emit != nil {
		s.emitServer = emit
	}
}

func (s *SystemService) setProjectEmitter(emit func(ProjectView)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if emit != nil {
		s.emitProject = emit
	}
}

func (s *SystemService) setDeploymentEmitter(emit func(DeploymentView)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if emit != nil {
		s.emitDeployment = emit
	}
}

// start initialises the application core without requiring the desktop UI.
// The desktop enables the local control socket; a one-shot CLI invocation does
// not, because it already owns and calls this service directly.
func (s *SystemService) start(ctx context.Context, serveControl bool) error {
	paths, err := appPaths.Resolve()
	if err != nil {
		return err
	}
	if err := appPaths.Ensure(paths); err != nil {
		return err
	}
	return s.startWithPaths(ctx, serveControl, paths)
}

func (s *SystemService) startWithPaths(ctx context.Context, serveControl bool, paths appPaths.AppPaths) error {
	runtimeLock, err := instance.Acquire(filepath.Join(paths.DataDir, "runtime.lock"))
	if err != nil {
		return err
	}

	db, err := database.Open(ctx, paths.DatabasePath)
	if err != nil {
		_ = runtimeLock.Release()
		return err
	}
	if err := recoverInterruptedControlOperations(ctx, db); err != nil {
		db.Close()
		_ = runtimeLock.Release()
		return err
	}
	store := job.NewStore(db)
	registries := registry.NewStore(db)
	databases := databasebrowser.NewStore(db)
	servers := server.NewStore(db)
	projects := project.NewStore(db)
	deployments := deployment.NewStore(db)
	recovered, err := store.RecoverInterrupted(ctx)
	if err != nil {
		db.Close()
		_ = runtimeLock.Release()
		return err
	}
	if _, err := deployments.RecoverInterruptedReleases(ctx); err != nil {
		db.Close()
		_ = runtimeLock.Release()
		return err
	}

	runner := job.NewRunner(store, func(value job.Job) {
		s.mu.RLock()
		emit := s.emit
		s.mu.RUnlock()
		emit(toJobView(value))
	})
	runner.Register("system.diagnostic", diagnosticHandler)
	runner.Register("registry.test", s.registryTestHandler)
	runner.Register("server.probe", s.serverProbeHandler)
	runner.Register("server.container.operation", s.serverContainerOperationHandler)
	runner.Register("deployment.operation", s.deploymentOperationHandler)
	runner.Register("deployment.release", s.deploymentReleaseHandler)
	runner.Register("deployment.rollback", s.deploymentRollbackHandler)
	runner.Register("deployment.resume", s.deploymentResumeHandler)

	s.mu.Lock()
	s.db = db
	s.store = store
	s.registries = registries
	s.databases = databases
	s.servers = servers
	s.projects = projects
	s.deployments = deployments
	s.runner = runner
	s.runtimeLock = runtimeLock
	s.paths = paths
	s.recoveredJobs = recovered
	s.mu.Unlock()

	runner.Start(ctx)
	if serveControl {
		controlServer, err := startCLIControlServer(filepath.Join(paths.DataDir, "control.sock"), s)
		if err != nil {
			_ = s.close()
			return err
		}
		s.mu.Lock()
		s.controlServer = controlServer
		s.mu.Unlock()
	}
	return nil
}

func (s *SystemService) ListRegistries() ([]RegistryView, error) {
	registries, err := s.registryStore()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connections, err := registries.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]RegistryView, 0, len(connections))
	for _, connection := range connections {
		result = append(result, toRegistryView(connection))
	}
	return result, nil
}

// ListDatabaseConnections returns locally saved remote database profiles. The
// password is intentionally never exposed to the UI.
func (s *SystemService) ListDatabaseConnections() ([]DatabaseConnectionView, error) {
	store, err := s.databaseStore()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connections, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]DatabaseConnectionView, 0, len(connections))
	for _, connection := range connections {
		result = append(result, toDatabaseConnectionView(connection))
	}
	return result, nil
}

func (s *SystemService) CreateDatabaseConnection(input DatabaseConnectionInput) (DatabaseConnectionView, error) {
	if strings.TrimSpace(input.Password) == "" {
		return DatabaseConnectionView{}, errors.New("请填写数据库密码")
	}
	store, err := s.databaseStore()
	if err != nil {
		return DatabaseConnectionView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	connection, err := store.Create(ctx, toDatabaseConnection(input, ""))
	if err != nil {
		return DatabaseConnectionView{}, err
	}
	credentialRef := databasebrowser.PasswordCredentialRef(connection.ID)
	if err := databasebrowser.SavePassword(ctx, credentialRef, input.Password); err != nil {
		_, _ = store.Delete(ctx, connection.ID)
		return DatabaseConnectionView{}, err
	}
	connection, err = store.Update(ctx, connection.ID, toDatabaseConnection(input, credentialRef))
	if err != nil {
		_ = databasebrowser.DeletePassword(ctx, credentialRef)
		_, _ = store.Delete(ctx, connection.ID)
		return DatabaseConnectionView{}, err
	}
	return toDatabaseConnectionView(connection), nil
}

func (s *SystemService) UpdateDatabaseConnection(id string, input DatabaseConnectionInput) (DatabaseConnectionView, error) {
	store, err := s.databaseStore()
	if err != nil {
		return DatabaseConnectionView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	current, err := store.Get(ctx, id)
	if err != nil {
		return DatabaseConnectionView{}, err
	}
	credentialRef := current.CredentialRef
	if strings.TrimSpace(input.Password) != "" {
		credentialRef = databasebrowser.PasswordCredentialRef(id)
		if err := databasebrowser.SavePassword(ctx, credentialRef, input.Password); err != nil {
			return DatabaseConnectionView{}, err
		}
	}
	if credentialRef == "" {
		return DatabaseConnectionView{}, errors.New("请填写数据库密码")
	}
	updated, err := store.Update(ctx, id, toDatabaseConnection(input, credentialRef))
	if err != nil {
		return DatabaseConnectionView{}, err
	}
	return toDatabaseConnectionView(updated), nil
}

func (s *SystemService) DeleteDatabaseConnection(id string) error {
	store, err := s.databaseStore()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	credentialRef, err := store.Delete(ctx, id)
	if err != nil {
		return err
	}
	if err := databasebrowser.DeletePassword(ctx, credentialRef); err != nil {
		return err
	}
	return nil
}

func (s *SystemService) TestDatabaseConnection(input DatabaseConnectionInput) error {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	err := databasebrowser.Test(ctx, toDatabaseConnection(input, ""), input.Password)
	return s.withDatabaseDiagnosticLog(input.Driver, input.Host, input.Port, input.DatabaseName, input.Username, input.SSLMode, err)
}

func (s *SystemService) TestSavedDatabaseConnection(id string) error {
	store, connection, password, ctx, cancel, err := s.databaseConnectionContext(id, 12*time.Second)
	if err != nil {
		return err
	}
	defer cancel()
	err = databasebrowser.Test(ctx, databasebrowser.NewConnection{Name: connection.Name, Driver: connection.Driver, Host: connection.Host, Port: connection.Port, DatabaseName: connection.DatabaseName, Username: connection.Username, SSLMode: connection.SSLMode}, password)
	_ = store.RecordConnection(context.Background(), id, err)
	return s.withDatabaseDiagnosticLog(string(connection.Driver), connection.Host, connection.Port, connection.DatabaseName, connection.Username, connection.SSLMode, err)
}

func (s *SystemService) ListDatabaseSchemas(id string) ([]DatabaseSchemaView, error) {
	store, connection, password, ctx, cancel, err := s.databaseConnectionContext(id, 15*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	values, err := databasebrowser.ListSchemas(ctx, connection, password)
	_ = store.RecordConnection(context.Background(), id, err)
	if err != nil {
		return nil, err
	}
	result := make([]DatabaseSchemaView, 0, len(values))
	for _, value := range values {
		result = append(result, DatabaseSchemaView{Name: value.Name})
	}
	return result, nil
}

func (s *SystemService) GetDatabaseOverview(id, schema string) (DatabaseOverviewView, error) {
	_, connection, password, ctx, cancel, err := s.databaseConnectionContext(id, 15*time.Second)
	if err != nil {
		return DatabaseOverviewView{}, err
	}
	defer cancel()
	value, err := databasebrowser.GetOverview(ctx, connection, password, schema)
	if err != nil {
		return DatabaseOverviewView{}, err
	}
	return DatabaseOverviewView{Version: value.Version, DatabaseSizeBytes: value.DatabaseSizeBytes, SizeDetail: value.SizeDetail}, nil
}

func (s *SystemService) ListDatabaseTables(id, schema string) ([]DatabaseTableView, error) {
	store, connection, password, ctx, cancel, err := s.databaseConnectionContext(id, 15*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	values, err := databasebrowser.ListTables(ctx, connection, password, schema)
	_ = store.RecordConnection(context.Background(), id, err)
	if err != nil {
		return nil, err
	}
	result := make([]DatabaseTableView, 0, len(values))
	for _, value := range values {
		result = append(result, DatabaseTableView{Schema: value.Schema, Name: value.Name, Type: value.Type})
	}
	return result, nil
}

func (s *SystemService) GetDatabaseTableColumns(id, schema, table string) ([]DatabaseColumnView, error) {
	store, connection, password, ctx, cancel, err := s.databaseConnectionContext(id, 15*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	values, err := databasebrowser.GetColumns(ctx, connection, password, schema, table)
	_ = store.RecordConnection(context.Background(), id, err)
	if err != nil {
		return nil, err
	}
	return toDatabaseColumnViews(values), nil
}

func (s *SystemService) GetDatabaseTableIndexes(id, schema, table string) ([]DatabaseIndexView, error) {
	store, connection, password, ctx, cancel, err := s.databaseConnectionContext(id, 15*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	values, err := databasebrowser.GetIndexes(ctx, connection, password, schema, table)
	_ = store.RecordConnection(context.Background(), id, err)
	if err != nil {
		return nil, err
	}
	result := make([]DatabaseIndexView, 0, len(values))
	for _, value := range values {
		result = append(result, DatabaseIndexView{Name: value.Name, Columns: value.Columns, Unique: value.Unique})
	}
	return result, nil
}

func (s *SystemService) GetDatabaseTableRows(id, schema, table string, input DatabaseTableQuery) (DatabaseTablePageView, error) {
	store, connection, password, ctx, cancel, err := s.databaseConnectionContext(id, 20*time.Second)
	if err != nil {
		return DatabaseTablePageView{}, err
	}
	defer cancel()
	page, err := databasebrowser.GetTableRows(ctx, connection, password, schema, table, databasebrowser.TableQuery{Page: input.Page, PageSize: input.PageSize, SortColumn: input.SortColumn, SortDesc: input.SortDesc})
	_ = store.RecordConnection(context.Background(), id, err)
	if err != nil {
		return DatabaseTablePageView{}, err
	}
	return DatabaseTablePageView{Columns: toDatabaseColumnViews(page.Columns), Rows: page.Rows, Page: page.Page, PageSize: page.PageSize, HasNext: page.HasNext, SortColumn: page.SortColumn, SortDesc: page.SortDesc}, nil
}

func (s *SystemService) CreateRegistry(input RegistryInput) (RegistryView, error) {
	registries, err := s.registryStore()
	if err != nil {
		return RegistryView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connection, err := registries.Create(ctx, registry.NewConnection{
		Name:     input.Name,
		Kind:     registry.Kind(input.Kind),
		Endpoint: input.Endpoint,
		Username: input.Username,
	})
	if err != nil {
		return RegistryView{}, err
	}
	if strings.TrimSpace(input.Password) != "" {
		if strings.TrimSpace(input.Username) == "" {
			_ = registries.Delete(ctx, connection.ID)
			return RegistryView{}, errors.New("配置仓库密码时必须填写用户名")
		}
		credentialRef := registry.PasswordCredentialRef(connection.ID)
		if err := registry.SavePassword(ctx, credentialRef, input.Password); err != nil {
			_ = registries.Delete(ctx, connection.ID)
			return RegistryView{}, err
		}
		connection, err = registries.UpdateCredential(ctx, connection.ID, input.Username, credentialRef)
		if err != nil {
			_ = registry.DeletePassword(ctx, credentialRef)
			return RegistryView{}, err
		}
	}
	view := toRegistryView(connection)
	s.emitRegistryView(view)
	return view, nil
}

func (s *SystemService) TestRegistryConnection(input RegistryInput) (RegistryProbeView, error) {
	validated, err := (registry.NewConnection{
		Name:     input.Name,
		Kind:     registry.Kind(input.Kind),
		Endpoint: input.Endpoint,
		Username: input.Username,
	}).Validate()
	if err != nil {
		return RegistryProbeView{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := probeRegistryForPublishing(ctx, registry.Connection{
		Kind:     validated.Kind,
		Endpoint: validated.Endpoint,
		Username: validated.Username,
	}, input.Password)
	return RegistryProbeView{
		Endpoint: validated.Endpoint,
		Status:   string(result.Status),
		Detail:   result.Detail,
	}, nil
}

func probeRegistryForPublishing(ctx context.Context, connection registry.Connection, password string) registry.ProbeResult {
	probeConnection := connection
	probeConnection.Password = password
	result := registry.NewTester().Probe(ctx, probeConnection)
	if result.Status == registry.StatusUnreachable || result.Status == registry.StatusDegraded {
		return result
	}
	if err := validateRegistryPushCredentials(connection.Username, password); err != nil {
		return registry.ProbeResult{Status: registry.StatusAuthRequired, Detail: "仓库服务可达，但发布需要配置用户名和密码或 Token"}
	}
	docker, err := toolchain.ResolveExecutable(ctx, "docker")
	if err != nil {
		return registry.ProbeResult{Status: registry.StatusDegraded, Detail: "无法执行 Docker 登录测试：" + err.Error()}
	}
	if _, err := runLocalCommandInput(ctx, "", nil, password+"\n", docker.Path, "login", registry.DockerHost(connection.Endpoint), "--username", connection.Username, "--password-stdin"); err != nil {
		return registry.ProbeResult{Status: registry.StatusAuthRequired, Detail: "Docker 登录失败，请核对用户名、密码或 Token：" + err.Error()}
	}
	return registry.ProbeResult{Status: registry.StatusHealthy, Detail: "Docker 登录成功，已具备镜像推送权限"}
}

func (s *SystemService) UpdateRegistry(id string, input RegistryInput) (RegistryView, error) {
	registries, err := s.registryStore()
	if err != nil {
		return RegistryView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	current, err := registries.Get(ctx, id)
	if err != nil {
		return RegistryView{}, err
	}
	credentialRef := current.CredentialRef
	if strings.TrimSpace(input.Username) == "" {
		credentialRef = ""
	} else if input.Password != "" {
		credentialRef = registry.PasswordCredentialRef(id)
		if err := registry.SavePassword(ctx, credentialRef, input.Password); err != nil {
			return RegistryView{}, err
		}
	} else if current.Username != strings.TrimSpace(input.Username) {
		return RegistryView{}, errors.New("修改仓库用户名时需要同时输入新的密码或 Token")
	}
	updated, err := registries.Update(ctx, id, registry.NewConnection{Name: input.Name, Kind: registry.Kind(input.Kind), Endpoint: input.Endpoint, Username: input.Username}, credentialRef)
	if err != nil {
		return RegistryView{}, err
	}
	if current.CredentialRef != "" && credentialRef == "" {
		_ = registry.DeletePassword(ctx, current.CredentialRef)
	}
	view := toRegistryView(updated)
	s.emitRegistryView(view)
	return view, nil
}

func (s *SystemService) ListRegistryProjects(id string) (RegistryProjectsView, error) {
	registries, err := s.registryStore()
	if err != nil {
		return RegistryProjectsView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	connection, err := registries.Get(ctx, id)
	if err != nil {
		return RegistryProjectsView{}, err
	}
	projects, err := registry.NewBrowser().ListProjects(ctx, connection)
	if err != nil {
		return RegistryProjectsView{
			RegistryID: id,
			Status:     registryBrowseStatus(err),
			Detail:     err.Error(),
		}, nil
	}
	result := RegistryProjectsView{
		RegistryID: id,
		Status:     string(registry.StatusHealthy),
		Detail:     "Harbor 项目列表已刷新",
		Projects:   make([]RegistryProjectView, 0, len(projects)),
	}
	for _, project := range projects {
		result.Projects = append(result.Projects, RegistryProjectView{
			Name:            project.Name,
			RepositoryCount: project.RepositoryCount,
		})
	}
	return result, nil
}

func (s *SystemService) ListRegistryRepositories(id, projectName string) (RegistryRepositoriesView, error) {
	registries, err := s.registryStore()
	if err != nil {
		return RegistryRepositoriesView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	connection, err := registries.Get(ctx, id)
	if err != nil {
		return RegistryRepositoriesView{}, err
	}
	repositories, err := registry.NewBrowser().ListRepositories(ctx, connection, projectName)
	if err != nil {
		return RegistryRepositoriesView{
			RegistryID: id,
			Status:     registryBrowseStatus(err),
			Detail:     err.Error(),
		}, nil
	}
	result := RegistryRepositoriesView{
		RegistryID:   id,
		Status:       string(registry.StatusHealthy),
		Detail:       "仓库列表已刷新",
		Repositories: make([]RegistryRepositoryView, 0, len(repositories)),
	}
	for _, repository := range repositories {
		result.Repositories = append(result.Repositories, RegistryRepositoryView{
			Name:          repository.Name,
			ArtifactCount: repository.ArtifactCount,
			PullCount:     repository.PullCount,
			UpdatedAt:     repository.UpdatedAt,
		})
	}
	return result, nil
}

func (s *SystemService) ListRepositoryArtifacts(registryID, repositoryName string) (RegistryArtifactsView, error) {
	registries, err := s.registryStore()
	if err != nil {
		return RegistryArtifactsView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	connection, err := registries.Get(ctx, registryID)
	if err != nil {
		return RegistryArtifactsView{}, err
	}
	artifacts, err := registry.NewBrowser().ListArtifacts(ctx, connection, repositoryName)
	if err != nil {
		return RegistryArtifactsView{
			RegistryID: registryID,
			Repository: repositoryName,
			Status:     registryBrowseStatus(err),
			Detail:     err.Error(),
		}, nil
	}
	result := RegistryArtifactsView{
		RegistryID: registryID,
		Repository: repositoryName,
		Status:     string(registry.StatusHealthy),
		Detail:     "镜像制品列表已刷新",
		Artifacts:  make([]RegistryArtifactView, 0, len(artifacts)),
	}
	for _, artifact := range artifacts {
		result.Artifacts = append(result.Artifacts, RegistryArtifactView{
			Digest:    artifact.Digest,
			Tags:      artifact.Tags,
			Size:      artifact.Size,
			CreatedAt: artifact.CreatedAt,
			MediaType: artifact.MediaType,
		})
	}
	return result, nil
}

func (s *SystemService) ListProjects() ([]ProjectView, error) {
	projects, err := s.projectStore()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	values, err := projects.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]ProjectView, 0, len(values))
	for _, value := range values {
		result = append(result, toProjectView(value))
	}
	return result, nil
}

func (s *SystemService) CreateProject(input ProjectInput) (ProjectView, error) {
	projects, err := s.projectStore()
	if err != nil {
		return ProjectView{}, err
	}
	value := toProjectInput(input)
	inspection := project.Inspection{}
	if value.SourceKind == project.SourceLocal {
		inspection, err = project.InspectLocal(value.LocalPath)
		if err != nil {
			return ProjectView{}, err
		}
		applyDetectedConfiguration(&value, inspection)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	created, err := projects.Create(ctx, value, inspection)
	if err != nil {
		return ProjectView{}, err
	}
	view := toProjectView(created)
	s.emitProjectView(view)
	return view, nil
}

func (s *SystemService) UpdateProject(id string, input ProjectInput) (ProjectView, error) {
	projects, err := s.projectStore()
	if err != nil {
		return ProjectView{}, err
	}
	value := toProjectInput(input)
	inspection := project.Inspection{}
	if value.SourceKind == project.SourceLocal {
		inspection, err = project.InspectLocal(value.LocalPath)
		if err != nil {
			return ProjectView{}, err
		}
		applyDetectedConfiguration(&value, inspection)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updated, err := projects.Update(ctx, id, value, inspection)
	if err != nil {
		return ProjectView{}, err
	}
	view := toProjectView(updated)
	s.emitProjectView(view)
	return view, nil
}

func (s *SystemService) InspectProject(id string) (ProjectView, error) {
	projects, err := s.projectStore()
	if err != nil {
		return ProjectView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	value, err := projects.Get(ctx, id)
	if err != nil {
		return ProjectView{}, err
	}
	if value.SourceKind != project.SourceLocal {
		return ProjectView{}, errors.New("Git 项目检查将在 Git 凭据与工作区缓存接入后开放")
	}
	inspection, err := project.InspectLocal(value.LocalPath)
	if err != nil {
		return ProjectView{}, err
	}
	updated, err := projects.UpdateInspection(ctx, id, inspection)
	if err != nil {
		return ProjectView{}, err
	}
	view := toProjectView(updated)
	s.emitProjectView(view)
	return view, nil
}

// ProjectGitStatus reads local worktree state. Network access is opt-in.
func (s *SystemService) ProjectGitStatus(id string, refreshRemote bool) (project.GitStatus, error) {
	projects, err := s.projectStore()
	if err != nil {
		return project.GitStatus{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	value, err := projects.Get(ctx, id)
	if err != nil {
		return project.GitStatus{}, err
	}
	if value.SourceKind != project.SourceLocal {
		return project.GitStatus{Detail: "Git 仓库地址尚未检出到本机，无法读取工作区状态"}, nil
	}
	return project.InspectGit(ctx, value.LocalPath, refreshRemote), nil
}

func (s *SystemService) DeleteProject(id string) error {
	projects, err := s.projectStore()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	deployments, err := s.deploymentStore()
	if err != nil {
		return err
	}
	refs, err := deployments.ProjectEnvironmentCredentialRefs(ctx, id)
	if err != nil {
		return err
	}
	if err := projects.Delete(ctx, id); err != nil {
		return err
	}
	cleanupEnvironmentSecrets(ctx, refs)
	return nil
}

func (s *SystemService) ListDeployments() ([]DeploymentView, error) {
	deployments, err := s.deploymentStore()
	if err != nil {
		return nil, err
	}
	projects, err := s.projectStore()
	if err != nil {
		return nil, err
	}
	servers, err := s.serverStore()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	targets, err := deployments.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]DeploymentView, 0, len(targets))
	for _, target := range targets {
		value, err := deploymentViewFor(ctx, projects, servers, target)
		if errors.Is(err, project.ErrNotFound) || errors.Is(err, server.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func (s *SystemService) CreateDeployment(input DeploymentInput) (DeploymentView, error) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if _, err := projects.Get(ctx, input.ProjectID); err != nil {
		return DeploymentView{}, err
	}
	if _, err := servers.Get(ctx, input.ServerID); err != nil {
		return DeploymentView{}, err
	}
	target, err := deployments.Create(ctx, deployment.Input{
		ProjectID: input.ProjectID, ServerID: input.ServerID, WorkloadKind: input.WorkloadKind,
		WorkloadName: input.WorkloadName, ContainerPorts: input.ContainerPorts, RestartPolicy: input.RestartPolicy,
	})
	if err != nil {
		return DeploymentView{}, err
	}
	view, err := deploymentViewFor(ctx, projects, servers, target)
	if err != nil {
		return DeploymentView{}, err
	}
	s.emitDeploymentView(view)
	return view, nil
}

// DeleteDeployment only removes iDeploy's local project-to-server association.
// It intentionally never changes remote containers or source files.
func (s *SystemService) DeleteDeployment(id string) error {
	deployments, err := s.deploymentStore()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	refs, err := deployments.EnvironmentCredentialRefs(ctx, id)
	if err != nil {
		return err
	}
	if err := deployments.Delete(ctx, id); err != nil {
		return err
	}
	cleanupEnvironmentSecrets(ctx, refs)
	return nil
}

func (s *SystemService) ListServers() ([]ServerView, error) {
	servers, err := s.serverStore()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connections, err := servers.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]ServerView, 0, len(connections))
	for _, connection := range connections {
		result = append(result, toServerView(connection))
	}
	return result, nil
}

func (s *SystemService) CreateServer(input ServerInput) (ServerView, error) {
	servers, err := s.serverStore()
	if err != nil {
		return ServerView{}, err
	}
	if input.AuthMethod == "" {
		input.AuthMethod = server.AuthMethodAgent
	}
	if err := server.ValidateAuthMethod(input.AuthMethod, input.Password, input.PrivateKeyPath); err != nil {
		return ServerView{}, err
	}
	if err := validatePrivateKey(input.AuthMethod, input.PrivateKeyPath); err != nil {
		return ServerView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connection, err := servers.Create(ctx, server.NewConnection{
		Name: input.Name, Host: input.Host, Port: input.Port, SSHUser: input.SSHUser,
		AuthMethod: input.AuthMethod, PrivateKeyPath: input.PrivateKeyPath,
	})
	if err != nil {
		return ServerView{}, err
	}
	credentialRef, secret := serverCredentialForInput(connection.ID, input)
	if credentialRef != "" {
		if err := server.SavePassword(ctx, credentialRef, secret); err != nil {
			_ = servers.Delete(ctx, connection.ID)
			return ServerView{}, err
		}
		connection, err = servers.UpdateCredentialRef(ctx, connection.ID, credentialRef)
		if err != nil {
			return ServerView{}, err
		}
	}
	view := toServerView(connection)
	s.emitServerView(view)
	return view, nil
}

func (s *SystemService) UpdateServer(id string, input ServerInput) (ServerView, error) {
	servers, err := s.serverStore()
	if err != nil {
		return ServerView{}, err
	}
	if input.AuthMethod == "" {
		input.AuthMethod = server.AuthMethodAgent
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, err := servers.Get(ctx, id)
	if err != nil {
		return ServerView{}, err
	}
	validationSecret := input.Password
	if input.AuthMethod == server.AuthMethodPassword && validationSecret == "" && connection.AuthMethod == server.AuthMethodPassword && connection.CredentialRef != "" {
		validationSecret = "stored"
	}
	if err := server.ValidateAuthMethod(input.AuthMethod, validationSecret, input.PrivateKeyPath); err != nil {
		return ServerView{}, err
	}
	if err := validatePrivateKey(input.AuthMethod, input.PrivateKeyPath); err != nil {
		return ServerView{}, err
	}
	credentialRef, secret := serverCredentialForInput(connection.ID, input)
	if credentialRef == "" && input.AuthMethod == connection.AuthMethod && connection.CredentialRef != "" {
		credentialRef = connection.CredentialRef
	}
	if secret != "" {
		if err := server.SavePassword(ctx, credentialRef, secret); err != nil {
			return ServerView{}, err
		}
	}
	updated, err := servers.UpdateConnection(ctx, id, server.NewConnection{
		Name: input.Name, Host: input.Host, Port: input.Port, SSHUser: input.SSHUser,
		AuthMethod: input.AuthMethod, PrivateKeyPath: input.PrivateKeyPath, CredentialRef: credentialRef,
	})
	if err != nil {
		return ServerView{}, err
	}
	if connection.CredentialRef != "" && connection.CredentialRef != credentialRef {
		if err := server.DeletePassword(ctx, connection.CredentialRef); err != nil {
			return ServerView{}, err
		}
	}
	view := toServerView(updated)
	s.emitServerView(view)
	return view, nil
}

// UpdateServerCredentials remains as a compatibility bridge for older
// frontends. New clients should use UpdateServer so address and login details
// are edited together like a conventional SSH connection profile.
func (s *SystemService) UpdateServerCredentials(id, authMethod, password string) (ServerView, error) {
	servers, err := s.serverStore()
	if err != nil {
		return ServerView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connection, err := servers.Get(ctx, id)
	if err != nil {
		return ServerView{}, err
	}
	return s.UpdateServer(id, ServerInput{
		Name: connection.Name, Host: connection.Host, Port: connection.Port, SSHUser: connection.SSHUser,
		AuthMethod: authMethod, Password: password, PrivateKeyPath: connection.PrivateKeyPath,
	})
}

func (s *SystemService) TestServer(id string) (JobView, error) {
	return s.enqueueServerJob(id, "server.probe", struct {
		ServerID string `json:"serverId"`
	}{ServerID: id})
}

func (s *SystemService) TestServerConnection(input ServerConnectionInput) (ServerConnectionTestView, error) {
	if input.AuthMethod == "" {
		input.AuthMethod = server.AuthMethodAgent
	}
	credentialRef := ""
	if input.ServerID != "" {
		servers, storeErr := s.serverStore()
		if storeErr != nil {
			return ServerConnectionTestView{}, storeErr
		}
		lookupCtx, lookupCancel := context.WithTimeout(context.Background(), 3*time.Second)
		stored, storeErr := servers.Get(lookupCtx, input.ServerID)
		lookupCancel()
		if storeErr != nil {
			return ServerConnectionTestView{}, storeErr
		}
		if stored.AuthMethod == input.AuthMethod && stored.CredentialRef != "" {
			credentialRef = stored.CredentialRef
		}
	}
	validationSecret := input.Password
	if input.AuthMethod == server.AuthMethodPassword && validationSecret == "" && credentialRef != "" {
		validationSecret = "stored"
	}
	if err := server.ValidateAuthMethod(input.AuthMethod, validationSecret, input.PrivateKeyPath); err != nil {
		return ServerConnectionTestView{}, err
	}
	if err := validatePrivateKey(input.AuthMethod, input.PrivateKeyPath); err != nil {
		return ServerConnectionTestView{}, err
	}
	validated, err := (server.NewConnection{
		Name: "connection-test", Host: input.Host, Port: input.Port, SSHUser: input.SSHUser,
		AuthMethod: input.AuthMethod, PrivateKeyPath: input.PrivateKeyPath, CredentialRef: credentialRef,
	}).Validate()
	if err != nil {
		return ServerConnectionTestView{}, err
	}
	connection := server.Connection{
		Host: validated.Host, Port: validated.Port, SSHUser: validated.SSHUser,
		AuthMethod: validated.AuthMethod, PrivateKeyPath: validated.PrivateKeyPath, CredentialRef: credentialRef,
	}
	secret := ""
	if input.AuthMethod == server.AuthMethodPassword {
		secret = input.Password
	} else if input.AuthMethod == server.AuthMethodPrivateKey {
		secret = input.PrivateKeyPassphrase
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result := server.NewRemote().TestConnection(ctx, connection, secret)
	return ServerConnectionTestView{Status: string(result.Status), Detail: result.Detail}, nil
}

func (s *SystemService) InspectSSHHostKey(input SSHHostKeyInput) (HostKeyView, error) {
	connection, err := validateSSHHostKeyInput(input)
	if err != nil {
		return HostKeyView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	value, err := server.InspectHostKey(ctx, connection)
	if err != nil {
		return HostKeyView{}, err
	}
	return HostKeyView{Status: string(value.Status), Host: value.Host, Fingerprint: value.Fingerprint, KeyType: value.KeyType, Detail: value.Detail}, nil
}

func (s *SystemService) TrustSSHHostKey(input SSHHostKeyInput, fingerprint string) (HostKeyView, error) {
	connection, err := validateSSHHostKeyInput(input)
	if err != nil {
		return HostKeyView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	value, err := server.TrustHostKey(ctx, connection, fingerprint)
	if err != nil {
		return HostKeyView{}, err
	}
	return HostKeyView{Status: string(value.Status), Host: value.Host, Fingerprint: value.Fingerprint, KeyType: value.KeyType, Detail: value.Detail}, nil
}

func validateSSHHostKeyInput(input SSHHostKeyInput) (server.Connection, error) {
	validated, err := (server.NewConnection{Name: "host-key", Host: input.Host, Port: input.Port, SSHUser: input.SSHUser}).Validate()
	if err != nil {
		return server.Connection{}, err
	}
	return server.Connection{Host: validated.Host, Port: validated.Port, SSHUser: validated.SSHUser}, nil
}

func validatePrivateKey(authMethod, path string) error {
	if authMethod != server.AuthMethodPrivateKey {
		return nil
	}
	info, err := os.Stat(strings.TrimSpace(path))
	if err != nil {
		return fmt.Errorf("无法读取 SSH 私钥文件: %w", err)
	}
	if info.IsDir() {
		return errors.New("SSH 私钥必须是文件，不能是目录")
	}
	return nil
}

func serverCredentialForInput(serverID string, input ServerInput) (string, string) {
	switch input.AuthMethod {
	case server.AuthMethodPassword:
		return server.PasswordCredentialRef(serverID), input.Password
	case server.AuthMethodPrivateKey:
		if input.PrivateKeyPassphrase != "" {
			return server.PrivateKeyPassphraseCredentialRef(serverID), input.PrivateKeyPassphrase
		}
	}
	return "", ""
}

func (s *SystemService) TestSavedServerConnection(id string) (ServerConnectionTestView, error) {
	servers, err := s.serverStore()
	if err != nil {
		return ServerConnectionTestView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	connection, err := servers.Get(ctx, id)
	if err != nil {
		return ServerConnectionTestView{}, err
	}
	result := server.NewRemote().TestConnection(ctx, connection, "")
	return ServerConnectionTestView{Status: string(result.Status), Detail: result.Detail}, nil
}

func (s *SystemService) InspectServerHostKey(id string) (HostKeyView, error) {
	servers, err := s.serverStore()
	if err != nil {
		return HostKeyView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	connection, err := servers.Get(ctx, id)
	if err != nil {
		return HostKeyView{}, err
	}
	value, err := server.InspectHostKey(ctx, connection)
	if err != nil {
		return HostKeyView{}, err
	}
	return HostKeyView{Status: string(value.Status), Host: value.Host, Fingerprint: value.Fingerprint, KeyType: value.KeyType, Detail: value.Detail}, nil
}

func (s *SystemService) TrustServerHostKey(id, fingerprint string) (HostKeyView, error) {
	servers, err := s.serverStore()
	if err != nil {
		return HostKeyView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	connection, err := servers.Get(ctx, id)
	if err != nil {
		return HostKeyView{}, err
	}
	value, err := server.TrustHostKey(ctx, connection, fingerprint)
	if err != nil {
		return HostKeyView{}, err
	}
	return HostKeyView{Status: string(value.Status), Host: value.Host, Fingerprint: value.Fingerprint, KeyType: value.KeyType, Detail: value.Detail}, nil
}

func (s *SystemService) ListRemoteContainers(serverID string) (RemoteContainersView, error) {
	servers, err := s.serverStore()
	if err != nil {
		return RemoteContainersView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	connection, err := servers.Get(ctx, serverID)
	if err != nil {
		return RemoteContainersView{}, err
	}
	if connection.Status != server.StatusHealthy {
		return RemoteContainersView{
			ServerID:   serverID,
			Status:     string(connection.Status),
			Detail:     "服务器尚未连接，已跳过远程 Docker 刷新。",
			Stale:      true,
			Containers: make([]RemoteContainerView, 0),
		}, nil
	}
	containers, err := server.NewRemote().ListContainers(ctx, connection)
	store, storeErr := s.deploymentStore()
	if storeErr != nil {
		return RemoteContainersView{}, storeErr
	}
	if err != nil {
		cached, observed, _ := store.Snapshot(ctx, serverID)
		result := RemoteContainersView{ServerID: serverID, Status: string(serverProbeStatus(err)), Detail: err.Error(), Stale: true, ObservedAt: observed, Containers: make([]RemoteContainerView, 0)}
		for _, c := range cached {
			result.Containers = append(result.Containers, toRemoteContainerView(c))
		}
		return result, nil
	}
	if err := store.SaveSnapshot(ctx, serverID, containers); err != nil {
		return RemoteContainersView{}, err
	}
	result := RemoteContainersView{ServerID: serverID, Status: string(server.StatusHealthy), Detail: "远程容器列表已刷新", ObservedAt: time.Now().UTC().Format(time.RFC3339), Containers: make([]RemoteContainerView, 0, len(containers))}
	for _, container := range containers {
		result.Containers = append(result.Containers, toRemoteContainerView(container))
	}
	return result, nil
}

func (s *SystemService) OperateRemoteContainer(serverID, containerID, action string) (JobView, error) {
	if action != "start" && action != "stop" && action != "restart" {
		return JobView{}, errors.New("unsupported container action")
	}
	return s.enqueueServerJob(serverID, "server.container.operation", struct {
		ServerID    string `json:"serverId"`
		ContainerID string `json:"containerId"`
		Action      string `json:"action"`
	}{ServerID: serverID, ContainerID: containerID, Action: action})
}

func (s *SystemService) DeleteServer(id string) error {
	servers, err := s.serverStore()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connection, err := servers.Get(ctx, id)
	if err != nil {
		return err
	}
	deployments, err := s.deploymentStore()
	if err != nil {
		return err
	}
	environmentRefs, err := deployments.ServerEnvironmentCredentialRefs(ctx, id)
	if err != nil {
		return err
	}
	if err := servers.Delete(ctx, id); err != nil {
		return err
	}
	// The database row is the source of truth. Remove the matching keychain
	// item on a best-effort basis after the row has been deleted.
	if connection.CredentialRef != "" {
		_ = server.DeletePassword(ctx, connection.CredentialRef)
	}
	cleanupEnvironmentSecrets(ctx, environmentRefs)
	return nil
}

func (s *SystemService) ClearRuntimeCache() (CleanupView, error) {
	deployments, err := s.deploymentStore()
	if err != nil {
		return CleanupView{}, err
	}
	jobs, paths, _, err := s.readyState()
	if err != nil {
		return CleanupView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	active, err := jobs.CountActive(ctx)
	if err != nil {
		return CleanupView{}, err
	}
	if active > 0 {
		return CleanupView{}, errors.New("仍有后台任务正在执行，请等待发布或检测完成后再清理缓存")
	}
	snapshots, err := deployments.ClearSnapshots(ctx)
	if err != nil {
		return CleanupView{}, fmt.Errorf("清除远程容器快照: %w", err)
	}
	items, err := clearDirectoryContents(paths.CacheDir)
	if err != nil {
		return CleanupView{}, err
	}
	return CleanupView{Snapshots: snapshots, CacheItems: items, Detail: "运行缓存已清理，服务器连接配置保持不变"}, nil
}

func (s *SystemService) ClearServerData() (CleanupView, error) {
	servers, err := s.serverStore()
	if err != nil {
		return CleanupView{}, err
	}
	_, paths, _, err := s.readyState()
	if err != nil {
		return CleanupView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	deployments, err := s.deploymentStore()
	if err != nil {
		return CleanupView{}, err
	}
	environmentRefs, err := deployments.AllEnvironmentCredentialRefs(ctx)
	if err != nil {
		return CleanupView{}, err
	}
	cleared, err := servers.ClearAll(ctx)
	if err != nil {
		return CleanupView{}, err
	}
	credentialErrors := int64(0)
	for _, ref := range cleared.CredentialRefs {
		if err := server.DeletePassword(ctx, ref); err != nil {
			credentialErrors++
		}
	}
	for _, ref := range environmentRefs {
		if err := deployment.DeleteEnvironmentSecret(ctx, ref); err != nil {
			credentialErrors++
		}
	}
	detail := "服务器本地数据已全部清除"
	if credentialErrors > 0 {
		detail = fmt.Sprintf("服务器数据已清除，但有 %d 个系统钥匙串凭据未能删除", credentialErrors)
	}
	cacheItems, cacheErr := clearDirectoryContents(paths.CacheDir)
	if cacheErr != nil {
		detail += "；文件缓存清理失败：" + cacheErr.Error()
	}
	return CleanupView{
		Servers: cleared.Servers, Deployments: cleared.Deployments, Snapshots: cleared.Snapshots,
		Jobs: cleared.Jobs, Credentials: int64(len(cleared.CredentialRefs)), CredentialErrors: credentialErrors,
		CacheItems: cacheItems, Detail: detail,
	}, nil
}

func clearDirectoryContents(path string) (int64, error) {
	cleaned := filepath.Clean(path)
	if cleaned == "." || cleaned == string(filepath.Separator) || filepath.Base(cleaned) != "iDeploy" {
		return 0, errors.New("拒绝清理不安全的缓存目录")
	}
	entries, err := os.ReadDir(cleaned)
	if errors.Is(err, os.ErrNotExist) {
		return 0, os.MkdirAll(cleaned, 0o700)
	}
	if err != nil {
		return 0, fmt.Errorf("读取缓存目录: %w", err)
	}
	var removed int64
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(cleaned, entry.Name())); err != nil {
			return removed, fmt.Errorf("清理缓存项 %q: %w", entry.Name(), err)
		}
		removed++
	}
	if err := os.Chmod(cleaned, 0o700); err != nil {
		return removed, fmt.Errorf("保护缓存目录权限: %w", err)
	}
	return removed, nil
}

func (s *SystemService) DeleteRegistry(id string) error {
	registries, err := s.registryStore()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connection, err := registries.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := registries.Delete(ctx, id); err != nil {
		return err
	}
	if connection.CredentialRef != "" {
		_ = registry.DeletePassword(ctx, connection.CredentialRef)
	}
	return nil
}

func (s *SystemService) TestRegistry(id string) (JobView, error) {
	registries, err := s.registryStore()
	if err != nil {
		return JobView{}, err
	}
	s.mu.RLock()
	runner := s.runner
	s.mu.RUnlock()
	if runner == nil {
		return JobView{}, errors.New("job runner is not ready")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := registries.Get(ctx, id); err != nil {
		return JobView{}, err
	}
	payload, err := json.Marshal(struct {
		RegistryID string `json:"registryId"`
	}{RegistryID: id})
	if err != nil {
		return JobView{}, fmt.Errorf("encode registry test job: %w", err)
	}
	queued, err := runner.Enqueue(ctx, job.NewJob{
		Type:    "registry.test",
		LockKey: "registry:" + id,
		Payload: string(payload),
	})
	if err != nil {
		return JobView{}, err
	}
	return toJobView(queued), nil
}

func (s *SystemService) enqueueServerJob(serverID, jobType string, payload any) (JobView, error) {
	servers, err := s.serverStore()
	if err != nil {
		return JobView{}, err
	}
	s.mu.RLock()
	runner := s.runner
	s.mu.RUnlock()
	if runner == nil {
		return JobView{}, errors.New("job runner is not ready")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := servers.Get(ctx, serverID); err != nil {
		return JobView{}, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return JobView{}, fmt.Errorf("encode server job: %w", err)
	}
	queued, err := runner.Enqueue(ctx, job.NewJob{Type: jobType, LockKey: "server:" + serverID, Payload: string(encoded)})
	if err != nil {
		return JobView{}, err
	}
	return toJobView(queued), nil
}

func (s *SystemService) ServiceShutdown() error {
	return s.close()
}

func (s *SystemService) close() error {
	s.mu.Lock()
	controlServer := s.controlServer
	runner := s.runner
	db := s.db
	runtimeLock := s.runtimeLock
	s.controlServer = nil
	s.runner = nil
	s.db = nil
	s.runtimeLock = nil
	s.store = nil
	s.registries = nil
	s.databases = nil
	s.servers = nil
	s.projects = nil
	s.deployments = nil
	s.mu.Unlock()

	if controlServer != nil {
		_ = controlServer.Close()
	}
	if runner != nil {
		runner.Stop()
	}
	var closeErr error
	if db != nil {
		closeErr = db.Close()
	}
	if runtimeLock != nil {
		if err := runtimeLock.Release(); closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func (s *SystemService) GetOverview() (Overview, error) {
	store, paths, recovered, err := s.readyState()
	if err != nil {
		return Overview{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	jobs, err := store.List(ctx, 20)
	if err != nil {
		return Overview{}, err
	}
	active, err := store.CountActive(ctx)
	if err != nil {
		return Overview{}, err
	}

	return Overview{
		Version:       appVersion,
		Platform:      runtime.GOOS,
		Architecture:  runtime.GOARCH,
		DatabasePath:  paths.DatabasePath,
		CachePath:     paths.CacheDir,
		LogPath:       paths.LogDir,
		RecoveredJobs: recovered,
		ActiveJobs:    active,
		Tools: []ToolStatus{
			inspectDocker(ctx),
			inspectTool(ctx, "Git", "git", "--version"),
			inspectTool(ctx, "SSH", "ssh", "-V"),
		},
		Jobs: mapJobs(jobs),
	}, nil
}

func (s *SystemService) RunDiagnostic() (JobView, error) {
	s.mu.RLock()
	runner := s.runner
	s.mu.RUnlock()
	if runner == nil {
		return JobView{}, errors.New("job runner is not ready")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	queued, err := runner.Enqueue(ctx, job.NewJob{
		Type:    "system.diagnostic",
		LockKey: "system:diagnostic",
		Payload: `{}`,
	})
	if err != nil {
		return JobView{}, err
	}
	return toJobView(queued), nil
}

func (s *SystemService) CancelJob(id string) error {
	s.mu.RLock()
	runner := s.runner
	s.mu.RUnlock()
	if runner == nil {
		return errors.New("job runner is not ready")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return runner.Cancel(ctx, id)
}

func (s *SystemService) readyState() (*job.Store, appPaths.AppPaths, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.store == nil {
		return nil, appPaths.AppPaths{}, 0, errors.New("system service is not ready")
	}
	return s.store, s.paths, s.recoveredJobs, nil
}

func (s *SystemService) registryStore() (*registry.Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.registries == nil {
		return nil, errors.New("registry service is not ready")
	}
	return s.registries, nil
}

func (s *SystemService) serverStore() (*server.Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.servers == nil {
		return nil, errors.New("server service is not ready")
	}
	return s.servers, nil
}

func (s *SystemService) projectStore() (*project.Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.projects == nil {
		return nil, errors.New("project service is not ready")
	}
	return s.projects, nil
}

func (s *SystemService) deploymentStore() (*deployment.Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.deployments == nil {
		return nil, errors.New("deployment service is not ready")
	}
	return s.deployments, nil
}

func (s *SystemService) databaseStore() (*databasebrowser.Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.databases == nil {
		return nil, errors.New("数据库浏览服务尚未初始化")
	}
	return s.databases, nil
}

func (s *SystemService) databaseConnectionContext(id string, timeout time.Duration) (*databasebrowser.Store, databasebrowser.Connection, string, context.Context, context.CancelFunc, error) {
	store, err := s.databaseStore()
	if err != nil {
		return nil, databasebrowser.Connection{}, "", nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	connection, err := store.Get(ctx, id)
	if err != nil {
		cancel()
		return nil, databasebrowser.Connection{}, "", nil, nil, err
	}
	password, err := databasebrowser.LoadPassword(ctx, connection.CredentialRef)
	if err != nil {
		cancel()
		return nil, databasebrowser.Connection{}, "", nil, nil, errors.New("此连接的数据库密码不可用，请编辑连接后重新保存")
	}
	return store, connection, password, ctx, cancel, nil
}

func toDatabaseConnection(input DatabaseConnectionInput, credentialRef string) databasebrowser.NewConnection {
	return databasebrowser.NewConnection{Name: input.Name, Driver: databasebrowser.Driver(input.Driver), Host: input.Host, Port: input.Port, DatabaseName: input.DatabaseName, Username: input.Username, SSLMode: input.SSLMode, CredentialRef: credentialRef}
}

func toDatabaseConnectionView(value databasebrowser.Connection) DatabaseConnectionView {
	lastConnected := ""
	if value.LastConnected != nil {
		lastConnected = value.LastConnected.UTC().Format(time.RFC3339Nano)
	}
	return DatabaseConnectionView{ID: value.ID, Name: value.Name, Driver: string(value.Driver), Host: value.Host, Port: value.Port, DatabaseName: value.DatabaseName, Username: value.Username, SSLMode: value.SSLMode, CredentialConfigured: value.CredentialRef != "", CreatedAt: value.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: value.UpdatedAt.UTC().Format(time.RFC3339Nano), LastConnectedAt: lastConnected, LastError: value.LastError}
}

func toDatabaseColumnViews(values []databasebrowser.Column) []DatabaseColumnView {
	result := make([]DatabaseColumnView, 0, len(values))
	for _, value := range values {
		result = append(result, DatabaseColumnView{Name: value.Name, DataType: value.DataType, Nullable: value.Nullable, DefaultValue: value.DefaultValue, PrimaryKey: value.PrimaryKey, Comment: value.Comment})
	}
	return result
}

// withDatabaseDiagnosticLog records connection test outcomes without ever
// writing a password or a full DSN. The returned error points users to the
// log file when an actionable failure occurred.
func (s *SystemService) withDatabaseDiagnosticLog(driver, host string, port int, databaseName, username, sslMode string, connectionErr error) error {
	s.mu.RLock()
	logDir := s.paths.LogDir
	s.mu.RUnlock()
	if logDir == "" {
		return connectionErr
	}
	logPath := filepath.Join(logDir, "database-connections.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err == nil {
		result := "success"
		detail := "read-only connection test passed"
		if connectionErr != nil {
			result = "failed"
			detail = strings.ReplaceAll(connectionErr.Error(), "\n", " ")
		}
		_, _ = fmt.Fprintf(file, "%s result=%s driver=%q host=%q port=%d database=%q username=%q tls=%q detail=%q\n", time.Now().UTC().Format(time.RFC3339Nano), result, driver, host, port, databaseName, username, sslMode, detail)
		_ = file.Close()
	}
	if connectionErr != nil {
		return fmt.Errorf("%w；诊断日志：%s", connectionErr, logPath)
	}
	return nil
}

func (s *SystemService) registryTestHandler(ctx context.Context, task job.Job, reporter job.Reporter) error {
	var payload struct {
		RegistryID string `json:"registryId"`
	}
	if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil || payload.RegistryID == "" {
		return errors.New("invalid registry test job payload")
	}
	registries, err := s.registryStore()
	if err != nil {
		return err
	}

	reporter.Progress(10, "Loading registry connection")
	connection, err := registries.Get(ctx, payload.RegistryID)
	if err != nil {
		return err
	}
	checking, err := registries.UpdateProbe(ctx, connection.ID, registry.StatusChecking, "正在测试连接")
	if err != nil {
		return err
	}
	s.emitRegistryView(toRegistryView(checking))

	reporter.Progress(45, "Connecting to registry")
	password := ""
	if connection.CredentialRef != "" {
		password, _ = registry.LoadPassword(ctx, connection.CredentialRef)
	}
	result := probeRegistryForPublishing(ctx, connection, password)
	updateContext, updateCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer updateCancel()
	updated, updateErr := registries.UpdateProbe(updateContext, connection.ID, result.Status, result.Detail)
	if updateErr != nil {
		return updateErr
	}
	s.emitRegistryView(toRegistryView(updated))
	reporter.Log(result.Detail)
	reporter.Progress(90, "Saving registry status")

	if result.Status == registry.StatusUnreachable || result.Status == registry.StatusDegraded {
		return errors.New(result.Detail)
	}
	return nil
}

func (s *SystemService) serverProbeHandler(ctx context.Context, task job.Job, reporter job.Reporter) error {
	var payload struct {
		ServerID string `json:"serverId"`
	}
	if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil || payload.ServerID == "" {
		return errors.New("invalid server probe job payload")
	}
	servers, err := s.serverStore()
	if err != nil {
		return err
	}
	reporter.Progress(10, "Loading server connection")
	connection, err := servers.Get(ctx, payload.ServerID)
	if err != nil {
		return err
	}
	checking, err := servers.UpdateProbe(ctx, connection.ID, server.StatusChecking, "正在检查 SSH 与 Docker 环境")
	if err != nil {
		return err
	}
	s.emitServerView(toServerView(checking))
	reporter.Progress(45, "Connecting over SSH")
	result := server.NewRemote().Probe(ctx, connection)
	updateContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	updated, err := servers.UpdateProbe(updateContext, connection.ID, result.Status, result.Detail)
	if err != nil {
		return err
	}
	s.emitServerView(toServerView(updated))
	reporter.Log(result.Detail)
	reporter.Progress(90, "Saving server status")
	if result.Status != server.StatusHealthy {
		return errors.New(result.Detail)
	}
	return nil
}

func (s *SystemService) serverContainerOperationHandler(ctx context.Context, task job.Job, reporter job.Reporter) error {
	var payload struct {
		ServerID    string `json:"serverId"`
		ContainerID string `json:"containerId"`
		Action      string `json:"action"`
	}
	if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil || payload.ServerID == "" || payload.ContainerID == "" {
		return errors.New("invalid server container operation payload")
	}
	servers, err := s.serverStore()
	if err != nil {
		return err
	}
	connection, err := servers.Get(ctx, payload.ServerID)
	if err != nil {
		return err
	}
	reporter.Progress(20, "Connecting to remote Docker")
	if err := server.NewRemote().OperateContainer(ctx, connection, payload.Action, payload.ContainerID); err != nil {
		return err
	}
	reporter.Log("docker " + payload.Action + " " + payload.ContainerID)
	reporter.Progress(90, "Remote Docker command completed")
	return nil
}

func (s *SystemService) emitRegistryView(view RegistryView) {
	s.mu.RLock()
	emit := s.emitRegistry
	s.mu.RUnlock()
	emit(view)
}

func (s *SystemService) emitServerView(view ServerView) {
	s.mu.RLock()
	emit := s.emitServer
	s.mu.RUnlock()
	emit(view)
}

func (s *SystemService) emitProjectView(view ProjectView) {
	s.mu.RLock()
	emit := s.emitProject
	s.mu.RUnlock()
	emit(view)
}

func (s *SystemService) emitDeploymentView(view DeploymentView) {
	s.mu.RLock()
	emit := s.emitDeployment
	s.mu.RUnlock()
	emit(view)
}

func registryBrowseStatus(err error) string {
	var browseError *registry.BrowseError
	if errors.As(err, &browseError) {
		return string(browseError.Status)
	}
	return string(registry.StatusDegraded)
}

func serverProbeStatus(err error) server.Status {
	detail := err.Error()
	lower := strings.ToLower(detail)
	if strings.Contains(lower, "host key verification failed") || strings.Contains(lower, "remote host identification has changed") {
		return server.StatusAuthRequired
	}
	if strings.Contains(detail, "Permission denied") || strings.Contains(detail, "Authentication failed") {
		return server.StatusAuthRequired
	}
	if strings.Contains(detail, "No route to host") || strings.Contains(detail, "Connection timed out") || strings.Contains(detail, "Could not resolve hostname") || strings.Contains(detail, "Connection refused") {
		return server.StatusUnreachable
	}
	return server.StatusDegraded
}

func diagnosticHandler(ctx context.Context, _ job.Job, reporter job.Reporter) error {
	steps := []struct {
		progress int
		message  string
	}{
		{20, "Checking local data store"},
		{45, "Checking job persistence"},
		{70, "Checking desktop event bridge"},
		{90, "Finalising diagnostics"},
	}

	for _, step := range steps {
		reporter.Progress(step.progress, step.message)
		reporter.Log(step.message)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(450 * time.Millisecond):
		}
	}
	return nil
}

func inspectTool(parent context.Context, displayName, executable string, args ...string) ToolStatus {
	resolved, err := toolchain.ResolveExecutable(parent, executable)
	if err != nil {
		return ToolStatus{Name: displayName, Available: false, Detail: err.Error()}
	}
	path := resolved.Path
	sourceLabel := "应用 PATH"
	if resolved.Source == toolchain.SourceZshStartup {
		sourceLabel = ".zprofile / .zshrc"
	}

	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path, args...)
	combined, commandErr := command.CombinedOutput()
	version := strings.TrimSpace(string(combined))
	if ctx.Err() != nil {
		return ToolStatus{Name: displayName, Available: false, Path: path, Source: string(resolved.Source), Detail: sourceLabel + " · Version check timed out"}
	}
	if commandErr != nil {
		return ToolStatus{Name: displayName, Available: false, Path: path, Source: string(resolved.Source), Version: version, Detail: fmt.Sprintf("%s · Version check failed: %v", sourceLabel, commandErr)}
	}
	return ToolStatus{Name: displayName, Available: true, Path: path, Source: string(resolved.Source), Version: version, Detail: sourceLabel}
}

// inspectDocker distinguishes an installed CLI from a reachable Docker daemon.
// `docker version --format ...` looks like a client version check but still
// contacts the daemon, which made the old overview error opaque.
func inspectDocker(parent context.Context) ToolStatus {
	resolved, err := toolchain.ResolveExecutable(parent, "docker")
	if err != nil {
		return ToolStatus{Name: "Docker", Available: false, Detail: err.Error()}
	}
	sourceLabel := "应用 PATH"
	if resolved.Source == toolchain.SourceZshStartup {
		sourceLabel = ".zprofile / .zshrc"
	}
	clientCtx, cancelClient := context.WithTimeout(parent, 2*time.Second)
	defer cancelClient()
	clientOutput, clientErr := exec.CommandContext(clientCtx, resolved.Path, "--version").CombinedOutput()
	clientVersion := strings.TrimSpace(string(clientOutput))
	if clientCtx.Err() != nil {
		return ToolStatus{Name: "Docker", Available: false, Path: resolved.Path, Source: string(resolved.Source), Detail: sourceLabel + " · Docker 客户端版本检查超时"}
	}
	if clientErr != nil {
		return ToolStatus{Name: "Docker", Available: false, Path: resolved.Path, Source: string(resolved.Source), Detail: fmt.Sprintf("%s · Docker 客户端不可用: %v", sourceLabel, clientErr)}
	}

	daemonCtx, cancelDaemon := context.WithTimeout(parent, 2*time.Second)
	defer cancelDaemon()
	serverOutput, daemonErr := exec.CommandContext(daemonCtx, resolved.Path, "info", "--format", "{{.ServerVersion}}").CombinedOutput()
	serverVersion := strings.TrimSpace(string(serverOutput))
	if daemonCtx.Err() != nil {
		return ToolStatus{Name: "Docker", Available: false, Path: resolved.Path, Source: string(resolved.Source), Version: clientVersion, Detail: sourceLabel + " · 客户端已安装，Docker daemon 检查超时"}
	}
	if daemonErr != nil {
		detail := summarizeCommandOutput(serverVersion)
		if detail == "" {
			detail = daemonErr.Error()
		}
		return ToolStatus{Name: "Docker", Available: false, Path: resolved.Path, Source: string(resolved.Source), Version: clientVersion, Detail: sourceLabel + " · 客户端已安装，Docker daemon 不可用: " + detail}
	}
	return ToolStatus{Name: "Docker", Available: true, Path: resolved.Path, Source: string(resolved.Source), Version: clientVersion + " · daemon " + serverVersion, Detail: sourceLabel}
}

func summarizeCommandOutput(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 180 {
		return value[:180] + "…"
	}
	return value
}

func mapJobs(values []job.Job) []JobView {
	result := make([]JobView, 0, len(values))
	for _, value := range values {
		result = append(result, toJobView(value))
	}
	return result
}

func toJobView(value job.Job) JobView {
	return JobView{
		LockKey:   value.LockKey,
		ID:        value.ID,
		Type:      value.Type,
		Status:    string(value.Status),
		Progress:  value.Progress,
		Message:   value.Message,
		Error:     value.Error,
		Attempt:   value.Attempt,
		CreatedAt: value.CreatedAt.Format(time.RFC3339),
		UpdatedAt: value.UpdatedAt.Format(time.RFC3339),
	}
}

func toRegistryView(value registry.Connection) RegistryView {
	lastCheckedAt := ""
	if value.LastCheckedAt != nil {
		lastCheckedAt = value.LastCheckedAt.Format(time.RFC3339)
	}
	return RegistryView{
		ID:                   value.ID,
		Name:                 value.Name,
		Kind:                 string(value.Kind),
		Endpoint:             value.Endpoint,
		Username:             value.Username,
		Status:               string(value.Status),
		LastCheckedAt:        lastCheckedAt,
		LastMessage:          value.LastMessage,
		LastError:            value.LastError,
		CredentialConfigured: value.CredentialRef != "",
	}
}

func toServerView(value server.Connection) ServerView {
	lastCheckedAt := ""
	if value.LastCheckedAt != nil {
		lastCheckedAt = value.LastCheckedAt.Format(time.RFC3339)
	}
	return ServerView{
		ID: value.ID, Name: value.Name, Host: value.Host, Port: value.Port, SSHUser: value.SSHUser,
		AuthMethod: value.AuthMethod, PrivateKeyPath: value.PrivateKeyPath,
		CredentialConfigured: value.CredentialRef != "",
		Status:               string(value.Status), LastCheckedAt: lastCheckedAt, LastMessage: value.LastMessage, LastError: value.LastError,
	}
}

func toProjectInput(value ProjectInput) project.Input {
	return project.Input{
		Name: value.Name, Description: value.Description, SourceKind: project.SourceKind(value.SourceKind),
		LocalPath: value.LocalPath, RepositoryURL: value.RepositoryURL, RefKind: project.RefKind(value.RefKind),
		RefValue: value.RefValue, ContextPath: value.ContextPath, DockerfilePath: value.DockerfilePath,
		ComposePath: value.ComposePath, ImageRepository: value.ImageRepository,
	}
}

func applyDetectedConfiguration(value *project.Input, inspection project.Inspection) {
	if value.DockerfilePath == "" {
		value.DockerfilePath = inspection.DockerfilePath
	}
	if value.ComposePath == "" {
		value.ComposePath = inspection.ComposePath
	}
}

func toProjectView(value project.Project) ProjectView {
	lastCheckedAt := ""
	if value.LastCheckedAt != nil {
		lastCheckedAt = value.LastCheckedAt.Format(time.RFC3339)
	}
	return ProjectView{
		ID: value.ID, Name: value.Name, Description: value.Description, SourceKind: string(value.SourceKind),
		LocalPath: value.LocalPath, RepositoryURL: value.RepositoryURL, RefKind: string(value.RefKind),
		RefValue: value.RefValue, ResolvedCommit: value.ResolvedCommit, Status: string(value.Status),
		LastCheckedAt: lastCheckedAt, LastError: value.LastError, ContextPath: value.ContextPath,
		DockerfilePath: value.DockerfilePath, ComposePath: value.ComposePath, ImageRepository: value.ImageRepository,
		CreatedAt: value.CreatedAt.Format(time.RFC3339), UpdatedAt: value.UpdatedAt.Format(time.RFC3339),
	}
}

func deploymentViewFor(ctx context.Context, projects *project.Store, servers *server.Store, target deployment.Target) (DeploymentView, error) {
	workspaceProject, err := projects.Get(ctx, target.ProjectID)
	if err != nil {
		return DeploymentView{}, err
	}
	remoteServer, err := servers.Get(ctx, target.ServerID)
	if err != nil {
		return DeploymentView{}, err
	}
	return DeploymentView{
		WorkloadKind: target.WorkloadKind, WorkloadName: target.WorkloadName,
		ID: target.ID, ProjectID: workspaceProject.ID, ProjectName: workspaceProject.Name,
		ProjectStatus: string(workspaceProject.Status), ImageRepository: workspaceProject.ImageRepository,
		ComposePath: workspaceProject.ComposePath, ServerID: remoteServer.ID, ServerName: remoteServer.Name,
		ServerStatus: string(remoteServer.Status), DesiredState: string(target.DesiredState),
		ContainerPorts: target.ContainerPorts, RestartPolicy: target.RestartPolicy,
		EnvironmentRevision: target.EnvironmentRevision, AppliedEnvironmentRevision: target.AppliedEnvironmentRevision,
		EnvironmentPendingApply: target.EnvironmentRevision != target.AppliedEnvironmentRevision,
		CreatedAt:               target.CreatedAt.Format(time.RFC3339), UpdatedAt: target.UpdatedAt.Format(time.RFC3339),
	}, nil
}

func toDeploymentReleaseView(value deployment.Release) DeploymentReleaseView {
	finishedAt := ""
	if value.FinishedAt != nil {
		finishedAt = value.FinishedAt.Format(time.RFC3339)
	}
	return DeploymentReleaseView{
		ArtifactPinned: validateRollbackArtifact(value.ImageRef, value.Manifest) == nil,
		ID:             value.ID, DeploymentID: value.DeploymentID, RegistryID: value.RegistryID, JobID: value.JobID,
		Action: value.Action, Version: value.Version, ImageRef: value.ImageRef,
		SourceRevision: value.SourceRevision, RollbackToID: value.RollbackToID,
		Status: string(value.Status), Detail: value.Detail,
		EnvironmentRevision: value.EnvironmentRevision,
		CreatedAt:           value.CreatedAt.Format(time.RFC3339), FinishedAt: finishedAt,
	}
}
