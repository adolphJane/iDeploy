import AppKit
import Combine
import Foundation

enum AppSection: String, CaseIterable, Identifiable {
    case overview, deployments, servers, domains, projects, registries, databases, settings
    var id: String { rawValue }
    var title: String {
        switch self {
        case .overview: "总览"
        case .deployments: "部署"
        case .servers: "服务器"
        case .domains: "域名"
        case .projects: "项目"
        case .registries: "镜像仓库"
        case .databases: "数据库"
        case .settings: "设置"
        }
    }
    var symbol: String {
        switch self {
        case .overview: "square.grid.2x2"
        case .deployments: "shippingbox"
        case .servers: "server.rack"
        case .domains: "globe"
        case .projects: "folder"
        case .registries: "archivebox"
        case .databases: "externaldrive"
        case .settings: "gearshape"
        }
    }
}

@MainActor
final class AppModel: ObservableObject {
    @Published var section: AppSection = .overview
    @Published var overview = J(nil)
    @Published var deployments: [J] = []
    @Published var servers: [J] = []
    @Published var domains: [J] = []
    @Published var dnsConnections: [J] = []
    @Published var projects: [J] = []
    @Published var registries: [J] = []
    @Published var databases: [J] = []
    @Published var error: String?
    @Published var busy = false
    @Published var ready = false
    @Published private(set) var manualRefreshInProgress = false
    @Published private(set) var manualRefreshRevision = 0
    @Published var domainDataLoading = false
    @Published var sharedMode = "local"
    @Published var sharedUnlocked = false
    @Published var sharedSyncError = ""
    @Published var sharedCursor = 0
    @Published var sharedUpToDate = false
    @Published var sharedLastSyncAt = ""
    @Published var sharedSyncDetail = ""
    @Published var openSharedSettings = false

    func refreshFromUser() async -> Bool {
        guard !manualRefreshInProgress else { return false }
        manualRefreshInProgress = true
        defer { manualRefreshInProgress = false }
        let dataRefreshed = await refresh()
        manualRefreshRevision += 1
        if !sharedSyncError.isEmpty {
            error = "同步检查失败：\(sharedSyncError)"
        }
        return dataRefreshed
    }

    func start() async {
        if ready { return }
        for _ in 0..<40 {
            do {
                _ = try await NativeBridge.call("GetOverview")
                ready = true
                await refresh()
                return
            } catch {
                try? await Task.sleep(nanoseconds: 150_000_000)
            }
        }
        error = "无法连接 iDeploy 后台服务。请重新打开应用。"
    }

    @discardableResult
    func refresh() async -> Bool {
        busy = true
        defer { busy = false }
        do {
            let status = try await NativeBridge.call("GetSharedWorkspaceStatus")
            sharedMode = status["mode"].string
            sharedUnlocked = status["unlocked"].bool
            if sharedMode == "shared" {
                if sharedUnlocked {
                    let next = try await NativeBridge.call("ReconcileSharedWorkspace", timeoutSeconds: 120)
                    sharedCursor = next["cursor"].int
                } else {
                    _ = try await NativeBridge.call("SyncSharedWorkspace", timeoutSeconds: 90)
                    sharedCursor = status["cursor"].int
                }
                let health = try await NativeBridge.call("GetSharedSyncHealth", timeoutSeconds: 35)
                sharedUpToDate = health["upToDate"].bool
                sharedLastSyncAt = health["lastSyncAt"].string
                sharedSyncDetail = health["detail"].string
            } else {
                sharedUpToDate = false
                sharedLastSyncAt = ""
                sharedSyncDetail = ""
            }
            sharedSyncError = ""
        } catch {
            sharedSyncError = error.localizedDescription
            sharedUpToDate = false
        }
        for attempt in 0..<16 {
            do {
                let nextOverview = try await NativeBridge.call("GetOverview")
                let nextDeployments = try await NativeBridge.call("ListDeployments").array
                let nextServers = try await NativeBridge.call("ListServers").array
                let nextDomains = try await NativeBridge.call("ListDomains").array
                let nextDNSConnections = try await NativeBridge.call("ListDNSConnections").array
                let nextProjects = try await NativeBridge.call("ListProjects").array
                let nextRegistries = try await NativeBridge.call("ListRegistries").array
                let nextDatabases = try await NativeBridge.call("ListDatabaseConnections").array
                overview = nextOverview
                deployments = nextDeployments
                servers = nextServers
                domains = nextDomains
                dnsConnections = nextDNSConnections
                projects = nextProjects
                registries = nextRegistries
                databases = nextDatabases
                return true
            } catch {
                let retryable = (error as? BridgeError).map {
                    ["无法连接 iDeploy 后台服务", "后台服务没有响应", "发送请求失败"].contains($0.message)
                } ?? false
                if retryable && attempt < 15 && !Task.isCancelled {
                    try? await Task.sleep(nanoseconds: 200_000_000)
                    continue
                }
                self.error = error.localizedDescription
                return false
            }
        }
        return false
    }

    @discardableResult
    func perform(_ method: String, _ args: [Any] = [], refreshAfter: Bool = true) async -> J? {
        busy = true
        defer { busy = false }
        do {
            let result = try await NativeBridge.call(method, args)
            if refreshAfter { await refresh() }
            return result
        } catch {
            self.error = error.localizedDescription
            return nil
        }
    }
}
