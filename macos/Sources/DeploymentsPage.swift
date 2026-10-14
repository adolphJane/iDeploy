import SwiftUI

private struct DeploymentFormRequest: Identifiable {
    let id = UUID()
    let deployment: J?
}

struct DeploymentsPage: View {
    @EnvironmentObject private var model: AppModel
    @State private var selectedID: String?
    @State private var runtime = J(nil)
    @State private var releases: [J] = []
    @State private var environment = J(nil)
    @State private var loadingDetail = false
    @State private var loadedDetailID: String?
    @State private var detailLoadID = UUID()
    @State private var formRequest: DeploymentFormRequest?
    @State private var showingPublish = false
    @State private var showingEnvironment = false
    @State private var deleteTarget: J?
    @State private var rollbackTarget: J?

    private var selected: J? { model.deployments.first { $0.id == selectedID } }

    var body: some View {
        HStack(spacing: 0) {
            VStack(spacing: 0) {
                HStack {
                    WorkspaceListHeader(title: "部署", count: model.deployments.count)
                    Spacer()
                    Button { formRequest = DeploymentFormRequest(deployment: nil) } label: { Image(systemName: "plus") }
                        .buttonStyle(DeploySecondaryButtonStyle())
                    .help("添加部署")
                }
                .padding(14)
                .background(DeployStyle.overviewHeader)
                Rectangle().fill(DeployStyle.line).frame(height: 1)
                ScrollView(.vertical, showsIndicators: false) {
                    LazyVStack(spacing: 5) {
                        ForEach(model.deployments, id: \.id) { deployment in
                            let isSelected = selectedID == deployment.id
                            Button {
                                selectedID = deployment.id
                            } label: {
                                HStack(spacing: 11) {
                                    Image(systemName: "shippingbox")
                                        .font(.system(size: 15, weight: .medium))
                                        .foregroundStyle(DeployStyle.green)
                                        .frame(width: 30, height: 30)
                                        .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 8))
                                    VStack(alignment: .leading, spacing: 5) {
                                        Text(deployment["projectName"].string)
                                            .font(.system(size: 12, weight: .semibold))
                                            .foregroundStyle(DeployStyle.ink)
                                            .lineLimit(1)
                                        Text("\(deployment["serverName"].string) · \(deployment["workloadName"].string)")
                                            .font(.system(size: 10))
                                            .foregroundStyle(DeployStyle.muted)
                                            .lineLimit(1)
                                    }
                                    Spacer(minLength: 0)
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
            if let deployment = selected {
                PageContainer {
                    HStack {
                        SectionHeading(deployment["projectName"].string,
                                       "\(deployment["serverName"].string) · \(deployment["workloadName"].string)")
                        Spacer()
                        if loadingDetail && loadedDetailID != deployment.id {
                            ProgressView()
                                .controlSize(.small)
                                .accessibilityLabel("正在读取部署状态")
                        } else {
                            StateBadge(state: runtime["status"].string)
                        }
                    }
                    HStack {
                        Button("发布新版本") { showingPublish = true }
                            .buttonStyle(.borderedProminent)
                        Button("刷新状态") { loadDetail(deployment.id) }
                        Menu("运行操作") {
                            Button("启动") { operate(deployment.id, "start") }
                            Button("重启") { operate(deployment.id, "restart") }
                            Button("停止") { operate(deployment.id, "stop") }
                        }
                        Button("编辑配置") { formRequest = DeploymentFormRequest(deployment: deployment) }
                        Spacer()
                        Button("移除", role: .destructive) { deleteTarget = deployment }
                    }
                    HStack(spacing: 12) {
                        InfoTile(title: "工作负载", value: deployment["workloadKind"].string == "compose" ? "Compose" : "容器", symbol: "shippingbox")
                        InfoTile(title: "目标服务器", value: deployment["serverName"].string, symbol: "server.rack")
                        InfoTile(title: "发布记录", value: loadingDetail && loadedDetailID != deployment.id ? "…" : "\(releases.count)", symbol: "clock.arrow.circlepath")
                    }
                    if loadingDetail {
                        AppLoadingView(title: "正在加载部署状态、环境变量和发布记录…")
                            .padding(18)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .deployCard(fill: DeployStyle.overviewPanel)
                    }
                    if !runtime["detail"].string.isEmpty {
                        Text(runtime["detail"].string)
                            .foregroundStyle(.secondary).textSelection(.enabled)
                    }
                    if !runtime["containers"].array.isEmpty {
                        SectionHeading("运行中的容器")
                        ForEach(runtime["containers"].array, id: \.id) { container in
                            GroupBox {
                                HStack {
                                    VStack(alignment: .leading, spacing: 5) {
                                        Text(container["name"].string).fontWeight(.medium)
                                        Text(container["image"].string)
                                            .font(.system(.caption, design: .monospaced))
                                            .textSelection(.enabled)
                                        Text(container["status"].string)
                                            .font(.caption).foregroundStyle(.secondary)
                                    }
                                    Spacer()
                                    StateBadge(state: container["state"].string)
                                }.padding(5)
                            }
                        }
                    }
                    VStack(alignment: .leading, spacing: 14) {
                        HStack {
                            SectionHeading("环境变量")
                            Spacer()
                            if environment["pendingApply"].bool {
                                Label("待下次发布生效", systemImage: "exclamationmark.circle")
                                    .foregroundStyle(.orange)
                            }
                            Button("管理变量") { showingEnvironment = true }
                        }
                        if loadingDetail && loadedDetailID != deployment.id {
                            Text("正在读取环境变量…").foregroundStyle(.secondary)
                        } else if environment["variables"].array.isEmpty {
                            Text("尚未配置环境变量").foregroundStyle(.secondary)
                        } else {
                            VStack(spacing: 0) {
                                ForEach(environment["variables"].array, id: \.selfName) { item in
                                    HStack {
                                        Text(item["name"].string).font(.system(.body, design: .monospaced))
                                        Spacer()
                                        Text(item["kind"].string == "secret" ? "钥匙串秘密" : item["value"].string)
                                            .foregroundStyle(.secondary)
                                    }
                                    .padding(.vertical, 7)
                                    if item.selfName != environment["variables"].array.last?.selfName {
                                        Divider()
                                    }
                                }
                            }
                        }
                    }
                    .padding(18)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .deployCard(fill: DeployStyle.overviewPanel)
                    SectionHeading("发布记录")
                    if loadingDetail && loadedDetailID != deployment.id {
                        EmptyView()
                    } else if releases.isEmpty {
                        HStack(spacing: 13) {
                            Image(systemName: "clock.arrow.circlepath")
                                .font(.system(size: 20, weight: .medium))
                                .foregroundStyle(DeployStyle.green)
                                .frame(width: 42, height: 42)
                                .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 11))
                            VStack(alignment: .leading, spacing: 4) {
                                Text("还没有发布记录")
                                    .font(.system(size: 13, weight: .semibold))
                                    .foregroundStyle(DeployStyle.ink)
                                Text("发布新版本后可在这里查看结果和回滚。")
                                    .font(.system(size: 11))
                                    .foregroundStyle(DeployStyle.muted)
                            }
                            Spacer(minLength: 12)
                        }
                        .padding(18)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .deployCard(fill: DeployStyle.overviewPanel)
                    } else {
                        ForEach(releases, id: \.id) { release in
                            GroupBox {
                                HStack(alignment: .top, spacing: 12) {
                                    StateBadge(state: release["status"].string)
                                    VStack(alignment: .leading, spacing: 5) {
                                        Text(release["version"].string).fontWeight(.semibold)
                                        Text(release["imageRef"].string)
                                            .font(.system(.caption, design: .monospaced))
                                            .foregroundStyle(.secondary).textSelection(.enabled)
                                        Text(release["detail"].string)
                                            .font(.caption).foregroundStyle(.secondary).textSelection(.enabled)
                                    }
                                    Spacer()
                                    if release["status"].string == "succeeded" {
                                        Button("回滚到此版本") { rollbackTarget = release }
                                    } else if release["status"].string == "failed" {
                                        Button("重试") {
                                            Task {
                                                await model.perform("RetryRelease", [release.id])
                                                loadDetail(deployment.id)
                                            }
                                        }
                                    }
                                }.padding(5)
                            }
                        }
                    }
                }
                .task(id: deployment.id) { loadDetail(deployment.id) }
            } else {
                VStack(spacing: 12) {
                    Image(systemName: "shippingbox")
                        .font(.system(size: 28, weight: .light))
                        .foregroundStyle(DeployStyle.green)
                        .frame(width: 68, height: 68)
                        .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 19))
                    Text("选择一个部署")
                        .font(.system(size: 16, weight: .semibold))
                        .foregroundStyle(DeployStyle.ink)
                    Text("查看运行状态、发布记录和环境配置。")
                        .font(.system(size: 12))
                        .foregroundStyle(DeployStyle.muted)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(DeployStyle.canvas)
            }
        }
        .environment(\.colorScheme, .light)
        .sheet(item: $formRequest) { request in
            DeploymentFormView(deployment: request.deployment)
                .environmentObject(model)
                .id(request.id)
        }
        .onChange(of: selectedID) { _ in
            runtime = J(nil)
            releases = []
            environment = J(nil)
            loadedDetailID = nil
            loadingDetail = selectedID != nil
        }
        .sheet(isPresented: $showingPublish) {
            if let selected {
                PublishView(deployment: selected) {
                    loadDetail(selected.id)
                }
                .environmentObject(model)
            }
        }
        .sheet(isPresented: $showingEnvironment) {
            if let selected {
                EnvironmentEditorView(deployment: selected) {
                    loadDetail(selected.id)
                }
                .environmentObject(model)
            }
        }
        .confirmationDialog("移除部署配置？", isPresented: Binding(
            get: { deleteTarget != nil },
            set: { if !$0 { deleteTarget = nil } }
        )) {
            Button("移除部署", role: .destructive) {
                guard let target = deleteTarget else { return }
                Task {
                    await model.perform("DeleteDeployment", [target.id])
                    selectedID = nil
                    deleteTarget = nil
                }
            }
        }
        .confirmationDialog("回滚到这个版本？", isPresented: Binding(
            get: { rollbackTarget != nil },
            set: { if !$0 { rollbackTarget = nil } }
        )) {
            Button("开始回滚", role: .destructive) {
                guard let target = rollbackTarget, let deployment = selected else { return }
                Task {
                    await model.perform("RollbackDeployment", [deployment.id, target.id])
                    rollbackTarget = nil
                    loadDetail(deployment.id)
                }
            }
        } message: {
            Text("将以所选发布记录的镜像和配置重建远程工作负载。")
        }
    }

    private func loadDetail(_ id: String) {
        guard selectedID == id else { return }
        let requestID = UUID()
        detailLoadID = requestID
        loadingDetail = true
        Task {
            let nextRuntime = await model.perform("InspectDeploymentRuntime", [id], refreshAfter: false) ?? J(nil)
            let nextReleases = (await model.perform("ListDeploymentReleases", [id], refreshAfter: false) ?? J(nil)).array
            let nextEnvironment = await model.perform("ListDeploymentEnvironment", [id], refreshAfter: false) ?? J(nil)
            guard selectedID == id, detailLoadID == requestID else { return }
            runtime = nextRuntime
            releases = nextReleases
            environment = nextEnvironment
            loadedDetailID = id
            loadingDetail = false
        }
    }

    private func operate(_ id: String, _ action: String) {
        Task {
            if await model.perform("OperateDeployment", [id, action]) != nil {
                loadDetail(id)
            }
        }
    }
}

private extension J {
    var selfName: String { self["name"].string }
}

struct DeploymentFormView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let deployment: J?
    @State private var projectID: String
    @State private var serverID: String
    @State private var workloadKind: String
    @State private var workloadName: String
    @State private var ports: String
    @State private var restartPolicy: String

