# iDeploy 项目总体规划

## 1. 产品定位

iDeploy 是一个面向个人开发者的单用户桌面应用。它负责把本机目录或 Git 仓库转换为 Docker 镜像，推送到 Harbor 等镜像仓库，并将镜像以 Docker Container 或 Docker Compose 的方式发布到远程服务器，同时提供日志、终端、容器生命周期、回滚和本地操作记录。

第一阶段不把 iDeploy 做成 Kubernetes 平台，也不替代完整 CI/CD 系统。核心目标是让用户在一个本地桌面应用内完成：

1. 接入代码；
2. 构建镜像；
3. 推送镜像；
4. 发布到服务器；
5. 查看运行状态并执行日常运维；
6. 在发布失败时快速回滚。

## 2. 产品边界

### 2.1 首期支持

- 项目来源：本机目录、Git 仓库；
- Git 协议：HTTPS Token、SSH Key；
- 构建：Dockerfile、BuildKit、构建参数、多平台构建（后续增强）；
- 镜像仓库：Harbor 优先，兼容标准 OCI/Docker Registry；
- 发布目标：通过 SSH 管理的 Linux Docker 主机；
- 工作负载：单容器、Docker Compose；
- 运维：启动、停止、重启、删除、重建、日志、资源指标、受控终端；
- 发布策略：重新创建、健康检查、失败自动回滚、手动回滚；
- 本地能力：系统钥匙串凭据、持久化任务、操作记录、桌面通知。

### 2.2 暂不纳入 MVP

- Kubernetes、Docker Swarm；
- 完整 GitLab CI / GitHub Actions 替代品；
- 源码托管、制品扫描引擎、自建监控平台；
- 跨地域多控制中心、高可用调度；
- 任意远程 Shell 自动化平台。

这些能力应保留扩展接口，但不阻塞首个可用版本。

## 3. 核心概念

| 概念 | 说明 |
| --- | --- |
| Project | 一个可构建、可发布的业务项目，关联代码源与构建配置 |
| Source | 本地目录或 Git 仓库及其认证方式 |
| Build | 一次不可变的构建记录，输出镜像摘要和日志 |
| Registry | Harbor 或兼容 OCI Registry 的连接配置 |
| Artifact | 镜像、标签、Digest、架构、扫描状态等元数据 |
| Server | 通过 SSH 接入的 Docker 主机 |
| Environment | dev/test/staging/prod 等逻辑环境 |
| Application | 部署到某个环境和服务器上的单容器或 Compose 应用 |
| Release | 一次发布记录，绑定确定的镜像 Digest 和配置版本 |
| Job | 构建、推送、发布、回滚、运维操作的异步执行实例 |
| Credential | Git、SSH、Registry 等加密保存的凭据 |

## 4. 关键业务流程

### 4.1 项目接入

1. 创建项目并选择本地目录或 Git 仓库；
2. 验证路径或 Git 凭据；
3. 选择分支、Tag 或 Commit；
4. 识别 Dockerfile/Compose 文件；
5. 配置构建上下文、忽略规则、构建参数和镜像命名规则。

本地目录通过系统目录选择器直接接入。应用只保存用户授权过的目录路径和项目配置，不复制源码；构建时生成确定的源码快照或记录 Git Commit，确保构建可追溯。

### 4.2 构建与推送

```text
代码快照 -> BuildKit 构建 -> 本地临时镜像 -> 镜像标签
         -> 登录 Registry -> 推送 -> 获取 Digest -> 保存制品记录
```

- 每次构建固定到 Git Commit 或本地源码快照；
- 发布必须优先引用镜像 Digest，而不是仅引用可变 Tag；
- 密钥使用 BuildKit Secret/SSH Mount，不进入镜像层或普通构建参数；
- 日志实时输出并持久化，支持取消和超时；
- 构建完成后保存耗时、源码版本、镜像大小和 Digest。

### 4.3 发布与回滚

```text
选择制品 -> 发布前检查 -> 拉取镜像 -> 写入版本化配置
         -> 启动/更新容器 -> 健康检查 -> 切换成功
                                  \-> 失败 -> 恢复上一 Release
```

- 单容器应用保存端口、卷、网络、环境变量、重启策略和健康检查；
- Compose 应用保存经过渲染的 Compose 文件及变量版本；
- 配置中的敏感值只保存密文，任务日志自动脱敏；
- 每次发布产生不可变 Release，可一键回滚到上一成功版本；
- 同一应用的变更必须串行执行，防止并发发布互相覆盖。

