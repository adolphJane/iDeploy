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
- `list_registries`
- `list_deployments`
- `get_deployment`
- `list_releases`
- `list_jobs`
- `get_job`
- `wait_job`

会改变远程环境的工具：

- `publish_deployment`
- `rollback_deployment`
- `start_deployment`
- `stop_deployment`
- `restart_deployment`
- `cancel_job`

变更类工具都带有 MCP destructive/open-world 标记，并在说明中要求 AI 仅在用户明确要求具体操作后调用。Registry、SSH 和 Keychain 凭据不会作为工具参数或结果返回。

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
