# iDeploy MCP Server

iDeploy 内置本地 MCP Server，供 Codex 和其他支持 stdio MCP 的本机 AI 客户端查询部署配置、执行发布操作并跟踪任务。MCP Server 与桌面应用和 CLI 复用同一个数据库、macOS Keychain、`SystemService` 与持久化 Job Runner。

## 启动方式

MCP Server 编译在 iDeploy 主程序中，不需要安装额外守护进程：

```bash
ideploy mcp
```

该命令使用标准输入和标准输出传输 MCP JSON-RPC 消息。标准输出只包含协议消息；运行日志写入标准错误。

使用 Codex 注册：

```bash
codex mcp add ideploy -- /usr/local/bin/ideploy mcp
codex mcp list
```

其他 MCP 客户端可使用等价配置：

```json
{
  "mcpServers": {
    "ideploy": {
      "command": "/usr/local/bin/ideploy",
      "args": ["mcp"]
    }
  }
}
```

## 工具

只读工具：

- `get_version`
- `doctor`
- `list_projects`
- `list_servers`
- `list_remote_containers`
- `inspect_deployment_runtime`
- `get_container_logs`
- `check_port_availability`
- `list_registries`
- `get_build_proxy_settings`
- `get_operation`
- `list_deployments`
- `get_deployment`
- `preflight_deployment`
- `list_releases`
- `list_jobs`
- `get_job`
- `wait_job`

本地配置写入工具（不会自动发布，也不会修改远程环境）：

- `create_project`
- `update_project`
- `validate_project`
- `create_deployment`
- `update_deployment`
- `clone_deployment`
- `configure_build_proxy`

会改变远程环境的工具：

- `publish_deployment`
- `retry_release`
- `rollback_deployment`
- `start_deployment`
- `stop_deployment`
- `restart_deployment`
- `cancel_job`

会删除本地配置、但不会删除源码、镜像或远程工作负载的工具：

- `delete_project`
- `delete_deployment`

远程变更工具带有 MCP destructive/open-world 标记，并在说明中要求 AI 仅在用户明确要求具体操作后调用；创建和更新配置标记为非破坏性的本地写入。删除工具只删除 iDeploy 本地配置，但仍要求 `confirm=true`。Registry、SSH 和 Keychain 凭据不会作为工具参数或结果返回。

配置写入可提供调用方生成的 `request_id`（8 到 128 个安全字符）。同一个请求不确定是否完成时，必须复用原 `request_id`，不能生成新 ID；iDeploy 会返回已保存的首次结果。收到 `operation_pending` 或本地控制连接超时后，调用 `get_operation` 查询最终状态。

### 不进入主界面完成新项目配置

MCP 客户端可以按以下顺序完成新项目的接入和发布：

1. 调用 `list_servers` 和 `list_registries`，让用户选择已有的目标服务器与镜像仓库。
2. 收集项目来源和构建配置，调用 `create_project`。本地项目使用 `source_kind=local` 并提供 iDeploy 所在机器可访问的绝对路径；Git 项目使用 `source_kind=git` 并提供仓库地址、引用类型和引用值。
3. 使用返回的项目 ID、服务器 ID、工作负载类型与名称调用 `create_deployment`。
4. 使用返回的部署 ID 和仓库 ID 调用 `publish_deployment`。

发布前建议调用 `preflight_deployment`。它会检查项目文件、基础镜像 manifest、构建代理、仓库凭据、SSH/Docker、目标架构、容器名称和远程端口占用，并在端口冲突时返回候选端口。`publish_deployment` 也会在开始构建前再次阻止已知的主机端口冲突。修改现有配置时使用 `update_project` 或 `update_deployment`，未提供的字段保持不变；`validate_project` 可重新扫描本地项目构建文件，`clone_deployment` 可继承现有配置并为另一服务器/工作负载创建无发布历史的新部署。

失败或取消的发布可用 `retry_release` 按原版本重试：构建或推送前失败时会重新构建；已经记录不可变镜像 Digest 或 Compose 快照时，会直接复用该制品继续拉取与远程部署，不重新构建，也不覆盖同一 Tag。成功版本仍不可覆盖。