### 4.4 部署项目状态模型

部署项目不能只保存一个简单的 `status`。iDeploy 分别记录期望状态、远端实际状态、健康状态、同步状态和当前操作状态，再计算用户看到的综合状态。

| 状态维度 | 可选值 | 说明 |
| --- | --- | --- |
| `desired_state` | `running`, `stopped` | 用户希望项目最终达到的状态 |
| `runtime_state` | `unknown`, `not_deployed`, `starting`, `running`, `degraded`, `stopped`, `exited`, `restarting` | Docker/Compose 实际运行状态 |
| `health_state` | `unknown`, `starting`, `healthy`, `unhealthy`, `none` | 容器健康检查的聚合结果 |
| `sync_state` | `checking`, `synced`, `drifted`, `unmanaged`, `unreachable` | 本地 Release 与远端镜像、配置是否一致 |
| `operation_state` | `idle`, `queued`, `running`, `canceling`, `failed` | 当前是否存在构建、发布或运维任务 |

UI 展示的综合状态按以下优先级计算：

1. 正在执行操作：显示“发布中”“停止中”“回滚中”等具体操作；
2. 主机无法连接：显示“主机离线”，同时保留最后一次已知状态和采集时间；
3. 已发现但尚未纳管：显示“已发现”，并附带其实际运行/停止状态；
4. 远端镜像 Digest 或配置哈希不一致：显示“配置漂移”；
5. 目标为运行且全部服务运行、健康检查通过：显示“运行正常”；
6. 部分服务停止、退出或不健康：显示“部分异常”；
7. 目标为停止且所有服务停止：显示“已停止”；
8. 从未发布或远端资源不存在：显示“未部署”；
9. 无法可靠判断：显示“状态未知”，不得把未知误报为停止。

Compose 项目在列表中显示 `运行服务数 / 期望服务数`，例如 `3/4`，并显示健康容器数、当前镜像版本、最后成功发布时间和状态更新时间。单容器项目使用相同模型，期望服务数固定为 1。

### 4.5 状态发现与同步逻辑

iDeploy 同时展示两类远端项目：

- 受管项目：由 iDeploy 发布，拥有完整的 Project、Application、Release 和操作历史；
- 已发现项目：远端已有的 Docker Compose Project 或容器，只提供查看和基础运维，可由用户显式“纳管”。

受管容器统一写入以下 Docker Label：

- `io.ideploy.managed=true`；
- `io.ideploy.project-id`；
- `io.ideploy.application-id`；
- `io.ideploy.release-id`；
- `io.ideploy.config-hash`。

状态同步流程：

```text
读取本地期望状态
  -> SSH 连通检查
  -> 查询 docker info / docker compose ls
  -> 查询目标容器 inspect、镜像 Digest、Label、Health
  -> 按 Application 聚合
  -> 与当前 Release 的 Digest、配置哈希比较
  -> 更新状态快照并通知 UI
```

采集时机：

- 应用启动、从后台恢复、打开项目详情时立即刷新；
- 发布或运维操作期间每 2 秒刷新当前项目；
- 前台空闲时每 15 秒刷新当前页面相关项目；
- 后台驻留时每 60 秒巡检受管项目；
- 用户可以手动刷新，连续操作需要合并，避免重复 SSH 请求；
- 同一主机的查询尽量在一次 SSH 会话内批量完成。

每个状态快照包含 `observed_at`。超过两个巡检周期未更新时，UI 标记为“状态可能已过期”；SSH 失败只更新 `sync_state=unreachable`，不覆盖最后一次可信运行状态。

### 4.6 部署项目操作逻辑

所有变更操作先获取 `server_id + application_id` 级别的互斥锁。同一应用同一时间只能有一个变更 Job；查看状态和日志不占变更锁。

操作可用性：

| 当前状态 | 可用操作 |
| --- | --- |
| 运行正常 | 停止、重启、部署新版本、重新部署、回滚、日志、终端、删除部署 |
| 部分异常 | 停止、重启、重新部署、回滚、日志、终端、诊断 |
| 已停止 | 启动、部署新版本、重新部署、回滚、删除部署 |
| 未部署 | 部署当前版本 |
| 配置漂移 | 查看差异、以本地 Release 覆盖、将远端状态纳管；禁止自动覆盖 |
| 已发现 | 查看、日志、启动、停止、重启、纳管；部署和回滚需先纳管 |
| 主机离线/状态未知 | 刷新、测试连接、编辑连接；禁用远端变更操作 |
| 正在变更 | 查看进度、查看日志、在安全阶段取消；其他变更操作禁用 |

