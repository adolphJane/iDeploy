import SwiftUI
import AppKit

private struct DeploymentFormRequest: Identifiable {
    let id = UUID()
    let deployment: J?
    var source: J? = nil
    var environment: String? = nil
}

struct DeploymentsPage: View {
    @EnvironmentObject private var model: AppModel
    @State private var selectedID: String?
    @State private var runtime = J(nil)
    @State private var releases: [J] = []
    @State private var releaseJobs: [String: J] = [:]
    @State private var jobDetail: J?
    @State private var releaseMonitor: Task<Void, Never>?
    @State private var environment = J(nil)
    @State private var domainBindings: [J] = []
    @State private var pathRoutes: [J] = []
    @State private var editingPathRoute: J?
    @State private var showingPathEditor = false
    @State private var loadingDetail = false
    @State private var loadedDetailID: String?
    @State private var detailLoadID = UUID()
    @State private var formRequest: DeploymentFormRequest?
    @State private var publishTarget: J?
    @State private var showingEnvironment = false
    @State private var deleteTarget: J?
    @State private var rollbackTarget: J?

    private var selected: J? { model.deployments.first { $0.id == selectedID } }

    private var deploymentRows: [J] {
        var projects = Set<String>()
        return model.deployments.compactMap { deployment in
            guard deployment.deploymentEnvironment != "default" else { return deployment }
            guard projects.insert(deployment["projectId"].string).inserted else { return nil }
            return environments(for: deployment).first
        }
    }

    private func environments(for deployment: J) -> [J] {
        model.deployments.filter {
            $0["projectId"].string == deployment["projectId"].string && $0.deploymentEnvironment != "default"
        }.sorted { $0.deploymentEnvironment == "test" && $1.deploymentEnvironment == "production" }
    }

    private func selectDeployment(_ id: String?) {
        guard selectedID != id else { return }
        releaseMonitor?.cancel()
        releaseMonitor = nil
        releaseJobs = [:]
        runtime = J(nil)
        releases = []
        environment = J(nil)
        domainBindings = []
        pathRoutes = []
        loadedDetailID = nil
        detailLoadID = UUID()
        loadingDetail = id != nil
        selectedID = id
    }

