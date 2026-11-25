import SwiftUI

struct ManagedDatabasesView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let onConnect: (J) -> Void

    @State private var instances: [J] = []
    @State private var selectedID = ""
    @State private var environment = "test"
    @State private var backups: [J] = []
    @State private var activeJob: J?
    @State private var adding = false
    @State private var confirmingDeployment = false
    @State private var backupToRestore: J?
    @State private var instanceToRemove: J?
    @State private var isSubmitting = false
    @State private var message = ""

    private var selected: J? { instances.first { $0.id == selectedID } }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                SectionHeading("数据库实例")
                Spacer()
                Button("添加实例") { adding = true }
                    .buttonStyle(DeployPrimaryButtonStyle())
            }
            Picker("环境", selection: $environment) {
                Text("测试环境").tag("test")
                Text("线上环境").tag("production")
            }
            .pickerStyle(.segmented)
            .onChange(of: environment) { _ in select("") }

            HStack(alignment: .top, spacing: 14) {
                List(selection: Binding<String?>(
                    get: { selectedID.isEmpty ? nil : selectedID },
                    set: { select($0 ?? "") }
                )) {
                    ForEach(instances.filter { $0.deploymentEnvironment == environment }, id: \.id) { instance in
                        VStack(alignment: .leading, spacing: 3) {
                            Text(instance["name"].string)
                            Text(driverName(instance) + " " + instance["engineVersion"].string)
                                .font(.caption).foregroundStyle(.secondary)
                        }
                        .tag(instance.id)
                    }
                }
                .frame(width: 210)
                .accessibilityLabel("当前环境的数据库实例")

                ScrollView {
                    if let selected {
                        instanceDetail(selected)
                    } else {
                        EmptyState(title: "选择数据库实例", detail: "在当前环境添加或选择一个数据库实例。", symbol: "cylinder")
                    }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            if let activeJob {
                HStack {
                    StateBadge(state: activeJob["status"].string)
                    Text(activeJob["message"].string + " " + activeJob["error"].string)
                        .foregroundStyle(DeployStyle.muted)
                }
            }
            if !message.isEmpty {
                Text(message).foregroundStyle(.red).textSelection(.enabled)
            }
            HStack {
                Spacer()
                Button("完成") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                    .buttonStyle(DeploySecondaryButtonStyle())
            }
        }
        .padding(22)
        .frame(width: 760, height: 510)
        .background(DeployStyle.canvas)
        .task { await refresh() }
        .task(id: selectedID) {
            let updating = selectedID
            guard !updating.isEmpty else { return }
            if let result = await model.perform("ListManagedDatabaseBackups", [updating], refreshAfter: false),
               updating == selectedID {
                backups = result.array
            }
        }
        .task(id: activeJob?.id) { await monitorJob() }
        .sheet(isPresented: $adding) {
            ManagedDatabaseForm(environment: environment) { instance in
                environment = instance.deploymentEnvironment
                if !instances.contains(where: { $0.id == instance.id }) { instances.append(instance) }
                select(instance.id)
            }
            .environmentObject(model)
        }
        .sheet(item: $backupToRestore) { backup in
            if let selected {
                RestoreManagedDatabaseSheet(source: selected, backup: backup, instances: instances) { target in
                    submit(target, "restore", backupID: backup.id)
                    select(target.id)
                }
            }
        }
        .alert("确认部署数据库实例？", isPresented: $confirmingDeployment) {
            Button("取消", role: .cancel) {}
            Button("部署") { if let selected { submit(selected, "deploy") } }
        } message: {
            if let selected {
                Text(instanceLocation(selected))
            }
        }
        .confirmationDialog("移除数据库实例配置？", isPresented: Binding(
            get: { instanceToRemove != nil }, set: { if !$0 { instanceToRemove = nil } }
        )) {
            Button("移除配置", role: .destructive) {
                guard let target = instanceToRemove else { return }
                Task {
                    if await model.perform("DeleteManagedDatabaseConfiguration", [target.id], refreshAfter: false) != nil {
                        select("")
                        await refresh()
                    }
                    instanceToRemove = nil
                }
            }
        } message: {
            Text("本机将移除此实例的配置、备份索引及钥匙串记录。服务器上的容器、数据卷和备份文件会保留。")
        }
    }

    private func instanceDetail(_ instance: J) -> some View {
        let status = instance["status"].string
        return VStack(alignment: .leading, spacing: 12) {
            SectionHeading(instance["name"].string, instance.deploymentEnvironmentLabel + " · " + serverName(instance))
            Text(driverName(instance) + " " + instance["engineVersion"].string + " · " + endpoint(instance))
                .textSelection(.enabled)
            Text("库名 " + instance["databaseName"].string + " · " + statusLabel(status))
                .foregroundStyle(DeployStyle.muted)
            if status == "restore_failed" {
                Text("请新建实例后重新恢复备份。")
                    .foregroundStyle(.orange)
            }
            HStack {
                if ["draft", "failed"].contains(status) {
                    Button(status == "draft" ? "部署实例" : "重试部署") { confirmingDeployment = true }
                        .buttonStyle(DeployPrimaryButtonStyle())
                }
                if status != "deploying" && status != "restoring" {
                    Button("移除配置", role: .destructive) { instanceToRemove = instance }
                        .buttonStyle(DeploySecondaryButtonStyle())
                }
                if status == "ready" {
                    Button("生成备份") { submit(instance, "backup") }
                        .buttonStyle(DeploySecondaryButtonStyle())
                    Button("添加浏览连接") { onConnect(instance) }
                        .buttonStyle(DeploySecondaryButtonStyle())
                }
            }
            .disabled(isSubmitting || jobRunning)
            if !backups.isEmpty {
                Divider()
                SectionHeading("备份", "选择此服务器上的备份，恢复到同环境的新实例。")
                ForEach(backups, id: \.id) { backup in
                    HStack {
                        Text(backup["createdAt"].string).font(.caption)
                        Spacer()
                        Button("恢复到新实例") { backupToRestore = backup }
                            .buttonStyle(DeploySecondaryButtonStyle())
                            .disabled(isSubmitting || jobRunning)
                    }
                }
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .deployCard()
    }

    private var jobRunning: Bool {
        guard let activeJob else { return false }
        return ["queued", "running", "canceling"].contains(activeJob["status"].string)
    }

    private func select(_ id: String) {
        guard id != selectedID else { return }
        backups = []
        selectedID = id
    }

    private func serverName(_ instance: J) -> String {
        model.servers.first { $0.id == instance["serverId"].string }?["name"].string ?? instance["serverId"].string
    }

    private func instanceLocation(_ instance: J) -> String {
        [instance.deploymentEnvironmentLabel, endpoint(instance), serverName(instance)].joined(separator: " · ")
    }

    private func endpoint(_ instance: J) -> String { instance["listenIp"].string + ":" + instance["port"].string }

    private func driverName(_ instance: J) -> String { instance["driver"].string == "mysql" ? "MySQL" : "PostgreSQL" }

    private func statusLabel(_ status: String) -> String {
        switch status {
        case "draft": return "待部署"
        case "deploying": return "部署中"
        case "restoring": return "恢复中"
        case "ready": return "已部署"
        case "restore_failed": return "恢复失败"
        default: return "部署失败"
        }
    }

    private func refresh() async {
        if let result = await model.perform("ListManagedDatabases", refreshAfter: false) { instances = result.array }
    }

    private func submit(_ instance: J, _ action: String, backupID: String = "") {
        isSubmitting = true
        message = ""
        Task {
            defer { isSubmitting = false }
            do {
                activeJob = try await NativeBridge.call("RunManagedDatabaseOperation", [instance.id, action, backupID])
            } catch {
                message = error.localizedDescription
            }
        }
    }

    private func monitorJob() async {
        guard let id = activeJob?.id else { return }
        while !Task.isCancelled && activeJob?.id == id {
            if let result = try? await NativeBridge.call("GetJob", [id]), !Task.isCancelled {
                activeJob = result
                if !jobRunning {
                    await refresh()
                    let updating = selectedID
                    if !updating.isEmpty, let result = try? await NativeBridge.call("ListManagedDatabaseBackups", [updating]),
                       selectedID == updating {
                        backups = result.array
                    }
                    return
                }
            }
            try? await Task.sleep(nanoseconds: 1_500_000_000)
        }
    }
}

private struct ManagedDatabaseForm: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let environment: String
    let onCreated: (J) -> Void

    @State private var name = ""
    @State private var serverID = ""
    @State private var driver = "postgres"
    @State private var listenIP = ""
    @State private var port = "5432"
    @State private var database = ""
    @State private var password = ""
    @State private var appUsername = "app"
    @State private var appPassword = ""
    @State private var saving = false

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            SectionHeading("添加数据库实例", DeploymentEnvironmentLabel.name(environment))
            Form {
                TextField("实例名称", text: $name)
                Picker("服务器", selection: $serverID) {
                    Text("选择服务器").tag("")
                    ForEach(model.servers, id: \.id) { server in
                        Text(server["name"].string + " · " + server["host"].string).tag(server.id)
                    }
                }
                Picker("数据库引擎", selection: $driver) {
                    Text("PostgreSQL 17").tag("postgres")
                    Text("MySQL 8.4").tag("mysql")
                }
                .onChange(of: driver) { value in port = value == "mysql" ? "3306" : "5432" }
                TextField("私网监听 IPv4", text: $listenIP)
                TextField("主机端口", text: $port)
                TextField("业务库名", text: $database)
                SecureField("管理员密码", text: $password)
                TextField("应用账号", text: $appUsername)
                SecureField("应用账号密码", text: $appPassword)
                Text("密码至少 16 字节。保存后可单独部署或将备份恢复到此实例。")
                    .foregroundStyle(DeployStyle.muted)
            }
            .textFieldStyle(.roundedBorder)
            HStack {
                Spacer()
                Button("取消") { dismiss() }.keyboardShortcut(.cancelAction)
                Button("保存实例") { save() }
                    .buttonStyle(DeployPrimaryButtonStyle())
                    .keyboardShortcut(.defaultAction)
                    .disabled(saving || name.isEmpty || serverID.isEmpty || listenIP.isEmpty || Int(port) == nil ||
                              database.isEmpty || appUsername.isEmpty || password.utf8.count < 16 || appPassword.utf8.count < 16)
            }
        }
        .padding(24)
        .frame(width: 530)
    }

    private func save() {
        saving = true
        Task {
            defer { saving = false }
            let input: [String: Any] = [
                "name": name, "environment": environment, "serverId": serverID, "driver": driver,
                "engineVersion": driver == "mysql" ? "8.4" : "17", "listenIp": listenIP,
                "port": Int(port) ?? 0, "databaseName": database, "password": password,
                "appUsername": appUsername, "appPassword": appPassword
            ]
            if let instance = await model.perform("CreateManagedDatabase", [input], refreshAfter: false) {
                onCreated(instance)
                dismiss()
            }
        }
    }
}

