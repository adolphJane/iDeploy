import AppKit
import SwiftUI

private struct DomainFormShell<Content: View>: View {
    @Environment(\.dismiss) private var dismiss
    let title: String
    let subtitle: String
    let saveTitle: String
    let canSave: Bool
    let save: () -> Void
    @ViewBuilder let content: Content

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 14) {
                Image(systemName: "globe")
                    .font(.system(size: 20)).foregroundStyle(DeployStyle.green)
                    .frame(width: 48, height: 48)
                    .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 13))
                VStack(alignment: .leading, spacing: 5) {
                    Text(title).font(.system(size: 21, weight: .bold)).foregroundStyle(DeployStyle.ink)
                    Text(subtitle).font(.system(size: 12)).foregroundStyle(DeployStyle.muted)
                }
                Spacer()
            }
            .padding(20).frame(maxWidth: .infinity, alignment: .leading)
            .background(DeployStyle.overviewHeader)
            ScrollView { content.padding(20).frame(maxWidth: .infinity, alignment: .leading) }
                .background(DeployStyle.canvas)
            Rectangle().fill(DeployStyle.line).frame(height: 1)
            HStack {
                Spacer()
                Button("取消") { dismiss() }.buttonStyle(DeploySecondaryButtonStyle()).keyboardShortcut(.cancelAction)
                Button(saveTitle, action: save).buttonStyle(DeployPrimaryButtonStyle())
                    .keyboardShortcut(.defaultAction).disabled(!canSave)
            }.padding(20).background(DeployStyle.overviewPanel)
        }
        .frame(width: 580, height: 440)
        .environment(\.colorScheme, .light)
    }
}

struct DomainFormView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let domain: J?
    let onSaved: (String?) -> Void
    @State private var name: String
    @State private var note: String
    @State private var connectionID: String

    init(domain: J?, onSaved: @escaping (String?) -> Void) {
        self.domain = domain
        self.onSaved = onSaved
        _name = State(initialValue: domain?["name"].string ?? "")
        _note = State(initialValue: domain?["note"].string ?? "")
        _connectionID = State(initialValue: domain?["dnsConnectionId"].string ?? "")
    }

    var body: some View {
        DomainFormShell(title: domain == nil ? "添加主域名" : "编辑主域名", subtitle: "主域名下可分别管理 @ 和子域名。", saveTitle: "保存", canSave: !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty) {
            Task {
                let input: [String: Any] = ["name": name, "note": note, "dnsConnectionId": connectionID]
                let result = await model.perform(domain == nil ? "CreateDomain" : "UpdateDomain", domain == nil ? [input] : [domain!.id, input])
                if let result { onSaved(domain == nil ? result.id : nil); dismiss() }
            }
        } content: {
            VStack(alignment: .leading, spacing: 17) {
                SectionHeading("域名信息", "只填写可注册的主域名，不包含协议或端口")
                TextField("例如 example.com", text: $name).textFieldStyle(.roundedBorder)
                    .disabled(domain != nil).accessibilityLabel("主域名")
                TextField("备注，可选", text: $note).textFieldStyle(.roundedBorder)
                    .accessibilityLabel("备注")
                Picker("DNS 管理", selection: $connectionID) {
                    Text("外部手动配置").tag("")
                    ForEach(model.dnsConnections, id: \.id) { connection in
                        Text("\(dnsProviderName(connection["provider"].string)) · \(connection["name"].string)").tag(connection.id)
                    }
                }
                Text("添加主域名只建立本地记录；选择 DNS 服务商连接后，可在应用内查询和修改解析记录。")
                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
            }.padding(16).deployCard(fill: DeployStyle.overviewPanel)
        }
    }
}

struct DomainHostFormView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let domain: J
    let onSaved: () -> Void
    @State private var label = ""
    @State private var note = ""

    var body: some View {
        DomainFormShell(title: "添加子域名", subtitle: "所属主域名：\(domain["name"].string)", saveTitle: "添加", canSave: !label.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty) {
            Task {
                let input: [String: Any] = ["domainId": domain.id, "label": label, "note": note]
                if await model.perform("CreateDomainHost", [input]) != nil { onSaved(); dismiss() }
            }
        } content: {
            VStack(alignment: .leading, spacing: 17) {
                SectionHeading("子域名", "根域名 @ 已自动建立")
                HStack(spacing: 8) {
                    TextField("例如 app", text: $label).textFieldStyle(.roundedBorder)
                        .accessibilityLabel("子域名前缀")
                    Text(".\(domain["name"].string)").foregroundStyle(DeployStyle.muted)
                }
                TextField("备注，可选", text: $note).textFieldStyle(.roundedBorder)
                    .accessibilityLabel("备注")
                Text("添加访问项不会自动修改 DNS 记录。")
                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
            }.padding(16).deployCard(fill: DeployStyle.overviewPanel)
        }
    }
}

struct DomainHostNoteFormView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let host: J
    let onSaved: () -> Void
    @State private var note: String

    init(host: J, onSaved: @escaping () -> Void) {
        self.host = host
        self.onSaved = onSaved
        _note = State(initialValue: host["note"].string)
    }

    var body: some View {
        DomainFormShell(title: "编辑子域名备注", subtitle: host["fqdn"].string, saveTitle: "保存", canSave: true) {
            Task {
                if await model.perform("UpdateDomainHost", [host.id, note]) != nil { onSaved(); dismiss() }
            }
        } content: {
            VStack(alignment: .leading, spacing: 15) {
                SectionHeading("子域名资料", "名称创建后不可直接修改")
                TextField("备注，可选", text: $note).textFieldStyle(.roundedBorder)
                    .accessibilityLabel("备注")
            }.padding(16).deployCard(fill: DeployStyle.overviewPanel)
        }
    }
}

private struct DNSConnectionFormRequest: Identifiable {
    let id = UUID()
    let connection: J?
}