Job 取消采用安全检查点：在拉取镜像、写入临时配置等尚未切换服务的阶段可以直接取消；进入容器替换或回滚阶段后，取消请求只设置 `canceling`，待当前原子步骤完成并重新采集状态后再结束，不能简单中断 SSH 导致远端状态未知。

#### 查看详情

- 显示综合状态、各服务/容器状态、镜像 Tag 与 Digest、端口、网络、卷、健康检查和最近错误；
- 显示当前 Release、上一成功 Release、本地配置哈希和远端配置哈希；
- 默认只读，不会因为打开页面而改变远端资源；
- 可进入日志、终端、发布历史和原始 `docker inspect` 诊断信息。

#### 启动

1. 获取锁并刷新状态；
2. 将 `desired_state` 设为 `running` 并创建启动 Job；
3. 如果远端资源存在但已停止，执行 `docker start` 或 `docker compose start`；
4. 如果资源不存在，不自动猜测配置，提示用户执行“部署当前版本”；
5. 等待容器进入运行状态并执行健康检查；
6. 失败时仍保留运行期望，并显示具体服务和错误，便于用户重试或改为停止。

#### 停止

1. 获取锁并确认当前资源；
2. 将 `desired_state` 设为 `stopped` 并创建停止 Job；
3. 使用带超时的 `docker stop` 或 `docker compose stop`；
4. 默认保留容器、网络、卷和版本化配置；
5. 部分失败时保留停止期望并显示“部分异常”。

#### 重启

1. 只重启当前 Release 的现有容器；
2. 不拉取新镜像、不修改配置、不改变 Release；
3. 重启后执行健康检查；
4. 若镜像或配置已漂移，保留漂移提示，不把重启当成重新发布。

#### 部署新版本

1. 固化镜像 Digest、Compose/容器配置、环境变量引用和配置哈希，创建待执行 Release；
2. 检查 SSH、Docker/Compose 版本、磁盘空间、端口冲突和 Registry 登录；
3. 将配置写入远端 `~/.ideploy/apps/{application_id}/releases/{release_id}`；
4. 拉取固定 Digest 的镜像并执行单容器更新或 `docker compose up -d`；
5. 轮询容器运行状态和健康检查；
6. 成功后更新 `current` 指针和当前 Release；
7. 失败时收集诊断信息并自动应用上一成功 Release；
8. 如果自动回滚也失败，状态显示“发布失败且回滚失败”，禁止自动清理现场。

#### 重新部署

- 复用当前 Release 的镜像 Digest 和配置快照；
- 重新拉取、重建并健康检查；
- 不产生业务版本变化，但创建新的操作 Job 和 Release Attempt；
- 适用于修复远端资源被误删或运行状态异常，不用于升级镜像。

#### 回滚

1. 用户选择一个历史成功 Release；
2. iDeploy 创建新的回滚 Release，引用所选版本的镜像 Digest 与配置快照；
3. 执行与正常发布相同的检查、应用和健康验证；
4. 历史记录保持不可变，回滚失败时仍可返回回滚前版本。

#### 删除部署

- 默认执行 `docker compose down` 或删除受管容器，保留命名卷、镜像和历史配置；
- 删除命名卷、清理镜像必须单独勾选并再次确认；
- 只允许删除带 iDeploy Label 或经用户明确纳管的资源；
- 删除成功后状态变为“未部署”，Application 和 Release 历史仍保留；
- “从 iDeploy 移除项目”和“删除远端部署”是两个独立操作。

#### 查看日志与终端

- 日志操作只读，支持服务筛选、时间范围、尾部行数、持续跟随和下载；
- 日志流断开后允许重新连接，不修改项目状态；
- 终端默认关闭，启用后必须选择明确的容器和 Shell；
- 终端退出只结束会话，不停止容器；所有会话记录开始时间、结束时间和目标容器。

### 4.7 远程运维

- 主机连通性、Docker 版本、磁盘、CPU、内存检查；
- 容器和 Compose 应用列表、状态及资源占用；
- 启停、重启、重新拉取、重建和删除；
- 实时日志与有限时长的终端会话；
- 镜像、网络、卷的查看与安全清理；
- 高风险操作二次确认并写入本地操作记录。

