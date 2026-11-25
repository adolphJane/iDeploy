import AppKit
import SwiftUI

private enum SettingsSection: String, CaseIterable, Identifiable {
    case app = "应用"
    case terminal = "终端集成"
    case build = "构建与智能"
    case data = "本机数据"
    case shared = "iDeploy 服务"
    case ingress = "应用入口"

    var id: String { rawValue }
    var symbol: String {
        switch self {
        case .app: "app.badge"
        case .terminal: "terminal"
        case .build: "hammer"
        case .data: "internaldrive"
        case .shared: "server.rack"
        case .ingress: "network"
        }
    }
    var subtitle: String {
        switch self {
        case .app: "版本、更新和启动"
        case .terminal: "命令行与 MCP"
        case .build: "构建代理与 AI"
        case .data: "文件、缓存和清理"
        case .shared: "远端构建、共享工作区与 MySQL"
        case .ingress: "域名、HTTPS 与项目路径"
        }
    }
}

struct SettingsPage: View {
    @EnvironmentObject private var model: AppModel
    @State private var section: SettingsSection = .app
    @State private var launch = J(nil)
    @State private var command = J(nil)
    @State private var update = J(nil)
    @State private var ai = J(nil)
    @State private var proxy = J(nil)
    @State private var proxyMode = "disabled"
    @State private var proxyRules = ""
    @State private var apiKey = ""
    @State private var autoUpdate = false
    @State private var cleanupMessage = ""
    @State private var updateMessage = ""
    @State private var proxyMessage = ""
    @State private var aiMessage = ""
    @State private var commandMessage = ""
    @State private var checkingUpdate = false
    @State private var savingProxy = false
    @State private var savingKey = false
    @State private var confirmingServerClear = false
    @AppStorage("sharedServerTargetID") private var sharedServerID = ""
    @AppStorage("sharedServerDatabaseID") private var sharedDatabaseID = ""
    @AppStorage("sharedServerDatabaseName") private var sharedDatabaseName = ""
    @AppStorage("sharedServerSkipTLSVerify") private var sharedSkipTLSVerify = false
    @State private var sharedSchemas: [J] = []
    @State private var sharedPlan = J(nil)
    @State private var sharedStatus = J(nil)
    @State private var sharedJob = J(nil)
    @State private var sharedMessage = ""
    @State private var sharedBusy = false
    @State private var workspaceStatus = J(nil)
    @State private var migrationPreview = J(nil)
    @State private var pairingCode = ""
    @State private var joinCode = ""
    @State private var deviceName = Host.current().localizedName ?? "Mac"
    @State private var sharedConflicts: [J] = []
    @State private var copiedConflictID = ""
    @State private var workspaceAction = ""
    @State private var workspaceFeedback = ""
    @State private var workspaceFailed = false
    @State private var sharedDevices: [J] = []