    init(deployment: J?) {
        self.deployment = deployment
        _projectID = State(initialValue: deployment?["projectId"].string ?? "")
        _serverID = State(initialValue: deployment?["serverId"].string ?? "")
        _workloadKind = State(initialValue: deployment?["workloadKind"].string ?? "container")
        _workloadName = State(initialValue: deployment?["workloadName"].string ?? "")
        _ports = State(initialValue: deployment?["containerPorts"].array.map(\.string).joined(separator: ", ") ?? "")
        _restartPolicy = State(initialValue: deployment?["restartPolicy"].string ?? "unless-stopped")
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 14) {
                Image(systemName: "shippingbox")
                    .font(.system(size: 20, weight: .medium))
                    .foregroundStyle(DeployStyle.green)
                    .frame(width: 48, height: 48)
                    .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 13))
                VStack(alignment: .leading, spacing: 5) {
                    Text(deployment == nil ? "添加部署" : "编辑部署配置")
                        .font(.system(size: 21, weight: .bold))
                        .foregroundStyle(DeployStyle.ink)
                    Text("选择项目和服务器，配置要运行的工作负载。")
                        .font(.system(size: 12))
                        .foregroundStyle(DeployStyle.muted)
                }
                Spacer()
            }
            .padding(20)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(DeployStyle.overviewHeader)

            ScrollView(.vertical, showsIndicators: false) {
                VStack(alignment: .leading, spacing: 14) {
                    VStack(alignment: .leading, spacing: 15) {
                        SectionHeading("来源与目标", "确定构建来源及部署位置")
                        HStack(alignment: .top, spacing: 16) {
                            VStack(alignment: .leading, spacing: 7) {
                                Text("项目").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                Picker("项目", selection: $projectID) {
                                    Text("选择项目").tag("")
                                    ForEach(model.projects, id: \.id) { project in
                                        Text(project["name"].string).tag(project.id)
                                    }
                                }
                                .labelsHidden()
                                .frame(maxWidth: .infinity, alignment: .leading)
                            }
                            VStack(alignment: .leading, spacing: 7) {
                                Text("服务器").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                Picker("服务器", selection: $serverID) {
                                    Text("选择服务器").tag("")
                                    ForEach(model.servers, id: \.id) { server in
                                        Text(server["name"].string).tag(server.id)
                                    }
                                }
                                .labelsHidden()
                                .frame(maxWidth: .infinity, alignment: .leading)
                            }
                        }
                    }
                    .padding(16)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .deployCard(fill: DeployStyle.overviewPanel)

                    VStack(alignment: .leading, spacing: 15) {
                        SectionHeading("工作负载", "设置远程运行方式")
                        HStack(alignment: .top, spacing: 16) {
                            VStack(alignment: .leading, spacing: 7) {
                                Text("类型").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                Picker("类型", selection: $workloadKind) {
                                    Text("独立容器").tag("container")
                                    Text("Docker Compose").tag("compose")
                                }
                                .labelsHidden()
                                .frame(maxWidth: .infinity, alignment: .leading)
                            }
                            VStack(alignment: .leading, spacing: 7) {
                                Text(workloadKind == "compose" ? "Compose 项目名称" : "容器名称")
                                    .font(.system(size: 12, weight: .semibold))
                                    .foregroundStyle(DeployStyle.ink)
                                TextField(workloadKind == "compose" ? "输入项目名称" : "输入容器名称", text: $workloadName)
                                    .textFieldStyle(.roundedBorder)
                            }
                        }
                        if workloadKind == "container" {
                            HStack(alignment: .top, spacing: 16) {
                                VStack(alignment: .leading, spacing: 7) {
                                    Text("端口映射").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                    TextField("例如 8080:80, 8443:443", text: $ports)
                                        .textFieldStyle(.roundedBorder)
                                    Text("多个端口用逗号分隔")
                                        .font(.system(size: 10))
                                        .foregroundStyle(DeployStyle.muted)
                                }
                                VStack(alignment: .leading, spacing: 7) {
                                    Text("重启策略").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                    Picker("重启策略", selection: $restartPolicy) {
                                        Text("除手动停止外自动重启").tag("unless-stopped")
                                        Text("始终重启").tag("always")
                                        Text("失败时重启").tag("on-failure")
                                        Text("不自动重启").tag("no")
                                    }
                                    .labelsHidden()
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                }
                            }
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
                Text("项目、服务器和名称为必填项")
                    .font(.system(size: 11))
                    .foregroundStyle(DeployStyle.muted)
                Spacer()
                Button("取消") { dismiss() }
                    .buttonStyle(DeploySecondaryButtonStyle())
                    .keyboardShortcut(.cancelAction)
                Button("保存") {
                    Task {
                        let input: [String: Any] = [
                            "projectId": projectID, "serverId": serverID, "workloadKind": workloadKind,
                            "workloadName": workloadName,
                            "containerPorts": ports.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) },
                            "restartPolicy": restartPolicy
                        ]
                        let result = await model.perform(deployment == nil ? "CreateDeployment" : "ConfigureDeployment",
                                                         deployment == nil ? [input] : [deployment!.id, input])
                        if result != nil { dismiss() }
                    }
                }
                .buttonStyle(DeployPrimaryButtonStyle())
                .keyboardShortcut(.defaultAction)
                .disabled(projectID.isEmpty || serverID.isEmpty || workloadName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 16)
            .background(DeployStyle.overviewPanel)
        }
        .frame(width: 650, height: workloadKind == "compose" ? 470 : 540)
        .environment(\.colorScheme, .light)
    }
}

