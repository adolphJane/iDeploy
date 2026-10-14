# iDeploy 项目协作指引

本文件适用于整个仓库。iDeploy 是 macOS 13+ 的单用户 Docker 交付与远程运维应用。当前桌面界面位于 `macos/Sources/`，使用 SwiftUI；业务 Runtime、CLI 和 MCP 使用 Go。仓库只保留当前原生界面。

## 代码与数据

- 界面通过 `macos/Sources/NativeBridge.swift` 调用 Go Runtime；服务入口和方法分别在 `native_call.go`、`systemservice.go`。修改跨层接口时同步检查参数、返回值和调用处。
- SQLite 数据结构在 `migrations/`，存取逻辑主要在 `internal/`。修改持久化结构时提供迁移，并考虑已有用户数据；不要只调整新建数据库的行为。
- 发布和回滚依赖持久化 Job Runner、部署配置及历史版本。修改流程时检查失败恢复、幂等性和回滚语义，避免只更新界面状态。
- Registry、SSH、数据库及 AI 凭据等敏感值使用 macOS Keychain。不要将密码、Token、私钥、签名材料或真实密钥写入源码、测试样例、文档或日志；不要将秘密值存入 SQLite。
- 应用数据默认位于 `~/Library/Application Support/iDeploy/`，日志位于 `~/Library/Logs/iDeploy/`。测试和调试时避免清除用户的现有数据。

## 界面

- 修改当前桌面界面时优先编辑 `macos/Sources/`，复用 `DesignSystem.swift` 中的 `DeployStyle`、卡片和按钮样式，以及已有页面模式。保持原生控件的键盘、焦点和辅助功能行为。
- 表单的必填项、默认值及错误提示应与 Go 服务端校验一致；编辑已有连接或配置时，应正确处理可保留的 Keychain 凭据和可选字段。

## macOS UI Development Rules

- 本项目仅面向 macOS；新增 UI 先阅读 `docs/design-system/`。以当前 SwiftUI 工作区的深绿侧栏、浅绿画布、白/淡绿卡片和紧凑列表/详情为视觉基准，不使用 iOS 专属交互模式。
- 优先复用 `DeployStyle`、`PageContainer`、`SectionHeading`、`StateBadge`、`EmptyState`、卡片及按钮样式。`macos/Sources/DesignSystem/` 的 Token 和组件须与当前界面一致，新增组件提供 Preview。
- 优先使用原生 SwiftUI 控件并保留系统键盘、焦点和辅助功能行为；不要未经单页方案确认就把现有手工分栏、标题区或菜单改为另一套导航结构。
- 新视图使用统一的颜色、字级、间距和形状，不在业务 View 中增加另一套硬编码设计语言。当前固定浅色是已知限制；Light/Dark、高对比度和放大文字需逐页成套适配并验证，不得仅凭预览宣称支持。
- 主要流程检查窗口缩放、键盘、鼠标和 VoiceOver。不得为了视觉改造破坏现有业务功能、API、数据结构或发布流程；代表页面改造先提交方案并等待用户确认。

## 验证与构建

- Go 逻辑改动运行相关包测试；跨模块改动或提交前运行 `go test -tags native ./...`。
- SwiftUI 或原生桥接改动在 macOS 上运行 `bash build/darwin/build-native.sh build`，确认 Go 后台和 SwiftUI 都编译成功。需要检查完整应用包时运行 `bash build/darwin/build-native.sh package`，产物为 `bin/iDeploy.app`；`universal` 模式会额外构建 Intel 架构。
- `bin/` 是生成目录，不提交。纯文档改动无需构建应用；报告验证结果时区分已执行的命令和未执行的检查。
- `.github/workflows/ci.yml` 当前运行 Go 测试和原生 macOS 构建；原生应用由 `.github/workflows/release.yml` 在版本标签或手动触发时构建。发布脚本位于 `build/darwin/release.sh`，不要用本地构建成功代替发布产物或 Actions 结果的核验。

## 发布与外部操作

- 不因普通代码修改自动推送、打标签、发布 GitHub Release 或操作远程部署。执行这些操作时遵循用户当前任务和 `docs/AUTO_UPDATE.md`、`docs/CLI.md`、`docs/MCP.md` 中相应流程。
- 终端和 MCP 操作可能影响远程服务器或镜像仓库；明确目标资源和操作范围，保留现有的凭据隔离与确认边界。
