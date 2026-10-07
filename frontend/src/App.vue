<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { Clipboard, Events } from '@wailsio/runtime'
import { SystemService } from '../bindings/ideploy'
import { releaseVersionIssue } from './release-version'

type ToolStatus = {
  name: string
  available: boolean
  version: string
  path: string
  source: string
  detail: string
}

type JobView = {
	lockKey: string
  id: string
  type: string
  status: string
  progress: number
  message: string
  error: string
  attempt: number
  createdAt: string
  updatedAt: string
}

type Overview = {
  version: string
  platform: string
  architecture: string
  databasePath: string
  cachePath: string
  logPath: string
  recoveredJobs: number
  activeJobs: number
  tools: ToolStatus[]
  jobs: JobView[]
}

type CleanupView = {
  servers: number
  deployments: number
  snapshots: number
  jobs: number
  credentials: number
  credentialErrors: number
  cacheItems: number
  detail: string
}

type RegistryView = {
  id: string
  name: string
  kind: 'harbor' | 'oci'
  endpoint: string
  username: string
  status: 'unknown' | 'checking' | 'healthy' | 'auth_required' | 'degraded' | 'unreachable'
  lastCheckedAt: string
  lastMessage: string
  lastError: string
  credentialConfigured: boolean
}

type RegistryInput = {
  name: string
  kind: 'harbor' | 'oci'
  endpoint: string
  username: string
  password: string
}

type DatabaseDriver = 'postgres' | 'mysql'
type DatabaseConnectionInput = {
  name: string
  driver: DatabaseDriver
  host: string
  port: number
  databaseName: string
  username: string
  password: string
  sslMode: 'require' | 'disable'
}
type DatabaseConnectionView = Omit<DatabaseConnectionInput, 'password'> & {
  id: string
  credentialConfigured: boolean
  createdAt: string
  updatedAt: string
  lastConnectedAt: string
  lastError: string
}
type DatabaseSchemaView = { name: string }
type DatabaseOverviewView = { version: string; databaseSizeBytes: number | null; sizeDetail: string }
type DatabaseTableView = { schema: string; name: string; type: string }
type DatabaseColumnView = { name: string; dataType: string; nullable: boolean; defaultValue: string; primaryKey: boolean; comment: string }
type DatabaseIndexView = { name: string; columns: string[]; unique: boolean }
type DatabaseTableQuery = { page: number; pageSize: number; sortColumn: string; sortDesc: boolean }
type DatabaseTablePageView = { columns: DatabaseColumnView[]; rows: Record<string, string>[]; page: number; pageSize: number; hasNext: boolean; sortColumn: string; sortDesc: boolean }

type RegistryProbeView = {
  endpoint: string
  status: 'healthy' | 'auth_required' | 'degraded' | 'unreachable'
  detail: string
}

type RegistryRepositoryView = {
  name: string
  artifactCount: number
  pullCount: number
  updatedAt: string
}

type RegistryRepositoriesView = {
  registryId: string
  status: RegistryView['status']
  detail: string
  repositories: RegistryRepositoryView[]
}

type RegistryProjectView = {
  name: string
  repositoryCount: number
}

type RegistryProjectsView = {
  registryId: string
  status: RegistryView['status']
  detail: string
  projects: RegistryProjectView[]
}

type RegistryArtifactView = {
  digest: string
  tags: string[]
  size: number
  createdAt: string
  mediaType: string
}

type RegistryArtifactsView = {
  registryId: string
  repository: string
  status: RegistryView['status']
  detail: string
  artifacts: RegistryArtifactView[]
}

type ServerStatus = 'unknown' | 'checking' | 'healthy' | 'auth_required' | 'degraded' | 'unreachable'
type SSHAuthMethod = 'agent' | 'password' | 'private_key'

type ServerView = {
  id: string
  name: string
  host: string
  port: number
  sshUser: string
  authMethod: SSHAuthMethod
  privateKeyPath: string
  credentialConfigured: boolean
  status: ServerStatus
  lastCheckedAt: string
  lastMessage: string
  lastError: string
}

type HostKeyView = {
  status: 'unknown' | 'trusted' | 'changed'
  host: string
  fingerprint: string
  keyType: string
  detail: string
}

type ServerInput = {
  name: string
  host: string
  port: number
  sshUser: string
  authMethod: SSHAuthMethod
  password: string
  privateKeyPath: string
  privateKeyPassphrase: string
}

type ServerConnectionTestView = {
  status: ServerStatus
  detail: string
}

type RemoteContainerView = {
  composeProject: string
  health: string
  oneOff: boolean
  id: string
  name: string
  image: string
  state: string
  status: string
  ports: string
  created: string
}

type RemoteContainersView = {
  observedAt?: string
  stale?: boolean
  serverId: string
  status: ServerStatus
  detail: string
  containers: RemoteContainerView[]
}

type ProjectStatus = 'ready' | 'needs_configuration' | 'unavailable' | 'checking' | 'failed'

type ProjectView = {
  id: string
  name: string
  description: string
  sourceKind: 'local' | 'git'
  localPath: string
  repositoryUrl: string
  refKind: 'branch' | 'tag' | 'commit'
  refValue: string
  resolvedCommit: string
  status: ProjectStatus
  lastCheckedAt: string
  lastError: string
  contextPath: string
  dockerfilePath: string
  composePath: string
  imageRepository: string
  createdAt: string
  updatedAt: string
}

type ProjectInput = Omit<ProjectView, 'id' | 'resolvedCommit' | 'status' | 'lastCheckedAt' | 'lastError' | 'createdAt' | 'updatedAt'>

type ProjectGitStatusView = {
  available: boolean
  detail: string
  root: string
  branch: string
  head: string
  upstream: string
  changedFiles: number
  stagedFiles: number
  untracked: number
  ahead: number
  behind: number
  remoteFresh: boolean
  checkedAt: string
}

type DeploymentVersionSuggestionView = {
  currentVersion: string
  usedVersions: string[]
  version: string
  source: string
  detail: string
}

type AISettingsView = { deepSeekConfigured: boolean }
type BuildProxyMode = 'disabled' | 'preferred' | 'required'
type BuildProxyRule = { upstream: string; mirror: string }
type BuildProxySettingsView = { mode: BuildProxyMode; rules: BuildProxyRule[]; configured: boolean; builderName: string; detail: string }
type CommandLineToolStatus = { installed: boolean; conflict: boolean; commandPath: string; executablePath: string; detail: string }
type DockerfileGenerationView = {
  content: string
  technology: string
  source: string
  generationMode: string
  detail: string
  warnings: string[]
}

type ProjectPage = 'list' | 'detail'

type DeploymentView = {
  workloadKind: string
  workloadName: string
  id: string
  projectId: string
  projectName: string
  projectStatus: ProjectStatus
  imageRepository: string
  composePath: string
  serverId: string
  serverName: string
  serverStatus: ServerStatus
  desiredState: 'running' | 'stopped'
  containerPorts: string[]
  restartPolicy: string
  createdAt: string
  updatedAt: string
}

type DeploymentInput = { projectId: string; serverId: string; workloadKind: string; workloadName: string; containerPorts: string[]; restartPolicy: string }
type DeploymentPreflightAction = 'project' | 'dockerfile' | 'image' | 'workload' | 'server' | 'registry' | 'docker' | 'settings'
type DeploymentPreflightCheck = {
  id: string
  label: string
  detail: string
  state: 'ready' | 'warning' | 'blocked'
  action?: DeploymentPreflightAction
  actionLabel?: string
}
type DeploymentReleaseView = {
  artifactPinned: boolean
  id: string
  deploymentId: string
  registryId: string
  jobId: string
  action: 'deploy' | 'rollback'
  version: string
  imageRef: string
  sourceRevision: string
  rollbackToId: string
  status: 'queued' | 'preparing' | 'building' | 'pushing' | 'pulling' | 'deploying' | 'checking' | 'succeeded' | 'failed' | 'canceled'
  detail: string
  createdAt: string
  finishedAt: string
}
type DeploymentRuntimeStatus = 'running' | 'degraded' | 'stopped' | 'not_deployed' | 'unreachable' | 'needs_configuration' | 'unknown'
type DeploymentRuntime = { deployment: DeploymentView; status: DeploymentRuntimeStatus; detail: string; containers: RemoteContainerView[] }

type RegistryPage = 'connections' | 'projects' | 'repositories' | 'artifacts'

type SectionKey = 'overview' | 'projects' | 'deployments' | 'servers' | 'registries' | 'databases' | 'settings'

type SectionMeta = {
  eyebrow: string
  title: string
  description: string
  emptyTitle: string
  items: string[]
}

const sectionMeta: Record<SectionKey, SectionMeta> = {
  overview: {
    eyebrow: 'WORKSPACE OVERVIEW',
    title: '准备开始交付',
    description: '',
    emptyTitle: '',
    items: [],
  },
  projects: {
    eyebrow: 'PROJECT WORKSPACE',
    title: '项目',
    description: '集中管理本地目录与 Git 仓库，维护每个项目的构建和发布配置。',
    emptyTitle: '还没有添加项目',
    items: ['本地项目目录', 'Git 仓库与分支', 'Docker 构建配置'],
  },
  deployments: {
    eyebrow: 'DEPLOYMENT CONTROL',
    title: '部署项目',
    description: '查看期望版本与服务器运行状态，并在同一操作流中完成发布和回滚。',
    emptyTitle: '还没有部署项目',
    items: ['期望与实际状态', '发布、停止与回滚', '操作记录与运行日志'],
  },
  servers: {
    eyebrow: 'REMOTE HOSTS',
    title: '服务器',
    description: '通过 SSH 管理远程主机，检查连接状态与 Docker 运行环境。',
    emptyTitle: '还没有连接服务器',
    items: ['SSH 连接验证', '主机资源状态', '远程 Docker 环境'],
  },
  registries: {
    eyebrow: 'IMAGE REGISTRIES',
    title: '镜像仓库',
    description: '连接 Harbor 等 Registry，管理镜像、项目空间以及远程镜像代理。',
    emptyTitle: '还没有配置镜像仓库',
    items: ['Harbor 连接', '镜像与标签管理', '代理缓存配置'],
  },
  databases: {
    eyebrow: 'REMOTE DATABASES',
    title: '数据库',
    description: '以只读方式浏览远程 PostgreSQL、MySQL 与 MariaDB 的结构和数据。',
    emptyTitle: '还没有配置数据库连接',
    items: ['安全保存连接凭据', '浏览表与字段', '受限分页读取数据'],
  },
  settings: {
    eyebrow: 'APPLICATION SETTINGS',
    title: '设置',
    description: '管理 iDeploy 的本地缓存、连接数据和安全边界。',
    emptyTitle: '',
    items: [],
  },
}

const overview = ref<Overview | null>(null)
const activeSection = ref<SectionKey>('overview')
const loading = ref(true)
const refreshing = ref(false)
const actionPending = ref(false)
const errorMessage = ref('')
const registries = ref<RegistryView[]>([])
const registryLoading = ref(false)
const registrySaving = ref(false)
const registryTestingID = ref('')
const registryError = ref('')
const registryDialogOpen = ref(false)
const editingRegistryID = ref('')
const registryFormTesting = ref(false)
const registryFormTestError = ref('')
const registryFormProbe = ref<RegistryProbeView | null>(null)
const selectedRegistryID = ref('')
const selectedProjectName = ref('')
const selectedRepositoryName = ref('')
const projectLoading = ref(false)
const repositoryLoading = ref(false)
const artifactLoading = ref(false)
const projectResult = ref<RegistryProjectsView | null>(null)
const repositoryResult = ref<RegistryRepositoriesView | null>(null)
const artifactResult = ref<RegistryArtifactsView | null>(null)
const registryPage = ref<RegistryPage>('connections')
const copiedArtifactKey = ref('')
const registryForm = reactive<RegistryInput>({
  name: '',
  kind: 'harbor',
  endpoint: '',
  username: '',
  password: '',
})
const databaseConnections = ref<DatabaseConnectionView[]>([])
const databaseLoading = ref(false)
const databaseSaving = ref(false)
const databaseError = ref('')
const databaseDialogOpen = ref(false)
const databaseFormTesting = ref(false)
const databaseFormTestSuccess = ref('')
const editingDatabaseID = ref('')
const selectedDatabaseID = ref('')
const databaseConnectionsCollapsed = ref(false)
const databaseSchemas = ref<DatabaseSchemaView[]>([])
const databaseOverview = ref<DatabaseOverviewView | null>(null)
const databaseOverviewLoading = ref(false)
const databaseOverviewError = ref('')
let databaseOverviewRequestID = 0
const databaseTables = ref<DatabaseTableView[]>([])
const selectedDatabaseSchema = ref('')
const selectedDatabaseTable = ref<DatabaseTableView | null>(null)
const databaseColumns = ref<DatabaseColumnView[]>([])
const databaseIndexes = ref<DatabaseIndexView[]>([])
const databaseTablePage = ref<DatabaseTablePageView | null>(null)
const databaseDetailTab = ref<'columns' | 'data' | 'indexes'>('columns')
const databaseSchemaLoading = ref(false)
const databaseTableLoading = ref(false)
const databaseDetailLoading = ref(false)
const savedDatabaseTestingID = ref('')
const databasePasswordVisible = ref(false)
const emptyDatabaseForm = (): DatabaseConnectionInput => ({ name: '', driver: 'postgres', host: '', port: 5432, databaseName: '', username: '', password: '', sslMode: 'require' })
const databaseForm = reactive<DatabaseConnectionInput>(emptyDatabaseForm())
const servers = ref<ServerView[]>([])
const serverLoading = ref(false)
const serverSaving = ref(false)
const serverTestingID = ref('')
const serverError = ref('')
const serverDialogOpen = ref(false)
const serverCredentialDialogOpen = ref(false)
const serverCredentialSaving = ref(false)
const editingServerCredentialID = ref('')
const serverFormTesting = ref(false)
const serverFormTestResult = ref<ServerConnectionTestView | null>(null)
const serverFormTestError = ref('')
const serverCredentialTesting = ref(false)
const serverCredentialTestResult = ref<ServerConnectionTestView | null>(null)
const serverCredentialTestError = ref('')
const serverFormHostKey = ref<HostKeyView | null>(null)
const serverCredentialHostKey = ref<HostKeyView | null>(null)
const dialogHostKeyLoading = ref<'' | 'create' | 'edit'>('')
const dialogHostKeyTrusting = ref<'' | 'create' | 'edit'>('')
const dialogHostKeyConfirming = ref<'' | 'create' | 'edit'>('')
const secretVisibility = reactive({ createPassword: false, createPassphrase: false, editPassword: false, editPassphrase: false, registryPassword: false })
const savedConnectionTestingID = ref('')
const savedConnectionTests = ref<Record<string, ServerConnectionTestView>>({})
const selectedServerID = ref('')
const serverHostKeys = ref<Record<string, HostKeyView>>({})
const hostKeyLoadingID = ref('')
const hostKeyTrustingID = ref('')
const hostKeyConfirmingID = ref('')
const remoteContainers = ref<RemoteContainersView | null>(null)
const containerLoading = ref(false)
const containerActionKey = ref('')
const emptyServerForm = (): ServerInput => ({ name: '', host: '', port: 22, sshUser: '', authMethod: 'agent', password: '', privateKeyPath: '', privateKeyPassphrase: '' })
const serverForm = reactive<ServerInput>(emptyServerForm())
const serverCredentialForm = reactive<ServerInput>(emptyServerForm())
const workspaceProjects = ref<ProjectView[]>([])
const workspaceProjectLoading = ref(false)
const workspaceProjectSaving = ref(false)
const workspaceProjectInspectingID = ref('')
const projectGitStatuses = reactive<Record<string, ProjectGitStatusView>>({})
const projectGitCheckingID = ref('')
const workspaceProjectError = ref('')
const workspaceProjectDialogOpen = ref(false)
const dockerfileDialogOpen = ref(false)
const dockerfileGenerating = ref(false)
const dockerfileSaving = ref(false)
const dockerfileGenerationError = ref('')
const dockerfileProjectID = ref('')
const dockerfileOverwriteConfirmed = ref(false)
const dockerfileMode = ref<'auto' | 'deepseek'>('auto')
const dockerfileGeneration = reactive<DockerfileGenerationView>({ content: '', technology: '', source: '', generationMode: '', detail: '', warnings: [] })
const workspaceProjectPage = ref<ProjectPage>('list')
const selectedWorkspaceProjectID = ref('')
const editingWorkspaceProjectID = ref('')
const workspaceProjectForm = reactive<ProjectInput>({
  name: '', description: '', sourceKind: 'local', localPath: '', repositoryUrl: '', refKind: 'branch', refValue: 'main',
  contextPath: '.', dockerfilePath: '', composePath: '', imageRepository: '',
})
const deployments = ref<DeploymentView[]>([])
const deploymentLoading = ref(false)
const deploymentRefreshing = ref(false)
const deploymentSaving = ref(false)
const deploymentError = ref('')
const deploymentDialogOpen = ref(false)
const deploymentActionID = ref('')
const deploymentProjectFilter = ref('all')
const deploymentServerFilter = ref('all')
const deploymentStatusFilter = ref('all')
const deploymentSearch = ref('')
const deploymentContainers = ref<Record<string, RemoteContainersView>>({})
const deploymentForm = reactive<DeploymentInput>({ projectId: '', serverId: '', workloadKind: 'container', workloadName: '', containerPorts: [], restartPolicy: 'unless-stopped' })
const deploymentPortsText = ref('')
const editingDeploymentID = ref('')
const deploymentFormError = ref('')
const releaseDialogOpen = ref(false)
const releaseSaving = ref(false)
const publishingDeploymentID = ref('')
const releaseFormError = ref('')
const releaseForm = reactive({ registryId: '', version: '' })
const releaseVersionSuggestion = reactive<DeploymentVersionSuggestionView>({ version: '', source: '', detail: '', currentVersion: '', usedVersions: [] })
const versionPolicyIssue = computed(() => releaseVersionIssue(releaseForm.version, releaseVersionSuggestion.usedVersions))
const releaseVersionDetecting = ref(false)
const pendingReleaseDeploymentID = ref('')
const deploymentReleases = ref<Record<string, DeploymentReleaseView[]>>({})
const releaseLoadingID = ref('')
const rollbackConfirmID = ref('')
const operationJobs = ref<Record<string, JobView>>({})
const clockNow = ref(Date.now())
const selectedDeploymentID = ref('')
const releaseHistoryPanel = ref<HTMLElement | null>(null)
const deploymentCurrentVersions = ref<Record<string, string>>({})
const settingsError = ref('')
type ToastKind = 'success' | 'info' | 'warning' | 'error'
type ToastMessage = { id: number; kind: ToastKind; message: string }
const toasts = ref<ToastMessage[]>([])
const toastTimers = new Map<number, number>()
let nextToastID = 0

function dismissToast(id: number) {
  const timer = toastTimers.get(id)
  if (timer !== undefined) window.clearTimeout(timer)
  toastTimers.delete(id)
  toasts.value = toasts.value.filter(toast => toast.id !== id)
}

function showToast(message: string, kind: ToastKind = 'success') {
  const id = ++nextToastID
  if (toasts.value.length >= 3) dismissToast(toasts.value[0].id)
  toasts.value.push({ id, kind, message })
  toastTimers.set(id, window.setTimeout(() => dismissToast(id), kind === 'error' ? 10000 : 4500))
}

const pageErrors = [
  { value: errorMessage, title: '本地核心返回错误' },
  { value: deploymentError, title: '部署项目操作失败' },
  { value: serverError, title: '服务器操作失败' },
  { value: workspaceProjectError, title: '项目操作失败' },
  { value: registryError, title: '镜像仓库操作失败' },
  { value: databaseError, title: '数据库操作失败' },
  { value: settingsError, title: '设置操作失败' },
]
for (const item of pageErrors) {
  watch(item.value, message => {
    if (message && !(item.value === databaseError && databaseDialogOpen.value)) showToast(`${item.title}：${message}`, 'error')
  })
}
const aiSettings = ref<AISettingsView>({ deepSeekConfigured: false })
const deepSeekAPIKey = ref('')
const deepSeekKeyVisible = ref(false)
const aiSettingsSaving = ref(false)
const buildProxySettings = ref<BuildProxySettingsView>({ mode: 'disabled', rules: [], configured: false, builderName: '', detail: '构建代理未启用' })
const buildProxyMode = ref<BuildProxyMode>('disabled')
const buildProxyRulesText = ref('')
const buildProxySaving = ref(false)
const commandLineTool = ref<CommandLineToolStatus | null>(null)
const commandLineToolInstalling = ref(false)
const copiedMCPConfig = ref<'codex' | 'json' | 'verify' | ''>('')
const mcpCommandPath = computed(() => commandLineTool.value?.commandPath || '/usr/local/bin/ideploy')
const mcpCodexCommand = computed(() => `codex mcp add ideploy -- ${mcpCommandPath.value} mcp`)
const mcpClientConfig = computed(() => JSON.stringify({
  mcpServers: {
    ideploy: {
      command: mcpCommandPath.value,
      args: ['mcp'],
    },
  },
}, null, 2))
const cacheCleaning = ref(false)
const serverDataClearing = ref(false)
const serverDataConfirming = ref(false)
const cleanupResult = ref<CleanupView | null>(null)
const selectedDeployment = computed(() => deploymentRuntimes.value.find(r => r.deployment.id === selectedDeploymentID.value))
const publishingDeployment = computed(() => deployments.value.find(value => value.id === publishingDeploymentID.value) ?? null)
const releasePreflightChecks = computed(() => publishingDeployment.value ? deploymentPreflightChecks(publishingDeployment.value, releaseForm.registryId) : [])
const releaseBlockingChecks = computed(() => releasePreflightChecks.value.filter(check => check.state === 'blocked'))
const busyServer = (id: string) => Object.values(operationJobs.value).some(j => j.lockKey === `server:${id}` && ['queued','running','canceling'].includes(j.status)) || !!overview.value?.jobs.some(j=> j.lockKey === `server:${id}` && ['queued','running','canceling'].includes(j.status))

const healthyTools = computed(() => overview.value?.tools.filter((tool) => tool.available).length ?? 0)
const activeSectionMeta = computed(() => sectionMeta[activeSection.value])
const healthyRegistries = computed(() => registries.value.filter((registry) => registry.status === 'healthy').length)
const selectedRegistry = computed(() => registries.value.find((registry) => registry.id === selectedRegistryID.value) ?? null)
const selectedDatabaseConnection = computed(() => databaseConnections.value.find((connection) => connection.id === selectedDatabaseID.value) ?? null)
const editingRegistry = computed(() => registries.value.find((registry) => registry.id === editingRegistryID.value) ?? null)
const selectedServer = computed(() => servers.value.find((server) => server.id === selectedServerID.value) ?? null)
const editingServerCredential = computed(() => servers.value.find((server) => server.id === editingServerCredentialID.value) ?? null)
const canTestServerCredential = computed(() => {
  if (serverCredentialForm.authMethod === 'agent') return true
  if (serverCredentialForm.authMethod === 'private_key') return !!serverCredentialForm.privateKeyPath
  return !!serverCredentialForm.password || (editingServerCredential.value?.authMethod === 'password' && !!editingServerCredential.value.credentialConfigured)
})
const selectedWorkspaceProject = computed(() => workspaceProjects.value.find((project) => project.id === selectedWorkspaceProjectID.value) ?? null)
const dockerfileProject = computed(() => workspaceProjects.value.find(project => project.id === dockerfileProjectID.value) ?? null)
const localWorkspaceProjects = computed(() => workspaceProjects.value.filter((project) => project.sourceKind === 'local').length)
const readyWorkspaceProjects = computed(() => workspaceProjects.value.filter((project) => project.status === 'ready').length)
const deploymentProjectCandidates = computed(() => workspaceProjects.value)
const deploymentRuntimes = computed<DeploymentRuntime[]>(() => deployments.value.map((deployment) => deploymentRuntime(deployment)))
const filteredDeployments = computed(() => deploymentRuntimes.value.filter((runtime) => {
  const { deployment } = runtime
  const query = deploymentSearch.value.trim().toLocaleLowerCase('zh-CN')
  return (deploymentProjectFilter.value === 'all' || deployment.projectId === deploymentProjectFilter.value)
    && (deploymentServerFilter.value === 'all' || deployment.serverId === deploymentServerFilter.value)
    && (deploymentStatusFilter.value === 'all' || runtime.status === deploymentStatusFilter.value)
    && (!query || `${deployment.projectName} ${deployment.serverName} ${deployment.imageRepository}`.toLocaleLowerCase('zh-CN').includes(query))
}))
const runningDeployments = computed(() => deploymentRuntimes.value.filter((runtime) => runtime.status === 'running').length)
const abnormalDeployments = computed(() => deploymentRuntimes.value.filter((runtime) => runtime.status === 'degraded').length)
const offlineDeployments = computed(() => deploymentRuntimes.value.filter((runtime) => runtime.status === 'unreachable').length)

const statusLabel: Record<string, string> = {
  queued: '等待中',
  running: '执行中',
  succeeded: '已完成',
  failed: '失败',
  canceling: '取消中',
  canceled: '已取消',
}

const registryKindLabel: Record<RegistryView['kind'], string> = {
  harbor: 'Harbor',
  oci: 'OCI Registry',
}

const databaseDriverLabel: Record<DatabaseDriver, string> = {
  postgres: 'PostgreSQL',
  mysql: 'MySQL / MariaDB',
}

const registryStatusLabel: Record<RegistryView['status'], string> = {
  unknown: '未检测',
  checking: '检测中',
  healthy: '可用',
  auth_required: '需要凭据',
  degraded: '服务异常',
  unreachable: '无法连接',
}

const serverStatusLabel: Record<ServerStatus, string> = {
  unknown: '未检测', checking: '检测中', healthy: '可用', auth_required: '需要认证', degraded: '环境异常', unreachable: '无法连接',
}

const sshAuthLabel: Record<SSHAuthMethod, string> = {
  agent: 'SSH Agent', password: '密码', private_key: '私钥',
}

function isSSHHostKeyIssue(result: ServerConnectionTestView | null) {
  const detail = result?.detail.toLocaleLowerCase('en-US') ?? ''
  return result?.status === 'auth_required' && (detail.includes('主机指纹') || detail.includes('host key') || detail.includes('host identification'))
}

const workspaceProjectStatusLabel: Record<ProjectStatus, string> = {
  ready: '已识别配置',
  needs_configuration: '待配置',
  unavailable: '来源不可用',
  checking: '检查中',
  failed: '检查失败',
}

const deploymentStatusLabel: Record<DeploymentRuntimeStatus, string> = {
  running: '运行正常', degraded: '部分异常', stopped: '已停止', not_deployed: '未部署',
  unreachable: '服务器不可用', needs_configuration: '待绑定工作负载', unknown: '状态待核对',
}

const releaseStatusLabel: Record<DeploymentReleaseView['status'], string> = {
  queued: '等待发布', preparing: '准备源码', building: '构建镜像', pushing: '推送镜像', pulling: '服务器拉取',
  deploying: '更新服务', checking: '健康检查', succeeded: '发布成功', failed: '发布失败', canceled: '已取消',
}

