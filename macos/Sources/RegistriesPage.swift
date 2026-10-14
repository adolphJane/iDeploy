import AppKit
import SwiftUI

private struct RegistryFormRequest: Identifiable {
    let id = UUID()
    let registry: J?
}

struct RegistriesPage: View {
    @EnvironmentObject private var model: AppModel
    @State private var selectedID: String?
    @State private var formRequest: RegistryFormRequest?
    @State private var projects: [J] = []
    @State private var repositories: [J] = []
    @State private var artifacts: [J] = []
    @State private var selectedProject = ""
    @State private var selectedRepository = ""
    @State private var isBrowsing = false
    @State private var browseLevel = 0
    @State private var projectSearch = ""
    @State private var repositorySearch = ""
    @State private var artifactSearch = ""
    @State private var didBrowse = false
    @State private var didLoadRepositories = false
    @State private var didLoadArtifacts = false
    @State private var loadingProjects = false
    @State private var loadingRepositories = false
    @State private var loadingArtifacts = false
    @State private var projectsError = ""
    @State private var repositoriesError = ""
    @State private var artifactsError = ""
    @State private var deleteTarget: J?
    @State private var checkingRegistryID: String?
    @State private var checkMessage = ""
    @State private var checkFailed = false

    private var selected: J? { model.registries.first { $0.id == selectedID } }
    private var repositoryBreadcrumb: String {
        let prefix = selectedProject + "/"
        return selectedRepository.hasPrefix(prefix) ? String(selectedRepository.dropFirst(prefix.count)) : selectedRepository
    }