    var body: some View {
        HStack(spacing: 0) {
            VStack(spacing: 0) {
                HStack {
                    WorkspaceListHeader(title: "部署", count: deploymentRows.count)
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
                        ForEach(deploymentRows, id: \.id) { deployment in
                            let named = deployment.deploymentEnvironment != "default"
                            let targets = named ? environments(for: deployment) : [deployment]
                            let isSelected = targets.contains { $0.id == selectedID }
                            Button {
                                if !isSelected { selectDeployment(deployment.id) }
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
                                        Text(named ? targets.map(\.deploymentEnvironmentLabel).joined(separator: " · ") : deployment.deploymentLocation)
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
                                       deployment.deploymentLocation)
                        Spacer()
                        if loadingDetail && loadedDetailID != deployment.id {
                            ProgressView()
                                .controlSize(.small)
                                .accessibilityLabel("正在读取部署状态")
                        } else {
                            StateBadge(state: runtime["status"].string)
                        }
                    }
                    if deployment.deploymentEnvironment != "default" {
                        let targets = environments(for: deployment)
                        HStack {
                            Picker("部署环境", selection: Binding(
                                get: { deployment.id },
                                set: { selectDeployment($0) }
                            )) {
                                ForEach(targets, id: \.id) { target in
                                    Text(target.deploymentEnvironmentLabel).tag(target.id)
                                }
                            }
                            .pickerStyle(.segmented)
                            .accessibilityLabel("部署环境")
                            if targets.count == 1 {
                                let next = deployment.deploymentEnvironment == "test" ? "production" : "test"
                                Button("添加" + DeploymentEnvironmentLabel.name(next)) {
                                    formRequest = DeploymentFormRequest(deployment: nil, source: deployment, environment: next)
                                }
                                .buttonStyle(DeploySecondaryButtonStyle())
                            }
                        }
                    }
                    HStack {
                        Button(deployment.deploymentPublishTitle) { publishTarget = deployment }
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
                        InfoTile(title: "最近成功版本",
                                 value: loadingDetail && loadedDetailID != deployment.id ? "…" : releases.first { $0["status"].string == "succeeded" }?["version"].string ?? "—",
                                 symbol: "clock.arrow.circlepath")
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
                            SectionHeading("域名与访问", "为当前部署配置独立域名或共享域名下的访问路径")
                            Spacer()
                            Button("添加访问路径") {
                                editingPathRoute = nil
                                showingPathEditor = true
                            }
                            .buttonStyle(DeploySecondaryButtonStyle())
                            Button("管理域名") { model.section = .domains }
                                .buttonStyle(DeploySecondaryButtonStyle())
                        }
                        if !pathRoutes.isEmpty {
                            Rectangle().fill(DeployStyle.line).frame(height: 1)
                            ForEach(pathRoutes, id: \.id) { route in
                                HStack(spacing: 10) {
                                    Text(route["fqdn"].string + route["pathPrefix"].string)
                                        .font(.system(size: 12, weight: .semibold))
                                        .textSelection(.enabled)
                                    Spacer()
                                    Text(route["status"].string == "active" ? "已应用" : route["status"].string == "draft" ? "草稿" : "处理中")
                                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                                    Button("管理路径") {
                                        editingPathRoute = route
                                        showingPathEditor = true
                                    }
                                    .buttonStyle(DeploySecondaryButtonStyle())
                                }
                            }
                        }
                        if domainBindings.isEmpty && pathRoutes.isEmpty {
                            Text("尚未配置域名访问。可在此添加访问路径，或前往域名页面绑定独立域名。")
                                .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                        }
                        if !domainBindings.isEmpty {
                            ForEach(domainBindings, id: \.hostID) { item in
                                HStack {
                                    Text(item["fqdn"].string).font(.system(size: 12, weight: .semibold))
                                    Spacer()
                                    Text("容器端口 \(item["containerPort"].int) · \(item["status"].string == "active" ? "入口已应用" : item["status"].string == "draft" ? "草稿" : "处理中")")
                                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                                }
                            }
                        }
                    }
                    .padding(18)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .deployCard(fill: DeployStyle.overviewPanel)
                    let databaseID = deployment["databaseConnectionId"].string
                    if deployment.deploymentEnvironment != "default" || !databaseID.isEmpty {
                        let database = model.databases.first { $0.id == databaseID }
                        HStack {
                            SectionHeading("数据库", database.map {
                                [$0["name"].string, $0["host"].string + ":" + $0["port"].string, $0["databaseName"].string]
                                    .filter { !$0.isEmpty }.joined(separator: " · ")
                            } ?? "尚未绑定数据库")
                            Spacer()
                            if let database {
                                let state = !database["lastError"].string.isEmpty ? "连接异常" :
                                    database["lastConnectedAt"].string.isEmpty ? "待测试" : "连接已验证"
                                Text(state).foregroundStyle(DeployStyle.muted)
                            }
                        }
                        .padding(18)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .deployCard(fill: DeployStyle.overviewPanel)
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
                                        if !release["jobId"].string.isEmpty &&
                                            !["succeeded", "failed", "canceled"].contains(release["status"].string) {
                                            let job = releaseJobs[release["jobId"].string]
                                            let message = job?["message"].string ?? ""
                                            HStack(spacing: 8) {
                                                Text(message.isEmpty ? "正在读取任务进度…" : message)
                                                    .font(.system(size: 11))
                                                    .foregroundStyle(DeployStyle.muted)
                                                    .lineLimit(2)
                                                Spacer(minLength: 4)
                                                Text("\(job?["progress"].int ?? 0)%")
                                                    .font(.system(size: 11, weight: .semibold, design: .monospaced))
                                                    .foregroundStyle(DeployStyle.green)
                                            }
                                            ProgressView(value: Double(job?["progress"].int ?? 0), total: 100)
                                                .tint(DeployStyle.green)
                                                .accessibilityLabel("发布进度")
                                        }
                                    }
                                    Spacer()
                                    if let job = releaseJobs[release["jobId"].string] {
                                        Button("任务详情") { jobDetail = job }
                                            .buttonStyle(DeploySecondaryButtonStyle())
                                    }
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
        .onDisappear {
            releaseMonitor?.cancel()
            releaseMonitor = nil
        }
        .sheet(item: $formRequest) { request in
            DeploymentFormView(deployment: request.deployment, source: request.source,
                               requestedEnvironment: request.environment) { id in
                selectDeployment(id)
            }
            .environmentObject(model)
            .id(request.id)
        }
        .sheet(item: $publishTarget) { target in
            PublishView(deployment: target) { job in
                guard selectedID == target.id else { return }
                releaseJobs[job.id] = job
                loadReleaseList(target.id)
            }
            .environmentObject(model)
        }
        .sheet(item: $jobDetail) { job in
            JobDetailSheet(job: job).environmentObject(model)
        }
        .sheet(isPresented: $showingEnvironment) {
            if let selected {
                EnvironmentEditorView(deployment: selected) {
                    if selectedID == selected.id { environment = $0 }
                }
                .environmentObject(model)
            }
        }
        .sheet(isPresented: $showingPathEditor) {
            if let selected {
                SharedPathEditor(route: editingPathRoute, fixedDeploymentID: selected.id) {
                    loadPathRoutes(selected.id)
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
                    let other = environments(for: target).first { $0.id != target.id }
                    selectDeployment(other?.id)
                    deleteTarget = nil
                }
            }
        }
        .confirmationDialog("回滚到这个版本？", isPresented: Binding(
            get: { rollbackTarget != nil },
            set: { if !$0 { rollbackTarget = nil } }
        )) {
            Button("开始回滚", role: .destructive) {
                guard let target = rollbackTarget, let deployment = selected, target["deploymentId"].string == deployment.id else { return }
                Task {
                    await model.perform("RollbackDeployment", [deployment.id, target.id])
                    rollbackTarget = nil
                    loadDetail(deployment.id)
                }
            }
        } message: {
            Text("\(selected?.deploymentEnvironmentLabel ?? "部署") · \(selected?["serverName"].string ?? "") · \(rollbackTarget?["version"].string ?? "")。将以所选发布制品重建远程工作负载。")
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
            let nextDomainBindings = (await model.perform("ListDeploymentDomains", [id], refreshAfter: false) ?? J(nil)).array
            let nextPathRoutes = (try? await NativeBridge.call("ListSharedPathRoutes").array) ?? []
            guard selectedID == id, detailLoadID == requestID else { return }
            runtime = nextRuntime
            releases = nextReleases
            environment = nextEnvironment
            domainBindings = nextDomainBindings
            pathRoutes = nextPathRoutes.filter { $0["deploymentId"].string == id }
            loadedDetailID = id
            loadingDetail = false
            monitorReleaseJobs(id)
        }
    }

    private func loadPathRoutes(_ id: String) {
        Task {
            guard let routes = try? await NativeBridge.call("ListSharedPathRoutes").array, selectedID == id else { return }
            pathRoutes = routes.filter { $0["deploymentId"].string == id }
        }
    }

    private func loadReleaseList(_ id: String) {
        Task {
            guard let result = try? await NativeBridge.call("ListDeploymentReleases", [id]), selectedID == id else { return }
            releases = result.array
            monitorReleaseJobs(id)
        }
    }

    private func monitorReleaseJobs(_ id: String) {
        releaseMonitor?.cancel()
        let active = releases.filter { !["succeeded", "failed", "canceled"].contains($0["status"].string) && !$0["jobId"].string.isEmpty }
        guard !active.isEmpty else { return }
        releaseMonitor = Task {
            while !Task.isCancelled && selectedID == id {
                let activeReleases = releases.filter { !["succeeded", "failed", "canceled"].contains($0["status"].string) && !$0["jobId"].string.isEmpty }
                if activeReleases.isEmpty { break }
                for release in activeReleases {
                    if Task.isCancelled { break }
                    let jobID = release["jobId"].string
                    if let job = try? await NativeBridge.call("GetJob", [jobID]), selectedID == id {
                        releaseJobs[jobID] = job
                    }
                }
                if let latest = try? await NativeBridge.call("ListDeploymentReleases", [id]), selectedID == id {
                    releases = latest.array
                }
                try? await Task.sleep(nanoseconds: 2_000_000_000)
            }
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
    var hostID: String { self["hostId"].string }
}

struct DeploymentFormView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let deployment: J?
    let source: J?
    let requestedEnvironment: String?
    let onCreated: (String) -> Void
    @State private var deploymentEnvironment: String
    @State private var databaseID: String
    @State private var projectID: String
    @State private var serverID: String
    @State private var workloadKind: String
    @State private var workloadName: String
    @State private var ports: String
    @State private var restartPolicy: String
    @State private var buildMode: String

    init(deployment: J?, source: J? = nil, requestedEnvironment: String? = nil,
         onCreated: @escaping (String) -> Void = { _ in }) {
        self.deployment = deployment
        self.source = source
        self.requestedEnvironment = requestedEnvironment
        self.onCreated = onCreated
        _deploymentEnvironment = State(initialValue: deployment?.deploymentEnvironment ?? requestedEnvironment ?? "default")
        _databaseID = State(initialValue: deployment?["databaseConnectionId"].string ?? "")
        _projectID = State(initialValue: (deployment ?? source)?["projectId"].string ?? "")
        _serverID = State(initialValue: deployment?["serverId"].string ?? "")
        _workloadKind = State(initialValue: (deployment ?? source)?["workloadKind"].string ?? "container")
        _workloadName = State(initialValue: deployment?["workloadName"].string ?? "")
        _ports = State(initialValue: deployment?["containerPorts"].array.map(\.string).joined(separator: ", ") ?? "")
        _restartPolicy = State(initialValue: (deployment ?? source)?["restartPolicy"].string ?? "unless-stopped")
        _buildMode = State(initialValue: (deployment ?? source)?["buildMode"].string ?? "local")
    }

    private var availableServers: [J] {
        let occupied = Set(model.deployments.filter {
            $0["projectId"].string == projectID && $0.id != deployment?.id
        }.map { $0["serverId"].string })
        return model.servers.filter { !occupied.contains($0.id) }
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
                    Text(deployment != nil ? "编辑部署配置" : requestedEnvironment.map { "添加" + DeploymentEnvironmentLabel.name($0) } ?? "添加部署")
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
                                .disabled(deployment != nil || source != nil)
                                .frame(maxWidth: .infinity, alignment: .leading)
                            }
                            VStack(alignment: .leading, spacing: 7) {
                                Text("服务器").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                Picker("服务器", selection: $serverID) {
                                    Text("选择服务器").tag("")
                                    ForEach(availableServers, id: \.id) { server in
                                        Text(server["name"].string).tag(server.id)
                                    }
                                }
                                .labelsHidden()
                                .disabled(deployment != nil)
                                .frame(maxWidth: .infinity, alignment: .leading)
                            }
                        }
                        Picker("部署环境", selection: $deploymentEnvironment) {
                            ForEach(DeploymentEnvironmentLabel.values, id: \.self) { value in
                                Text(DeploymentEnvironmentLabel.name(value)).tag(value)
                            }
                        }
                        .disabled(requestedEnvironment != nil)
                        Picker("关联数据库", selection: $databaseID) {
                            Text("未绑定").tag("")
                            ForEach(model.databases.filter { $0.deploymentEnvironment == deploymentEnvironment }, id: \.id) { database in
                                Text(database["name"].string + " · " + database["host"].string).tag(database.id)
                            }
                        }
                        Picker("构建位置", selection: $buildMode) {
                            Text("本机构建").tag("local")
                            Text("iDeploy 服务构建").tag("remote")
                        }
                        .accessibilityLabel("发布构建位置")
                        if buildMode == "remote" {
                            Text("远端构建要求 Git 项目；发布前会将部署文件推送到 Git，再由 iDeploy 服务构建。")
                                .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
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
                            "restartPolicy": restartPolicy, "buildMode": buildMode,
                            "environment": deploymentEnvironment, "databaseConnectionId": databaseID
                        ]
                        let result = await model.perform(deployment == nil ? "CreateDeployment" : "ConfigureDeployment",
                                                         deployment == nil ? [input] : [deployment!.id, input])
                        if let result {
                            if deployment == nil { onCreated(result.id) }
                            dismiss()
                        }
                    }
                }
                .buttonStyle(DeployPrimaryButtonStyle())
                .keyboardShortcut(.defaultAction)
                .disabled(projectID.isEmpty || !availableServers.contains(where: { $0.id == serverID }) || workloadName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ||
                          (buildMode == "remote" && model.projects.first(where: { $0.id == projectID })?["sourceKind"].string != "git"))
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 16)
            .background(DeployStyle.overviewPanel)
        }
        .frame(width: 650, height: workloadKind == "compose" ? 570 : 630)
        .onChange(of: projectID) { _ in
            if !availableServers.contains(where: { $0.id == serverID }) { serverID = "" }
        }
        .onChange(of: deploymentEnvironment) { _ in databaseID = "" }
        .environment(\.colorScheme, .light)
    }
}