MVP 使用 SSH 的无 Agent 模式，通过远程 Docker CLI/Compose CLI 执行经过参数化的固定操作。普通运维动作不允许直接拼接任意 Shell；独立的交互终端功能需要显式开启。后续存在大量主机或弱网络场景时，再考虑常驻 Agent。

### 4.8 Harbor 与镜像代理

这里要区分两个概念：

- Harbor Proxy Cache：由 Harbor 项目代理 Docker Hub 或其他上游 Registry，iDeploy 通过 Harbor API 创建/管理代理项目并展示缓存制品；
- Docker Registry Mirror：修改远程主机 Docker daemon 的 registry-mirrors 配置，通常需要重启 Docker，属于高风险主机配置操作。

MVP 优先支持 Harbor Registry、项目、仓库、制品、标签、机器人账号和 Proxy Cache 的使用。远程主机的 registry mirror 配置放到后续版本，并要求预览变更、备份 daemon.json、连接恢复检查和明确确认。

## 5. 总体架构

```text
┌──────────────────── iDeploy Desktop ────────────────────────┐
│ SwiftUI：项目 / 构建 / 制品 / 发布 / 主机 / Harbor / 运维     │
├────────────────────────────┬─────────────────────────────────┤
│ Go Application Core        │ Job Runner / Scheduler          │
│ Project / Registry / SSH   │ 构建、推送、发布、巡检、回滚     │
├─────────────────────┬──────┴─────────────────────────────────┤
│ SQLite（WAL）        │ OS Keychain / Credential Store         │
└──────────┬──────────┴───────────────────┬────────────────────┘
           │                              │
  Local FS / Git / Docker / BuildKit     SSH + HTTPS
                                          │
                                ┌─────────┴─────────┐
                                │ Harbor / Hosts   │
                                └───────────────────┘
```

### 5.1 架构原则

- 桌面模块化单体优先：SwiftUI 与 Go Core 通过当前用户专属的本地 Socket 通信；
- Job Runner 运行在 Go 后台进程中，主窗口关闭后仍可继续任务；
- 所有耗时或远程操作必须进入 Job，不阻塞 UI；
- 所有发布动作幂等、可重试、可取消、可追踪；
- 外部系统通过 Adapter 隔离，未来可替换 Docker、Harbor、Git、SSH 实现；
- SQLite 保存期望状态与操作记录，远端实时状态通过巡检同步；
- 配置与密钥分离，日志默认脱敏。

## 6. 推荐技术栈

### 6.1 本地核心

- Go 稳定版本；
- 桌面界面：SwiftUI 与 AppKit，Go 后台进程负责文件、Docker、SSH、Harbor、SQLite 和任务执行；
- 原生界面通过当前用户专属的 Unix Socket 调用 Go Runtime；应用包同时包含界面与后台可执行文件；
- 数据访问：SQLite WAL + sqlc，应用启动时执行内嵌迁移；
- 异步任务：基于 SQLite 的持久化 Job Runner，支持启动恢复、取消、超时和串行锁；
- Docker：官方 Docker Engine Go SDK；
- SSH：`golang.org/x/crypto/ssh`，SFTP 用于传输版本化配置；
- Git：优先调用受控的系统 Git，凭据通过临时 askpass/SSH 配置注入；
- Harbor：Harbor v2 REST API；
- 日志与状态流：使用本地服务接口；交互终端采用独立流式通道；
- 指标：Prometheus 格式；日志：结构化 JSON。

选择 Go 的原因是桌面打包、并发任务、SSH/Docker 生态和较低资源占用都适合本地核心服务。

### 6.2 桌面界面

- SwiftUI 与 AppKit 原生控件；
- 本地能力：系统目录选择器、菜单栏、桌面通知、系统钥匙串；
- 实时状态由原生界面通过本地服务接口获取。

### 6.3 本地运行依赖

- SQLite 数据库随应用保存在用户数据目录，不要求安装数据库服务；
- 优先复用本机 Docker Desktop/Docker Engine 的 BuildKit；
- 首次启动执行 Docker、Git、SSH 能力检测，并明确提示缺失依赖；
- 数据库、日志和加密配置支持一键导出备份；敏感凭据默认不包含在普通备份中；
- macOS 13+ 是主要交付平台，优先支持 Apple Silicon，并保留 Intel 构建能力；
- 发布产物包括签名后的 `.app` 和 `.dmg`，正式分发需要 Apple Developer ID 签名与 Notarization；
- Windows/Linux 只保留 Adapter 边界，不进入首版验收范围。

