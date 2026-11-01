import AppKit
import SwiftUI

private struct DNSRecordEditor: Identifiable {
    let id = UUID()
    let record: J?
}

struct DNSRecordsView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let domain: J
    @State private var records: [J] = []
    @State private var hosts: [J] = []
    @State private var bindingError: String?
    @State private var loading = false
    @State private var errorMessage: String?
    @State private var lastLoaded: Date?
    @State private var loadSequence = 0
    @State private var editor: DNSRecordEditor?
    @State private var deleting: J?
    @State private var bindingRecord: J?

    private var provider: String {
        model.dnsConnections.first(where: { $0.id == domain["dnsConnectionId"].string })?["provider"].string ?? "aliyun"
    }
    var body: some View {
        VStack(spacing: 0) {
            HStack {
                VStack(alignment: .leading, spacing: 5) {
                    Text("\(dnsProviderName(provider))解析记录").font(.system(size: 21, weight: .bold)).foregroundStyle(DeployStyle.ink)
                    Text(lastLoaded.map { "\(domain["name"].string) · 上次读取 \($0.formatted(date: .omitted, time: .standard))" }
                         ?? domain["name"].string)
                        .font(.system(size: 12)).foregroundStyle(DeployStyle.muted)
                }
                Spacer()
                Button {
                    load()
                } label: {
                    HStack(spacing: 6) {
                        if loading { ProgressView().controlSize(.small) }
                        Text(loading ? "正在刷新…" : "刷新数据")
                    }
                }
                .buttonStyle(DeploySecondaryButtonStyle())
                .disabled(loading || domain["dnsConnectionId"].string.isEmpty)
                Button("新增记录") { editor = DNSRecordEditor(record: nil) }
                    .buttonStyle(DeployPrimaryButtonStyle())
                    .disabled(domain["dnsConnectionId"].string.isEmpty)
            }.padding(20).background(DeployStyle.overviewHeader)
            if domain["dnsConnectionId"].string.isEmpty {
                VStack(spacing: 12) {
                    Image(systemName: "globe").font(.system(size: 28)).foregroundStyle(DeployStyle.green)
                    Text("尚未选择 DNS 服务商").font(.system(size: 16, weight: .semibold))
                    Text("请先在主域名的 DNS 配置中选择服务商连接。")
                        .font(.system(size: 12)).foregroundStyle(DeployStyle.muted)
                }.frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if loading && lastLoaded == nil {
                ProgressView("正在读取解析记录…").frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ScrollView {
                    LazyVStack(spacing: 0) {
                        if let errorMessage {
                            DNSRecordLoadErrorView(detail: errorMessage).padding(18)
                        }
                        if let bindingError {
                            DNSRecordLoadErrorView(detail: bindingError, title: "服务绑定状态读取失败").padding(18)
                        }
                        if records.isEmpty {
                            Text(errorMessage == nil ? "服务商返回 0 条解析记录" : "未能读取解析记录")
                                .foregroundStyle(DeployStyle.muted).padding(35)
                        }
                        ForEach(records, id: \.id) { record in
                            let matchedHost = hosts.first { $0["fqdn"].string == record["fqdn"].string }
                            let binding = matchedHost?["binding"] ?? J(nil)
                            let external = matchedHost?["externalRoute"] ?? J(nil)
                            let serviceState = domainBindingState(external.isNull ? binding : external)
                            DomainDNSRecordRow(hostRecord: record["rr"].string,
                                               name: dnsRecordDisplayName(record, domain: domain), type: record["type"].string,
                                               value: record["value"].string, ttl: record["ttl"].int,
                                               status: dnsRecordStatusText(record, provider: provider),
                                               statusTone: dnsRecordStatusTone(record, provider: provider),
                                               serviceSummary: external.isNull
                                                ? domainBindingSummary(binding, deployments: model.deployments)
                                                : "服务器服务 · \(external["hostIp"].string):\(external["hostPort"].string) · \(external["certificatePath"].string.isEmpty ? "HTTP" : "HTTPS")",
                                               serviceStatus: serviceState.title, serviceTone: serviceState.tone) {
                                HStack(spacing: AppSpacing.xs) {
                                    Button(binding.isNull && external.isNull ? "服务绑定" : "管理绑定") { bindingRecord = record }
                                        .buttonStyle(DeploySecondaryButtonStyle())
                                        .accessibilityLabel("\(binding.isNull && external.isNull ? "服务绑定" : "管理绑定")，\(dnsRecordDisplayName(record, domain: domain))")
                                        .disabled(!record["bindingEligible"].bool)
                                        .help(record["bindingEligible"].bool ? "选择此域名的部署服务" : record["bindingDisabledReason"].string)
                                    Menu {
                                        Button("编辑") { editor = DNSRecordEditor(record: record) }
                                        if provider == "aliyun" {
                                            Button(record["status"].string.uppercased() == "ENABLE" ? "停用" : "启用") { setStatus(record) }
                                                .disabled(!["ENABLE", "DISABLE"].contains(record["status"].string.uppercased()))
                                        }
                                        Button("删除", role: .destructive) { deleting = record }
                                    } label: { Image(systemName: "ellipsis") }
                                        .buttonStyle(DeploySecondaryButtonStyle())
                                        .menuIndicator(.hidden)
                                        .accessibilityLabel("解析记录操作，\(dnsRecordDisplayName(record, domain: domain))")
                                        .disabled(record["locked"].bool)
                                }
                            }
                            Rectangle().fill(DeployStyle.line).frame(height: 1)
                        }
                    }.deployCard().padding(18)
                }.background(DeployStyle.canvas)
            }
            HStack {
                Text("操作直接提交 \(dnsProviderName(provider))；公开 DNS 生效可能需要等待 TTL。")
                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                Spacer()
                Button("关闭") { dismiss() }.buttonStyle(DeploySecondaryButtonStyle())
                    .keyboardShortcut(.cancelAction)
            }.padding(18).background(DeployStyle.overviewPanel)
        }
        .frame(width: 880, height: 620)
        .background(DeployStyle.canvas)
        .environment(\.colorScheme, .light)
        .task { if !domain["dnsConnectionId"].string.isEmpty { load() } }
        .onChange(of: model.manualRefreshRevision) { _ in
            if !loading && !domain["dnsConnectionId"].string.isEmpty { load() }
        }
        .sheet(item: $editor) { request in
            DNSRecordFormView(domain: domain, record: request.record) { load() }
                .environmentObject(model)
        }
        .sheet(item: $bindingRecord, onDismiss: { load() }) { record in
            DNSRecordBindingView(domainID: domain.id, recordID: record.id) { load() }
                .environmentObject(model)
        }
        .confirmationDialog("删除解析记录？", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } })) {
            Button("删除解析记录", role: .destructive) {
                guard let record = deleting else { return }
                Task {
                    if await model.perform("DeleteDNSRecord", [domain.id, record.id, record["updatedAt"].int], refreshAfter: false) != nil { load() }
                    deleting = nil
                }
            }
        } message: { Text("此操作会从 DNS 服务商删除该条记录，并影响使用此记录的访问。") }
    }

    private func load() {
        loadSequence += 1
        let sequence = loadSequence
        loading = true
        errorMessage = nil
        bindingError = nil
        Task {
            async let fetchedHosts = NativeBridge.call("ListDomainHosts", [domain.id])
            do {
                let result = try await NativeBridge.call("ListDNSRecords", [domain.id])
                guard loadSequence == sequence else { return }
                records = result.array
                lastLoaded = Date()
                do {
                    let result = try await fetchedHosts
                    guard loadSequence == sequence else { return }
                    hosts = result.array
                } catch {
                    guard loadSequence == sequence else { return }
                    bindingError = error.localizedDescription
                }
            } catch {
                guard loadSequence == sequence else { return }
                errorMessage = error.localizedDescription
            }
            loading = false
        }
    }

    private func setStatus(_ record: J) {
        let next = record["status"].string.uppercased() == "ENABLE" ? "Disable" : "Enable"
        Task {
            if await model.perform("SetDNSRecordStatus", [domain.id, record.id, next, record["updatedAt"].int], refreshAfter: false) != nil { load() }
        }
    }
}

