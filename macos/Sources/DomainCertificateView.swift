import AppKit
import SwiftUI

private struct DomainCertificateOptions: Codable {
    var serverID: String
    var email: String
    var webroot: String
}

struct DomainCertificateView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss

    let fqdn: String
    let suggestedServerID: String

    @State private var serverID = ""
    @State private var email = ""
    @State private var webroot = ""
    @State private var check = J(nil)
    @State private var checkedParameters = ""
    @State private var checking = false
    @State private var issuing = false
    @State private var message = ""
    @State private var error = ""
    @State private var jobStatus = ""

    private var optionsKey: String { "ideploy.domain.certificate.\(fqdn.lowercased())" }
    private var parameters: String { "\(serverID)|\(webroot)|\(email)" }
    private var existing: J { check["existing"] }
    private var canIssue: Bool {
        !checking && !issuing && existing.isNull && checkedParameters == parameters &&
        check["certbotAvailable"].bool && check["certificateMount"].bool &&
        check["challengeReady"].bool && check["dnsReady"].bool &&
        (!email.isEmpty || check["existingAccount"].bool) &&
        !check["nginxContainer"].string.isEmpty
    }

    var body: some View {
        VStack(spacing: 0) {
            SectionHeading("配置 HTTPS 证书", fqdn)
                .padding(20)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(DeployStyle.overviewHeader)
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    Text("为此子域名查找已有证书；缺少证书时可使用服务器 Agent 和 Let’s Encrypt + Certbot 申请。")
                        .font(.system(size: 12)).foregroundStyle(DeployStyle.muted)
                    Picker("入口服务器", selection: $serverID) {
                        Text("请选择服务器").tag("")
                        ForEach(model.servers, id: \.id) { server in
                            Text("\(server["name"].string) · \(server["host"].string)").tag(server.id)
                        }
                    }
                    .disabled(checking || issuing)
                    TextField("服务器 ACME Webroot 目录", text: $webroot)
                        .textFieldStyle(.roundedBorder)
                        .disabled(issuing)
                    Text("默认从服务器 Nginx 的 ACME 目录挂载推导；可修改为实际宿主机目录。")
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    TextField("证书通知邮箱（已有 Certbot 账户可留空）", text: $email)
                        .textFieldStyle(.roundedBorder)
                        .disabled(issuing)
                    HStack(spacing: 10) {
                        Button("预检并查找证书") { checkCertificate() }
                            .buttonStyle(DeploySecondaryButtonStyle())
                            .disabled(serverID.isEmpty || checking || issuing)
                        ZStack {
                            if checking { ProgressView().controlSize(.small) }
                        }
                        .frame(width: 16, height: 16)
                        .accessibilityHidden(true)
                        Spacer()
                        Button("保存参数") { saveOptions() }
                            .buttonStyle(DeploySecondaryButtonStyle())
                            .disabled(serverID.isEmpty || issuing)
                    }
                    if !check.isNull {
                        VStack(alignment: .leading, spacing: 8) {
                            Text("服务器检查").font(.system(size: 12, weight: .semibold))
                            Text("Agent Nginx：\(check["nginxContainer"].string.isEmpty ? "未识别" : check["nginxContainer"].string) · Certbot：\(status(check["certbotAvailable"].bool))")
                            Text("证书挂载：\(status(check["certificateMount"].bool)) · DNS：\(status(check["dnsReady"].bool)) · HTTP 验证：\(status(check["challengeReady"].bool))")
                            if !check["detail"].string.isEmpty {
                                Text(check["detail"].string).textSelection(.enabled)
                            }
                        }
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                        .padding(14)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .deployCard()
                    }
                    if !existing.isNull {
                        VStack(alignment: .leading, spacing: 8) {
                            Text("已有有效证书").font(.system(size: 12, weight: .semibold))
                            Text("证书：\(existing["certificate"].string)")
                            Text("私钥：\(existing["certificateKey"].string)")
                            Text("到期时间：\(existing["expiresAt"].string)")
                            Button("复制证书路径") { copy(existing["certificate"].string) }
                                .buttonStyle(DeploySecondaryButtonStyle())
                        }
                        .font(.system(size: 11)).textSelection(.enabled)
                        .padding(14)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .deployCard()
                    } else {
                        Button("申请证书并配置自动续期") { issueCertificate() }
                            .buttonStyle(DeployPrimaryButtonStyle())
                            .disabled(!canIssue)
                    }
                    if issuing {
                        HStack(spacing: 8) {
                            ProgressView().controlSize(.small)
                            Text(jobStatus.isEmpty ? "正在申请证书…" : jobStatus)
                        }
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    }
                    if !message.isEmpty { Text(message).font(.system(size: 11)).foregroundStyle(DeployStyle.green).textSelection(.enabled) }
                    if !error.isEmpty { Text(error).font(.system(size: 11)).foregroundStyle(.orange).textSelection(.enabled) }
                    Text("证书签发后，还需在服务绑定或部署访问路径中应用 HTTPS 入口。保存参数不会修改服务器配置。")
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                }
                .padding(20)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            HStack {
                Spacer()
                Button("完成") { dismiss() }
                    .buttonStyle(DeploySecondaryButtonStyle())
            }
            .padding(16)
            .background(DeployStyle.overviewPanel)
        }
        .frame(width: 650, height: 680)
        .background(DeployStyle.canvas)
        .modifier(DomainCertificateSheetBackground())
        .foregroundStyle(DeployStyle.ink)
        .environment(\.colorScheme, .light)
        .onAppear(perform: loadOptions)
        .onChange(of: serverID) { _ in invalidateCheck() }
        .onChange(of: webroot) { _ in invalidateCheck() }
        .onChange(of: email) { _ in invalidateCheck() }
    }

    private func status(_ ready: Bool) -> String { ready ? "就绪" : "未就绪" }

    private func invalidateCheck() {
        checkedParameters = ""
        check = J(nil)
    }

    private func loadOptions() {
        if let data = UserDefaults.standard.data(forKey: optionsKey),
           let saved = try? JSONDecoder().decode(DomainCertificateOptions.self, from: data) {
            serverID = saved.serverID
            email = saved.email
            webroot = saved.webroot
        }
        if !model.servers.contains(where: { $0.id == serverID }) {
            serverID = model.servers.contains(where: { $0.id == suggestedServerID }) ? suggestedServerID :
                (model.servers.count == 1 ? model.servers[0].id : "")
        }
        if !serverID.isEmpty { checkCertificate() }
    }

    private func saveOptions() {
        let value = DomainCertificateOptions(serverID: serverID, email: email.trimmingCharacters(in: .whitespacesAndNewlines),
                                             webroot: webroot.trimmingCharacters(in: .whitespacesAndNewlines))
        guard let data = try? JSONEncoder().encode(value) else { return }
        UserDefaults.standard.set(data, forKey: optionsKey)
        message = "参数已保存在本机。"
    }

    private func checkCertificate() {
        guard !serverID.isEmpty else { return }
        checking = true
        error = ""
        message = ""
        let requestedServer = serverID
        let requestedWebroot = webroot.trimmingCharacters(in: .whitespacesAndNewlines)
        Task {
            defer { checking = false }
            do {
                var result = try await NativeBridge.call("CheckServerCertificate", [requestedServer, fqdn, requestedWebroot], timeoutSeconds: 70)
                guard serverID == requestedServer, webroot.trimmingCharacters(in: .whitespacesAndNewlines) == requestedWebroot else { return }
                let suggestion = result["suggestedWebroot"].string
                if requestedWebroot.isEmpty && !suggestion.isEmpty {
                    webroot = suggestion
                    result = try await NativeBridge.call("CheckServerCertificate", [requestedServer, fqdn, suggestion], timeoutSeconds: 70)
                    guard serverID == requestedServer, webroot == suggestion else { return }
                }
                check = result
                checkedParameters = parameters
                if !existing.isNull { message = "已找到覆盖此子域名且 Nginx 可读取的有效证书。" }
            } catch { self.error = error.localizedDescription }
        }
    }

    private func issueCertificate() {
        guard canIssue else { return }
        saveOptions()
        issuing = true
        error = ""
        message = ""
        jobStatus = "正在提交证书申请…"
        Task {
            defer { issuing = false }
            do {
                let job = try await NativeBridge.call("IssueServerCertificate", [serverID, fqdn, email, webroot, check["nginxContainer"].string])
                for _ in 0..<450 {
                    try await Task.sleep(nanoseconds: 2_000_000_000)
                    let state = try await NativeBridge.call("GetJob", [job.id])
                    let status = state["status"].string
                    jobStatus = state["message"].string.isEmpty ? "证书任务进行中…" : state["message"].string
                    if status == "succeeded" || status == "completed" {
                        check = try await NativeBridge.call("CheckServerCertificate", [serverID, fqdn, webroot], timeoutSeconds: 70)
                        message = existing.isNull ? "证书任务已完成，请重新预检确认证书路径。" : "证书已签发并配置自动续期。请在入口配置中启用 HTTPS。"
                        return
                    }
                    if status == "failed" || status == "canceled" {
                        error = state["error"].string.isEmpty ? "证书任务失败，请查看任务详情。" : state["error"].string
                        return
                    }
                }
                error = "证书任务仍在执行，请稍后重新打开并预检。"
            } catch { self.error = error.localizedDescription }
        }
    }

    private func copy(_ value: String) {
        NSPasteboard.general.clearContents()
        _ = NSPasteboard.general.setString(value, forType: .string)
    }
}

private struct DomainCertificateSheetBackground: ViewModifier {
    @ViewBuilder func body(content: Content) -> some View {
        if #available(macOS 13.3, *) {
            content.presentationBackground(DeployStyle.canvas)
        } else {
            content.background(DeployStyle.canvas)
        }
    }
}

#Preview("子域名 HTTPS") {
    DomainCertificateView(fqdn: "apps.example.com", suggestedServerID: "")
        .environmentObject(AppModel())
}
