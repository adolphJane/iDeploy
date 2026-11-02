import SwiftUI

// A DNS hostname can target a service already running on the chosen server.
// The scan is read-only; only an explicitly previewed job changes Nginx.
struct DomainExternalRouteView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let host: J
    let onSaved: () -> Void
    let compact: Bool

    @State private var route: J
    @State private var serverID: String
    @State private var hostIP: String
    @State private var hostPort: String
    @State private var useHTTPS: Bool
    @State private var certificatePath: String
    @State private var certificateKeyPath: String
    @State private var scan = J(nil)
    @State private var scanError = ""
    @State private var scanning = false
    @State private var plan = J(nil)
    @State private var planning = false
    @State private var saving = false
    @State private var errorMessage = ""
    @State private var queuedJob: J?
    @State private var showingRemove = false
    @State private var access = J(nil)
    @State private var checkingAccess = false

    init(host: J, onSaved: @escaping () -> Void, compact: Bool = false) {
        self.host = host
        self.onSaved = onSaved
        self.compact = compact
        let saved = host["externalRoute"]
        _route = State(initialValue: saved)
        _serverID = State(initialValue: saved["serverId"].string)
        _hostIP = State(initialValue: saved["hostIp"].string.isEmpty ? "127.0.0.1" : saved["hostIp"].string)
        _hostPort = State(initialValue: saved["hostPort"].int == 0 ? "" : saved["hostPort"].string)
        _useHTTPS = State(initialValue: !saved["certificatePath"].string.isEmpty)
        _certificatePath = State(initialValue: saved["certificatePath"].string)
        _certificateKeyPath = State(initialValue: saved["certificateKeyPath"].string)
    }

    private var active: Bool { route["status"].string == "active" }
    private var processing: Bool { ["applying", "removing"].contains(route["status"].string) }
    private var canSave: Bool {
        !saving && !serverID.isEmpty && !hostIP.isEmpty &&
        Int(hostPort).map { (1...65535).contains($0) } == true &&
        (!useHTTPS || (!certificatePath.isEmpty && !certificateKeyPath.isEmpty))
    }
    private var dirty: Bool {
        route.isNull || serverID != route["serverId"].string || hostIP != route["hostIp"].string ||
        hostPort != route["hostPort"].string ||
        (useHTTPS ? certificatePath : "") != route["certificatePath"].string ||
        (useHTTPS ? certificateKeyPath : "") != route["certificateKeyPath"].string
    }

    var body: some View {
        VStack(spacing: 0) {
            VStack(alignment: .leading, spacing: 5) {
                Text(route.isNull ? "服务绑定" : "管理绑定").font(.system(size: 21, weight: .bold))
                Text(host["fqdn"].string).font(.system(size: 12)).foregroundStyle(DeployStyle.muted)
            }.frame(maxWidth: .infinity, alignment: .leading)
                .padding(20).background(DeployStyle.overviewHeader)
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    SectionHeading("服务目标", "将 DNS 完整域名转发到服务器上已有的 HTTP 服务")
                    Picker("服务器", selection: $serverID) {
                        Text("选择服务器").tag("")
                        ForEach(model.servers, id: \.id) { item in
                            Text(item["name"].string).tag(item.id)
                        }
                    }.disabled(active || processing)
                    HStack {
                        Button("扫描现有 Nginx 路由") { loadScan() }
                            .buttonStyle(DeploySecondaryButtonStyle())
                            .disabled(serverID.isEmpty || scanning)
                        if scanning { ProgressView().controlSize(.small).accessibilityLabel("正在扫描") }
                    }
                    if !scanError.isEmpty { Text(scanError).font(.system(size: 11)).foregroundStyle(AppColors.warning).textSelection(.enabled) }
                    if !scan.isNull {
                        Text("现有入口：\(scan["container"].string) · \(scan["routes"].array.count) 个域名")
                            .font(.system(size: 11, weight: .semibold))
                        if !scan["services"].array.isEmpty {
                            Text("服务器已发布的 TCP 端口（选择后需通过 HTTP 预检）")
                                .font(.system(size: 11, weight: .semibold))
                            ForEach(0..<scan["services"].array.count, id: \.self) { index in
                                let candidate = scan["services"][index]
                                HStack {
                                    Text("\(candidate["service"].string) · \(candidate["hostIp"].string):\(candidate["hostPort"].string) → 容器 :\(candidate["containerPort"].string)")
                                        .font(.system(size: 11)).textSelection(.enabled)
                                    Spacer()
                                    Button("选用") {
                                        hostIP = candidate["hostIp"].string
                                        hostPort = candidate["hostPort"].string
                                        plan = J(nil)
                                    }.buttonStyle(DeploySecondaryButtonStyle()).disabled(active || processing)
                                }.padding(9).deployCard()
                            }
                        }
                        ForEach(0..<scan["routes"].array.count, id: \.self) { index in
                            let found = scan["routes"][index]
                            VStack(alignment: .leading, spacing: 5) {
                                Text("\(found["fqdn"].string) · \(found["service"].string.isEmpty ? "未识别服务" : found["service"].string) · \(found["managed"].bool ? "iDeploy 管理" : "服务器已有")")
                                    .font(.system(size: 11, weight: .semibold))
                                Text("转发 \(found["upstream"].string.isEmpty ? "未识别" : found["upstream"].string) · \(found["https"].bool ? "HTTPS" : "HTTP")")
                                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted).textSelection(.enabled)
                                if let endpoint = URLComponents(string: found["upstream"].string), endpoint.scheme == "http",
                                   let target = endpoint.host, let port = endpoint.port,
                                   target.split(separator: ".").count == 4,
                                   target.split(separator: ".").allSatisfy({ Int($0).map { (0...255).contains($0) } ?? false }),
                                   !active && !processing {
                                    Button("使用此服务端口") { hostIP = target; hostPort = String(port); plan = J(nil) }
                                        .buttonStyle(DeploySecondaryButtonStyle())
                                }
                                if !found["certificate"].string.isEmpty && !active && !processing {
                                    Button("尝试复用此证书") {
                                        useHTTPS = true
                                        certificatePath = found["certificate"].string
                                        certificateKeyPath = found["certificateKey"].string
                                        plan = J(nil)
                                    }.buttonStyle(DeploySecondaryButtonStyle())
                                }
                            }.padding(10).frame(maxWidth: .infinity, alignment: .leading).deployCard()
                        }
                    }
                    HStack {
                        TextField("后端 IP", text: $hostIP).textFieldStyle(.roundedBorder)
                            .accessibilityLabel("后端 IP")
                        TextField("HTTP 端口，例如 8080", text: $hostPort).textFieldStyle(.roundedBorder)
                            .accessibilityLabel("HTTP 端口")
                    }.disabled(active || processing)
                    Text("例如 Harbor 可选服务器上已发布的 HTTP 端口。已有同名 Nginx 路由会在预检中提示冲突，避免覆盖手工配置。")
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    Toggle("启用 HTTPS", isOn: $useHTTPS).disabled(active || processing)
                    if useHTTPS {
                        TextField("Nginx 容器内证书路径（fullchain.pem）", text: $certificatePath)
                            .textFieldStyle(.roundedBorder).disabled(active || processing)
                        TextField("Nginx 容器内私钥路径（privkey.pem）", text: $certificateKeyPath)
                            .textFieldStyle(.roundedBorder).disabled(active || processing)
                        Text("优先复用服务器已有证书；预检会验证证书覆盖此域名且未过期。没有匹配证书时，请先在服务器配置证书。")
                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    }
                    if !route["detail"].string.isEmpty {
                        Text(route["detail"].string).font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    }
                    if !errorMessage.isEmpty { DNSRecordLoadErrorView(detail: errorMessage, title: "服务路由操作失败") }
                    if active {
                        Text("已应用到 Nginx · \(route["hostIp"].string):\(route["hostPort"].string) · \(useHTTPS ? "HTTPS" : "HTTP")")
                            .font(.system(size: 12, weight: .semibold))
                        HStack {
                            Button("检查访问") { inspectAccess() }.buttonStyle(DeploySecondaryButtonStyle())
                            if checkingAccess { ProgressView().controlSize(.small) }
                        }
                        if !access.isNull {
                            Text("DNS：\(access["dnsStatus"].string) · 入口：\(access["ingressStatus"].string) · HTTPS：\(access["httpsStatus"].string) · 后端：\(access["backendStatus"].string)")
                                .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                            Text(access["detail"].string).font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                                .textSelection(.enabled)
                        }
                    } else if processing {
                        Text("入口任务处理中，请在任务列表查看结果。")
                            .font(.system(size: 11)).foregroundStyle(AppColors.warning)
                    } else {
                        Text("先保存草稿，再预检 Nginx 路由；预检通过后才会执行应用。")
                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                        if !route.isNull && !dirty {
                            Button("删除草稿", role: .destructive) { deleteDraft() }
                                .buttonStyle(DeploySecondaryButtonStyle())
                        }
                    }
                    if !plan.isNull {
                        VStack(alignment: .leading, spacing: 8) {
                            Text("入口预检").font(.system(size: 12, weight: .semibold))
                            Text(plan["detail"].string).font(.system(size: 11))
                                .foregroundStyle(plan["canApply"].bool ? DeployStyle.muted : AppColors.warning)
                                .textSelection(.enabled)
                            if !plan["ingressPreview"].string.isEmpty {
                                ScrollView {
                                    Text(plan["ingressPreview"].string)
                                        .font(.system(size: 10, design: .monospaced)).textSelection(.enabled)
                                        .frame(maxWidth: .infinity, alignment: .leading)
                                }.frame(height: 150).padding(8).background(DeployStyle.overviewPanel)
                            }
                            if plan["canApply"].bool {
                                Button("应用到现有 Nginx") { apply() }.buttonStyle(DeployPrimaryButtonStyle())
                            }
                        }.padding(12).deployCard()
                    }
                }.padding(20).frame(maxWidth: .infinity, alignment: .leading)
            }.background(DeployStyle.canvas)
            Rectangle().fill(DeployStyle.line).frame(height: 1)
            HStack(spacing: 10) {
                if active {
                    Button("解除入口路由", role: .destructive) { showingRemove = true }
                        .buttonStyle(DeploySecondaryButtonStyle())
                } else if !processing {
                    Button("预检已保存的路由") { loadPlan() }
                        .buttonStyle(DeploySecondaryButtonStyle())
                        .disabled(route.isNull || dirty || planning || saving)
                    if planning { ProgressView().controlSize(.small) }
                }
                Spacer()
                Button("关闭") { dismiss() }
                    .buttonStyle(DeploySecondaryButtonStyle())
                    .keyboardShortcut(.cancelAction)
                if !active && !processing {
                    Button(saving ? "保存中…" : "保存草稿") { save() }
                        .buttonStyle(DeployPrimaryButtonStyle())
                        .disabled(!canSave || !dirty)
                        .keyboardShortcut(.defaultAction)
                }
            }
            .padding(18)
            .background(DeployStyle.overviewPanel)
        }
        .frame(width: 700, height: compact ? 580 : 650)
        .foregroundStyle(DeployStyle.ink)
        .environment(\.colorScheme, .light)
        .onChange(of: serverID) { _ in scan = J(nil); plan = J(nil) }
        .onChange(of: hostIP) { _ in plan = J(nil) }
        .onChange(of: hostPort) { _ in plan = J(nil) }
        .onChange(of: useHTTPS) { _ in plan = J(nil) }
        .onChange(of: certificatePath) { _ in plan = J(nil) }
        .onChange(of: certificateKeyPath) { _ in plan = J(nil) }
        .sheet(item: $queuedJob) { job in
            JobDetailSheet(job: job).environmentObject(model).onDisappear { onSaved(); dismiss() }
        }
        .confirmationDialog("解除服务器服务路由？", isPresented: $showingRemove) {
            Button("解除入口路由", role: .destructive) { remove() }
        } message: {
            Text("只移除 iDeploy 管理的 Nginx 路由；DNS 解析记录和应用服务保持不变。")
        }
        .onAppear { if active { inspectAccess() } }
    }

    private func loadScan() {
        scanning = true; scanError = ""
        let requested = serverID
        Task {
            do { let result = try await NativeBridge.call("ScanServerNginxRoutes", [requested]); if serverID == requested { scan = result } }
            catch { if serverID == requested { scanError = error.localizedDescription } }
            scanning = false
        }
    }
    private func save() {
        saving = true; errorMessage = ""
        let input: [String: Any] = ["hostId": host.id, "serverId": serverID, "hostIp": hostIP,
                                    "hostPort": Int(hostPort) ?? 0, "upstreamScheme": "http",
                                    "certificatePath": useHTTPS ? certificatePath : "",
                                    "certificateKeyPath": useHTTPS ? certificateKeyPath : ""]
        Task {
            do {
                route = try await NativeBridge.call("SaveExternalRouteDraft", [input])
                plan = J(nil)
                onSaved()
            } catch { errorMessage = error.localizedDescription }
            saving = false
        }
    }
    private func loadPlan() {
        planning = true; errorMessage = ""
        Task {
            do { plan = try await NativeBridge.call("PlanExternalRoute", [host.id]) }
            catch { errorMessage = error.localizedDescription }
            planning = false
        }
    }
    private func apply() {
        Task {
            do {
                queuedJob = try await NativeBridge.call("ApplyExternalRoute", [host.id, plan["updatedAt"].string, plan["ingressConfigHash"].string])
                plan = J(nil)
            } catch { errorMessage = error.localizedDescription }
        }
    }
    private func remove() {
        Task {
            do { queuedJob = try await NativeBridge.call("RemoveExternalRoute", [host.id]) }
            catch { errorMessage = error.localizedDescription }
        }
    }
    private func deleteDraft() {
        Task {
            do { _ = try await NativeBridge.call("DeleteExternalRouteDraft", [host.id]); onSaved(); dismiss() }
            catch { errorMessage = error.localizedDescription }
        }
    }
    private func inspectAccess() {
        checkingAccess = true
        Task {
            do { access = try await NativeBridge.call("InspectDomainAccess", [host.id]) }
            catch { errorMessage = error.localizedDescription }
            checkingAccess = false
        }
    }
}

#Preview("服务器现有服务路由") {
    DomainExternalRouteView(host: J(["id": "preview", "fqdn": "harbor.example.com"]), onSaved: {})
        .environmentObject(AppModel())
}