async function refresh() {
  try {
    errorMessage.value = ''
    overview.value = await SystemService.GetOverview() as unknown as Overview
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    loading.value = false
  }
}

async function refreshWithFeedback() {
  if (refreshing.value) return

  refreshing.value = true
  try {
    await Promise.all([
      refresh(),
      new Promise((resolve) => window.setTimeout(resolve, 500)),
    ])
  } finally {
    refreshing.value = false
  }
}

async function loadWorkspaceProjects() {
  workspaceProjectLoading.value = true
  try {
    workspaceProjectError.value = ''
    workspaceProjects.value = await SystemService.ListProjects() as unknown as ProjectView[]
    if (selectedWorkspaceProjectID.value && !workspaceProjects.value.some((project) => project.id === selectedWorkspaceProjectID.value)) {
      selectedWorkspaceProjectID.value = ''
      workspaceProjectPage.value = 'list'
    }
  } catch (error) {
    workspaceProjectError.value = error instanceof Error ? error.message : String(error)
  } finally {
    workspaceProjectLoading.value = false
  }
}

async function loadAISettings() {
  try {
    aiSettings.value = await SystemService.GetAISettings() as unknown as AISettingsView
  } catch (error) {
    settingsError.value = error instanceof Error ? error.message : String(error)
  }
}

async function loadBuildProxySettings() {
  try {
    const settings = await SystemService.GetBuildProxySettings() as unknown as BuildProxySettingsView
    buildProxySettings.value = settings
    buildProxyMode.value = settings.mode
    buildProxyRulesText.value = settings.rules.map(rule => `${rule.upstream}=${rule.mirror}`).join('\n')
  } catch (error) {
    settingsError.value = error instanceof Error ? error.message : String(error)
  }
}

function parseBuildProxyRules(value: string): BuildProxyRule[] {
  return value.split('\n').map(line => line.trim()).filter(line => line && !line.startsWith('#')).map((line, index) => {
    const separator = line.indexOf('=')
    if (separator <= 0 || separator === line.length - 1) throw new Error(`第 ${index + 1} 条代理规则格式无效，请使用 upstream=host/project`)
    return { upstream: line.slice(0, separator).trim(), mirror: line.slice(separator + 1).trim() }
  })
}

async function saveBuildProxySettings() {
  if (buildProxySaving.value) return
  buildProxySaving.value = true
  settingsError.value = ''
  try {
    const rules = parseBuildProxyRules(buildProxyRulesText.value)
    const settings = await SystemService.SaveBuildProxySettings({ mode: buildProxyMode.value, rules }) as unknown as BuildProxySettingsView
    buildProxySettings.value = settings
    buildProxyMode.value = settings.mode
    buildProxyRulesText.value = settings.rules.map(rule => `${rule.upstream}=${rule.mirror}`).join('\n')
    showToast(settings.configured ? '构建代理配置已保存，后续发布将自动使用专用 Builder' : '构建代理已停用')
  } catch (error) {
    settingsError.value = error instanceof Error ? error.message : String(error)
  } finally {
    buildProxySaving.value = false
  }
}

async function loadCommandLineToolStatus() {
  try {
    commandLineTool.value = await SystemService.GetCommandLineToolStatus() as unknown as CommandLineToolStatus
  } catch (error) {
    settingsError.value = error instanceof Error ? error.message : String(error)
  }
}

async function installCommandLineTool() {
  if (commandLineToolInstalling.value) return
  commandLineToolInstalling.value = true
  settingsError.value = ''
  try {
    commandLineTool.value = await SystemService.InstallCommandLineTool() as unknown as CommandLineToolStatus
    showToast('终端命令已安装，可执行 ideploy version 验证。')
  } catch (error) {
    settingsError.value = error instanceof Error ? error.message : String(error)
  } finally {
    commandLineToolInstalling.value = false
  }
}

async function copyMCPConfig(kind: 'codex' | 'json' | 'verify') {
  const content = kind === 'codex'
    ? mcpCodexCommand.value
    : kind === 'json'
      ? mcpClientConfig.value
      : 'codex mcp list'
  try {
    await Clipboard.SetText(content)
    copiedMCPConfig.value = kind
    showToast(kind === 'json' ? '通用 MCP 配置已复制' : '终端命令已复制')
    window.setTimeout(() => {
      if (copiedMCPConfig.value === kind) copiedMCPConfig.value = ''
    }, 1800)
  } catch (error) {
    settingsError.value = error instanceof Error ? error.message : String(error)
  }
}

async function saveDeepSeekAPIKey() {
  const apiKey = deepSeekAPIKey.value.trim()
  if (aiSettingsSaving.value || !apiKey) return
  aiSettingsSaving.value = true
  settingsError.value = ''
  try {
    await SystemService.SaveDeepSeekAPIKey(apiKey)
    aiSettings.value = { deepSeekConfigured: true }
    deepSeekAPIKey.value = ''
    deepSeekKeyVisible.value = false
    showToast('DeepSeek API Key 已保存到系统钥匙串')
  } catch (error) {
    settingsError.value = error instanceof Error ? error.message : String(error)
  } finally {
    aiSettingsSaving.value = false
  }
}

async function clearDeepSeekAPIKey() {
  if (aiSettingsSaving.value || !aiSettings.value.deepSeekConfigured) return
  aiSettingsSaving.value = true
  settingsError.value = ''
  try {
    await SystemService.SaveDeepSeekAPIKey('')
    aiSettings.value = { deepSeekConfigured: false }
    deepSeekAPIKey.value = ''
    deepSeekKeyVisible.value = false
    showToast('DeepSeek API Key 已清除')
  } catch (error) {
    settingsError.value = error instanceof Error ? error.message : String(error)
  } finally {
    aiSettingsSaving.value = false
  }
}

function resetDockerfileGeneration() {
  dockerfileGeneration.content = ''
  dockerfileGeneration.technology = ''
  dockerfileGeneration.source = ''
  dockerfileGeneration.generationMode = ''
  dockerfileGeneration.detail = ''
  dockerfileGeneration.warnings = []
}

async function openDockerfileGenerator(project: ProjectView) {
  if (project.sourceKind !== 'local') return
  dockerfileProjectID.value = project.id
  dockerfileOverwriteConfirmed.value = false
  dockerfileMode.value = 'auto'
  dockerfileGenerationError.value = ''
  resetDockerfileGeneration()
  dockerfileDialogOpen.value = true
  await generateDockerfile('auto')
}

async function generateDockerfile(mode: 'auto' | 'deepseek') {
  if (dockerfileGenerating.value || !dockerfileProjectID.value) return
  dockerfileGenerating.value = true
  dockerfileGenerationError.value = ''
  dockerfileMode.value = mode
  try {
    const result = await SystemService.GenerateDockerfile({ projectId: dockerfileProjectID.value, mode }) as unknown as DockerfileGenerationView
    Object.assign(dockerfileGeneration, result)
  } catch (error) {
    dockerfileGenerationError.value = error instanceof Error ? error.message : String(error)
  } finally {
    dockerfileGenerating.value = false
  }
}

async function saveGeneratedDockerfile() {
  if (dockerfileSaving.value || !dockerfileProjectID.value || !dockerfileGeneration.content.trim()) return
  dockerfileSaving.value = true
  try {
    const updated = await SystemService.SaveGeneratedDockerfile({ projectId: dockerfileProjectID.value, content: dockerfileGeneration.content, overwrite: dockerfileOverwriteConfirmed.value }) as unknown as ProjectView
    upsertWorkspaceProject(updated)
    dockerfileDialogOpen.value = false
    showToast(`Dockerfile 已写入项目（${updated.dockerfilePath}）`)
    const pendingDeployment = deployments.value.find(value => value.id === pendingReleaseDeploymentID.value)
    if (pendingDeployment?.projectId === updated.id) {
      if (!updated.imageRepository) {
        openDeploymentProjectConfig(pendingDeployment, 'image')
      } else {
        pendingReleaseDeploymentID.value = ''
        await openReleaseDialog(pendingDeployment)
      }
    }
  } catch (error) {
    dockerfileGenerationError.value = error instanceof Error ? error.message : String(error)
  } finally {
    dockerfileSaving.value = false
  }
}

function closeDockerfileDialog() {
  if (!dockerfileGenerating.value && !dockerfileSaving.value) {
    dockerfileDialogOpen.value = false
    pendingReleaseDeploymentID.value = ''
  }
}

async function refreshWorkspaceProjects() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    await Promise.all([loadWorkspaceProjects(), new Promise((resolve) => window.setTimeout(resolve, 300))])
  } finally {
    refreshing.value = false
  }
}

function resetWorkspaceProjectForm() {
  Object.assign(workspaceProjectForm, {
    name: '', description: '', sourceKind: 'local', localPath: '', repositoryUrl: '', refKind: 'branch', refValue: 'main',
    contextPath: '.', dockerfilePath: '', composePath: '', imageRepository: '',
  })
}

function openWorkspaceProjectDialog(project?: ProjectView) {
  workspaceProjectError.value = ''
  editingWorkspaceProjectID.value = project?.id ?? ''
  if (project) {
    Object.assign(workspaceProjectForm, {
      name: project.name, description: project.description, sourceKind: project.sourceKind, localPath: project.localPath,
      repositoryUrl: project.repositoryUrl, refKind: project.refKind, refValue: project.refValue, contextPath: project.contextPath,
      dockerfilePath: project.dockerfilePath, composePath: project.composePath, imageRepository: project.imageRepository,
    })
  } else {
    resetWorkspaceProjectForm()
  }
  workspaceProjectDialogOpen.value = true
}

function closeWorkspaceProjectDialog() {
  if (!workspaceProjectSaving.value) {
    workspaceProjectDialogOpen.value = false
    pendingReleaseDeploymentID.value = ''
  }
}

async function chooseWorkspaceProjectDirectory() {
  try {
    const selected = await SystemService.ChooseProjectDirectory()
    if (!selected) return
    workspaceProjectForm.localPath = selected
    if (!workspaceProjectForm.name) {
      workspaceProjectForm.name = selected.split('/').filter(Boolean).pop() ?? ''
    }
  } catch (error) {
    workspaceProjectError.value = error instanceof Error ? error.message : String(error)
  }
}

async function saveWorkspaceProject() {
  if (workspaceProjectSaving.value) return
  workspaceProjectSaving.value = true
  try {
    workspaceProjectError.value = ''
    const saved = editingWorkspaceProjectID.value
      ? await SystemService.UpdateProject(editingWorkspaceProjectID.value, { ...workspaceProjectForm }) as unknown as ProjectView
      : await SystemService.CreateProject({ ...workspaceProjectForm }) as unknown as ProjectView
    upsertWorkspaceProject(saved)
    selectedWorkspaceProjectID.value = saved.id
    workspaceProjectDialogOpen.value = false
    const pendingDeployment = deployments.value.find(value => value.id === pendingReleaseDeploymentID.value)
    if (pendingDeployment?.projectId === saved.id) {
      pendingReleaseDeploymentID.value = ''
      await openReleaseDialog(pendingDeployment)
    }
  } catch (error) {
    workspaceProjectError.value = error instanceof Error ? error.message : String(error)
  } finally {
    workspaceProjectSaving.value = false
  }
}

async function inspectWorkspaceProject(project: ProjectView) {
  if (workspaceProjectInspectingID.value) return
  workspaceProjectInspectingID.value = project.id
  try {
    workspaceProjectError.value = ''
    upsertWorkspaceProject(await SystemService.InspectProject(project.id) as unknown as ProjectView)
  } catch (error) {
    workspaceProjectError.value = error instanceof Error ? error.message : String(error)
  } finally {
    workspaceProjectInspectingID.value = ''
  }
}

function openWorkspaceProject(project: ProjectView) {
  selectedWorkspaceProjectID.value = project.id
  workspaceProjectPage.value = 'detail'
  void checkProjectGitStatus(project)
}

async function checkProjectGitStatus(project: ProjectView, refreshRemote = false) {
  if (projectGitCheckingID.value) return
  projectGitCheckingID.value = project.id
  try {
    projectGitStatuses[project.id] = await SystemService.ProjectGitStatus(project.id, refreshRemote) as unknown as ProjectGitStatusView
  } catch (error) {
    workspaceProjectError.value = friendlyError(error)
  } finally {
    projectGitCheckingID.value = ''
  }
}

function goBackWorkspaceProjectList() {
  workspaceProjectPage.value = 'list'
}

async function deleteWorkspaceProject(project: ProjectView) {
  if (!window.confirm(`从 iDeploy 移除“${project.name}”？这不会删除本地源码或远端仓库。`)) return
  try {
    await SystemService.DeleteProject(project.id)
    workspaceProjects.value = workspaceProjects.value.filter((item) => item.id !== project.id)
    if (selectedWorkspaceProjectID.value === project.id) {
      selectedWorkspaceProjectID.value = ''
      workspaceProjectPage.value = 'list'
    }
  } catch (error) {
    workspaceProjectError.value = error instanceof Error ? error.message : String(error)
  }
}

function upsertWorkspaceProject(updated: ProjectView) {
  const index = workspaceProjects.value.findIndex((project) => project.id === updated.id)
  if (index >= 0) workspaceProjects.value[index] = updated
  else workspaceProjects.value.push(updated)
  workspaceProjects.value.sort((left, right) => left.name.localeCompare(right.name, 'zh-CN'))
}

function projectSourceLabel(project: ProjectView) {
  return project.sourceKind === 'local' ? '本地目录' : 'Git 仓库'
}

function projectSourceValue(project: ProjectView) {
  return project.sourceKind === 'local' ? project.localPath : `${project.repositoryUrl} · ${project.refValue}`
}

function deploymentRuntime(deployment: DeploymentView): DeploymentRuntime {
  if (!deployment.workloadName) {
    return { deployment, status: 'needs_configuration', detail: '请选择明确的远端容器或 Compose 项目', containers: [] }
  }
  const result = deploymentContainers.value[deployment.serverId]
  if (!result) return { deployment, status: 'unknown', detail: '尚未读取远程 Docker 状态', containers: [] }
  const containers = (result.containers ?? []).filter(c => deployment.workloadKind === 'compose' ? c.composeProject === deployment.workloadName && !c.oneOff : c.name === deployment.workloadName)
  if (result.status !== 'healthy') return {deployment,status:'unreachable',detail:result.detail,containers}
  if (result.stale || !result.observedAt || !Number.isFinite(Date.parse(result.observedAt)) || clockNow.value - Date.parse(result.observedAt) > 60000) return {deployment,status:'unknown',detail:'状态已过期，请刷新后操作',containers}
  if (!containers.length) return { deployment, status: 'not_deployed', detail: '未发现绑定的工作负载；此页暂不创建远端容器', containers }
  if (containers.every(c=>c.state === 'running' && c.health !== 'unhealthy' && c.health !== 'starting')) return { deployment, status: 'running', detail: `${containers.length} 个容器运行中 · ${containers.filter(c=>c.health==='healthy').length} 个健康检查通过`, containers }
  if (containers.every(c=>['created','exited'].includes(c.state)) && containers.every(c=>c.state==='created' || deployment.desiredState==='stopped' || /Exited \(0\)/.test(c.status))) return {deployment,status:'stopped',detail:'容器已停止，数据卷与配置保留',containers}
  return { deployment, status: 'degraded', detail: `${containers.filter(c => c.state === 'running').length}/${containers.length} 个容器运行 · 请查看健康与退出信息`, containers }
}

function projectForDeployment(deployment: DeploymentView) {
  return workspaceProjects.value.find(value => value.id === deployment.projectId) ?? null
}

function deploymentPreflightChecks(deployment: DeploymentView, registryID = ''): DeploymentPreflightCheck[] {
  const project = projectForDeployment(deployment)
  const server = servers.value.find(value => value.id === deployment.serverId)
  const runtimeProbe = deploymentContainers.value[deployment.serverId]
  const registry = registryID
    ? registries.value.find(value => value.id === registryID)
    : registries.value.find(value => ['healthy', 'auth_required'].includes(value.status))
  const docker = overview.value?.tools.find(tool => tool.name.toLocaleLowerCase('en-US') === 'docker')
  const checks: DeploymentPreflightCheck[] = []

  checks.push(!project
    ? { id: 'project', label: '项目源码', detail: '项目配置尚未加载', state: 'blocked', action: 'project', actionLabel: '查看项目' }
    : ['unavailable', 'failed'].includes(project.status)
      ? { id: 'project', label: '项目源码', detail: project.lastError || '项目源码当前不可用', state: 'blocked', action: 'project', actionLabel: '检查项目' }
      : { id: 'project', label: '项目源码', detail: project.sourceKind === 'local' ? '本地目录已就绪' : `Git ${project.refKind} · ${project.refValue}`, state: project.status === 'checking' ? 'warning' : 'ready', action: 'project', actionLabel: '查看配置' })

  if (deployment.workloadKind === 'compose') {
    checks.push(project?.composePath
      ? { id: 'build', label: '构建与发布文件', detail: project.composePath, state: 'ready', action: 'project', actionLabel: '查看 Compose' }
      : { id: 'build', label: '构建与发布文件', detail: '未配置 Compose 文件', state: 'blocked', action: 'project', actionLabel: '配置 Compose' })
  } else {
    checks.push(project?.dockerfilePath
      ? { id: 'build', label: 'Dockerfile', detail: project.dockerfilePath, state: 'ready', action: 'dockerfile', actionLabel: '重新生成' }
      : { id: 'build', label: 'Dockerfile', detail: '未配置，可先生成并预览后写入项目', state: 'blocked', action: project?.sourceKind === 'local' ? 'dockerfile' : 'project', actionLabel: project?.sourceKind === 'local' ? '生成并预览' : '配置项目' })
    const imageIssue = project?.imageRepository ? imageRepositoryValidationIssue(project.imageRepository, registry) : ''
    checks.push(project?.imageRepository && !imageIssue
      ? { id: 'image', label: '镜像地址', detail: project.imageRepository, state: 'ready', action: 'image', actionLabel: '修改地址' }
      : { id: 'image', label: '镜像地址', detail: imageIssue || '尚未指定构建后要推送的仓库路径', state: 'blocked', action: 'image', actionLabel: project?.imageRepository ? '自动修正地址' : '自动填写地址' })
  }

  checks.push(deployment.workloadName
    ? { id: 'workload', label: '目标工作负载', detail: `${deployment.workloadKind === 'compose' ? 'Compose' : '容器'} · ${deployment.workloadName}`, state: 'ready', action: 'workload', actionLabel: '修改配置' }
    : { id: 'workload', label: '目标工作负载', detail: '尚未设置容器或 Compose 项目名称', state: 'blocked', action: 'workload', actionLabel: '配置工作负载' })

  checks.push(docker?.available
    ? { id: 'docker', label: '本机 Docker', detail: docker.version || 'Docker daemon 可用', state: 'ready' }
    : { id: 'docker', label: '本机 Docker', detail: docker?.detail || '尚未完成 Docker 环境检测', state: docker ? 'blocked' : 'warning', action: 'docker', actionLabel: '查看本机检测' })

  checks.push(buildProxySettings.value.configured
    ? { id: 'build-proxy', label: '构建镜像代理', detail: buildProxySettings.value.detail, state: buildProxySettings.value.mode === 'preferred' ? 'warning' : 'ready', action: 'settings', actionLabel: '查看代理' }
    : { id: 'build-proxy', label: '构建镜像代理', detail: '未启用，基础镜像将按 Docker 当前配置拉取', state: 'warning', action: 'settings', actionLabel: '配置代理' })

  const serverReady = server?.status === 'healthy' && (!runtimeProbe || runtimeProbe.status === 'healthy')
  checks.push(serverReady
    ? { id: 'server', label: '目标服务器', detail: `${server.name} · SSH 与 Docker 已通过检测`, state: 'ready', action: 'server', actionLabel: '查看服务器' }
    : { id: 'server', label: '目标服务器', detail: runtimeProbe?.detail || server?.lastError || server?.lastMessage || '需要先通过 SSH 与 Docker 检测', state: 'blocked', action: 'server', actionLabel: '去测试连接' })

  checks.push(!registry
    ? { id: 'registry', label: '镜像仓库', detail: '尚未选择镜像仓库', state: 'blocked', action: 'registry', actionLabel: '添加仓库' }
    : !registry.username || !registry.credentialConfigured
      ? { id: 'registry', label: '镜像仓库', detail: `${registry.name} · 服务可达，但未配置镜像推送凭据`, state: 'blocked', action: 'registry', actionLabel: '配置推送凭据' }
      : registry.status !== 'healthy'
        ? { id: 'registry', label: '镜像仓库', detail: registry.lastError || registry.lastMessage || `${registry.name} 尚未通过 Docker 登录测试`, state: 'blocked', action: 'registry', actionLabel: '检查凭据' }
        : { id: 'registry', label: '镜像仓库', detail: `${registry.name} · Docker 登录已通过`, state: 'ready', action: 'registry', actionLabel: '查看仓库' })

  if (deployment.workloadKind === 'compose') {
    checks.push({ id: 'ports', label: '端口映射', detail: '由 Compose 文件统一管理', state: 'ready' })
  } else if (!deployment.containerPorts.length) {
    checks.push({ id: 'ports', label: '端口映射', detail: '未公开主机端口', state: 'ready', action: 'workload', actionLabel: '添加端口' })
  } else if (deployment.containerPorts.some(value => !value.includes(':'))) {
    checks.push({ id: 'ports', label: '端口映射', detail: `${deployment.containerPorts.join(', ')} · 发布时将根据 Dockerfile EXPOSE 自动匹配`, state: 'warning', action: 'workload', actionLabel: '核对端口' })
  } else {
    checks.push({ id: 'ports', label: '端口映射', detail: deployment.containerPorts.join(', '), state: 'ready', action: 'workload', actionLabel: '修改端口' })
  }
  return checks
}

function deploymentPublishIssue(deployment: DeploymentView) {
  return deploymentPreflightChecks(deployment).find(check => check.state === 'blocked')?.detail ?? ''
}

function deploymentReadiness(deployment: DeploymentView) {
  const checks = deploymentPreflightChecks(deployment)
  const blocked = checks.filter(check => check.state === 'blocked').length
  const warnings = checks.filter(check => check.state === 'warning').length
  if (blocked) return { state: 'blocked', title: `发布准备未完成 · ${blocked} 项待处理`, detail: checks.find(check => check.state === 'blocked')?.detail ?? '' }
  if (warnings) return { state: 'warning', title: `发布前需核对 · ${warnings} 项`, detail: checks.find(check => check.state === 'warning')?.detail ?? '' }
  return { state: 'ready', title: '发布准备已完成', detail: '点击“发布版本”确认版本与镜像仓库' }
}