struct DNSConnectionFormView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let connection: J?
    let domainName: String
    let onSaved: ((String) -> Void)?
    @State private var name: String
    @State private var accessKeyID: String
    @State private var secret = ""
    @State private var provider: String
    @State private var testDomainName: String
    @State private var isTesting = false
    @State private var isSaving = false
    @State private var testMessage = ""
    @State private var testSucceeded = false

    init(connection: J? = nil, domainName: String = "", onSaved: ((String) -> Void)? = nil) {
        self.connection = connection
        self.domainName = domainName
        self.onSaved = onSaved
        _name = State(initialValue: connection?["name"].string ?? "")
        _accessKeyID = State(initialValue: connection?["accessKeyId"].string ?? "")
        _provider = State(initialValue: connection?["provider"].string ?? "aliyun")
        _testDomainName = State(initialValue: domainName)
    }

    private var hasNewSecret: Bool { !secret.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
    private var changedAccessKeyID: Bool {
        connection != nil && provider == "aliyun" &&
        accessKeyID.trimmingCharacters(in: .whitespacesAndNewlines) != connection?["accessKeyId"].string
    }
    private var canTest: Bool {
        (hasNewSecret || connection != nil) &&
        (!changedAccessKeyID || hasNewSecret) &&
        (provider != "aliyun" || !accessKeyID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
    }

    private var canSave: Bool {
        !isTesting && !isSaving && !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && canTest
    }

    private func testConnection() {
        let input: [String: Any] = ["provider": provider, "accessKeyId": accessKeyID,
                                    "accessKeySecret": provider == "aliyun" ? secret : "",
                                    "apiToken": provider == "cloudflare" ? secret : "",
                                    "domainName": testDomainName]
        Task {
            isTesting = true
            testSucceeded = false
            testMessage = "正在测试 DNS 连接…"
            defer { isTesting = false }
            do {
                let result: J
                if let connection, !hasNewSecret {
                    result = try await NativeBridge.call("TestSavedDNSConnection", [
                        ["id": connection.id, "domainName": testDomainName]
                    ])
                } else {
                    result = try await NativeBridge.call("TestDNSConnection", [input])
                }
                testSucceeded = true
                testMessage = result.string
            } catch {
                testMessage = "连接失败：\(error.localizedDescription)"
            }
        }
    }

    var body: some View {
        DomainFormShell(title: connection == nil ? "添加 DNS 连接" : "编辑 DNS 连接",
                        subtitle: connection == nil ? "选择 DNS 服务商并保存访问凭据。" : "更新连接信息；留空的密钥字段会保留原值。",
                        saveTitle: "保存连接", canSave: canSave) {
            Task {
                isSaving = true
                defer { isSaving = false }
                let input: [String: Any] = ["provider": provider, "name": name, "accessKeyId": accessKeyID,
                                            "accessKeySecret": provider == "aliyun" ? secret : "",
                                            "apiToken": provider == "cloudflare" ? secret : ""]
                let method = connection == nil ? "CreateDNSConnection" : "UpdateDNSConnection"
                let args: [Any] = connection == nil ? [input] : [connection!.id, input]
                if let result = await model.perform(method, args) {
                    onSaved?(result.id)
                    dismiss()
                }
            }
        } content: {
            VStack(alignment: .leading, spacing: 17) {
                SectionHeading("连接凭据", "密钥保存在本机 Keychain")
                HStack(spacing: 10) {
                    Picker("DNS 服务商", selection: $provider) {
                        Text("阿里云 DNS").tag("aliyun")
                        Text("Cloudflare DNS").tag("cloudflare")
                    }
                    .disabled(connection != nil)
                    if provider == "aliyun" { AliDNSGuideButton() }
                    Spacer(minLength: 0)
                }
                if connection != nil {
                    Text("服务商类型不可修改；需要切换服务商时请新增连接。")
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                }
                TextField("连接名称", text: $name).textFieldStyle(.roundedBorder)
                    .accessibilityLabel("连接名称")
                if provider == "aliyun" {
                    TextField("AccessKey ID", text: $accessKeyID).textFieldStyle(.roundedBorder)
                        .accessibilityLabel("AccessKey ID")
                    SecureField(connection == nil ? "AccessKey Secret" : "AccessKey Secret（留空保留原值）", text: $secret)
                        .textFieldStyle(.roundedBorder)
                    if changedAccessKeyID && !hasNewSecret {
                        Text("更换 AccessKey ID 时需要填写对应的 AccessKey Secret。")
                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    }
                } else {
                    SecureField(connection == nil ? "API Token" : "API Token（留空保留原值）", text: $secret)
                        .textFieldStyle(.roundedBorder)
                }
                Text(provider == "aliyun"
                     ? "使用具备目标域名解析权限的 RAM 用户 AccessKey。"
                     : "Token 需具备目标 Zone 的 Zone Read 与 DNS Edit 权限。")
                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                if provider != "aliyun" {
                    Link("查看 Cloudflare API Token 创建指引",
                         destination: URL(string: "https://developers.cloudflare.com/fundamentals/api/get-started/create-token/")!)
                        .font(.system(size: 11))
                }
                TextField("测试域名（可选，例如 example.com）", text: $testDomainName)
                    .textFieldStyle(.roundedBorder)
                    .accessibilityLabel("测试域名")
                Text("填写主域名可验证其解析记录读取权限；留空时只测试服务商的域名列表访问权限。")
                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                HStack(spacing: 8) {
                    Button(isTesting ? "正在测试…" : "测试连接", action: testConnection)
                        .buttonStyle(DeploySecondaryButtonStyle())
                        .disabled(!canTest || isTesting || isSaving)
                    if isTesting { ProgressView().controlSize(.small) }
                }
                if !testMessage.isEmpty {
                    DNSConnectionTestResult(message: testMessage, success: testSucceeded, testing: isTesting)
                }
            }.padding(16).deployCard(fill: DeployStyle.overviewPanel)
        }
        .onChange(of: provider) { _ in testMessage = "" }
        .onChange(of: accessKeyID) { _ in testMessage = "" }
        .onChange(of: secret) { _ in testMessage = "" }
        .onChange(of: testDomainName) { _ in testMessage = "" }
    }
}

struct DNSConnectionsView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let domainName: String
    @State private var formRequest: DNSConnectionFormRequest?
    @State private var deleting: J?
    @State private var testDomainName: String
    @State private var testingID: String?
    @State private var testedID: String?
    @State private var testMessage = ""
    @State private var testSucceeded = false

    init(domainName: String = "") {
        self.domainName = domainName
        _testDomainName = State(initialValue: domainName)
    }

    private func testConnection(_ id: String) {
        let target: [String: Any] = ["id": id, "domainName": testDomainName]
        Task {
            testingID = id
            testedID = id
            testSucceeded = false
            testMessage = "正在测试 DNS 连接…"
            defer { testingID = nil }
            do {
                let result = try await NativeBridge.call("TestSavedDNSConnection", [target])
                testSucceeded = true
                testMessage = result.string
            } catch {
                testMessage = "连接失败：\(error.localizedDescription)"
            }
        }
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                VStack(alignment: .leading, spacing: 5) {
                    Text("DNS 服务商连接").font(.system(size: 21, weight: .bold)).foregroundStyle(DeployStyle.ink)
                    Text("连接保存后，在主域名的 DNS 配置中选择使用。").font(.system(size: 12)).foregroundStyle(DeployStyle.muted)
                }
                Spacer()
                Button("添加连接") { formRequest = DNSConnectionFormRequest(connection: nil) }
                    .buttonStyle(DeployPrimaryButtonStyle())
            }.padding(20).background(DeployStyle.overviewHeader)
            Group {
                if model.dnsConnections.isEmpty {
                    VStack(spacing: 10) {
                        Image(systemName: "key.horizontal")
                            .font(.system(size: 24)).foregroundStyle(DeployStyle.green)
                            .frame(width: 58, height: 58)
                            .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 16))
                        Text("尚未添加 DNS 服务商连接")
                            .font(.system(size: 14, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                        Text("添加阿里云或 Cloudflare 连接后，可在主域名的 DNS 配置中选择。")
                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                            .multilineTextAlignment(.center)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(DeployStyle.canvas)
                } else {
                    ScrollView {
                        LazyVStack(spacing: 0) {
                            VStack(alignment: .leading, spacing: 5) {
                                TextField("测试域名（可选，例如 example.com）", text: $testDomainName)
                                    .textFieldStyle(.roundedBorder)
                                    .accessibilityLabel("测试域名")
                                Text("填写主域名可验证解析记录读取权限；留空时测试服务商域名列表访问权限。")
                                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                            }
                            .padding(16)
                            Rectangle().fill(DeployStyle.line).frame(height: 1)
                            ForEach(model.dnsConnections, id: \.id) { connection in
                                VStack(alignment: .leading, spacing: 7) {
                                    HStack(spacing: 12) {
                                        Image(systemName: "key.horizontal").foregroundStyle(DeployStyle.green)
                                        VStack(alignment: .leading, spacing: 4) {
                                            Text(connection["name"].string)
                                                .font(.system(size: 12, weight: .semibold))
                                                .lineLimit(1)
                                            Text("\(dnsProviderName(connection["provider"].string)) · \(connection["provider"].string == "aliyun" ? connection["accessKeyId"].string : "API Token 已保存")")
                                                .font(.system(size: 10)).foregroundStyle(DeployStyle.muted)
                                                .lineLimit(1).truncationMode(.middle)
                                        }
                                        .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
                                        Button(testingID == connection.id ? "正在测试…" : "测试连接") { testConnection(connection.id) }
                                            .buttonStyle(DeploySecondaryButtonStyle())
                                            .disabled(testingID != nil)
                                        Button("编辑") {
                                            testMessage = ""
                                            formRequest = DNSConnectionFormRequest(connection: connection)
                                        }
                                        .buttonStyle(DeploySecondaryButtonStyle())
                                        .disabled(testingID != nil)
                                        Button("删除", role: .destructive) { deleting = connection }
                                            .buttonStyle(DeploySecondaryButtonStyle())
                                            .disabled(testingID != nil)
                                    }
                                    if testedID == connection.id && !testMessage.isEmpty {
                                        DNSConnectionTestResult(message: testMessage, success: testSucceeded,
                                                                testing: testingID == connection.id)
                                    }
                                }
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .padding(16)
                                if connection.id != model.dnsConnections.last?.id {
                                    Rectangle().fill(DeployStyle.line).frame(height: 1)
                                }
                            }
                        }
                        .frame(maxWidth: .infinity)
                        .deployCard()
                        .padding(18)
                    }
                    .background(DeployStyle.canvas)
                }
            }
            HStack {
                Text("被主域名使用的连接无法删除；凭据存放在本机 Keychain。")
                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                Spacer()
                Button("关闭") { dismiss() }.buttonStyle(DeploySecondaryButtonStyle())
            }.padding(18).background(DeployStyle.overviewPanel)
        }
        .frame(width: 590, height: 500).environment(\.colorScheme, .light)
        .sheet(item: $formRequest) { request in
            DNSConnectionFormView(connection: request.connection, domainName: testDomainName)
                .environmentObject(model)
        }
        .onChange(of: testDomainName) { _ in testMessage = "" }
        .confirmationDialog("删除 DNS 服务商连接？", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } })) {
            Button("删除连接", role: .destructive) {
                guard let target = deleting else { return }
                Task { _ = await model.perform("DeleteDNSConnection", [target.id]); deleting = nil }
            }
        } message: { Text("将删除本机保存的连接凭据，不会修改服务商的解析记录。") }
    }
}

