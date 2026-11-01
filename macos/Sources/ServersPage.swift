import AppKit
import SwiftUI

private struct ServerFormRequest: Identifiable {
    let id = UUID()
    let server: J?
}

struct ServersPage: View {
    @EnvironmentObject private var model: AppModel
    @State private var selectedID: String?
    @State private var formRequest: ServerFormRequest?
    @State private var containers = J(nil)
    @State private var loadingContainers = false
    @State private var loadedContainersID: String?
    @State private var containerLoadID = UUID()
    @State private var hostKey = J(nil)
    @State private var agentStatus = J(nil)
    @State private var loadingAgent = false
    @State private var agentLoadID = UUID()
    @State private var deleteTarget: J?
    @State private var agentUninstallTarget: J?
    @State private var logText = ""
    @State private var showingLogs = false

    private var selected: J? { model.servers.first { $0.id == selectedID } }

    var body: some View {
        HStack(spacing: 0) {
            VStack(spacing: 0) {
                HStack {
                    WorkspaceListHeader(title: "服务器", count: model.servers.count)
                    Spacer()
                    Button {
                        formRequest = ServerFormRequest(server: nil)
                    } label: { Image(systemName: "plus") }
                    .buttonStyle(DeploySecondaryButtonStyle())
                    .help("添加服务器")
                }
                .padding(14)
                .background(DeployStyle.overviewHeader)
                Rectangle().fill(DeployStyle.line).frame(height: 1)
                ScrollView(.vertical, showsIndicators: false) {
                    LazyVStack(spacing: 5) {
                        ForEach(model.servers, id: \.id) { server in
                            let isSelected = selectedID == server.id
                            Button {
                                selectedID = server.id
                            } label: {
                                HStack(spacing: 11) {
                                    Image(systemName: "server.rack")
                                        .font(.system(size: 15, weight: .medium))
                                        .foregroundStyle(DeployStyle.green)
                                        .frame(width: 30, height: 30)
                                        .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 8))
                                    VStack(alignment: .leading, spacing: 5) {
                                        Text(server["name"].string)
                                            .font(.system(size: 12, weight: .semibold))
                                            .foregroundStyle(DeployStyle.ink)
                                            .lineLimit(1)
                                        Text("\(server["sshUser"].string)@\(server["host"].string):\(server["port"].string)")
                                            .font(.system(size: 10, design: .monospaced))
                                            .foregroundStyle(DeployStyle.muted)
                                            .lineLimit(1)
                                    }
                                    Spacer(minLength: 0)
                                    Circle()
                                        .fill(server["status"].string == "healthy" ? DeployStyle.greenBright : DeployStyle.muted.opacity(0.55))
                                        .frame(width: 8, height: 8)
                                    if isSelected {
                                        Capsule().fill(DeployStyle.greenBright).frame(width: 3, height: 20)
                                    }
                                }
                                .padding(.horizontal, 12)
                                .frame(maxWidth: .infinity, minHeight: 56, alignment: .leading)
                                .background(isSelected ? DeployStyle.greenSoft : Color.clear,
                                            in: RoundedRectangle(cornerRadius: 10))
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)
                            .accessibilityAddTraits(isSelected ? .isSelected : [])
                        }
                    }
                    .padding(10)
                }
                .background(DeployStyle.canvas)
            }
            .frame(minWidth: 240, idealWidth: 280, maxWidth: 330)
            .background(DeployStyle.canvas)
            Rectangle().fill(DeployStyle.line).frame(width: 1)
            if let server = selected {
                PageContainer {
                    HStack {
                        SectionHeading(server["name"].string, "\(server["sshUser"].string)@\(server["host"].string)")
                        Spacer()
                        StateBadge(state: server["status"].string)
                    }
                    GroupBox("SSH 连接") {
                        VStack(spacing: 9) {
                            KeyValue(key: "地址", value: "\(server["host"].string):\(server["port"].string)")
                            KeyValue(key: "用户", value: server["sshUser"].string)
                            KeyValue(key: "认证", value: server["authMethod"].string)
                            KeyValue(key: "私钥", value: server["privateKeyPath"].string)
                            if !server["lastError"].string.isEmpty {
                                Text(server["lastError"].string).foregroundStyle(.red).textSelection(.enabled)
                            }
                        }.padding(6)
                    }
                    HStack {
                        Button("编辑连接") { formRequest = ServerFormRequest(server: server) }
                        Button("测试 SSH") {
                            Task { await model.perform("TestSavedServerConnection", [server.id]) }
                        }
                        Button("环境检测") { Task { await model.perform("TestServer", [server.id]) } }
                        Button("刷新容器") { loadContainers(server.id) }
                        Spacer()
                        Button("移除", role: .destructive) { deleteTarget = server }
                    }
                    GroupBox("服务端 Agent") {
                        VStack(alignment: .leading, spacing: 14) {
                            HStack(spacing: 10) {
                                Image(systemName: "server.rack")
                                    .font(.system(size: 16, weight: .medium))
                                    .foregroundStyle(DeployStyle.green)
                                    .frame(width: 38, height: 38)
                                    .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 10))
                                VStack(alignment: .leading, spacing: 3) {
                                    Text(agentStatus["status"].string == "ready" ? "Agent 已连接" :
                                         agentStatus.isNull ? (loadingAgent ? "正在读取 Agent 状态" : "Agent 状态未读取") : "Agent 尚未就绪")
                                        .font(.system(size: 13, weight: .semibold))
                                        .foregroundStyle(DeployStyle.ink)
                                    if agentStatus["status"].string == "ready" {
                                        Text("Linux \(agentStatus["hello"]["arch"].string) · 版本 \(String(agentStatus["hello"]["binarySHA256"].string.prefix(12)))\(agentStatus["updateAvailable"].bool ? " · 有可用更新" : " · 与当前应用一致")")
                                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                                    }
                                }
                                Spacer(minLength: 8)
                                if loadingAgent { ProgressView().controlSize(.small).accessibilityLabel("正在刷新 Agent") }
                            }
                            if !loadingAgent && agentStatus["status"].string != "ready" {
                                Text(agentStatus.isNull ? "无法读取状态，请检查服务器连接后刷新。" :
                                     agentStatus["detail"].string.isEmpty ? "无法连接 Agent，请检查服务器连接或重新安装。" : agentStatus["detail"].string)
                                    .font(.system(size: 11)).foregroundStyle(AppColors.warning)
                                    .textSelection(.enabled)
                            }
                            if agentStatus["status"].string == "ready" {
                                let metrics = agentStatus["metrics"]
                                let cpu = metrics["cpu"]
                                let memory = metrics["memory"]
                                Rectangle().fill(DeployStyle.line).frame(height: 1)
                                Text("服务器资源 · 采集于 \(metrics["observedAt"].string)")
                                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                                HStack(alignment: .top, spacing: 20) {
                                    KeyValue(key: "CPU", value: "\(cpu["status"].string == "ok" ? String(format: "%.1f%%", (cpu["usagePercent"].raw as? NSNumber)?.doubleValue ?? 0) : cpu["status"].string) · \(cpu["logicalCores"].string) 核")
                                    KeyValue(key: "内存", value: "\(ByteCountFormatter.string(fromByteCount: Int64(memory["usedBytes"].int), countStyle: .binary)) / \(ByteCountFormatter.string(fromByteCount: Int64(memory["totalBytes"].int), countStyle: .binary))")
                                }
                                ForEach(0..<metrics["storage"].array.count, id: \.self) { index in
                                    let disk = metrics["storage"][index]
                                    KeyValue(key: "存储", value: "\(disk["mountPoint"].string) · \(ByteCountFormatter.string(fromByteCount: Int64(disk["usedBytes"].int), countStyle: .binary)) / \(ByteCountFormatter.string(fromByteCount: Int64(disk["totalBytes"].int), countStyle: .binary))")
                                }
                            }
                            Rectangle().fill(DeployStyle.line).frame(height: 1)
                            HStack(spacing: 8) {
                                Button("刷新状态") { loadAgent(server.id) }
                                    .buttonStyle(DeploySecondaryButtonStyle())
                                    .disabled(loadingAgent)
                                Button("安装/修复") { Task { await model.perform("InstallServerAgent", [server.id]) } }
                                    .buttonStyle(DeploySecondaryButtonStyle())
                                Button("更新 Agent") { Task { await model.perform("UpdateServerAgent", [server.id]) } }
                                    .buttonStyle(DeploySecondaryButtonStyle())
                                Spacer(minLength: 0)
                                Button("完全卸载", role: .destructive) { agentUninstallTarget = server }
                                    .buttonStyle(DeploySecondaryButtonStyle())
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(6)
                    }
                    if server["status"].string == "auth_required" {
                        GroupBox("SSH 主机指纹") {
                            VStack(alignment: .leading, spacing: 10) {
                                if hostKey.isNull {
                                    Button("读取主机指纹") {
                                        Task {
                                            hostKey = await model.perform("InspectServerHostKey", [server.id], refreshAfter: false) ?? J(nil)
                                        }
                                    }
                                } else {
                                    Text(hostKey["fingerprint"].string)
                                        .font(.system(.body, design: .monospaced))
                                        .textSelection(.enabled)
                                    Text("请与服务器管理员提供的指纹核对。")
                                        .foregroundStyle(.secondary)
                                    Button("确认并信任") {
                                        Task {
                                            if await model.perform("TrustServerHostKey", [server.id, hostKey["fingerprint"].string]) != nil {
                                                hostKey = J(nil)
                                            }
                                        }
                                    }
                                }
                            }.padding(6)
                        }
                    }
                    GroupBox("对外监听端口") {
                        VStack(alignment: .leading, spacing: 9) {
                            Text("当前绑定非回环地址的 TCP 端口；防火墙和公网可达性尚未验证。")
                                .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                            if !containers["portsError"].string.isEmpty {
                                Text(containers["portsError"].string)
                                    .font(.system(size: 11)).foregroundStyle(AppColors.warning).textSelection(.enabled)
                            } else if containers.isNull || loadedContainersID != server.id {
                                Text("刷新服务器数据后显示监听端口。")
                                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                            } else if containers["listeningPorts"].array.isEmpty {
                                Text("未发现对外监听的 TCP 端口。")
                                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                            } else {
                                ForEach(0..<containers["listeningPorts"].array.count, id: \.self) { index in
                                    let item = containers["listeningPorts"][index]
                                    HStack(alignment: .firstTextBaseline, spacing: 12) {
                                        Text(":\(item["port"].string)/tcp")
                                            .font(.system(size: 12, weight: .semibold, design: .monospaced))
                                            .foregroundStyle(DeployStyle.green)
                                            .frame(width: 90, alignment: .leading)
                                        VStack(alignment: .leading, spacing: 2) {
                                            Text(item["service"].string.isEmpty ? "未识别服务" : item["service"].string)
                                                .font(.system(size: 12, weight: .medium))
                                            if !item["container"].string.isEmpty && item["container"].string != item["service"].string {
                                                Text("容器：\(item["container"].string)")
                                                    .font(.system(size: 10)).foregroundStyle(DeployStyle.muted)
                                            }
                                        }
                                        Spacer(minLength: 8)
                                        Text(item["bindAddresses"].array.map(\.string).joined(separator: ", "))
                                            .font(.system(size: 10, design: .monospaced))
                                            .foregroundStyle(DeployStyle.muted).textSelection(.enabled)
                                    }
                                    .padding(.vertical, 5)
                                    .accessibilityElement(children: .combine)
                                    if index < containers["listeningPorts"].array.count - 1 {
                                        Rectangle().fill(DeployStyle.line).frame(height: 1)
                                    }
                                }
                            }
                        }.padding(6)
                    }
                    HStack {
                        SectionHeading("远程容器")
                        Spacer()
                        if loadingContainers {
                            AppLoadingView(title: "正在加载容器…")
                        }
                        if containers["stale"].bool { Text("缓存数据").foregroundStyle(.secondary) }
                    }
                    if loadingContainers && loadedContainersID != server.id {
                        AppLoadingView(title: "正在读取这台服务器上的容器…")
                            .padding(18)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .deployCard(fill: DeployStyle.overviewPanel)
                    } else if containers.isNull || containers["containers"].array.isEmpty {
                        HStack(spacing: 13) {
                            Image(systemName: "shippingbox")
                                .font(.system(size: 20, weight: .medium))
                                .foregroundStyle(DeployStyle.green)
                                .frame(width: 42, height: 42)
                                .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 11))
                            VStack(alignment: .leading, spacing: 4) {
                                Text("没有容器数据")
                                    .font(.system(size: 13, weight: .semibold))
                                    .foregroundStyle(DeployStyle.ink)
                                Text("刷新容器后可查看这台服务器上的 Docker 工作负载。")
                                    .font(.system(size: 11))
                                    .foregroundStyle(DeployStyle.muted)
                            }
                            Spacer(minLength: 0)
                        }
                        .padding(18)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .deployCard(fill: DeployStyle.overviewPanel)
                    } else {
                        ForEach(containers["containers"].array, id: \.id) { container in
                            GroupBox {
                                HStack(alignment: .top) {
                                    VStack(alignment: .leading, spacing: 5) {
                                        Text(container["name"].string).fontWeight(.semibold)
                                        Text(container["image"].string)
                                            .font(.system(.caption, design: .monospaced))
                                            .foregroundStyle(.secondary).textSelection(.enabled)
                                        Text(container["status"].string).font(.caption).foregroundStyle(.secondary)
                                        if !container["ports"].string.isEmpty {
                                            Text(container["ports"].string).font(.caption)
                                        }
                                    }
                                    Spacer()
                                    StateBadge(state: container["state"].string)
                                    Menu {
                                        Button("启动") { operate(server.id, container.id, "start") }
                                        Button("重启") { operate(server.id, container.id, "restart") }
                                        Button("停止") { operate(server.id, container.id, "stop") }
                                        Divider()
                                        Button("查看日志") {
                                            Task {
                                                if let result = await model.perform("GetContainerLogs",
                                                                                    [["serverId": server.id, "containerId": container.id, "tail": 200]],
                                                                                    refreshAfter: false) {
                                                    logText = result["logs"].string
                                                    showingLogs = true
                                                }
                                            }
                                        }
                                    } label: { Image(systemName: "ellipsis.circle") }
                                }.padding(5)
                            }
                        }
                    }
                }
                .task(id: server.id) {
                    loadContainers(server.id)
                    loadAgent(server.id)
                }
                .groupBoxStyle(DeployGroupBoxStyle(fill: DeployStyle.overviewPanel))
            } else {
                VStack(spacing: 12) {
                    Image(systemName: "server.rack")
                        .font(.system(size: 28, weight: .light))
                        .foregroundStyle(DeployStyle.green)
                        .frame(width: 68, height: 68)
                        .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 19))
                    Text("选择一台服务器")
                        .font(.system(size: 16, weight: .semibold))
                        .foregroundStyle(DeployStyle.ink)
                    Text("查看 SSH 连接和远程 Docker 容器。")
                        .font(.system(size: 12))
                        .foregroundStyle(DeployStyle.muted)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(DeployStyle.canvas)
            }
        }
        .environment(\.colorScheme, .light)
        .sheet(item: $formRequest) { request in
            ServerFormView(server: request.server)
                .environmentObject(model)
                .id(request.id)
        }
        .onChange(of: selectedID) { _ in
            containers = J(nil)
            agentStatus = J(nil)
            loadingAgent = false
            agentLoadID = UUID()
            hostKey = J(nil)
            loadedContainersID = nil
            loadingContainers = selectedID != nil
        }
        .sheet(isPresented: $showingLogs) {
            VStack(alignment: .leading, spacing: 12) {
                Text("容器日志").font(.title2)
                TextEditor(text: .constant(logText))
                    .font(.system(.body, design: .monospaced))
                HStack { Spacer(); Button("关闭") { showingLogs = false } }
            }
            .padding(20)
            .frame(width: 800, height: 570)
        }
        .confirmationDialog("移除服务器？", isPresented: Binding(
            get: { deleteTarget != nil },
            set: { if !$0 { deleteTarget = nil } }
        )) {
            Button("移除服务器", role: .destructive) {
                guard let target = deleteTarget else { return }
                Task {
                    await model.perform("DeleteServer", [target.id])
                    selectedID = nil
                    deleteTarget = nil
                }
            }
        }
        .confirmationDialog("完全卸载服务端 Agent？", isPresented: Binding(
            get: { agentUninstallTarget != nil },
            set: { if !$0 { agentUninstallTarget = nil } }
        )) {
            Button("卸载 Agent", role: .destructive) {
                guard let target = agentUninstallTarget else { return }
                Task {
                    if await model.perform("UninstallServerAgent", [target.id]) != nil {
                        agentStatus = J(nil)
                    }
                    agentUninstallTarget = nil
                }
            }
        } message: {
            Text("将停止并移除 Agent、配对凭据、操作日志和 Agent 创建的续期任务；现有容器、Nginx 配置和证书保留。")
        }
    }

    private func loadContainers(_ id: String) {
        guard selectedID == id else { return }
        let requestID = UUID()
        containerLoadID = requestID
        loadingContainers = true
        Task {
            let nextContainers = await model.perform("ListRemoteContainers", [id], refreshAfter: false) ?? J(nil)
            guard selectedID == id, containerLoadID == requestID else { return }
            containers = nextContainers
            loadedContainersID = id
            loadingContainers = false
        }
    }

    private func loadAgent(_ id: String) {
        guard selectedID == id else { return }
        let requestID = UUID()
        agentLoadID = requestID
        loadingAgent = true
        Task {
            let result = await model.perform("GetServerAgentStatus", [id], refreshAfter: false) ?? J(nil)
            guard selectedID == id, agentLoadID == requestID else { return }
            agentStatus = result
            loadingAgent = false
        }
    }

    private func operate(_ serverID: String, _ containerID: String, _ action: String) {
        Task {
            if await model.perform("OperateRemoteContainer", [serverID, containerID, action]) != nil {
                loadContainers(serverID)
            }
        }
    }
}

