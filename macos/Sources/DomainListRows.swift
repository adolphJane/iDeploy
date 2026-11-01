import SwiftUI

// Presentation-only rows keep record editing and binding actions in their pages.
struct DomainListHeader<Actions: View>: View {
    let title: String
    let subtitle: String
    let actions: Actions

    init(_ title: String, subtitle: String, @ViewBuilder actions: () -> Actions) {
        self.title = title
        self.subtitle = subtitle
        self.actions = actions()
    }

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .center, spacing: AppSpacing.md) {
                SectionHeading(title, subtitle).frame(minWidth: 180, maxWidth: .infinity, alignment: .leading)
                actions.fixedSize()
            }
            VStack(alignment: .leading, spacing: AppSpacing.sm) {
                SectionHeading(title, subtitle)
                actions.fixedSize()
            }.frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(AppSpacing.md)
        .background(DeployStyle.overviewPanel)
    }
}

struct DomainDNSRecordRow<Actions: View>: View {
    let hostRecord: String
    let name: String
    let type: String
    let value: String
    let ttl: Int
    let status: String
    let statusTone: AppStatusBadge.Tone
    let serviceSummary: String
    let serviceStatus: String
    let serviceTone: AppStatusBadge.Tone
    let actions: Actions

    init(hostRecord: String, name: String, type: String, value: String, ttl: Int, status: String,
         statusTone: AppStatusBadge.Tone, serviceSummary: String = "", serviceStatus: String = "",
         serviceTone: AppStatusBadge.Tone = .neutral, @ViewBuilder actions: () -> Actions) {
        self.hostRecord = hostRecord
        self.name = name
        self.type = type
        self.value = value
        self.ttl = ttl
        self.status = status
        self.statusTone = statusTone
        self.serviceSummary = serviceSummary
        self.serviceStatus = serviceStatus
        self.serviceTone = serviceTone
        self.actions = actions()
    }

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: AppSpacing.sm) {
                recordTypeColumn.frame(width: 50, alignment: .leading)
                hostRecordColumn.frame(width: 82, alignment: .leading)
                identity.frame(minWidth: 150, maxWidth: .infinity, alignment: .leading)
                destination.frame(minWidth: 110, maxWidth: .infinity, alignment: .leading)
                service.frame(minWidth: 140, maxWidth: 180, alignment: .leading)
                badge
                actions.fixedSize()
            }.frame(minWidth: 800)
            VStack(alignment: .leading, spacing: AppSpacing.sm) {
                HStack(alignment: .top, spacing: AppSpacing.sm) {
                    recordTypeColumn.frame(width: 56, alignment: .leading)
                    hostRecordColumn.frame(width: 88, alignment: .leading)
                    identity.frame(maxWidth: .infinity, alignment: .leading)
                }
                HStack(alignment: .top, spacing: AppSpacing.sm) {
                    destination.frame(maxWidth: .infinity, alignment: .leading)
                    service.frame(maxWidth: .infinity, alignment: .leading)
                }
                HStack(spacing: AppSpacing.sm) {
                    Spacer()
                    badge
                    actions.fixedSize()
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.horizontal, AppSpacing.md)
        .padding(.vertical, AppSpacing.sm)
        .foregroundStyle(DeployStyle.ink)
        .accessibilityElement(children: .contain)
    }

    private var recordTypeColumn: some View {
        AppEnvironmentBadge(name: type, accessibilityPrefix: "记录类型").fixedSize()
    }

    private var hostRecordColumn: some View {
        HStack(spacing: 0) {
            Text(hostRecord.isEmpty ? "—" : hostRecord)
                .font(.system(size: 12, weight: .semibold, design: .monospaced))
                .lineLimit(2).textSelection(.enabled).help(hostRecord)
                .fixedSize(horizontal: false, vertical: true)
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel("主机记录：\(hostRecord.isEmpty ? "未提供" : hostRecord)")
    }

    private var identity: some View {
        Text(name)
            .font(.system(size: 12, weight: .semibold))
            .lineLimit(2).textSelection(.enabled).help(name)
            .fixedSize(horizontal: false, vertical: true)
    }

    private var service: some View {
        VStack(alignment: .leading, spacing: AppSpacing.xxs) {
            if serviceSummary.isEmpty {
                Text("未绑定").font(.system(size: 10)).foregroundStyle(DeployStyle.muted)
            } else {
                AppStatusBadge(title: serviceStatus, tone: serviceTone).fixedSize()
                Text(serviceSummary).font(.system(size: 10)).foregroundStyle(DeployStyle.muted)
                    .lineLimit(2).textSelection(.enabled).help(serviceSummary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(serviceSummary.isEmpty ? "服务未绑定" : "服务\(serviceStatus)：\(serviceSummary)")
    }

    private var destination: some View {
        VStack(alignment: .leading, spacing: AppSpacing.xxs) {
            Text(value).font(.system(size: 11, design: .monospaced))
                .lineLimit(2).textSelection(.enabled).help(value)
                .fixedSize(horizontal: false, vertical: true)
            Text(ttl == 1 ? "TTL 自动" : ttl == 0 ? "TTL 服务商默认" : "TTL \(String(ttl)) 秒")
                .font(.system(size: 10)).foregroundStyle(DeployStyle.muted)
        }
    }

    private var badge: some View {
        AppStatusBadge(title: status, tone: statusTone).fixedSize()
    }
}

func domainBindingState(_ binding: J) -> (title: String, tone: AppStatusBadge.Tone) {
    if binding.isNull { return ("未绑定", .neutral) }
    switch binding["status"].string {
    case "active": return ("入口已应用", .success)
    case "applying": return ("应用中", .warning)
    case "removing": return ("解绑中", .warning)
    case "failed": return ("应用失败", .danger)
    default: return ("草稿", .warning)
    }
}

func domainBindingSummary(_ binding: J, deployments: [J]) -> String {
    guard !binding.isNull else { return "" }
    let deployment = deployments.first { $0.id == binding["deploymentId"].string }
    let target = deployment.map { "\($0["projectName"].string) · \($0["serverName"].string)" } ?? "部署已移除"
    let access = !binding["certificatePath"].string.isEmpty ? "域名 HTTPS（现有证书）"
        : binding["ingressMode"].string == "caddy" ? "域名 HTTPS（自动）" : "域名 HTTP"
    return "\(target) · HTTP 后端 :\(String(binding["containerPort"].int)) · \(access)"
}

struct DomainServiceAccessRow<Actions: View>: View {
    let name: String
    let source: String
    let target: String
    let backend: String
    let status: String
    let statusTone: AppStatusBadge.Tone
    let bound: Bool
    let actions: Actions

    init(name: String, source: String, target: String, backend: String, status: String,
         statusTone: AppStatusBadge.Tone, bound: Bool, @ViewBuilder actions: () -> Actions) {
        self.name = name
        self.source = source
        self.target = target
        self.backend = backend
        self.status = status
        self.statusTone = statusTone
        self.bound = bound
        self.actions = actions()
    }

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: AppSpacing.lg) {
                identity.frame(minWidth: 210, maxWidth: .infinity, alignment: .leading)
                destination.frame(minWidth: 160, maxWidth: .infinity, alignment: .leading)
                badge
                actions.fixedSize()
            }.frame(minWidth: 660)
            VStack(alignment: .leading, spacing: AppSpacing.sm) {
                HStack(alignment: .top, spacing: AppSpacing.sm) {
                    identity.frame(maxWidth: .infinity, alignment: .leading)
                    badge
                }
                HStack(alignment: .bottom, spacing: AppSpacing.sm) {
                    destination.frame(maxWidth: .infinity, alignment: .leading)
                    actions.fixedSize()
                }
            }.frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.horizontal, AppSpacing.md)
        .padding(.vertical, AppSpacing.sm)
        .foregroundStyle(DeployStyle.ink)
        .accessibilityElement(children: .contain)
    }

    private var identity: some View {
        HStack(alignment: .top, spacing: AppSpacing.sm) {
            AppObjectIcon(systemName: bound ? "link" : "globe")
            VStack(alignment: .leading, spacing: AppSpacing.xxs) {
                Text(name).font(.system(size: 12, weight: .semibold))
                    .lineLimit(2).textSelection(.enabled).help(name)
                    .fixedSize(horizontal: false, vertical: true)
                Text(source).font(.system(size: 10)).foregroundStyle(DeployStyle.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    private var destination: some View {
        VStack(alignment: .leading, spacing: AppSpacing.xxs) {
            Text(target).font(.system(size: 12, weight: bound ? .medium : .regular))
                .foregroundStyle(bound ? DeployStyle.ink : DeployStyle.muted)
                .lineLimit(2).help(target).fixedSize(horizontal: false, vertical: true)
            if !backend.isEmpty {
                Text(backend).font(.system(size: 10)).foregroundStyle(DeployStyle.muted)
            }
        }
    }

    private var badge: some View {
        AppStatusBadge(title: status, tone: statusTone).fixedSize()
    }
}

#Preview("域名记录 · 宽列表") {
    VStack(spacing: 0) {
        DomainListHeader("DNS 解析记录", subtitle: "阿里云 · 5 条记录") {
            Button("管理解析") {}.buttonStyle(DeploySecondaryButtonStyle())
        }
        Divider()
        DomainDNSRecordRow(hostRecord: "api", name: "api.example.com", type: "A", value: "192.0.2.10", ttl: 600,
                           status: "已启用", statusTone: .success, serviceSummary: "API 服务 · 开发服务器 · HTTP :8080",
                           serviceStatus: "草稿", serviceTone: .warning) {
            HStack(spacing: 12) {
                Button("编辑") {}.buttonStyle(.borderless)
                Button("服务绑定") {}.buttonStyle(DeploySecondaryButtonStyle())
            }
        }
        Divider()
        DomainServiceAccessRow(name: "api.example.com", source: "子域名 · DNS 记录", target: "API 服务 · 生产服务器",
                               backend: "HTTP · 容器端口 8080", status: "草稿", statusTone: .warning, bound: true) {
            Button("管理绑定") {}.buttonStyle(DeploySecondaryButtonStyle())
        }
    }.frame(width: 900).deployCard().padding(20).background(DeployStyle.canvas)
        .environment(\.colorScheme, .light)
}

#Preview("域名记录 · 窄列表与长内容") {
    VStack(spacing: 0) {
        DomainListHeader("域名访问", subtitle: "5 个域名 · 1 个已配置") {
            Picker("显示", selection: .constant("全部")) {
                Text("全部").tag("全部")
                Text("有绑定").tag("有绑定")
                Text("无绑定").tag("无绑定")
            }.pickerStyle(.segmented).frame(width: 220)
        }
        Divider()
        DomainDNSRecordRow(hostRecord: "long-service-name.dev", name: "long-service-name.dev.example.com", type: "AAAA", value: "2001:db8:1234:5678::10", ttl: 600,
                           status: "已启用", statusTone: .success, serviceSummary: "API 服务 · 生产服务器 · HTTP :8080",
                           serviceStatus: "入口已应用", serviceTone: .success) {
            HStack(spacing: 12) {
                Button("编辑") {}.buttonStyle(.borderless)
                Button("服务绑定") {}.buttonStyle(DeploySecondaryButtonStyle())
            }
        }
        Divider()
        DomainServiceAccessRow(name: "long-service-name.dev.example.com", source: "子域名 · DNS 记录",
                               target: "尚未绑定部署", backend: "", status: "未绑定", statusTone: .neutral, bound: false) {
            Button("绑定服务") {}.buttonStyle(DeploySecondaryButtonStyle())
        }
    }.frame(width: 360).deployCard().padding(20).background(DeployStyle.canvas)
        .environment(\.colorScheme, .light)
}
