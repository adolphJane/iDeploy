# iDeploy CLI

`ideploy` 用于从终端、IDE 或其他本机应用调用 iDeploy。它与桌面应用使用相同的数据目录和 macOS Keychain 凭据，不需要重复配置。

## 安装

打开 iDeploy 的“设置 → 终端集成”，点击“安装命令”。macOS 会请求管理员授权，并创建：

```text
/usr/local/bin/ideploy -> /Applications/iDeploy.app/Contents/MacOS/iDeploy
```

如果应用安装在其他目录，链接会指向实际位置。移动应用后需要在设置中重新安装命令。
如果 `/usr/local/bin/ideploy` 已经是其他文件或其他程序的链接，iDeploy 会报告冲突，不会覆盖它。

验证：

```bash
ideploy version
ideploy doctor
ideploy mcp
```

## 命令

```text
ideploy version
ideploy doctor

ideploy project list
ideploy server list
ideploy registry list
ideploy deployment list
ideploy deployment get <deployment-id>

ideploy deployment publish <deployment-id> --registry <registry-id> [--version <version>]
ideploy deployment rollback <deployment-id> --release <release-id>
ideploy deployment start <deployment-id>
ideploy deployment stop <deployment-id>
ideploy deployment restart <deployment-id>

ideploy release list --deployment <deployment-id>
ideploy job list
ideploy job get <job-id>
ideploy job wait <job-id>
ideploy job cancel <job-id>
```

`ideploy mcp` 是供 AI 客户端启动的 stdio MCP Server，不是面向人工交互的普通终端命令。配置方式和工具清单见 [`MCP.md`](MCP.md)。

全局参数可以放在命令的任意位置：

| 参数 | 说明 |
| --- | --- |
| `--json` | 输出稳定的 JSON envelope |
| `--quiet` | 成功时不输出内容 |
| `--wait` | 等待异步任务结束，默认开启 |
| `--no-wait` | 任务入队后立即返回；要求桌面应用正在运行 |
| `--timeout 30m` | 设置整个调用的超时 |
| `--no-color` | 禁止 ANSI 颜色输出 |

## 自动化调用

推荐其他应用使用 `--json --wait`。标准输出只包含 JSON；人类可读的启动或参数错误写入标准错误。JSON 格式为：

```json
{
  "ok": true,
  "data": {
    "id": "job-id",
    "status": "succeeded",
    "progress": 100
  }
}
```

错误格式为：

```json
{
  "ok": false,
  "error": "error detail",
  "code": "operation_failed"
}
```

稳定退出码：

| 退出码 | 含义 |
| --- | --- |
| `0` | 成功 |
| `1` | 一般运行错误 |
| `2` | 参数或命令错误 |
| `3` | 资源不存在 |
| `4` | 应用核心忙或后台执行器不可用 |
| `5` | 所需 Keychain 凭据不可用 |
| `7` | 发布等 Job 执行失败 |
| `8` | 超时 |
| `130` | 取消 |

## 并发模型

- 桌面应用运行时，CLI 通过权限为 `0600` 的本地 Unix Socket 调用桌面进程。
- 桌面应用未运行时，CLI 获取应用运行锁并启动一次性的无界面核心。
- 数据库和 Job Runner 同一时刻只有一个拥有者，避免重复消费任务或错误恢复正在运行的发布。
- 本地控制通道不监听 TCP 端口，不提供远程网络访问。