struct ServerFormView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let server: J?
    @State private var name: String
    @State private var host: String
    @State private var port: String
    @State private var user: String
    @State private var method: String
    @State private var password = ""
    @State private var keyPath: String
    @State private var passphrase = ""

    init(server: J?) {
        self.server = server
        _name = State(initialValue: server?["name"].string ?? "")
        _host = State(initialValue: server?["host"].string ?? "")
        _port = State(initialValue: server?["port"].string ?? "22")
        _user = State(initialValue: server?["sshUser"].string ?? "root")
        _method = State(initialValue: server?["authMethod"].string ?? "agent")
        _keyPath = State(initialValue: server?["privateKeyPath"].string ?? "")
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 14) {
                Image(systemName: "server.rack")
                    .font(.system(size: 20, weight: .medium))
                    .foregroundStyle(DeployStyle.green)
                    .frame(width: 48, height: 48)
                    .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 13))
                VStack(alignment: .leading, spacing: 5) {
                    Text(server == nil ? "添加服务器" : "编辑服务器连接")
                        .font(.system(size: 21, weight: .bold))
                        .foregroundStyle(DeployStyle.ink)
                    Text("配置 SSH 地址和认证方式，用于连接远程 Docker。")
                        .font(.system(size: 12))
                        .foregroundStyle(DeployStyle.muted)
                }
                Spacer()
            }
            .padding(20)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(DeployStyle.overviewHeader)

            ScrollView(.vertical, showsIndicators: true) {
                VStack(alignment: .leading, spacing: 14) {
                    VStack(alignment: .leading, spacing: 15) {
                        SectionHeading("服务器", "为这台主机设置便于识别的名称")
                        HStack(alignment: .top, spacing: 16) {
                            VStack(alignment: .leading, spacing: 7) {
                                Text("名称").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                TextField("例如 生产环境", text: $name)
                                    .textFieldStyle(.roundedBorder)
                            }
                            VStack(alignment: .leading, spacing: 7) {
                                Text("主机地址").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                TextField("域名或 IP 地址", text: $host)
                                    .textFieldStyle(.roundedBorder)
                            }
                        }
                        HStack(alignment: .top, spacing: 16) {
                            VStack(alignment: .leading, spacing: 7) {
                                Text("SSH 端口").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                TextField("22", text: $port)
                                    .textFieldStyle(.roundedBorder)
                            }
                            VStack(alignment: .leading, spacing: 7) {
                                Text("登录用户").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                TextField("例如 root", text: $user)
                                    .textFieldStyle(.roundedBorder)
                            }
                        }
                    }
                    .padding(16)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .deployCard(fill: DeployStyle.overviewPanel)

                    VStack(alignment: .leading, spacing: 15) {
                        SectionHeading("认证方式", "凭据保存在本机，用于建立 SSH 连接")
                        VStack(alignment: .leading, spacing: 7) {
                            Text("登录方式").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                            Picker("登录方式", selection: $method) {
                                Text("SSH Agent").tag("agent")
                                Text("密码").tag("password")
                                Text("私钥文件").tag("private_key")
                            }
                            .labelsHidden()
                            .frame(maxWidth: .infinity, alignment: .leading)
                        }
                        if method == "password" {
                            VStack(alignment: .leading, spacing: 7) {
                                Text("登录密码").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                SecureField("输入服务器密码", text: $password)
                                    .textFieldStyle(.roundedBorder)
                            }
                        }
                        if method == "private_key" {
                            VStack(alignment: .leading, spacing: 12) {
                                VStack(alignment: .leading, spacing: 7) {
                                    Text("私钥文件").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                    HStack(spacing: 10) {
                                        TextField("选择或输入私钥路径", text: $keyPath)
                                            .textFieldStyle(.roundedBorder)
                                        Button("选择文件…") {
                                            let panel = NSOpenPanel()
                                            panel.canChooseDirectories = false
                                            panel.canChooseFiles = true
                                            if panel.runModal() == .OK { keyPath = panel.url?.path ?? "" }
                                        }
                                        .buttonStyle(DeploySecondaryButtonStyle())
                                    }
                                }
                                VStack(alignment: .leading, spacing: 7) {
                                    Text("私钥口令").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                    SecureField("如私钥没有口令，可留空", text: $passphrase)
                                        .textFieldStyle(.roundedBorder)
                                }
                            }
                        }
                        if method == "agent" {
                            Label("将使用当前 macOS 用户的 SSH Agent。", systemImage: "key.horizontal")
                                .font(.system(size: 11))
                                .foregroundStyle(DeployStyle.muted)
                        }
                    }
                    .padding(16)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .deployCard(fill: DeployStyle.overviewPanel)
                }
                .padding(.horizontal, 24)
                .padding(.vertical, 16)
            }
            .background(DeployStyle.canvas)

            Rectangle().fill(DeployStyle.line).frame(height: 1)
            HStack {
                Text("名称、主机地址和登录用户为必填项")
                    .font(.system(size: 11))
                    .foregroundStyle(DeployStyle.muted)
                Spacer()
                Button("取消") { dismiss() }
                    .buttonStyle(DeploySecondaryButtonStyle())
                    .keyboardShortcut(.cancelAction)
                Button("保存") {
                    Task {
                        let input: [String: Any] = [
                            "name": name, "host": host, "port": Int(port) ?? 22,
                            "sshUser": user, "authMethod": method,
                            "password": password, "privateKeyPath": keyPath,
                            "privateKeyPassphrase": passphrase
                        ]
                        let result = await model.perform(server == nil ? "CreateServer" : "UpdateServer",
                                                         server == nil ? [input] : [server!.id, input])
                        if let result {
                            if server == nil {
                                _ = await model.perform("InstallServerAgent", [result.id], refreshAfter: false)
                            }
                            dismiss()
                        }
                    }
                }
                .buttonStyle(DeployPrimaryButtonStyle())
                .keyboardShortcut(.defaultAction)
                .disabled(name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ||
                          host.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ||
                          user.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 16)
            .background(DeployStyle.overviewPanel)
        }
        .frame(width: 650, height: method == "private_key" ? 680 : method == "password" ? 600 : 540)
        .environment(\.colorScheme, .light)
    }
}