    var body: some View {
        HStack(spacing: 0) {
            VStack(spacing: 0) {
                HStack {
                    VStack(alignment: .leading, spacing: 3) {
                        Text("设置")
                            .font(.system(size: 16, weight: .bold))
                            .foregroundStyle(DeployStyle.ink)
                        Text("6 个分类")
                            .font(.caption)
                            .foregroundStyle(DeployStyle.muted)
                    }
                    Spacer()
                }
                .padding(14)
                .background(DeployStyle.overviewHeader)
                Rectangle().fill(DeployStyle.line).frame(height: 1)
                VStack(spacing: 5) {
                    ForEach(SettingsSection.allCases) { item in
                        Button { section = item } label: {
                            HStack(spacing: 11) {
                                Image(systemName: item.symbol)
                                    .font(.system(size: 15, weight: .medium))
                                    .foregroundStyle(DeployStyle.green)
                                    .frame(width: 30, height: 30)
                                    .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 8))
                                VStack(alignment: .leading, spacing: 5) {
                                    Text(item.rawValue)
                                        .font(.system(size: 12, weight: .semibold))
                                        .foregroundStyle(DeployStyle.ink)
                                    Text(item.subtitle)
                                        .font(.system(size: 10))
                                        .foregroundStyle(DeployStyle.muted)
                                }
                                Spacer(minLength: 0)
                                if section == item {
                                    Capsule().fill(DeployStyle.greenBright).frame(width: 3, height: 20)
                                }
                            }
                            .padding(.horizontal, 12)
                            .frame(maxWidth: .infinity, minHeight: 56, alignment: .leading)
                            .background(section == item ? DeployStyle.greenSoft : Color.clear,
                                        in: RoundedRectangle(cornerRadius: 10))
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .accessibilityAddTraits(section == item ? .isSelected : [])
                    }
                    Spacer(minLength: 0)
                }
                .padding(10)
                .background(DeployStyle.canvas)
            }
            .frame(minWidth: 240, idealWidth: 280, maxWidth: 330)
            .background(DeployStyle.canvas)
            Rectangle().fill(DeployStyle.line).frame(width: 1)
            PageContainer {
                SectionHeading(section.rawValue, section.subtitle)
                switch section {
                case .app: appSettings
                case .terminal: terminalSettings
                case .build: buildSettings
                case .data: dataSettings
                case .shared: sharedSettings
                case .ingress: InternalIngressSettings()
                }
            }
            .id(section)
        }
        .environment(\.colorScheme, .light)
        .onAppear { focusSharedSettingsIfRequested() }
        .onChange(of: model.openSharedSettings) { requested in
            if requested { focusSharedSettingsIfRequested() }
        }
        .task { await load() }
        .confirmationDialog("清除全部服务器数据？", isPresented: $confirmingServerClear) {
            Button("确认清除", role: .destructive) {
                Task {
                    if let result = await model.perform("ClearServerData") {
                        cleanupMessage = result["detail"].string
                    }
                }
            }
        } message: {
            Text("这会删除本机保存的服务器、应用和数据库实例配置、相关任务与钥匙串凭据，无法撤销。")
        }
    }

    private var appSettings: some View {
        VStack(alignment: .leading, spacing: 16) {
            card("版本与更新", "保持应用为最新版本", symbol: "arrow.triangle.2.circlepath") {
                HStack(spacing: 14) {
                    Image(systemName: "app")
                        .font(.system(size: 24))
                        .foregroundStyle(DeployStyle.green)
                        .frame(width: 48, height: 48)
                        .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 12))
                    VStack(alignment: .leading, spacing: 4) {
                        Text("iDeploy").font(.system(size: 14, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                        Text("当前版本 \(model.overview["version"].string)")
                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    }
                    Spacer()
                    Button(checkingUpdate ? "检查中…" : "检查更新") { checkForUpdates() }
                        .buttonStyle(DeploySecondaryButtonStyle()).disabled(checkingUpdate)
                    if update["updateAvailable"].bool {
                        Button("下载安装") { downloadUpdate() }.buttonStyle(DeployPrimaryButtonStyle())
                    }
                }
                if update["updateAvailable"].bool {
                    status("新版本 \(update["latestVersion"].string) 已发布", success: true)
                } else if !updateMessage.isEmpty {
                    status(updateMessage)
                }
                divider
                settingRow("自动检查更新", detail: "应用会在后台定期检查可用版本") {
                    Toggle("自动检查更新", isOn: $autoUpdate).labelsHidden()
                        .onChange(of: autoUpdate) { enabled in
                            ideploy_set_automatic_update_checks(enabled ? 1 : 0)
                        }
                }
            }
            card("启动行为", "登录 macOS 后自动打开应用", symbol: "power") {
                settingRow("登录后启动", detail: launch["detail"].string) {
                    Toggle("登录后启动", isOn: Binding(
                        get: { launch["enabled"].bool },
                        set: { enabled in
                            Task {
                                launch = await model.perform("SetLaunchAtLogin", [enabled], refreshAfter: false) ?? launch
                            }
                        }
                    ))
                    .labelsHidden()
                    .disabled(!launch["available"].bool || launch["requiresApproval"].bool)
                }
            }
        }
    }

    private var terminalSettings: some View {
        VStack(alignment: .leading, spacing: 16) {
            card("命令行工具", "从终端直接运行 iDeploy 命令", symbol: "terminal") {
                HStack(alignment: .top, spacing: 20) {
                    VStack(alignment: .leading, spacing: 7) {
                        Text("ideploy 命令").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                        Text(command["detail"].string).font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                        if !command["commandPath"].string.isEmpty {
                            Text(command["commandPath"].string)
                                .font(.system(size: 11, design: .monospaced))
                                .foregroundStyle(DeployStyle.ink).textSelection(.enabled)
                        }
                    }
                    Spacer(minLength: 0)
                    Button(command["installed"].bool ? "已安装" : "安装命令") { installCommand() }
                        .buttonStyle(DeploySecondaryButtonStyle())
                        .disabled(command["installed"].bool || command["conflict"].bool)
                }
                if !commandMessage.isEmpty { status(commandMessage) }
            }
            card("MCP 接入", "在本机 AI 客户端中使用 iDeploy", symbol: "point.3.connected.trianglepath.dotted") {
                Text("通过 stdio 连接本机 iDeploy 服务。")
                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                Text("codex mcp add ideploy -- /usr/local/bin/ideploy mcp")
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(DeployStyle.ink).textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(12)
                    .background(DeployStyle.canvas, in: RoundedRectangle(cornerRadius: 9))
            }
        }
    }

    private var buildSettings: some View {
        VStack(alignment: .leading, spacing: 16) {
            card("BuildKit 构建代理", "配置镜像拉取的代理策略和上游映射", symbol: "shippingbox") {
                if !proxy["detail"].string.isEmpty {
                    Text(proxy["detail"].string).font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                }
                HStack {
                    Text("代理策略").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                    Spacer()
                    Picker("代理策略", selection: $proxyMode) {
                        Text("停用").tag("disabled")
                        Text("优先代理").tag("preferred")
                        Text("强制代理").tag("required")
                    }
                    .labelsHidden().frame(width: 170)
                }
                divider
                VStack(alignment: .leading, spacing: 7) {
                    Text("上游映射").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                    Text("每行填写 upstream=host/proxy-project")
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    TextEditor(text: $proxyRules)
                        .font(.system(size: 12, design: .monospaced))
                        .scrollContentBackground(.hidden)
                        .frame(height: 96).padding(8)
                        .background(Color.white, in: RoundedRectangle(cornerRadius: 9))
                        .overlay(RoundedRectangle(cornerRadius: 9).strokeBorder(DeployStyle.line))
                }
                HStack {
                    if !proxyMessage.isEmpty { status(proxyMessage) }
                    Spacer()
                    Button(savingProxy ? "保存中…" : "保存代理配置") { saveProxy() }
                        .buttonStyle(DeployPrimaryButtonStyle()).disabled(savingProxy)
                }
            }
            card("Dockerfile 智能生成", "管理 DeepSeek 服务凭据", symbol: "sparkles") {
                VStack(alignment: .leading, spacing: 4) {
                    Text("DeepSeek API Key").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                    Text(ai["deepSeekConfigured"].bool ? "已保存在 macOS 钥匙串" : "尚未配置")
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                }
                HStack(spacing: 10) {
                    SecureField("输入新的 API Key", text: $apiKey).textFieldStyle(.roundedBorder)
                    Button("保存") { saveKey(apiKey) }
                        .buttonStyle(DeployPrimaryButtonStyle()).disabled(apiKey.isEmpty || savingKey)
                    if ai["deepSeekConfigured"].bool {
                        Button("清除") { saveKey("") }
                            .buttonStyle(SettingsDangerButtonStyle()).disabled(savingKey)
                    }
                }
                if !aiMessage.isEmpty { status(aiMessage) }
            }
        }
    }

    private var dataSettings: some View {
        VStack(alignment: .leading, spacing: 16) {
            card("本机文件", "iDeploy 数据在这台 Mac 上的保存位置", symbol: "internaldrive") {
                pathRow("数据库", model.overview["databasePath"].string)
                divider
                pathRow("缓存", model.overview["cachePath"].string)
                divider
                pathRow("日志", model.overview["logPath"].string)
            }
            card("数据维护", "管理运行缓存和服务器配置", symbol: "externaldrive.badge.minus") {
                HStack(spacing: 12) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("运行缓存").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                        Text("清理构建和运行过程中生成的临时文件")
                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    }
                    Spacer()
                    Button("清理运行缓存") { clearCache() }.buttonStyle(DeploySecondaryButtonStyle())
                }
                divider
                HStack(spacing: 12) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("服务器数据").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                        Text("删除本机保存的服务器、部署及相关凭据")
                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    }
                    Spacer()
                    Button("清除服务器数据") { confirmingServerClear = true }
                        .buttonStyle(SettingsDangerButtonStyle())
                }
                if !cleanupMessage.isEmpty { status(cleanupMessage) }
            }
        }
    }

    private var sharedSettings: some View {
        VStack(alignment: .leading, spacing: 16) {
            card("iDeploy 服务远程部署", "安装服务与独立构建执行器到已连接服务器", symbol: "server.rack") {
                Picker("目标服务器", selection: $sharedServerID) {
                    Text("选择服务器").tag("")
                    ForEach(model.servers, id: \.id) { item in Text(item["name"].string).tag(item.id) }
                }
                .onChange(of: sharedServerID) { _ in
                    sharedPlan = J(nil); sharedStatus = J(nil)
                    if !sharedServerID.isEmpty { inspectSharedServer() }
                }
                Picker("远程 MySQL", selection: $sharedDatabaseID) {
                    Text("选择 MySQL 连接").tag("")
                    ForEach(model.databases.filter { $0["driver"].string == "mysql" }, id: \.id) { item in
                        Text(item["name"].string + " · " + item["host"].string).tag(item.id)
                    }
                }
                .onChange(of: sharedDatabaseID) { _ in
                    sharedPlan = J(nil)
                    sharedSchemas = []
                    sharedDatabaseName = model.databases.first(where: { $0.id == sharedDatabaseID })?["databaseName"].string ?? ""
                    loadSharedSchemas()
                }
                HStack {
                    TextField("共享服务数据库名，例如 ideploy_shared", text: $sharedDatabaseName)
                        .textFieldStyle(.roundedBorder)
                        .onChange(of: sharedDatabaseName) { _ in sharedPlan = J(nil) }
                    Button("读取数据库列表") { loadSharedSchemas() }
                        .buttonStyle(DeploySecondaryButtonStyle())
                        .disabled(sharedDatabaseID.isEmpty || sharedBusy)
                }
                if !sharedSchemas.isEmpty {
                    Menu("从已有数据库选择") {
                        ForEach(sharedSchemas.indices, id: \.self) { index in
                            let item = sharedSchemas[index]
                            Button(item["name"].string) { sharedDatabaseName = item["name"].string }
                        }
                    }
                    .buttonStyle(DeploySecondaryButtonStyle())
                }
                Text("数据库名可从列表选择，也可手动填写；尚未创建时可先到“数据库 → 浏览数据”创建。账号需有建表和读写权限，并允许目标服务器通过 TLS 连接。服务仅监听服务器本机 127.0.0.1:8765。")
                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                Toggle("跳过 MySQL 服务器证书验证", isOn: $sharedSkipTLSVerify)
                    .onChange(of: sharedSkipTLSVerify) { _ in sharedPlan = J(nil) }
                if sharedSkipTLSVerify {
                    Text("仍使用 TLS 加密，但无法确认连接的数据库服务器身份。适用于当前自动生成证书无法通过校验的 MySQL；建议后续更换标准证书。")
                        .font(.system(size: 11)).foregroundStyle(.orange)
                }
                HStack(spacing: 10) {
                    Button("检查服务器状态") { inspectSharedServer() }
                        .buttonStyle(DeploySecondaryButtonStyle()).disabled(sharedServerID.isEmpty || sharedBusy)
                    Button("发布前预检") { planSharedServer() }
                        .buttonStyle(DeploySecondaryButtonStyle()).disabled(sharedServerID.isEmpty || sharedDatabaseID.isEmpty || sharedDatabaseName.isEmpty || sharedBusy)
                    if sharedBusy { ProgressView().controlSize(.small) }
                }
                if !sharedStatus.isNull {
                    HStack(spacing: 8) {
                        AppStatusBadge(title: sharedStatus["dockerReady"].bool ? "服务器 Docker 可用" : sharedStatus["dockerInstalled"].bool ? "Docker 引擎不可用" : "未安装 Docker", tone: sharedStatus["dockerReady"].bool ? .success : .warning)
                        if sharedStatus["installed"].bool {
                            AppStatusBadge(title: sharedStatus["workerRunning"].bool ? "构建执行器运行中" : "构建执行器未运行", tone: sharedStatus["workerRunning"].bool ? .success : .warning)
                        }
                    }
                    if !sharedStatus["dockerReady"].bool {
                        Text(sharedStatus["dockerDetail"].string)
                            .font(.system(size: 11)).foregroundStyle(.orange).textSelection(.enabled)
                    }
                    if sharedStatus["installed"].bool {
                        HStack(spacing: 8) {
                            AppStatusBadge(title: sharedStatus["updateAvailable"].bool ? "有可用更新" : "已是最新版本", tone: sharedStatus["updateAvailable"].bool ? .warning : .success)
                            Text("当前：\(sharedStatus["imageTag"].string.replacingOccurrences(of: "ideploy/shared-server:", with: "")) · 本机：\(sharedStatus["latestImageTag"].string.replacingOccurrences(of: "ideploy/shared-server:", with: ""))")
                                .font(.system(size: 11, design: .monospaced))
                                .foregroundStyle(DeployStyle.muted)
                                .textSelection(.enabled)
                        }
                    }
                    Text(sharedStatus["detail"].string).font(.system(size: 11)).foregroundStyle(DeployStyle.muted).textSelection(.enabled)
                }
                if !sharedPlan.isNull {
                    Text(sharedPlan["detail"].string).font(.system(size: 11)).foregroundStyle(DeployStyle.muted).textSelection(.enabled)
                    if sharedPlan["canDeploy"].bool {
                        Button(sharedPlan["repair"].bool ? "修复并重新部署" : sharedPlan["upgrade"].bool ? "更新 iDeploy 服务" : "部署 iDeploy 服务") { deploySharedServer() }
                            .buttonStyle(DeployPrimaryButtonStyle()).disabled(sharedBusy)
                    }
                }
                if !sharedJob.isNull {
                    ProgressView(value: Double(sharedJob["progress"].int), total: 100)
                    Text(sharedJob["message"].string).font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                }
                if !sharedMessage.isEmpty { Text(sharedMessage).font(.system(size: 11)).foregroundStyle(.orange).textSelection(.enabled) }
            }
            card("共享工作区", "首次迁移、设备配对与持续同步", symbol: "arrow.triangle.2.circlepath") {
                HStack(spacing: 10) {
                    Text(workspaceStatus["mode"].string == "shared" ? "已加入共享工作区" : "本机工作区")
                        .font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                    Spacer()
                    Button(workspaceAction == "status" ? "刷新中…" : "刷新状态") { loadWorkspaceStatus() }
                        .buttonStyle(SharedWorkspaceButtonStyle()).disabled(!workspaceAction.isEmpty)
                }
                if !workspaceAction.isEmpty { ProgressView().controlSize(.small).accessibilityLabel("共享工作区正在执行操作") }
                if !workspaceFeedback.isEmpty {
                    Text(workspaceFeedback).font(.system(size: 11))
                        .foregroundStyle(workspaceFailed ? Color.orange : DeployStyle.green)
                        .textSelection(.enabled)
                }
                if workspaceStatus["mode"].string == "shared" {
                    Text("已同步游标：\(workspaceStatus["cursor"].int) · 上次拉取：\(SharedSyncDateText.localDateTime(workspaceStatus["lastSyncAt"].string))")
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    if !workspaceStatus["unlocked"].bool {
                        Text("共享凭据暂不可用，请检查共享服务连接后刷新状态。")
                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    }
                    HStack(spacing: 10) {
                        Button(workspaceAction == "sync" ? "同步中…" : "立即同步") { syncWorkspace() }
                            .buttonStyle(SharedWorkspaceButtonStyle()).disabled(!workspaceAction.isEmpty || !workspaceStatus["unlocked"].bool)
                        Button(workspaceAction == "pair" ? "生成中…" : "生成设备配对码") { createPairingCode() }
                            .buttonStyle(SharedWorkspaceButtonStyle()).disabled(!workspaceAction.isEmpty || !workspaceStatus["unlocked"].bool)
                    }
                    if !pairingCode.isEmpty {
                        Text("10 分钟内在新设备输入配对码：\(pairingCode)")
                            .font(.system(size: 11)).textSelection(.enabled)
                    }
                    HStack {
                        Text("已接入设备").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                        Spacer()
                        Button(workspaceAction == "devices" ? "读取中…" : "刷新设备") { loadSharedDevices() }
                            .buttonStyle(SharedWorkspaceButtonStyle()).disabled(!workspaceAction.isEmpty || !workspaceStatus["unlocked"].bool)
                    }
                    ForEach(sharedDevices, id: \.id) { device in
                        HStack {
                            Text(device["name"].string + (device["id"].string == workspaceStatus["deviceId"].string ? "（本机）" : ""))
                                .font(.system(size: 11)).foregroundStyle(DeployStyle.ink)
                            Spacer()
                            if device["revoked"].bool {
                                Text("已撤销").font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                            } else if device["id"].string != workspaceStatus["deviceId"].string {
                                Button("撤销") { revokeSharedDevice(device["id"].string) }
                                    .buttonStyle(SharedWorkspaceButtonStyle()).disabled(!workspaceAction.isEmpty || !workspaceStatus["unlocked"].bool)
                            }
                        }
                    }
                    if workspaceStatus["pending"].int > 0 || workspaceStatus["conflicts"].int > 0 {
                        Text("待同步 \(workspaceStatus["pending"].int) 项 · 冲突 \(workspaceStatus["conflicts"].int) 项")
                            .font(.system(size: 11)).foregroundStyle(.orange)
                        Button(workspaceAction == "conflicts" ? "读取中…" : "查看冲突") { loadSharedConflicts() }
                            .buttonStyle(SharedWorkspaceButtonStyle()).disabled(!workspaceAction.isEmpty || !workspaceStatus["unlocked"].bool)
                    }
                    ForEach(sharedConflicts, id: \.id) { conflict in
                        VStack(alignment: .leading, spacing: 8) {
                            Text(conflict["error"].string).font(.system(size: 11)).foregroundStyle(.orange)
                            ForEach(conflict["items"].array, id: \.id) { item in
                                Text(item["kind"].string + " · " + item["id"].string)
                                    .font(.system(size: 11, weight: .semibold))
                                Text("本机：" + sharedJSON(item["local"])).font(.system(size: 10)).textSelection(.enabled)
                                Text("远端：" + sharedJSON(item["remote"])).font(.system(size: 10)).textSelection(.enabled)
                            }
                            HStack {
                                Button(copiedConflictID == conflict.id ? "已复制" : "复制冲突详情") { copySharedConflict(conflict) }
                                Button("保留远端") { resolveSharedConflict(conflict.id, choice: "remote") }
                                    .disabled(!workspaceAction.isEmpty || !workspaceStatus["unlocked"].bool)
                                Button("保留本机") { resolveSharedConflict(conflict.id, choice: "local") }
                                    .disabled(!workspaceAction.isEmpty || !workspaceStatus["unlocked"].bool)
                            }
                            .buttonStyle(SharedWorkspaceButtonStyle())
                        }
                        .padding(12)
                        .deployCard(fill: DeployStyle.overviewPanel)
                    }
                } else {
                    Text("首次迁移会同步本机配置；SSH 私钥文件和本地源码目录不会上传。已有共享数据的设备请使用配对码加入。")
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    HStack(spacing: 10) {
                        Button("预览本机迁移") { previewMigration() }
                            .buttonStyle(SharedWorkspaceButtonStyle()).disabled(sharedServerID.isEmpty || !workspaceAction.isEmpty)
                        if !migrationPreview.isNull {
                        Button(workspaceStatus["mode"].string == "preparing" ? "继续上次迁移" : "迁移到共享服务") { migrateWorkspace() }
                                .buttonStyle(SharedWorkspaceButtonStyle(primary: true))
                                .disabled(!workspaceAction.isEmpty || !migrationPreview["blockers"].array.isEmpty)
                        }
                    }
                    if !migrationPreview.isNull {
                        Text("将迁移 \(migrationPreview["totalRecords"].int) 条配置及 \(migrationPreview["secretCount"].int) 项加密凭据")
                            .font(.system(size: 11)).foregroundStyle(DeployStyle.ink)
                        ForEach(migrationPreview["localPaths"].array.indices, id: \.self) { index in
                            Text(migrationPreview["localPaths"][index].string).font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                        }
                        ForEach(migrationPreview["blockers"].array.indices, id: \.self) { index in
                            Text(migrationPreview["blockers"][index].string).font(.system(size: 11)).foregroundStyle(.orange)
                        }
                    }
                    divider
                    Text("从其他设备加入").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                    TextField("一次性配对码", text: $joinCode).textFieldStyle(.roundedBorder)
                    TextField("此设备名称", text: $deviceName).textFieldStyle(.roundedBorder)
                    Button("加入共享工作区") { joinWorkspace() }
                        .buttonStyle(SharedWorkspaceButtonStyle())
                        .disabled(!workspaceAction.isEmpty || sharedServerID.isEmpty || joinCode.isEmpty)
                }
            }
        }
        .onAppear {
            if sharedDatabaseName.isEmpty {
                sharedDatabaseName = model.databases.first(where: { $0.id == sharedDatabaseID })?["databaseName"].string ?? ""
            }
            if sharedSchemas.isEmpty && !sharedDatabaseID.isEmpty { loadSharedSchemas() }
            if !sharedServerID.isEmpty { inspectSharedServer() }
            loadWorkspaceStatus()
        }
    }

    private func focusSharedSettingsIfRequested() {
        if model.openSharedSettings || (model.sharedMode == "shared" && !model.sharedUnlocked) {
            section = .shared
            model.openSharedSettings = false
        }
    }

    private func inspectSharedServer() {
        sharedBusy = true; sharedMessage = ""
        Task { defer { sharedBusy = false }; do { sharedStatus = try await NativeBridge.call("GetSharedServerStatus", [sharedServerID], timeoutSeconds: 25) } catch { sharedMessage = error.localizedDescription } }
    }

    private func sharedJSON(_ value: J) -> String {
        if value.isNull { return "已删除" }
        guard JSONSerialization.isValidJSONObject(value.raw),
              let data = try? JSONSerialization.data(withJSONObject: value.raw, options: [.sortedKeys]),
              let text = String(data: data, encoding: .utf8) else { return value.string }
        return text
    }

    private func copySharedConflict(_ conflict: J) {
        var lines = ["共享工作区冲突", "请求 ID：\(conflict.id)", "错误：\(conflict["error"].string)"]
        for item in conflict["items"].array {
            lines.append("\n\(item["kind"].string) · \(item["id"].string)")
            lines.append("本机：\(sharedJSON(item["local"]))")
            lines.append("远端：\(sharedJSON(item["remote"]))")
        }
        if conflict["hasSecrets"].bool { lines.append("\n包含加密凭据变更；凭据内容未复制。") }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(lines.joined(separator: "\n"), forType: .string)
        copiedConflictID = conflict.id
    }

    private func loadWorkspaceStatus() {
        guard workspaceAction.isEmpty else { return }
        workspaceAction = "status"; workspaceFeedback = "正在读取共享工作区状态…"; workspaceFailed = false
        Task {
            defer { workspaceAction = "" }
            do {
                workspaceStatus = try await NativeBridge.call("GetSharedWorkspaceStatus")
                workspaceFeedback = workspaceStatus["mode"].string == "shared" ? "状态已更新" : "当前使用本机工作区"
                if workspaceStatus["mode"].string == "shared" && !model.sharedSyncError.isEmpty {
                    workspaceFeedback = model.sharedSyncError
                    workspaceFailed = true
                }
                if workspaceStatus["mode"].string == "shared" && workspaceStatus["unlocked"].bool {
                    sharedDevices = try await NativeBridge.call("ListSharedDevices", timeoutSeconds: 35).array
                }
            }
            catch { workspaceFeedback = error.localizedDescription; workspaceFailed = true }
        }
    }

    private func loadSharedDevices() {
        guard workspaceAction.isEmpty && workspaceStatus["unlocked"].bool else { return }
        workspaceAction = "devices"; workspaceFeedback = "正在读取设备…"; workspaceFailed = false
        Task {
            defer { workspaceAction = "" }
            do {
                sharedDevices = try await NativeBridge.call("ListSharedDevices", timeoutSeconds: 35).array
                workspaceFeedback = "设备列表已更新"
            } catch { workspaceFeedback = error.localizedDescription; workspaceFailed = true }
        }
    }

    private func revokeSharedDevice(_ id: String) {
        guard workspaceAction.isEmpty else { return }
        workspaceAction = "revoke"; workspaceFeedback = "正在撤销设备…"; workspaceFailed = false
        Task { defer { workspaceAction = "" }
            do {
                _ = try await NativeBridge.call("RevokeSharedDevice", [id], timeoutSeconds: 35)
                sharedDevices = try await NativeBridge.call("ListSharedDevices").array
                workspaceFeedback = "设备已撤销"
            } catch { workspaceFeedback = error.localizedDescription; workspaceFailed = true }
        }
    }

    private func previewMigration() {
        guard workspaceAction.isEmpty else { return }
        workspaceAction = "preview"; workspaceFeedback = "正在生成迁移预览…"; workspaceFailed = false; migrationPreview = J(nil)
        Task { defer { workspaceAction = "" }
            do {
                migrationPreview = try await NativeBridge.call("PreviewSharedWorkspaceMigration", [sharedServerID], timeoutSeconds: 45)
                workspaceFeedback = "迁移预览已更新"
            } catch { workspaceFeedback = error.localizedDescription; workspaceFailed = true }
        }
    }

    private func migrateWorkspace() {
        guard workspaceAction.isEmpty else { return }
        workspaceAction = "migrate"; workspaceFeedback = "正在备份并迁移本机数据…"; workspaceFailed = false
        Task { defer { workspaceAction = "" }
            do {
                let result = try await NativeBridge.call("StartSharedWorkspaceMigration", [sharedServerID], timeoutSeconds: 300)
                migrationPreview = J(nil)
                workspaceStatus = result["status"]
                workspaceFeedback = "已迁移 \(result["count"].int) 条配置"
                await model.refresh()
            } catch { workspaceFeedback = error.localizedDescription; workspaceFailed = true }
        }
    }

    private func syncWorkspace() {
        guard workspaceAction.isEmpty else { return }
        workspaceAction = "sync"; workspaceFeedback = "正在同步共享工作区…"; workspaceFailed = false
        Task { defer { workspaceAction = "" }
            do {
                workspaceStatus = try await NativeBridge.call("ReconcileSharedWorkspace", timeoutSeconds: 180)
                await model.refresh()
                if model.sharedSyncError.isEmpty {
                    workspaceFeedback = "共享工作区已同步"
                } else {
                    workspaceFeedback = model.sharedSyncError
                    workspaceFailed = true
                }
            } catch { workspaceFeedback = error.localizedDescription; workspaceFailed = true }
        }
    }

    private func createPairingCode() {
        guard workspaceAction.isEmpty else { return }
        workspaceAction = "pair"; workspaceFeedback = "正在生成设备配对码…"; workspaceFailed = false
        Task { defer { workspaceAction = "" }
            do {
                pairingCode = try await NativeBridge.call("NewSharedPairingCode", timeoutSeconds: 35)["code"].string
                workspaceFeedback = "配对码已生成，10 分钟内有效"
            } catch { workspaceFeedback = error.localizedDescription; workspaceFailed = true }
        }
    }

    private func joinWorkspace() {
        guard workspaceAction.isEmpty else { return }
        workspaceAction = "join"; workspaceFeedback = "正在通过 SSH 隧道加入共享工作区…"; workspaceFailed = false
        Task { defer { workspaceAction = "" }
            do {
                let result = try await NativeBridge.call("JoinSharedWorkspace", [sharedServerID, joinCode, deviceName], timeoutSeconds: 300)
                workspaceStatus = result["status"]
                joinCode = ""
                workspaceFeedback = "已加入共享工作区，同步 \(result["count"].int) 条配置。请选择本机 SSH 私钥和项目目录。"
                await model.refresh()
            } catch { workspaceFeedback = error.localizedDescription; workspaceFailed = true }
        }
    }

    private func loadSharedConflicts() {
        guard workspaceAction.isEmpty else { return }
        workspaceAction = "conflicts"; workspaceFeedback = "正在读取冲突…"; workspaceFailed = false
        Task { defer { workspaceAction = "" }
            do {
                sharedConflicts = try await NativeBridge.call("ListSharedConflicts", timeoutSeconds: 60).array
                workspaceFeedback = sharedConflicts.isEmpty ? "当前没有冲突" : "已读取 \(sharedConflicts.count) 条冲突"
                workspaceStatus = try await NativeBridge.call("GetSharedWorkspaceStatus")
            } catch { workspaceFeedback = error.localizedDescription; workspaceFailed = true }
        }
    }

    private func resolveSharedConflict(_ id: String, choice: String) {
        guard workspaceAction.isEmpty else { return }
        workspaceAction = "resolve"; workspaceFeedback = "正在处理冲突…"; workspaceFailed = false
        Task { defer { workspaceAction = "" }
            do {
                _ = try await NativeBridge.call("ResolveSharedConflict", [id, choice], timeoutSeconds: 90)
                sharedConflicts = try await NativeBridge.call("ListSharedConflicts").array
                workspaceStatus = try await NativeBridge.call("GetSharedWorkspaceStatus")
                await model.refresh()
                workspaceFeedback = "冲突已处理"
            } catch { workspaceFeedback = error.localizedDescription; workspaceFailed = true }
        }
    }

    private func planSharedServer() {
        sharedBusy = true; sharedMessage = ""
        Task { defer { sharedBusy = false }; do { sharedPlan = try await NativeBridge.call("PlanSharedServerDeployment", [sharedServerID, sharedDatabaseID, sharedDatabaseName, sharedSkipTLSVerify], timeoutSeconds: 45) } catch { sharedMessage = error.localizedDescription } }
    }

    private func loadSharedSchemas() {
        guard !sharedDatabaseID.isEmpty else { return }
        sharedBusy = true
        Task { defer { sharedBusy = false }; do {
            sharedSchemas = try await NativeBridge.call("ListDatabaseSchemas", [sharedDatabaseID], timeoutSeconds: 25).array
            sharedMessage = ""
        } catch { sharedMessage = "读取数据库列表失败：" + error.localizedDescription } }
    }

    private func deploySharedServer() {
        sharedBusy = true; sharedMessage = ""
        Task { defer { sharedBusy = false }; do {
            let job = try await NativeBridge.call("DeploySharedServer", [sharedServerID, sharedDatabaseID, sharedDatabaseName, sharedSkipTLSVerify], timeoutSeconds: 45)
            sharedPlan = J(nil)
            for _ in 0..<150 {
                try? await Task.sleep(nanoseconds: 2_000_000_000)
                sharedJob = try await NativeBridge.call("GetJob", [job.id])
                if sharedJob["status"].string == "succeeded" {
                    sharedMessage = "共享服务部署成功，远程 MySQL 健康检查通过。"
                    sharedStatus = try await NativeBridge.call("GetSharedServerStatus", [sharedServerID], timeoutSeconds: 25)
                    return
                }
                if ["failed", "canceled"].contains(sharedJob["status"].string) {
                    sharedMessage = sharedJob["error"].string.isEmpty ? "部署失败，请查看任务详情。" : sharedJob["error"].string
                    return
                }
            }
            sharedMessage = "部署仍在执行，可稍后刷新状态。"
        } catch { sharedMessage = error.localizedDescription } }
    }

    private func card<Content: View>(_ title: String, _ subtitle: String, symbol: String,
                                     @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack(spacing: 11) {
                Image(systemName: symbol)
                    .font(.system(size: 16, weight: .medium)).foregroundStyle(DeployStyle.green)
                    .frame(width: 34, height: 34)
                    .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 9))
                VStack(alignment: .leading, spacing: 4) {
                    Text(title).font(.system(size: 14, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                    Text(subtitle).font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                }
                Spacer(minLength: 0)
            }
            divider
            content()
        }
        .padding(18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .deployCard(fill: DeployStyle.overviewPanel)
    }

    private var divider: some View { Rectangle().fill(DeployStyle.line).frame(height: 1) }

    private func settingRow<Content: View>(_ title: String, detail: String,
                                           @ViewBuilder control: () -> Content) -> some View {
        HStack(spacing: 14) {
            VStack(alignment: .leading, spacing: 4) {
                Text(title).font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                if !detail.isEmpty { Text(detail).font(.system(size: 11)).foregroundStyle(DeployStyle.muted) }
            }
            Spacer(minLength: 0)
            control()
        }
    }

    private func pathRow(_ title: String, _ path: String) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(title).font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
            Text(path.isEmpty ? "尚无路径信息" : path)
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(DeployStyle.muted).textSelection(.enabled)
        }
    }

    private func status(_ message: String, success: Bool = false) -> some View {
        Label(message, systemImage: success ? "checkmark.circle.fill" : "info.circle")
            .font(.system(size: 11))
            .foregroundStyle(success ? DeployStyle.green : DeployStyle.muted)
            .textSelection(.enabled)
    }

    private func checkForUpdates() {
        Task {
            checkingUpdate = true
            defer { checkingUpdate = false }
            if let result = await model.perform("GetAppUpdateStatus", refreshAfter: false) {
                update = result
                updateMessage = result["updateAvailable"].bool ? "" : "当前已是最新版本"
            }
        }
    }

    private func downloadUpdate() {
        guard ideploy_check_for_updates() == 0 else { return }
        Task { _ = await model.perform("DownloadAppUpdate", [update["downloadURL"].string], refreshAfter: false) }
    }

    private func installCommand() {
        Task {
            if let result = await model.perform("InstallCommandLineTool", refreshAfter: false) {
                command = result
                commandMessage = result["detail"].string
            }
        }
    }

    private func saveProxy() {
        Task {
            savingProxy = true
            defer { savingProxy = false }
            let rules = proxyRules.split(separator: "\n").compactMap { line -> [String: String]? in
                let pair = line.split(separator: "=", maxSplits: 1)
                guard pair.count == 2 else { return nil }
                return ["upstream": String(pair[0]).trimmingCharacters(in: .whitespaces),
                        "mirror": String(pair[1]).trimmingCharacters(in: .whitespaces)]
            }
            if let result = await model.perform("SaveBuildProxySettings",
                                                [["mode": proxyMode, "rules": rules]], refreshAfter: false) {
                proxy = result
                proxyMessage = "代理配置已保存"
            }
        }
    }

    private func saveKey(_ value: String) {
        Task {
            savingKey = true
            defer { savingKey = false }
            if await model.perform("SaveDeepSeekAPIKey", [value], refreshAfter: false) != nil {
                apiKey = ""
                ai = await model.perform("GetAISettings", refreshAfter: false) ?? ai
                aiMessage = value.isEmpty ? "API Key 已清除" : "API Key 已保存到钥匙串"
            }
        }
    }

    private func clearCache() {
        Task {
            if let result = await model.perform("ClearRuntimeCache") {
                cleanupMessage = result["detail"].string
            }
        }
    }

    private func load() async {
        launch = await model.perform("GetLaunchAtLoginStatus", refreshAfter: false) ?? J(nil)
        command = await model.perform("GetCommandLineToolStatus", refreshAfter: false) ?? J(nil)
        ai = await model.perform("GetAISettings", refreshAfter: false) ?? J(nil)
        proxy = await model.perform("GetBuildProxySettings", refreshAfter: false) ?? J(nil)
        proxyMode = proxy["mode"].string.isEmpty ? "disabled" : proxy["mode"].string
        proxyRules = proxy["rules"].array.map { "\($0["upstream"].string)=\($0["mirror"].string)" }.joined(separator: "\n")
        autoUpdate = ideploy_automatic_update_checks() != 0
    }
}

