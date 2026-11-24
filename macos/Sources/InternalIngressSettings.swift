import SwiftUI

struct InternalIngressSettings: View {
    @EnvironmentObject private var model: AppModel
    @State private var entries: [J] = []
    @State private var knownHosts: [String] = []
    @State private var loadingKnownHosts = false
    @State private var knownHostsError = ""
    @State private var fqdn = ""
    @State private var serverID = ""
    @State private var port = "8766"
    @State private var webroot = ""
    @State private var certificate = ""
    @State private var certificateKey = ""
    @State private var plan = J(nil)
    @State private var inspection = J(nil)
    @State private var busy = false
    @State private var message = ""
    @State private var certificateSheet = false
    @State private var jobID = ""

    private var selected: J? { entries.first { $0["fqdn"].string == fqdn.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() } }
    private var ready: Bool { selected?["status"].string == "ready" }
    private var applying: Bool { selected?["status"].string == "applying" }
    private var canSave: Bool { !busy && !ready && !fqdn.isEmpty && !serverID.isEmpty && Int(port) != nil }

    private var savedIngressSelection: Binding<String> {
        Binding(
            get: { selected?["fqdn"].string ?? "" },
            set: { value in
                if value.isEmpty {
                    serverID = ""
                    webroot = ""
                }
                fqdn = value
            }
        )
    }