func dnsRecordDisplayName(_ record: J, domain: J) -> String {
    if !record["fqdn"].string.isEmpty { return record["fqdn"].string }
    return record["rr"].string == "@" ? domain["name"].string : record["rr"].string + "." + domain["name"].string
}

func dnsRecordStatusText(_ record: J, provider: String) -> String {
    if provider == "cloudflare" { return "已配置" }
    switch record["status"].string.uppercased() {
    case "ENABLE": return "已启用"
    case "DISABLE": return "已停用"
    default: return "状态未知"
    }
}

func dnsRecordStatusTone(_ record: J, provider: String) -> AppStatusBadge.Tone {
    if provider == "cloudflare" || record["status"].string.uppercased() == "ENABLE" { return .success }
    return record["status"].string.uppercased() == "DISABLE" ? .neutral : .warning
}

struct DNSRecordLoadErrorView: View {
    let detail: String
    var title = "解析记录刷新失败"
    @State private var showingDetails = false
    @State private var copied = false

    private var summary: String {
        if detail.contains("Forbidden.RAM") && detail.contains("DescribeDomainRecords") {
            return "阿里云 RAM 用户无法读取该域名的解析记录。请检查 alidns:DescribeDomainRecords 权限及域名所属资源组。"
        }
        if detail.count > 220 {
            return String(detail.prefix(220)) + "…"
        }
        return detail
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            Label(title, systemImage: "exclamationmark.triangle")
                .font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
            Text(summary).font(.system(size: 11)).foregroundStyle(DeployStyle.ink)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
            HStack(spacing: 8) {
                Button(copied ? "已复制" : "复制完整错误") {
                    NSPasteboard.general.clearContents()
                    copied = NSPasteboard.general.setString(detail, forType: .string)
                }.buttonStyle(DeploySecondaryButtonStyle())
                Button(showingDetails ? "收起详情" : "查看详情") { showingDetails.toggle() }
                    .buttonStyle(DeploySecondaryButtonStyle())
            }
            if showingDetails {
                ScrollView(.vertical) {
                    Text(detail)
                        .font(.system(size: 11, design: .monospaced))
                        .textSelection(.enabled)
                        .fixedSize(horizontal: false, vertical: true)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .frame(maxHeight: 150)
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(DeployStyle.overviewHeader, in: RoundedRectangle(cornerRadius: 9))
        .overlay { RoundedRectangle(cornerRadius: 9).strokeBorder(DeployStyle.line, lineWidth: 1) }
        .onChange(of: detail) { _ in copied = false; showingDetails = false }
    }
}

#Preview("DNS 解析刷新失败") {
    DNSRecordLoadErrorView(detail: "读取阿里云解析记录失败: Forbidden.RAM: alidns:DescribeDomainRecords")
        .padding(20)
        .frame(width: 580)
        .background(DeployStyle.canvas)
}

private struct DNSRecordFormView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let domain: J
    let record: J?
    let onSaved: () -> Void
    @State private var rr: String
    @State private var kind: String
    @State private var value: String
    @State private var ttl: String
    @State private var line: String
    @State private var saving = false
    @State private var errorMessage: String?
    private var provider: String {
        model.dnsConnections.first(where: { $0.id == domain["dnsConnectionId"].string })?["provider"].string ?? "aliyun"
    }
    private var hasChanges: Bool {
        guard let record else { return true }
        return rr.trimmingCharacters(in: .whitespacesAndNewlines) != record["rr"].string
            || kind != record["type"].string
            || value.trimmingCharacters(in: .whitespacesAndNewlines) != record["value"].string
            || Int(ttl) != record["ttl"].int
            || (provider == "aliyun" && line.trimmingCharacters(in: .whitespacesAndNewlines) != record["line"].string)
    }

