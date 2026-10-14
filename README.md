# iDeploy

iDeploy 是面向 macOS 的单用户 Docker 交付与远程运维桌面应用。桌面界面使用 SwiftUI，业务核心使用 Go，并具备 SQLite、持久化 Job Runner，以及可执行的 Docker 发布与回滚闭环。

## 当前发布流程

`选择项目 → 构建镜像 → 推送镜像仓库 → 服务器拉取镜像 → 创建或更新容器 / Compose → 健康检查 → 发布记录 → 回滚`

- 支持本地目录，以及通过本机 Git Credential Helper 或 SSH Agent 访问的 Git 项目；
- 单容器发布使用项目 Dockerfile 和目标镜像，运行端口与重启策略保存在部署配置中；端口可只填写外部端口，发布时会根据 Dockerfile 的 `EXPOSE` 自动补全内部端口；
- Compose 发布要求 `image` 使用 `${IMAGE_TAG}`，发布时保存 Compose 快照以保证历史版本可重放；
- Harbor / OCI 用户名保存在 SQLite，密码或 Token 保存在 macOS Keychain；
- “数据库”页可使用独立的只读 PostgreSQL、MySQL / MariaDB 账号查看远程 Schema、表、字段、索引和受限分页数据；连接密码仅保存在 macOS Keychain；
- 镜像仓库登录通过标准输入完成，不把密码写入任务参数或命令行；
- 可配置 iDeploy 专用 BuildKit Builder，将 Docker Hub、GHCR 等基础镜像统一映射到 Harbor Proxy Cache；MCP 发布自动继承全局代理策略；
- MCP 支持幂等配置更新、项目验证、部署配置克隆、发布前检查，以及失败版本按原版本重建或复用已推送 Digest 继续部署；
- 单容器在服务端按 Digest 拉取和校验，再以本地版本 Tag 启动，兼顾确定性发布与可读的 `docker ps` 镜像名称；
- 发布与回滚由服务器级互斥任务执行，完成后核对 Docker 运行状态和容器健康检查。

## Dockerfile 智能生成

- 项目详情页可用内置模板为 Node.js、Go、Python、Maven / Gradle 项目生成 Dockerfile；
- 未识别的项目可在“设置”中配置 DeepSeek API Key，再选择 DeepSeek 生成草案；
- 模型只接收受限的项目清单摘要，敏感字段会脱敏；生成结果必须在应用内预览、编辑并确认后才会写入；
- 已存在的 Dockerfile 不会被覆盖，DeepSeek 不可用时仍可使用内置模板或手动配置。

## 技术栈

- SwiftUI + AppKit（macOS 原生窗口、导航、表单和菜单栏）；
- Go；
- 当前用户专属 Unix Socket 连接 SwiftUI 与 Go Runtime；
- SQLite WAL；
- macOS 系统菜单栏与后台生命周期。
- 可在设置中启用 macOS 登录后启动；启动后菜单栏驻留，本机 MCP 客户端可直接调用发布 Runtime。

## 本地开发

环境要求：Go 1.25+、Xcode 及其命令行工具、Docker、Git 和 SSH。

```bash
bash build/darwin/build-native.sh package
open bin/iDeploy.app
```

运行 `bash build/darwin/build-native.sh universal` 可生成 arm64 与 Intel 通用应用包。原生构建需要 Go 和 Xcode。

在 macOS 上需要调试打包后的应用时，生成本地 `.app`：

```bash
bash build/darwin/build-native.sh universal
open bin/iDeploy.app
```

这会在本机编译 arm64 与 Intel 通用版本并生成 `bin/iDeploy.app`，不需要触发 GitHub Actions。

## 终端调用

安装 `iDeploy.app` 后，在应用的“设置 → 终端集成”中安装 `ideploy` 命令。命令行与桌面端共享项目、服务器、镜像仓库、部署记录和 macOS Keychain 凭据：桌面端运行时通过当前用户专属的 Unix Socket 请求桌面后台执行；桌面端未运行时由 CLI 无界面执行。

```bash
ideploy doctor
ideploy deployment list --json
ideploy deployment publish <deployment-id> \
  --registry <registry-id> \
  --version v1.2.3 \
  --wait \
  --json
```

`--json` 始终返回 `{ "ok": ..., "data": ... }` 或 `{ "ok": false, "code": ..., "error": ... }`，适合其他应用读取。发布、回滚和运维命令默认等待任务结束并用退出码表示最终结果；`--no-wait` 需要 `iDeploy.app` 正在运行，以确保后台任务拥有持续运行的执行器。完整命令见 [`docs/CLI.md`](docs/CLI.md)。