### 6.4 macOS 集成约定

- 主窗口关闭后应用继续驻留，菜单栏图标展示运行中任务数和异常项目数；
- 用户执行 `Cmd+Q` 时，如果存在不可安全中断的任务，弹出任务清单并阻止直接退出；
- 可选“登录时启动”，默认关闭；后台启动时不自动弹出主窗口；
- 从系统睡眠恢复或网络切换后，重建连接池并立即刷新正在运行的 Job 与项目状态；
- Docker、Git、SSH 路径先从应用进程 `PATH` 解析；macOS 图形应用找不到时，通过 `/bin/zsh -lic` 加载 `.zprofile` 和 `.zshrc` 后再次解析，并在诊断页显示路径来源；
- SQLite 放在 `~/Library/Application Support/iDeploy/`，日志放在 `~/Library/Logs/iDeploy/`，缓存放在 `~/Library/Caches/iDeploy/`；
- 凭据写入 macOS Keychain，数据库仅保存随机生成的凭据 ID；
- 使用系统原生目录选择器、通知中心、应用菜单和深浅色主题；
- 正式发布提供 Apple Silicon 与 Intel 构建，并完成代码签名、Hardened Runtime 和 Notarization。

## 7. 后端模块划分

建议目录结构：

```text
cmd/
  ideploy/             桌面应用入口
internal/
  app/                 生命周期、托盘、桌面桥接
  project/             项目、代码源、构建配置
  build/               源码快照、BuildKit、构建记录
  registry/            OCI Registry 通用能力
  harbor/              Harbor 专属 API 与 Proxy Cache
  server/              主机、SSH、巡检、主机密钥
  application/         单容器/Compose 应用定义
  release/             发布、回滚、健康检查
  job/                 队列、状态机、日志、取消与重试
  credential/          加密、引用、轮换、脱敏
  audit/               本地操作记录
  notification/        桌面与 Webhook 通知
  adapter/             Docker、Git、SSH、Harbor 外部适配器
  platform/            DB、配置、日志、指标、桌面运行基础设施
macos/Sources/          SwiftUI 原生界面
migrations/             数据库迁移
packaging/              macOS 打包、签名与公证资源
docs/                   架构、接口、运维文档
```

模块之间通过服务接口交互，禁止 UI 桥接层直接操作 Docker、SSH 或数据库细节。

## 8. 数据模型初稿

主要表：

- `app_settings`；
- `projects`, `project_sources`, `build_configs`；
- `builds`, `build_artifacts`；
- `registries`, `registry_projects`；
- `servers`, `server_status_snapshots`；
- `environments`；
- `applications`, `application_configs`；
- `application_status_snapshots`, `discovered_workloads`；
- `releases`, `release_steps`；
- `jobs`, `job_logs`, `job_attempts`；
- `credentials`；
- `audit_events`, `notifications`。

重要约束：

- 凭据表只存系统钥匙串引用和非敏感元数据，不保存或返回明文；
- Build、Release、配置版本采用不可变记录；
- Project、Server、Application 支持软删除；
- Job 带幂等键、租约、心跳、超时和取消标记；
- Application 同时保存期望状态、当前 Release 和最后可信状态快照；
- Job 使用 `server_id + application_id` 锁键阻止并发变更操作；
- Audit Event 只追加，不允许业务层更新或删除；
- 镜像以 `registry/repository@sha256:digest` 作为最终发布标识。

## 9. 任务状态机

统一任务状态：

```text
pending -> running -> succeeded
                   -> failed
                   -> canceled
        -> canceling -> canceled
```

Job Runner 获取任务时写入租约与心跳。应用异常退出后，重新启动时检查未完成任务，并根据任务类型标记失败、恢复巡检或询问用户是否重试。每个任务由多个 Step 组成，Step 保存开始时间、结束时间、退出码、结构化结果和脱敏日志。

发布任务至少包含：前置检查、登录仓库、拉取镜像、写配置、启动、健康检查、确认状态、清理旧版本。回滚本身也是新的 Release 和 Job，而不是篡改历史记录。

## 10. 应用服务接口规划

