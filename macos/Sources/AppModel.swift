import AppKit
import Combine
import Foundation

enum AppSection: String, CaseIterable, Identifiable {
    case overview, deployments, servers, projects, registries, databases, settings
    var id: String { rawValue }
    var title: String {
        switch self {
        case .overview: "总览"
        case .deployments: "部署"
        case .servers: "服务器"
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
    @Published var projects: [J] = []
    @Published var registries: [J] = []
    @Published var databases: [J] = []
    @Published var error: String?
    @Published var busy = false
    @Published var ready = false

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

    func refresh() async {
        busy = true
        defer { busy = false }
        for attempt in 0..<16 {
            do {
                let nextOverview = try await NativeBridge.call("GetOverview")
                let nextDeployments = try await NativeBridge.call("ListDeployments").array
                let nextServers = try await NativeBridge.call("ListServers").array
                let nextProjects = try await NativeBridge.call("ListProjects").array
                let nextRegistries = try await NativeBridge.call("ListRegistries").array
                let nextDatabases = try await NativeBridge.call("ListDatabaseConnections").array
                overview = nextOverview
                deployments = nextDeployments
                servers = nextServers
                projects = nextProjects
                registries = nextRegistries
                databases = nextDatabases
                return
            } catch {
                let retryable = (error as? BridgeError).map {
                    ["无法连接 iDeploy 后台服务", "后台服务没有响应", "发送请求失败"].contains($0.message)
                } ?? false
                if retryable && attempt < 15 && !Task.isCancelled {
                    try? await Task.sleep(nanoseconds: 200_000_000)
                    continue
                }
                self.error = error.localizedDescription
                return
            }
        }
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