    private var knownHostSelection: Binding<String> {
        Binding(get: { knownHosts.contains(fqdn) ? fqdn : "" }, set: { fqdn = $0 })
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            VStack(alignment: .leading, spacing: 12) {
                Text("内部 HTTPS 入口")
                    .font(.system(size: 14, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                Text("先选域名与入口服务器，配置证书，再部署仅监听服务器本机的入口服务。就绪后，项目可在发布时选择这个域名下的路径。")
                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                if !entries.isEmpty {
                    Picker("已有入口", selection: savedIngressSelection) {
                        Text("新建入口").tag("")
                        ForEach(entries.indices, id: \.self) { index in
                            let entry = entries[index]
                            Text("\(entry["fqdn"].string) · \(entry["status"].string == "ready" ? "已就绪" : "待配置")")
                                .tag(entry["fqdn"].string)
                        }
                    }
                }
                HStack(spacing: 8) {
                    Picker("从域名列表选择", selection: knownHostSelection) {
                        Text("选择域名").tag("")
                        ForEach(knownHosts, id: \.self) { host in Text(host).tag(host) }
                    }
                    if loadingKnownHosts { ProgressView().controlSize(.small).accessibilityLabel("正在读取域名和 DNS 解析记录") }
                }
                if !knownHostsError.isEmpty {
                    Text(knownHostsError).font(.system(size: 11)).foregroundStyle(.orange)
                }
                TextField("完整域名，例如 apps.example.com", text: $fqdn)
                    .textFieldStyle(.roundedBorder)
                    .accessibilityLabel("应用入口域名")
                Picker("入口服务器", selection: $serverID) {
                    Text("选择服务器").tag("")
                    ForEach(model.servers, id: \.id) { item in
                        Text("\(item["name"].string) · \(item["host"].string)").tag(item.id)
                    }
                }
                .disabled(ready)
                TextField("入口服务本机端口", text: $port)
                    .textFieldStyle(.roundedBorder)
                    .disabled(ready)
                Text("服务只绑定 127.0.0.1；同一服务器的多个域名使用不同本机端口。现有 Nginx 负责 80/443 与 HTTPS。")
                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                TextField("服务器 ACME Webroot 目录（可选）", text: $webroot)
                    .textFieldStyle(.roundedBorder).disabled(ready)
                HStack(spacing: 10) {
                    Button("查找已有证书") { scanCertificate() }
                        .buttonStyle(DeploySecondaryButtonStyle()).disabled(busy || fqdn.isEmpty || serverID.isEmpty)
                    Button("申请 HTTPS 证书") { certificateSheet = true }
                        .buttonStyle(DeploySecondaryButtonStyle()).disabled(busy || fqdn.isEmpty || serverID.isEmpty)
                    if busy { ProgressView().controlSize(.small) }
                }
                TextField("Nginx 容器内证书路径", text: $certificate)
                    .textFieldStyle(.roundedBorder).disabled(ready)
                TextField("Nginx 容器内私钥路径", text: $certificateKey)
                    .textFieldStyle(.roundedBorder).disabled(ready)
                HStack(spacing: 10) {
                    Button("保存入口参数") { save() }
                        .buttonStyle(DeploySecondaryButtonStyle()).disabled(!canSave || applying)
                    if selected?["status"].string == "draft" {
                        Button("删除草稿") { deleteDraft() }
                            .buttonStyle(DeploySecondaryButtonStyle()).disabled(busy)
                    }
                    Button("预检 HTTPS 入口") { preflight() }
                        .buttonStyle(DeploySecondaryButtonStyle()).disabled(busy || selected == nil || ready || applying)
                    if plan["canDeploy"].bool {
                        Button("部署入口并绑定 HTTPS") { deploy() }
                            .buttonStyle(DeployPrimaryButtonStyle()).disabled(busy)
                    }
                    if selected != nil {
                        Button("检查实际状态") { inspect() }
                            .buttonStyle(DeploySecondaryButtonStyle()).disabled(busy)
                    }
                    if inspection["canRepair"].bool {
                        Button("恢复就绪状态") { repair() }
                            .buttonStyle(DeployPrimaryButtonStyle()).disabled(busy)
                    }
                }
                if ready {
                    Text("已就绪 · https://\(fqdn)/ · 发布页面可选择此域名和项目路径")
                        .font(.system(size: 11, weight: .medium)).foregroundStyle(DeployStyle.green)
                }
                if applying { Text("入口部署任务正在运行，请等待完成。").font(.system(size: 11)).foregroundStyle(DeployStyle.muted) }
                if let detail = selected?["detail"].string, !detail.isEmpty {
                    Text(detail).font(.system(size: 11)).foregroundStyle(.orange).textSelection(.enabled)
                }
                if !plan["detail"].string.isEmpty {
                    Text(plan["detail"].string).font(.system(size: 11)).textSelection(.enabled)
                }
                if !inspection["detail"].string.isEmpty {
                    Text("实际状态：\(inspection["detail"].string)")
                        .font(.system(size: 11)).foregroundStyle(inspection["status"].string == "ready" ? DeployStyle.green : .orange)
                        .textSelection(.enabled)
                }
                if !plan["ingressPreview"].string.isEmpty {
                    ScrollView(.horizontal) {
                        Text(plan["ingressPreview"].string)
                            .font(.system(size: 10, design: .monospaced)).textSelection(.enabled)
                    }
                    .frame(maxHeight: 190).padding(10).background(DeployStyle.canvas, in: RoundedRectangle(cornerRadius: 8))
                }
                if !message.isEmpty { Text(message).font(.system(size: 11)).foregroundStyle(DeployStyle.muted).textSelection(.enabled) }
            }
            .padding(18).frame(maxWidth: .infinity, alignment: .leading).deployCard()
        }
        .task { await load() }
        .onChange(of: fqdn) { value in
            plan = J(nil)
            inspection = J(nil)
            if let entry = entries.first(where: { $0["fqdn"].string == value.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() }) {
                serverID = entry["serverId"].string
                port = entry["gatewayPort"].string
                webroot = entry["webroot"].string
                certificate = entry["certificatePath"].string
                certificateKey = entry["certificateKeyPath"].string
            } else {
                certificate = ""
                certificateKey = ""
                port = suggestedPort()
            }
        }
        .onChange(of: serverID) { _ in
            if selected == nil { port = suggestedPort() }
        }
        .sheet(isPresented: $certificateSheet, onDismiss: { scanCertificate() }) {
            DomainCertificateView(fqdn: fqdn, suggestedServerID: serverID).environmentObject(model)
        }
    }

    private func loadEntries(selectDefault: Bool = false) async {
        do {
            let result = try await NativeBridge.call("ListInternalIngresses")
            guard !Task.isCancelled else { return }
            entries = result.array
            if selectDefault, fqdn.isEmpty, serverID.isEmpty, port == "8766",
               webroot.isEmpty, certificate.isEmpty, certificateKey.isEmpty,
               let entry = entries.first(where: { $0["status"].string == "ready" }) ?? entries.first {
                fqdn = entry["fqdn"].string
            }
        } catch {
            if selectDefault && !Task.isCancelled {
                message = "读取已有 HTTPS 入口失败。\(error.localizedDescription)"
            }
        }
    }

    private func load() async {
        await loadEntries(selectDefault: true)
        guard !Task.isCancelled else { return }
        loadingKnownHosts = true
        knownHostsError = ""
        defer { loadingKnownHosts = false }
        var names = Set<String>()
        let domains = (try? await NativeBridge.call("ListDomains"))?.array ?? model.domains
        for domain in domains {
            if let result = try? await NativeBridge.call("ListDomainHosts", [domain.id]) {
                for host in result.array { if !host["fqdn"].string.isEmpty { names.insert(host["fqdn"].string) } }
            }
        }
        knownHosts = names.sorted()
        for domain in domains where !domain["dnsConnectionId"].string.isEmpty {
            do {
                let records = try await NativeBridge.call("ListDNSRecords", [domain.id], timeoutSeconds: 40)
                for record in records.array where record["bindingEligible"].bool {
                    let host = record["fqdn"].string
                    if !host.isEmpty { names.insert(host) }
                }
                knownHosts = names.sorted()
            } catch {
                knownHostsError = "部分 DNS 解析记录读取失败；仍可手动填写完整域名。\(error.localizedDescription)"
            }
        }
    }

    private func suggestedPort() -> String {
        let used = Set(entries.filter { $0["serverId"].string == serverID }.map { $0["gatewayPort"].int })
        var candidate = 8766
        while used.contains(candidate) && candidate < 65535 { candidate += 1 }
        return String(candidate)
    }

    private func scanCertificate() {
        guard !fqdn.isEmpty && !serverID.isEmpty else { return }
        busy = true; message = "正在检查证书…"
        Task {
            defer { busy = false }
            do {
                let result = try await NativeBridge.call("CheckServerCertificate", [serverID, fqdn, webroot], timeoutSeconds: 70)
                if !result["existing"].isNull {
                    certificate = result["existing"]["certificate"].string
                    certificateKey = result["existing"]["certificateKey"].string
                    message = "已找到有效证书，请保存入口参数。"
                } else { message = "尚未找到可用证书；请先申请或填写已有证书路径。" }
            } catch { message = error.localizedDescription }
        }
    }

    private func save() {
        busy = true; message = "正在保存入口参数…"; plan = J(nil)
        Task {
            defer { busy = false }
            do {
                let input: [String: Any] = ["fqdn": fqdn, "serverId": serverID,
                    "gatewayPort": Int(port) ?? 0, "webroot": webroot,
                    "certificatePath": certificate, "certificateKeyPath": certificateKey]
                let entry = try await NativeBridge.call("SaveInternalIngressDraft", [input])
                fqdn = entry["fqdn"].string
                await loadEntries()
                message = "入口参数已保存，可以运行预检。"
            } catch { message = error.localizedDescription }
        }
    }

    private func deleteDraft() {
        busy = true; message = "正在删除草稿…"
        Task {
            defer { busy = false }
            do {
                _ = try await NativeBridge.call("DeleteInternalIngressDraft", [fqdn])
                fqdn = ""; plan = J(nil)
                await loadEntries()
                message = "入口草稿已删除。"
            } catch { message = error.localizedDescription }
        }
    }

    private func preflight() {
        busy = true; message = "正在检查域名和 Nginx…"; plan = J(nil)
        Task {
            defer { busy = false }
            do {
                plan = try await NativeBridge.call("PlanInternalIngress", [fqdn], timeoutSeconds: 80)
                message = plan["detail"].string
            } catch { message = error.localizedDescription }
        }
    }

    private func inspect() {
        busy = true; message = "正在核对远端入口…"; inspection = J(nil)
        Task {
            defer { busy = false }
            do {
                inspection = try await NativeBridge.call("InspectInternalIngress", [fqdn], timeoutSeconds: 75)
                message = inspection["detail"].string
            } catch { message = error.localizedDescription }
        }
    }

    private func repair() {
        busy = true; message = "正在恢复本地入口状态…"
        Task {
            defer { busy = false }
            do {
                _ = try await NativeBridge.call("RepairInternalIngress", [fqdn], timeoutSeconds: 80)
                await loadEntries()
                inspection = J(nil); plan = J(nil)
                message = "远端入口已验证，本地状态恢复为就绪。"
            } catch { message = error.localizedDescription }
        }
    }

    private func deploy() {
        busy = true; message = "正在部署入口…"
        Task {
            defer { busy = false }
            do {
                let job = try await NativeBridge.call("DeployInternalIngress", [fqdn, plan["ingressConfigHash"].string], timeoutSeconds: 80)
                jobID = job.id
                for _ in 0..<150 {
                    let state = try await NativeBridge.call("GetJob", [jobID])
                    message = state["error"].string.isEmpty ? (state["detail"].string.isEmpty ? "入口部署中…" : state["detail"].string) : state["error"].string
                    if state["status"].string == "succeeded" { await loadEntries(); plan = J(nil); message = "内部 HTTPS 入口已就绪。"; return }
                    if ["failed", "canceled"].contains(state["status"].string) { return }
                    try await Task.sleep(nanoseconds: 1_000_000_000)
                }
                message = "部署仍在进行，请稍后刷新入口状态。"
            } catch { message = error.localizedDescription }
        }
    }
}

#Preview("内部应用入口") {
    PageContainer { InternalIngressSettings() }
        .environmentObject(AppModel())
}
