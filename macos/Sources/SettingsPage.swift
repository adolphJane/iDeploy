import AppKit
import SwiftUI

private enum SettingsSection: String, CaseIterable, Identifiable {
    case app = "应用"
    case terminal = "终端集成"
    case build = "构建与智能"
    case data = "本机数据"

    var id: String { rawValue }
    var symbol: String {
        switch self {
        case .app: "app.badge"
        case .terminal: "terminal"
        case .build: "hammer"
        case .data: "internaldrive"
        }
    }
    var subtitle: String {
        switch self {
        case .app: "版本、更新和启动"
        case .terminal: "命令行与 MCP"
        case .build: "构建代理与 AI"
        case .data: "文件、缓存和清理"
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

    var body: some View {
        HStack(spacing: 0) {
            VStack(spacing: 0) {
                HStack {
                    VStack(alignment: .leading, spacing: 3) {
                        Text("设置")
                            .font(.system(size: 16, weight: .bold))
                            .foregroundStyle(DeployStyle.ink)
                        Text("4 个分类")
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
                }
            }
            .id(section)
        }
        .environment(\.colorScheme, .light)
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
            Text("这会删除本机保存的服务器、部署、相关任务和 SSH 凭据，无法撤销。")
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