桌面 UI 只调用稳定的应用服务接口，不直接调用基础设施 Adapter：

- `ProjectService`：项目、代码源、构建配置；
- `BuildService`：触发构建、日志、取消；
- `RegistryService`：连接测试、仓库与制品；
- `HarborService`：项目、Proxy Cache、机器人账号；
- `ServerService`：连接测试、巡检、Docker 资源；
- `EnvironmentService`：环境管理；
- `ApplicationService`：应用定义和运行状态；
- `ReleaseService`：发布、回滚、发布记录；
- `JobService`：任务、步骤、日志流、取消、重试；
- `CredentialService`：保存、轮换、删除系统钥匙串凭据；
- `HistoryService`：本地操作记录查询；
- `SystemService`：版本、依赖检测、诊断、备份和恢复。

耗时写操作立即返回 `job_id`，UI 通过事件订阅任务状态和增量日志。容器终端会话绑定到明确的服务器与容器，窗口关闭或超时后主动断开。

## 11. 安全设计

- 单用户本地应用不设置登录、RBAC 和操作审批；系统登录态即本机使用边界；
- 凭据优先保存到 macOS Keychain、Windows Credential Manager 或 Linux Secret Service；
- SQLite 只保存凭据引用和非敏感元数据；不支持系统钥匙串时才启用本地主密码加密存储；
- SSH 默认校验 host key，首次信任需显示指纹并确认；
- Registry、Git、SSH 凭据按资源引用，不复制到项目配置；
- 禁止把密码写进命令行参数、构建日志或操作记录详情；
- 终端默认关闭，由用户在设置中显式开启，并记录会话元数据；
- 删除容器、卷、镜像清理、修改 Docker daemon 配置属于高风险操作；
- 生产环境发布、回滚以及破坏性操作进行二次确认；
- 应用数据目录权限限制为当前系统用户；
- 如果启用 Webhook，则由只监听回环地址的本地接收器处理；外网接入必须由用户单独配置安全隧道。

## 12. 可观测性与可靠性

- UI、Go Core 和 Job Runner 使用统一结构化日志和 Trace ID；
- Job、Release、远程命令使用同一个 Correlation ID；
- 记录任务队列长度、任务成功率、构建耗时、发布耗时等本地诊断指标；
- 设置页提供数据库、Docker、Git、SSH、Harbor 和任务系统诊断；
- 远程连接失败采用有限次数指数退避，发布动作不得无限重试；
- 支持手动和定期本地备份，系统钥匙串凭据不随数据库明文导出；
- 任务日志配置保留周期，操作记录保留周期更长；
- 应用重启不丢失任务，远端状态可通过重新巡检恢复。

## 13. 前端信息架构

```text
概览
项目
  项目详情 / 构建 / 制品 / 发布
应用
  受管项目 / 已发现项目 / 状态总览
  应用详情 / 服务状态 / 日志 / 终端 / 发布历史
基础设施
  服务器 / 镜像仓库 / Harbor 代理缓存 / 环境
任务中心
操作记录
系统设置
  凭据 / 本地数据与备份 / 通知 / 应用设置
```

首页重点展示失败任务、异常主机、异常应用和最近发布，而不是只展示静态数量。

## 14. 分阶段实施计划

### Phase 0：工程骨架（约 1 周）

- 初始化 Git、Go、SwiftUI 和本地开发环境；
- 建立桌面桥接、SQLite 迁移、Job Runner、统一配置与日志；
- 验证 macOS 原生目录选择、Keychain、窗口关闭后台驻留、系统托盘、休眠唤醒和任务恢复；
- 完成 CI：lint、test、桌面构建、安装包产物校验；
- 输出 ADR、开发规范、错误码规范和桌面桥接接口基线。

验收：一条命令启动开发应用，可生成首个 macOS 安装包，窗口关闭后任务继续运行，应用重启后本地数据保持完整。

### Phase 1：主机与容器运维 MVP（约 2 周）

- 系统钥匙串、设置页和依赖诊断；
- SSH 凭据、主机接入、host key 验证和连通测试；
- Docker/Compose 环境巡检；
- 发现远端 Compose 项目与容器，展示服务数、健康状态、镜像版本和最后采集时间；
- 容器/项目详情、日志、启动、停止、重启、重新部署和安全删除；
- 期望状态、实际状态、健康状态、同步状态和操作状态聚合；
- Job 队列、日志流和本地操作记录。