struct PublishView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let deployment: J
    let onPublished: (J) -> Void
    @State private var registryID = ""
    @State private var version = ""
    @State private var readyIngresses: [String] = []
    @State private var ingressFQDN = ""
    @State private var publishPath = ""
    @State private var containerPort = ""
    @State private var serviceName = ""
    @State private var ingressError = ""
    @State private var preflight = J(nil)
    @State private var preflightProgress = J(nil)
    @State private var activePreflightID = ""
    @State private var checkStartedAt = Date()
    @State private var checking = false
    @State private var publishing = false
    @State private var publishStage = ""
    @State private var checkError = ""
    @State private var publishError = ""
    @State private var copied = false
    @State private var confirmingProduction = false

    private var trimmedVersion: String { version.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var canPublish: Bool { !publishing && !checking && !registryID.isEmpty && !trimmedVersion.isEmpty && preflight["ok"].bool && !ingressFQDN.isEmpty && !publishPath.isEmpty && (Int(containerPort) ?? 0) > 0 && (Int(containerPort) ?? 0) <= 65535 }
    private var visibleChecks: [J] { checking ? preflightProgress["checks"].array : preflight["checks"].array }
    private var checkSummary: String {
        let heading = checking ? "发布前检查：进行中" :
            preflight["ok"].bool ? "发布前检查：已就绪" : "发布前检查：需要处理"
        let lines = visibleChecks.map { check in
            let status = check["status"].string
            return "[\(status)] \(check["detail"].string)"
        }
        return ([heading] + lines).joined(separator: "\n")
    }

    var body: some View {
        VStack(spacing: 0) {
            SectionHeading(deployment.deploymentPublishTitle, deployment.deploymentDisplayName + " · " + deployment.deploymentLocation)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(24)
                .background(DeployStyle.overviewHeader)
            Rectangle().fill(DeployStyle.line).frame(height: 1)
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    VStack(alignment: .leading, spacing: 14) {
                        Text("发布配置")
                            .font(.system(size: 14, weight: .semibold))
                            .foregroundStyle(DeployStyle.ink)
                        Text(deployment["buildMode"].string == "remote" ? "构建位置：iDeploy 服务" : "构建位置：本机")
                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                        Picker("镜像仓库", selection: $registryID) {
                            Text("选择镜像仓库").tag("")
                            ForEach(model.registries, id: \.id) { registry in
                                Text(registry["name"].string).tag(registry.id)
                            }
                        }
                        .disabled(checking || publishing)
                        TextField("版本", text: $version)
                            .textFieldStyle(.roundedBorder)
                            .accessibilityLabel("发布版本")
                        Rectangle().fill(DeployStyle.line).frame(height: 1)
                        Text("应用 HTTPS 路径")
                            .font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                        if readyIngresses.isEmpty {
                            Text(ingressError.isEmpty ? "请先到设置 → 应用入口，完成目标服务器的域名和 HTTPS 配置。" : ingressError)
                                .font(.system(size: 11)).foregroundStyle(.orange)
                        } else {
                            Picker("入口域名", selection: $ingressFQDN) {
                                Text("选择已就绪域名").tag("")
                                ForEach(readyIngresses, id: \.self) { value in Text(value).tag(value) }
                            }
                            TextField("项目路径名，例如 demo", text: $publishPath)
                                .textFieldStyle(.roundedBorder)
                                .accessibilityLabel("项目发布路径")
                            TextField("应用容器内部 HTTP 端口", text: $containerPort)
                                .textFieldStyle(.roundedBorder)
                                .accessibilityLabel("应用容器端口")
                            TextField("容器名称或 Compose 服务名（多个后端时填写）", text: $serviceName)
                                .textFieldStyle(.roundedBorder)
                            Text("发布成功后访问 https://\(ingressFQDN.isEmpty ? "域名" : ingressFQDN)/\(publishPath.trimmingCharacters(in: CharacterSet(charactersIn: "/")))/。单容器构建和运行会自动收到 IDEPLOY_BASE_PATH；Compose 服务需在配置中引用它。项目应据此配置资源、路由和 API 路径。仅支持单级路径；更换已有生效路径前需先解除旧路由。")
                                .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                        }
                    }
                    .padding(18)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .deployCard()

                    VStack(alignment: .leading, spacing: 12) {
                        HStack {
                            Text("发布前检查")
                                .font(.system(size: 14, weight: .semibold))
                                .foregroundStyle(DeployStyle.ink)
                            Spacer()
                            if !visibleChecks.isEmpty {
                                Button(copied ? "已复制" : "复制检查结果") {
                                    NSPasteboard.general.clearContents()
                                    copied = NSPasteboard.general.setString(checkSummary, forType: .string)
                                }
                                .buttonStyle(DeploySecondaryButtonStyle())
                            }
                        }
                        if checking {
                            VStack(alignment: .leading, spacing: 10) {
                                HStack(spacing: 10) {
                                    ProgressView().controlSize(.small)
                                    Text(preflightProgress["detail"].string.isEmpty ? "正在启动发布前检查…" : preflightProgress["detail"].string)
                                        .foregroundStyle(DeployStyle.ink)
                                }
                                ProgressView(value: Double(max(0, preflightProgress["stage"].int - 1)), total: 5)
                                    .tint(DeployStyle.green)
                                TimelineView(.periodic(from: .now, by: 1)) { timeline in
                                    Text("阶段 \(max(1, preflightProgress["stage"].int))/5 · 已完成 \(visibleChecks.count) 项 · 已用 \(Int(timeline.date.timeIntervalSince(checkStartedAt))) 秒 / 最多 180 秒")
                                        .foregroundStyle(DeployStyle.muted)
                                }
                                ForEach(visibleChecks, id: \.checkID) { check in
                                    HStack(alignment: .top, spacing: 8) {
                                        Image(systemName: check["status"].string == "failed" ? "xmark.circle.fill" :
                                                check["status"].string == "warning" ? "exclamationmark.triangle.fill" : "checkmark.circle.fill")
                                            .foregroundStyle(check["status"].string == "failed" ? .red :
                                                             check["status"].string == "warning" ? .orange : DeployStyle.green)
                                        Text(check["detail"].string)
                                            .foregroundStyle(DeployStyle.ink)
                                            .textSelection(.enabled)
                                            .frame(maxWidth: .infinity, alignment: .leading)
                                    }
                                }
                            }
                        } else if !checkError.isEmpty {
                            Text(checkError).foregroundStyle(.red).textSelection(.enabled)
                        } else if preflight.isNull {
                            Text("选择镜像仓库后运行检查，确认部署环境就绪。")
                                .foregroundStyle(DeployStyle.muted)
                        } else {
                            Text(preflight["ok"].bool ? "检查通过，可以开始发布。" : "检查未通过，请先处理下列失败项。")
                                .font(.system(size: 12, weight: .medium))
                                .foregroundStyle(preflight["ok"].bool ? DeployStyle.green : .orange)
                            ForEach(preflight["checks"].array, id: \.checkID) { check in
                                HStack(alignment: .top, spacing: 10) {
                                    Image(systemName: check["status"].string == "failed" ? "xmark.circle.fill" :
                                            check["status"].string == "warning" ? "exclamationmark.triangle.fill" : "checkmark.circle.fill")
                                        .foregroundStyle(check["status"].string == "failed" ? .red :
                                                         check["status"].string == "warning" ? .orange : DeployStyle.green)
                                    Text(check["detail"].string)
                                        .foregroundStyle(DeployStyle.ink)
                                        .textSelection(.enabled)
                                        .frame(maxWidth: .infinity, alignment: .leading)
                                }
                            }
                        }
                    }
                    .font(.system(size: 12))
                    .padding(18)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .deployCard()
                }
                .padding(24)
            }
            .background(DeployStyle.canvas)
            Rectangle().fill(DeployStyle.line).frame(height: 1)
            if publishing || !publishError.isEmpty {
                HStack(alignment: .top, spacing: 10) {
                    if publishing { ProgressView().controlSize(.small) }
                    Text(publishing ? publishStage : publishError)
                        .foregroundStyle(publishing ? DeployStyle.ink : .red)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    if !publishError.isEmpty {
                        Button("复制错误") {
                            NSPasteboard.general.clearContents()
                            _ = NSPasteboard.general.setString(publishError, forType: .string)
                        }
                        .buttonStyle(DeploySecondaryButtonStyle())
                    }
                }
                .font(.system(size: 12))
                .padding(.horizontal, 24)
                .padding(.vertical, 12)
                .background(DeployStyle.overviewPanel)
            }
            HStack(spacing: 12) {
                Button("检查就绪状态") { runPreflight() }
                    .buttonStyle(DeploySecondaryButtonStyle())
                    .disabled(registryID.isEmpty || ingressFQDN.isEmpty || checking || publishing)
                if readyIngresses.isEmpty {
                    Text("请先配置应用入口").foregroundStyle(DeployStyle.muted)
                } else if registryID.isEmpty {
                    Text("请先选择镜像仓库").foregroundStyle(DeployStyle.muted)
                } else if trimmedVersion.isEmpty {
                    Text("请填写发布版本").foregroundStyle(DeployStyle.muted)
                } else if !checking && preflight.isNull {
                    Text(checkError.isEmpty ? "检查通过后才能发布" : "检查失败，请重试")
                        .foregroundStyle(DeployStyle.muted)
                } else if !checking && !preflight["ok"].bool {
                    Text("请处理检查失败项").foregroundStyle(DeployStyle.muted)
                }
                Spacer()
                Button("取消") { dismiss() }
                    .buttonStyle(DeploySecondaryButtonStyle(height: 34))
                    .disabled(publishing)
                Button(publishing ? "正在提交…" : deployment.deploymentPublishTitle) {
                    if deployment.deploymentEnvironment == "production" {
                        confirmingProduction = true
                    } else {
                        publish()
                    }
                }
                    .buttonStyle(DeployPrimaryButtonStyle())
                    .disabled(!canPublish)
            }
            .font(.system(size: 12))
            .padding(.horizontal, 24)
            .padding(.vertical, 16)
            .background(DeployStyle.overviewPanel)
        }
        .alert("确认发布到线上环境？", isPresented: $confirmingProduction) {
            Button("取消", role: .cancel) {}
            Button("发布到线上环境") { publish() }
        } message: {
            Text("\(deployment.deploymentDisplayName) · \(deployment.deploymentLocation) · \(trimmedVersion)\nhttps://\(ingressFQDN)/\(publishPath.trimmingCharacters(in: CharacterSet(charactersIn: "/")))")
        }
        .frame(width: 680, height: 680)
        .environment(\.colorScheme, .light)
        .task {
            if model.registries.count == 1 { registryID = model.registries[0].id }
            do {
                readyIngresses = try await NativeBridge.call("ListInternalIngresses").array
                    .filter { $0["status"].string == "ready" && $0["serverId"].string == deployment["serverId"].string }
                    .map { $0["fqdn"].string }
                if readyIngresses.count == 1 { ingressFQDN = readyIngresses[0] }
            } catch { ingressError = error.localizedDescription }
            if let existing = try? await NativeBridge.call("ListSharedPathRoutes"),
               let route = existing.array.first(where: { $0["deploymentId"].string == deployment.id && $0["status"].string == "active" }),
               readyIngresses.contains(route["fqdn"].string) {
                ingressFQDN = route["fqdn"].string
                publishPath = route["pathPrefix"].string.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
                containerPort = route["containerPort"].string
                serviceName = route["serviceName"].string
            }
            let ports = deployment["containerPorts"].array.map(\.string)
            if containerPort.isEmpty, let first = ports.first {
                containerPort = first.split(separator: ":").last.map(String.init) ?? ""
            }
            if let suggestion = try? await NativeBridge.call("DetectDeploymentVersion", [deployment.id]), version.isEmpty {
                version = suggestion["version"].string
            }
        }
        .onChange(of: registryID) { _ in
            preflight = J(nil)
            checkError = ""
            copied = false
        }
        .onChange(of: ingressFQDN) { _ in preflight = J(nil) }
        .onChange(of: publishPath) { _ in preflight = J(nil) }
        .onChange(of: containerPort) { _ in preflight = J(nil) }
        .onChange(of: serviceName) { _ in preflight = J(nil) }
        .onDisappear {
            activePreflightID = ""
        }
    }

    private func runPreflight() {
        guard !registryID.isEmpty else { return }
        guard !ingressFQDN.isEmpty, !publishPath.isEmpty, let port = Int(containerPort), port > 0, port <= 65535 else {
            checkError = "请先选择入口域名、项目路径和有效的容器端口。"
            return
        }
        let selectedRegistryID = registryID
        let selectedFQDN = ingressFQDN
        let selectedPath = publishPath
        let selectedService = serviceName
        let selectedPort = port
        checking = true
        checkStartedAt = Date()
        checkError = ""
        preflight = J(nil)
        preflightProgress = J(nil)
        copied = false
        let requestID = UUID().uuidString
        activePreflightID = requestID
        Task {
            while checking && activePreflightID == requestID {
                if let next = try? await NativeBridge.call("GetPreflightProgress", [requestID], timeoutSeconds: 5),
                   checking && activePreflightID == requestID && next["stage"].int > 0 {
                    preflightProgress = next
                }
                try? await Task.sleep(nanoseconds: 500_000_000)
            }
        }
        Task {
            defer { checking = false }
            do {
                preflight = try await NativeBridge.call("PreflightDeployment",
                    [["deploymentId": deployment.id, "registryId": selectedRegistryID, "requestId": requestID,
                      "fqdn": selectedFQDN, "pathPrefix": selectedPath, "serviceName": selectedService, "containerPort": selectedPort]],
                    timeoutSeconds: 195)
                if registryID != selectedRegistryID || ingressFQDN != selectedFQDN || publishPath != selectedPath || serviceName != selectedService || (Int(containerPort) ?? 0) != selectedPort { preflight = J(nil) }
            } catch {
                if registryID == selectedRegistryID { checkError = error.localizedDescription }
            }
        }
    }

    private func publish() {
        guard canPublish else { return }
        guard !ingressFQDN.isEmpty, !publishPath.isEmpty, let port = Int(containerPort), port > 0, port <= 65535 else {
            publishError = "请选择已就绪的入口域名、项目路径和有效的容器端口。"
            return
        }
        guard !registryID.isEmpty else {
            publishError = "请先选择镜像仓库。"
            return
        }
        guard !trimmedVersion.isEmpty else {
            publishError = "请先填写发布版本。"
            return
        }
        guard !preflight.isNull else {
            publishError = checkError.isEmpty ? "请先点击“检查就绪状态”，检查通过后再发布。" : "发布前检查失败：\(checkError)"
            return
        }
        guard preflight["ok"].bool else {
            publishError = "发布前检查未通过，请处理失败项后重新检查。"
            return
        }
        publishing = true
        let remoteBuild = deployment["buildMode"].string == "remote"
        publishStage = remoteBuild ? "正在检查本机 SSH、Git 与远端服务…" : "正在检查本机 SSH、Docker 和 Git…"
        publishError = ""
        Task {
            defer { publishing = false }
            do {
                let overview = try await NativeBridge.call("GetOverview", timeoutSeconds: 30)
                let tools = overview["tools"].array
                guard !tools.isEmpty else {
                    publishError = "未能读取本机工具状态，请重试。"
                    return
                }
                let requiredTools = remoteBuild ? ["SSH", "Git"] : ["SSH", "Docker", "Git"]
                let unavailable = tools.filter { requiredTools.contains($0["name"].string) && !$0["available"].bool }
                guard unavailable.isEmpty else {
                    publishError = "本机发布环境未就绪：\n" + unavailable.map {
                        let name = $0["name"].string
                        let suggestion = name == "Docker" ? "请启动 Docker Desktop，等待引擎就绪后重试。" : "请安装或修复该工具后重试。"
                        return "• \(name)：\($0["detail"].string)\n  \(suggestion)"
                    }.joined(separator: "\n")
                    return
                }
                publishStage = "本机工具已就绪，正在创建发布任务…"
                let input: [String: Any] = [
                    "deploymentId": deployment.id, "registryId": registryID, "version": trimmedVersion,
                    "expectedEnvironment": deployment.deploymentEnvironment,
                    "fqdn": ingressFQDN, "pathPrefix": publishPath, "serviceName": serviceName, "containerPort": port
                ]
                let job = try await NativeBridge.call("PublishDeployment", [input], timeoutSeconds: 140)
                onPublished(job)
                dismiss()
            } catch {
                publishError = error.localizedDescription
            }
        }
    }
}

