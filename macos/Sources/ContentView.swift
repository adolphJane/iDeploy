import AppKit
import SwiftUI

struct ContentView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.openWindow) private var openWindow
    @State private var refreshing = false

    var body: some View {
        HStack(spacing: 0) {
            sidebar
            VStack(spacing: 0) {
                workspaceHeader
                Rectangle().fill(DeployStyle.line).frame(height: 1)
                Group {
                    if !model.ready {
                        ProgressView("正在连接后台服务…")
                            .frame(maxWidth: .infinity, maxHeight: .infinity)
                    } else {
                        switch model.section {
                        case .overview: OverviewPage()
                        case .deployments: DeploymentsPage()
                        case .servers: ServersPage()
                        case .projects: ProjectsPage()
                        case .registries: RegistriesPage()
                        case .databases: DatabasesPage()
                        case .settings: SettingsPage()
                        }
                    }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            .background(DeployStyle.canvas)
        }
        .tint(DeployStyle.green)
        .groupBoxStyle(DeployGroupBoxStyle())
        .background(DeployStyle.nav)
        .alert("iDeploy", isPresented: Binding(
            get: { model.error != nil },
            set: { if !$0 { model.error = nil } }
        )) {
            Button("确定", role: .cancel) { model.error = nil }
        } message: {
            Text(model.error ?? "")
        }
        .onAppear {
            (NSApp.delegate as? NativeAppDelegate)?.showWindow = { openWindow(id: "main") }
        }
    }

    private var sidebar: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 11) {
                Text("iD")
                    .font(.system(size: 14, weight: .heavy, design: .rounded))
                    .foregroundStyle(Color(red: 0.76, green: 0.97, blue: 0.83))
                    .frame(width: 38, height: 38)
                    .background(DeployStyle.green.opacity(0.27), in: RoundedRectangle(cornerRadius: 11))
                    .overlay {
                        RoundedRectangle(cornerRadius: 11)
                            .strokeBorder(DeployStyle.greenBright.opacity(0.42))
                    }
                VStack(alignment: .leading, spacing: 2) {
                    Text("iDeploy")
                        .font(.system(size: 15, weight: .bold))
                        .foregroundStyle(.white)
                    Text("交付控制台")
                        .font(.system(size: 10))
                        .foregroundStyle(Color.white.opacity(0.49))
                }
                Spacer()
            }
            .padding(.horizontal, 21)
            .padding(.top, 27)
            .padding(.bottom, 33)

            Text("工作空间")
                .font(.system(size: 10, weight: .bold))
                .tracking(1.8)
                .foregroundStyle(Color.white.opacity(0.33))
                .padding(.horizontal, 25)
                .padding(.bottom, 12)

            VStack(spacing: 4) {
                ForEach(AppSection.allCases.filter { $0 != .settings }) { section in
                    sidebarButton(section)
                }
            }
            .padding(.horizontal, 14)

            Spacer(minLength: 18)
            Rectangle().fill(Color.white.opacity(0.08)).frame(height: 1)
                .padding(.horizontal, 19)
            sidebarButton(.settings)
                .padding(.horizontal, 14)
                .padding(.top, 10)
            HStack(spacing: 10) {
                Circle()
                    .fill(model.ready ? Color(red: 0.33, green: 0.79, blue: 0.52) : .orange)
                    .frame(width: 8, height: 8)
                    .shadow(color: model.ready ? DeployStyle.greenBright.opacity(0.8) : .clear, radius: 5)
                VStack(alignment: .leading, spacing: 2) {
                    Text(model.ready ? "后台服务已连接" : "正在连接")
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundStyle(Color.white.opacity(0.81))
                    Text("本机工作空间")
                        .font(.system(size: 10))
                        .foregroundStyle(Color.white.opacity(0.38))
                }
                Spacer()
            }
            .padding(.horizontal, 24)
            .padding(.top, 21)
            .padding(.bottom, 25)
        }
        .frame(width: 226)
        .frame(maxHeight: .infinity)
        .background {
            LinearGradient(colors: [Color(red: 0.12, green: 0.19, blue: 0.15), DeployStyle.nav],
                           startPoint: .topLeading, endPoint: .bottomTrailing)
        }
    }

    private func sidebarButton(_ section: AppSection) -> some View {
        let selected = model.section == section
        return Button {
            model.section = section
        } label: {
            HStack(spacing: 12) {
                Image(systemName: section.symbol)
                    .font(.system(size: 15, weight: .medium))
                    .frame(width: 19)
                Text(section.title)
                    .font(.system(size: 13, weight: selected ? .semibold : .medium))
                Spacer()
                if selected {
                    Capsule().fill(DeployStyle.greenBright).frame(width: 3, height: 17)
                }
            }
            .foregroundStyle(selected ? Color(red: 0.88, green: 0.98, blue: 0.91) : Color.white.opacity(0.57))
            .padding(.horizontal, 12)
            .frame(height: 42)
            .background(selected ? DeployStyle.greenBright.opacity(0.18) : .clear,
                        in: RoundedRectangle(cornerRadius: 9, style: .continuous))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private var workspaceHeader: some View {
        HStack(alignment: .center) {
            VStack(alignment: .leading, spacing: 5) {
                Text("WORKSPACE  /  \(model.section.title.uppercased())")
                    .font(.system(size: 10, weight: .bold))
                    .tracking(1.5)
                    .foregroundStyle(DeployStyle.green)
                Text(model.section == .overview ? "工作台" : model.section.title)
                    .font(.system(size: 26, weight: .bold))
                    .tracking(-0.7)
                    .foregroundStyle(DeployStyle.ink)
            }
            Spacer()
            Button {
                Task {
                    refreshing = true
                    await model.refresh()
                    // Keep the response visible even when the local request is very fast.
                    try? await Task.sleep(nanoseconds: 1_600_000_000)
                    refreshing = false
                }
            } label: {
                HStack(spacing: 8) {
                    if refreshing {
                        ProgressView()
                            .controlSize(.small)
                            .frame(width: 14, height: 14)
                    } else {
                        Image(systemName: "arrow.clockwise")
                            .font(.system(size: 13, weight: .semibold))
                            .frame(width: 14, height: 14)
                    }
                    Text(refreshing ? "刷新中…" : "刷新数据")
                }
            }
            .buttonStyle(DeploySecondaryButtonStyle())
            .help("重新获取项目、服务器、数据库和任务状态")
            .disabled(model.busy || refreshing)
        }
        .padding(.horizontal, 30)
        .frame(height: 92)
        .background(Color.white.opacity(0.5))
    }
}

struct PageContainer<Content: View>: View {
    let content: Content
    init(@ViewBuilder content: () -> Content) { self.content = content() }
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) { content }
                .frame(maxWidth: 1080, alignment: .leading)
                .padding(30)
                .frame(maxWidth: .infinity, alignment: .topLeading)
        }
        .background {
            DeployStyle.canvas
                .overlay(alignment: .topTrailing) {
                    Circle()
                        .fill(DeployStyle.greenBright.opacity(0.075))
                        .frame(width: 410, height: 410)
                        .blur(radius: 90)
                        .offset(x: 130, y: -230)
                }
        }
    }
}