private struct DNSConnectionTestResult: View {
    let message: String
    let success: Bool
    let testing: Bool
    @State private var copied = false

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            HStack(spacing: 8) {
                Image(systemName: testing ? "arrow.triangle.2.circlepath" : success ? "checkmark.circle.fill" : "exclamationmark.circle.fill")
                    .foregroundStyle(success ? DeployStyle.green : DeployStyle.ink)
                    .accessibilityHidden(true)
                Text(testing ? "正在测试" : success ? "连接测试成功" : "连接测试失败")
                    .font(.system(size: 12, weight: .semibold))
                    .foregroundStyle(DeployStyle.ink)
                Spacer(minLength: 8)
                if !testing {
                    Button(copied ? "已复制" : "复制详情") {
                        NSPasteboard.general.clearContents()
                        copied = NSPasteboard.general.setString(message, forType: .string)
                    }
                    .buttonStyle(DeploySecondaryButtonStyle())
                    .help("复制完整测试结果")
                }
            }
            ScrollView(.vertical) {
                Text(message)
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(DeployStyle.ink)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .frame(maxHeight: 120)
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(success ? DeployStyle.greenSoft : DeployStyle.overviewHeader,
                    in: RoundedRectangle(cornerRadius: 9))
        .overlay { RoundedRectangle(cornerRadius: 9).strokeBorder(DeployStyle.line, lineWidth: 1) }
        .onChange(of: message) { _ in copied = false }
        .accessibilityElement(children: .contain)
    }
}

#Preview("DNS 连接测试结果") {
    DNSConnectionTestResult(
        message: "连接失败：服务商拒绝读取 example.com 的解析记录。请检查凭据、目标域名和 DNS 读取权限。",
        success: false,
        testing: false
    )
    .padding(20)
    .frame(width: 540)
    .background(DeployStyle.canvas)
}

func dnsProviderName(_ provider: String) -> String {
    switch provider {
    case "aliyun": return "阿里云"
    case "cloudflare": return "Cloudflare"
    default: return "未知服务商"
    }
}

private struct AliDNSGuideButton: View {
    @State private var showingGuide = false

    private let hoverGuide = """
    创建阿里云 RAM AccessKey：
    1. RAM 控制台 → 身份管理 → 用户，创建仅用于程序访问的 RAM 用户；已有用户可在“凭证管理”中创建 AccessKey。
    2. 为该用户新增授权，选择云解析 DNS 权限（如 AliyunDNSFullAccess）。
    3. 保存仅显示一次的 AccessKey ID 和 Secret，填入“添加 DNS 连接”表单；再到主域名的 DNS 配置选择此连接。
    """

