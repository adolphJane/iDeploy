import SwiftUI

private enum JobPresentation {
    static func name(_ type: String) -> String {
        switch type {
        case "system.diagnostic": "系统诊断"
        case "server.probe": "服务器检测"
        case "registry.test": "镜像仓库检测"
        case "server.container.operation": "容器操作"
        case "deployment.operation": "工作负载操作"
        case "deployment.release": "项目发布"
        case "deployment.rollback": "版本回滚"
        case "deployment.resume": "继续发布"
        default: type.isEmpty ? "后台任务" : type
        }
    }

    static func status(_ value: String) -> String {
        switch value {
        case "queued": "等待中"
        case "running": "执行中"
        case "canceling": "取消中"
        case "canceled": "已取消"
        case "succeeded": "已完成"
        case "failed": "失败"
        default: value.isEmpty ? "未知状态" : value
        }
    }

    static func color(_ value: String) -> Color {
        switch value {
        case "succeeded": DeployStyle.green
        case "running", "queued", "canceling": Color(red: 0.13, green: 0.48, blue: 0.65)
        case "failed": .red
        default: DeployStyle.muted
        }
    }

    static func time(_ value: String) -> String {
        let parser = ISO8601DateFormatter()
        guard let date = parser.date(from: value) else { return value.isEmpty ? "—" : value }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "zh_CN")
        formatter.dateFormat = "MM-dd HH:mm:ss"
        return formatter.string(from: date)
    }

    static func summary(_ job: J) -> String {
        let error = job["error"].string
        if !error.isEmpty { return error }
        let message = job["message"].string
        if message == "Completed" { return "执行完成" }
        if message == "Failed" { return "执行失败" }
        if !message.isEmpty { return message }
        return status(job["status"].string)
    }

    static func isActive(_ value: String) -> Bool {
        ["queued", "running", "canceling"].contains(value)
    }

    @MainActor static func resourceName(_ lockKey: String, model: AppModel) -> String? {
        let parts = lockKey.split(separator: ":", maxSplits: 1).map(String.init)
        guard parts.count == 2 else { return nil }
        switch parts[0] {
        case "server":
            return model.servers.first { $0.id == parts[1] }.map { "服务器：\($0["name"].string)" }
        case "deployment":
            return model.deployments.first { $0.id == parts[1] }.map { "部署：\($0["projectName"].string)" }
        case "registry":
            return model.registries.first { $0.id == parts[1] }.map { "仓库：\($0["name"].string)" }
        case "project":
            return model.projects.first { $0.id == parts[1] }.map { "项目：\($0["name"].string)" }
        default: return nil
        }
    }
}

private struct JobProgressBar: View {
    let value: Int

    var body: some View {
        GeometryReader { geometry in
            ZStack(alignment: .leading) {
                Capsule().fill(DeployStyle.line)
                Capsule().fill(DeployStyle.greenBright)
                    .frame(width: geometry.size.width * CGFloat(max(0, min(100, value))) / 100)
            }
        }
        .frame(height: 6)
        .animation(.easeInOut(duration: 0.25), value: value)
    }
}

private struct JobStateBadge: View {
    let status: String

    var body: some View {
        Text(JobPresentation.status(status))
            .font(.system(size: 10, weight: .semibold))
            .foregroundStyle(JobPresentation.color(status))
            .padding(.horizontal, 8)
            .padding(.vertical, 5)
            .background(JobPresentation.color(status).opacity(0.11), in: Capsule())
    }
}

struct OverviewJobRow: View {
    @EnvironmentObject private var model: AppModel
    let job: J

    private var summary: String {
        let detail = JobPresentation.summary(job)
        guard let resource = JobPresentation.resourceName(job["lockKey"].string, model: model) else {
            return detail
        }
        return "\(resource) · \(detail)"
    }

