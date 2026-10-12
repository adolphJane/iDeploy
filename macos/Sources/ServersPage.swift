import AppKit
import SwiftUI

struct ServersPage: View {
    @EnvironmentObject private var model: AppModel
    @State private var selectedID: String?
    @State private var editing: J?
    @State private var showingForm = false
    @State private var containers = J(nil)
    @State private var hostKey = J(nil)
    @State private var deleteTarget: J?
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
                        editing = nil
                        showingForm = true
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
                        Button("编辑连接") { editing = server; showingForm = true }
                        Button("测试 SSH") {
                            Task { await model.perform("TestSavedServerConnection", [server.id]) }
                        }
                        Button("环境检测") { Task { await model.perform("TestServer", [server.id]) } }
                        Button("刷新容器") { loadContainers(server.id) }
                        Spacer()
                        Button("移除", role: .destructive) { deleteTarget = server }
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
                    HStack {
                        SectionHeading("远程容器")
                        Spacer()
                        if containers["stale"].bool { Text("缓存数据").foregroundStyle(.secondary) }
                    }
                    if containers.isNull || containers["containers"].array.isEmpty {
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
                .task(id: server.id) { loadContainers(server.id) }
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
        .sheet(isPresented: $showingForm) {
            ServerFormView(server: editing).environmentObject(model)
        }
        .onChange(of: selectedID) { _ in
            containers = J(nil)
            hostKey = J(nil)
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
    }

    private func loadContainers(_ id: String) {
        Task { containers = await model.perform("ListRemoteContainers", [id], refreshAfter: false) ?? J(nil) }
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
                        if result != nil { dismiss() }
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