struct PublishView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let deployment: J
    let onPublished: () -> Void
    @State private var registryID = ""
    @State private var version = ""
    @State private var preflight = J(nil)
    @State private var checking = false

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            SectionHeading("发布新版本", deployment["projectName"].string)
            Form {
                Picker("镜像仓库", selection: $registryID) {
                    Text("选择镜像仓库").tag("")
                    ForEach(model.registries, id: \.id) { registry in
                        Text(registry["name"].string).tag(registry.id)
                    }
                }
                TextField("版本", text: $version)
            }
            if checking { ProgressView("正在执行发布前检查…") }
            if !preflight.isNull {
                GroupBox("发布前检查") {
                    VStack(alignment: .leading, spacing: 10) {
                        ForEach(preflight["checks"].array, id: \.checkID) { check in
                            HStack(alignment: .top) {
                                Image(systemName: check["status"].string == "failed" ? "xmark.circle.fill" : "checkmark.circle.fill")
                                    .foregroundStyle(check["status"].string == "failed" ? .red : .green)
                                Text(check["detail"].string)
                            }
                        }
                    }.padding(5)
                }
            }
            Spacer()
            HStack {
                Button("检查就绪状态") {
                    Task {
                        checking = true
                        preflight = await model.perform("PreflightDeployment",
                                                        [["deploymentId": deployment.id, "registryId": registryID]],
                                                        refreshAfter: false) ?? J(nil)
                        checking = false
                    }
                }
                .disabled(registryID.isEmpty || checking)
                Spacer()
                Button("取消") { dismiss() }
                Button("开始发布") {
                    Task {
                        let input: [String: Any] = [
                            "deploymentId": deployment.id, "registryId": registryID, "version": version
                        ]
                        if await model.perform("PublishDeployment", [input]) != nil {
                            onPublished()
                            dismiss()
                        }
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(!preflight["ok"].bool || version.isEmpty || registryID.isEmpty)
            }
        }
        .padding(20)
        .frame(width: 640, height: 540)
        .task {
            if let suggestion = await model.perform("DetectDeploymentVersion", [deployment.id], refreshAfter: false) {
                version = suggestion["version"].string
            }
        }
        .onChange(of: registryID) { _ in preflight = J(nil) }
        .onChange(of: version) { _ in preflight = J(nil) }
    }
}