    var body: some View {
        HStack(alignment: .top, spacing: 11) {
            Circle()
                .fill(JobPresentation.color(job["status"].string))
                .frame(width: 8, height: 8)
                .padding(.top, 7)
            VStack(alignment: .leading, spacing: 6) {
                HStack(spacing: 7) {
                    Text(JobPresentation.name(job["type"].string))
                        .font(.system(size: 12, weight: .semibold))
                        .foregroundStyle(DeployStyle.ink)
                    Spacer(minLength: 6)
                    Text(JobPresentation.time(job["updatedAt"].string))
                        .font(.system(size: 10, design: .monospaced))
                        .foregroundStyle(DeployStyle.muted)
                }
                Text(String(summary.prefix(180)))
                    .font(.system(size: 11))
                    .foregroundStyle(job["status"].string == "failed" ? Color.red : DeployStyle.muted)
                    .lineLimit(2)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if JobPresentation.isActive(job["status"].string) {
                    JobProgressBar(value: job["progress"].int)
                }
            }
            JobStateBadge(status: job["status"].string)
            Image(systemName: "chevron.right")
                .font(.system(size: 10, weight: .semibold))
                .foregroundStyle(DeployStyle.muted)
                .padding(.top, 6)
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 12)
        .contentShape(Rectangle())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(JobPresentation.name(job["type"].string))，\(JobPresentation.status(job["status"].string))，\(JobPresentation.time(job["updatedAt"].string))，\(String(summary.prefix(120)))")
    }
}

struct JobDetailSheet: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var current: J

    init(job: J) {
        _current = State(initialValue: job)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 17) {
            HStack(alignment: .top) {
                VStack(alignment: .leading, spacing: 5) {
                    Text(JobPresentation.name(current["type"].string))
                        .font(.system(size: 23, weight: .bold))
                        .foregroundStyle(DeployStyle.ink)
                    Text("任务明细")
                        .font(.caption)
                        .foregroundStyle(DeployStyle.muted)
                }
                Spacer()
                JobStateBadge(status: current["status"].string)
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    VStack(alignment: .leading, spacing: 12) {
                        Text("执行信息")
                            .font(.system(size: 14, weight: .semibold))
                            .foregroundStyle(DeployStyle.ink)
                        Rectangle().fill(DeployStyle.line).frame(height: 1)
                        detailRow("任务类型", current["type"].string)
                        detailRow("任务 ID", current.id)
                        if let resource = JobPresentation.resourceName(current["lockKey"].string, model: model) {
                            detailRow("关联资源", resource)
                        }
                        detailRow("资源键", current["lockKey"].string)
                        detailRow("尝试次数", "\(current["attempt"].int)")
                        detailRow("创建时间", JobPresentation.time(current["createdAt"].string))
                        detailRow("更新时间", JobPresentation.time(current["updatedAt"].string))
                    }
                    .padding(18)
                    .deployCard()

                    VStack(alignment: .leading, spacing: 12) {
                        Text("执行结果")
                            .font(.system(size: 14, weight: .semibold))
                            .foregroundStyle(DeployStyle.ink)
                        Rectangle().fill(DeployStyle.line).frame(height: 1)
                        HStack {
                            Text("进度").foregroundStyle(DeployStyle.muted)
                            Spacer()
                            Text("\(current["progress"].int)%")
                                .fontWeight(.semibold)
                        }
                        JobProgressBar(value: current["progress"].int)
                        Text(JobPresentation.summary(current))
                            .foregroundStyle(current["status"].string == "failed" ? Color.red : DeployStyle.ink)
                            .textSelection(.enabled)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(18)
                    .deployCard()
                }
            }
            HStack {
                Button("刷新任务") { Task { await reload() } }
                    .buttonStyle(DeploySecondaryButtonStyle())
                if ["queued", "running"].contains(current["status"].string) {
                    Button("取消任务", role: .destructive) {
                        Task {
                            await model.perform("CancelJob", [current.id])
                            await reload()
                        }
                    }
                }
                Spacer()
                Button("关闭") { dismiss() }
                    .buttonStyle(DeployPrimaryButtonStyle())
            }
        }
        .padding(24)
        .frame(width: 560, height: 560)
        .background(DeployStyle.canvas)
        .task { await reload() }
    }

    private func detailRow(_ label: String, _ value: String) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Text(label)
                .font(.system(size: 11))
                .foregroundStyle(DeployStyle.muted)
                .frame(width: 70, alignment: .leading)
            Text(value.isEmpty ? "—" : value)
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(DeployStyle.ink)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private func reload() async {
        do {
            current = try await NativeBridge.call("GetJob", [current.id])
        } catch {
            model.error = error.localizedDescription
        }
    }
}
