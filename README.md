# iDeploy

iDeploy 是面向 macOS 的单用户 Docker 交付与远程运维桌面应用。应用已具备 Wails、Go、Vue、SQLite、持久化 Job Runner，以及可执行的 Docker 发布与回滚闭环。

## 当前发布流程

`选择项目 → 构建镜像 → 推送镜像仓库 → 服务器拉取镜像 → 创建或更新容器 / Compose → 健康检查 → 发布记录 → 回滚`

- 支持本地目录，以及通过本机 Git Credential Helper 或 SSH Agent 访问的 Git 项目；
- 单容器发布使用项目 Dockerfile 和目标镜像，运行端口与重启策略保存在部署配置中；端口可只填写外部端口，发布时会根据 Dockerfile 的 `EXPOSE` 自动补全内部端口；
- Compose 发布要求 `image` 使用 `${IMAGE_TAG}`，发布时保存 Compose 快照以保证历史版本可重放；
- Harbor / OCI 用户名保存在 SQLite，密码或 Token 保存在 macOS Keychain；
- 镜像仓库登录通过标准输入完成，不把密码写入任务参数或命令行；
- 发布与回滚由服务器级互斥任务执行，完成后核对 Docker 运行状态和容器健康检查。

## Dockerfile 智能生成

- 项目详情页可用内置模板为 Node.js、Go、Python、Maven / Gradle 项目生成 Dockerfile；
- 未识别的项目可在“设置”中配置 DeepSeek API Key，再选择 DeepSeek 生成草案；
- 模型只接收受限的项目清单摘要，敏感字段会脱敏；生成结果必须在应用内预览、编辑并确认后才会写入；
- 已存在的 Dockerfile 不会被覆盖，DeepSeek 不可用时仍可使用内置模板或手动配置。

## 技术栈

- Wails v3 Beta（已锁定到 `v3.0.0-beta.16`）；
- Go；
- Vue 3 + TypeScript + Vite；
- SQLite WAL；
- macOS 系统菜单栏与后台生命周期。

## 本地开发

环境要求：Go 1.25+、Node.js、Xcode Command Line Tools、Docker、Git 和 SSH。

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.16
cd frontend
npm install
cd ..
wails3 dev
```

如果 `wails3` 不在 `PATH` 中，可直接运行 `$(go env GOPATH)/bin/wails3`。

## 验证

```bash
go test ./...
cd frontend && npm run build
cd .. && wails3 build
```

## 目录

- `main.go`：macOS 窗口、菜单栏与应用入口；
- `systemservice.go`：Vue 可调用的桌面服务接口；
- `internal/job`：SQLite 持久化任务队列；
- `internal/platform`：数据库和本地路径基础设施；
- `migrations`：数据库迁移；
- `frontend`：Vue 桌面界面；
- `docs/PROJECT_PLAN.md`：产品与工程总体规划。

应用数据默认保存在 `~/Library/Application Support/iDeploy/`，日志保存在 `~/Library/Logs/iDeploy/`。
