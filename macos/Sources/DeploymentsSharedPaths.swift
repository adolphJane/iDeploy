import SwiftUI

private struct SharedPathSheetBackground: ViewModifier {
    @ViewBuilder func body(content: Content) -> some View {
        if #available(macOS 13.3, *) {
            content.presentationBackground(DeployStyle.canvas)
        } else {
            content.background(DeployStyle.canvas)
        }
    }
}

struct SharedPathEditor: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let route: J?
    let fixedDeploymentID: String
    let onChanged: () -> Void
    @State private var path = ""
    @State private var fqdn = ""
    @State private var deploymentID = ""
    @State private var serviceName = ""
    @State private var containerPort = ""
    @State private var certificatePath = ""
    @State private var certificateKeyPath = ""
    @State private var endpoints: [J] = []
    @State private var plan = J(nil)
    @State private var access = J(nil)
    @State private var saved = J(nil)
    @State private var busy = false
    @State private var scanningEndpoints = false
    @State private var scanningCertificate = false
    @State private var message = ""

    private var isActive: Bool { saved["status"].string == "active" }
    private var isProcessing: Bool { ["applying", "removing"].contains(saved["status"].string) }
    private var isDraft: Bool { saved["status"].string == "draft" }
    private var locked: Bool { isActive || isProcessing }
    private var canSave: Bool { !fqdn.isEmpty && !path.isEmpty && !deploymentID.isEmpty && Int(containerPort) != nil && !busy && !isProcessing }

    init(route: J?, fixedDeploymentID: String, onChanged: @escaping () -> Void) {
        self.route = route
        self.fixedDeploymentID = fixedDeploymentID
        self.onChanged = onChanged
    }

    var body: some View {
        VStack(spacing: 0) {
            SectionHeading("配置部署访问路径", fqdn.isEmpty ? "为域名添加项目路径" : "https://\(fqdn)/项目名/")
                .padding(20).frame(maxWidth: .infinity, alignment: .leading).background(DeployStyle.overviewHeader)
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    TextField("完整域名，例如 apps.example.com", text: $fqdn).textFieldStyle(.roundedBorder).disabled(locked)
                    TextField("路径名，例如 demo", text: $path).textFieldStyle(.roundedBorder).disabled(locked)
                    if let deployment = model.deployments.first(where: { $0.id == fixedDeploymentID }) {
                        Text("部署目标：\(deployment.deploymentDisplayName) · \(deployment.deploymentLocation)")
                            .font(.system(size: 12)).foregroundStyle(DeployStyle.muted)
                    }
                    HStack {
                        Button(action: scan) {
                            HStack(spacing: 8) {
                                Text("扫描已发布端口")
                                ZStack {
                                    if scanningEndpoints { ProgressView().controlSize(.small) }
                                }
                                .frame(width: 16, height: 16)
                                .accessibilityHidden(true)
                            }
                        }
                        .buttonStyle(DeploySecondaryButtonStyle())
                        .disabled(deploymentID.isEmpty || locked || busy)
                        .accessibilityLabel(scanningEndpoints ? "正在扫描已发布端口" : "扫描已发布端口")
                        if !endpoints.isEmpty { Text("选择实际端口映射").font(.system(size: 11)).foregroundStyle(DeployStyle.muted) }
                    }
                    ForEach(endpoints.indices, id: \.self) { index in
                        let endpoint = endpoints[index]
                        Button("\(endpoint["containerName"].string) · 服务器 \(endpoint["hostPort"].int) → 容器 \(endpoint["containerPort"].int)") {
                            serviceName = endpoint["containerName"].string
                            containerPort = endpoint["containerPort"].string
                            plan = J(nil)
                        }.buttonStyle(DeploySecondaryButtonStyle()).disabled(locked)
                    }
                    TextField("容器名称或 Compose 服务名", text: $serviceName).textFieldStyle(.roundedBorder).disabled(locked)
                    TextField("容器内部端口，例如 8080", text: $containerPort).textFieldStyle(.roundedBorder).disabled(locked)
                    Text("HTTPS 使用现有 Nginx 证书；共享域名下所有路径需使用相同证书。")
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                    Button(action: scanCertificate) {
                        HStack(spacing: 8) {
                            Text("查找服务器已有证书")
                            ZStack {
                                if scanningCertificate { ProgressView().controlSize(.small) }
                            }
                            .frame(width: 16, height: 16)
                            .accessibilityHidden(true)
                        }
                    }
                        .buttonStyle(DeploySecondaryButtonStyle())
                        .disabled(deploymentID.isEmpty || locked || busy)
                        .accessibilityLabel(scanningCertificate ? "正在查找服务器已有证书" : "查找服务器已有证书")
                    TextField("Nginx 容器内证书路径（可选）", text: $certificatePath).textFieldStyle(.roundedBorder).disabled(locked)
                    TextField("Nginx 容器内私钥路径（可选）", text: $certificateKeyPath).textFieldStyle(.roundedBorder).disabled(locked)
                    if isDraft {
                        HStack {
                            Button("预检入口") { preflight() }.buttonStyle(DeploySecondaryButtonStyle()).disabled(busy)
                            Button("删除草稿") { removeDraft() }.buttonStyle(DeploySecondaryButtonStyle()).disabled(busy)
                        }
                    }
                    if !plan.isNull {
                        Text(plan["detail"].string).font(.system(size: 11)).textSelection(.enabled)
                        if !plan["ingressPreview"].string.isEmpty {
                            ScrollView(.horizontal) { Text(plan["ingressPreview"].string).font(.system(size: 10, design: .monospaced)).textSelection(.enabled) }
                                .frame(maxHeight: 180).padding(8).background(DeployStyle.canvas)
                        }
                        if plan["canApply"].bool {
                            Button("应用路径路由") { apply() }.buttonStyle(DeployPrimaryButtonStyle()).disabled(busy)
                        }
                    }
                    if isActive {
                        HStack {
                            Button("检查访问") { inspectAccess() }.buttonStyle(DeploySecondaryButtonStyle()).disabled(busy)
                            Button("解除路径路由") { removeActive() }.buttonStyle(DeploySecondaryButtonStyle()).disabled(busy)
                        }
                    }
                    if !access.isNull {
                        VStack(alignment: .leading, spacing: 6) {
                            Text(access["url"].string).font(.system(size: 11, design: .monospaced)).textSelection(.enabled)
                            Text("DNS：\(access["dnsStatus"].string) · 入口：\(access["ingressStatus"].string) · 后端：\(access["backendStatus"].string) · HTTPS：\(access["httpsStatus"].string) · HTTP \(access["publicStatus"].int)")
                            Text(access["detail"].string).textSelection(.enabled)
                        }
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted)
                        .padding(12).frame(maxWidth: .infinity, alignment: .leading)
                        .background(DeployStyle.canvas, in: RoundedRectangle(cornerRadius: 9))
                    }
                    if isProcessing { Text("入口任务正在执行，请等待任务完成后刷新列表。")
                        .font(.system(size: 11)).foregroundStyle(DeployStyle.muted) }
                    if !saved["detail"].string.isEmpty { Text(saved["detail"].string)
                        .font(.system(size: 11)).foregroundStyle(.orange).textSelection(.enabled) }
                    if !message.isEmpty { Text(message).font(.system(size: 11)).foregroundStyle(.orange).textSelection(.enabled) }
                }.padding(20).frame(maxWidth: .infinity, alignment: .leading)
            }
            HStack {
                Spacer()
                Button("关闭") { dismiss() }.buttonStyle(DeploySecondaryButtonStyle())
                if !isActive && !isProcessing { Button("保存草稿") { save() }.buttonStyle(DeployPrimaryButtonStyle()).disabled(!canSave) }
            }.padding(16).background(DeployStyle.overviewPanel)
        }
        .frame(width: 650, height: 620)
        .background(DeployStyle.canvas)
        .modifier(SharedPathSheetBackground())
        .foregroundStyle(DeployStyle.ink)
        .environment(\.colorScheme, .light)
        .onAppear {
            saved = route ?? J(nil)
            fqdn = route?["fqdn"].string ?? ""
            path = route?["pathPrefix"].string ?? ""
            deploymentID = fixedDeploymentID.isEmpty ? (route?["deploymentId"].string ?? "") : fixedDeploymentID
            serviceName = route?["serviceName"].string ?? ""
            containerPort = route?["containerPort"].string ?? ""
            certificatePath = route?["certificatePath"].string ?? ""
            certificateKeyPath = route?["certificateKeyPath"].string ?? ""
        }
        .onChange(of: path) { _ in plan = J(nil) }
        .onChange(of: fqdn) { _ in plan = J(nil) }
        .onChange(of: deploymentID) { _ in plan = J(nil); endpoints = [] }
        .onChange(of: serviceName) { _ in plan = J(nil) }
        .onChange(of: containerPort) { _ in plan = J(nil) }
        .onChange(of: certificatePath) { _ in plan = J(nil) }
        .onChange(of: certificateKeyPath) { _ in plan = J(nil) }
    }

    private func scan() {
        busy = true; scanningEndpoints = true; message = ""
        Task {
            defer { scanningEndpoints = false; busy = false }
            do { endpoints = try await NativeBridge.call("DiscoverDeploymentEndpoints", [deploymentID]).selfEndpoints }
            catch { message = error.localizedDescription }
        }
    }
    private func scanCertificate() {
        guard let deployment = model.deployments.first(where: { $0.id == deploymentID }) else { return }
        busy = true; scanningCertificate = true; message = ""
        Task { defer { scanningCertificate = false; busy = false }; do {
            let result = try await NativeBridge.call("ScanServerNginxRoutes", [deployment["serverId"].string])
            if let match = result["routes"].array.first(where: { $0["fqdn"].string == fqdn && !$0["certificate"].string.isEmpty }) {
                certificatePath = match["certificate"].string
                certificateKeyPath = match["certificateKey"].string
                message = "已填入共享域名的现有证书，预检时会检查覆盖范围和有效期。"
            } else { message = "未找到 \(fqdn) 的现有证书；可填写 Nginx 容器内证书路径。" }
        } catch { message = error.localizedDescription } }
    }
    private func save() {
        busy = true; message = ""
        let input: [String: Any] = ["id": saved.id, "fqdn": fqdn, "pathPrefix": path, "deploymentId": deploymentID,
                                    "serviceName": serviceName, "containerPort": Int(containerPort) ?? 0,
                                    "certificatePath": certificatePath, "certificateKeyPath": certificateKeyPath]
        Task { defer { busy = false }; do { saved = try await NativeBridge.call("SaveSharedPathRouteDraft", [input]); path = saved["pathPrefix"].string; plan = J(nil); onChanged(); message = "草稿已保存，请预检后应用。" } catch { message = error.localizedDescription } }
    }
    private func preflight() {
        busy = true; message = ""
        Task { defer { busy = false }; do { plan = try await NativeBridge.call("PlanSharedPathRoute", [saved.id], timeoutSeconds: 75) } catch { message = error.localizedDescription } }
    }
    private func apply() {
        busy = true; message = ""
        Task { defer { busy = false }; do {
            let job = try await NativeBridge.call("ApplySharedPathRoute", [saved.id, plan["updatedAt"].string, plan["ingressConfigHash"].string])
            message = "入口任务已提交。"
            await follow(job.id, removing: false)
        } catch { message = error.localizedDescription } }
    }
    private func removeActive() {
        busy = true; message = ""
        Task { defer { busy = false }; do { let job = try await NativeBridge.call("RemoveSharedPathRoute", [saved.id]); await follow(job.id, removing: true) } catch { message = error.localizedDescription } }
    }
    private func removeDraft() {
        busy = true; message = ""
        Task { defer { busy = false }; do { _ = try await NativeBridge.call("DeleteSharedPathRouteDraft", [saved.id]); onChanged(); dismiss() } catch { message = error.localizedDescription } }
    }
    private func inspectAccess() {
        busy = true; message = ""
        Task { defer { busy = false }; do { access = try await NativeBridge.call("InspectSharedPathAccess", [saved.id], timeoutSeconds: 40) } catch { message = error.localizedDescription } }
    }
    private func follow(_ id: String, removing: Bool) async {
        for _ in 0..<90 {
            try? await Task.sleep(nanoseconds: 1_000_000_000)
            do {
                let job = try await NativeBridge.call("GetJob", [id])
                if job["status"].string == "succeeded" || job["status"].string == "completed" {
                    onChanged()
                    if removing { dismiss(); return }
                    saved = try await NativeBridge.call("ListSharedPathRoutes").array.first(where: { $0.id == saved.id }) ?? saved
                    access = try await NativeBridge.call("InspectSharedPathAccess", [saved.id], timeoutSeconds: 40)
                    message = "路径入口已应用。"
                    return
                }
                if job["status"].string == "failed" || job["status"].string == "canceled" {
                    message = job["error"].string.isEmpty ? "入口任务失败，请查看任务详情。" : job["error"].string
                    onChanged(); return
                }
                message = job["message"].string.isEmpty ? "正在应用入口配置…" : job["message"].string
            } catch { message = error.localizedDescription; return }
        }
        message = "任务仍在执行，请稍后刷新路径列表。"
    }
}

private extension J {
    var selfEndpoints: [J] { self["endpoints"].array }
}

#Preview("部署访问路径") {
    SharedPathEditor(route: nil, fixedDeploymentID: "preview-deployment", onChanged: {})
        .environmentObject(AppModel())
}