private extension J {
    var checkID: String { self["id"].string }
}

struct EnvironmentEditorView: View {
    @Environment(\.dismiss) private var dismiss
    let deployment: J
    let onChanged: (J) -> Void
    @State private var environment = J(nil)
    @State private var variables: [J] = []
    @State private var search = ""
    @State private var editingName: String?
    @State private var name = ""
    @State private var value = ""
    @State private var kind = "plain"
    @State private var buildSecret = false
    @State private var loading = false
    @State private var saving = false
    @State private var error = ""
    @State private var deleteTarget: J?
    @FocusState private var nameFocused: Bool
    @FocusState private var valueFocused: Bool

    private var composeBuildOnly: Bool { deployment["workloadKind"].string == "compose" && deployment["buildMode"].string == "remote" }
    private var supportsVariables: Bool { deployment["workloadKind"].string == "container" || composeBuildOnly }
    private var current: J? { variables.first { $0["name"].string == editingName } }
    private var filteredVariables: [J] {
        let query = search.trimmingCharacters(in: .whitespacesAndNewlines)
        return query.isEmpty ? variables : variables.filter { $0["name"].string.localizedCaseInsensitiveContains(query) }
    }
    private var normalizedName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var validName: Bool {
        normalizedName.range(of: "^[A-Za-z_][A-Za-z0-9_]{0,127}$", options: .regularExpression) != nil
    }
    private var duplicateName: Bool {
        editingName == nil && variables.contains { $0["name"].string == normalizedName }
    }
    private var canSave: Bool {
        supportsVariables && (!composeBuildOnly || (kind == "secret" && buildSecret)) && validName && !duplicateName && !saving && !loading &&
            (kind == "plain" || !value.isEmpty || (current?["kind"].string == "secret" && current?["configured"].bool == true))
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                SectionHeading("环境变量", deployment["projectName"].string)
                Spacer()
                Text("\(variables.count) 个变量")
                    .font(.system(size: 11))
                    .foregroundStyle(DeployStyle.muted)
            }
            .padding(24)
            .background(DeployStyle.overviewHeader)
            Rectangle().fill(DeployStyle.line).frame(height: 1)
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    if environment["pendingApply"].bool {
                        Label("变更已保存，将在下次发布或重新部署时生效", systemImage: "clock.arrow.circlepath")
                            .font(.system(size: 12))
                            .foregroundStyle(.orange)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    if !supportsVariables {
                        Label("当前部署类型不能配置环境变量；远端 Compose 可配置 BuildKit 构建秘密。", systemImage: "info.circle")
                            .font(.system(size: 12))
                            .foregroundStyle(DeployStyle.muted)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    VStack(alignment: .leading, spacing: 14) {
                        HStack {
                            Text("已配置变量")
                                .font(.system(size: 14, weight: .semibold))
                                .foregroundStyle(DeployStyle.ink)
                            Spacer()
                            Button("新增变量") { resetEditor(focus: true) }
                                .buttonStyle(DeploySecondaryButtonStyle())
                                .disabled(saving || !supportsVariables)
                        }
                        if variables.count > 6 {
                            TextField("搜索变量名", text: $search)
                                .textFieldStyle(.roundedBorder)
                                .accessibilityLabel("搜索环境变量")
                        }
                        if loading {
                            HStack(spacing: 8) {
                                ProgressView().controlSize(.small)
                                Text("正在读取环境变量…")
                            }
                            .foregroundStyle(DeployStyle.muted)
                        } else if variables.isEmpty {
                            Text("尚未配置环境变量。可在下方添加普通值或保存在钥匙串中的秘密值。")
                                .foregroundStyle(DeployStyle.muted)
                        } else if filteredVariables.isEmpty {
                            Text("没有匹配的变量")
                                .foregroundStyle(DeployStyle.muted)
                        } else {
                            VStack(spacing: 0) {
                                ForEach(filteredVariables, id: \.selfName) { item in
                                    HStack(spacing: 10) {
                                        Button {
                                            select(item)
                                        } label: {
                                            HStack(spacing: 10) {
                                                Image(systemName: item["kind"].string == "secret" ? "lock.fill" : "curlybraces")
                                                    .foregroundStyle(DeployStyle.green)
                                                    .frame(width: 20)
                                                VStack(alignment: .leading, spacing: 4) {
                                                    Text(item["name"].string)
                                                        .font(.system(size: 12, weight: .semibold, design: .monospaced))
                                                        .foregroundStyle(DeployStyle.ink)
                                                    Text(composeBuildOnly ? "秘密值 · 仅远端构建时" : item["kind"].string == "secret" ? (item["buildSecret"].bool ? "秘密值 · 运行时及远端构建时" : "秘密值 · 运行时") : item["value"].string.isEmpty ? "普通值 · 空值" : "普通值 · \(item["value"].string)")
                                                        .font(.system(size: 11))
                                                        .foregroundStyle(DeployStyle.muted)
                                                        .lineLimit(1)
                                                }
                                                Spacer(minLength: 0)
                                                if editingName == item["name"].string {
                                                    Text("编辑中")
                                                        .font(.system(size: 10, weight: .medium))
                                                        .foregroundStyle(DeployStyle.green)
                                                }
                                            }
                                            .contentShape(Rectangle())
                                        }
                                        .buttonStyle(.plain)
                                        .disabled(saving || !supportsVariables)
                                        .accessibilityLabel("编辑环境变量 \(item["name"].string)")
                                        Button("编辑") { select(item) }
                                            .buttonStyle(DeploySecondaryButtonStyle())
                                            .disabled(saving || !supportsVariables)
                                            .accessibilityLabel("编辑环境变量 \(item["name"].string)")
                                        Button {
                                            deleteTarget = item
                                        } label: {
                                            Image(systemName: "trash")
                                        }
                                        .buttonStyle(.borderless)
                                        .foregroundStyle(.red)
                                        .help("删除 \(item["name"].string)")
                                        .accessibilityLabel("删除环境变量 \(item["name"].string)")
                                        .disabled(saving || !supportsVariables)
                                    }
                                    .padding(.horizontal, 12)
                                    .padding(.vertical, 10)
                                    .background(editingName == item["name"].string ? DeployStyle.greenSoft : Color.clear,
                                                in: RoundedRectangle(cornerRadius: 8))
                                    if item.selfName != filteredVariables.last?.selfName {
                                        Rectangle().fill(DeployStyle.line).frame(height: 1)
                                    }
                                }
                            }
                        }
                    }
                    .font(.system(size: 12))
                    .padding(18)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .deployCard()

                    VStack(alignment: .leading, spacing: 14) {
                        Text(editingName == nil ? "新增变量" : "编辑 \(editingName ?? "")")
                            .font(.system(size: 14, weight: .semibold))
                            .foregroundStyle(DeployStyle.ink)
                        VStack(alignment: .leading, spacing: 6) {
                            Text("变量名")
                                .font(.system(size: 12, weight: .medium))
                                .foregroundStyle(DeployStyle.ink)
                            TextField("例如 DATABASE_URL", text: $name)
                                .textFieldStyle(.roundedBorder)
                                .font(.system(.body, design: .monospaced))
                                .focused($nameFocused)
                                .disabled(editingName != nil || saving)
                            if !name.isEmpty && !validName {
                                Text("以字母或下划线开头，只能包含字母、数字和下划线，最长 128 个字符。")
                                    .foregroundStyle(.red)
                            } else if duplicateName {
                                Text("该变量已存在，请在上方列表中选择它进行编辑。")
                                    .foregroundStyle(.red)
                            }
                        }
                        Picker("类型", selection: Binding(
                            get: { kind },
                            set: { newKind in
                                kind = newKind
                                value = ""
                            }
                        )) {
                            if !composeBuildOnly { Text("普通值").tag("plain") }
                            Text("秘密值").tag("secret")
                        }
                        .disabled(saving)
                        if kind == "secret" {
                            Toggle("远端构建时作为 BuildKit secret 提供", isOn: $buildSecret)
                                .toggleStyle(.checkbox)
                                .disabled(composeBuildOnly)
                            Text(composeBuildOnly ? "Compose 文件需在 build.secrets 引用变量名，并在顶层 secrets 声明 environment: 变量名；秘密值不会写入 Git。" : "Dockerfile 可使用 RUN --mount=type=secret,id=变量名 读取；秘密值不会写入 Git。")
                                .foregroundStyle(DeployStyle.muted)
                        }
                        VStack(alignment: .leading, spacing: 6) {
                            Text(kind == "secret" ? "秘密值" : "值")
                                .font(.system(size: 12, weight: .medium))
                                .foregroundStyle(DeployStyle.ink)
                            if kind == "secret" {
                                SecureField(editingName == nil ? "输入秘密值" : "留空则保留当前秘密值", text: $value)
                                    .textFieldStyle(.roundedBorder)
                                    .focused($valueFocused)
                                    .disabled(saving)
                                Text(current?["kind"].string == "secret" ? "留空会保留现有钥匙串凭据；输入新值会替换它。" : "秘密值将存入 macOS 钥匙串，不会在列表中显示。")
                                    .foregroundStyle(DeployStyle.muted)
                            } else {
                                TextField("输入变量值，可留空", text: $value)
                                    .textFieldStyle(.roundedBorder)
                                    .focused($valueFocused)
                                    .disabled(saving)
                                Text("普通值可留空，并会显示在部署详情中。")
                                    .foregroundStyle(DeployStyle.muted)
                            }
                            if current?["kind"].string == "secret" && kind == "plain" {
                                Text("保存后会移除原来的钥匙串凭据。")
                                    .foregroundStyle(.orange)
                            }
                        }
                        if !error.isEmpty {
                            Text(error).foregroundStyle(.red).textSelection(.enabled)
                        }
                    }
                    .font(.system(size: 11))
                    .padding(18)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .deployCard()
                }
                .padding(24)
            }
            .background(DeployStyle.canvas)
            Rectangle().fill(DeployStyle.line).frame(height: 1)
            HStack(spacing: 12) {
                if saving {
                    ProgressView().controlSize(.small)
                    Text("正在保存…").foregroundStyle(DeployStyle.muted)
                } else {
                    Text("修改后需重新发布，运行中的容器不会立即变化。")
                        .foregroundStyle(DeployStyle.muted)
                }
                Spacer()
                Button("完成") { dismiss() }
                    .buttonStyle(DeploySecondaryButtonStyle(width: 110, height: 34))
                    .disabled(saving)
                Button(editingName == nil ? "添加变量" : "保存修改") { save() }
                    .buttonStyle(DeployPrimaryButtonStyle(width: 110))
                    .keyboardShortcut(.defaultAction)
                    .disabled(!canSave)
            }
            .font(.system(size: 11))
            .padding(.horizontal, 24)
            .padding(.vertical, 16)
            .background(DeployStyle.overviewPanel)
        }
        .frame(width: 680, height: 620)
        .environment(\.colorScheme, .light)
        .task { resetEditor(); load() }
        .confirmationDialog("删除环境变量 \(deleteTarget?["name"].string ?? "")？", isPresented: Binding(
            get: { deleteTarget != nil },
            set: { if !$0 { deleteTarget = nil } }
        )) {
            Button("删除变量", role: .destructive) {
                guard let target = deleteTarget else { return }
                remove(target)
            }
        } message: {
            Text("删除后，下次发布或重新部署时生效。")
        }
    }

    private func select(_ item: J) {
        editingName = item["name"].string
        name = item["name"].string
        kind = item["kind"].string
        buildSecret = item["buildSecret"].bool
        value = kind == "secret" ? "" : item["value"].string
        error = ""
        nameFocused = false
        valueFocused = true
    }

    private func resetEditor(focus: Bool = false) {
        editingName = nil
        name = ""
        kind = composeBuildOnly ? "secret" : "plain"
        buildSecret = composeBuildOnly
        value = ""
        error = ""
        valueFocused = false
        if focus { nameFocused = true }
    }

    private func load() {
        guard !loading else { return }
        loading = true
        error = ""
        Task {
            defer { loading = false }
            do {
                let response = try await NativeBridge.call("ListDeploymentEnvironment", [deployment.id])
                environment = response
                variables = response["variables"].array
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    private func save() {
        guard canSave else { return }
        saving = true
        error = ""
        let input: [String: Any] = [
            "deploymentId": deployment.id,
            "variables": [["name": normalizedName, "kind": kind, "value": value, "buildSecret": kind == "secret" && buildSecret]],
            "remove": []
        ]
        Task {
            defer { saving = false }
            do {
                let response = try await NativeBridge.call("UpdateDeploymentEnvironment", [input])
                environment = response
                variables = response["variables"].array
                resetEditor()
                onChanged(response)
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    private func remove(_ item: J) {
        saving = true
        error = ""
        deleteTarget = nil
        let removedName = item["name"].string
        let input: [String: Any] = [
            "deploymentId": deployment.id,
            "variables": [],
            "remove": [removedName]
        ]
        Task {
            defer { saving = false }
            do {
                let response = try await NativeBridge.call("UpdateDeploymentEnvironment", [input])
                environment = response
                variables = response["variables"].array
                if editingName == removedName { resetEditor() }
                onChanged(response)
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}
