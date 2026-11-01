import SwiftUI

private struct DomainSheetRequest: Identifiable {
    enum Kind { case domain, host, hostNote, dnsConfig, connections, records, binding, dnsBinding, proxyScan }
    let id = UUID()
    let kind: Kind
    var domain: J? = nil
    var host: J? = nil
    var recordID: String? = nil
}

private struct DomainAccessItem: Identifiable {
    let fqdn: String
    let host: J?
    let record: J?
    var id: String { fqdn }
    var binding: J { host?["binding"] ?? J(nil) }
}

struct DomainsPage: View {
    @EnvironmentObject private var model: AppModel
    @State private var selectedID: String?
    @State private var hosts: [J] = []
    @State private var search = ""
    @State private var loading = false
    @State private var hostsError: String?
    @State private var loadedHostsDomainID = ""
    @State private var hostsLoadSequence = 0
    @State private var dnsRecords: [J] = []
    @State private var dnsLoading = false
    @State private var dnsError: String?
    @State private var dnsLastLoaded: Date?
    @State private var dnsLoadedKey = ""
    @State private var dnsLoadSequence = 0
    @State private var sheet: DomainSheetRequest?
    @State private var deleteDomain: J?
    @State private var deleteHost: J?
    @State private var showingGuide = false

    private var selected: J? { model.domains.first { $0.id == selectedID } }
    private func dnsConnection(for domain: J) -> J? {
        model.dnsConnections.first { $0.id == domain["dnsConnectionId"].string }
    }
    private var visibleDomains: [J] {
        let query = search.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        if query.isEmpty { return model.domains }
        return model.domains.filter { $0["name"].string.lowercased().contains(query) || $0["note"].string.lowercased().contains(query) }
    }
    private var unmatchedAccessItems: [DomainAccessItem] {
        let represented = Set(dnsRecords.filter { $0["bindingEligible"].bool }.map { $0["fqdn"].string })
        return hosts.filter { !represented.contains($0["fqdn"].string) }
            .map { DomainAccessItem(fqdn: $0["fqdn"].string, host: $0, record: nil) }
    }