验收：可安全接入一台 Linux Docker 主机，正确区分受管项目与已发现项目；断网时保留最后可信状态；所有项目级变更互斥且有完整操作记录。

### Phase 2：项目与镜像构建（约 2～3 周）

- 本地目录和 Git 项目接入；
- Dockerfile 构建配置；
- BuildKit 构建、实时日志、取消和超时；
- Harbor/Registry 连接、推送、制品列表；
- Commit、Build、Digest 的完整追踪关系。

验收：从 Git Commit 构建镜像并推送到 Harbor，页面可查看完整链路和日志。

### Phase 3：发布与回滚（约 3 周）

- Environment、Application、ApplicationConfig；
- 单容器和 Compose 发布；
- 健康检查、失败回滚、手动回滚；
- 环境变量与密钥引用；
- 发布历史、状态同步、并发锁。

验收：选择一个已构建 Digest 发布到远程主机，失败时自动恢复上一成功版本。

### Phase 4：Harbor 与自动化能力（约 2 周）

- Harbor 项目、制品、标签、机器人账号；
- Proxy Cache 配置与使用引导；
- 本地 Webhook 接收器与自动构建规则；
- 桌面、企业微信、钉钉等通知适配器；
- 数据备份恢复与更完整的操作记录查询。

验收：用户能够使用 Harbor 代理缓存、自动触发构建，并完整追踪发布与高风险操作。

### Phase 5：增强能力（按需求）

- 多架构构建与远程 BuildKit Worker；
- 蓝绿或双容器切换策略；
- 常驻主机 Agent；
- Windows/Linux 桌面适配；
- Kubernetes 部署适配器；
- 镜像漏洞扫描结果聚合和策略门禁。

## 15. 测试策略

- 单元测试：领域状态机、危险操作确认、命令参数生成、日志脱敏；
- 状态聚合测试：多服务部分退出、健康检查失败、主机离线、配置漂移和状态过期；
- 集成测试：SQLite 迁移与恢复、任务租约、系统钥匙串、Harbor API Adapter；
- 端到端测试：临时 Git 仓库 + BuildKit + 本地 Registry + Docker 主机；
- 故障测试：SSH 中断、镜像拉取失败、健康检查失败、应用异常退出后重启；
- 安全测试：命令注入、路径穿越、凭据泄漏、本地数据目录权限和终端会话残留；
- 发布前必须覆盖“构建成功但推送失败”“容器启动但健康失败”“回滚本身失败”等路径。

## 16. 首版验收标准

首个正式版本应能完整演示以下闭环：

1. 用户在系统钥匙串中保存 Harbor 凭据和 SSH 凭据；
2. 接入一个 Git 项目和一台 Docker 主机；
3. 从指定 Commit 构建镜像并推送到 Harbor；
4. 使用镜像 Digest 发布单容器或 Compose 应用；
5. 实时查看任务日志、容器日志和健康状态；
6. 查看受管项目和远端已发现项目，并正确识别运行正常、部分异常、停止、漂移、离线和未部署状态；
7. 对部署项目执行启动、停止、重启、重新部署、回滚和安全删除；
8. 发布失败自动回滚，或手动回滚到上一成功版本；
9. 操作记录能够回答何时对哪个环境执行了什么操作以及结果如何；
10. 重启 iDeploy 应用后，项目、任务、发布历史和远端状态均可恢复。

## 17. 已确认决策与默认假设

已确认：

1. iDeploy 是单用户本地桌面应用，不建设多用户、RBAC、审批和服务端部署体系；
2. 本机项目通过系统目录选择器直接接入；
3. macOS 是主要交付平台。

当前默认假设：

1. Apple Silicon 优先，同时保留 Intel 构建能力；
2. 私有 GitLab/Gitea Webhook 自动触发放到 Phase 4；
3. Harbor Proxy Cache 首版先连接和使用，随后再开放创建上游 Registry 与代理项目；
4. 在线交互终端默认关闭，由用户显式开启；
5. 应用关闭窗口后保留后台进程和菜单栏入口，让正在运行的 Job 继续执行。

## 18. 推荐的下一步

按单用户桌面方案开展 Phase 0。工程骨架完成后，优先实现“系统钥匙串 + SQLite Job Runner + 远程主机 + 操作记录”这条底座，因为后续构建、发布、回滚、Harbor 管理都依赖同一套持久化任务和安全模型。