struct SectionHeading: View {
    let title: String
    let subtitle: String
    init(_ title: String, _ subtitle: String = "") {
        self.title = title
        self.subtitle = subtitle
    }
    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(title)
                .font(.system(size: 18, weight: .bold))
                .foregroundStyle(DeployStyle.ink)
            if !subtitle.isEmpty {
                Text(subtitle)
                    .font(.system(size: 12))
                    .foregroundStyle(DeployStyle.muted)
            }
        }
    }
}

struct InfoTile: View {
    let title: String
    let value: String
    let symbol: String
    var body: some View {
        HStack(alignment: .center, spacing: 13) {
            Image(systemName: symbol)
                .font(.system(size: 17, weight: .medium))
                .foregroundStyle(DeployStyle.green)
                .frame(width: 42, height: 42)
                .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 11))
            VStack(alignment: .leading, spacing: 5) {
                Text(title).font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                Text(value)
                    .font(.system(size: 20, weight: .bold))
                    .foregroundStyle(DeployStyle.ink)
                    .lineLimit(1)
                    .minimumScaleFactor(0.75)
            }
            Spacer()
        }
        .frame(maxWidth: .infinity, minHeight: 53, alignment: .leading)
        .padding(17)
        .deployCard(fill: DeployStyle.overviewPanel)
    }
}