    var body: some View {
        HStack(spacing: 0) {
            VStack(spacing: 0) {
                HStack {
                    WorkspaceListHeader(title: "域名", count: model.domains.count)
                    Spacer()
                    Button { sheet = DomainSheetRequest(kind: .domain) } label: { Image(systemName: "plus") }
                        .buttonStyle(DeploySecondaryButtonStyle()).help("添加主域名")
                    Button { sheet = DomainSheetRequest(kind: .connections, domain: selected) } label: { Image(systemName: "key.horizontal") }
                        .buttonStyle(DeploySecondaryButtonStyle()).help("管理 DNS 服务商连接")
                }
                .padding(14).background(DeployStyle.overviewHeader)
                Rectangle().fill(DeployStyle.line).frame(height: 1)
                HStack(spacing: 7) {
                    Image(systemName: "magnifyingglass").foregroundStyle(DeployStyle.muted)
                    TextField("搜索主域名或备注", text: $search).textFieldStyle(.plain)
                }
                .font(.system(size: 12)).padding(10)
                .background(.white, in: RoundedRectangle(cornerRadius: 8))
                .padding(10)
                ScrollView(.vertical, showsIndicators: false) {
                    LazyVStack(spacing: 5) {
                        ForEach(visibleDomains, id: \.id) { item in
                            let isSelected = item.id == selectedID
                            Button { selectedID = item.id } label: {
                                HStack(spacing: 10) {
                                    Image(systemName: "globe")
                                        .font(.system(size: 15)).foregroundStyle(DeployStyle.green)
                                        .frame(width: 30, height: 30)
                                        .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 8))
                                    VStack(alignment: .leading, spacing: 4) {
                                        Text(item["name"].string)
                                            .font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                        Text("\(item["bindingCount"].int) 个服务绑定")
                                            .font(.system(size: 10)).foregroundStyle(DeployStyle.muted)
                                    }.lineLimit(1)
                                    Spacer(minLength: 0)
                                    if isSelected { Capsule().fill(DeployStyle.greenBright).frame(width: 3, height: 20) }
                                }
                                .padding(.horizontal, 12).frame(maxWidth: .infinity, minHeight: 56, alignment: .leading)
                                .background(isSelected ? DeployStyle.greenSoft : .clear, in: RoundedRectangle(cornerRadius: 10))
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain).accessibilityAddTraits(isSelected ? .isSelected : [])
                        }
                    }.padding(10)
                }
            }
            .frame(minWidth: 240, idealWidth: 280, maxWidth: 330)
            .background(DeployStyle.canvas)
            Rectangle().fill(DeployStyle.line).frame(width: 1)
            if let domain = selected {
                PageContainer {
                    HStack(alignment: .top) {
                        SectionHeading(domain["name"].string, domain["note"].string.isEmpty ? "根域名与子域名可分别管理" : domain["note"].string)
                        Spacer()
                        if dnsConnection(for: domain) == nil {
                            Button("添加访问域名") { sheet = DomainSheetRequest(kind: .host, domain: domain) }
                                .buttonStyle(DeployPrimaryButtonStyle())
                        }
                        Menu {
                            Button("手动添加访问域名") { sheet = DomainSheetRequest(kind: .host, domain: domain) }
                            Button("编辑资料") { sheet = DomainSheetRequest(kind: .domain, domain: domain) }
                            Button("解析记录") { sheet = DomainSheetRequest(kind: .records, domain: domain) }
                                .disabled(dnsConnection(for: domain) == nil)
                            Button("DNS 配置") { sheet = DomainSheetRequest(kind: .dnsConfig, domain: domain) }
                            Button("管理 DNS 连接") { sheet = DomainSheetRequest(kind: .connections, domain: domain) }
                            Button("扫描服务器域名服务") { sheet = DomainSheetRequest(kind: .proxyScan, domain: domain) }
                            Button("移除主域名", role: .destructive) { deleteDomain = domain }
                        } label: { Image(systemName: "ellipsis") }
                            .buttonStyle(DeploySecondaryButtonStyle())
                            .menuIndicator(.hidden)
                    }
                    HStack(spacing: 10) {
                        Image(systemName: "globe.asia.australia")
                            .foregroundStyle(DeployStyle.green)
                            .frame(width: 30, height: 30)
                            .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 8))
                        VStack(alignment: .leading, spacing: 2) {
                            Text(dnsConnection(for: domain).map { dnsProviderName($0["provider"].string) + " DNS" } ?? "尚未关联 DNS 连接")
                                .font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                            Text(dnsConnection(for: domain).map { "当前域名使用 · " + $0["name"].string }
                                 ?? (model.dnsConnections.isEmpty ? "添加服务商连接后，将其关联到此主域名" : "已保存 \(model.dnsConnections.count) 个连接，选择后即可读取解析记录"))
                                .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        Spacer()
                        Button(dnsConnection(for: domain) == nil ? "连接 DNS" : "DNS 配置") { sheet = DomainSheetRequest(kind: .dnsConfig, domain: domain) }
                            .buttonStyle(DeploySecondaryButtonStyle())
                    }
                    .padding(16).deployCard(fill: DeployStyle.overviewPanel)
                    if let hostsError {
                        DNSRecordLoadErrorView(detail: hostsError, title: "服务绑定状态读取失败")
                    }

                    VStack(spacing: 0) {
                        DomainListHeader("DNS 解析记录", subtitle: dnsRecordSubtitle(for: domain)) {
                            if !domain["dnsConnectionId"].string.isEmpty {
                                HStack(spacing: AppSpacing.sm) {
                                    Group {
                                        if dnsLoading {
                                            ProgressView().controlSize(.small)
                                                .accessibilityLabel("正在刷新解析记录")
                                        } else {
                                            Color.clear
                                        }
                                    }
                                    .frame(width: 16, height: 16)
                                    Button("管理解析") { sheet = DomainSheetRequest(kind: .records, domain: domain) }
                                        .buttonStyle(DeploySecondaryButtonStyle())
                                        .help("查看、新增和编辑服务商解析记录")
                                }
                            }
                        }
                        Rectangle().fill(DeployStyle.line).frame(height: 1)
                        if domain["dnsConnectionId"].string.isEmpty {
                            Text("点击上方“连接 DNS”，选择已保存的连接并保存，随后自动读取服务商已有记录。")
                                .font(.system(size: 12)).foregroundStyle(DeployStyle.muted)
                                .frame(maxWidth: .infinity, alignment: .leading).padding(18)
                        } else {
                            if dnsLoading && dnsLastLoaded == nil {
                                ProgressView("正在从服务商读取解析记录…")
                                    .frame(maxWidth: .infinity, alignment: .leading).padding(18)
                            }
                            if let dnsError {
                                DNSRecordLoadErrorView(detail: dnsError).padding(18)
                            }
                            if dnsRecords.isEmpty && !dnsLoading && dnsError == nil {
                                Text("服务商返回 0 条解析记录。")
                                    .font(.system(size: 12)).foregroundStyle(DeployStyle.muted)
                                    .frame(maxWidth: .infinity, alignment: .leading).padding(18)
                            }
                            ForEach(Array(dnsRecords.prefix(10)), id: \.id) { record in
                                let provider = dnsConnection(for: domain)?["provider"].string ?? ""
                                let name = dnsRecordDisplayName(record, domain: domain)
                                let linkedHost = hosts.first { $0["fqdn"].string == record["fqdn"].string }
                                let binding = linkedHost?["binding"] ?? J(nil)
                                let serviceState = domainBindingState(binding)
                                DomainDNSRecordRow(hostRecord: record["rr"].string, name: name,
                                                   type: record["type"].string, value: record["value"].string,
                                                   ttl: record["ttl"].int, status: dnsRecordStatusText(record, provider: provider),
                                                   statusTone: dnsRecordStatusTone(record, provider: provider),
                                                   serviceSummary: domainBindingSummary(binding, deployments: model.deployments),
                                                   serviceStatus: serviceState.title, serviceTone: serviceState.tone) {
                                    HStack(spacing: AppSpacing.sm) {
                                        Button(binding.isNull ? "服务绑定" : "管理绑定") { openDNSBinding(record, domain: domain) }
                                            .buttonStyle(DeploySecondaryButtonStyle())
                                            .accessibilityLabel("\(binding.isNull ? "服务绑定" : "管理绑定")，\(name)")
                                            .disabled(!record["bindingEligible"].bool || dnsLoading || loading)
                                            .help(record["bindingEligible"].bool ? "为此域名选择部署服务" : record["bindingDisabledReason"].string)
                                        Menu {
                                            Button("复制主机记录") { copyDNSRecordText(record["rr"].string) }
                                            Button("复制域名") { copyDNSRecordText(name) }
                                            Button("复制解析值") { copyDNSRecordText(record["value"].string) }
                                            if let linkedHost, linkedHost["label"].string != "@" {
                                                Divider()
                                                Button("编辑访问备注") { sheet = DomainSheetRequest(kind: .hostNote, host: linkedHost) }
                                            }
                                        } label: { Image(systemName: "ellipsis") }
                                            .buttonStyle(DeploySecondaryButtonStyle())
                                            .menuIndicator(.hidden)
                                            .accessibilityLabel("更多解析操作，\(name)")
                                    }
                                }
                                Rectangle().fill(DeployStyle.line).frame(height: 1)
                            }
                            if dnsRecords.count > 10 {
                                Text("还有 \(dnsRecords.count - 10) 条记录，可在“管理解析”中查看。")
                                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                                    .frame(maxWidth: .infinity, alignment: .leading).padding(18)
                            }
                            Text("从解析记录直接绑定服务；同一域名的多条解析共用一个服务绑定。")
                                .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .padding(.horizontal, 18).padding(.vertical, 12)
                        }
                    }
                    .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
                    .deployCard()

                    DisclosureGroup("域名配置说明", isExpanded: $showingGuide) {
                      VStack(alignment: .leading, spacing: 12) {
                        HStack(alignment: .top, spacing: 12) {
                            guideStep("1", "选择 DNS 服务商", "在 DNS 配置中选择阿里云、Cloudflare 或外部手动配置。")
                            guideStep("2", "配置解析记录", "连接后在应用内管理记录；A/AAAA 指向入口公网 IP，不填写端口。")
                            guideStep("3", "绑定服务", "从对应解析记录直接绑定服务，无需重复添加子域名。")
                        }
                        Text("连接服务商不等于切换权威 NS。请确认域名当前使用该服务商的 DNS；若需迁移，先同步原有网站与邮件记录。")
                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                      }
                      .padding(.top, 12)
                    }
                    .font(.system(size: 12, weight: .semibold))
                    .padding(16).deployCard()

                    if !unmatchedAccessItems.isEmpty && (!dnsLoading || dnsLastLoaded != nil) {
                        VStack(spacing: 0) {
                            DomainListHeader(domain["dnsConnectionId"].string.isEmpty ? "服务绑定" : "其他访问项",
                                             subtitle: domain["dnsConnectionId"].string.isEmpty
                                                ? "手动配置的访问域名"
                                                : "未匹配到当前 DNS 记录，已有配置已保留") { EmptyView() }
                            Rectangle().fill(DeployStyle.line).frame(height: 1)
                            ForEach(unmatchedAccessItems) { item in
                                domainRow(item, domain: domain)
                                if item.id != unmatchedAccessItems.last?.id {
                                    Rectangle().fill(DeployStyle.line).frame(height: 1).padding(.horizontal, 18)
                                }
                            }
                        }
                        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
                        .deployCard()
                    }
                    Text("入口已应用不等于 DNS 和 HTTPS 已就绪；草稿尚未修改服务器配置。")
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                }
                .task(id: "\(domain.id):\(domain["dnsConnectionId"].string)") {
                    refreshDomainData(domain)
                }
            } else {
                VStack(spacing: 12) {
                    Image(systemName: "globe").font(.system(size: 28)).foregroundStyle(DeployStyle.green)
                        .frame(width: 68, height: 68).background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 19))
                    Text(model.domains.isEmpty ? "还没有主域名" : "选择一个主域名")
                        .font(.system(size: 16, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                    Text("添加已有域名后，先选择 DNS 服务商连接，再管理解析记录并绑定部署。")
                        .font(.system(size: 12)).foregroundStyle(DeployStyle.muted)
                    if model.domains.isEmpty {
                        Button("添加主域名") { sheet = DomainSheetRequest(kind: .domain) }
                            .buttonStyle(DeployPrimaryButtonStyle())
                    }
                }.frame(maxWidth: .infinity, maxHeight: .infinity).background(DeployStyle.canvas)
            }
        }
        .foregroundStyle(DeployStyle.ink)
        .environment(\.colorScheme, .light)
        .onAppear { if selectedID == nil { selectedID = model.domains.first?.id } }
        .onChange(of: loading || dnsLoading) { model.domainDataLoading = $0 }
        .onDisappear { model.domainDataLoading = false }
        .onChange(of: model.manualRefreshRevision) { _ in
            if let domain = selected, sheet == nil, !loading && !dnsLoading { refreshDomainData(domain) }
        }
        .onChange(of: model.domains.map(\.id)) { ids in
            if let id = selectedID, !ids.contains(id) { selectedID = ids.first }
            if selectedID == nil { selectedID = ids.first }
        }
        .sheet(item: $sheet, onDismiss: {
            if let domain = selected { refreshDomainData(domain) }
        }) { request in
            switch request.kind {
            case .domain:
                DomainFormView(domain: request.domain) { createdID in
                    if let createdID { selectedID = createdID }
                }.environmentObject(model)
            case .host:
                if let domain = request.domain {
                    DomainHostFormView(domain: domain) { loadHosts(domain.id) }.environmentObject(model)
                }
            case .hostNote:
                if let host = request.host {
                    DomainHostNoteFormView(host: host) { loadHosts(host["domainId"].string) }.environmentObject(model)
                }
            case .dnsConfig:
                if let domain = request.domain { DomainDNSConfigurationView(domain: domain).environmentObject(model) }
            case .connections:
                DNSConnectionsView(domainName: request.domain?["name"].string ?? "").environmentObject(model)
            case .records:
                if let domain = request.domain {
                    DNSRecordsView(domain: domain).environmentObject(model)
                }
            case .proxyScan:
                ServerNginxRoutesView(domainName: request.domain?["name"].string ?? "")
                    .environmentObject(model)
            case .binding:
                if let host = request.host {
                    if ["active", "applying", "removing"].contains(host["binding"]["status"].string) {
                        DomainAppliedBindingView(host: host) { loadHosts(host["domainId"].string) }.environmentObject(model)
                    } else {
                        DomainBindingDraftView(host: host) { loadHosts(host["domainId"].string) }.environmentObject(model)
                    }
                }
            case .dnsBinding:
                if let domain = request.domain, let recordID = request.recordID {
                    DNSRecordBindingView(domainID: domain.id, recordID: recordID) {
                        refreshDomainData(domain)
                    }.environmentObject(model)
                }
            }
        }
        .confirmationDialog("移除主域名？", isPresented: Binding(get: { deleteDomain != nil }, set: { if !$0 { deleteDomain = nil } })) {
            Button("仅移除本地记录", role: .destructive) {
                guard let value = deleteDomain else { return }
                Task { _ = await model.perform("DeleteDomain", [value.id]); deleteDomain = nil }
            }
        } message: { Text("请先移除子域名和绑定。服务商处的解析记录不会随本地记录删除。") }
        .confirmationDialog("移除子域名？", isPresented: Binding(get: { deleteHost != nil }, set: { if !$0 { deleteHost = nil } })) {
            Button("仅移除本地记录", role: .destructive) {
                guard let value = deleteHost else { return }
                Task { _ = await model.perform("DeleteDomainHost", [value.id]); loadHosts(value["domainId"].string); deleteHost = nil }
            }
        } message: { Text("已绑定的子域名需要先解绑。服务商处的解析记录仍会保留。") }
    }

    private func guideStep(_ number: String, _ title: String, _ detail: String) -> some View {
        HStack(alignment: .top, spacing: 9) {
            Text(number).font(.system(size: 11, weight: .bold)).foregroundStyle(DeployStyle.green)
                .frame(width: 22, height: 22).background(DeployStyle.greenSoft, in: Circle())
            VStack(alignment: .leading, spacing: 4) {
                Text(title).font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                Text(detail).font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }.frame(maxWidth: .infinity, alignment: .leading)
    }

    private func domainRow(_ item: DomainAccessItem, domain: J) -> some View {
        let host = item.host ?? J(nil)
        let binding = item.binding
        let state = domainBindingState(binding)
        let source = item.record == nil ? "本地访问项 · DNS 待核对"
            : (item.fqdn == domain["name"].string ? "根域名 · DNS 记录" : "子域名 · DNS 记录")
        let backend = binding.isNull ? "" : "\(binding["upstreamProtocol"].string.uppercased()) · 容器端口 \(String(binding["containerPort"].int))"
        return DomainServiceAccessRow(name: item.fqdn, source: host["note"].string.isEmpty ? source : source + " · " + host["note"].string,
                                      target: binding.isNull ? "尚未绑定部署" : deploymentName(binding["deploymentId"].string),
                                      backend: backend, status: state.title, statusTone: state.tone, bound: !binding.isNull) {
            HStack(spacing: AppSpacing.xs) {
                Button(binding.isNull ? "绑定服务" : binding["status"].string == "active" ? "管理入口" : "管理绑定") {
                    if let record = item.record { openDNSBinding(record, domain: domain) }
                    else { sheet = DomainSheetRequest(kind: .binding, domain: domain, host: host) }
                }.buttonStyle(DeploySecondaryButtonStyle()).disabled(loading)
                    .accessibilityLabel("\(binding.isNull ? "绑定服务" : "管理绑定")，\(item.fqdn)")
                Group {
                    if item.host != nil && host["label"].string != "@" {
                        Menu {
                            Button("编辑备注") { sheet = DomainSheetRequest(kind: .hostNote, host: host) }
                            if item.record == nil {
                                Button("移除访问项", role: .destructive) { deleteHost = host }
                            }
                        } label: { Image(systemName: "ellipsis") }
                            .buttonStyle(DeploySecondaryButtonStyle())
                            .menuIndicator(.hidden)
                            .accessibilityLabel("更多操作，\(item.fqdn)")
                    } else {
                        Color.clear.accessibilityHidden(true)
                    }
                }.frame(width: 40, height: 32)
            }
        }
    }

    private func openDNSBinding(_ record: J, domain: J) {
        if let host = hosts.first(where: { $0["fqdn"].string == record["fqdn"].string }) {
            sheet = DomainSheetRequest(kind: .binding, domain: domain, host: host)
        } else {
            sheet = DomainSheetRequest(kind: .dnsBinding, domain: domain, recordID: record.id)
        }
    }

    private func deploymentName(_ id: String) -> String {
        guard let value = model.deployments.first(where: { $0.id == id }) else { return "部署已移除" }
        return "\(value["projectName"].string) · \(value["serverName"].string)"
    }

    private func copyDNSRecordText(_ value: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(value, forType: .string)
    }

    private func dnsRecordSubtitle(for domain: J) -> String {
        guard let connection = dnsConnection(for: domain) else { return "尚未连接服务商" }
        let provider = dnsProviderName(connection["provider"].string)
        guard let dnsLastLoaded, dnsLoadedKey == "\(domain.id):\(connection.id)" else {
            return "\(provider) · 尚未读取"
        }
        return "\(provider) · \(dnsRecords.count) 条 · 上次读取 \(dnsLastLoaded.formatted(date: .omitted, time: .standard))"
    }

    private func refreshDomainData(_ domain: J) {
        loadHosts(domain.id)
        loadDNSRecords(domain)
    }

    private func loadHosts(_ id: String) {
        if loadedHostsDomainID != id {
            hosts = []
            loadedHostsDomainID = id
        }
        hostsLoadSequence += 1
        let sequence = hostsLoadSequence
        loading = true
        hostsError = nil
        Task {
            do {
                let result = try await NativeBridge.call("ListDomainHosts", [id])
                guard selectedID == id && hostsLoadSequence == sequence else { return }
                hosts = result.array
            } catch {
                guard selectedID == id && hostsLoadSequence == sequence else { return }
                hostsError = error.localizedDescription
            }
            loading = false
        }
    }

    private func loadDNSRecords(_ domain: J) {
        let connectionID = domain["dnsConnectionId"].string
        let key = "\(domain.id):\(connectionID)"
        dnsLoadSequence += 1
        let sequence = dnsLoadSequence
        if dnsLoadedKey != key {
            dnsRecords = []
            dnsLastLoaded = nil
            dnsLoadedKey = key
        }
        dnsError = nil
        guard !connectionID.isEmpty else {
            dnsLoading = false
            return
        }
        dnsLoading = true
        Task {
            do {
                let result = try await NativeBridge.call("ListDNSRecords", [domain.id])
                guard selectedID == domain.id && dnsLoadSequence == sequence else { return }
                dnsRecords = result.array
                dnsLastLoaded = Date()
            } catch {
                guard selectedID == domain.id && dnsLoadSequence == sequence else { return }
                dnsError = error.localizedDescription
            }
            dnsLoading = false
        }
    }
}