    init(domain: J, record: J?, onSaved: @escaping () -> Void) {
        self.domain = domain
        self.record = record
        self.onSaved = onSaved
        _rr = State(initialValue: record?["rr"].string ?? "")
        _kind = State(initialValue: record?["type"].string ?? "A")
        _value = State(initialValue: record?["value"].string ?? "")
        _ttl = State(initialValue: record?["ttl"].string ?? "600")
        _line = State(initialValue: record?["line"].string ?? "default")
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                VStack(alignment: .leading, spacing: 5) {
                    Text(record == nil ? "新增解析记录" : "编辑解析记录")
                        .font(.system(size: 21, weight: .bold)).foregroundStyle(DeployStyle.ink)
                    Text("直接提交到所选 DNS 服务商 · \(domain["name"].string)")
                        .font(.system(size: 12)).foregroundStyle(DeployStyle.muted)
                }
                Spacer()
            }.padding(20).background(DeployStyle.overviewHeader)
            ScrollView {
              VStack(alignment: .leading, spacing: 16) {
                TextField("主机记录，例如 @ 或 app", text: $rr).textFieldStyle(.roundedBorder)
                    .accessibilityLabel("主机记录")
                Picker("记录类型", selection: $kind) {
                    ForEach(["A", "AAAA", "CNAME", "TXT"], id: \.self) { Text($0) }
                }
                TextField("记录值", text: $value).textFieldStyle(.roundedBorder)
                    .accessibilityLabel("记录值")
                HStack {
                    TextField("TTL（秒）", text: $ttl).textFieldStyle(.roundedBorder)
                        .accessibilityLabel("TTL 秒数")
                    TextField("解析线路", text: $line).textFieldStyle(.roundedBorder)
                        .accessibilityLabel("解析线路")
                        .disabled(provider == "cloudflare")
                }
                Text(provider == "cloudflare"
                     ? "@ 表示根域名；Cloudflare TTL 可填 1（自动）或不少于 60 秒。请核对记录值。"
                     : "@ 表示根域名。请核对记录值及线路，修改后可能影响线上访问。")
                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                if let errorMessage {
                    DNSRecordLoadErrorView(detail: errorMessage, title: "保存解析记录失败")
                }
              }.padding(18).deployCard(fill: DeployStyle.overviewPanel).padding(20)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(DeployStyle.canvas)
            HStack {
                Spacer()
                Button("取消") { dismiss() }.buttonStyle(DeploySecondaryButtonStyle())
                    .keyboardShortcut(.cancelAction).disabled(saving)
                Button(saving ? "正在保存…" : "保存到\(dnsProviderName(provider))") { save() }
                    .buttonStyle(DeployPrimaryButtonStyle())
                    .keyboardShortcut(.defaultAction)
                    .disabled(saving || !hasChanges || rr.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                              || value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || Int64(ttl) == nil)
            }.padding(20).background(DeployStyle.overviewPanel)
        }
        .frame(width: 540, height: 500)
        .background(DeployStyle.canvas)
        .environment(\.colorScheme, .light)
    }

    private func save() {
        guard !saving else { return }
        saving = true
        errorMessage = nil
        let input: [String: Any] = [
            "domainId": domain.id, "recordId": record?.id ?? "",
            "expectedUpdatedAt": record?["updatedAt"].int ?? 0,
            "record": ["rr": rr, "type": kind, "value": value, "ttl": Int64(ttl) ?? 0, "line": line]
        ]
        Task {
            do {
                _ = try await NativeBridge.call(record == nil ? "AddDNSRecord" : "UpdateDNSRecord", [input])
                onSaved(); dismiss()
            } catch {
                errorMessage = error.localizedDescription
            }
            saving = false
        }
    }
}