    var body: some View {
        Button { showingGuide.toggle() } label: {
            Image(systemName: "questionmark.circle")
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(DeployStyle.green)
                .frame(width: 24, height: 24)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(hoverGuide)
        .accessibilityLabel("阿里云 RAM AccessKey 创建步骤")
        .accessibilityHint(hoverGuide)
        .popover(isPresented: $showingGuide, arrowEdge: .trailing) {
            VStack(alignment: .leading, spacing: 12) {
                Text("创建阿里云 RAM AccessKey")
                    .font(.system(size: 14, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                Text("1. 在 RAM 控制台创建仅用于程序访问的用户；已有 RAM 用户可在“凭证管理”中创建 AccessKey。")
                Text("2. 为该用户新增云解析 DNS 权限。AliyunDNSFullAccess 可管理账号内全部 DNS 域名，也可使用权限范围更小的自定义策略。")
                Text("3. 保存仅显示一次的 AccessKey ID 和 Secret，填写连接表单，然后在主域名的 DNS 配置中选择此连接。")
                Link("查看阿里云官方步骤",
                     destination: URL(string: "https://help.aliyun.com/zh/ram/user-guide/create-an-accesskey-pair")!)
            }
            .font(.system(size: 12))
            .foregroundStyle(DeployStyle.muted)
            .padding(16)
            .frame(width: 370, alignment: .leading)
            .background(DeployStyle.overviewPanel)
        }
    }
}

struct DomainDNSConfigurationView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let domain: J
    @State private var provider: String
    @State private var connectionID: String
    @State private var showingConnections = false
    @State private var showingAddConnection = false
    @State private var initialized = false
    @State private var saving = false
    @State private var saveError: String?

    init(domain: J) {
        self.domain = domain
        let id = domain["dnsConnectionId"].string
        _connectionID = State(initialValue: id)
        _provider = State(initialValue: id.isEmpty ? "unselected" : "aliyun")
    }

    private var choices: [J] { model.dnsConnections.filter { $0["provider"].string == provider } }

    var body: some View {
        DomainFormShell(title: "DNS 配置", subtitle: domain["name"].string,
                        saveTitle: saving ? "正在保存…" : provider == "manual" ? "保存配置" : "保存并读取解析",
                        canSave: !saving && (provider == "manual" || choices.contains(where: { $0.id == connectionID }))) {
            Task {
                saving = true
                saveError = nil
                defer { saving = false }
                let input: [String: Any] = ["name": domain["name"].string, "note": domain["note"].string,
                                            "dnsConnectionId": provider == "manual" ? "" : connectionID]
                do {
                    _ = try await NativeBridge.call("UpdateDomain", [domain.id, input])
                    await model.refresh()
                    dismiss()
                } catch {
                    saveError = error.localizedDescription
                }
            }
        } content: {
            VStack(alignment: .leading, spacing: 17) {
                SectionHeading("关联此主域名", "已保存连接需要在这里选中并保存，才会用于读取本域名的解析记录")
                HStack(spacing: 10) {
                    Picker("DNS 服务商", selection: $provider) {
                        Text("请选择服务商").tag("unselected")
                        Text("外部手动配置").tag("manual")
                        Text("阿里云 DNS").tag("aliyun")
                        Text("Cloudflare DNS").tag("cloudflare")
                    }
                    if provider == "aliyun" { AliDNSGuideButton() }
                    Spacer(minLength: 0)
                }
                .onChange(of: provider) { _ in
                    if !choices.contains(where: { $0.id == connectionID }) {
                        connectionID = choices.count == 1 ? choices[0].id : ""
                    }
                    saveError = nil
                }
                if provider == "manual" {
                    Text("在当前 DNS 服务商处配置解析记录，然后返回域名访问项检查 DNS 与 HTTPS。")
                        .font(.system(size: 12)).foregroundStyle(DeployStyle.muted)
                } else if provider != "unselected" {
                    Picker("选择连接", selection: $connectionID) {
                        Text("请选择连接").tag("")
                        ForEach(choices, id: \.id) { connection in
                            Text(connection["name"].string).tag(connection.id)
                        }
                    }
                    if choices.isEmpty {
                        Text("尚未保存此服务商的连接，请先新增连接。")
                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    }
                    Text("点击“保存并读取解析”后，将此连接用于 \(domain["name"].string)，并返回主页显示服务商的现有解析记录。")
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    Text("关联操作不会修改解析记录或切换权威 NS；读取失败会在主页显示原因。")
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                }
                HStack(spacing: 8) {
                    Button("新增连接…") { showingAddConnection = true }
                        .buttonStyle(DeploySecondaryButtonStyle())
                    if !model.dnsConnections.isEmpty {
                        Button("管理已保存连接…") { showingConnections = true }
                            .buttonStyle(DeploySecondaryButtonStyle())
                    }
                }
                if let saveError {
                    Text("保存失败：\(saveError)")
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.ink)
                        .textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                }
            }.padding(16).deployCard(fill: DeployStyle.overviewPanel)
        }
        .onAppear {
            guard !initialized else { return }
            initialized = true
            if let connection = model.dnsConnections.first(where: { $0.id == connectionID }) {
                provider = connection["provider"].string
            } else if model.dnsConnections.count == 1 {
                let connection = model.dnsConnections[0]
                provider = connection["provider"].string
                connectionID = connection.id
            }
        }
        .sheet(isPresented: $showingAddConnection) {
            DNSConnectionFormView(domainName: domain["name"].string) { id in
                if let connection = model.dnsConnections.first(where: { $0.id == id }) {
                    provider = connection["provider"].string
                    connectionID = id
                }
            }.environmentObject(model)
        }
        .sheet(isPresented: $showingConnections) { DNSConnectionsView(domainName: domain["name"].string).environmentObject(model) }
    }
}

#Preview("DNS 配置") {
    DomainDNSConfigurationView(domain: J(["id": "preview", "name": "example.com", "note": "", "dnsConnectionId": ""]))
        .environmentObject(AppModel())
}

#Preview("DNS 服务商连接") {
    DNSConnectionFormView().environmentObject(AppModel())
}

#Preview("编辑 DNS 连接") {
    DNSConnectionFormView(connection: J(["id": "preview", "name": "阿里云 DNS", "provider": "aliyun", "accessKeyId": "example-key-id"]))
        .environmentObject(AppModel())
}

#Preview("DNS 连接空态") {
    DNSConnectionsView().environmentObject(AppModel())
}

#Preview("阿里云 DNS 引导") {
    AliDNSGuideButton().padding(30)
}

struct DNSRecordBindingView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let domainID: String
    let recordID: String
    let onSaved: () -> Void
    @State private var host: J?
    @State private var errorMessage: String?
    @State private var targetMode = "deployment"

    var body: some View {
        Group {
            if let host {
                VStack(spacing: 0) {
                    if host["binding"].isNull && host["externalRoute"].isNull {
                        Picker("服务来源", selection: $targetMode) {
                            Text("iDeploy 部署").tag("deployment")
                            Text("服务器现有服务").tag("server")
                        }.pickerStyle(.segmented).padding(16).background(DeployStyle.overviewHeader)
                    }
                    if !host["externalRoute"].isNull || targetMode == "server" {
                        DomainExternalRouteView(host: host, onSaved: onSaved)
                    } else if ["active", "applying", "removing"].contains(host["binding"]["status"].string) {
                        DomainAppliedBindingView(host: host, onSaved: onSaved)
                    } else {
                        DomainBindingDraftView(host: host, onSaved: onSaved)
                    }
                }
            } else {
                VStack(alignment: .leading, spacing: 16) {
                    SectionHeading("服务绑定", "按 DNS 完整域名匹配已有服务配置")
                    if let errorMessage {
                        DNSRecordLoadErrorView(detail: errorMessage, title: "无法读取服务绑定")
                        HStack {
                            Button("关闭") { dismiss() }.buttonStyle(DeploySecondaryButtonStyle())
                                .keyboardShortcut(.cancelAction)
                            Button("重试") { Task { await resolve() } }.buttonStyle(DeployPrimaryButtonStyle())
                        }
                    } else {
                        ProgressView("正在读取域名及已有绑定…")
                    }
                }
                .padding(24).frame(width: 580, alignment: .leading)
                .background(DeployStyle.canvas)
            }
        }
        .foregroundStyle(DeployStyle.ink)
        .environment(\.colorScheme, .light)
        .task { await resolve() }
    }

    private func resolve() async {
        errorMessage = nil
        do {
            let resolved = try await NativeBridge.call("PrepareDNSRecordBinding", [domainID, recordID])
            await model.refresh()
            host = resolved
            if !resolved["externalRoute"].isNull { targetMode = "server" }
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}

#Preview("从 DNS 记录绑定服务") {
    DNSRecordBindingView(domainID: "preview-domain", recordID: "preview-record", onSaved: {})
        .environmentObject(AppModel())
}