    var body: some View {
        HStack(spacing: 0) {
            VStack(spacing: 0) {
                HStack {
                    WorkspaceListHeader(title: "镜像仓库", count: model.registries.count)
                    Spacer()
                    Button { formRequest = RegistryFormRequest(registry: nil) } label: { Image(systemName: "plus") }
                        .buttonStyle(DeploySecondaryButtonStyle())
                    .help("添加仓库")
                }
                .padding(14)
                .background(DeployStyle.overviewHeader)
                Rectangle().fill(DeployStyle.line).frame(height: 1)
                ScrollView(.vertical, showsIndicators: false) {
                    LazyVStack(spacing: 5) {
                        ForEach(model.registries, id: \.id) { registry in
                            let isSelected = selectedID == registry.id
                            Button {
                                selectedID = registry.id
                            } label: {
                                HStack(spacing: 11) {
                                    Image(systemName: "archivebox")
                                        .font(.system(size: 15, weight: .medium))
                                        .foregroundStyle(DeployStyle.green)
                                        .frame(width: 30, height: 30)
                                        .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 8))
                                    VStack(alignment: .leading, spacing: 5) {
                                        Text(registry["name"].string)
                                            .font(.system(size: 12, weight: .semibold))
                                            .foregroundStyle(DeployStyle.ink)
                                            .lineLimit(1)
                                        Text(registry["endpoint"].string)
                                            .font(.system(size: 10))
                                            .foregroundStyle(DeployStyle.muted)
                                            .lineLimit(1)
                                    }
                                    Spacer(minLength: 0)
                                    if isSelected {
                                        Capsule().fill(DeployStyle.greenBright).frame(width: 3, height: 20)
                                    }
                                }
                                .padding(.horizontal, 12)
                                .frame(maxWidth: .infinity, minHeight: 56, alignment: .leading)
                                .background(isSelected ? DeployStyle.greenSoft : Color.clear,
                                            in: RoundedRectangle(cornerRadius: 10))
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)
                            .accessibilityAddTraits(isSelected ? .isSelected : [])
                        }
                    }
                    .padding(10)
                }
                .background(DeployStyle.canvas)
            }
            .frame(minWidth: 240, idealWidth: 280, maxWidth: 330)
            .background(DeployStyle.canvas)
            Rectangle().fill(DeployStyle.line).frame(width: 1)
            if let registry = selected {
                ZStack {
                    overview(registry)
                        .opacity(isBrowsing ? 0 : 1)
                        .allowsHitTesting(!isBrowsing)
                        .accessibilityHidden(isBrowsing)
                    browser(registry)
                        .opacity(isBrowsing ? 1 : 0)
                        .allowsHitTesting(isBrowsing)
                        .accessibilityHidden(!isBrowsing)
                }
                .id(registry.id)
            } else {
                VStack(spacing: 12) {
                    Image(systemName: "archivebox")
                        .font(.system(size: 28, weight: .light))
                        .foregroundStyle(DeployStyle.green)
                        .frame(width: 68, height: 68)
                        .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 19))
                    Text("选择一个镜像仓库")
                        .font(.system(size: 16, weight: .semibold))
                        .foregroundStyle(DeployStyle.ink)
                    Text("查看连接状态、项目与制品。")
                        .font(.system(size: 12))
                        .foregroundStyle(DeployStyle.muted)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(DeployStyle.canvas)
            }
        }
        .sheet(item: $formRequest) { request in
            RegistryFormView(registry: request.registry).environmentObject(model)
        }
        .onChange(of: selectedID) { _ in
            isBrowsing = false
            browseLevel = 0
            projects = []
            repositories = []
            artifacts = []
            selectedProject = ""
            selectedRepository = ""
            projectSearch = ""
            repositorySearch = ""
            artifactSearch = ""
            didBrowse = false
            didLoadRepositories = false
            didLoadArtifacts = false
            loadingProjects = false
            loadingRepositories = false
            loadingArtifacts = false
            projectsError = ""
            repositoriesError = ""
            artifactsError = ""
            checkMessage = ""
            checkFailed = false
        }
        .confirmationDialog("移除镜像仓库？", isPresented: Binding(
            get: { deleteTarget != nil },
            set: { if !$0 { deleteTarget = nil } }
        )) {
            Button("移除", role: .destructive) {
                guard let target = deleteTarget else { return }
                Task {
                    await model.perform("DeleteRegistry", [target.id])
                    selectedID = nil
                    deleteTarget = nil
                }
            }
        }
    }

    private func overview(_ registry: J) -> some View {
        PageContainer {
            HStack {
                SectionHeading(registry["name"].string, registry["endpoint"].string)
                Spacer()
                StateBadge(state: registry["status"].string)
            }
            HStack(spacing: 8) {
                Button("编辑配置") { formRequest = RegistryFormRequest(registry: registry) }
                    .buttonStyle(DeployPrimaryButtonStyle())
                Button(checkingRegistryID == registry.id ? "检测中…" : "检测连接") {
                    Task { await testConnection(registry.id) }
                }
                .buttonStyle(DeploySecondaryButtonStyle())
                .disabled(checkingRegistryID == registry.id)
                Button("浏览仓库") { openBrowser(registry.id) }
                    .buttonStyle(DeploySecondaryButtonStyle())
                Spacer(minLength: 8)
                Button("移除", role: .destructive) { deleteTarget = registry }
            }
            VStack(alignment: .leading, spacing: 12) {
                Text("连接信息")
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(DeployStyle.ink)
                RegistryInfoRow(key: "类型", value: registry["kind"].string.uppercased())
                RegistryInfoRow(key: "地址", value: registry["endpoint"].string)
                RegistryInfoRow(key: "用户名", value: registry["username"].string)
                RegistryInfoRow(key: "凭据", value: registry["credentialConfigured"].bool ? "已保存到钥匙串" : "未设置")
            }
            .padding(18)
            .frame(maxWidth: .infinity, alignment: .leading)
            .deployCard(fill: DeployStyle.overviewPanel)
            if !checkMessage.isEmpty {
                HStack(spacing: 8) {
                    if checkingRegistryID == registry.id {
                        ProgressView().controlSize(.small)
                    } else {
                        Image(systemName: checkFailed ? "exclamationmark.circle.fill" : "checkmark.circle.fill")
                    }
                    Text(checkMessage).textSelection(.enabled)
                }
                .font(.system(size: 12))
                .foregroundStyle(checkFailed ? Color.red : DeployStyle.green)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            if !registry["lastError"].string.isEmpty {
                Label(registry["lastError"].string, systemImage: "exclamationmark.triangle")
                    .font(.system(size: 12))
                    .foregroundStyle(.red)
                    .textSelection(.enabled)
            }
            RegistryBrowseEmpty(symbol: "square.stack.3d.up", title: "浏览镜像仓库",
                                detail: "在独立视图中查看项目、仓库与制品。")
        }
        .environment(\.colorScheme, .light)
    }

    private func openBrowser(_ id: String) {
        isBrowsing = true
        if !didBrowse && !loadingProjects { loadProjects(id) }
    }

    private func browser(_ registry: J) -> some View {
        VStack(spacing: 0) {
            HStack(spacing: 12) {
                Button {
                    isBrowsing = false
                } label: {
                    Label("连接信息", systemImage: "chevron.left")
                }
                .buttonStyle(DeploySecondaryButtonStyle())
                VStack(alignment: .leading, spacing: 3) {
                    Text(registry["name"].string)
                        .font(.system(size: 18, weight: .bold))
                        .foregroundStyle(DeployStyle.ink)
                    Text(registry["endpoint"].string)
                        .font(.system(size: 11))
                        .foregroundStyle(DeployStyle.muted)
                        .lineLimit(1)
                }
                Spacer(minLength: 0)
                StateBadge(state: registry["status"].string)
                Button {
                    refreshCurrentLevel(registry.id)
                } label: {
                    Label("刷新", systemImage: "arrow.clockwise")
                }
                .buttonStyle(DeploySecondaryButtonStyle())
                .disabled(loadingProjects || loadingRepositories || loadingArtifacts)
            }
            .padding(.horizontal, 20)
            .frame(height: 74)
            .background(DeployStyle.overviewHeader)
            Rectangle().fill(DeployStyle.line).frame(height: 1)
            HStack(spacing: 8) {
                Button("项目") { browseLevel = 0 }
                    .foregroundStyle(browseLevel == 0 ? DeployStyle.ink : DeployStyle.green)
                if !selectedProject.isEmpty {
                    Image(systemName: "chevron.right")
                        .foregroundStyle(DeployStyle.muted)
                    Button(selectedProject) { browseLevel = 1 }
                        .foregroundStyle(browseLevel == 1 ? DeployStyle.ink : DeployStyle.green)
                }
                if !selectedRepository.isEmpty {
                    Image(systemName: "chevron.right")
                        .foregroundStyle(DeployStyle.muted)
                    Button(repositoryBreadcrumb) { browseLevel = 2 }
                        .foregroundStyle(browseLevel == 2 ? DeployStyle.ink : DeployStyle.green)
                }
                Spacer(minLength: 0)
            }
            .buttonStyle(.plain)
            .font(.system(size: 12, weight: .semibold))
            .lineLimit(1)
            .truncationMode(.middle)
            .padding(.horizontal, 24)
            .frame(height: 46)
            ZStack {
                projectList(registry.id)
                    .opacity(browseLevel == 0 ? 1 : 0)
                    .allowsHitTesting(browseLevel == 0)
                    .accessibilityHidden(browseLevel != 0)
                repositoryList(registry.id)
                    .opacity(browseLevel == 1 ? 1 : 0)
                    .allowsHitTesting(browseLevel == 1)
                    .accessibilityHidden(browseLevel != 1)
                artifactList()
                    .opacity(browseLevel == 2 ? 1 : 0)
                    .allowsHitTesting(browseLevel == 2)
                    .accessibilityHidden(browseLevel != 2)
            }
            .padding(.horizontal, 20)
            .padding(.bottom, 20)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(DeployStyle.canvas)
        .environment(\.colorScheme, .light)
    }

    private func refreshCurrentLevel(_ registryID: String) {
        switch browseLevel {
        case 2: loadArtifacts(registryID, selectedRepository)
        case 1: loadRepositories(registryID, selectedProject)
        default: loadProjects(registryID)
        }
    }

    private func projectList(_ registryID: String) -> some View {
        RegistryBrowserList(title: "项目", detail: "选择项目查看其中的镜像仓库", count: projects.count,
                            search: $projectSearch, searchHint: "搜索项目") {
            if loadingProjects {
                RegistryBrowserMessage(symbol: "arrow.triangle.2.circlepath", title: "正在读取项目", detail: "请稍候…")
            } else if !projectsError.isEmpty {
                RegistryBrowserMessage(symbol: "exclamationmark.triangle", title: "读取失败", detail: projectsError)
            } else if projects.isEmpty {
                RegistryBrowserMessage(symbol: "folder", title: "没有项目", detail: "当前镜像仓库没有可浏览的项目。")
            } else {
                let filtered = projects.filter { projectSearch.isEmpty || $0["name"].string.localizedCaseInsensitiveContains(projectSearch) }
                if filtered.isEmpty {
                    RegistryBrowserMessage(symbol: "magnifyingglass", title: "没有匹配项目", detail: "试试其他关键词。")
                } else {
                    ForEach(filtered, id: \.selfName) { project in
                        Button {
                            selectProject(project["name"].string, registryID: registryID)
                        } label: {
                            RegistryBrowseRow(title: project["name"].string,
                                              detail: "\(project["repositoryCount"].int) 个仓库",
                                              symbol: "folder", isSelected: selectedProject == project["name"].string)
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
        }
    }

    private func repositoryList(_ registryID: String) -> some View {
        RegistryBrowserList(title: "仓库", detail: selectedProject.isEmpty ? "请先选择项目" : "项目：\(selectedProject)",
                            count: repositories.count, search: $repositorySearch, searchHint: "搜索仓库") {
            if selectedProject.isEmpty {
                RegistryBrowserMessage(symbol: "folder", title: "选择项目", detail: "仓库将显示在这里。")
            } else if loadingRepositories {
                RegistryBrowserMessage(symbol: "arrow.triangle.2.circlepath", title: "正在读取仓库", detail: "请稍候…")
            } else if !repositoriesError.isEmpty {
                RegistryBrowserMessage(symbol: "exclamationmark.triangle", title: "读取失败", detail: repositoriesError)
            } else if didLoadRepositories && repositories.isEmpty {
                RegistryBrowserMessage(symbol: "shippingbox", title: "没有仓库", detail: "这个项目中没有镜像仓库。")
            } else {
                let filtered = repositories.filter { repositorySearch.isEmpty || $0["name"].string.localizedCaseInsensitiveContains(repositorySearch) }
                if filtered.isEmpty {
                    RegistryBrowserMessage(symbol: "magnifyingglass", title: "没有匹配仓库", detail: "试试其他关键词。")
                } else {
                    ForEach(filtered, id: \.selfName) { repository in
                        Button {
                            selectRepository(repository["name"].string, registryID: registryID)
                        } label: {
                            RegistryBrowseRow(title: repository["name"].string,
                                              detail: "\(repository["artifactCount"].int) 个制品",
                                              symbol: "shippingbox", isSelected: selectedRepository == repository["name"].string)
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
        }
    }

    private func artifactList() -> some View {
        RegistryBrowserList(title: "制品", detail: selectedRepository.isEmpty ? "请先选择仓库" : "仓库：\(selectedRepository)",
                            count: artifacts.count, search: $artifactSearch, searchHint: "搜索标签或摘要") {
            if selectedRepository.isEmpty {
                RegistryBrowserMessage(symbol: "shippingbox", title: "选择仓库", detail: "制品将显示在这里。")
            } else if loadingArtifacts {
                RegistryBrowserMessage(symbol: "arrow.triangle.2.circlepath", title: "正在读取制品", detail: "请稍候…")
            } else if !artifactsError.isEmpty {
                RegistryBrowserMessage(symbol: "exclamationmark.triangle", title: "读取失败", detail: artifactsError)
            } else if didLoadArtifacts && artifacts.isEmpty {
                RegistryBrowserMessage(symbol: "cube", title: "没有制品", detail: "这个仓库还没有可用的镜像制品。")
            } else {
                let filtered = artifacts.filter {
                    artifactSearch.isEmpty || $0["digest"].string.localizedCaseInsensitiveContains(artifactSearch) ||
                    $0["tags"].array.contains { $0.string.localizedCaseInsensitiveContains(artifactSearch) }
                }
                if filtered.isEmpty {
                    RegistryBrowserMessage(symbol: "magnifyingglass", title: "没有匹配制品", detail: "试试其他关键词。")
                } else {
                    ForEach(filtered, id: \.selfDigest) { artifact in
                        RegistryArtifactRow(artifact: artifact,
                                            endpoint: selected?["endpoint"].string ?? "",
                                            repository: selectedRepository)
                    }
                }
            }
        }
    }

    private func selectProject(_ name: String, registryID: String) {
        browseLevel = 1
        if selectedProject == name {
            if !didLoadRepositories || !repositoriesError.isEmpty { loadRepositories(registryID, name) }
            return
        }
        selectedProject = name
        selectedRepository = ""
        repositorySearch = ""
        artifactSearch = ""
        repositories = []
        artifacts = []
        didLoadRepositories = false
        didLoadArtifacts = false
        repositoriesError = ""
        artifactsError = ""
        loadRepositories(registryID, name)
    }

    private func selectRepository(_ name: String, registryID: String) {
        browseLevel = 2
        if selectedRepository == name {
            if !didLoadArtifacts || !artifactsError.isEmpty { loadArtifacts(registryID, name) }
            return
        }
        selectedRepository = name
        artifactSearch = ""
        artifacts = []
        didLoadArtifacts = false
        artifactsError = ""
        loadArtifacts(registryID, name)
    }

    private func testConnection(_ id: String) async {
        guard checkingRegistryID == nil else { return }
        checkingRegistryID = id
        checkFailed = false
        checkMessage = "正在提交连接检测…"

        guard let queued = await model.perform("TestRegistry", [id], refreshAfter: false),
              !queued.id.isEmpty else {
            if selectedID == id {
                checkFailed = true
                checkMessage = model.error ?? "无法启动连接检测。"
            }
            checkingRegistryID = nil
            return
        }

        if selectedID == id { checkMessage = "连接检测已提交，正在等待结果…" }
        do {
            var job = queued
            while ["queued", "running", "canceling"].contains(job["status"].string) {
                try await Task.sleep(nanoseconds: 500_000_000)
                job = try await NativeBridge.call("GetJob", [queued.id])
                if selectedID == id {
                    let status = job["status"].string == "queued" ? "等待执行" : "正在检测"
                    checkMessage = "\(status)镜像仓库连接…"
                }
            }
            await model.refresh()
            if selectedID == id {
                let registry = model.registries.first { $0.id == id }
                let succeeded = job["status"].string == "succeeded"
                checkFailed = !succeeded
                let detail = succeeded ? registry?["lastMessage"].string :
                    (registry?["lastError"].string.isEmpty == false ? registry?["lastError"].string : job["error"].string)
                checkMessage = detail?.isEmpty == false ? detail! : (succeeded ? "连接检测成功。" : "连接检测失败。")
            }
        } catch {
            if selectedID == id {
                checkFailed = true
                checkMessage = "读取检测结果失败：\(error.localizedDescription)"
            }
        }
        checkingRegistryID = nil
    }

    private func loadProjects(_ id: String) {
        loadingProjects = true
        projectsError = ""
        Task {
            let result = await model.perform("ListRegistryProjects", [id], refreshAfter: false)
            guard selectedID == id else { return }
            loadingProjects = false
            if let result {
                guard result["status"].string == "healthy" else {
                    projectsError = result["detail"].string.isEmpty ? "无法读取项目。" : result["detail"].string
                    return
                }
                projects = result["projects"].array
                didBrowse = true
                if !selectedProject.isEmpty && projects.contains(where: { $0["name"].string == selectedProject }) {
                    loadRepositories(id, selectedProject)
                } else {
                    selectedProject = ""
                    selectedRepository = ""
                    repositories = []
                    artifacts = []
                    didLoadRepositories = false
                    didLoadArtifacts = false
                    browseLevel = 0
                }
            } else {
                projectsError = model.error ?? "无法读取项目。"
            }
        }
    }

    private func loadRepositories(_ registryID: String, _ projectName: String) {
        loadingRepositories = true
        repositoriesError = ""
        Task {
            let result = await model.perform("ListRegistryRepositories", [registryID, projectName], refreshAfter: false)
            guard selectedID == registryID && selectedProject == projectName else { return }
            loadingRepositories = false
            if let result {
                guard result["status"].string == "healthy" else {
                    repositoriesError = result["detail"].string.isEmpty ? "无法读取仓库。" : result["detail"].string
                    return
                }
                repositories = result["repositories"].array
                didLoadRepositories = true
                if !selectedRepository.isEmpty && repositories.contains(where: { $0["name"].string == selectedRepository }) {
                    loadArtifacts(registryID, selectedRepository)
                } else {
                    selectedRepository = ""
                    artifacts = []
                    didLoadArtifacts = false
                    if browseLevel == 2 { browseLevel = 1 }
                }
            } else {
                repositoriesError = model.error ?? "无法读取仓库。"
            }
        }
    }

    private func loadArtifacts(_ registryID: String, _ repositoryName: String) {
        loadingArtifacts = true
        artifactsError = ""
        Task {
            let result = await model.perform("ListRepositoryArtifacts", [registryID, repositoryName], refreshAfter: false)
            guard selectedID == registryID && selectedRepository == repositoryName else { return }
            loadingArtifacts = false
            if let result {
                guard result["status"].string == "healthy" else {
                    artifactsError = result["detail"].string.isEmpty ? "无法读取制品。" : result["detail"].string
                    return
                }
                artifacts = result["artifacts"].array
                didLoadArtifacts = true
            } else {
                artifactsError = model.error ?? "无法读取制品。"
            }
        }
    }
}

private struct RegistryBrowserList<Content: View>: View {
    let title: String
    let detail: String
    let count: Int
    @Binding var search: String
    let searchHint: String
    @ViewBuilder let content: Content

    var body: some View {
        VStack(spacing: 0) {
            HStack(alignment: .top) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(title)
                        .font(.system(size: 17, weight: .bold))
                        .foregroundStyle(DeployStyle.ink)
                    Text(detail)
                        .font(.system(size: 11))
                        .foregroundStyle(DeployStyle.muted)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                Spacer(minLength: 4)
                Text("\(count)")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(DeployStyle.green)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(DeployStyle.greenSoft, in: Capsule())
            }
            .padding(18)
            HStack(spacing: 7) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(DeployStyle.muted)
                TextField(searchHint, text: $search)
                    .textFieldStyle(.plain)
                    .font(.system(size: 12))
                    .foregroundStyle(DeployStyle.ink)
            }
            .padding(.horizontal, 12)
            .frame(height: 36)
            .background(DeployStyle.canvas, in: RoundedRectangle(cornerRadius: 8))
            .padding(.horizontal, 18)
            .padding(.bottom, 16)
            Rectangle().fill(DeployStyle.line).frame(height: 1)
            ScrollView(.vertical) {
                LazyVStack(spacing: 7) { content }
                    .frame(maxWidth: .infinity, alignment: .topLeading)
                    .padding(14)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .deployCard(fill: DeployStyle.overviewPanel)
    }
}

private struct RegistryBrowserMessage: View {
    let symbol: String
    let title: String
    let detail: String

    var body: some View {
        VStack(spacing: 9) {
            Image(systemName: symbol)
                .font(.system(size: 22, weight: .light))
                .foregroundStyle(DeployStyle.green)
            Text(title)
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(DeployStyle.ink)
            Text(detail)
                .font(.system(size: 11))
                .foregroundStyle(DeployStyle.muted)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 42)
        .padding(.horizontal, 12)
    }
}

private struct RegistryArtifactRow: View {
    let artifact: J
    let endpoint: String
    let repository: String
    @State private var copied = false

    private var digest: String { artifact["digest"].string }
    private var tags: String {
        let values = artifact["tags"].array.map(\.string).filter { !$0.isEmpty }
        return values.isEmpty ? "未标记" : values.joined(separator: ", ")
    }
    private var size: String {
        let bytes = artifact["size"].int
        return bytes > 0 ? ByteCountFormatter.string(fromByteCount: Int64(bytes), countStyle: .file) : "—"
    }
    private var pullCommand: String {
        let registryHost: String
        if endpoint.hasPrefix("https://") {
            registryHost = String(endpoint.dropFirst("https://".count))
        } else if endpoint.hasPrefix("http://") {
            registryHost = String(endpoint.dropFirst("http://".count))
        } else {
            registryHost = endpoint
        }
        let image = "\(registryHost.trimmingCharacters(in: CharacterSet(charactersIn: "/")))/\(repository)"
        if !digest.isEmpty { return "docker pull \(image)@\(digest)" }
        if let tag = artifact["tags"].array.map(\.string).first(where: { !$0.isEmpty }) {
            return "docker pull \(image):\(tag)"
        }
        return "docker pull \(image)"
    }

    var body: some View {
        HStack(spacing: 14) {
            Image(systemName: "cube.box")
                .font(.system(size: 15))
                .foregroundStyle(DeployStyle.green)
                .frame(width: 34, height: 34)
                .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 8))
            Text(tags)
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(DeployStyle.ink)
                .lineLimit(1)
                .truncationMode(.middle)
                .frame(width: 155, alignment: .leading)
            Text(digest)
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(DeployStyle.muted)
                .lineLimit(1)
                .truncationMode(.middle)
                .textSelection(.enabled)
                .help(digest)
                .frame(maxWidth: .infinity, alignment: .leading)
            Text(size)
                .font(.system(size: 11))
                .foregroundStyle(DeployStyle.muted)
                .frame(width: 70, alignment: .trailing)
            Button {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(pullCommand, forType: .string)
                copied = true
            } label: {
                Image(systemName: copied ? "checkmark" : "doc.on.doc")
            }
            .buttonStyle(.plain)
            .foregroundStyle(DeployStyle.green)
            .help(copied ? "已复制拉取命令" : pullCommand)
            .accessibilityLabel(copied ? "已复制 Docker 拉取命令" : "复制 Docker 拉取命令")
        }
        .padding(.horizontal, 16)
        .frame(height: 60)
        .frame(maxWidth: .infinity, alignment: .leading)
        .deployCard(radius: 10, fill: DeployStyle.overviewPanel)
    }
}

private struct RegistryInfoRow: View {
    let key: String
    let value: String

    var body: some View {
        HStack(alignment: .top, spacing: 16) {
            Text(key)
                .font(.system(size: 12))
                .foregroundStyle(DeployStyle.muted)
                .frame(width: 82, alignment: .leading)
            Text(value.isEmpty ? "—" : value)
                .font(.system(size: 12, weight: .medium))
                .foregroundStyle(DeployStyle.ink)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

private struct RegistryBrowseRow: View {
    let title: String
    let detail: String
    let symbol: String
    let isSelected: Bool

    var body: some View {
        HStack(spacing: 14) {
            Image(systemName: symbol)
                .font(.system(size: 15, weight: .medium))
                .foregroundStyle(DeployStyle.green)
                .frame(width: 34, height: 34)
                .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 8))
            Text(title)
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(DeployStyle.ink)
                .lineLimit(1)
                .truncationMode(.middle)
            Spacer(minLength: 0)
            Text(detail)
                .font(.system(size: 11))
                .foregroundStyle(DeployStyle.muted)
            Image(systemName: "chevron.right")
                .font(.system(size: 10, weight: .semibold))
                .foregroundStyle(DeployStyle.muted)
        }
        .padding(.horizontal, 16)
        .frame(height: 60)
        .frame(maxWidth: .infinity, alignment: .leading)
        .deployCard(fill: isSelected ? DeployStyle.greenSoft : DeployStyle.overviewPanel)
    }
}

private struct RegistryBrowseEmpty: View {
    let symbol: String
    let title: String
    let detail: String

    var body: some View {
        HStack(spacing: 13) {
            Image(systemName: symbol)
                .font(.system(size: 20, weight: .medium))
                .foregroundStyle(DeployStyle.green)
                .frame(width: 42, height: 42)
                .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 11))
            VStack(alignment: .leading, spacing: 4) {
                Text(title)
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(DeployStyle.ink)
                Text(detail)
                    .font(.system(size: 11))
                    .foregroundStyle(DeployStyle.muted)
            }
            Spacer(minLength: 0)
        }
        .padding(18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .deployCard(fill: DeployStyle.overviewPanel)
    }
}

private extension J {
    var selfName: String { self["name"].string }
    var selfDigest: String { self["digest"].string }
}

struct RegistryFormView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let registry: J?
    @State private var name: String
    @State private var kind: String
    @State private var endpoint: String
    @State private var username: String
    @State private var password = ""
    @State private var probe = ""
    @State private var probeSucceeded = false
    @State private var isTesting = false
    @State private var isSaving = false

    init(registry: J?) {
        self.registry = registry
        _name = State(initialValue: registry?["name"].string ?? "")
        _kind = State(initialValue: registry?["kind"].string ?? "harbor")
        _endpoint = State(initialValue: registry?["endpoint"].string ?? "")
        _username = State(initialValue: registry?["username"].string ?? "")
    }

    private var input: [String: Any] {
        ["name": name.trimmingCharacters(in: .whitespacesAndNewlines),
         "kind": kind,
         "endpoint": endpoint.trimmingCharacters(in: .whitespacesAndNewlines),
         "username": username.trimmingCharacters(in: .whitespacesAndNewlines),
         "password": password]
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 14) {
                Image(systemName: "archivebox")
                    .font(.system(size: 20, weight: .medium))
                    .foregroundStyle(DeployStyle.green)
                    .frame(width: 48, height: 48)
                    .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 13))
                VStack(alignment: .leading, spacing: 5) {
                    Text(registry == nil ? "添加镜像仓库" : "编辑镜像仓库")
                        .font(.system(size: 21, weight: .bold))
                        .foregroundStyle(DeployStyle.ink)
                    Text("连接 Harbor 或 OCI Registry，管理镜像与制品。")
                        .font(.system(size: 12))
                        .foregroundStyle(DeployStyle.muted)
                }
                Spacer()
            }
            .padding(20)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(DeployStyle.overviewHeader)

            ScrollView(.vertical, showsIndicators: true) {
                VStack(alignment: .leading, spacing: 14) {
                    VStack(alignment: .leading, spacing: 15) {
                        SectionHeading("仓库信息", "填写仓库名称、类型和服务地址")
                        HStack(alignment: .top, spacing: 16) {
                            VStack(alignment: .leading, spacing: 7) {
                                Text("名称").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                TextField("例如 开发 Harbor", text: $name)
                                    .textFieldStyle(.roundedBorder)
                            }
                            VStack(alignment: .leading, spacing: 7) {
                                Text("类型").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                Picker("类型", selection: $kind) {
                                    Text("Harbor").tag("harbor")
                                    Text("OCI Registry").tag("oci")
                                }
                                .labelsHidden()
                                .frame(maxWidth: .infinity, alignment: .leading)
                            }
                        }
                        VStack(alignment: .leading, spacing: 7) {
                            Text("服务地址").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                            TextField("例如 https://registry.example.com", text: $endpoint)
                                .textFieldStyle(.roundedBorder)
                        }
                    }
                    .padding(16)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .deployCard(fill: DeployStyle.overviewPanel)

                    VStack(alignment: .leading, spacing: 15) {
                        SectionHeading("访问凭据", "用于浏览仓库和推送镜像")
                        HStack(alignment: .top, spacing: 16) {
                            VStack(alignment: .leading, spacing: 7) {
                                Text("用户名").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                TextField("仓库用户名", text: $username)
                                    .textFieldStyle(.roundedBorder)
                            }
                            VStack(alignment: .leading, spacing: 7) {
                                Text("密码或 Token").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                SecureField("输入密码或访问令牌", text: $password)
                                    .textFieldStyle(.roundedBorder)
                            }
                        }
                        if registry != nil {
                            Label("留空将保留钥匙串中已保存的凭据。", systemImage: "key.horizontal")
                                .font(.system(size: 11))
                                .foregroundStyle(DeployStyle.muted)
                        }
                    }
                    .padding(16)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .deployCard(fill: DeployStyle.overviewPanel)

                }
                .padding(.horizontal, 24)
                .padding(.vertical, 16)
            }
            .background(DeployStyle.canvas)

            Rectangle().fill(DeployStyle.line).frame(height: 1)
            if !probe.isEmpty {
                Label(probe, systemImage: isTesting ? "arrow.triangle.2.circlepath" : probeSucceeded ? "checkmark.circle.fill" : "exclamationmark.circle.fill")
                    .font(.system(size: 11))
                    .foregroundStyle(probeSucceeded ? DeployStyle.green : DeployStyle.ink)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 24)
                    .padding(.vertical, 10)
                    .background(probeSucceeded ? DeployStyle.greenSoft : DeployStyle.overviewHeader)
                    .accessibilityAddTraits(.updatesFrequently)
            }
            HStack {
                Button {
                    guard !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                          !endpoint.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
                        probeSucceeded = false
                        probe = "请先填写仓库名称和服务地址，再测试连接。"
                        return
                    }
                    Task {
                        isTesting = true
                        probeSucceeded = false
                        probe = "正在检测仓库连接与 Docker 登录…"
                        defer { isTesting = false }
                        do {
                            let result = try await NativeBridge.call("TestRegistryConnection", [input])
                            probe = result["detail"].string.isEmpty ? "检测完成。" : result["detail"].string
                            probeSucceeded = result["status"].string == "healthy"
                        } catch {
                            probe = "连接检测失败：\(error.localizedDescription)"
                        }
                    }
                } label: {
                    if isTesting { ProgressView().controlSize(.small) }
                    Text(isTesting ? "正在检测…" : "测试连接")
                }
                .buttonStyle(DeploySecondaryButtonStyle())
                .disabled(isTesting || isSaving)
                Spacer()
                Button("取消") { dismiss() }
                    .buttonStyle(DeploySecondaryButtonStyle())
                    .keyboardShortcut(.cancelAction)
                Button("保存") {
                    Task {
                        isSaving = true
                        defer { isSaving = false }
                        let result = await model.perform(registry == nil ? "CreateRegistry" : "UpdateRegistry",
                                                         registry == nil ? [input] : [registry!.id, input])
                        if result != nil { dismiss() }
                    }
                }
                .buttonStyle(DeployPrimaryButtonStyle())
                .keyboardShortcut(.defaultAction)
                .disabled(isSaving || isTesting || name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || endpoint.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 16)
            .background(DeployStyle.overviewPanel)
        }
        .frame(width: 650, height: 545)
        .environment(\.colorScheme, .light)
        .onChange(of: name) { _ in probe = "" }
        .onChange(of: kind) { _ in probe = "" }
        .onChange(of: endpoint) { _ in probe = "" }
        .onChange(of: username) { _ in probe = "" }
        .onChange(of: password) { _ in probe = "" }
    }
}