单容器发布需要项目具有 `dockerfile_path` 和不含 Tag/Digest 的 `image_repository`；本地项目未显式填写 Dockerfile 时会尝试自动检测。Compose 发布需要 `compose_path`，且 Compose 的 `image` 配置应使用 `${IMAGE_TAG}`。构建上下文、Dockerfile 和 Compose 文件路径均相对于项目根目录。

创建本地项目示例：

```json
{
  "name": "orders-api",
  "description": "订单服务",
  "source_kind": "local",
  "local_path": "/absolute/path/to/orders-api",
  "context_path": ".",
  "dockerfile_path": "Dockerfile",
  "image_repository": "registry.example.com/team/orders-api"
}
```

创建 Git 项目示例：

```json
{
  "name": "orders-api",
  "source_kind": "git",
  "repository_url": "git@example.com:team/orders-api.git",
  "ref_kind": "branch",
  "ref_value": "main",
  "context_path": ".",
  "dockerfile_path": "Dockerfile",
  "image_repository": "registry.example.com/team/orders-api"
}
```

创建部署配置示例：

```json
{
  "project_id": "project-id",
  "server_id": "server-id",
  "workload_kind": "container",
  "workload_name": "orders-api",
  "container_ports": ["8080:8080"],
  "restart_policy": "unless-stopped"
}
```

### 统一使用构建镜像代理

`configure_build_proxy` 保存全局 BuildKit 代理策略，之后由桌面端、CLI 或 MCP 发起的发布都会自动继承，无需修改 `publish_deployment` 参数。规则把基础镜像的上游 Registry 映射到 Harbor Proxy Cache 项目：

```json
{
  "mode": "required",
  "rules": [
    {
      "upstream": "docker.io",
      "mirror": "harbor.example.com/proxy-dockerhub"
    },
    {
      "upstream": "ghcr.io",
      "mirror": "harbor.example.com/proxy-ghcr"
    }
  ]
}
```

策略说明：

- `disabled`：不创建专用 Builder，保持 Docker 当前行为。
- `preferred`：优先使用 iDeploy 专用 BuildKit Builder，初始化失败时记录原因并回退默认 Builder。
- `required`：专用 Builder、代理认证或启动失败时终止发布。

代理地址使用 `host/proxy-project` 格式，不包含 `http://`、Tag 或 Digest。iDeploy 会复用相同主机下已保存的 Registry/Keychain 凭据进行登录。Harbor Proxy Cache 项目只能用于基础镜像拉取；发布产物仍需推送到普通 Harbor/OCI 项目。

发布示例参数：

```json
{
  "deployment_id": "deployment-id",
  "registry_id": "registry-id",
  "version": "v1.2.3",
  "wait": true,
  "timeout_seconds": 1800
}
```

`wait` 默认为 `true`。AI 不应在发布调用超时后直接重试，而应先通过 `list_jobs` 或 `get_job` 判断原任务是否已经创建。

## 运行时模型

- 桌面应用运行时，MCP 通过权限为 `0600` 的当前用户 Unix Socket 调用桌面 Runtime。此时允许 `wait=false`，任务由桌面 Job Runner 持续执行。
- 桌面应用未运行时，MCP 为单次工具调用获取运行锁并启动 Headless Runtime，执行完成后关闭。
- 桌面应用未运行时不允许变更操作使用 `wait=false`，避免工具调用返回后失去任务执行器。
- MCP 进程空闲时不持有数据库或 Runtime 锁，因此不会妨碍桌面应用启动。

## 响应与错误

工具成功和业务失败都返回结构化结果：

```json
{
  "ok": true,
  "data": {},
  "message": "Listed iDeploy deployments."
}
```

```json
{
  "ok": false,
  "code": "not_found",
  "message": "deployment not found"
}
```

参数格式错误由 MCP SDK 的输入 Schema 拒绝；运行时业务错误以 `isError: true` 的工具结果返回，使 AI 可以读取稳定错误码。调用超时最大为 86400 秒。

## 当前安全边界

MCP Server 是当前 macOS 用户的本地 stdio 服务，不监听 TCP 端口。它依赖本机进程权限、Unix Socket 权限和已有 Keychain 访问控制。

工具安全标记用于客户端确认和模型决策，不构成独立的服务端人工审批。当前版本适用于用户主动配置并信任的本机 MCP 客户端；正式环境的强制双阶段审批和发布幂等键仍属于后续加固范围。