struct DomainBindingDraftView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let host: J
    let onSaved: () -> Void
    @State private var deploymentID: String
    @State private var serviceName: String
    @State private var containerPort: String
    @State private var upstreamProtocol: String
    @State private var ingressMode: String
    @State private var useHTTPS: Bool
    @State private var certificatePath: String
    @State private var certificateKeyPath: String
    @State private var certificateScan = J(nil)
    @State private var scanningCertificates = false
    @State private var certificateScanError = ""
    @State private var certbotEmail = ""
    @State private var certbotWebroot = ""
    @State private var certbotCheck = J(nil)
    @State private var checkingCertbot = false
    @State private var issuingCertbot = false
    @State private var certbotMessage = ""
    @State private var endpointDiscovery = J(nil)
    @State private var discovering = false
    @State private var discoverySequence = 0
    @State private var selectedEndpointKey = ""
    @State private var plan = J(nil)
    @State private var planning = false
    @State private var queuedJob: J?

    private var discoveredEndpoints: [J] { endpointDiscovery["endpoints"].array }

    private var canSaveDraft: Bool {
        guard !deploymentID.isEmpty, !discovering, let port = Int(containerPort), (1...65535).contains(port) else { return false }
        let targetReady = discoveredEndpoints.isEmpty || discoveredEndpoints.contains { endpoint in
            endpoint.selfPortKey == selectedEndpointKey && !hasAmbiguousMapping(endpoint, in: discoveredEndpoints)
        }
        return targetReady && (ingressMode != "nginx" || !useHTTPS || (!certificatePath.isEmpty && !certificateKeyPath.isEmpty))
    }

    private func hasAmbiguousMapping(_ endpoint: J, in endpoints: [J]) -> Bool {
        endpoints.contains { other in
            other.selfPortKey != endpoint.selfPortKey &&
                other["containerName"].string == endpoint["containerName"].string &&
                other["containerPort"].int == endpoint["containerPort"].int &&
                other["hostPort"].int != endpoint["hostPort"].int
        }
    }

    private var draftDirty: Bool {
        let saved = host["binding"]
        return saved.isNull || deploymentID != saved["deploymentId"].string ||
            serviceName != saved["serviceName"].string || containerPort != saved["containerPort"].string ||
            upstreamProtocol != saved["upstreamProtocol"].string || ingressMode != saved["ingressMode"].string ||
            (useHTTPS ? certificatePath : "") != saved["certificatePath"].string ||
            (useHTTPS ? certificateKeyPath : "") != saved["certificateKeyPath"].string
    }

    init(host: J, onSaved: @escaping () -> Void) {
        self.host = host
        self.onSaved = onSaved
        let binding = host["binding"]
        _deploymentID = State(initialValue: binding["deploymentId"].string)
        _serviceName = State(initialValue: binding["serviceName"].string)
        _containerPort = State(initialValue: binding["containerPort"].string)
        _upstreamProtocol = State(initialValue: "http")
        _ingressMode = State(initialValue: binding["ingressMode"].string.isEmpty ? "caddy" : binding["ingressMode"].string)
        _useHTTPS = State(initialValue: !binding["certificatePath"].string.isEmpty)
        _certificatePath = State(initialValue: binding["certificatePath"].string)
        _certificateKeyPath = State(initialValue: binding["certificateKeyPath"].string)
    }

    var body: some View {
        DomainFormShell(title: "配置访问目标", subtitle: host["fqdn"].string, saveTitle: "保存草稿", canSave: canSaveDraft) {
            Task {
                let input: [String: Any] = ["hostId": host.id, "deploymentId": deploymentID,
                                            "serviceName": serviceName, "containerPort": Int(containerPort) ?? 0,
                                            "upstreamProtocol": "http", "ingressMode": ingressMode,
                                            "certificatePath": ingressMode == "nginx" && useHTTPS ? certificatePath : "",
                                            "certificateKeyPath": ingressMode == "nginx" && useHTTPS ? certificateKeyPath : ""]
                if await model.perform("SaveDomainBindingDraft", [input]) != nil { onSaved(); dismiss() }
            }
        } content: {
            VStack(alignment: .leading, spacing: 17) {
                HStack {
                    SectionHeading("部署目标", "保存为待应用配置")
                    Spacer()
                    Group {
                        if discovering {
                            ProgressView().controlSize(.small)
                                .accessibilityLabel("正在查询远端端口")
                        } else {
                            Color.clear
                        }
                    }
                    .frame(width: 16, height: 16)
                }
                Picker("部署", selection: $deploymentID) {
                    Text("选择部署").tag("")
                    ForEach(model.deployments, id: \.id) { deployment in
                        Text("\(deployment["projectName"].string) · \(deployment["serverName"].string)").tag(deployment.id)
                    }
                }
                if !deploymentID.isEmpty {
                    let endpoints = discoveredEndpoints
                    VStack(alignment: .leading, spacing: 8) {
                        Text("实际端口映射").font(.system(size: 12, weight: .semibold))
                        Group {
                            if discovering {
                                Text("正在查询远端端口…")
                                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                            } else if endpoints.isEmpty {
                                Text(endpointDiscovery["detail"].string.isEmpty ? "尚无可推荐的实际端口" : endpointDiscovery["detail"].string)
                                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                            } else {
                                ScrollView(.vertical, showsIndicators: endpoints.count > 2) {
                                    VStack(alignment: .leading, spacing: 6) {
                                        ForEach(endpoints, id: \.selfPortKey) { endpoint in
                                            let selected = selectedEndpointKey == endpoint.selfPortKey
                                            let ambiguous = hasAmbiguousMapping(endpoint, in: endpoints)
                                            Button {
                                                selectedEndpointKey = endpoint.selfPortKey
                                                serviceName = endpoint["containerName"].string
                                                containerPort = endpoint["containerPort"].string
                                            } label: {
                                                HStack(spacing: 8) {
                                                    Image(systemName: selected ? "checkmark.circle.fill" : "circle")
                                                        .accessibilityHidden(true)
                                                    Text("\(endpoint["containerName"].string) · 服务器 \(endpoint["hostPort"].string) → 容器 \(endpoint["containerPort"].string)/tcp")
                                                    Spacer(minLength: 0)
                                                    if endpoint["bindAddresses"].array.count > 1 {
                                                        Text("IPv4 / IPv6").font(.system(size: 10))
                                                    }
                                                }
                                                .font(.system(size: 11, weight: selected ? .semibold : .regular))
                                                .foregroundStyle(selected ? DeployStyle.green : DeployStyle.ink)
                                                .padding(.horizontal, 10).padding(.vertical, 8)
                                                .background(selected ? DeployStyle.greenSoft : Color.white, in: RoundedRectangle(cornerRadius: 9))
                                                .overlay(RoundedRectangle(cornerRadius: 9).stroke(selected ? DeployStyle.green : DeployStyle.line))
                                            }
                                            .buttonStyle(.plain)
                                            .disabled(ambiguous)
                                            .accessibilityLabel("\(endpoint["containerName"].string)，服务器端口 \(endpoint["hostPort"].string)，容器端口 \(endpoint["containerPort"].string)\(ambiguous ? "，映射不唯一" : "")")
                                            .accessibilityAddTraits(selected ? [.isSelected] : [])
                                        }
                                    }.frame(maxWidth: .infinity, alignment: .leading)
                                }
                            }
                        }
                        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
                        if !discovering && !endpoints.isEmpty {
                            Text(endpoints.contains(where: { hasAmbiguousMapping($0, in: endpoints) })
                                 ? "同一容器端口对应多个服务器端口的候选项暂不可选；请先调整这些映射。"
                                 : "选择一个实际映射即可确定服务与容器端口；HTTP 和入口可达性将在预检时验证。")
                                .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                        }
                    }
                    .frame(height: 145, alignment: .topLeading)
                }
                if !deploymentID.isEmpty && !discovering && discoveredEndpoints.isEmpty {
                    Text("未扫描到可选映射，可先手动保存草稿；应用前仍需发布对应的 TCP 端口。")
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    TextField("容器名称或 Compose 服务名", text: $serviceName).textFieldStyle(.roundedBorder)
                    HStack {
                        TextField("容器内部端口，例如 8080", text: $containerPort).textFieldStyle(.roundedBorder)
                        Text("后端协议：HTTP").font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    }
                } else if !discoveredEndpoints.isEmpty {
                    if let selected = discoveredEndpoints.first(where: { $0.selfPortKey == selectedEndpointKey }) {
                        Text("已选目标：\(selected["containerName"].string) · 容器端口 \(selected["containerPort"].string) · HTTP")
                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    } else {
                        Text("请选择上方的服务端口映射。")
                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    }
                }
                Picker("入口方式", selection: $ingressMode) {
                    Text("iDeploy 受管入口（自动 HTTPS）").tag("caddy")
                    Text("使用现有入口代理（Nginx）").tag("nginx")
                }
                .accessibilityHint("选择应用域名路由的代理；Nginx 模式会备份并校验主配置后重载")
                if ingressMode == "nginx" {
                    Toggle("启用域名 HTTPS", isOn: $useHTTPS)
                    if useHTTPS {
                        TextField("Nginx 容器内证书路径（fullchain.pem）", text: $certificatePath)
                            .textFieldStyle(.roundedBorder)
                        TextField("Nginx 容器内私钥路径（privkey.pem）", text: $certificateKeyPath)
                            .textFieldStyle(.roundedBorder)
                        HStack {
                            Button("查找服务器已有证书") { scanCertificates() }
                                .buttonStyle(DeploySecondaryButtonStyle())
                                .disabled(deploymentID.isEmpty || scanningCertificates)
                            if scanningCertificates { ProgressView().controlSize(.small) }
                        }
                        if !certificateScanError.isEmpty {
                            Text(certificateScanError).font(.system(size: 11)).foregroundStyle(AppColors.warning).textSelection(.enabled)
                        }
                        ForEach(0..<certificateScan["routes"].array.count, id: \.self) { index in
                            let route = certificateScan["routes"][index]
                            if !route["certificate"].string.isEmpty && !route["certificateKey"].string.isEmpty {
                                Button("尝试复用 \(route["fqdn"].string) 的证书") {
                                    certificatePath = route["certificate"].string
                                    certificateKeyPath = route["certificateKey"].string
                                }.buttonStyle(DeploySecondaryButtonStyle())
                            }
                        }
                        DisclosureGroup("使用 Let's Encrypt + Certbot") {
                            VStack(alignment: .leading, spacing: 10) {
                                TextField("证书通知邮箱（已有 Certbot 账户可留空）", text: $certbotEmail)
                                    .textFieldStyle(.roundedBorder)
                                TextField("服务器 ACME Webroot 目录，例如 /var/www/certbot", text: $certbotWebroot)
                                    .textFieldStyle(.roundedBorder)
                                HStack {
                                    Button("预检并查找证书") { checkCertbot() }
                                        .buttonStyle(DeploySecondaryButtonStyle())
                                        .disabled(checkingCertbot || issuingCertbot || deploymentID.isEmpty)
                                    if checkingCertbot || issuingCertbot { ProgressView().controlSize(.small) }
                                }
                                if !certbotCheck.isNull {
                                    if !certbotCheck["existing"].isNull {
                                        Button("使用已找到的有效证书") {
                                            certificatePath = certbotCheck["existing"]["certificate"].string
                                            certificateKeyPath = certbotCheck["existing"]["certificateKey"].string
                                        }.buttonStyle(DeploySecondaryButtonStyle())
                                    } else {
                                        Text("Certbot \(certbotCheck["certbotAvailable"].bool ? "已安装" : "未安装") · 证书挂载 \(certbotCheck["certificateMount"].bool ? "正常" : "缺失") · DNS \(certbotCheck["dnsReady"].bool ? "正常" : "未通过") · HTTP 挑战 \(certbotCheck["challengeReady"].bool ? "正常" : "未通过")")
                                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                                        Button("申请证书并配置续期") { issueCertbot() }
                                            .buttonStyle(DeploySecondaryButtonStyle())
                                            .disabled(issuingCertbot || (certbotEmail.isEmpty && !certbotCheck["existingAccount"].bool) || !certbotCheck["certbotAvailable"].bool || !certbotCheck["certificateMount"].bool || !certbotCheck["dnsReady"].bool || !certbotCheck["challengeReady"].bool)
                                    }
                                    if !certbotCheck["detail"].string.isEmpty {
                                        Text(certbotCheck["detail"].string).font(.system(size: 11)).foregroundStyle(AppColors.warning).textSelection(.enabled)
                                    }
                                }
                                if !certbotMessage.isEmpty {
                                    Text(certbotMessage).font(.system(size: 11)).foregroundStyle(DeployStyle.muted).textSelection(.enabled)
                                }
                                Text("仅在 Nginx 已通过 80 端口提供 ACME 挑战目录，且完整挂载 /etc/letsencrypt 时自动申请；会复用已有证书。")
                                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                            }.padding(.top, 8)
                        }
                        Text("预检会验证现有证书覆盖 \(host["fqdn"].string) 且未过期；没有匹配证书时请先在服务器配置。")
                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    }
                } else {
                    Text("受管入口使用 Caddy 自动申请域名 HTTPS 证书；需要公网 DNS 和 80/443 可达。")
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                }
                Text("保存草稿不会修改服务器入口、DNS 或证书。保存后重新打开此项，依次预检、应用入口配置，再检查域名访问。")
                    .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                if !host["binding"].isNull {
                    HStack {
                        Button("预检已保存的入口") { loadPlan() }
                            .buttonStyle(DeploySecondaryButtonStyle()).disabled(draftDirty)
                        if planning { ProgressView().controlSize(.small) }
                    }
                    if draftDirty {
                        Text("请先保存当前修改，再重新打开草稿预检。")
                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    }
                    if !plan.isNull {
                        VStack(alignment: .leading, spacing: 6) {
                            Text("入口预检").font(.system(size: 12, weight: .semibold))
                            Text(plan["detail"].string)
                                .font(.system(size: 11))
                                .foregroundStyle(plan["canApply"].bool ? DeployStyle.muted : AppColors.warning)
                                .textSelection(.enabled)
                            if plan["hostPort"].int > 0 {
                                Text("\(plan["fqdn"].string) → \(plan["hostIp"].string):\(String(plan["hostPort"].int)) → 容器端口 \(String(plan["containerPort"].int))")
                                    .font(.system(size: 11)).textSelection(.enabled)
                            }
                            if !plan["ingressPreview"].string.isEmpty {
                                Text("将添加的 Nginx 路由")
                                    .font(.system(size: 11, weight: .semibold))
                                ScrollView(.vertical) {
                                    Text(plan["ingressPreview"].string)
                                        .font(.system(size: 10, design: .monospaced))
                                        .textSelection(.enabled)
                                        .frame(maxWidth: .infinity, alignment: .leading)
                                }
                                .frame(height: 150)
                                .padding(8)
                                .background(DeployStyle.overviewPanel)
                            }
                            if plan["canApply"].bool {
                                Button("应用入口配置") { apply() }.buttonStyle(DeployPrimaryButtonStyle())
                            }
                        }.padding(12).deployCard()
                    }
                }
                if !host["binding"].isNull {
                    Button("删除绑定草稿", role: .destructive) {
                        Task {
                            if await model.perform("DeleteDomainBindingDraft", [host.id]) != nil { onSaved(); dismiss() }
                        }
                    }.buttonStyle(DeploySecondaryButtonStyle())
                }
            }.padding(16).deployCard(fill: DeployStyle.overviewPanel)
        }
        .onAppear { discover() }
        .onChange(of: deploymentID) { _ in
            plan = J(nil)
            selectedEndpointKey = ""
            serviceName = ""
            containerPort = ""
            discover()
        }
        .onChange(of: containerPort) { _ in plan = J(nil) }
        .onChange(of: serviceName) { _ in plan = J(nil) }
        .onChange(of: upstreamProtocol) { _ in plan = J(nil) }
        .onChange(of: ingressMode) { _ in plan = J(nil) }
        .onChange(of: useHTTPS) { _ in plan = J(nil) }
        .onChange(of: certificatePath) { _ in plan = J(nil) }
        .onChange(of: certificateKeyPath) { _ in plan = J(nil) }
        .sheet(item: $queuedJob) { job in
            JobDetailSheet(job: job).environmentObject(model).onDisappear { onSaved() }
        }
    }

    private func loadPlan() {
        planning = true
        Task {
            let method = ingressMode == "nginx" ? "PlanExistingNginxBinding" : "PlanDomainBinding"
            plan = await model.perform(method, [host.id], refreshAfter: false) ?? J(nil)
            planning = false
        }
    }

    private func scanCertificates() {
        guard let deployment = model.deployments.first(where: { $0.id == deploymentID }) else { return }
        let serverID = deployment["serverId"].string
        guard !serverID.isEmpty else { return }
        scanningCertificates = true
        certificateScanError = ""
        Task {
            do { certificateScan = try await NativeBridge.call("ScanServerNginxRoutes", [serverID]) }
            catch { certificateScanError = error.localizedDescription }
            scanningCertificates = false
        }
    }

    private func certificateServerID() -> String {
        model.deployments.first(where: { $0.id == deploymentID })?["serverId"].string ?? ""
    }

    private func checkCertbot() {
        let serverID = certificateServerID()
        guard !serverID.isEmpty else { certbotMessage = "请先选择部署目标"; return }
        checkingCertbot = true
        certbotMessage = ""
        Task {
            do {
                certbotCheck = try await NativeBridge.call("CheckServerCertificate", [serverID, host["fqdn"].string, certbotWebroot])
                if certbotCheck["existing"].isNull && certbotWebroot.isEmpty {
                    certbotMessage = "未找到可复用证书。填写服务器 ACME Webroot 后重新预检。"
                }
            } catch { certbotMessage = error.localizedDescription }
            checkingCertbot = false
        }
    }

    private func issueCertbot() {
        let serverID = certificateServerID()
        guard !serverID.isEmpty else { certbotMessage = "请先选择部署目标"; return }
        issuingCertbot = true
        certbotMessage = "正在申请证书；完成前请保持此窗口打开。"
        Task {
            do {
                let job = try await NativeBridge.call("IssueServerCertificate", [serverID, host["fqdn"].string, certbotEmail, certbotWebroot, certbotCheck["nginxContainer"].string])
                for _ in 0..<450 {
                    try await Task.sleep(nanoseconds: 2_000_000_000)
                    let state = try await NativeBridge.call("GetJob", [job.id])
                    if state["status"].string == "succeeded" {
                        certbotCheck = try await NativeBridge.call("CheckServerCertificate", [serverID, host["fqdn"].string, certbotWebroot])
                        certificatePath = certbotCheck["existing"]["certificate"].string
                        certificateKeyPath = certbotCheck["existing"]["certificateKey"].string
                        certbotMessage = "证书已签发并填入路径。保存草稿后预检并应用域名入口。"
                        break
                    }
                    if state["status"].string == "failed" || state["status"].string == "canceled" {
                        certbotMessage = state["error"].string.isEmpty ? "证书任务未完成，请查看任务详情。" : state["error"].string
                        break
                    }
                }
            } catch { certbotMessage = error.localizedDescription }
            issuingCertbot = false
        }
    }

    private func apply() {
        Task {
            let method = ingressMode == "nginx" ? "ApplyExistingNginxBinding" : "ApplyDomainBinding"
            var arguments: [Any] = [host.id, plan["bindingUpdatedAt"].string]
            if ingressMode == "nginx" { arguments.append(plan["ingressConfigHash"].string) }
            if let job = await model.perform(method, arguments, refreshAfter: false) {
                queuedJob = job
                plan = J(nil)
            }
        }
    }

    private func discover() {
        discoverySequence += 1
        let sequence = discoverySequence
        endpointDiscovery = J(nil)
        guard !deploymentID.isEmpty else { discovering = false; return }
        let requestedID = deploymentID
        discovering = true
        Task {
            let result = await model.perform("DiscoverDeploymentEndpoints", [requestedID], refreshAfter: false)
            guard discoverySequence == sequence && deploymentID == requestedID else { return }
            endpointDiscovery = result ?? J(nil)
            let endpoints = endpointDiscovery["endpoints"].array
            let matching = endpoints.filter { endpoint in
                endpoint["containerPort"].string == containerPort &&
                    (serviceName.isEmpty || endpoint["containerName"].string == serviceName ||
                     endpoint["containerName"].string.contains("-\(serviceName)-"))
            }
            if matching.count == 1 && !hasAmbiguousMapping(matching[0], in: endpoints) {
                selectedEndpointKey = matching[0].selfPortKey
            } else if endpoints.count == 1 && containerPort.isEmpty {
                selectedEndpointKey = endpoints[0].selfPortKey
                serviceName = endpoints[0]["containerName"].string
                containerPort = endpoints[0]["containerPort"].string
            }
            discovering = false
        }
    }
}

