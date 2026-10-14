# ADR 0002：使用 SwiftUI 实现 macOS 界面

- 状态：已采纳
- 日期：2026-10-12

## 背景

iDeploy 只发布 macOS 应用。为了与其他 Mac 产品在窗口、侧边栏、工具栏、表单和菜单栏行为上保持一致，需要使用系统原生控件。

## 决策

- macOS 窗口与功能页面由 SwiftUI 实现，必要的菜单栏和更新器生命周期使用 AppKit。
- Go 继续负责 Docker、SSH、数据库、发布任务、CLI 和 MCP，不重写已经验证的业务逻辑。
- SwiftUI 通过当前用户专属的 Unix Socket 调用经过白名单限制的 Go 服务方法。请求限制大小，Socket 权限为 0600。
- 应用包内包含 `iDeployUI` 与 `iDeployBackend` 两个 arm64/x86_64 通用可执行文件；前者是应用入口，后者在窗口启动后作为子进程运行。

## 影响

原生构建只需要 Go 与 Xcode，桌面界面不再依赖 WebView。CLI 和 MCP 仍通过同一 Go Runtime 访问现有数据。应用更新框架由 SwiftUI 进程持有，登录启动和终端命令继续指向同一个应用包。