private struct RestoreManagedDatabaseSheet: View {
    @Environment(\.dismiss) private var dismiss
    let source: J
    let backup: J
    let instances: [J]
    let onRestore: (J) -> Void
    @State private var targetID = ""

    private var candidates: [J] {
        instances.filter { target in
            target["status"].string == "draft" && ["serverId", "environment", "driver", "engineVersion", "databaseName", "appUsername"]
                .allSatisfy { target[$0].string == source[$0].string }
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            SectionHeading("恢复到新实例", source.deploymentEnvironmentLabel + " · " + backup["createdAt"].string)
            Picker("恢复目标", selection: $targetID) {
                Text("选择目标实例").tag("")
                ForEach(candidates, id: \.id) { item in
                    Text(item["name"].string + " · " + item["listenIp"].string + ":" + item["port"].string).tag(item.id)
                }
            }
            if candidates.isEmpty {
                Text("请先在同一服务器和环境添加相同引擎版本、库名和应用账号的实例草稿。")
                    .foregroundStyle(DeployStyle.muted)
            }
            HStack {
                Spacer()
                Button("取消") { dismiss() }.keyboardShortcut(.cancelAction)
                Button("恢复备份") {
                    guard let target = candidates.first(where: { $0.id == targetID }) else { return }
                    onRestore(target)
                    dismiss()
                }
                .buttonStyle(DeployPrimaryButtonStyle())
                .disabled(targetID.isEmpty)
            }
        }
        .padding(24)
        .frame(width: 500)
    }
}