## AI / MCP 调用

iDeploy 同一二进制内置 stdio MCP Server。安装终端命令后，本机 Codex 或其他支持 MCP 的 AI 客户端可以直接调用结构化的查询、发布、回滚、启停和任务工具：

```bash
codex mcp add ideploy -- /usr/local/bin/ideploy mcp
codex mcp list
```

MCP 与 CLI 共享 Runtime Broker：桌面端运行时通过 Unix Socket 执行，桌面端未运行时为单次同步调用启动 Headless Runtime。服务不监听网络端口，也不会向 AI 返回 Registry、SSH 或 Keychain 凭据。工具清单、配置和安全边界见 [`docs/MCP.md`](docs/MCP.md)。

## 验证

```bash
go test -tags native ./...
bash build/darwin/build-native.sh universal
codesign --verify --deep --strict bin/iDeploy.app
```

## GitHub Releases 自动打包

工作流：`.github/workflows/release.yml`。发布任务使用 GitHub 自动提供的 `GITHUB_TOKEN` 创建 Release，并需要一次性配置 `SPARKLE_ED25519_PRIVATE_KEY` Actions Secret 来签名更新包。仓库需允许 GitHub Actions 运行；组织策略不能禁止该任务的 `contents: write` 权限。完整设置见 [自动更新发布说明](docs/AUTO_UPDATE.md)。

先将代码合并/推送到 `main`，再为需要发布的提交创建新标签：

```bash
git switch main
git pull --ff-only
git tag -a v0.2.0 -m "Release v0.2.0"
git push origin v0.2.0
```

推送版本标签后，Actions 自动测试并构建 macOS 13+ Universal 应用（arm64 + Intel x86_64），创建 DMG 安装镜像和 Sparkle 更新 ZIP，并校验两者的 SHA256。Release 附件包括 `iDeploy-v0.2.0-macos-universal.dmg`、`iDeploy-v0.2.0-macos-universal.zip`、`SHA256SUMS.txt` 和签名的 `appcast.xml`。DMG 可将应用拖到 Applications；ZIP 供应用内更新器使用。`v0.2.1-beta.1` 等带后缀的标签自动标记为预发布，不会通过正式版更新源推送；已有 Release 不覆盖，请使用新版本标签。

应用显示版本由标签注入，Info.plist 的 `CFBundleShortVersionString` 使用对应的三段数字版本，`CFBundleVersion` 使用递增的发布构建号。未经过 Apple Developer ID 签名和公证，macOS 可能拦截首次启动；仅在确认下载来源及校验值后按系统安全提示处理，不要关闭全局安全保护。凭据存储当前依赖 macOS 钥匙串，因此暂不发布 Windows/Linux 安装包。

按需触发构建、不发布：进入 **Actions → Release macOS → Run workflow**，填写测试版本（默认 `v0.0.0-test`）。完成后从该运行的 Artifacts 下载 `macos-release`，其中包含 DMG、ZIP、校验文件和 appcast（保留 14 天）。普通代码推送不会触发发布工作流。

注意：这里的标签是 **iDeploy 应用版本**，不是应用内管理的部署项目版本。首次安装带 Sparkle 的版本仍需手动完成；之后可在“设置 → 自动检测更新”控制是否每次启动检查。关闭后仍可手动检查并下载；开启时 Sparkle 会验证更新包，并在应用退出时安装已下载的更新。更新源必须允许未登录的应用访问。

## 目录结构

- `macos/Sources`：SwiftUI 原生窗口和功能页面；
- `main.go`：Go 后台、CLI 和 MCP 入口；
- `native_call.go`：原生界面与 Go 服务之间的调用桥；
- `systemservice.go`：桌面服务接口；
- `internal/job`：SQLite 持久化任务队列；
- `internal/platform`：数据库和本地路径基础设施；
- `migrations`：数据库迁移；
- `cli.go`、`cli_control.go`：终端命令及本地控制通道；
- `mcp.go`：stdio MCP Server、工具 Schema 与业务请求映射；
- `runtime_broker.go`：CLI 与 MCP 共用的桌面/Headless Runtime 调度；
- `docs/PROJECT_PLAN.md`：产品与工程总体规划。

应用数据默认保存在 `~/Library/Application Support/iDeploy/`，日志保存在 `~/Library/Logs/iDeploy/`。