struct DomainAppliedBindingView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let host: J
    let onSaved: () -> Void
    @State private var queuedJob: J?
    @State private var showingRemove = false
    @State private var access = J(nil)
    @State private var checkingAccess = false

    var body: some View {
        let binding = host["binding"]
        VStack(spacing: 0) {
            HStack {
                VStack(alignment: .leading, spacing: 5) {
                    Text("域名入口").font(.system(size: 21, weight: .bold)).foregroundStyle(DeployStyle.ink)
                    Text(host["fqdn"].string).font(.system(size: 12)).foregroundStyle(DeployStyle.muted)
                }
                Spacer()
            }.padding(20).background(DeployStyle.overviewHeader)
            VStack(alignment: .leading, spacing: 14) {
                SectionHeading(binding["status"].string == "active" ? "已应用的入口" : "入口任务处理中", "DNS 解析和 HTTPS 证书需分别核验")
                if binding["status"].string == "active" {
                    Text("后端：\(binding["hostIp"].string):\(String(binding["hostPort"].int)) → 容器端口 \(String(binding["containerPort"].int))")
                        .font(.system(size: 12)).textSelection(.enabled)
                }
                Text(binding["detail"].string).font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                if binding["status"].string == "active" {
                    HStack {
                        Button("检查访问") { inspectAccess() }.buttonStyle(DeploySecondaryButtonStyle())
                        if checkingAccess { ProgressView().controlSize(.small) }
                    }
                    if !access.isNull {
                        Text(access["status"].string == "reachable" ? "本机访问验证通过" : "访问尚未就绪")
                            .font(.system(size: 12, weight: .semibold))
                            .foregroundStyle(access["status"].string == "reachable" ? DeployStyle.green : .orange)
                        Text("DNS：\(access["dnsStatus"].string) · 入口：\(access["ingressStatus"].string) · 后端：\(access["backendStatus"].string) · HTTPS：\(access["httpsStatus"].string)")
                            .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                        Text(access["detail"].string).font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    }
                }
                if ["applying", "removing"].contains(binding["status"].string) {
                    Text("任务执行中或需要重试；请在任务列表查看结果。")
                        .foregroundStyle(.orange)
                }
            }.padding(16).frame(maxWidth: .infinity, alignment: .leading)
                .deployCard(fill: DeployStyle.overviewPanel).padding(20)
            Spacer()
            HStack {
                Button("解除入口绑定", role: .destructive) { showingRemove = true }
                    .buttonStyle(DeploySecondaryButtonStyle())
                    .disabled(binding["status"].string != "active")
                Spacer()
                Button("关闭") { dismiss() }.buttonStyle(DeployPrimaryButtonStyle())
                    .keyboardShortcut(.cancelAction)
            }.padding(20).background(DeployStyle.overviewPanel)
        }
        .frame(width: 620, height: 510).environment(\.colorScheme, .light)
        .confirmationDialog("解除域名入口绑定？", isPresented: $showingRemove) {
            Button("解除入口绑定", role: .destructive) {
                Task {
                    if let job = await model.perform("RemoveDomainBinding", [host.id], refreshAfter: false) { queuedJob = job }
                }
            }
        } message: { Text("将从当前入口代理移除此域名的 iDeploy 路由，保留域名资产和服务商处的 DNS 记录。") }
        .sheet(item: $queuedJob) { job in
            JobDetailSheet(job: job).environmentObject(model).onDisappear { onSaved(); dismiss() }
        }
        .onAppear {
            if binding["status"].string == "active" { inspectAccess() }
        }
    }

    private func inspectAccess() {
        checkingAccess = true
        Task {
            access = await model.perform("InspectDomainAccess", [host.id], refreshAfter: false) ?? J(nil)
            checkingAccess = false
        }
    }
}