struct KeyValue: View {
    let key: String
    let value: String
    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Text(key)
                .foregroundStyle(.secondary)
                .frame(width: 48, alignment: .leading)
            Text(value.isEmpty ? "—" : value)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

struct StateBadge: View {
    let state: String
    var body: some View {
        Text(label)
            .font(.system(size: 10, weight: .semibold))
            .foregroundStyle(color)
            .padding(.horizontal, 9)
            .padding(.vertical, 5)
            .background(color.opacity(0.12), in: Capsule())
    }
    private var label: String {
        switch state {
        case "healthy", "succeeded", "ready", "running": "正常"
        case "checking", "pending", "queued", "active": "进行中"
        case "failed", "unreachable", "error": "失败"
        case "auth_required": "需要认证"
        case "stopped": "已停止"
        default: state.isEmpty || state == "unknown" ? "未检查" : state
        }
    }
    private var color: Color {
        switch state {
        case "healthy", "succeeded", "ready", "running": DeployStyle.green
        case "failed", "unreachable", "error": .red
        case "auth_required", "degraded": .orange
        default: .secondary
        }
    }
}

struct EmptyState: View {
    let title: String
    let detail: String
    let symbol: String
    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: symbol)
                .font(.system(size: 26, weight: .light))
                .foregroundStyle(DeployStyle.green)
                .frame(width: 58, height: 58)
                .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 17))
            Text(title).font(.headline).foregroundStyle(DeployStyle.ink)
            Text(detail).font(.subheadline).foregroundStyle(DeployStyle.muted)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, minHeight: 250)
        .deployCard()
    }
}

struct OverviewPage: View {
    @EnvironmentObject private var model: AppModel
    @State private var selectedJob: J?
    private let panelHeight: CGFloat = 290
    var body: some View {
        PageContainer {
            SectionHeading("交付概况", "项目、部署、服务器、数据库和后台任务")
            LazyVGrid(columns: Array(repeating: GridItem(.flexible(minimum: 150), spacing: 12), count: 3), spacing: 12) {
                InfoTile(title: "项目", value: "\(model.projects.count)", symbol: "folder")
                InfoTile(title: "部署", value: "\(model.deployments.count)", symbol: "shippingbox")
                InfoTile(title: "服务器", value: "\(model.servers.count)", symbol: "server.rack")
                InfoTile(title: "数据库连接", value: "\(model.databases.count)", symbol: "externaldrive")
                InfoTile(title: "镜像仓库", value: "\(model.registries.count)", symbol: "archivebox")
                InfoTile(title: "运行任务", value: "\(model.overview["activeJobs"].int)", symbol: "bolt")
            }
            HStack(alignment: .top, spacing: 15) {
                databasePanel
                    .frame(maxWidth: .infinity)

                VStack(alignment: .leading, spacing: 0) {
                    HStack {
                        SectionHeading("最近任务", "构建、发布与检测记录")
                        Spacer()
                        Button("运行诊断") { Task { await model.perform("RunDiagnostic") } }
                            .buttonStyle(DeploySecondaryButtonStyle())
                    }
                    .padding(18)
                    .background(DeployStyle.overviewHeader)
                    Rectangle().fill(DeployStyle.line).frame(height: 1)
                    if model.overview["jobs"].array.isEmpty {
                        VStack(spacing: 8) {
                            Image(systemName: "clock.arrow.circlepath")
                                .font(.system(size: 24))
                                .foregroundStyle(DeployStyle.green)
                            Text("还没有任务").font(.system(size: 13, weight: .semibold))
                                .foregroundStyle(DeployStyle.ink)
                            Text("构建、发布和服务器检测会显示在这里")
                                .font(.system(size: 11))
                                .foregroundStyle(DeployStyle.muted)
                        }
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                    } else {
                        ScrollView(.vertical, showsIndicators: false) {
                            VStack(spacing: 0) {
                                ForEach(Array(model.overview["jobs"].array.enumerated()), id: \.element.id) { index, job in
                                    Button {
                                        selectedJob = job
                                    } label: {
                                        OverviewJobRow(job: job)
                                            .background(index.isMultiple(of: 2) ? Color.clear : DeployStyle.greenSoft.opacity(0.24))
                                    }
                                    .buttonStyle(.plain)
                                    .help("查看任务明细")
                                    Rectangle().fill(DeployStyle.line).frame(height: 1)
                                }
                            }
                        }
                    }
                }
                .frame(maxWidth: .infinity, minHeight: panelHeight, maxHeight: panelHeight, alignment: .topLeading)
                .deployCard(fill: DeployStyle.overviewPanel)
            }
            toolsPanel
        }
        .sheet(item: $selectedJob) { job in
            JobDetailSheet(job: job)
                .environmentObject(model)
        }
    }