private extension J {
    var checkID: String { self["id"].string }
}

struct EnvironmentEditorView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let deployment: J
    let onChanged: () -> Void
    @State private var variables: [J] = []
    @State private var name = ""
    @State private var value = ""
    @State private var kind = "plain"

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            SectionHeading("环境变量", deployment["projectName"].string)
            List {
                ForEach(variables, id: \.selfName) { item in
                    HStack {
                        Text(item["name"].string).font(.system(.body, design: .monospaced))
                        Spacer()
                        Text(item["kind"].string == "secret" ? "钥匙串秘密" : item["value"].string)
                            .foregroundStyle(.secondary)
                        Button(role: .destructive) {
                            Task {
                                let input: [String: Any] = [
                                    "deploymentId": deployment.id, "variables": [],
                                    "remove": [item["name"].string]
                                ]
                                if await model.perform("UpdateDeploymentEnvironment", [input]) != nil {
                                    load()
                                    onChanged()
                                }
                            }
                        } label: { Image(systemName: "minus.circle") }
                        .buttonStyle(.borderless)
                    }
                }
            }
            .frame(minHeight: 180)
            Divider()
            Form {
                TextField("变量名", text: $name)
                Picker("类型", selection: $kind) {
                    Text("普通值").tag("plain")
                    Text("秘密").tag("secret")
                }
                if kind == "secret" {
                    SecureField("秘密值", text: $value)
                } else {
                    TextField("值", text: $value)
                }
            }
            HStack {
                Button("添加变量") {
                    Task {
                        let input: [String: Any] = [
                            "deploymentId": deployment.id,
                            "variables": [["name": name, "kind": kind, "value": value]],
                            "remove": []
                        ]
                        if await model.perform("UpdateDeploymentEnvironment", [input]) != nil {
                            name = ""
                            value = ""
                            load()
                            onChanged()
                        }
                    }
                }
                .disabled(name.isEmpty || value.isEmpty)
                Spacer()
                Button("完成") { dismiss() }
            }
        }
        .padding(20)
        .frame(width: 620, height: 550)
        .task { load() }
    }

    private func load() {
        Task {
            variables = (await model.perform("ListDeploymentEnvironment", [deployment.id], refreshAfter: false) ?? J(nil))["variables"].array
        }
    }
}
