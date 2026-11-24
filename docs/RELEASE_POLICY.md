# iDeploy 发布策略与验收

iDeploy Runtime 负责发布校验。MCP 工具提供可移植的执行和查询接口，`ideploy_release` Prompt 和 `ideploy-release` 技能说明如何组合这些工具。

## 启用策略

1. 通过 `inspect_deployment_runtime` 读取目标的实际容器、镜像 ID、版本标签和挂载。结合最近成功发布记录核对镜像 Digest。
2. 用 `get_deployment_runtime_policy` 读取当前修订。通过独立的 `update_deployment_runtime_policy` 操作提交期望修订号和完整策略。首次保存时使用 `expected_revision: 0`。
3. 先保存为 `observe` 记录检查。核对实际数据目录和 Docker 卷名后再切换为 `enforce`。共享工作区须使用支持发布策略的共享服务和桌面客户端，策略修改通过服务器租约与发布串行执行。

对于 IFX，先在开发机读取实际运行信息，逐项确认哪些目录需要持久化，同时记录挂载类型、宿主机路径或实际卷名、容器内目标路径和读写模式。生产策略以生产目标的实际观测为准。应用的文件内容和数据库数据通过各自的验证和备份流程确认。

## 策略示例

以下名称和目录均为示例。Compose 的 `service` 使用服务名，`source` 使用 Docker 实际卷名或绝对绑定路径。单容器省略 `services` 和每条规则的 `service`。

```json
{
  "deployment_id": "实际部署 ID",
  "expected_revision": 0,
  "mode": "observe",
  "services": ["api"],
  "mounts": [
    {
      "service": "api",
      "type": "volume",
      "source": "example_data",
      "destination": "/app/data",
      "readOnly": false
    }
  ],
  "directories": [
    {"service": "api", "path": "/app/data", "access": "writable"}
  ]
}
```

数据目录的 `access` 可省略，也可设为 `readable` 或 `writable`。iDeploy 以容器配置的运行用户调用 `test`，请求读写权限时也检查进入目录的权限。容器缺少该工具或 Docker 无法执行检查时会返回未知。检查全程只读取状态。单容器会保留可安全重放的现有持久化挂载；启用强制策略时，登记的挂载进入该次发布快照并用于创建新容器。

## 发布与恢复

1. 使用相同部署、仓库和入口参数执行 `preflight_deployment`，取返回的 `runtimePolicyRevision`。发布时将其作为 `policy_revision` 传给 `publish_deployment`。
2. 提交时生成稳定的 `request_id`。连接中断或等待超时时可用 `get_operation` 查询收据，或用原参数和原请求 ID 获取同一 Job，再用 `get_job` 或 `wait_job` 查看进度。异步提交要求桌面应用运行。
3. 替换前在服务器租约内复核实际挂载和挂载源。Compose 还会校验待部署快照中的服务和挂载，尤其是已登记数据卷的实际身份。替换后按目标 Docker 的本地镜像 ID 验证 Digest，再执行存储和入口验收。
4. 用 `get_release_runtime_verification` 读取有界的发布前后检查摘要。强制策略下的检查失败会阻止继续发布，或触发已有的工作负载恢复流程。挂载恢复覆盖容器连接的数据源；应用运行期间的数据写入由应用和备份机制管理。
5. 已登记策略下的重试和回滚复用原快照。现行强制策略发生变化时，旧策略的发布任务停止，由操作人核对后重新发版。早于策略功能且保存了固定制品的历史回滚，根据当前策略验证旧制品。