    private var toolsPanel: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                SectionHeading("本机工具", "构建和发布所需的运行环境")
                Spacer()
                Text("\(model.overview["tools"].array.count) 项")
                    .font(.caption).foregroundStyle(DeployStyle.muted)
            }
            .padding(.horizontal, 18)
            .padding(.top, 18)
            HStack(alignment: .top, spacing: 0) {
                ForEach(model.overview["tools"].array, id: \.selfString) { tool in
                    HStack(spacing: 11) {
                        Image(systemName: tool["available"].bool ? "checkmark.circle.fill" : "exclamationmark.circle.fill")
                            .foregroundStyle(tool["available"].bool ? DeployStyle.greenBright : .orange)
                            .font(.system(size: 17))
                        VStack(alignment: .leading, spacing: 3) {
                            Text(tool["name"].string).font(.system(size: 12, weight: .semibold))
                                .foregroundStyle(DeployStyle.ink)
                            Text(tool["version"].string.isEmpty ? tool["detail"].string : tool["version"].string)
                                .font(.system(size: 10, design: .monospaced))
                                .foregroundStyle(DeployStyle.muted)
                                .lineLimit(1)
                        }
                        Spacer(minLength: 3)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(18)
                }
            }
        }
        .deployCard(fill: DeployStyle.overviewPanel)
    }

    private var databasePanel: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                SectionHeading("数据库", "已保存的远程数据库连接")
                Spacer()
                Button("查看全部") { model.section = .databases }
                    .buttonStyle(DeploySecondaryButtonStyle())
            }
            .padding(18)
            .background(DeployStyle.overviewHeader)
            Rectangle().fill(DeployStyle.line).frame(height: 1)
            if model.databases.isEmpty {
                HStack(spacing: 10) {
                    Image(systemName: "externaldrive.badge.plus")
                        .foregroundStyle(DeployStyle.green)
                    Text("尚未添加数据库连接")
                        .foregroundStyle(DeployStyle.muted)
                    Spacer()
                    Button("前往添加") { model.section = .databases }
                        .buttonStyle(DeploySecondaryButtonStyle())
                }
                .padding(18)
            } else {
                ForEach(Array(model.databases.prefix(3).enumerated()), id: \.element.id) { index, database in
                    HStack(spacing: 12) {
                        Image(systemName: "externaldrive")
                            .font(.system(size: 15))
                            .foregroundStyle(DeployStyle.green)
                            .frame(width: 34, height: 34)
                            .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 9))
                        VStack(alignment: .leading, spacing: 3) {
                            Text(database["name"].string)
                                .font(.system(size: 12, weight: .semibold))
                                .foregroundStyle(DeployStyle.ink)
                            Text(databaseSummary(database))
                                .font(.system(size: 10, design: .monospaced))
                                .foregroundStyle(DeployStyle.muted)
                                .lineLimit(1)
                        }
                        Spacer()
                        Text(database["lastError"].string.isEmpty ? "已配置" : "上次连接失败")
                            .font(.system(size: 10, weight: .semibold))
                            .foregroundStyle(database["lastError"].string.isEmpty ? DeployStyle.green : .red)
                    }
                    .padding(.horizontal, 18)
                    .frame(minHeight: 54)
                    .background(index.isMultiple(of: 2) ? Color.clear : DeployStyle.greenSoft.opacity(0.24))
                    if index < min(model.databases.count, 3) - 1 {
                        Rectangle().fill(DeployStyle.line).frame(height: 1).padding(.leading, 64)
                    }
                }
            }
            Spacer(minLength: 0)
            HStack(spacing: 8) {
                Text("本机数据文件")
                    .font(.system(size: 10, weight: .semibold))
                Text(model.overview["databasePath"].string)
                    .font(.system(size: 10, design: .monospaced))
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .textSelection(.enabled)
            }
            .foregroundStyle(DeployStyle.muted)
            .padding(.horizontal, 18)
            .padding(.vertical, 11)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(DeployStyle.overviewHeader.opacity(0.6))
        }
        .frame(maxWidth: .infinity, minHeight: panelHeight, maxHeight: panelHeight, alignment: .topLeading)
        .deployCard(fill: DeployStyle.overviewPanel)
    }

    private func databaseSummary(_ database: J) -> String {
        let address = "\(database["driver"].string.uppercased()) · \(database["host"].string):\(database["port"].string)"
        let name = database["databaseName"].string
        return name.isEmpty ? address : "\(address) / \(name)"
    }
}

private extension J {
    var selfString: String { id.isEmpty ? self["name"].string : id }
}