struct ServerNginxRoutesView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let domainName: String
    @State private var serverID = ""
    @State private var scan = J(nil)
    @State private var scanning = false
    @State private var scanError = ""

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                VStack(alignment: .leading, spacing: 5) {
                    Text("服务器域名服务").font(.system(size: 21, weight: .bold))
                    Text("只读扫描现有 Nginx 路由、转发目标和 HTTPS 配置")
                        .font(.system(size: 12)).foregroundStyle(DeployStyle.muted)
                }
                Spacer()
            }.padding(20).background(DeployStyle.overviewHeader)
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Picker("服务器", selection: $serverID) {
                        Text("选择服务器").tag("")
                        ForEach(model.servers, id: \.id) { item in
                            Text(item["name"].string).tag(item.id)
                        }
                    }
                    Button("扫描") { load() }
                        .buttonStyle(DeploySecondaryButtonStyle())
                        .disabled(serverID.isEmpty || scanning)
                    if scanning { ProgressView().controlSize(.small).accessibilityLabel("正在扫描 Nginx") }
                }
                if !scanError.isEmpty {
                    Text(scanError).font(.system(size: 11)).foregroundStyle(AppColors.warning)
                        .textSelection(.enabled)
                }
                if !scan.isNull {
                    Text("入口容器：\(scan["container"].string) · \(scan["routes"].array.count) 个域名")
                        .font(.system(size: 12, weight: .semibold))
                    Text("已发布的服务端口：\(scan["services"].array.count) 个；可在解析记录的服务绑定中选用。")
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    ScrollView {
                        LazyVStack(spacing: 8) {
                            ForEach(0..<scan["routes"].array.count, id: \.self) { index in
                                let route = scan["routes"][index]
                                VStack(alignment: .leading, spacing: 5) {
                                    HStack {
                                        Text(route["fqdn"].string).font(.system(size: 12, weight: .semibold))
                                        Text(route["managed"].bool ? "iDeploy 管理" : "服务器已有")
                                            .font(.system(size: 10)).foregroundStyle(DeployStyle.muted)
                                        Spacer()
                                        Text(route["https"].bool ? "HTTPS 已配置" : "仅 HTTP")
                                            .font(.system(size: 10)).foregroundStyle(route["https"].bool ? DeployStyle.green : AppColors.warning)
                                    }
                                    Text("服务：\(route["service"].string.isEmpty ? "未识别" : route["service"].string) · 转发：\(route["upstream"].string.isEmpty ? "未识别" : route["upstream"].string)")
                                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                                        .textSelection(.enabled)
                                    if !route["certificate"].string.isEmpty {
                                        Text("证书：\(route["certificate"].string)")
                                            .font(.system(size: 10)).foregroundStyle(DeployStyle.muted)
                                            .textSelection(.enabled)
                                    }
                                }.padding(12).frame(maxWidth: .infinity, alignment: .leading)
                                    .deployCard()
                            }
                        }
                    }
                } else if !scanning && scanError.isEmpty {
                    Text("选择服务器后扫描。扫描只读取配置，不会修改 Nginx 或 DNS。")
                        .font(.system(size: 12)).foregroundStyle(DeployStyle.muted)
                    Spacer()
                }
            }.padding(20).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            HStack { Spacer(); Button("关闭") { dismiss() }.buttonStyle(DeployPrimaryButtonStyle()).keyboardShortcut(.cancelAction) }
                .padding(20).background(DeployStyle.overviewPanel)
        }
        .frame(width: 760, height: 560)
        .foregroundStyle(DeployStyle.ink)
        .environment(\.colorScheme, .light)
        .onAppear { if serverID.isEmpty { serverID = model.servers.first?.id ?? "" } }
        .onChange(of: serverID) { _ in scan = J(nil); scanError = "" }
    }

    private func load() {
        scanning = true
        scanError = ""
        let requestedID = serverID
        Task {
            do {
                let result = try await NativeBridge.call("ScanServerNginxRoutes", [requestedID])
                if serverID == requestedID { scan = result }
            } catch {
                if serverID == requestedID { scanError = String(describing: error) }
            }
            scanning = false
        }
    }
}

#Preview("扫描服务器域名服务") {
    ServerNginxRoutesView(domainName: "example.com").environmentObject(AppModel())
}

private extension J {
    var selfPortKey: String { "\(self["containerName"].string):\(self["hostIp"].string):\(self["hostPort"].int):\(self["containerPort"].int)" }
}