private struct SettingsDangerButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 12, weight: .medium))
            .foregroundStyle(Color.red.opacity(0.82))
            .padding(.horizontal, 12)
            .frame(height: 32)
            .background(configuration.isPressed ? Color.red.opacity(0.11) : Color.white,
                        in: RoundedRectangle(cornerRadius: 8))
            .overlay(RoundedRectangle(cornerRadius: 8)
                .strokeBorder(Color.red.opacity(0.20), lineWidth: 1))
    }
}

private struct SharedWorkspaceButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled
    var primary = false

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 12, weight: primary ? .semibold : .medium))
            .foregroundStyle(isEnabled ? (primary ? Color.white : DeployStyle.green) : DeployStyle.muted.opacity(0.75))
            .padding(.horizontal, primary ? 15 : 12)
            .frame(height: primary ? 34 : 32)
            .background(isEnabled ? (primary ? DeployStyle.green : configuration.isPressed ? DeployStyle.greenSoft : Color.white) : DeployStyle.overviewPanel,
                        in: RoundedRectangle(cornerRadius: primary ? 9 : 8, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: primary ? 9 : 8, style: .continuous)
                    .strokeBorder(isEnabled ? DeployStyle.line : DeployStyle.line.opacity(0.55), lineWidth: 1)
            }
            .opacity(isEnabled ? 1 : 0.72)
    }
}