function suggestedImageRepository(project: ProjectView) {
  const registry = registries.value.find(value => ['healthy', 'auth_required'].includes(value.status))
  if (!registry) return ''
  const host = registry.endpoint.replace(/^https?:\/\//, '').replace(/\/+$/, '')
  const slug = project.name.toLocaleLowerCase('en-US').replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'app'
  return registry.kind === 'harbor' ? `${host}/library/${slug}` : `${host}/${slug}`
}

function imageRepositoryValidationIssue(repository: string, selectedRegistry?: RegistryView) {
  const value = repository.trim().replace(/^https?:\/\//, '').replace(/^\/+|\/+$/g, '')
  if (!value) return '镜像地址不能为空'
  if (value.includes('@') || value.slice(value.indexOf('/') + 1).includes(':')) return '镜像地址只填写仓库路径，不要包含 Tag 或 Digest'
  const parts = value.split('/').filter(Boolean)
  const explicitHost = parts.length > 1 && (parts[0].includes('.') || parts[0].includes(':') || parts[0] === 'localhost')
  const registry = selectedRegistry ?? (explicitHost ? registries.value.find(item => item.endpoint.replace(/^https?:\/\//, '').replace(/\/+$/, '').toLocaleLowerCase('en-US') === parts[0].toLocaleLowerCase('en-US')) : undefined)
  if (registry?.kind === 'harbor') {
    const host = registry.endpoint.replace(/^https?:\/\//, '').replace(/\/+$/, '')
    if (!explicitHost || parts[0].toLocaleLowerCase('en-US') !== host.toLocaleLowerCase('en-US')) return `Harbor 镜像地址必须以 ${host}/ 开头`
    if (parts.length < 3) return `Harbor 镜像地址缺少项目空间，例如 ${host}/library/${parts.at(-1) || 'app'}`
  }
  return ''
}

function openDeploymentProjectConfig(deployment: DeploymentView, action: 'dockerfile' | 'image') {
  const project = projectForDeployment(deployment)
  if (!project) {
    showToast('项目配置尚未加载，请刷新后重试。', 'warning')
    return
  }
  if (action === 'dockerfile') {
    if (project.sourceKind !== 'local') {
      showToast('Git 项目需要先拉取到本地后才能自动生成 Dockerfile。', 'warning')
      openWorkspaceProjectDialog(project)
      return
    }
    void openDockerfileGenerator(project)
    return
  }
  openWorkspaceProjectDialog(project)
  const selectedRegistry = registries.value.find(value => ['healthy', 'auth_required'].includes(value.status))
  const currentIssue = workspaceProjectForm.imageRepository ? imageRepositoryValidationIssue(workspaceProjectForm.imageRepository, selectedRegistry) : ''
  if (!workspaceProjectForm.imageRepository || currentIssue) {
    const suggested = suggestedImageRepository(project)
    if (suggested) {
      workspaceProjectForm.imageRepository = suggested
      showToast(`${currentIssue ? '已修正不完整的 Harbor 镜像地址' : '已根据镜像仓库和项目名称生成目标镜像建议'}：${suggested}，请确认后保存。`, 'info')
    } else {
      showToast('请先配置并检测镜像仓库，才能生成目标镜像建议。', 'warning')
    }
  }
}

function handleReleasePreflightAction(check: DeploymentPreflightCheck) {
  const deployment = publishingDeployment.value
  if (!deployment || !check.action) return
  const project = projectForDeployment(deployment)
  releaseDialogOpen.value = false
  releaseFormError.value = ''
  if (['project', 'dockerfile', 'image', 'registry'].includes(check.action) && project) {
    pendingReleaseDeploymentID.value = deployment.id
  }
  switch (check.action) {
    case 'dockerfile':
      openDeploymentProjectConfig(deployment, 'dockerfile')
      break
    case 'image':
      openDeploymentProjectConfig(deployment, 'image')
      break
    case 'project':
      activeSection.value = 'projects'
      if (project) openWorkspaceProjectDialog(project)
      break
    case 'workload':
      openDeploymentDialog(deployment)
      break
    case 'server':
      activeSection.value = 'servers'
      void openServer(deployment.serverId)
      break
    case 'registry':
      activeSection.value = 'registries'
      {
        const selectedRegistry = registries.value.find(value => value.id === releaseForm.registryId)
        if (selectedRegistry) openRegistryDialog(selectedRegistry)
      }
      break
    case 'docker':
      activeSection.value = 'overview'
      break
    case 'settings':
      activeSection.value = 'settings'
      break
  }
}

async function loadDeployments() {
  deploymentLoading.value = true
  try {
    deploymentError.value = ''
    deployments.value = await SystemService.ListDeployments() as unknown as DeploymentView[]
    await Promise.all(deployments.value.map(async deployment => {
      try {
        const version = await SystemService.DetectDeploymentVersion(deployment.id)
        deploymentCurrentVersions.value[deployment.id] = version.currentVersion || '尚未发布'
      } catch {
        deploymentCurrentVersions.value[deployment.id] = '版本读取失败'
      }
    }))
  } catch (error) {
    deploymentError.value = error instanceof Error ? error.message : String(error)
  } finally {
    deploymentLoading.value = false
  }
}

async function refreshDeploymentRuntimes() {
  const serverIDs = [...new Set(deployments.value.map((deployment) => deployment.serverId))]
  const serversByID = new Map(servers.value.map((server) => [server.id, server]))
  const results = await Promise.all(serverIDs.map(async (serverID) => {
    const server = serversByID.get(serverID)
    if (!server || server.status !== 'healthy') {
      const cached = deploymentContainers.value[serverID]
      return [serverID, {
        ...cached,
        serverId: serverID,
        status: server?.status ?? 'unknown',
        stale: true,
        detail: '服务器尚未连接，已跳过远程 Docker 刷新。',
        containers: cached?.containers ?? [],
      }] as const
    }
    try {
      return [serverID, await SystemService.ListRemoteContainers(serverID) as unknown as RemoteContainersView] as const
    } catch (error) {
      return [serverID, { ...deploymentContainers.value[serverID], serverId: serverID, status: 'degraded' as ServerStatus, stale:true, detail: error instanceof Error ? error.message : String(error), containers: deploymentContainers.value[serverID]?.containers ?? [] }] as const
    }
  }))
  deploymentContainers.value = Object.fromEntries(results)
}

async function refreshDeploymentsWithFeedback() {
  if (deploymentRefreshing.value) return
  deploymentRefreshing.value = true
  try {
    await Promise.all([loadDeployments(), loadWorkspaceProjects(), loadServers(), loadRegistries()])
    await refreshDeploymentRuntimes()
  } finally {
    deploymentRefreshing.value = false
  }
}

function openDeploymentDialog(target?: DeploymentView) {
  deploymentError.value = ''
  deploymentFormError.value = ''
  editingDeploymentID.value = target?.id ?? ''
  deploymentForm.projectId = target?.projectId ?? deploymentProjectCandidates.value[0]?.id ?? ''
  deploymentForm.serverId = target?.serverId ?? servers.value[0]?.id ?? ''
  deploymentForm.workloadKind = target?.workloadKind || 'container'
  deploymentForm.workloadName = target?.workloadName || ''
  deploymentForm.containerPorts = target?.containerPorts ?? []
  deploymentForm.restartPolicy = target?.restartPolicy || 'unless-stopped'
  deploymentPortsText.value = deploymentForm.containerPorts.join('\n')
  deploymentDialogOpen.value = true
}

function closeDeploymentDialog() {
  if (!deploymentSaving.value) deploymentDialogOpen.value = false
}

async function createDeployment() {
  if (deploymentSaving.value) return
  deploymentSaving.value = true
  try {
    deploymentError.value = ''
    deploymentForm.containerPorts = deploymentForm.workloadKind === 'container'
      ? deploymentPortsText.value.split(/[\n,]+/).map(value => value.trim()).filter(Boolean)
      : []
    let savedDeployment: DeploymentView | null = null
    if (editingDeploymentID.value) {
      await SystemService.ConfigureDeployment(editingDeploymentID.value, { ...deploymentForm })
      await loadDeployments()
      savedDeployment = deployments.value.find(value => value.id === editingDeploymentID.value) ?? null
    } else {
      const created = await SystemService.CreateDeployment({ ...deploymentForm }) as unknown as DeploymentView
      upsertDeployment(created)
      savedDeployment = created
    }
    deploymentDialogOpen.value = false
    await refreshDeploymentRuntimes()
    if (!savedDeployment) {
      showToast('部署配置已保存，但暂时无法读取部署目标，请刷新后继续。', 'warning')
      return
    }
    selectedDeploymentID.value = savedDeployment.id
    showToast('部署配置已保存，正在进入发布前检查。')
    await openReleaseDialog(savedDeployment)
  } catch (error) {
    deploymentFormError.value = friendlyError(error)
  } finally {
    deploymentSaving.value = false
  }
}

function generatedReleaseVersion() {
  const now = new Date()
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`
}

async function openReleaseDialog(deployment: DeploymentView) {
  publishingDeploymentID.value = deployment.id
  releaseForm.registryId = registries.value.find(value => value.status === 'healthy' && value.credentialConfigured)?.id
    ?? registries.value.find(value => value.status === 'healthy')?.id
    ?? registries.value.find(value => value.status === 'auth_required')?.id
    ?? registries.value[0]?.id
    ?? ''
  releaseForm.version = ''
  releaseVersionSuggestion.currentVersion = ''
  releaseVersionSuggestion.usedVersions = []
  releaseVersionSuggestion.version = ''
  releaseVersionSuggestion.source = ''
  releaseVersionSuggestion.detail = '正在读取项目版本'
  releaseVersionDetecting.value = true
  releaseFormError.value = ''
  releaseDialogOpen.value = true
  try {
    const suggestion = await SystemService.DetectDeploymentVersion(deployment.id) as unknown as DeploymentVersionSuggestionView
    if (publishingDeploymentID.value !== deployment.id) return
    releaseVersionSuggestion.version = suggestion.version || ''
    releaseVersionSuggestion.source = suggestion.source || ''
    releaseVersionSuggestion.detail = suggestion.detail || ''
    releaseVersionSuggestion.currentVersion = suggestion.currentVersion || ''
    releaseVersionSuggestion.usedVersions = suggestion.usedVersions || []
    if (suggestion.version) releaseForm.version = suggestion.version
  } catch (_error) {
    releaseVersionSuggestion.detail = '版本识别失败，请手动输入'
  } finally {
    releaseVersionDetecting.value = false
  }
}

function closeReleaseDialog() {
  if (!releaseSaving.value) releaseDialogOpen.value = false
}

async function publishDeployment() {
  if (releaseSaving.value || !publishingDeploymentID.value) return
  if (releaseVersionDetecting.value || versionPolicyIssue.value) {
    releaseFormError.value = versionPolicyIssue.value || '正在读取版本信息，请稍候'
    return
  }
  if (releaseBlockingChecks.value.length) {
    releaseFormError.value = `还有 ${releaseBlockingChecks.value.length} 项发布准备未完成，请先按提示处理。`
    return
  }
  releaseSaving.value = true
  try {
    releaseFormError.value = ''
    const deploymentID = publishingDeploymentID.value
    const task = await SystemService.PublishDeployment({ deploymentId: deploymentID, registryId: releaseForm.registryId, version: releaseForm.version }) as unknown as JobView
    releaseDialogOpen.value = false
    operationJobs.value[task.id] = task
    void monitorDeploymentJob(task, deploymentID)
  } catch (error) {
    releaseFormError.value = friendlyError(error)
  } finally {
    releaseSaving.value = false
  }
}

async function monitorDeploymentJob(task: JobView, deploymentID: string) {
  try {
    await trackJob(task, () => loadDeploymentReleases(deploymentID))
  } catch (error) {
    deploymentError.value = friendlyError(error)
  } finally {
    await Promise.all([loadDeploymentReleases(deploymentID), refreshDeploymentsWithFeedback()])
  }
}

async function loadDeploymentReleases(deploymentID: string) {
  if (!deploymentID) return
  releaseLoadingID.value = deploymentID
  try {
    deploymentReleases.value[deploymentID] = await SystemService.ListDeploymentReleases(deploymentID) as unknown as DeploymentReleaseView[]
  } catch (error) {
    deploymentError.value = friendlyError(error)
  } finally {
    if (releaseLoadingID.value === deploymentID) releaseLoadingID.value = ''
  }
}

async function toggleDeploymentDetails(deploymentID: string) {
  if (selectedDeploymentID.value === deploymentID) {
    selectedDeploymentID.value = ''
    rollbackConfirmID.value = ''
    return
  }
  await openReleaseHistory(deploymentID)
}

async function openReleaseHistory(deploymentID: string) {
  selectedDeploymentID.value = deploymentID
  rollbackConfirmID.value = ''
  const loading = loadDeploymentReleases(deploymentID)
  await nextTick()
  releaseHistoryPanel.value?.scrollIntoView({ behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth', block: 'start' })
  releaseHistoryPanel.value?.focus({ preventScroll: true })
  await loading
}

async function rollbackDeployment(release: DeploymentReleaseView) {
  if (rollbackConfirmID.value !== release.id || deploymentActionID.value) {
    rollbackConfirmID.value = release.id
    return
  }
  const deploymentID = release.deploymentId
  deploymentActionID.value = deploymentID
  rollbackConfirmID.value = ''
  try {
    deploymentError.value = ''
    const task = await SystemService.RollbackDeployment(deploymentID, release.id) as unknown as JobView
    operationJobs.value[task.id] = task
    void monitorDeploymentJob(task, deploymentID).finally(() => { deploymentActionID.value = '' })
  } catch (error) {
    deploymentError.value = friendlyError(error)
    deploymentActionID.value = ''
  }
}

function currentReleaseID(deploymentID: string) {
  return deploymentReleases.value[deploymentID]?.find(release => release.status === 'succeeded')?.id ?? ''
}

async function showRollbackHistory() {
  const id = publishingDeploymentID.value
  releaseDialogOpen.value = false
  activeSection.value = 'deployments'
  await openReleaseHistory(id)
}

async function deleteDeployment(deployment: DeploymentView) {
  if (!window.confirm(`移除“${deployment.projectName}”在“${deployment.serverName}”上的部署配置？本地发布记录会一并移除，但不会停止或删除远程容器。`)) return
  try {
    deploymentError.value = ''
    await SystemService.DeleteDeployment(deployment.id)
    deployments.value = deployments.value.filter((item) => item.id !== deployment.id)
  } catch (error) {
    deploymentError.value = error instanceof Error ? error.message : String(error)
  }
}

async function operateDeployment(runtime: DeploymentRuntime, action: 'start' | 'stop' | 'restart') {
  if (!canOperate(runtime) || deploymentActionID.value) return
  if (!window.confirm(`${{start:'启动',stop:'停止',restart:'重启'}[action]}“${runtime.deployment.serverName}”上的 ${runtime.deployment.workloadName}？\n范围：${runtime.containers.map(c=>c.name).join('、')}\n${action === 'start' ? '将启动现有容器。' : '服务可能暂时不可用。'}`)) return
  deploymentActionID.value = runtime.deployment.id
  try {
    deploymentError.value = ''
    await trackJob(await SystemService.OperateDeployment(runtime.deployment.id, action) as unknown as JobView)
  } catch (error) {
    deploymentError.value = error instanceof Error ? error.message : String(error)
  } finally {
    deploymentActionID.value = ''
    await refreshDeploymentsWithFeedback()
  }
}

function canOperate(runtime: DeploymentRuntime) {
  return runtime.containers.length > 0 && ['running','stopped','degraded'].includes(runtime.status) && !busyServer(runtime.deployment.serverId) && !deploymentRefreshing.value
}

function friendlyError(error: unknown) {
  const message = error instanceof Error ? error.message : String(error)
  return message.includes('UNIQUE constraint') ? '该关联或工作负载已经存在，请检查列表后重试。' : message
}

async function clearRuntimeCache() {
  if (cacheCleaning.value || serverDataClearing.value) return
  cacheCleaning.value = true
  settingsError.value = ''
  cleanupResult.value = null
  try {
    const result = await SystemService.ClearRuntimeCache() as unknown as CleanupView
    cleanupResult.value = result
    remoteContainers.value = null
    deploymentContainers.value = {}
    showToast(`缓存已清理：${result.snapshots} 份远程快照，${result.cacheItems} 个文件缓存项。`)
  } catch (error) {
    settingsError.value = friendlyError(error)
  } finally {
    cacheCleaning.value = false
  }
}

async function clearAllServerData() {
  if (serverDataClearing.value || cacheCleaning.value || !serverDataConfirming.value) return
  serverDataClearing.value = true
  settingsError.value = ''
  cleanupResult.value = null
  try {
    const result = await SystemService.ClearServerData() as unknown as CleanupView
    cleanupResult.value = result
    servers.value = []
    deployments.value = []
    remoteContainers.value = null
    deploymentContainers.value = {}
    selectedServerID.value = ''
    selectedDeploymentID.value = ''
    savedConnectionTests.value = {}
    serverHostKeys.value = {}
    operationJobs.value = {}
    serverDataConfirming.value = false
    await refresh()
    showToast(`${result.detail}：${result.servers} 台服务器、${result.deployments} 个部署关联、${result.jobs} 条任务记录。`)
  } catch (error) {
    settingsError.value = friendlyError(error)
  } finally {
    serverDataClearing.value = false
  }
}

async function trackJob(initial: JobView, onPoll?: () => Promise<void>) {
  let current = initial
  operationJobs.value[current.id] = current
  while (['queued','running','canceling'].includes(current.status)) {
    await new Promise(resolve => window.setTimeout(resolve, 1000))
    current = await SystemService.GetJob(current.id) as unknown as JobView
    operationJobs.value[current.id] = current
    applyJobUpdate(current)
    if (onPoll) await onPoll()
  }
  if (current.status !== 'succeeded') throw new Error(current.error || '操作已取消，请刷新确认远端状态。')
  showToast('操作完成，正在核对最新状态', 'info')
}

function upsertDeployment(updated: DeploymentView) {
  const index = deployments.value.findIndex((deployment) => deployment.id === updated.id)
  if (index >= 0) deployments.value[index] = updated
  else deployments.value.unshift(updated)
}

async function loadServers() {
  serverLoading.value = true
  try {
    serverError.value = ''
    servers.value = await SystemService.ListServers() as unknown as ServerView[]
    if (selectedServerID.value && !servers.value.some((server) => server.id === selectedServerID.value)) {
      selectedServerID.value = ''
      remoteContainers.value = null
    }
  } catch (error) {
    serverError.value = friendlyError(error)
  } finally {
    serverLoading.value = false
  }
}

async function refreshServersWithFeedback() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    await Promise.all([loadServers(), new Promise((resolve) => window.setTimeout(resolve, 500))])
    if (selectedServerID.value) await loadRemoteContainers(selectedServerID.value)
  } finally {
    refreshing.value = false
  }
}

function openServerDialog() {
  serverError.value = ''
  Object.assign(serverForm, emptyServerForm())
  Object.assign(secretVisibility, { createPassword: false, createPassphrase: false })
  serverFormTestResult.value = null
  serverFormTestError.value = ''
  serverFormHostKey.value = null
  dialogHostKeyConfirming.value = ''
  serverDialogOpen.value = true
}

function closeServerDialog() {
  if (!serverSaving.value && !serverFormTesting.value) serverDialogOpen.value = false
}

function clearServerFormTest() {
  serverFormTestResult.value = null
  serverFormTestError.value = ''
  serverFormHostKey.value = null
  dialogHostKeyConfirming.value = ''
}

async function testServerFormConnection() {
  if (serverFormTesting.value || serverSaving.value) return
  serverFormTesting.value = true
  serverFormTestResult.value = null
  serverFormTestError.value = ''
  try {
    serverFormTestResult.value = await SystemService.TestServerConnection({
      serverId: '',
      host: serverForm.host,
      port: serverForm.port,
      sshUser: serverForm.sshUser,
      authMethod: serverForm.authMethod,
      password: serverForm.password,
      privateKeyPath: serverForm.privateKeyPath,
      privateKeyPassphrase: serverForm.privateKeyPassphrase,
    }) as unknown as ServerConnectionTestView
  } catch (error) {
    serverFormTestError.value = friendlyError(error)
  } finally {
    serverFormTesting.value = false
  }
}

async function createServer() {
  if (serverSaving.value) return
  serverSaving.value = true
  try {
    const created = await SystemService.CreateServer({ ...serverForm }) as unknown as ServerView
    upsertServer(created)
    serverDialogOpen.value = false
    serverForm.password = ''
    serverForm.privateKeyPassphrase = ''
  } catch (error) {
    serverFormTestError.value = friendlyError(error)
  } finally {
    serverSaving.value = false
  }
}

function openServerCredentialDialog(server: ServerView) {
  serverError.value = ''
  editingServerCredentialID.value = server.id
  Object.assign(serverCredentialForm, {
    name: server.name,
    host: server.host,
    port: server.port,
    sshUser: server.sshUser,
    authMethod: server.authMethod,
    password: '',
    privateKeyPath: server.privateKeyPath || '',
    privateKeyPassphrase: '',
  })
  Object.assign(secretVisibility, { editPassword: false, editPassphrase: false })
  serverCredentialTestResult.value = null
  serverCredentialTestError.value = ''
  serverCredentialHostKey.value = null
  dialogHostKeyConfirming.value = ''
  serverCredentialDialogOpen.value = true
}

function closeServerCredentialDialog() {
  if (!serverCredentialSaving.value && !serverCredentialTesting.value) serverCredentialDialogOpen.value = false
}

function clearServerCredentialTest() {
  serverCredentialTestResult.value = null
  serverCredentialTestError.value = ''
  serverCredentialHostKey.value = null
  dialogHostKeyConfirming.value = ''
}

function dialogSSHHostKeyInput(target: 'create' | 'edit') {
  const form = target === 'create' ? serverForm : serverCredentialForm
  return { host: form.host, port: form.port, sshUser: form.sshUser }
}

async function inspectDialogSSHHostKey(target: 'create' | 'edit') {
  if (dialogHostKeyLoading.value || dialogHostKeyTrusting.value) return
  dialogHostKeyLoading.value = target
  try {
    const value = await SystemService.InspectSSHHostKey(dialogSSHHostKeyInput(target)) as unknown as HostKeyView
    if (target === 'create') serverFormHostKey.value = value
    else serverCredentialHostKey.value = value
  } catch (error) {
    if (target === 'create') serverFormTestError.value = friendlyError(error)
    else serverCredentialTestError.value = friendlyError(error)
  } finally {
    dialogHostKeyLoading.value = ''
  }
}

async function trustDialogSSHHostKey(target: 'create' | 'edit') {
  if (dialogHostKeyLoading.value || dialogHostKeyTrusting.value) return
  const key = target === 'create' ? serverFormHostKey.value : serverCredentialHostKey.value
  if (!key?.fingerprint || key.status !== 'unknown') return
  dialogHostKeyTrusting.value = target
  try {
    const value = await SystemService.TrustSSHHostKey(dialogSSHHostKeyInput(target), key.fingerprint) as unknown as HostKeyView
    if (target === 'create') serverFormHostKey.value = value
    else serverCredentialHostKey.value = value
    dialogHostKeyConfirming.value = ''
    showToast('SSH 主机指纹已信任，请重新点击“测试 SSH”。')
  } catch (error) {
    if (target === 'create') serverFormTestError.value = friendlyError(error)
    else serverCredentialTestError.value = friendlyError(error)
  } finally {
    dialogHostKeyTrusting.value = ''
  }
}

async function testServerCredentialConnection() {
  if (serverCredentialTesting.value || serverCredentialSaving.value || !editingServerCredentialID.value) return
  const current = servers.value.find((server) => server.id === editingServerCredentialID.value)
  if (!current) return
  serverCredentialTesting.value = true
  serverCredentialTestResult.value = null
  serverCredentialTestError.value = ''
  try {
    serverCredentialTestResult.value = await SystemService.TestServerConnection({
      serverId: current.id,
      host: serverCredentialForm.host,
      port: serverCredentialForm.port,
      sshUser: serverCredentialForm.sshUser,
      authMethod: serverCredentialForm.authMethod,
      password: serverCredentialForm.password,
      privateKeyPath: serverCredentialForm.privateKeyPath,
      privateKeyPassphrase: serverCredentialForm.privateKeyPassphrase,
    }) as unknown as ServerConnectionTestView
  } catch (error) {
    serverCredentialTestError.value = friendlyError(error)
  } finally {
    serverCredentialTesting.value = false
  }
}

async function testSavedServerConnection(id: string) {
  if (savedConnectionTestingID.value) return
  savedConnectionTestingID.value = id
  try {
    serverError.value = ''
    savedConnectionTests.value[id] = await SystemService.TestSavedServerConnection(id) as unknown as ServerConnectionTestView
  } catch (error) {
    savedConnectionTests.value[id] = { status: 'degraded', detail: friendlyError(error) }
  } finally {
    savedConnectionTestingID.value = ''
  }
}

async function saveServerCredentials() {
  if (serverCredentialSaving.value || !editingServerCredentialID.value) return
  serverCredentialSaving.value = true
  try {
    const updated = await SystemService.UpdateServer(editingServerCredentialID.value, { ...serverCredentialForm }) as unknown as ServerView
    upsertServer(updated)
    delete savedConnectionTests.value[editingServerCredentialID.value]
    serverCredentialDialogOpen.value = false
    serverCredentialForm.password = ''
    serverCredentialForm.privateKeyPassphrase = ''
    showToast(serverCredentialForm.authMethod === 'agent' ? '服务器连接已更新。' : '服务器连接已更新，认证密钥已安全保存。')
  } catch (error) {
    serverCredentialTestError.value = friendlyError(error)
  } finally {
    serverCredentialSaving.value = false
  }
}

async function chooseSSHPrivateKey(target: 'create' | 'edit') {
  try {
    const path = await SystemService.ChooseSSHPrivateKey()
    if (!path) return
    if (target === 'create') serverForm.privateKeyPath = path
    else serverCredentialForm.privateKeyPath = path
    target === 'create' ? clearServerFormTest() : clearServerCredentialTest()
  } catch (error) {
    if (target === 'create') serverFormTestError.value = friendlyError(error)
    else serverCredentialTestError.value = friendlyError(error)
  }
}

async function testServer(id: string) {
  if (serverTestingID.value) return
  serverTestingID.value = id
  try {
    serverError.value = ''
    await SystemService.TestServer(id)
  } catch (error) {
    serverError.value = error instanceof Error ? error.message : String(error)
  } finally {
    serverTestingID.value = ''
  }
}

async function loadServerHostKey(id: string) {
  if (hostKeyLoadingID.value === id) return
  hostKeyLoadingID.value = id
  try {
    serverError.value = ''
    serverHostKeys.value[id] = await SystemService.InspectServerHostKey(id) as unknown as HostKeyView
  } catch (error) {
    serverError.value = error instanceof Error ? error.message : String(error)
  } finally {
    hostKeyLoadingID.value = ''
  }
}

async function trustServerHostKey(id: string) {
  const hostKey = serverHostKeys.value[id]
  if (!hostKey?.fingerprint || hostKey.status !== 'unknown' || hostKeyTrustingID.value) return
  hostKeyTrustingID.value = id
  try {
    serverError.value = ''
    serverHostKeys.value[id] = await SystemService.TrustServerHostKey(id, hostKey.fingerprint) as unknown as HostKeyView
    hostKeyConfirmingID.value = ''
    showToast('主机指纹已保存，可以重新检测服务器。')
  } catch (error) {
    serverError.value = error instanceof Error ? error.message : String(error)
  } finally {
    hostKeyTrustingID.value = ''
  }
}

async function openServer(id: string) {
  selectedServerID.value = id
  await loadRemoteContainers(id)
}

async function loadRemoteContainers(id = selectedServerID.value) {
  if (!id) return
  const server = servers.value.find((item) => item.id === id)
  if (!server || server.status !== 'healthy') {
    if (selectedServerID.value === id) {
      remoteContainers.value = {
        serverId: id,
        status: server?.status ?? 'unknown',
        detail: server?.status === 'checking' ? '正在检测服务器连接，连接成功后才能读取 Docker。' : '服务器尚未连接，先运行连接检测后再读取 Docker。',
        containers: [],
      }
    }
    return
  }
  containerLoading.value = true
  try {
    const result = await SystemService.ListRemoteContainers(id) as unknown as RemoteContainersView
    if (selectedServerID.value === id) remoteContainers.value = result
  } catch (error) {
    if (selectedServerID.value === id) remoteContainers.value = { serverId: id, status: 'degraded', detail: error instanceof Error ? error.message : String(error), containers: [] }
  } finally {
    if (selectedServerID.value === id) containerLoading.value = false
  }
}

async function operateContainer(container: RemoteContainerView, action: 'start' | 'stop' | 'restart') {
  if (!selectedServerID.value || containerActionKey.value || busyServer(selectedServerID.value)) return
  if (action !== 'start' && !window.confirm(`${action === 'stop' ? '停止' : '重启'}远程容器“${container.name}”？服务可能暂时不可用。`)) return
  const serverID = selectedServerID.value
  const key = `${container.id}:${action}`
  containerActionKey.value = key
  try {
    serverError.value = ''
    await trackJob(await SystemService.OperateRemoteContainer(serverID, container.id, action) as unknown as JobView)
    if (selectedServerID.value === serverID) await loadRemoteContainers(serverID)
  } catch (error) {
    serverError.value = error instanceof Error ? error.message : String(error)
  } finally {
    containerActionKey.value = ''
  }
}

async function deleteServer(server: ServerView) {
  if (!window.confirm(`从 iDeploy 移除“${server.name}”服务器连接？这不会修改远程服务器。`)) return
  try {
    await SystemService.DeleteServer(server.id)
    servers.value = servers.value.filter((item) => item.id !== server.id)
    if (selectedServerID.value === server.id) {
      selectedServerID.value = ''
      remoteContainers.value = null
    }
  } catch (error) {
    serverError.value = error instanceof Error ? error.message : String(error)
  }
}

function upsertServer(updated: ServerView) {
  const index = servers.value.findIndex((server) => server.id === updated.id)
  if (index >= 0) servers.value[index] = updated
  else servers.value.push(updated)
  servers.value.sort((left, right) => left.name.localeCompare(right.name, 'zh-CN'))
  if (selectedServerID.value === updated.id && updated.status !== 'healthy') {
    remoteContainers.value = {
      serverId: updated.id,
      status: updated.status,
      detail: updated.status === 'checking' ? '正在检测服务器连接，连接成功后才能读取 Docker。' : '服务器尚未连接，先运行连接检测后再读取 Docker。',
      containers: [],
    }
  }
}

async function loadRegistries() {
  registryLoading.value = true
  try {
    registryError.value = ''
    registries.value = await SystemService.ListRegistries() as unknown as RegistryView[]
    if (!registries.value.length) {
      selectedRegistryID.value = ''
      selectedProjectName.value = ''
      selectedRepositoryName.value = ''
      projectResult.value = null
      repositoryResult.value = null
      artifactResult.value = null
    } else if (!registries.value.some((registry) => registry.id === selectedRegistryID.value)) {
      selectedRegistryID.value = registries.value[0].id
    }
  } catch (error) {
    registryError.value = error instanceof Error ? error.message : String(error)
  } finally {
    registryLoading.value = false
  }
}

async function refreshRegistriesWithFeedback() {
  if (refreshing.value) return

  refreshing.value = true
  try {
    await Promise.all([
      loadRegistries(),
      new Promise((resolve) => window.setTimeout(resolve, 500)),
    ])
    if (registryPage.value === 'projects' && selectedRegistryID.value) await loadProjects(selectedRegistryID.value)
    if (registryPage.value === 'repositories' && selectedRegistryID.value) await loadRepositories(selectedRegistryID.value, selectedProjectName.value)
    if (registryPage.value === 'artifacts' && selectedRepositoryName.value) await selectRepository(selectedRepositoryName.value, true)
  } finally {
    refreshing.value = false
  }
}

function openRegistryDialog(registry?: RegistryView) {
  registryError.value = ''
  editingRegistryID.value = registry?.id ?? ''
  registryForm.name = registry?.name ?? ''
  registryForm.kind = registry?.kind ?? 'harbor'
  registryForm.endpoint = registry?.endpoint ?? ''
  registryForm.username = registry?.username ?? ''
  registryForm.password = ''
  secretVisibility.registryPassword = false
  registryFormTestError.value = ''
  registryFormProbe.value = null
  registryDialogOpen.value = true
}

function closeRegistryDialog() {
  if (!registrySaving.value && !registryFormTesting.value) registryDialogOpen.value = false
}

function clearRegistryFormProbe() {
  registryFormTestError.value = ''
  registryFormProbe.value = null
}

async function testRegistryForm() {
  if (registryFormTesting.value || registrySaving.value) return

  registryFormTesting.value = true
  registryFormTestError.value = ''
  registryFormProbe.value = null
  try {
    if (editingRegistry.value?.credentialConfigured && !registryForm.password && editingRegistry.value.username === registryForm.username) {
      const task = await SystemService.TestRegistry(editingRegistry.value.id) as unknown as JobView
      operationJobs.value[task.id] = task
      await trackJob(task, loadRegistries)
      await loadRegistries()
      const tested = registries.value.find(value => value.id === editingRegistryID.value)
      if (tested) {
        registryFormProbe.value = {
          endpoint: tested.endpoint,
          status: tested.status === 'checking' || tested.status === 'unknown' ? 'degraded' : tested.status,
          detail: tested.lastError || tested.lastMessage || 'Docker 登录测试已完成',
        }
      }
    } else {
      registryFormProbe.value = await SystemService.TestRegistryConnection({ ...registryForm }) as unknown as RegistryProbeView
    }
  } catch (error) {
    registryFormTestError.value = friendlyError(error)
  } finally {
    registryFormTesting.value = false
  }
}

async function createRegistry() {
  if (registrySaving.value) return

  registrySaving.value = true
  try {
    registryError.value = ''
    const saved = editingRegistryID.value
      ? await SystemService.UpdateRegistry(editingRegistryID.value, { ...registryForm }) as unknown as RegistryView
      : await SystemService.CreateRegistry({ ...registryForm }) as unknown as RegistryView
    upsertRegistry(saved)
    registryDialogOpen.value = false
    registryForm.password = ''
    if (saved.credentialConfigured) {
      try {
        const task = await SystemService.TestRegistry(saved.id) as unknown as JobView
        operationJobs.value[task.id] = task
        await trackJob(task, loadRegistries)
      } catch (error) {
        registryError.value = friendlyError(error)
      }
      await loadRegistries()
    }
    const pendingDeployment = deployments.value.find(value => value.id === pendingReleaseDeploymentID.value)
    if (pendingDeployment) {
      pendingReleaseDeploymentID.value = ''
      await openReleaseDialog(pendingDeployment)
    }
  } catch (error) {
    registryError.value = error instanceof Error ? error.message : String(error)
  } finally {
    registrySaving.value = false
  }
}

async function testRegistry(id: string) {
  if (registryTestingID.value) return

  registryTestingID.value = id
  try {
    registryError.value = ''
    await SystemService.TestRegistry(id)
    await loadRegistries()
  } catch (error) {
    registryError.value = error instanceof Error ? error.message : String(error)
  } finally {
    registryTestingID.value = ''
  }
}

async function deleteRegistry(registry: RegistryView) {
  if (!window.confirm(`从 iDeploy 移除“${registry.name}”连接？这不会删除远端镜像或 Harbor 项目。`)) return

  try {
    registryError.value = ''
    await SystemService.DeleteRegistry(registry.id)
    registries.value = registries.value.filter((item) => item.id !== registry.id)
  } catch (error) {
    registryError.value = error instanceof Error ? error.message : String(error)
  }
}

function upsertRegistry(updated: RegistryView) {
  const index = registries.value.findIndex((item) => item.id === updated.id)
  if (index >= 0) registries.value[index] = updated
  else registries.value.push(updated)
  registries.value.sort((left, right) => left.name.localeCompare(right.name, 'zh-CN'))
}

async function openRegistryContents(id: string) {
  const registry = registries.value.find((item) => item.id === id)
  selectedRegistryID.value = id
  selectedProjectName.value = ''
  selectedRepositoryName.value = ''
  projectResult.value = null
  repositoryResult.value = null
  artifactResult.value = null
  if (registry?.kind === 'harbor') {
    registryPage.value = 'projects'
    await loadProjects(id)
    return
  }
  registryPage.value = 'repositories'
  await loadRepositories(id)
}

async function loadProjects(id = selectedRegistryID.value) {
  if (!id) return
  projectLoading.value = true
  try {
    const result = await SystemService.ListRegistryProjects(id) as unknown as RegistryProjectsView
    if (selectedRegistryID.value === id) projectResult.value = result
  } catch (error) {
    if (selectedRegistryID.value === id) {
      projectResult.value = { registryId: id, status: 'degraded', detail: error instanceof Error ? error.message : String(error), projects: [] }
    }
  } finally {
    if (selectedRegistryID.value === id) projectLoading.value = false
  }
}

async function openProject(name: string) {
  selectedProjectName.value = name
  selectedRepositoryName.value = ''
  artifactResult.value = null
  registryPage.value = 'repositories'
  await loadRepositories(selectedRegistryID.value, name)
}

async function loadRepositories(id = selectedRegistryID.value, projectName = selectedProjectName.value) {
  if (!id) return
  repositoryLoading.value = true
  try {
    const result = await SystemService.ListRegistryRepositories(id, projectName) as unknown as RegistryRepositoriesView
    if (selectedRegistryID.value === id && selectedProjectName.value === projectName) repositoryResult.value = result
  } catch (error) {
    if (selectedRegistryID.value === id && selectedProjectName.value === projectName) {
      repositoryResult.value = { registryId: id, status: 'degraded', detail: error instanceof Error ? error.message : String(error), repositories: [] }
    }
  } finally {
    if (selectedRegistryID.value === id && selectedProjectName.value === projectName) repositoryLoading.value = false
  }
}

async function selectRepository(name: string, force = false) {
  if (!selectedRegistryID.value || (!force && selectedRepositoryName.value === name && artifactResult.value)) return
  const registryID = selectedRegistryID.value
  registryPage.value = 'artifacts'
  selectedRepositoryName.value = name
  artifactLoading.value = true
  artifactResult.value = null
  try {
    const result = await SystemService.ListRepositoryArtifacts(registryID, name) as unknown as RegistryArtifactsView
    if (selectedRegistryID.value === registryID && selectedRepositoryName.value === name) artifactResult.value = result
  } catch (error) {
    if (selectedRegistryID.value === registryID && selectedRepositoryName.value === name) {
      artifactResult.value = { registryId: registryID, repository: name, status: 'degraded', detail: error instanceof Error ? error.message : String(error), artifacts: [] }
    }
  } finally {
    if (selectedRegistryID.value === registryID && selectedRepositoryName.value === name) artifactLoading.value = false
  }
}

function showRegistryConnections() {
  registryPage.value = 'connections'
}

function showRepositories() {
  registryPage.value = 'repositories'
}

function showProjects() {
  registryPage.value = 'projects'
}

function goBackRegistryPage() {
  if (registryPage.value === 'artifacts') {
    showRepositories()
    return
  }
  if (registryPage.value === 'repositories' && selectedRegistry.value?.kind === 'harbor') {
    showProjects()
    return
  }
  showRegistryConnections()
}

function pullCommand(artifact: RegistryArtifactView) {
  const endpoint = selectedRegistry.value?.endpoint.replace(/^https?:\/\//, '') ?? ''
  const image = `${endpoint}/${selectedRepositoryName.value}`
  if (artifact.digest) return `docker pull ${image}@${artifact.digest}`
  if (artifact.tags.length) return `docker pull ${image}:${artifact.tags[0]}`
  return `docker pull ${image}`
}

async function copyPullCommand(artifact: RegistryArtifactView) {
  const key = artifact.digest || artifact.tags.join(',')
  try {
    await Clipboard.SetText(pullCommand(artifact))
    copiedArtifactKey.value = key
    window.setTimeout(() => {
      if (copiedArtifactKey.value === key) copiedArtifactKey.value = ''
    }, 1800)
  } catch (error) {
    registryError.value = error instanceof Error ? error.message : String(error)
  }
}

async function loadDatabaseConnections() {
  databaseLoading.value = true
  try {
    databaseError.value = ''
    databaseConnections.value = await (SystemService as any).ListDatabaseConnections() as DatabaseConnectionView[]
    if (selectedDatabaseID.value && !databaseConnections.value.some(connection => connection.id === selectedDatabaseID.value)) {
      resetDatabaseBrowser()
    }
  } catch (error) {
    databaseError.value = friendlyError(error)
  } finally {
    databaseLoading.value = false
  }
}

function resetDatabaseBrowser() {
  databaseOverviewRequestID++
  selectedDatabaseID.value = ''
  selectedDatabaseSchema.value = ''
  databaseSchemas.value = []
  databaseOverview.value = null
  databaseOverviewLoading.value = false
  databaseOverviewError.value = ''
  databaseTables.value = []
  selectedDatabaseTable.value = null
  databaseColumns.value = []
  databaseIndexes.value = []
  databaseTablePage.value = null
}

function openDatabaseDialog(connection?: DatabaseConnectionView) {
  databaseError.value = ''
  databaseFormTestSuccess.value = ''
  editingDatabaseID.value = connection?.id ?? ''
  Object.assign(databaseForm, connection
    ? { name: connection.name, driver: connection.driver, host: connection.host, port: connection.port, databaseName: connection.databaseName, username: connection.username, password: '', sslMode: connection.sslMode }
    : emptyDatabaseForm())
  databasePasswordVisible.value = false
  databaseDialogOpen.value = true
}

function closeDatabaseDialog() {
  if (!databaseSaving.value && !databaseFormTesting.value) databaseDialogOpen.value = false
}

function updateDatabaseDriverDefaults() {
  const previousDefault = databaseForm.driver === 'postgres' ? 3306 : 5432
  if (!databaseForm.port || databaseForm.port === previousDefault) databaseForm.port = databaseForm.driver === 'postgres' ? 5432 : 3306
  databaseFormTestSuccess.value = ''
}

async function testDatabaseForm() {
  if (databaseFormTesting.value) return
  databaseFormTesting.value = true
  databaseError.value = ''
  databaseFormTestSuccess.value = ''
  try {
    await (SystemService as any).TestDatabaseConnection({ ...databaseForm })
    databaseFormTestSuccess.value = '连接、认证和只读会话已验证。'
  } catch (error) {
    databaseError.value = friendlyError(error)
  } finally {
    databaseFormTesting.value = false
  }
}

async function saveDatabaseConnection() {
  if (databaseSaving.value) return
  databaseSaving.value = true
  databaseError.value = ''
  try {
    const saved = editingDatabaseID.value
      ? await (SystemService as any).UpdateDatabaseConnection(editingDatabaseID.value, { ...databaseForm }) as DatabaseConnectionView
      : await (SystemService as any).CreateDatabaseConnection({ ...databaseForm }) as DatabaseConnectionView
    const index = databaseConnections.value.findIndex(connection => connection.id === saved.id)
    if (index >= 0) databaseConnections.value[index] = saved
    else databaseConnections.value.push(saved)
    databaseConnections.value.sort((left, right) => left.name.localeCompare(right.name, 'zh-CN'))
    databaseDialogOpen.value = false
    if (!editingDatabaseID.value) await selectDatabaseConnection(saved)
    else if (selectedDatabaseID.value === saved.id) await selectDatabaseConnection(saved)
  } catch (error) {
    databaseError.value = friendlyError(error)
  } finally {
    databaseSaving.value = false
  }
}

async function testSavedDatabaseConnection(connection: DatabaseConnectionView) {
  if (savedDatabaseTestingID.value) return
  savedDatabaseTestingID.value = connection.id
  databaseError.value = ''
  try {
    await (SystemService as any).TestSavedDatabaseConnection(connection.id)
    await loadDatabaseConnections()
    showToast(`“${connection.name}”连接成功`)
  } catch (error) {
    databaseError.value = friendlyError(error)
    await loadDatabaseConnections()
  } finally {
    savedDatabaseTestingID.value = ''
  }
}

async function deleteDatabaseConnection(connection: DatabaseConnectionView) {
  if (!window.confirm(`移除数据库连接“${connection.name}”？这不会修改远程数据库。`)) return
  try {
    await (SystemService as any).DeleteDatabaseConnection(connection.id)
    databaseConnections.value = databaseConnections.value.filter(item => item.id !== connection.id)
    if (selectedDatabaseID.value === connection.id) resetDatabaseBrowser()
  } catch (error) {
    databaseError.value = friendlyError(error)
  }
}

async function selectDatabaseConnection(connection: DatabaseConnectionView) {
  databaseOverviewRequestID++
  selectedDatabaseID.value = connection.id
  selectedDatabaseSchema.value = ''
  databaseOverview.value = null
  databaseOverviewLoading.value = false
  databaseOverviewError.value = ''
  databaseTables.value = []
  selectedDatabaseTable.value = null
  databaseColumns.value = []
  databaseIndexes.value = []
  databaseTablePage.value = null
  databaseSchemaLoading.value = true
  databaseError.value = ''
  try {
    databaseSchemas.value = await (SystemService as any).ListDatabaseSchemas(connection.id) as DatabaseSchemaView[]
    const preferred = connection.driver === 'postgres' ? 'public' : connection.databaseName
    const schema = databaseSchemas.value.find(item => item.name === preferred)?.name ?? databaseSchemas.value[0]?.name ?? ''
    if (schema) await selectDatabaseSchema(schema)
    else void loadDatabaseOverview(connection.id, '')
  } catch (error) {
    databaseError.value = friendlyError(error)
  } finally {
    databaseSchemaLoading.value = false
  }
}

async function selectDatabaseSchema(schema: string) {
  if (!selectedDatabaseID.value || databaseTableLoading.value) return
  selectedDatabaseSchema.value = schema
  void loadDatabaseOverview(selectedDatabaseID.value, schema)
  selectedDatabaseTable.value = null
  databaseColumns.value = []
  databaseIndexes.value = []
  databaseTablePage.value = null
  databaseTableLoading.value = true
  try {
    databaseTables.value = await (SystemService as any).ListDatabaseTables(selectedDatabaseID.value, schema) as DatabaseTableView[]
  } catch (error) {
    databaseError.value = friendlyError(error)
  } finally {
    databaseTableLoading.value = false
  }
}

async function loadDatabaseOverview(connectionID: string, schema: string) {
  const requestID = ++databaseOverviewRequestID
  databaseOverview.value = null
  databaseOverviewLoading.value = true
  databaseOverviewError.value = ''
  try {
    const overview = await (SystemService as any).GetDatabaseOverview(connectionID, schema) as DatabaseOverviewView
    if (requestID === databaseOverviewRequestID && selectedDatabaseID.value === connectionID && selectedDatabaseSchema.value === schema) databaseOverview.value = overview
  } catch (error) {
    if (requestID === databaseOverviewRequestID) databaseOverviewError.value = friendlyError(error)
  } finally {
    if (requestID === databaseOverviewRequestID) databaseOverviewLoading.value = false
  }
}

function formatDatabaseSize(value: number | null | undefined) {
  if (value == null) return '—'
  return value === 0 ? '0 B' : formatBytes(value)
}

async function selectDatabaseTable(table: DatabaseTableView) {
  if (!selectedDatabaseID.value) return
  selectedDatabaseTable.value = table
  databaseDetailTab.value = 'columns'
  databaseDetailLoading.value = true
  databaseError.value = ''
  try {
    const [columns, indexes] = await Promise.all([
      (SystemService as any).GetDatabaseTableColumns(selectedDatabaseID.value, table.schema, table.name) as Promise<DatabaseColumnView[]>,
      (SystemService as any).GetDatabaseTableIndexes(selectedDatabaseID.value, table.schema, table.name) as Promise<DatabaseIndexView[]>,
    ])
    databaseColumns.value = columns
    databaseIndexes.value = indexes
    await loadDatabaseTablePage(1)
  } catch (error) {
    databaseError.value = friendlyError(error)
  } finally {
    databaseDetailLoading.value = false
  }
}

async function loadDatabaseTablePage(page: number, sortColumn = databaseTablePage.value?.sortColumn ?? '', sortDesc = databaseTablePage.value?.sortDesc ?? false) {
  const table = selectedDatabaseTable.value
  if (!selectedDatabaseID.value || !table) return
  try {
    databaseTablePage.value = await (SystemService as any).GetDatabaseTableRows(selectedDatabaseID.value, table.schema, table.name, { page, pageSize: 100, sortColumn, sortDesc } as DatabaseTableQuery) as DatabaseTablePageView
  } catch (error) {
    databaseError.value = friendlyError(error)
  }
}

async function toggleDatabaseSort(column: string) {
  const current = databaseTablePage.value
  const sortDesc = current?.sortColumn === column ? !current.sortDesc : false
  await loadDatabaseTablePage(1, column, sortDesc)
}

async function runDiagnostic() {
  actionPending.value = true
  try {
    await SystemService.RunDiagnostic()
    await refresh()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    actionPending.value = false
  }
}

async function cancelJob(id: string) {
  try {
    await SystemService.CancelJob(id)
    await refresh()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  }
}

function applyJobUpdate(updated: JobView) {
  if (operationJobs.value[updated.id]) operationJobs.value[updated.id] = updated
  if (!overview.value) {
    void refresh()
    return
  }
  const index = overview.value.jobs.findIndex((job) => job.id === updated.id)
  const wasActive = index >= 0 && ['queued','running','canceling'].includes(overview.value.jobs[index].status)
  const isActive = ['queued','running','canceling'].includes(updated.status)
  if (index >= 0) {
    overview.value.jobs[index] = updated
  } else {
    overview.value.jobs.unshift(updated)
  }
  overview.value.jobs = overview.value.jobs.slice(0, 20)
  overview.value.activeJobs = Math.max(0, overview.value.activeJobs + Number(isActive) - Number(wasActive))
}

function formatTime(value: string) {
  if (!value || !Number.isFinite(Date.parse(value))) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit',
  }).format(new Date(value))
}

function formatShortTime(value: string) {
  if (!value || !Number.isFinite(Date.parse(value))) return '尚未检测'
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
  }).format(new Date(value))
}

function formatBytes(value: number) {
  if (!value) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  const amount = value / 1024 ** index
  return `${amount >= 10 || index === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[index]}`
}

function shortVersion(value: string) {
  return value.replace(/^git version\s*/i, '').replace(/^OpenSSH_/i, '').split('\n')[0] || '已找到'
}

const eventCleanups: Array<() => void> = []
let refreshTimer: number | undefined
let clockTimer: number | undefined
onMounted(() => {
  void refresh()
  void loadAISettings()
  void loadBuildProxySettings()
  eventCleanups.push(Events.On('job:updated', (event: { data: JobView }) => applyJobUpdate(event.data)))
  eventCleanups.push(Events.On('registry:updated', (event) => upsertRegistry(event.data as RegistryView)))
  eventCleanups.push(Events.On('server:updated', (event) => upsertServer(event.data as ServerView)))
  eventCleanups.push(Events.On('project:updated', (event) => upsertWorkspaceProject(event.data as ProjectView)))
  eventCleanups.push(Events.On('deployment:updated', (event) => upsertDeployment(event.data as DeploymentView)))
  clockTimer = window.setInterval(() => { clockNow.value = Date.now() },1000)
  refreshTimer = window.setInterval(() => {
    if (activeSection.value === 'deployments' && !document.hidden && !deploymentDialogOpen.value && !deploymentActionID.value) void refreshDeploymentsWithFeedback()
  }, 30000)
  document.addEventListener('keydown', modalKeyboard)
})
onUnmounted(() => { eventCleanups.forEach(off=>off()); window.clearInterval(refreshTimer); window.clearInterval(clockTimer); for (const timer of toastTimers.values()) window.clearTimeout(timer); toastTimers.clear(); document.removeEventListener('keydown',modalKeyboard) })

const anyDialog = computed(()=>registryDialogOpen.value || databaseDialogOpen.value || serverDialogOpen.value || serverCredentialDialogOpen.value || workspaceProjectDialogOpen.value || dockerfileDialogOpen.value || deploymentDialogOpen.value || releaseDialogOpen.value)
let focusBeforeDialog: HTMLElement | null = null
watch(anyDialog, async open => {
  if (open) {
    focusBeforeDialog=document.activeElement as HTMLElement
    await nextTick()
    const dialog = document.querySelector<HTMLElement>('.modal-backdrop form')
    dialog?.setAttribute('role','dialog'); dialog?.setAttribute('aria-modal','true')
    dialog?.setAttribute('aria-label',dialog.querySelector('h2')?.textContent || '配置');
    (dialog?.querySelector<HTMLElement>('input:not(:disabled),select:not(:disabled)') ?? dialog?.querySelector<HTMLElement>('button'))?.focus()
  } else focusBeforeDialog?.focus()
})
function modalKeyboard(event: KeyboardEvent) {
  if (!anyDialog.value) return
  if (event.key === 'Escape') { closeRegistryDialog(); closeDatabaseDialog(); closeServerDialog(); closeServerCredentialDialog(); closeWorkspaceProjectDialog(); closeDockerfileDialog(); closeDeploymentDialog(); closeReleaseDialog(); event.preventDefault() }
  if (event.key !== 'Tab') return
  const elements = Array.from(document.querySelectorAll<HTMLElement>('.modal-backdrop button:not(:disabled), .modal-backdrop input:not(:disabled), .modal-backdrop select:not(:disabled), .modal-backdrop textarea:not(:disabled)')).filter(e=>e.getClientRects().length)
  const first=elements[0],last=elements[elements.length-1]
  if (event.shiftKey && document.activeElement===first) { last?.focus(); event.preventDefault() }
  else if (!event.shiftKey && document.activeElement===last) { first?.focus(); event.preventDefault() }
}

watch(activeSection, (section) => {
  if (section === 'projects') void loadWorkspaceProjects().then(() => {
    if (workspaceProjectPage.value === 'detail' && selectedWorkspaceProject.value) void checkProjectGitStatus(selectedWorkspaceProject.value)
  })
  if (section === 'registries') void loadRegistries()
  if (section === 'databases') void loadDatabaseConnections()
  if (section === 'servers') void loadServers()
  if (section === 'deployments') void refreshDeploymentsWithFeedback()
  if (section === 'settings') void Promise.all([refresh(), loadServers(), loadDeployments(), loadAISettings(), loadBuildProxySettings(), loadCommandLineToolStatus()])
})
</script>

<template>
  <div class="app-shell">
    <aside class="sidebar">
      <div class="window-drag-region"></div>
      <div class="brand">
        <div class="brand-mark">iD</div>
        <div>
          <strong>iDeploy</strong>
          <span>本地交付控制台</span>
        </div>
      </div>

      <nav class="navigation" aria-label="主导航">
        <button class="nav-item" :class="{ active: activeSection === 'overview' }" :aria-current="activeSection === 'overview' ? 'page' : undefined" @click="activeSection = 'overview'">
          <svg viewBox="0 0 24 24"><path d="M4 13h6V4H4v9Zm0 7h6v-5H4v5Zm10 0h6v-9h-6v9Zm0-16v5h6V4h-6Z"/></svg>
          概览
        </button>
        <button class="nav-item" :class="{ active: activeSection === 'projects' }" :aria-current="activeSection === 'projects' ? 'page' : undefined" @click="activeSection = 'projects'">
          <svg viewBox="0 0 24 24"><path d="M20 6H4V4h16v2Zm0 2H4a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-8a2 2 0 0 0-2-2Zm-9 8H5v-2h6v2Z"/></svg>
          项目
        </button>
        <button class="nav-item" :class="{ active: activeSection === 'deployments' }" :aria-current="activeSection === 'deployments' ? 'page' : undefined" @click="activeSection = 'deployments'">
          <svg viewBox="0 0 24 24"><path d="m12 2 9 5-9 5-9-5 9-5Zm-7.74 9L12 15.3 19.74 11 21 11.7l-9 5-9-5L4.26 11ZM3 16.4l9 5 9-5-1.26-.7L12 20l-7.74-4.3L3 16.4Z"/></svg>
          部署项目
        </button>
        <button class="nav-item" :class="{ active: activeSection === 'servers' }" :aria-current="activeSection === 'servers' ? 'page' : undefined" @click="activeSection = 'servers'">
          <svg viewBox="0 0 24 24"><path d="M20 8h-3V4H3a2 2 0 0 0-2 2v11h2a3 3 0 0 0 6 0h6a3 3 0 0 0 6 0h2v-5l-3-4ZM6 18.5A1.5 1.5 0 1 1 6 15a1.5 1.5 0 0 1 0 3.5ZM15 15H8.82A3 3 0 0 0 3 15V6h12v9Zm3 3.5a1.5 1.5 0 1 1 0-3.5 1.5 1.5 0 0 1 0 3.5ZM17 12V10h2l1.5 2H17Z"/></svg>
          服务器
        </button>
        <button class="nav-item" :class="{ active: activeSection === 'registries' }" :aria-current="activeSection === 'registries' ? 'page' : undefined" @click="activeSection = 'registries'">
          <svg viewBox="0 0 24 24"><path d="M12 2 2 7l10 5 8.18-4.09L22 7 12 2Zm-8 8.24V17l8 4 8-4v-6.76l-8 4-8-4Z"/></svg>
          镜像仓库
        </button>
        <button class="nav-item" :class="{ active: activeSection === 'databases' }" :aria-current="activeSection === 'databases' ? 'page' : undefined" @click="activeSection = 'databases'">
          <svg viewBox="0 0 24 24"><path d="M12 3C7.03 3 3 4.79 3 7v10c0 2.21 4.03 4 9 4s9-1.79 9-4V7c0-2.21-4.03-4-9-4Zm0 2c4.11 0 7 .99 7 2s-2.89 2-7 2-7-.99-7-2 2.89-2 7-2Zm0 14c-4.11 0-7-.99-7-2v-2.05c1.65 1.03 4.43 1.55 7 1.55s5.35-.52 7-1.55V17c0 1.01-2.89 2-7 2Zm0-5c-4.11 0-7-.99-7-2V9.95c1.65 1.03 4.43 1.55 7 1.55s5.35-.52 7-1.55V12c0 1.01-2.89 2-7 2Z"/></svg>
          数据库
        </button>
        <button class="nav-item settings-nav-item" :class="{ active: activeSection === 'settings' }" :aria-current="activeSection === 'settings' ? 'page' : undefined" @click="activeSection = 'settings'">
          <svg viewBox="0 0 24 24"><path d="M19.14 12.94c.04-.31.06-.63.06-.94s-.02-.63-.07-.94l2.03-1.58a.5.5 0 0 0 .12-.64l-1.92-3.32a.5.5 0 0 0-.61-.22l-2.39.96a7.1 7.1 0 0 0-1.62-.94L14.38 2.8a.49.49 0 0 0-.49-.4h-3.84a.49.49 0 0 0-.49.4L9.2 5.32c-.58.24-1.12.55-1.62.94l-2.39-.96a.48.48 0 0 0-.61.22L2.66 8.84a.49.49 0 0 0 .12.64l2.03 1.58c-.05.31-.09.64-.09.94s.03.63.08.94l-2.03 1.58a.5.5 0 0 0-.12.64l1.92 3.32c.12.22.37.31.61.22l2.39-.96c.5.39 1.05.71 1.62.94l.36 2.52c.04.24.25.4.49.4h3.84c.24 0 .44-.16.49-.4l.36-2.52c.58-.24 1.12-.55 1.62-.94l2.39.96c.24.09.49 0 .61-.22l1.92-3.32a.5.5 0 0 0-.12-.64l-2.01-1.58ZM12 15.5A3.5 3.5 0 1 1 12 8a3.5 3.5 0 0 1 0 7.5Z"/></svg>
          设置
        </button>
      </nav>

      <div class="sidebar-footer">
        <span class="status-dot"></span>
        <div>
          <strong>{{ overview ? '本地核心已连接' : errorMessage ? '连接失败' : '正在连接' }}</strong>
          <span>{{ overview?.version ?? '正在初始化' }}</span>
        </div>
      </div>
    </aside>

    <main class="workspace">
      <header class="topbar">
        <div>
          <p class="eyebrow">{{ activeSectionMeta.eyebrow }}</p>
          <h1>{{ activeSectionMeta.title }}</h1>
          <p class="page-description">{{ activeSection === 'deployments' ? '构建并发布项目到远端 Docker，持续核对运行状态并保留可回滚版本。' : activeSectionMeta.description }}</p>
        </div>
        <div v-if="activeSection === 'overview'" class="topbar-actions">
          <button
            class="icon-button"
            :class="{ refreshing }"
            :disabled="refreshing"
            :title="refreshing ? '正在刷新' : '刷新'"
            :aria-label="refreshing ? '正在刷新' : '刷新'"
            :aria-busy="refreshing"
            @click="refreshWithFeedback"
          >
            <svg class="refresh-icon" viewBox="0 0 24 24"><path d="M17.65 6.35A8 8 0 1 0 20 12h-2a6 6 0 1 1-1.76-4.24L13 11h8V3l-3.35 3.35Z"/></svg>
          </button>
          <button class="primary-button" :disabled="actionPending" @click="runDiagnostic">
            <span class="button-indicator"></span>
            {{ actionPending ? '正在创建任务' : '运行诊断任务' }}
          </button>
        </div>
        <div v-else-if="activeSection === 'registries'" class="topbar-actions">
          <button
            class="icon-button"
            :class="{ refreshing }"
            :disabled="refreshing"
            :title="refreshing ? '正在刷新' : '刷新仓库'"
            :aria-label="refreshing ? '正在刷新' : '刷新仓库'"
            :aria-busy="refreshing"
            @click="refreshRegistriesWithFeedback"
          >
            <svg class="refresh-icon" viewBox="0 0 24 24"><path d="M17.65 6.35A8 8 0 1 0 20 12h-2a6 6 0 1 1-1.76-4.24L13 11h8V3l-3.35 3.35Z"/></svg>
          </button>
          <button v-if="registryPage === 'connections'" class="primary-button" @click="openRegistryDialog()">
            <span class="button-indicator"></span>
            添加仓库
          </button>
        </div>
        <div v-else-if="activeSection === 'databases'" class="topbar-actions">
          <button class="icon-button" :class="{ refreshing: databaseLoading }" :disabled="databaseLoading" title="刷新数据库连接" aria-label="刷新数据库连接" @click="loadDatabaseConnections">
            <svg class="refresh-icon" viewBox="0 0 24 24"><path d="M17.65 6.35A8 8 0 1 0 20 12h-2a6 6 0 1 1-1.76-4.24L13 11h8V3l-3.35 3.35Z"/></svg>
          </button>
          <button class="primary-button" @click="openDatabaseDialog()"><span class="button-indicator"></span>添加数据库</button>
        </div>
        <div v-else-if="activeSection === 'projects'" class="topbar-actions">
          <button class="icon-button" :class="{ refreshing }" :disabled="refreshing" title="刷新项目" aria-label="刷新项目" @click="refreshWorkspaceProjects">
            <svg class="refresh-icon" viewBox="0 0 24 24"><path d="M17.65 6.35A8 8 0 1 0 20 12h-2a6 6 0 1 1-1.76-4.24L13 11h8V3l-3.35 3.35Z"/></svg>
          </button>
          <button v-if="workspaceProjectPage === 'list'" class="primary-button" @click="openWorkspaceProjectDialog()">
            <span class="button-indicator"></span>添加项目
          </button>
        </div>
        <div v-else-if="activeSection === 'deployments'" class="topbar-actions">
          <button class="icon-button" :class="{ refreshing: deploymentRefreshing }" :disabled="deploymentRefreshing" :title="deploymentRefreshing ? '正在刷新' : '刷新部署状态'" :aria-label="deploymentRefreshing ? '正在刷新' : '刷新部署状态'" @click="refreshDeploymentsWithFeedback">
            <svg class="refresh-icon" viewBox="0 0 24 24"><path d="M17.65 6.35A8 8 0 1 0 20 12h-2a6 6 0 1 1-1.76-4.24L13 11h8V3l-3.35 3.35Z"/></svg>
          </button>
          <button class="primary-button" :disabled="!deploymentProjectCandidates.length || !servers.length" @click="openDeploymentDialog()"><span class="button-indicator"></span>新建部署</button>
        </div>
        <div v-else-if="activeSection === 'servers'" class="topbar-actions">
          <button class="icon-button" :class="{ refreshing }" :disabled="refreshing" :title="refreshing ? '正在刷新' : '刷新服务器'" :aria-label="refreshing ? '正在刷新' : '刷新服务器'" :aria-busy="refreshing" @click="refreshServersWithFeedback">
            <svg class="refresh-icon" viewBox="0 0 24 24"><path d="M17.65 6.35A8 8 0 1 0 20 12h-2a6 6 0 1 1-1.76-4.24L13 11h8V3l-3.35 3.35Z"/></svg>
          </button>
          <button class="primary-button" @click="openServerDialog"><span class="button-indicator"></span>添加服务器</button>
        </div>
      </header>

      <template v-if="activeSection === 'overview'">
      <section class="summary-grid" aria-label="系统概况">
        <article class="summary-card accent-card">
          <div class="card-icon">
            <svg viewBox="0 0 24 24"><path d="M12 2 4 5v6c0 5.05 3.41 9.74 8 11 4.59-1.26 8-5.95 8-11V5l-8-3Zm-1 14-4-4 1.41-1.41L11 13.17l4.59-4.58L17 10l-6 6Z"/></svg>
          </div>
          <div><span>本地核心</span><strong>{{ loading ? '检查中' : overview ? '已连接' : '连接失败' }}</strong></div>
          <span v-if="overview" class="state-pill good">已连接</span>
        </article>
        <article class="summary-card">
          <div class="card-icon neutral">
            <svg viewBox="0 0 24 24"><path d="M4 6h16v2H4V6Zm0 5h16v2H4v-2Zm0 5h10v2H4v-2Z"/></svg>
          </div>
          <div><span>活动任务</span><strong>{{ overview?.activeJobs ?? 0 }}</strong></div>
          <span class="card-note">持久化队列</span>
        </article>
        <article class="summary-card">
          <div class="card-icon neutral">
            <svg viewBox="0 0 24 24"><path d="M4 4h16v16H4V4Zm2 2v3h12V6H6Zm0 5v7h5v-7H6Zm7 0v7h5v-7h-5Z"/></svg>
          </div>
          <div><span>开发工具</span><strong>{{ healthyTools }}/{{ overview?.tools.length ?? 3 }}</strong></div>
          <span class="card-note">已就绪</span>
        </article>
      </section>

      <section class="content-grid">
        <article class="panel tools-panel">
          <div class="panel-header">
            <div><span class="section-kicker">LOCAL ENVIRONMENT</span><h2>本机能力</h2></div>
            <span class="platform-badge">{{ overview?.platform ?? 'darwin' }} / {{ overview?.architecture ?? 'arm64' }}</span>
          </div>
          <div class="tool-list">
            <div v-for="tool in overview?.tools ?? []" :key="tool.name" class="tool-row">
              <div class="tool-symbol" :class="{ unavailable: !tool.available }">{{ tool.name.slice(0, 1) }}</div>
              <div class="tool-copy">
                <strong>{{ tool.name }}</strong>
                <span :title="tool.available ? `${shortVersion(tool.version)} · ${tool.detail}` : tool.detail">{{ tool.available ? `${shortVersion(tool.version)} · ${tool.detail}` : tool.detail }}</span>
              </div>
              <span class="check-mark" :class="{ missing: !tool.available }">{{ tool.available ? '✓' : '!' }}</span>
            </div>
            <div v-if="loading" class="empty-state">正在检测 Docker、Git 与 SSH…</div>
          </div>
          <div class="database-path">
            <span>SQLite</span>
            <code>{{ overview?.databasePath ?? '正在初始化本地数据库…' }}</code>
          </div>
        </article>

        <article class="panel jobs-panel">
          <div class="panel-header">
            <div><span class="section-kicker">JOB RUNNER</span><h2>最近任务</h2></div>
            <span v-if="overview?.recoveredJobs" class="recovery-badge">恢复 {{ overview.recoveredJobs }} 个中断任务</span>
          </div>
          <div class="job-list">
            <div v-for="job in overview?.jobs ?? []" :key="job.id" class="job-row">
              <div class="job-state" :class="job.status"><span></span></div>
              <div class="job-main">
                <div class="job-title-row">
                  <strong>{{ ({'system.diagnostic':'系统诊断','server.probe':'服务器检测','registry.test':'仓库检测','server.container.operation':'容器操作','deployment.operation':'工作负载操作','deployment.release':'项目发布','deployment.rollback':'版本回滚'} as Record<string,string>)[job.type] || job.type }}</strong>
                  <span class="job-time">{{ formatTime(job.updatedAt) }}</span>
                </div>
                <span class="job-message">{{ job.error || job.message || statusLabel[job.status] }}</span>
                <div v-if="['queued', 'running', 'canceling'].includes(job.status)" class="progress-track">
                  <span :style="{ width: `${Math.max(job.progress, 4)}%` }"></span>
                </div>
              </div>
              <div class="job-actions">
                <span class="job-status" :class="job.status">{{ statusLabel[job.status] ?? job.status }}</span>
                <button v-if="['queued', 'running'].includes(job.status)" class="text-button" @click="cancelJob(job.id)">取消</button>
              </div>
            </div>
            <div v-if="!loading && !(overview?.jobs.length)" class="empty-state jobs-empty">
              <div class="empty-icon">✓</div>
              <strong>任务队列为空</strong>
              <span>运行一次诊断任务，验证 SQLite 和事件桥。</span>
            </div>
          </div>
        </article>
      </section>

      <footer class="workspace-footer">
        <span>{{ overview?.version }} · 本地交付工作区</span>
        <span>关闭窗口后 iDeploy 将继续在菜单栏运行</span>
      </footer>
      </template>

      <section v-else-if="activeSection === 'deployments'" class="deployment-page" aria-label="部署项目">
        <div class="delivery-flow" aria-label="发布流程">
          <div class="delivery-flow-heading"><strong>标准发布流程</strong><small>先完成发布前检查，提交后按阶段自动执行</small></div>
          <ol>
            <li><b>1</b><span>选择项目</span></li><li><b>2</b><span>构建镜像</span></li><li><b>3</b><span>推送仓库</span></li><li><b>4</b><span>服务器拉取</span></li>
            <li><b>5</b><span>更新服务</span></li><li><b>6</b><span>健康检查</span></li><li><b>7</b><span>发布记录</span></li><li><b>8</b><span>可回滚</span></li>
          </ol>
        </div>
        <div v-if="Object.values(operationJobs).length" class="operation-feedback" aria-live="polite"><div v-for="task in Object.values(operationJobs)" :key="task.id"><span class="job-status" :class="task.status">{{ statusLabel[task.status] }}</span> {{ task.error || task.message }}<progress v-if="['queued','running','canceling'].includes(task.status)" :value="task.progress" max="100" /></div></div>
        <section class="deployment-summary-grid" aria-label="部署项目概况">
          <article class="registry-summary-card"><span>部署目标</span><strong>{{ deployments.length }}</strong><small>本地项目与远程服务器的关联</small></article>
          <article class="registry-summary-card healthy"><span>运行正常</span><strong>{{ runningDeployments }}</strong><small>所有匹配的远程服务均在运行</small></article>
          <article class="registry-summary-card" :class="{ warning: abnormalDeployments || offlineDeployments }"><span>需要关注</span><strong>{{ abnormalDeployments + offlineDeployments }}</strong><small>部分异常或服务器不可用</small></article>
        </section>

        <section class="panel deployment-panel">
          <div class="panel-header">
            <div><span class="section-kicker">PROJECT DELIVERY TARGETS</span><h2>部署项目</h2></div>
            <span class="platform-badge">项目 × 服务器 × Docker</span>
          </div>
          <div class="deployment-filters">
            <select v-model="deploymentProjectFilter" aria-label="筛选项目"><option value="all">全部项目</option><option v-for="project in workspaceProjects" :key="project.id" :value="project.id">{{ project.name }}</option></select>
            <select v-model="deploymentServerFilter" aria-label="筛选服务器"><option value="all">全部服务器</option><option v-for="server in servers" :key="server.id" :value="server.id">{{ server.name }}</option></select>
            <select v-model="deploymentStatusFilter" aria-label="筛选状态"><option value="all">全部状态</option><option value="running">运行正常</option><option value="degraded">部分异常</option><option value="stopped">已停止</option><option value="not_deployed">未部署</option><option value="unreachable">服务器不可用</option></select>
            <input v-model.trim="deploymentSearch" type="search" placeholder="搜索项目、服务器或镜像" aria-label="搜索部署项目">
          </div>

          <div v-if="deploymentLoading" class="registry-empty">正在读取部署目标…</div>
          <div v-else-if="!workspaceProjects.length || !servers.length" class="registry-empty onboarding"><span class="section-kicker">开始持续交付</span><strong>先准备项目与目标服务器</strong><span>项目需要 Dockerfile 或 Compose 配置，服务器需要通过 SSH 与 Docker 环境检测。</span><div class="setup-steps"><button class="secondary-button" @click="activeSection='projects'">1. {{ workspaceProjects.length ? '查看项目' : '添加项目' }}</button><button class="secondary-button" @click="activeSection='servers'">2. {{ servers.length ? '查看服务器' : '连接服务器' }}</button><button class="secondary-button" @click="activeSection='registries'">3. 配置镜像仓库</button></div></div>
          <div v-else-if="!deployments.length" class="registry-empty"><strong>交付基础设施已准备好</strong><span>创建部署目标，配置工作负载名称和运行参数后即可发布第一个版本。</span><button class="secondary-button" @click="openDeploymentDialog()">新建第一个部署</button></div>
          <div v-else-if="!filteredDeployments.length" class="registry-empty"><strong>没有匹配的部署项目</strong><span>请调整筛选条件或搜索关键词。</span><button class="secondary-button" @click="deploymentProjectFilter='all';deploymentServerFilter='all';deploymentStatusFilter='all';deploymentSearch=''">清除筛选</button></div>
          <div v-else class="deployment-list">
            <article v-for="runtime in filteredDeployments" :key="runtime.deployment.id" class="deployment-row" :class="runtime.status">
              <div class="deployment-mark">{{ runtime.deployment.projectName.slice(0, 1).toUpperCase() }}</div>
              <div class="deployment-main">
                <div class="deployment-title"><button class="deployment-title-button" @click="toggleDeploymentDetails(runtime.deployment.id)">{{ runtime.deployment.projectName }}</button><span class="deployment-status" :class="runtime.status">{{ busyServer(runtime.deployment.serverId) ? '操作中' : deploymentStatusLabel[runtime.status] }}</span></div>
                <code>{{ runtime.deployment.workloadKind === 'compose' ? 'Compose' : '容器' }} · {{ runtime.deployment.workloadName || '尚未绑定' }}</code>
                <small>{{ runtime.deployment.serverName }} · {{ runtime.detail }}</small>
                <div class="deployment-version-summary"><span>当前版本</span><strong>{{ deploymentCurrentVersions[runtime.deployment.id] || '读取中…' }}</strong><small>最近成功发布</small></div>
                <div class="deployment-readiness" :class="deploymentReadiness(runtime.deployment).state">
                  <span class="readiness-indicator">{{ deploymentReadiness(runtime.deployment).state === 'ready' ? '✓' : '!' }}</span>
                  <div><strong>{{ deploymentReadiness(runtime.deployment).title }}</strong><small>{{ deploymentReadiness(runtime.deployment).detail }}</small></div>
                  <button class="text-button" @click.stop="openReleaseDialog(runtime.deployment)">{{ deploymentReadiness(runtime.deployment).state === 'ready' ? '查看检查' : '检查并处理' }}</button>
                </div>
                <div v-if="runtime.containers.length" class="deployment-services">
                  <span v-for="container in runtime.containers" :key="container.id" :class="container.state">{{ container.name }} · {{ container.state || 'unknown' }}</span>
                </div>
              </div>
              <div class="deployment-actions">
                <span>采集于 {{ formatShortTime(deploymentContainers[runtime.deployment.serverId]?.observedAt || '') }}</span>
                <button class="primary-button compact-button" :disabled="busyServer(runtime.deployment.serverId)" :title="busyServer(runtime.deployment.serverId) ? '服务器存在未完成任务' : '打开发布前检查'" @click="openReleaseDialog(runtime.deployment)">发布版本</button>
                <button class="secondary-button release-history-entry" :aria-expanded="selectedDeploymentID === runtime.deployment.id" aria-controls="deployment-release-history" @click="openReleaseHistory(runtime.deployment.id)">发布记录 / 回滚 <span aria-hidden="true">↗</span></button>
                <div>
                  <button class="text-button" :disabled="!canOperate(runtime) || runtime.status === 'running' || !!deploymentActionID" @click="operateDeployment(runtime, 'start')">启动</button>
                  <button class="text-button" :disabled="!canOperate(runtime) || runtime.status === 'stopped' || !!deploymentActionID" @click="operateDeployment(runtime, 'restart')">重启</button>
                  <button class="text-button danger-text" :disabled="!canOperate(runtime) || runtime.status === 'stopped' || !!deploymentActionID" @click="operateDeployment(runtime, 'stop')">停止</button>
                </div>
                <div><button class="text-button" :disabled="busyServer(runtime.deployment.serverId)" @click="openDeploymentDialog(runtime.deployment)">部署配置</button><button class="text-button danger-text" :disabled="!!deploymentActionID || busyServer(runtime.deployment.serverId)" @click="deleteDeployment(runtime.deployment)">移除部署</button></div>
              </div>
            </article>
          </div>
        </section>
        <section v-if="selectedDeployment" id="deployment-release-history" ref="releaseHistoryPanel" tabindex="-1" aria-label="发布记录与回滚" class="panel deployment-detail">
          <div class="panel-header"><div><span class="section-kicker">RELEASE HISTORY</span><h2>{{ selectedDeployment.deployment.projectName }} · 发布记录与回滚</h2></div><button class="text-button" @click="selectedDeploymentID=''">收起记录</button></div>
          <div class="detail-body">
            <div class="deployment-detail-grid"><p><span>目标服务器</span><strong>{{ selectedDeployment.deployment.serverName }}</strong></p><p><span>工作负载</span><strong>{{ selectedDeployment.deployment.workloadName }}</strong></p><p><span>发布方式</span><strong>{{ selectedDeployment.deployment.workloadKind === 'compose' ? 'Docker Compose' : '单容器镜像' }}</strong></p><p><span>运行状态</span><strong>{{ deploymentStatusLabel[selectedDeployment.status] }}</strong></p></div>
            <div v-for="c in selectedDeployment.containers" :key="c.id" class="detail-container"><strong>{{ c.name }}</strong><code>{{ c.image }}</code><span>{{ c.status }} · {{ c.health === 'none' ? '未配置健康检查' : c.health }}</span><span>端口：{{ c.ports || '未公开' }}</span></div>
            <div class="release-history-heading"><h3>发布记录</h3><button class="text-button" :disabled="releaseLoadingID === selectedDeployment.deployment.id" @click="loadDeploymentReleases(selectedDeployment.deployment.id)">{{ releaseLoadingID === selectedDeployment.deployment.id ? '刷新中' : '刷新记录' }}</button></div>
            <div v-if="releaseLoadingID === selectedDeployment.deployment.id && !deploymentReleases[selectedDeployment.deployment.id]?.length" class="release-empty">正在读取发布记录…</div>
            <div v-else-if="!deploymentReleases[selectedDeployment.deployment.id]?.length" class="release-empty">还没有发布记录。点击“发布版本”开始第一次交付。</div>
            <div v-else class="release-list">
              <article v-for="release in deploymentReleases[selectedDeployment.deployment.id]" :key="release.id" class="release-row">
                <span class="release-dot" :class="release.status"></span>
                <div class="release-copy">
                  <div class="release-record-heading"><strong>{{ release.version }}</strong><span class="release-status" :class="release.status">{{ release.action === 'rollback' ? '回滚 · ' : '' }}{{ releaseStatusLabel[release.status] }}</span><time>{{ formatTime(release.createdAt) }}</time></div>
                  <code>{{ release.imageRef || '等待生成制品' }}</code>
                  <small v-if="release.imageRef">{{ release.artifactPinned ? '制品已按 Digest 固定' : '旧记录未固定 Digest，无法保证准确回滚' }}</small>
                  <details v-if="release.detail && (release.status === 'failed' || release.detail.length > 120)" class="release-log"><summary>查看执行详情</summary><pre>{{ release.detail }}</pre></details>
                  <small v-else>{{ release.detail || '任务已进入队列' }}</small>
                </div>
                <div class="release-actions"><template v-if="release.status === 'succeeded' && currentReleaseID(selectedDeployment.deployment.id) !== release.id"><button v-if="rollbackConfirmID !== release.id" class="text-button" :disabled="busyServer(selectedDeployment.deployment.serverId)" @click="rollbackDeployment(release)">回滚到此版本</button><div v-else class="inline-confirm"><span>确认回滚？</span><button class="text-button" @click="rollbackConfirmID=''">取消</button><button class="text-button danger-text" @click="rollbackDeployment(release)">确认</button></div></template><span v-else-if="release.status === 'succeeded'" class="current-release-label">当前版本</span></div>
              </article>
            </div>
            <div class="setup-steps"><button class="secondary-button" @click="openReleaseDialog(selectedDeployment.deployment)">发布新版本</button><button class="secondary-button" @click="selectedWorkspaceProjectID=selectedDeployment.deployment.projectId;workspaceProjectPage='detail';activeSection='projects'">查看项目配置</button><button class="secondary-button" @click="activeSection='servers';openServer(selectedDeployment.deployment.serverId)">查看服务器</button></div>
          </div>
        </section>
        <p class="registry-footnote">发布任务在后台持久化执行 · 每个版本保留镜像引用与 Compose 快照 · 健康检查最长等待 90 秒 · 同一服务器同时只执行一个变更任务</p>
      </section>

      <section v-else-if="activeSection === 'servers'" class="server-page" aria-label="开发服务器">
        <section class="server-summary-grid" aria-label="服务器概况">
          <article class="registry-summary-card"><span>已添加服务器</span><strong>{{ servers.length }}</strong><small>本地保存 SSH 连接元数据</small></article>
          <article class="registry-summary-card healthy"><span>可用服务器</span><strong>{{ servers.filter((server) => server.status === 'healthy').length }}</strong><small>SSH 与 Docker 最近检测</small></article>
          <article class="registry-summary-card"><span>认证方式</span><strong>Agent / 密码 / 私钥</strong><small>敏感信息保存在 macOS 系统钥匙串</small></article>
        </section>

        <section class="server-workspace">
          <article class="panel server-list-panel">
            <div class="panel-header"><div><span class="section-kicker">DEVELOPMENT HOSTS</span><h2>服务器列表</h2></div><span class="platform-badge">SSH / Docker</span></div>
            <div v-if="serverLoading" class="registry-empty">正在读取服务器连接…</div>
            <div v-else-if="!servers.length" class="registry-empty"><div class="empty-icon">◇</div><strong>还没有开发服务器</strong><span>添加 SSH 主机后，可检查远程 Docker 环境和容器状态。</span><button class="secondary-button" @click="openServerDialog">添加第一台服务器</button></div>
            <div v-else class="server-list">
              <article v-for="server in servers" :key="server.id" class="server-row" :class="{ selected: selectedServerID === server.id }">
                <button class="server-row-main" :aria-pressed="selectedServerID === server.id" @click="openServer(server.id)"><span class="server-avatar">{{ server.name.slice(0, 1).toUpperCase() }}</span><span class="server-copy"><span class="server-name-line"><strong>{{ server.name }}</strong><span class="auth-chip">{{ sshAuthLabel[server.authMethod] }}</span><span class="registry-status" :class="server.status">{{ serverStatusLabel[server.status] }}</span></span><code>{{ server.sshUser }}@{{ server.host }}:{{ server.port }}</code></span></button>
                <details v-if="server.lastMessage || server.lastError" class="server-diagnostic"><summary>最近检测信息</summary><pre>{{ server.lastMessage || server.lastError }}</pre></details>
                <small v-else class="server-pending">等待连接检测</small>
                <div class="server-row-side"><button class="text-button" :disabled="serverTestingID === server.id || server.status === 'checking'" @click="testServer(server.id)">{{ serverTestingID === server.id || server.status === 'checking' ? '检测中' : '环境检测' }}</button><button class="text-button" @click="openServerCredentialDialog(server)">编辑连接</button><button class="text-button danger-text" @click="deleteServer(server)">移除</button></div>
                <div v-if="server.status === 'auth_required'" class="host-key-card">
                  <div><strong>需要确认 SSH 主机指纹</strong><span v-if="!serverHostKeys[server.id]">应用不会自动跳过主机指纹校验。</span><span v-else>{{ serverHostKeys[server.id].detail }}</span></div>
                  <button v-if="!serverHostKeys[server.id]" class="secondary-button" :disabled="hostKeyLoadingID === server.id" @click="loadServerHostKey(server.id)">{{ hostKeyLoadingID === server.id ? '获取中' : '获取主机指纹' }}</button>
                  <template v-else>
                    <code class="host-key-fingerprint">{{ serverHostKeys[server.id].keyType }} · {{ serverHostKeys[server.id].fingerprint }}</code>
                    <button v-if="serverHostKeys[server.id].status === 'unknown' && hostKeyConfirmingID !== server.id" class="secondary-button" @click="hostKeyConfirmingID = server.id">核对后信任</button>
                    <div v-else-if="serverHostKeys[server.id].status === 'unknown'" class="host-key-confirm-actions"><span>确认指纹完全一致？</span><button class="text-button" @click="hostKeyConfirmingID = ''">取消</button><button class="secondary-button" :disabled="hostKeyTrustingID === server.id" @click="trustServerHostKey(server.id)">{{ hostKeyTrustingID === server.id ? '保存中' : '确认并写入' }}</button></div>
                    <button v-else-if="serverHostKeys[server.id].status === 'trusted'" class="secondary-button" @click="testServer(server.id)">重新检测</button>
                  </template>
                </div>
              </article>
            </div>
          </article>

          <article class="panel remote-panel">
            <div class="panel-header"><div><span class="section-kicker">REMOTE DOCKER</span><h2>{{ selectedServer ? selectedServer.name : '远程容器' }}</h2></div><div v-if="selectedServer" class="server-detail-actions"><button class="text-button" :disabled="savedConnectionTestingID === selectedServer.id" @click="testSavedServerConnection(selectedServer.id)">{{ savedConnectionTestingID === selectedServer.id ? '测试中' : '测试 SSH' }}</button><button v-if="selectedServer.status === 'healthy'" class="text-button" :disabled="containerLoading" @click="loadRemoteContainers()">刷新容器</button></div></div>
            <div v-if="selectedServer" class="connection-profile"><div><span>连接地址</span><strong>{{ selectedServer.host }}:{{ selectedServer.port }}</strong></div><div><span>登录用户</span><strong>{{ selectedServer.sshUser }}</strong></div><div><span>认证方式</span><strong>{{ sshAuthLabel[selectedServer.authMethod] }}</strong></div><button class="text-button" @click="openServerCredentialDialog(selectedServer)">编辑连接</button></div>
            <div v-if="selectedServer && savedConnectionTests[selectedServer.id]" class="form-probe-result server-connection-result" :class="savedConnectionTests[selectedServer.id].status" role="status"><strong>{{ savedConnectionTests[selectedServer.id].status === 'healthy' ? 'SSH 连接成功' : serverStatusLabel[savedConnectionTests[selectedServer.id].status] }}</strong><span>{{ savedConnectionTests[selectedServer.id].detail }}</span></div>
            <div v-if="!selectedServer" class="registry-empty"><div class="empty-icon">→</div><strong>选择一台服务器</strong><span>查看远程 Docker 容器和运行状态。</span></div>
            <div v-else-if="containerLoading" class="registry-empty">正在通过 SSH 读取 Docker 容器…</div>
            <div v-else-if="remoteContainers && remoteContainers.status !== 'healthy'" class="registry-empty"><strong>{{ serverStatusLabel[remoteContainers.status] }}</strong><span>{{ remoteContainers.detail }}</span></div>
            <div v-else-if="remoteContainers && !remoteContainers.containers.length" class="registry-empty"><div class="empty-icon">✓</div><strong>未发现容器</strong><span>远程 Docker 已连接，但当前没有容器。</span></div>
            <div v-else class="remote-container-list">
              <article v-for="container in remoteContainers?.containers ?? []" :key="container.id" class="remote-container-row"><div class="container-main"><strong>{{ container.name }}</strong><code>{{ container.image }}</code><small>{{ container.status }}</small><small v-if="container.ports" class="container-ports">端口：{{ container.ports }}</small></div><div class="container-actions"><span class="container-state" :class="container.state">{{ container.state || 'unknown' }}</span><div class="container-operation-buttons"><button class="text-button" :disabled="!!containerActionKey" @click="operateContainer(container, 'start')">启动</button><button class="text-button" :disabled="!!containerActionKey" @click="operateContainer(container, 'restart')">重启</button><button class="text-button danger-text" :disabled="!!containerActionKey" @click="operateContainer(container, 'stop')">停止</button></div></div></article>
            </div>
          </article>
        </section>
        <p class="registry-footnote">“测试 SSH”会自动信任首次看到的主机指纹并验证登录，但不会执行 Docker；已记录的指纹发生变化时会阻止连接。“环境检测”才会检查远程 Docker。</p>
      </section>

      <section v-else-if="activeSection === 'projects'" class="workspace-project-page" aria-label="项目">
        <template v-if="workspaceProjectPage === 'list'">
          <section class="project-summary-grid" aria-label="项目概况">
            <article class="registry-summary-card"><span>项目总数</span><strong>{{ workspaceProjects.length }}</strong><small>本地持久化的项目配置</small></article>
            <article class="registry-summary-card healthy"><span>已识别配置</span><strong>{{ readyWorkspaceProjects }}</strong><small>发现 Dockerfile，尚未执行构建</small></article>
            <article class="registry-summary-card"><span>本地目录</span><strong>{{ localWorkspaceProjects }}</strong><small>源码不复制进 iDeploy</small></article>
          </section>

          <section class="panel workspace-project-panel">
            <div class="panel-header">
              <div><span class="section-kicker">PROJECT WORKSPACE</span><h2>项目列表</h2></div>
              <span class="platform-badge">本地目录 / Git</span>
            </div>
            <div v-if="workspaceProjectLoading" class="registry-empty">正在读取本地项目…</div>
            <div v-else-if="!workspaceProjects.length" class="registry-empty">
              <div class="empty-icon">◇</div><strong>还没有项目</strong>
              <span>添加本地目录或 Git 仓库，iDeploy 会识别 Docker 构建配置。</span>
              <button class="secondary-button" @click="openWorkspaceProjectDialog()">添加第一个项目</button>
            </div>
            <div v-else class="workspace-project-list">
              <article v-for="project in workspaceProjects" :key="project.id" class="workspace-project-row" :class="project.status">
                <button class="workspace-project-main" @click="openWorkspaceProject(project)">
                  <div class="workspace-project-avatar" :class="project.sourceKind">{{ project.sourceKind === 'local' ? '⌂' : 'G' }}</div>
                  <div class="workspace-project-copy">
                    <div><strong>{{ project.name }}</strong><span class="registry-kind-label">{{ projectSourceLabel(project) }}</span><span class="workspace-project-status" :class="project.status">{{ workspaceProjectStatusLabel[project.status] }}</span><span v-if="projectGitStatuses[project.id]?.available" class="project-git-chip" :class="{ warning: projectGitStatuses[project.id].behind > 0 || projectGitStatuses[project.id].changedFiles > 0 || projectGitStatuses[project.id].untracked > 0 }">{{ projectGitStatuses[project.id].behind ? `待更新 ${projectGitStatuses[project.id].behind}` : projectGitStatuses[project.id].changedFiles || projectGitStatuses[project.id].untracked ? '有未提交代码' : 'Git 正常' }}</span></div>
                    <code>{{ projectSourceValue(project) }}</code>
                    <small>{{ project.dockerfilePath ? `Dockerfile · ${project.dockerfilePath}` : project.lastError || '尚未配置 Dockerfile' }}</small>
                  </div>
                </button>
                <div class="workspace-project-actions">
                  <span>{{ formatShortTime(project.lastCheckedAt) }}</span>
                  <button class="text-button" :disabled="workspaceProjectInspectingID === project.id || project.sourceKind !== 'local'" @click="inspectWorkspaceProject(project)">{{ workspaceProjectInspectingID === project.id ? '检查中' : '检查' }}</button>
                  <button class="text-button" @click="openWorkspaceProjectDialog(project)">编辑</button>
                  <button class="text-button danger-text" @click="deleteWorkspaceProject(project)">移除</button>
                </div>
              </article>
            </div>
          </section>
        </template>

        <section v-else class="panel workspace-project-detail">
          <div class="list-page-header">
            <div>
              <span class="section-kicker">PROJECT CONFIGURATION</span>
              <h2>{{ selectedWorkspaceProject?.name ?? '未选择项目' }}</h2>
              <p>{{ selectedWorkspaceProject ? projectSourceValue(selectedWorkspaceProject) : '从项目列表选择一个项目' }}</p>
            </div>
            <div class="list-page-actions"><button class="text-button back-button" @click="goBackWorkspaceProjectList">← 返回</button><button v-if="selectedWorkspaceProject" class="text-button" @click="openWorkspaceProjectDialog(selectedWorkspaceProject)">编辑配置</button></div>
          </div>
          <div v-if="!selectedWorkspaceProject" class="browser-empty">找不到已选择的项目。</div>
          <div v-else class="workspace-project-detail-grid">
            <section><span class="section-kicker">SOURCE</span><h3>代码来源</h3><dl>
              <div><dt>类型</dt><dd>{{ projectSourceLabel(selectedWorkspaceProject) }}</dd></div>
              <div><dt>{{ selectedWorkspaceProject.sourceKind === 'local' ? '目录' : '仓库' }}</dt><dd><code>{{ selectedWorkspaceProject.sourceKind === 'local' ? selectedWorkspaceProject.localPath : selectedWorkspaceProject.repositoryUrl }}</code></dd></div>
              <div v-if="selectedWorkspaceProject.sourceKind === 'git'"><dt>引用</dt><dd>{{ selectedWorkspaceProject.refKind }} · {{ selectedWorkspaceProject.refValue }}</dd></div>
              <div><dt>最近检查</dt><dd>{{ formatShortTime(selectedWorkspaceProject.lastCheckedAt) }} · {{ workspaceProjectStatusLabel[selectedWorkspaceProject.status] }}</dd></div>
            </dl></section>
            <section><span class="section-kicker">BUILD CONFIG</span><h3>构建配置</h3><dl>
              <div><dt>构建上下文</dt><dd><code>{{ selectedWorkspaceProject.contextPath }}</code></dd></div>
              <div><dt>Dockerfile</dt><dd><code>{{ selectedWorkspaceProject.dockerfilePath || '尚未配置' }}</code></dd></div>
              <div><dt>Compose 文件</dt><dd><code>{{ selectedWorkspaceProject.composePath || '未检测到' }}</code></dd></div>
              <div><dt>目标镜像</dt><dd><code>{{ selectedWorkspaceProject.imageRepository || '将在构建前配置' }}</code></dd></div>
            </dl>
            <button class="secondary-button" :disabled="workspaceProjectInspectingID === selectedWorkspaceProject.id || selectedWorkspaceProject.sourceKind !== 'local'" @click="inspectWorkspaceProject(selectedWorkspaceProject)">{{ workspaceProjectInspectingID === selectedWorkspaceProject.id ? '正在检查…' : '重新检查本地目录' }}</button>
            <button class="secondary-button" :disabled="selectedWorkspaceProject.sourceKind !== 'local'" :title="selectedWorkspaceProject.dockerfilePath ? '重新生成后可在预览中对比、修改再写入' : '生成并预览 Dockerfile'" @click="openDockerfileGenerator(selectedWorkspaceProject)">{{ selectedWorkspaceProject.dockerfilePath ? '重新生成 Dockerfile' : '生成 Dockerfile' }}</button>
            </section>
          </div>
          <section v-if="selectedWorkspaceProject" class="project-git-panel" aria-label="Git 状态">
            <div class="project-git-heading"><div><span class="section-kicker">GIT STATUS</span><h3>代码状态</h3></div><div class="project-git-actions"><button class="secondary-button" :disabled="!!projectGitCheckingID" @click="checkProjectGitStatus(selectedWorkspaceProject)">刷新本地状态</button><button class="secondary-button" :disabled="!!projectGitCheckingID || !projectGitStatuses[selectedWorkspaceProject.id]?.upstream" @click="checkProjectGitStatus(selectedWorkspaceProject, true)">{{ projectGitCheckingID === selectedWorkspaceProject.id ? '检查中…' : '检查远端更新' }}</button></div></div>
            <div v-if="projectGitCheckingID === selectedWorkspaceProject.id && !projectGitStatuses[selectedWorkspaceProject.id]" class="project-git-note">正在读取 Git 工作区…</div>
            <div v-else-if="!projectGitStatuses[selectedWorkspaceProject.id]?.available" class="project-git-note">{{ projectGitStatuses[selectedWorkspaceProject.id]?.detail || '尚未读取 Git 状态' }}</div>
            <template v-else>
              <div class="project-git-metrics"><div><span>分支</span><strong>{{ projectGitStatuses[selectedWorkspaceProject.id].branch }}</strong><small>{{ projectGitStatuses[selectedWorkspaceProject.id].head || '暂无提交' }}</small></div><div><span>未提交改动</span><strong>{{ projectGitStatuses[selectedWorkspaceProject.id].changedFiles + projectGitStatuses[selectedWorkspaceProject.id].untracked }}</strong><small>已暂存 {{ projectGitStatuses[selectedWorkspaceProject.id].stagedFiles }} · 未跟踪 {{ projectGitStatuses[selectedWorkspaceProject.id].untracked }}</small></div><div><span>远端待更新</span><strong>{{ projectGitStatuses[selectedWorkspaceProject.id].upstream ? projectGitStatuses[selectedWorkspaceProject.id].behind : '—' }}</strong><small>本地领先 {{ projectGitStatuses[selectedWorkspaceProject.id].ahead }} 个提交</small></div></div>
              <p class="project-git-note">{{ projectGitStatuses[selectedWorkspaceProject.id].detail || (projectGitStatuses[selectedWorkspaceProject.id].behind ? '远端有新提交，可先检查本地改动，再自行拉取。' : '已检查远端，当前没有待拉取提交。') }}<span v-if="projectGitStatuses[selectedWorkspaceProject.id].upstream"> · 跟踪 {{ projectGitStatuses[selectedWorkspaceProject.id].upstream }}</span> · {{ formatShortTime(projectGitStatuses[selectedWorkspaceProject.id].checkedAt) }}</p>
            </template>
          </section>
        </section>
      </section>

      <section v-else-if="activeSection === 'registries'" class="registry-page" aria-label="镜像仓库">
        <template v-if="registryPage === 'connections'">
        <section class="registry-summary-grid" aria-label="仓库概况">
          <article class="registry-summary-card">
            <span>已连接仓库</span>
            <strong>{{ registries.length }}</strong>
            <small>本地保存连接元数据</small>
          </article>
          <article class="registry-summary-card healthy">
            <span>可用连接</span>
            <strong>{{ healthyRegistries }}</strong>
            <small>最近检测状态</small>
          </article>
          <article class="registry-summary-card">
            <span>凭据边界</span>
            <strong>{{ registries.filter(registry => registry.credentialConfigured).length }} 个凭据</strong>
            <small>密码与 Token 保存在 macOS Keychain</small>
          </article>
        </section>

        <section class="panel registry-panel">
          <div class="panel-header">
            <div>
              <span class="section-kicker">REGISTRY CONNECTIONS</span>
              <h2>连接列表</h2>
            </div>
            <span class="platform-badge">Harbor / OCI</span>
          </div>

          <div v-if="registryLoading" class="empty-state registry-empty">正在读取本地镜像仓库连接…</div>
          <div v-else-if="!registries.length" class="registry-empty">
            <div class="empty-icon">◇</div>
            <strong>还没有镜像仓库</strong>
            <span>添加 Harbor 或标准 OCI Registry，随后可进行连接检测。</span>
            <button class="secondary-button" @click="openRegistryDialog()">添加第一个仓库</button>
          </div>
          <div v-else class="registry-list">
            <article v-for="registry in registries" :key="registry.id" class="registry-row" :class="{ selected: selectedRegistryID === registry.id }">
              <div class="registry-kind" :class="registry.kind">{{ registry.kind === 'harbor' ? 'H' : 'O' }}</div>
              <div class="registry-main">
                <div class="registry-title-row">
                  <strong>{{ registry.name }}</strong>
                  <span class="registry-kind-label">{{ registryKindLabel[registry.kind] }}</span>
                  <span class="registry-status" :class="registry.status">{{ registryStatusLabel[registry.status] }}</span>
                </div>
                <code>{{ registry.endpoint }}</code>
                <span class="registry-detail">{{ registry.lastMessage || registry.lastError || '等待首次连接检测' }}</span>
              </div>
              <div class="registry-meta">
                <span>{{ formatShortTime(registry.lastCheckedAt) }}</span>
                <span>{{ registry.credentialConfigured ? '已关联 Keychain 凭据' : '未配置凭据' }}</span>
              </div>
              <div class="registry-actions">
                <button class="text-button" @click="openRegistryContents(registry.id)">{{ registry.kind === 'harbor' ? '浏览项目' : '浏览仓库' }}</button>
                <button class="text-button" :disabled="registryTestingID === registry.id || registry.status === 'checking'" @click="testRegistry(registry.id)">
                  {{ registryTestingID === registry.id || registry.status === 'checking' ? '检测中' : '检测' }}
                </button>
                <button class="text-button" @click="openRegistryDialog(registry)">编辑</button>
                <button class="text-button danger-text" @click="deleteRegistry(registry)">移除</button>
              </div>
            </article>
          </div>
        </section>
        </template>

        <section v-else-if="registryPage === 'projects'" class="panel registry-list-page" aria-label="Harbor 项目列表">
          <div class="list-page-header">
            <div>
              <span class="section-kicker">HARBOR PROJECTS</span>
              <h2>项目列表</h2>
              <p>{{ selectedRegistry?.name ?? '未选择连接' }}<span v-if="selectedRegistry"> · {{ selectedRegistry.endpoint }}</span></p>
            </div>
            <div class="list-page-actions">
              <button class="text-button back-button" @click="goBackRegistryPage">← 返回</button>
              <button class="text-button" :disabled="projectLoading" @click="loadProjects()">刷新列表</button>
            </div>
          </div>
          <div v-if="!selectedRegistry" class="browser-empty">请选择一个 Harbor 连接。</div>
          <div v-else-if="projectLoading" class="browser-empty">正在读取 {{ selectedRegistry.name }} 的 Harbor 项目…</div>
          <div v-else-if="projectResult && projectResult.status !== 'healthy'" class="browser-empty status-empty" :class="projectResult.status">
            <strong>{{ registryStatusLabel[projectResult.status] }}</strong>
            <span>{{ projectResult.detail }}</span>
          </div>
          <div v-else-if="projectResult && !projectResult.projects.length" class="browser-empty">该 Harbor 连接下暂未发现项目。</div>
          <div v-else class="repository-list standalone">
            <button v-for="project in projectResult?.projects ?? []" :key="project.name" class="repository-row" @click="openProject(project.name)">
              <span class="repository-name">{{ project.name }}</span>
              <span>{{ project.repositoryCount ? `${project.repositoryCount} 个镜像仓库` : '暂无镜像仓库' }}</span>
            </button>
          </div>
        </section>

        <section v-else-if="registryPage === 'repositories'" class="panel registry-list-page" aria-label="镜像仓库列表">
          <div class="list-page-header">
            <div>
              <span class="section-kicker">REPOSITORIES</span>
              <h2>镜像仓库</h2>
              <p>{{ selectedProjectName || selectedRegistry?.name || '未选择连接' }}<span v-if="selectedProjectName && selectedRegistry"> · {{ selectedRegistry.name }}</span></p>
            </div>
            <div class="list-page-actions">
              <button class="text-button back-button" @click="goBackRegistryPage">← 返回</button>
              <button class="text-button" :disabled="repositoryLoading" @click="loadRepositories()">刷新列表</button>
            </div>
          </div>
          <div v-if="!selectedRegistry" class="browser-empty">请选择一个镜像仓库连接。</div>
          <div v-else-if="repositoryLoading" class="browser-empty">正在读取 {{ selectedRegistry.name }} 的镜像仓库…</div>
          <div v-else-if="repositoryResult && repositoryResult.status !== 'healthy'" class="browser-empty status-empty" :class="repositoryResult.status">
            <strong>{{ registryStatusLabel[repositoryResult.status] }}</strong>
            <span>{{ repositoryResult.detail }}</span>
          </div>
          <div v-else-if="repositoryResult && !repositoryResult.repositories.length" class="browser-empty">该连接下暂未发现镜像仓库。</div>
          <div v-else class="repository-list standalone">
            <button v-for="repository in repositoryResult?.repositories ?? []" :key="repository.name" class="repository-row" @click="selectRepository(repository.name)">
              <span class="repository-name">{{ repository.name }}</span>
              <span>{{ repository.artifactCount ? `${repository.artifactCount} 个制品` : '查看 Tag' }}</span>
            </button>
          </div>
        </section>

        <section v-else class="panel registry-list-page" aria-label="镜像制品列表">
          <div class="list-page-header">
            <div>
              <span class="section-kicker">ARTIFACTS & TAGS</span>
              <h2>镜像制品</h2>
              <p>{{ selectedRepositoryName || '未选择镜像仓库' }}</p>
            </div>
            <div class="list-page-actions">
              <button class="text-button back-button" @click="goBackRegistryPage">← 返回</button>
              <button v-if="selectedRepositoryName" class="text-button" :disabled="artifactLoading" @click="selectRepository(selectedRepositoryName, true)">刷新列表</button>
            </div>
          </div>
          <div v-if="!selectedRepositoryName" class="browser-empty">从镜像仓库列表选择一个仓库后查看制品。</div>
          <div v-else-if="artifactLoading" class="browser-empty">正在读取镜像制品…</div>
          <div v-else-if="artifactResult && artifactResult.status !== 'healthy'" class="browser-empty status-empty" :class="artifactResult.status">
            <strong>{{ registryStatusLabel[artifactResult.status] }}</strong>
            <span>{{ artifactResult.detail }}</span>
          </div>
          <div v-else-if="artifactResult && !artifactResult.artifacts.length" class="browser-empty">该镜像仓库暂无可展示的制品。</div>
          <div v-else class="artifact-list standalone">
            <button v-for="artifact in artifactResult?.artifacts ?? []" :key="artifact.digest || artifact.tags.join(',')" class="artifact-copy-row" @click="copyPullCommand(artifact)">
              <div class="artifact-main">
                <div class="tag-list"><span v-for="tag in artifact.tags.length ? artifact.tags : ['untagged']" :key="tag">{{ tag }}</span></div>
                <code>{{ artifact.digest || '标准 OCI Registry 未返回 Digest' }}</code>
                <small>{{ pullCommand(artifact) }}</small>
              </div>
              <div class="artifact-meta">
                <span>{{ formatBytes(artifact.size) }}</span>
                <span>{{ formatShortTime(artifact.createdAt) }}</span>
                <strong>{{ copiedArtifactKey === (artifact.digest || artifact.tags.join(',')) ? '已复制' : '点击复制拉取命令' }}</strong>
              </div>
            </button>
          </div>
        </section>

        <p class="registry-footnote">Harbor 显示项目空间下的制品 Digest 与 Tag；标准 OCI Registry 显示 catalog 与 Tag。制品行会复制可直接执行的 docker pull 命令。</p>
      </section>

      <section v-else-if="activeSection === 'databases'" class="database-page" aria-label="远程数据库">
        <div class="database-overview" aria-label="数据库概况"><span><strong>{{ databaseConnections.length }}</strong> 个已保存连接</span><span class="database-overview-divider"></span><span class="database-readonly-indicator">只读浏览</span><span class="database-overview-note">选择连接和数据库后查看表结构与数据</span></div>
        <section class="database-browser-layout" :class="{ collapsed: databaseConnectionsCollapsed }">
          <article class="panel database-connections-panel" :class="{ collapsed: databaseConnectionsCollapsed }">
            <div class="panel-header"><div class="database-panel-title" :aria-hidden="databaseConnectionsCollapsed"><span class="section-kicker">CONNECTIONS</span><h2>数据库连接</h2></div><button type="button" class="database-collapse-button" :aria-label="databaseConnectionsCollapsed ? '展开数据库连接' : '收起数据库连接'" :title="databaseConnectionsCollapsed ? '展开数据库连接' : '收起数据库连接'" :aria-expanded="!databaseConnectionsCollapsed" @click="databaseConnectionsCollapsed = !databaseConnectionsCollapsed">{{ databaseConnectionsCollapsed ? '›' : '‹' }}</button></div>
            <Transition name="database-content" mode="out-in">
              <div v-if="databaseConnectionsCollapsed" key="collapsed" class="database-collapsed-summary"><span>{{ databaseConnections.length }}</span><strong>数据库连接</strong></div>
              <div v-else key="expanded" class="database-connections-content">
                <div v-if="databaseLoading" class="registry-empty">正在读取数据库连接…</div>
                <div v-else-if="!databaseConnections.length" class="registry-empty"><div class="empty-icon">◫</div><strong>还没有数据库连接</strong><span>添加 PostgreSQL、MySQL 或 MariaDB 连接后，即可只读浏览表结构和数据。</span><button class="secondary-button" @click="openDatabaseDialog()">添加第一个连接</button></div>
                <div v-else class="database-connection-list">
              <article v-for="connection in databaseConnections" :key="connection.id" class="database-connection-row" :class="{ selected: selectedDatabaseID === connection.id }">
                <button class="database-connection-main" :aria-pressed="selectedDatabaseID === connection.id" @click="selectDatabaseConnection(connection)">
                  <span class="registry-kind" :class="connection.driver">{{ connection.driver === 'postgres' ? 'PG' : 'MY' }}</span>
                  <span><strong :title="connection.name">{{ connection.name }}</strong><code :title="`${connection.username}@${connection.host}:${connection.port}${connection.databaseName ? `/${connection.databaseName}` : ''}`">{{ connection.host }}:{{ connection.port }}{{ connection.databaseName ? ` / ${connection.databaseName}` : '' }}</code><small :class="{ error: connection.lastError }" :title="connection.lastError || ''">{{ connection.lastError || (connection.lastConnectedAt ? `已连接 · ${formatShortTime(connection.lastConnectedAt)}` : '尚未测试连接') }}</small></span>
                </button>
                <div class="database-connection-actions"><button class="text-button" :disabled="savedDatabaseTestingID === connection.id" @click="testSavedDatabaseConnection(connection)">{{ savedDatabaseTestingID === connection.id ? '测试中' : '测试连接' }}</button><button class="text-button" @click="openDatabaseDialog(connection)">编辑</button><button class="text-button danger-text" @click="deleteDatabaseConnection(connection)">移除</button></div>
              </article>
                </div>
              </div>
            </Transition>
          </article>
          <article class="panel database-explorer-panel">
            <div class="panel-header"><div><span class="section-kicker">READ-ONLY EXPLORER</span><h2 :title="selectedDatabaseConnection?.name || ''">{{ selectedDatabaseConnection?.name || '数据库资源管理器' }}</h2></div><span v-if="selectedDatabaseConnection" class="platform-badge">{{ databaseDriverLabel[selectedDatabaseConnection.driver] }}</span></div>
            <div v-if="!selectedDatabaseConnection" class="registry-empty"><div class="empty-icon">←</div><strong>选择一个数据库连接</strong><span>iDeploy 会读取可见的 Schema、表、字段、索引和受限分页数据。</span></div>
            <template v-else>
              <div class="database-explorer-toolbar" aria-label="数据库概况">
                <label class="database-schema-select">{{ selectedDatabaseConnection.driver === 'mysql' ? '数据库' : 'Schema' }}<select :value="selectedDatabaseSchema" :disabled="databaseSchemaLoading || databaseTableLoading" @change="selectDatabaseSchema(($event.target as HTMLSelectElement).value)"><option v-for="schema in databaseSchemas" :key="schema.name" :value="schema.name">{{ schema.name }}</option></select></label>
                <span class="database-object-count">{{ databaseSchemaLoading || databaseTableLoading ? '正在读取对象…' : `${databaseTables.length} 个表或视图` }}</span>
                <span class="database-toolbar-divider" aria-hidden="true"></span>
                <span class="database-info-item"><span>版本</span><strong :title="databaseOverview?.version || databaseOverviewError">{{ databaseOverviewLoading ? '读取中…' : (databaseOverview?.version || '—') }}</strong></span>
                <span class="database-info-item"><span>{{ selectedDatabaseConnection.driver === 'mysql' ? '当前库大小' : '数据库大小' }}</span><strong :title="databaseOverview?.sizeDetail || databaseOverviewError">{{ databaseOverviewLoading ? '读取中…' : formatDatabaseSize(databaseOverview?.databaseSizeBytes) }}</strong><span class="database-info-help-wrap"><button type="button" class="database-info-help" aria-label="查看数据库大小说明">?</button><span class="database-help-tooltip" role="tooltip">{{ databaseOverview?.sizeDetail || databaseOverviewError || '数据与索引占用，不是实例磁盘容量。' }}</span></span></span>
                <button type="button" class="database-metrics-refresh" :disabled="databaseOverviewLoading" title="刷新数据库概况" aria-label="刷新数据库概况" @click="loadDatabaseOverview(selectedDatabaseID, selectedDatabaseSchema)">↻</button>
              </div>
              <div class="database-explorer-body">
                <aside class="database-table-tree"><div class="database-tree-heading">表与视图 <span>{{ databaseTables.length }}</span></div><button v-for="table in databaseTables" :key="`${table.schema}.${table.name}`" class="database-table-item" :class="{ selected: selectedDatabaseTable?.schema === table.schema && selectedDatabaseTable?.name === table.name }" :title="table.name" @click="selectDatabaseTable(table)"><span>{{ table.type === 'VIEW' ? '◱' : '▦' }}</span><strong>{{ table.name }}</strong><small>{{ table.type === 'VIEW' ? '视图' : '表' }}</small></button><div v-if="!databaseTableLoading && !databaseTables.length" class="database-tree-empty">当前数据库中没有可读取的表或视图。</div></aside>
                <section class="database-table-detail">
                  <div v-if="!selectedDatabaseTable" class="registry-empty"><strong>选择一张表</strong><span>查看字段定义、索引和分页数据。</span></div>
                  <template v-else>
                    <div class="database-detail-heading"><span>{{ selectedDatabaseTable.type === 'VIEW' ? '视图' : '数据表' }}</span><strong :title="`${selectedDatabaseTable.schema}.${selectedDatabaseTable.name}`">{{ selectedDatabaseTable.name }}</strong></div>
                    <div class="database-detail-tabs"><button :class="{ active: databaseDetailTab === 'columns' }" @click="databaseDetailTab='columns'">字段 {{ databaseColumns.length }}</button><button :class="{ active: databaseDetailTab === 'data' }" @click="databaseDetailTab='data'">数据</button><button :class="{ active: databaseDetailTab === 'indexes' }" @click="databaseDetailTab='indexes'">索引 {{ databaseIndexes.length }}</button></div>
                    <div v-if="databaseDetailLoading" class="registry-empty">正在读取表结构与数据…</div>
                    <div v-else-if="databaseDetailTab === 'columns'" class="database-scroll"><table class="database-metadata-table"><thead><tr><th>字段</th><th>类型</th><th>可空</th><th>默认值</th><th>说明</th></tr></thead><tbody><tr v-for="column in databaseColumns" :key="column.name"><td><code>{{ column.name }}</code><span v-if="column.primaryKey" class="database-key-label">PK</span></td><td>{{ column.dataType }}</td><td>{{ column.nullable ? '是' : '否' }}</td><td><code>{{ column.defaultValue || '—' }}</code></td><td>{{ column.comment || '—' }}</td></tr></tbody></table></div>
                    <div v-else-if="databaseDetailTab === 'indexes'" class="database-index-list"><div v-if="!databaseIndexes.length" class="database-tree-empty">未读取到索引。</div><article v-for="index in databaseIndexes" :key="index.name"><strong>{{ index.name }}</strong><span>{{ index.unique ? '唯一索引' : '普通索引' }}</span><code>{{ index.columns.join(', ') }}</code></article></div>
                    <div v-else class="database-data-pane"><p>每页最多读取 100 行；点击字段名可排序。单元格最大显示 4,096 个字符。</p><div class="database-scroll"><table class="database-data-table"><thead><tr><th v-for="column in databaseTablePage?.columns ?? databaseColumns" :key="column.name"><button @click="toggleDatabaseSort(column.name)">{{ column.name }} <span v-if="databaseTablePage?.sortColumn === column.name">{{ databaseTablePage.sortDesc ? '↓' : '↑' }}</span></button></th></tr></thead><tbody><tr v-for="(row, index) in databaseTablePage?.rows ?? []" :key="index"><td v-for="column in databaseTablePage?.columns ?? databaseColumns" :key="column.name" :title="row[column.name]"><code :class="{ null: row[column.name] === 'NULL' }">{{ row[column.name] }}</code></td></tr><tr v-if="!(databaseTablePage?.rows?.length)"><td :colspan="Math.max(databaseColumns.length, 1)">没有可显示的数据。</td></tr></tbody></table></div><div class="database-pagination"><span>第 {{ databaseTablePage?.page ?? 1 }} 页</span><div><button class="text-button" :disabled="!databaseTablePage || databaseTablePage.page <= 1" @click="loadDatabaseTablePage((databaseTablePage?.page ?? 1) - 1)">上一页</button><button class="text-button" :disabled="!databaseTablePage?.hasNext" @click="loadDatabaseTablePage((databaseTablePage?.page ?? 1) + 1)">下一页</button></div></div></div>
                  </template>
                </section>
              </div>
            </template>
          </article>
        </section>
        <p class="registry-footnote">建议使用只读数据库账号。密码仅保存在 macOS 系统钥匙串。</p>
      </section>

      <section v-else-if="activeSection === 'settings'" class="settings-page" aria-label="设置">
        <section class="settings-summary-grid" aria-label="本地数据概况">
          <article class="registry-summary-card"><span>服务器连接</span><strong>{{ servers.length }}</strong><small>保存在本地数据库中的 SSH 配置</small></article>
          <article class="registry-summary-card"><span>部署关联</span><strong>{{ deployments.length }}</strong><small>项目与服务器之间的运行绑定</small></article>
          <article class="registry-summary-card"><span>活动任务</span><strong>{{ overview?.activeJobs ?? 0 }}</strong><small>有服务器任务执行时禁止清除</small></article>
        </section>

        <section class="panel settings-panel">
          <div class="panel-header"><div><span class="section-kicker">COMMAND LINE</span><h2>终端集成</h2></div><span class="platform-badge">macOS</span></div>
          <div class="settings-list">
            <div class="settings-row">
              <div class="settings-icon">›_</div>
              <div class="settings-copy"><strong>ideploy 命令</strong><span>{{ commandLineTool?.detail || '正在检查终端命令…' }}</span><code :title="commandLineTool?.commandPath">{{ commandLineTool?.commandPath || '/usr/local/bin/ideploy' }}</code><small>命令与桌面端共享项目、服务器、镜像仓库、发布记录和 Keychain 凭据；安装时 macOS 会请求管理员授权。</small></div>
              <button class="secondary-button" :disabled="commandLineToolInstalling || commandLineTool?.installed || commandLineTool?.conflict" @click="installCommandLineTool">{{ commandLineToolInstalling ? '安装中…' : commandLineTool?.installed ? '已安装' : commandLineTool?.conflict ? '路径被占用' : '安装命令' }}</button>
            </div>
          </div>
        </section>

        <section class="panel settings-panel build-proxy-panel">
          <div class="panel-header"><div><span class="section-kicker">BUILDKIT / REGISTRY MIRROR</span><h2>构建镜像代理</h2></div><span class="platform-badge" :class="{ healthy: buildProxySettings.configured }">{{ buildProxySettings.configured ? '已启用' : '未启用' }}</span></div>
          <div class="settings-row build-proxy-row">
            <div class="settings-icon">⇄</div>
            <div class="settings-copy"><strong>iDeploy 专用 BuildKit Builder</strong><span>{{ buildProxySettings.detail }}</span><code v-if="buildProxySettings.builderName">{{ buildProxySettings.builderName }}</code><small>规则只影响 Dockerfile 基础镜像拉取；发布产物仍推送到发布时选择的普通仓库项目。Harbor Proxy Cache 项目只用于拉取，不能作为产物推送目标。</small></div>
            <div class="build-proxy-form">
              <div class="build-proxy-fields">
                <label class="build-proxy-policy"><span>代理策略</span><select v-model="buildProxyMode" :disabled="buildProxySaving"><option value="disabled">停用</option><option value="preferred">优先代理，失败时回退</option><option value="required">强制代理，初始化失败即停止发布</option></select><small>决定代理不可用时是否允许回退到上游仓库。</small></label>
                <label class="build-proxy-mappings"><span>上游映射</span><textarea v-model="buildProxyRulesText" :disabled="buildProxySaving" spellcheck="false" placeholder="docker.io=harbor.example.com/proxy-dockerhub&#10;ghcr.io=harbor.example.com/proxy-ghcr"></textarea><small>每行一条 <code>upstream=host/proxy-project</code>，不要填写 http://、Tag 或 Digest。</small></label>
              </div>
              <div class="build-proxy-actions"><span>保存后，桌面端、CLI 和 MCP 的后续构建统一使用此策略。</span><button class="secondary-button" :disabled="buildProxySaving || (buildProxyMode !== 'disabled' && !buildProxyRulesText.trim())" @click="saveBuildProxySettings">{{ buildProxySaving ? '保存中…' : '保存代理配置' }}</button></div>
            </div>
          </div>
          <div class="mcp-security-note"><strong>MCP 自动继承</strong><span><code>publish_deployment</code> 无需增加参数；桌面端、CLI 和 MCP 的后续发布都会读取这份全局策略。私有代理的认证会复用同主机下已经保存在 iDeploy Keychain 中的 Registry 凭据。</span></div>
        </section>

        <section class="panel settings-panel mcp-settings-panel">
          <div class="panel-header"><div><span class="section-kicker">AI / MODEL CONTEXT PROTOCOL</span><h2>MCP 访问配置</h2></div><span class="platform-badge">本机 stdio</span></div>
          <div class="mcp-setup-intro">
            <div class="settings-icon">AI</div>
            <div><strong>让 Codex 或其他 AI 客户端调用 iDeploy</strong><span>MCP Server 已内置在 ideploy 命令中，无需启动额外服务。请先安装上方终端命令，再将下面任一种配置添加到 AI 客户端。</span></div>
          </div>
          <div class="mcp-setup-grid">
            <article class="mcp-setup-card">
              <div class="mcp-step-heading"><span>A</span><div><strong>Codex</strong><small>在终端执行注册命令</small></div></div>
              <div class="mcp-code-block"><code>{{ mcpCodexCommand }}</code><button type="button" :aria-label="copiedMCPConfig === 'codex' ? '已复制 Codex 注册命令' : '复制 Codex 注册命令'" @click="copyMCPConfig('codex')">{{ copiedMCPConfig === 'codex' ? '已复制' : '复制' }}</button></div>
              <div class="mcp-verify"><span>注册后运行</span><code>codex mcp list</code><button type="button" @click="copyMCPConfig('verify')">{{ copiedMCPConfig === 'verify' ? '已复制' : '复制' }}</button></div>
            </article>
            <article class="mcp-setup-card">
              <div class="mcp-step-heading"><span>B</span><div><strong>其他 MCP 客户端</strong><small>添加 stdio Server 配置</small></div></div>
              <div class="mcp-code-block json"><pre>{{ mcpClientConfig }}</pre><button type="button" :aria-label="copiedMCPConfig === 'json' ? '已复制通用 MCP 配置' : '复制通用 MCP 配置'" @click="copyMCPConfig('json')">{{ copiedMCPConfig === 'json' ? '已复制' : '复制 JSON' }}</button></div>
            </article>
          </div>
          <div class="mcp-security-note"><strong>连接方式与权限</strong><span>客户端通过标准输入输出启动 <code>{{ mcpCommandPath }} mcp</code>，不会监听网络端口。MCP 与桌面端共享项目、部署记录和系统钥匙串凭据，但不会把 Registry、SSH 或 Keychain 密钥返回给 AI。发布、回滚、启停等操作会修改远程环境，请仅授权可信的本机客户端。</span></div>
        </section>

        <section class="panel settings-panel">
          <div class="panel-header"><div><span class="section-kicker">LOCAL DATA</span><h2>本地数据管理</h2></div><span class="platform-badge">仅影响本机</span></div>
          <div class="settings-list">
            <article class="settings-row">
              <div class="settings-icon">↻</div>
              <div class="settings-copy"><strong>清理运行缓存</strong><span>删除文件缓存和远程 Docker 容器快照；保留服务器、项目、部署关联和登录凭据。</span><code :title="overview?.cachePath">{{ overview?.cachePath || '正在读取缓存目录…' }}</code></div>
              <button class="secondary-button" :disabled="cacheCleaning || serverDataClearing" @click="clearRuntimeCache">{{ cacheCleaning ? '清理中…' : '清理缓存' }}</button>
            </article>

            <article class="settings-row danger-zone">
              <div class="settings-icon danger">!</div>
              <div class="settings-copy"><strong>清除所有服务器数据</strong><span>删除服务器连接、部署关联、远程容器快照、服务器任务记录及 iDeploy 保存的 Keychain SSH 凭据。</span><small>不会删除项目、镜像仓库、远程容器或系统级 SSH known_hosts。</small></div>
              <button v-if="!serverDataConfirming" class="secondary-button danger-button" :disabled="serverDataClearing || cacheCleaning" @click="serverDataConfirming = true">清除服务器数据</button>
              <div v-else class="settings-confirm">
                <strong>确定清除？</strong>
                <span>此操作无法在应用内撤销。</span>
                <div><button class="text-button" :disabled="serverDataClearing" @click="serverDataConfirming = false">取消</button><button class="secondary-button danger-button" :disabled="serverDataClearing" @click="clearAllServerData">{{ serverDataClearing ? '正在清除…' : '确认全部清除' }}</button></div>
              </div>
            </article>
          </div>
        </section>

        <section class="panel settings-panel">
          <div class="panel-header"><div><span class="section-kicker">AI ASSISTANT</span><h2>Dockerfile 智能生成</h2></div><span class="platform-badge">可选</span></div>
          <div class="settings-row ai-settings-row">
            <div class="settings-icon">✦</div>
            <div class="settings-copy"><strong>DeepSeek</strong><span>{{ aiSettings.deepSeekConfigured ? 'API Key 已配置，保存在 macOS 系统钥匙串。' : '未配置 API Key；常见技术栈仍可使用内置模板离线生成。' }}</span><small>调用 DeepSeek 时只发送受限项目清单摘要；生成结果必须预览确认后才会写入项目。</small></div>
            <div class="ai-settings-form">
              <label class="ai-key-label" for="deepseek-api-key">API Key</label>
              <div class="ai-key-controls">
                <div class="secret-input">
                  <input id="deepseek-api-key" v-model="deepSeekAPIKey" :type="deepSeekKeyVisible ? 'text' : 'password'" autocomplete="new-password" placeholder="输入 DeepSeek API Key">
                  <button type="button" :aria-label="deepSeekKeyVisible ? '隐藏 API Key' : '显示 API Key'" :aria-pressed="deepSeekKeyVisible" @click="deepSeekKeyVisible = !deepSeekKeyVisible">{{ deepSeekKeyVisible ? '隐藏' : '显示' }}</button>
                </div>
                <div class="ai-key-actions">
                  <button class="secondary-button ai-key-action" :disabled="aiSettingsSaving || !deepSeekAPIKey.trim()" @click="saveDeepSeekAPIKey">{{ aiSettingsSaving && deepSeekAPIKey.trim() ? '保存中…' : '保存 Key' }}</button>
                  <button v-if="aiSettings.deepSeekConfigured" class="text-button danger-text ai-key-clear" :disabled="aiSettingsSaving" @click="clearDeepSeekAPIKey">清除 Key</button>
                </div>
              </div>
            </div>
          </div>
        </section>

        <section v-if="cleanupResult" class="cleanup-result" role="status"><strong>{{ cleanupResult.detail }}</strong><span>服务器 {{ cleanupResult.servers }} · 部署关联 {{ cleanupResult.deployments }} · 快照 {{ cleanupResult.snapshots }} · 任务 {{ cleanupResult.jobs }} · Keychain 凭据 {{ cleanupResult.credentials }}</span><span v-if="cleanupResult.credentialErrors" class="cleanup-warning">有 {{ cleanupResult.credentialErrors }} 个 Keychain 凭据需要手动清理。</span></section>
        <section class="panel storage-paths"><div><span>数据库</span><code :title="overview?.databasePath">{{ overview?.databasePath || '—' }}</code></div><div><span>日志</span><code :title="overview?.logPath">{{ overview?.logPath || '—' }}</code></div></section>
      </section>

      <section v-else class="module-placeholder" :aria-label="activeSectionMeta.title">
        <div class="module-illustration" aria-hidden="true">
          <span></span><span></span><span></span>
        </div>
        <span class="section-kicker">MODULE FOUNDATION</span>
        <h2>{{ activeSectionMeta.emptyTitle }}</h2>
        <p>{{ activeSectionMeta.description }}</p>
        <div class="module-capabilities">
          <span v-for="item in activeSectionMeta.items" :key="item">{{ item }}</span>
        </div>
        <span class="module-stage">将在后续阶段接入真实数据</span>
      </section>

      <div v-if="registryDialogOpen" class="modal-backdrop" @click.self="closeRegistryDialog">
        <form class="registry-dialog" @submit.prevent="createRegistry">
          <div class="dialog-heading">
            <div>
              <span class="section-kicker">{{ editingRegistryID ? 'EDIT CONNECTION' : 'NEW CONNECTION' }}</span>
              <h2>{{ editingRegistryID ? '编辑镜像仓库' : '添加镜像仓库' }}</h2>
            </div>
            <button type="button" class="dialog-close" aria-label="关闭" @click="closeRegistryDialog">×</button>
          </div>
          <label class="form-field">
            <span>显示名称</span>
            <input v-model.trim="registryForm.name" autocomplete="off" maxlength="80" placeholder="例如：团队 Harbor" required @input="clearRegistryFormProbe">
          </label>
          <label class="form-field">
            <span>仓库类型</span>
            <select v-model="registryForm.kind" @change="clearRegistryFormProbe">
              <option value="harbor">Harbor</option>
              <option value="oci">标准 OCI Registry</option>
            </select>
          </label>
          <label class="form-field">
            <span>服务地址</span>
            <input v-model.trim="registryForm.endpoint" autocomplete="url" placeholder="harbor.example.com 或 https://registry.example.com" required @input="clearRegistryFormProbe">
            <small>省略协议时默认使用 HTTPS；当前不接受带路径或内嵌凭据的地址。</small>
          </label>
          <div class="server-form-grid">
            <label class="form-field"><span>用户名（发布必填）</span><input v-model.trim="registryForm.username" autocomplete="username" maxlength="255" placeholder="robot$ideploy 或账号" @input="clearRegistryFormProbe"></label>
            <label class="form-field"><span>密码 / Token（发布必填）</span><div class="secret-input"><input v-model="registryForm.password" :type="secretVisibility.registryPassword ? 'text' : 'password'" autocomplete="new-password" :required="!!registryForm.username && !(editingRegistry?.credentialConfigured && editingRegistry.username === registryForm.username)" :placeholder="editingRegistry?.credentialConfigured ? '留空继续使用 Keychain 凭据' : '仓库密码或访问令牌'" @input="clearRegistryFormProbe"><button type="button" @click="secretVisibility.registryPassword = !secretVisibility.registryPassword">{{ secretVisibility.registryPassword ? '隐藏' : '显示' }}</button></div></label>
          </div>
          <div v-if="registryFormTestError" class="form-probe-result error">{{ registryFormTestError }}</div>
          <div v-else-if="registryFormProbe" class="form-probe-result" :class="registryFormProbe.status">
            <strong>{{ registryStatusLabel[registryFormProbe.status] }}</strong>
            <span>{{ registryFormProbe.detail }} · {{ registryFormProbe.endpoint }}</span>
          </div>
          <div class="dialog-security-note">用户名保存在本地数据库，密码或 Token 只保存到 macOS 系统钥匙串。发布时会安全地通过标准输入完成本机和服务器 Docker 登录。</div>
          <div class="dialog-actions">
            <button type="button" class="secondary-button" :disabled="registrySaving || registryFormTesting" @click="closeRegistryDialog">取消</button>
            <button type="button" class="secondary-button" :disabled="registrySaving || registryFormTesting" @click="testRegistryForm">{{ registryFormTesting ? '正在测试' : '测试连接与登录' }}</button>
            <button type="submit" class="primary-button" :disabled="registrySaving || registryFormTesting">{{ registrySaving ? '正在保存' : editingRegistryID ? '保存修改' : '保存连接' }}</button>
          </div>
        </form>
      </div>

      <div v-if="workspaceProjectDialogOpen" class="modal-backdrop" @click.self="closeWorkspaceProjectDialog">
        <form class="registry-dialog project-dialog" @submit.prevent="saveWorkspaceProject">
          <div class="dialog-heading">
            <div><span class="section-kicker">{{ editingWorkspaceProjectID ? 'EDIT PROJECT' : 'NEW PROJECT' }}</span><h2>{{ editingWorkspaceProjectID ? '编辑项目' : '添加项目' }}</h2></div>
            <button type="button" class="dialog-close" aria-label="关闭" @click="closeWorkspaceProjectDialog">×</button>
          </div>
          <label class="form-field"><span>项目名称</span><input v-model.trim="workspaceProjectForm.name" autocomplete="off" maxlength="80" placeholder="例如：订单 API" required></label>
          <label class="form-field"><span>说明（可选）</span><input v-model.trim="workspaceProjectForm.description" autocomplete="off" maxlength="500" placeholder="用于区分业务或服务"></label>
          <label class="form-field"><span>代码来源</span><select v-model="workspaceProjectForm.sourceKind"><option value="local">本地目录</option><option value="git">Git 仓库</option></select></label>
          <template v-if="workspaceProjectForm.sourceKind === 'local'">
            <label class="form-field"><span>项目目录</span><div class="path-picker"><input v-model.trim="workspaceProjectForm.localPath" autocomplete="off" placeholder="通过“选择目录”授权访问" required><button type="button" class="secondary-button" @click="chooseWorkspaceProjectDirectory">选择目录</button></div><small>只保存你授权的目录路径；iDeploy 不会复制或删除源码。</small></label>
          </template>
          <template v-else>
            <label class="form-field"><span>仓库地址</span><input v-model.trim="workspaceProjectForm.repositoryUrl" autocomplete="url" placeholder="https://github.com/org/repository.git" required></label>
            <div class="server-form-grid"><label class="form-field"><span>引用类型</span><select v-model="workspaceProjectForm.refKind"><option value="branch">分支</option><option value="tag">标签</option><option value="commit">Commit</option></select></label><label class="form-field"><span>引用</span><input v-model.trim="workspaceProjectForm.refValue" autocomplete="off" placeholder="main" required></label></div>
            <div class="dialog-security-note">发布时会把该引用检出到受保护的临时缓存；私有仓库使用本机 Git Credential Helper 或 SSH Agent，不在项目记录中保存密码。</div>
          </template>
          <div class="form-section"><span class="section-kicker">BUILD CONFIGURATION</span></div>
          <div class="server-form-grid"><label class="form-field"><span>构建上下文</span><input v-model.trim="workspaceProjectForm.contextPath" autocomplete="off" placeholder="."></label><label class="form-field"><span>Dockerfile（单容器必填）</span><input v-model.trim="workspaceProjectForm.dockerfilePath" autocomplete="off" placeholder="Dockerfile"></label></div>
          <label class="form-field"><span>Compose 文件（可选）</span><input v-model.trim="workspaceProjectForm.composePath" autocomplete="off" placeholder="docker-compose.yml"><small>用于发布时，服务的 image 字段需要包含 ${IMAGE_TAG}，例如 registry.example.com/team/api:${IMAGE_TAG}。</small></label>
          <label class="form-field"><span>目标镜像（单容器必填）</span><input v-model.trim="workspaceProjectForm.imageRepository" autocomplete="off" placeholder="registry.example.com/team/api"><small>填写仓库路径，不要带 Tag 或 Digest；例如 registry.example.com/team/api。Compose 发布可不填写。</small></label>
          <div class="dialog-actions"><button type="button" class="secondary-button" :disabled="workspaceProjectSaving" @click="closeWorkspaceProjectDialog">取消</button><button type="submit" class="primary-button" :disabled="workspaceProjectSaving">{{ workspaceProjectSaving ? '正在保存' : '保存项目' }}</button></div>
        </form>
      </div>

      <div v-if="dockerfileDialogOpen" class="modal-backdrop" @click.self="closeDockerfileDialog">
        <form class="registry-dialog dockerfile-dialog" @submit.prevent="saveGeneratedDockerfile">
          <div class="dialog-heading"><div><span class="section-kicker">DOCKERFILE ASSISTANT</span><h2>生成 Dockerfile</h2></div><button type="button" class="dialog-close" aria-label="关闭" @click="closeDockerfileDialog">×</button></div>
          <div v-if="dockerfileGenerationError" class="form-probe-result error" role="alert">{{ dockerfileGenerationError }}</div>
          <div class="dockerfile-toolbar"><div><strong>{{ dockerfileGeneration.technology || '等待识别' }}</strong><span>{{ dockerfileGeneration.detail }}</span></div><div class="dockerfile-actions"><button type="button" class="secondary-button" :disabled="dockerfileGenerating" @click="generateDockerfile('auto')">内置模板</button><button type="button" class="secondary-button" :disabled="dockerfileGenerating || !aiSettings.deepSeekConfigured" :title="aiSettings.deepSeekConfigured ? '使用 DeepSeek 生成' : '请先在设置中配置 API Key'" @click="generateDockerfile('deepseek')">{{ dockerfileGenerating ? '生成中…' : 'DeepSeek 生成' }}</button></div></div>
          <label class="form-field"><span>Dockerfile 内容</span><textarea v-model="dockerfileGeneration.content" rows="18" spellcheck="false" :disabled="dockerfileGenerating" placeholder="生成结果会显示在这里"></textarea></label>
          <div v-if="dockerfileGeneration.warnings.length" class="dockerfile-warnings"><strong>写入前请确认</strong><span v-for="warning in dockerfileGeneration.warnings" :key="warning">{{ warning }}</span></div>
          <label v-if="dockerfileProject?.dockerfilePath" class="dockerfile-overwrite-confirm"><input v-model="dockerfileOverwriteConfirmed" type="checkbox"><span><strong>覆盖现有 {{ dockerfileProject.dockerfilePath }}</strong><small>我已对比并确认上方启动命令、端口和构建步骤正确。</small></span></label>
          <div class="dialog-security-note">{{ dockerfileProject?.dockerfilePath ? '当前是重新生成预览；只有勾选覆盖确认后才会替换现有 Dockerfile。' : '确认后会在项目构建上下文中新建 Dockerfile。' }}</div>
          <div class="dialog-actions"><button type="button" class="secondary-button" :disabled="dockerfileGenerating || dockerfileSaving" @click="closeDockerfileDialog">取消</button><button type="submit" class="primary-button" :disabled="dockerfileGenerating || dockerfileSaving || !dockerfileGeneration.content.trim() || (!!dockerfileProject?.dockerfilePath && !dockerfileOverwriteConfirmed)">{{ dockerfileSaving ? '正在写入' : dockerfileProject?.dockerfilePath ? '确认覆盖并写入' : '确认并写入项目' }}</button></div>
        </form>
      </div>

      <div v-if="deploymentDialogOpen" class="modal-backdrop" @click.self="closeDeploymentDialog">
        <form class="registry-dialog" @submit.prevent="createDeployment">
          <div class="dialog-heading"><div><span class="section-kicker">DEPLOYMENT TARGET</span><h2>{{ editingDeploymentID ? '修改部署配置' : '新建部署' }}</h2></div><button type="button" class="dialog-close" aria-label="关闭" @click="closeDeploymentDialog">×</button></div>
          <div v-if="deploymentFormError" class="form-probe-result error" role="alert">{{ deploymentFormError }}</div>
          <label class="form-field"><span>项目</span><select v-model="deploymentForm.projectId" :disabled="!!editingDeploymentID" required><option v-for="project in deploymentProjectCandidates" :key="project.id" :value="project.id">{{ project.name }}</option></select></label>
          <label class="form-field"><span>目标服务器</span><select v-model="deploymentForm.serverId" :disabled="!!editingDeploymentID" required><option v-for="server in servers" :key="server.id" :value="server.id">{{ server.name }} · {{ server.sshUser }}@{{ server.host }}</option></select></label>
          <label class="form-field"><span>工作负载类型</span><select v-model="deploymentForm.workloadKind"><option value="container">独立容器</option><option value="compose">Compose 项目</option></select></label>
          <label class="form-field"><span>{{ deploymentForm.workloadKind === 'compose' ? 'Compose 项目名称' : '容器名称' }}</span><input v-model.trim="deploymentForm.workloadName" required maxlength="128" autocomplete="off"><small>名称必须与服务器完全一致；Compose 按项目标签识别并排除一次性任务，不按镜像猜测归属。</small></label>
          <template v-if="deploymentForm.workloadKind === 'container'">
            <label class="form-field"><span>端口映射（可选）</span><textarea v-model.trim="deploymentPortsText" rows="3" placeholder="8080&#10;127.0.0.1:5432:5432"></textarea><small>每行一条。只填外部端口（如 8080）时，发布会读取 Dockerfile 的 EXPOSE 自动匹配内部端口；也可填写完整的“外部端口:内部端口”。</small></label>
            <label class="form-field"><span>容器重启策略</span><select v-model="deploymentForm.restartPolicy"><option value="unless-stopped">unless-stopped</option><option value="always">always</option><option value="on-failure">on-failure</option><option value="no">no</option></select></label>
          </template>
          <div class="dialog-security-note">保存后可从部署列表发起发布。单容器模式会替换同名容器；Compose 模式会上传当前 Compose 快照并执行 pull / up。</div>
          <div class="dialog-actions"><button type="button" class="secondary-button" :disabled="deploymentSaving" @click="closeDeploymentDialog">取消</button><button type="submit" class="primary-button" :disabled="deploymentSaving || !deploymentForm.projectId || !deploymentForm.serverId || !deploymentForm.workloadName">{{ deploymentSaving ? '正在保存' : '保存部署配置' }}</button></div>
        </form>
      </div>

      <div v-if="releaseDialogOpen" class="modal-backdrop" @click.self="closeReleaseDialog">
        <form class="registry-dialog release-dialog" @submit.prevent="publishDeployment">
          <div class="dialog-heading"><div><span class="section-kicker">NEW RELEASE</span><h2>发布新版本</h2></div><button type="button" class="dialog-close" aria-label="关闭" @click="closeReleaseDialog">×</button></div>
          <div v-if="releaseFormError" class="form-probe-result error" role="alert">{{ releaseFormError }}</div>
          <div v-if="publishingDeployment" class="release-target-summary"><div><span>项目</span><strong>{{ publishingDeployment.projectName }}</strong></div><div><span>服务器</span><strong>{{ publishingDeployment.serverName }}</strong></div><div><span>工作负载</span><strong>{{ publishingDeployment.workloadName || '待配置' }}</strong></div></div>
          <ol class="release-pipeline-preview" aria-label="发布阶段">
            <li class="active"><b>1</b><span>发布检查</span></li><li><b>2</b><span>构建镜像</span></li><li><b>3</b><span>推送仓库</span></li><li><b>4</b><span>服务器拉取</span></li>
            <li><b>5</b><span>更新服务</span></li><li><b>6</b><span>健康检查</span></li><li><b>7</b><span>发布记录</span></li><li><b>8</b><span>可回滚</span></li>
          </ol>
          <section class="release-readiness" aria-label="发布前检查">
            <div class="release-readiness-heading"><div><strong>发布前检查</strong><small>红色项目处理完成后才能提交发布</small></div><span :class="releaseBlockingChecks.length ? 'blocked' : 'ready'">{{ releaseBlockingChecks.length ? `${releaseBlockingChecks.length} 项待处理` : '已就绪' }}</span></div>
            <div class="release-check-list">
              <div v-for="check in releasePreflightChecks" :key="check.id" class="release-check" :class="check.state">
                <span class="release-check-icon">{{ check.state === 'ready' ? '✓' : check.state === 'warning' ? '!' : '×' }}</span>
                <div><strong>{{ check.label }}</strong><small>{{ check.detail }}</small></div>
                <button v-if="check.action && (check.state !== 'ready' || ['build', 'image', 'workload'].includes(check.id))" type="button" class="text-button" @click="handleReleasePreflightAction(check)">{{ check.actionLabel }}</button>
              </div>
            </div>
          </section>
          <div class="release-target-summary"><div><span>当前版本（最近成功发布）</span><strong>{{ releaseVersionDetecting ? '读取中…' : releaseVersionSuggestion.currentVersion || '尚未成功发布' }}</strong></div><div><span>待发布版本</span><strong>{{ releaseForm.version || '计算中…' }}</strong></div></div>
          <label class="form-field"><span>发布版本</span><input v-model.trim="releaseForm.version" required maxlength="128" autocomplete="off" placeholder="例如 1.0.1" :disabled="releaseVersionDetecting"><small v-if="releaseVersionDetecting">正在比对全部发布记录并计算下一版本…</small><small v-else>{{ releaseVersionSuggestion.detail }}</small></label>
          <div class="setup-steps"><button type="button" class="secondary-button" :disabled="releaseVersionDetecting || !releaseVersionSuggestion.version" @click="releaseForm.version = releaseVersionSuggestion.version">使用自动升级版本 {{ releaseVersionSuggestion.version }}</button><button type="button" class="text-button" @click="showRollbackHistory">查看发布记录 / 回滚</button></div>
          <div v-if="!releaseVersionDetecting && versionPolicyIssue" class="form-probe-result error" role="alert">{{ versionPolicyIssue }}</div>
          <small>新版本默认递增补丁号；失败版本也不复用。发布后固定镜像 Digest，回滚恢复原制品。</small>
          <label class="form-field"><span>镜像仓库</span><select v-model="releaseForm.registryId" required><option value="" disabled>选择镜像仓库</option><option v-for="registry in registries" :key="registry.id" :value="registry.id">{{ registry.name }} · {{ registryStatusLabel[registry.status] }}</option></select><small>本机推送和远端拉取分别使用各自 Docker 已保存的登录凭据。</small></label>
          <div v-if="!registries.length" class="form-probe-result error">尚未配置镜像仓库，请先前往“镜像仓库”页面添加并测试连接。</div>
          <div class="dialog-security-note">发布会替换远端同名容器或更新 Compose 项目，服务可能短暂不可用。成功后会保存制品引用、源码修订与 Compose 快照用于回滚。</div>
          <div class="dialog-actions"><button type="button" class="secondary-button" :disabled="releaseSaving" @click="closeReleaseDialog">取消</button><button type="submit" class="primary-button" :disabled="releaseSaving || releaseVersionDetecting || !!versionPolicyIssue || !!releaseBlockingChecks.length || !releaseForm.version || !releaseForm.registryId" :title="versionPolicyIssue || (releaseBlockingChecks.length ? '请先完成发布前检查' : '确认并创建发布任务')">{{ releaseSaving ? '正在创建任务' : '确认并开始发布' }}</button></div>
        </form>
      </div>

      <div v-if="databaseDialogOpen" class="modal-backdrop" @click.self="closeDatabaseDialog">
        <form class="registry-dialog database-connection-dialog" @submit.prevent="saveDatabaseConnection">
          <div class="dialog-heading"><div><span class="section-kicker">READ-ONLY DATABASE</span><h2>{{ editingDatabaseID ? '编辑数据库连接' : '添加数据库连接' }}</h2></div><button type="button" class="dialog-close" aria-label="关闭" @click="closeDatabaseDialog">×</button></div>
          <div class="form-section-heading"><span>连接信息</span><small>请使用仅拥有读取权限的数据库账号</small></div>
          <label class="form-field"><span>显示名称</span><input v-model.trim="databaseForm.name" autocomplete="off" maxlength="80" placeholder="例如：生产订单库" required @input="databaseFormTestSuccess=''" /></label>
          <div class="server-form-grid"><label class="form-field"><span>数据库类型</span><select v-model="databaseForm.driver" @change="updateDatabaseDriverDefaults"><option value="postgres">PostgreSQL</option><option value="mysql">MySQL / MariaDB</option></select></label><label class="form-field"><span>TLS</span><select v-model="databaseForm.sslMode"><option value="require">启用加密</option><option value="disable">不加密</option></select></label></div>
          <div class="server-address-grid"><label class="form-field"><span>主机名或 IP</span><input v-model.trim="databaseForm.host" autocomplete="off" placeholder="db.example.com" required @input="databaseFormTestSuccess=''" /></label><label class="form-field"><span>端口</span><input v-model.number="databaseForm.port" type="number" min="1" max="65535" required @input="databaseFormTestSuccess=''" /></label></div>
          <div class="server-form-grid"><label class="form-field"><span>数据库名称 <small>MySQL 可选</small></span><input v-model.trim="databaseForm.databaseName" autocomplete="off" placeholder="留空后连接并选择数据库" :required="databaseForm.driver === 'postgres'" @input="databaseFormTestSuccess=''" /><small v-if="databaseForm.driver === 'mysql'">留空后可选择数据库。</small><small v-else>PostgreSQL 连接必须指定一个数据库。</small></label><label class="form-field"><span>用户名</span><input v-model.trim="databaseForm.username" autocomplete="username" placeholder="readonly_user" required @input="databaseFormTestSuccess=''" /></label></div>
          <label class="form-field"><span>数据库密码</span><div class="secret-input"><input v-model="databaseForm.password" :type="databasePasswordVisible ? 'text' : 'password'" autocomplete="new-password" :placeholder="editingDatabaseID ? '留空继续使用系统钥匙串中的密码' : '输入数据库密码'" :required="!editingDatabaseID" @input="databaseFormTestSuccess=''" /><button type="button" :aria-label="databasePasswordVisible ? '隐藏数据库密码' : '显示数据库密码'" @click="databasePasswordVisible = !databasePasswordVisible">{{ databasePasswordVisible ? '隐藏' : '显示' }}</button></div><small>密码仅保存在 macOS 系统钥匙串，不会写入 iDeploy 的 SQLite 数据库。</small></label>
          <div v-if="databaseError" class="form-probe-result error" role="alert">{{ databaseError }}</div><div v-else-if="databaseFormTestSuccess" class="form-probe-result healthy" role="status"><strong>连接成功</strong><span>{{ databaseFormTestSuccess }}</span></div>
          <div class="dialog-security-note">此模块只会读取元数据和受限分页数据。请为生产环境创建权限最小化的只读账号；关闭 TLS 会降低网络传输安全性。</div>
          <div class="dialog-actions"><button type="button" class="secondary-button" :disabled="databaseSaving || databaseFormTesting" @click="closeDatabaseDialog">取消</button><button type="button" class="secondary-button" :disabled="databaseSaving || databaseFormTesting || !databaseForm.host || (databaseForm.driver === 'postgres' && !databaseForm.databaseName) || !databaseForm.username || (!editingDatabaseID && !databaseForm.password)" @click="testDatabaseForm">{{ databaseFormTesting ? '测试中' : '测试连接' }}</button><button type="submit" class="primary-button" :disabled="databaseSaving || databaseFormTesting">{{ databaseSaving ? '保存中' : '保存连接' }}</button></div>
        </form>
      </div>

      <div v-if="serverDialogOpen" class="modal-backdrop" @click.self="closeServerDialog">
        <form class="registry-dialog server-connection-dialog" @submit.prevent="createServer">
          <div class="dialog-heading"><div><span class="section-kicker">NEW REMOTE HOST</span><h2>添加开发服务器</h2></div><button type="button" class="dialog-close" aria-label="关闭" @click="closeServerDialog">×</button></div>
          <div class="form-section-heading"><span>连接信息</span><small>填写服务器地址与 SSH 端口</small></div>
          <label class="form-field"><span>显示名称</span><input v-model.trim="serverForm.name" autocomplete="off" maxlength="80" placeholder="例如：生产 API" required></label>
          <div class="server-address-grid"><label class="form-field"><span>IP 地址或主机名</span><input v-model.trim="serverForm.host" autocomplete="off" placeholder="10.0.0.8 或 server.example.com" required @input="clearServerFormTest"></label><label class="form-field"><span>端口</span><input v-model.number="serverForm.port" type="number" min="1" max="65535" required @input="clearServerFormTest"></label></div>
          <div class="form-section-heading"><span>登录信息</span><small>选择服务器实际启用的认证方式</small></div>
          <div class="server-form-grid"><label class="form-field"><span>用户名</span><input v-model.trim="serverForm.sshUser" autocomplete="username" placeholder="root 或 deploy" required @input="clearServerFormTest"></label><label class="form-field"><span>登录方式</span><select v-model="serverForm.authMethod" @change="clearServerFormTest"><option value="agent">SSH Agent</option><option value="password">密码</option><option value="private_key">私钥文件</option></select></label></div>
          <label v-if="serverForm.authMethod === 'password'" class="form-field"><span>SSH 密码</span><div class="secret-input"><input v-model="serverForm.password" :type="secretVisibility.createPassword ? 'text' : 'password'" autocomplete="new-password" placeholder="输入服务器密码" required @input="clearServerFormTest"><button type="button" :aria-label="secretVisibility.createPassword ? '隐藏 SSH 密码' : '显示 SSH 密码'" @click="secretVisibility.createPassword = !secretVisibility.createPassword">{{ secretVisibility.createPassword ? '隐藏' : '显示' }}</button></div><small>密码仅保存到 macOS 系统钥匙串，不写入 SQLite。</small></label>
          <template v-else-if="serverForm.authMethod === 'private_key'">
            <label class="form-field"><span>私钥文件</span><div class="path-picker"><input v-model.trim="serverForm.privateKeyPath" autocomplete="off" placeholder="选择 id_ed25519、id_rsa 等私钥" required @input="clearServerFormTest"><button type="button" class="secondary-button" @click="chooseSSHPrivateKey('create')">选择文件</button></div><small>只保存私钥路径，私钥文件不会复制到 iDeploy。</small></label>
            <label class="form-field"><span>私钥口令（可选）</span><div class="secret-input"><input v-model="serverForm.privateKeyPassphrase" :type="secretVisibility.createPassphrase ? 'text' : 'password'" autocomplete="new-password" placeholder="未加密的私钥请留空" @input="clearServerFormTest"><button type="button" :aria-label="secretVisibility.createPassphrase ? '隐藏私钥口令' : '显示私钥口令'" @click="secretVisibility.createPassphrase = !secretVisibility.createPassphrase">{{ secretVisibility.createPassphrase ? '隐藏' : '显示' }}</button></div><small>填写时仅保存到 macOS 系统钥匙串。</small></label>
          </template>
          <div v-else class="auth-method-note"><strong>使用本机 SSH Agent</strong><span>将使用已加载到 Agent 的密钥和你的 SSH 配置，不保存额外凭据。</span></div>
          <div v-if="serverFormTestError" class="form-probe-result error" role="alert">{{ serverFormTestError }}</div>
          <div v-else-if="serverFormTestResult" class="form-probe-result" :class="serverFormTestResult.status" role="status"><strong>{{ serverFormTestResult.status === 'healthy' ? 'SSH 连接成功' : serverStatusLabel[serverFormTestResult.status] }}</strong><span>{{ serverFormTestResult.detail }}</span></div>
          <div v-if="isSSHHostKeyIssue(serverFormTestResult)" class="dialog-host-key">
            <div><strong>确认服务器身份</strong><span v-if="!serverFormHostKey">先获取主机公开密钥，再与服务器控制台中的指纹核对。</span><template v-else><code>{{ serverFormHostKey.keyType }} · {{ serverFormHostKey.fingerprint }}</code><span>{{ serverFormHostKey.detail }}</span></template></div>
            <button v-if="!serverFormHostKey" type="button" class="secondary-button" :disabled="!!dialogHostKeyLoading" @click="inspectDialogSSHHostKey('create')">{{ dialogHostKeyLoading === 'create' ? '获取中' : '获取主机指纹' }}</button>
            <button v-else-if="serverFormHostKey.status === 'unknown' && dialogHostKeyConfirming !== 'create'" type="button" class="secondary-button" @click="dialogHostKeyConfirming = 'create'">核对后信任</button>
            <div v-else-if="serverFormHostKey.status === 'unknown'" class="host-key-confirm-actions"><span>确认指纹完全一致？</span><button type="button" class="text-button" @click="dialogHostKeyConfirming = ''">取消</button><button type="button" class="secondary-button" :disabled="!!dialogHostKeyTrusting" @click="trustDialogSSHHostKey('create')">{{ dialogHostKeyTrusting === 'create' ? '保存中' : '确认并写入' }}</button></div>
            <span v-else-if="serverFormHostKey.status === 'trusted'" class="trusted-label">✓ 已信任，请重新测试</span>
            <span v-else class="changed-label">指纹已变化，已阻止自动覆盖</span>
          </div>
          <div class="dialog-security-note">首次点击“测试 SSH”会自动信任首次看到的主机指纹并继续验证登录；指纹发生变化时仍会阻止连接。测试不会访问 Docker。</div>
          <div class="dialog-actions"><button type="button" class="secondary-button" :disabled="serverSaving || serverFormTesting" @click="closeServerDialog">取消</button><button type="button" class="secondary-button" :disabled="serverSaving || serverFormTesting || !serverForm.host || !serverForm.sshUser || (serverForm.authMethod === 'password' && !serverForm.password) || (serverForm.authMethod === 'private_key' && !serverForm.privateKeyPath)" @click="testServerFormConnection">{{ serverFormTesting ? '正在测试' : '测试 SSH' }}</button><button type="submit" class="primary-button" :disabled="serverSaving || serverFormTesting">{{ serverSaving ? '正在保存' : '保存服务器' }}</button></div>
        </form>
      </div>

      <div v-if="serverCredentialDialogOpen" class="modal-backdrop" @click.self="closeServerCredentialDialog">
        <form class="registry-dialog server-connection-dialog" @submit.prevent="saveServerCredentials">
          <div class="dialog-heading"><div><span class="section-kicker">SSH CONNECTION</span><h2>编辑服务器连接</h2></div><button type="button" class="dialog-close" aria-label="关闭" @click="closeServerCredentialDialog">×</button></div>
          <div class="form-section-heading"><span>连接信息</span><small>修改后服务器状态会重置为待检测</small></div>
          <label class="form-field"><span>显示名称</span><input v-model.trim="serverCredentialForm.name" autocomplete="off" maxlength="80" required @input="clearServerCredentialTest"></label>
          <div class="server-address-grid"><label class="form-field"><span>IP 地址或主机名</span><input v-model.trim="serverCredentialForm.host" autocomplete="off" required @input="clearServerCredentialTest"></label><label class="form-field"><span>端口</span><input v-model.number="serverCredentialForm.port" type="number" min="1" max="65535" required @input="clearServerCredentialTest"></label></div>
          <div class="form-section-heading"><span>登录信息</span><small>凭据不会写入应用数据库</small></div>
          <div class="server-form-grid"><label class="form-field"><span>用户名</span><input v-model.trim="serverCredentialForm.sshUser" autocomplete="username" required @input="clearServerCredentialTest"></label><label class="form-field"><span>登录方式</span><select v-model="serverCredentialForm.authMethod" @change="clearServerCredentialTest"><option value="agent">SSH Agent</option><option value="password">密码</option><option value="private_key">私钥文件</option></select></label></div>
          <label v-if="serverCredentialForm.authMethod === 'password'" class="form-field"><span>SSH 密码</span><div class="secret-input"><input v-model="serverCredentialForm.password" :type="secretVisibility.editPassword ? 'text' : 'password'" autocomplete="new-password" :placeholder="editingServerCredential?.authMethod === 'password' && editingServerCredential?.credentialConfigured ? '留空继续使用钥匙串中的密码' : '输入服务器密码'" @input="clearServerCredentialTest"><button type="button" :aria-label="secretVisibility.editPassword ? '隐藏 SSH 密码' : '显示 SSH 密码'" @click="secretVisibility.editPassword = !secretVisibility.editPassword">{{ secretVisibility.editPassword ? '隐藏' : '显示' }}</button></div><small>留空会继续使用已经保存的密码；输入新密码则替换它。</small></label>
          <template v-else-if="serverCredentialForm.authMethod === 'private_key'">
            <label class="form-field"><span>私钥文件</span><div class="path-picker"><input v-model.trim="serverCredentialForm.privateKeyPath" autocomplete="off" required @input="clearServerCredentialTest"><button type="button" class="secondary-button" @click="chooseSSHPrivateKey('edit')">选择文件</button></div></label>
            <label class="form-field"><span>私钥口令（可选）</span><div class="secret-input"><input v-model="serverCredentialForm.privateKeyPassphrase" :type="secretVisibility.editPassphrase ? 'text' : 'password'" autocomplete="new-password" :placeholder="editingServerCredential?.authMethod === 'private_key' && editingServerCredential?.credentialConfigured ? '留空继续使用钥匙串中的口令' : '未加密的私钥请留空'" @input="clearServerCredentialTest"><button type="button" :aria-label="secretVisibility.editPassphrase ? '隐藏私钥口令' : '显示私钥口令'" @click="secretVisibility.editPassphrase = !secretVisibility.editPassphrase">{{ secretVisibility.editPassphrase ? '隐藏' : '显示' }}</button></div><small>留空会继续使用已保存的口令；更换认证方式会移除旧凭据。</small></label>
          </template>
          <div v-else class="auth-method-note"><strong>使用本机 SSH Agent</strong><span>适合已执行 ssh-add 或由系统钥匙串管理私钥的环境。</span></div>
          <div v-if="serverCredentialTestError" class="form-probe-result error" role="alert">{{ serverCredentialTestError }}</div>
          <div v-else-if="serverCredentialTestResult" class="form-probe-result" :class="serverCredentialTestResult.status" role="status"><strong>{{ serverCredentialTestResult.status === 'healthy' ? 'SSH 连接成功' : serverStatusLabel[serverCredentialTestResult.status] }}</strong><span>{{ serverCredentialTestResult.detail }}</span></div>
          <div v-if="isSSHHostKeyIssue(serverCredentialTestResult)" class="dialog-host-key">
            <div><strong>确认服务器身份</strong><span v-if="!serverCredentialHostKey">先获取主机公开密钥，再与服务器控制台中的指纹核对。</span><template v-else><code>{{ serverCredentialHostKey.keyType }} · {{ serverCredentialHostKey.fingerprint }}</code><span>{{ serverCredentialHostKey.detail }}</span></template></div>
            <button v-if="!serverCredentialHostKey" type="button" class="secondary-button" :disabled="!!dialogHostKeyLoading" @click="inspectDialogSSHHostKey('edit')">{{ dialogHostKeyLoading === 'edit' ? '获取中' : '获取主机指纹' }}</button>
            <button v-else-if="serverCredentialHostKey.status === 'unknown' && dialogHostKeyConfirming !== 'edit'" type="button" class="secondary-button" @click="dialogHostKeyConfirming = 'edit'">核对后信任</button>
            <div v-else-if="serverCredentialHostKey.status === 'unknown'" class="host-key-confirm-actions"><span>确认指纹完全一致？</span><button type="button" class="text-button" @click="dialogHostKeyConfirming = ''">取消</button><button type="button" class="secondary-button" :disabled="!!dialogHostKeyTrusting" @click="trustDialogSSHHostKey('edit')">{{ dialogHostKeyTrusting === 'edit' ? '保存中' : '确认并写入' }}</button></div>
            <span v-else-if="serverCredentialHostKey.status === 'trusted'" class="trusted-label">✓ 已信任，请重新测试</span>
            <span v-else class="changed-label">指纹已变化，已阻止自动覆盖</span>
          </div>
          <div class="dialog-security-note">“测试 SSH”会自动信任首次看到的主机指纹并继续验证登录；已记录指纹发生变化时不会自动覆盖。测试不会执行 Docker 命令。</div>
          <div class="dialog-actions"><button type="button" class="secondary-button" :disabled="serverCredentialSaving || serverCredentialTesting" @click="closeServerCredentialDialog">取消</button><button type="button" class="secondary-button" :disabled="serverCredentialSaving || serverCredentialTesting || !canTestServerCredential || !serverCredentialForm.host || !serverCredentialForm.sshUser" @click="testServerCredentialConnection">{{ serverCredentialTesting ? '正在测试' : '测试 SSH' }}</button><button type="submit" class="primary-button" :disabled="serverCredentialSaving || serverCredentialTesting || !serverCredentialForm.name || !serverCredentialForm.host || !serverCredentialForm.sshUser || (serverCredentialForm.authMethod === 'password' && !serverCredentialForm.password && !(editingServerCredential?.authMethod === 'password' && editingServerCredential?.credentialConfigured)) || (serverCredentialForm.authMethod === 'private_key' && !serverCredentialForm.privateKeyPath)">{{ serverCredentialSaving ? '正在保存' : '保存连接' }}</button></div>
        </form>
      </div>
    </main>
    <div class="toast-stack" aria-live="polite" aria-atomic="false">
      <div v-for="toast in toasts" :key="toast.id" class="app-toast" :class="toast.kind" :role="toast.kind === 'error' ? 'alert' : 'status'">
        <span class="toast-icon" aria-hidden="true">{{ toast.kind === 'success' ? '✓' : toast.kind === 'error' ? '!' : 'i' }}</span>
        <span class="toast-message">{{ toast.message }}</span>
        <button type="button" class="toast-dismiss" aria-label="关闭提示" @click="dismissToast(toast.id)">×</button>
      </div>
    </div>
  </div>
</template>
