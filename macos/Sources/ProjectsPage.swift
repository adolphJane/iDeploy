import AppKit
import SwiftUI

private struct ProjectFormRequest: Identifiable {
    let id = UUID()
    let project: J?
}

struct ProjectsPage: View {
    @EnvironmentObject private var model: AppModel
    @State private var selectedID: String?
    @State private var formRequest: ProjectFormRequest?
    @State private var showingGenerator = false
    @State private var gitStatus = J(nil)
    @State private var deleteTarget: J?

    private var selected: J? { model.projects.first { $0.id == selectedID } }

    var body: some View {
        HStack(spacing: 0) {
            VStack(spacing: 0) {
                HStack {
                    WorkspaceListHeader(title: "项目", count: model.projects.count)
                    Spacer()
                    Button {
                        formRequest = ProjectFormRequest(project: nil)
                    } label: { Image(systemName: "plus") }
                    .buttonStyle(DeploySecondaryButtonStyle())
                    .help("添加项目")
                }
                .padding(14)
                .background(DeployStyle.overviewHeader)
                Rectangle().fill(DeployStyle.line).frame(height: 1)
                ScrollView(.vertical, showsIndicators: false) {
                    LazyVStack(spacing: 5) {
                        ForEach(model.projects, id: \.id) { project in
                            let isSelected = selectedID == project.id
                            Button {
                                selectedID = project.id
                            } label: {
                                HStack(spacing: 11) {
                                    Image(systemName: project["sourceKind"].string == "git" ? "point.3.connected.trianglepath.dotted" : "folder")
                                        .font(.system(size: 15, weight: .medium))
                                        .foregroundStyle(DeployStyle.green)
                                        .frame(width: 30, height: 30)
                                        .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 8))
                                    VStack(alignment: .leading, spacing: 5) {
                                        Text(project["name"].string)
                                            .font(.system(size: 12, weight: .semibold))
                                            .foregroundStyle(DeployStyle.ink)
                                            .lineLimit(1)
                                        Text(project["sourceKind"].string == "git" ? project["repositoryUrl"].string : project["localPath"].string)
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
            if let project = selected {
                PageContainer {
                    HStack(alignment: .top) {
                        SectionHeading(project["name"].string, project["description"].string)
                        Spacer()
                        StateBadge(state: project["status"].string)
                    }
                    HStack(spacing: 8) {
                        Button("编辑配置") { formRequest = ProjectFormRequest(project: project) }
                        .buttonStyle(DeployPrimaryButtonStyle())
                        Button("检查项目") {
                            Task { await model.perform("InspectProject", [project.id]) }
                        }
                        .buttonStyle(DeploySecondaryButtonStyle())
                        .disabled(project["sourceKind"].string != "local")
                        Button("Git 状态") {
                            Task { gitStatus = await model.perform("ProjectGitStatus", [project.id, true], refreshAfter: false) ?? J(nil) }
                        }
                        .buttonStyle(DeploySecondaryButtonStyle())
                        Button("生成 Dockerfile") { showingGenerator = true }
                            .buttonStyle(DeploySecondaryButtonStyle())
                            .disabled(project["sourceKind"].string != "local")
                        Spacer(minLength: 8)
                        Button("移除", role: .destructive) { deleteTarget = project }
                    }
                    ProjectDetailCard(title: "代码来源") {
                        ProjectDetailRow(key: "类型", value: project["sourceKind"].string == "git" ? "Git 仓库" : "本地目录")
                        ProjectDetailRow(key: "位置", value: project["sourceKind"].string == "git" ? project["repositoryUrl"].string : project["localPath"].string)
                        if project["sourceKind"].string == "git" {
                            ProjectDetailRow(key: "引用", value: "\(project["refKind"].string) \(project["refValue"].string)")
                        }
                    }
                    ProjectDetailCard(title: "构建配置") {
                        ProjectDetailRow(key: "上下文", value: project["contextPath"].string)
                        ProjectDetailRow(key: "Dockerfile", value: project["dockerfilePath"].string)
                        ProjectDetailRow(key: "Compose", value: project["composePath"].string)
                        ProjectDetailRow(key: "镜像仓库", value: project["imageRepository"].string)
                    }
                    if !gitStatus.isNull {
                        ProjectDetailCard(title: "Git 状态") {
                            ProjectDetailRow(key: "分支", value: gitStatus["branch"].string)
                            ProjectDetailRow(key: "提交", value: gitStatus["head"].string)
                            ProjectDetailRow(key: "未提交文件", value: gitStatus["changedFiles"].string)
                            ProjectDetailRow(key: "落后远端", value: gitStatus["behind"].string)
                        }
                    }
                    if !project["lastError"].string.isEmpty {
                        Label(project["lastError"].string, systemImage: "exclamationmark.triangle")
                            .foregroundStyle(.orange)
                    }
                }
                .environment(\.colorScheme, .light)
            } else {
                VStack(spacing: 12) {
                    Image(systemName: "folder")
                        .font(.system(size: 28, weight: .light))
                        .foregroundStyle(DeployStyle.green)
                        .frame(width: 68, height: 68)
                        .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 19))
                    Text("选择一个项目")
                        .font(.system(size: 16, weight: .semibold))
                        .foregroundStyle(DeployStyle.ink)
                    Text("查看源码、构建配置与 Git 状态。")
                        .font(.system(size: 12))
                        .foregroundStyle(DeployStyle.muted)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(DeployStyle.canvas)
            }
        }
        .sheet(item: $formRequest) { request in
            ProjectFormView(project: request.project)
                .environmentObject(model)
        }
        .onChange(of: selectedID) { _ in gitStatus = J(nil) }
        .sheet(isPresented: $showingGenerator) {
            if let selected {
                DockerfileGeneratorView(project: selected)
                    .environmentObject(model)
            }
        }
        .confirmationDialog("移除项目？", isPresented: Binding(
            get: { deleteTarget != nil },
            set: { if !$0 { deleteTarget = nil } }
        )) {
            Button("移除项目", role: .destructive) {
                guard let target = deleteTarget else { return }
                Task {
                    await model.perform("DeleteProject", [target.id])
                    selectedID = nil
                    deleteTarget = nil
                }
            }
        } message: {
            Text("这会从 iDeploy 移除配置，不会删除源码。")
        }
    }
}

private struct ProjectDetailCard<Content: View>: View {
    let title: String
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(title)
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(DeployStyle.ink)
            content
        }
        .padding(18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .deployCard(fill: DeployStyle.overviewPanel)
    }
}

private struct ProjectDetailRow: View {
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

struct ProjectFormView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let project: J?
    @State private var name: String
    @State private var description: String
    @State private var sourceKind: String
    @State private var localPath: String
    @State private var repositoryURL: String
    @State private var refKind: String
    @State private var refValue: String
    @State private var contextPath: String
    @State private var dockerfilePath: String
    @State private var composePath: String
    @State private var imageRepository: String

    init(project: J?) {
        self.project = project
        _name = State(initialValue: project?["name"].string ?? "")
        _description = State(initialValue: project?["description"].string ?? "")
        _sourceKind = State(initialValue: project?["sourceKind"].string ?? "local")
        _localPath = State(initialValue: project?["localPath"].string ?? "")
        _repositoryURL = State(initialValue: project?["repositoryUrl"].string ?? "")
        _refKind = State(initialValue: project?["refKind"].string ?? "branch")
        _refValue = State(initialValue: project?["refValue"].string ?? "main")
        _contextPath = State(initialValue: project?["contextPath"].string ?? ".")
        _dockerfilePath = State(initialValue: project?["dockerfilePath"].string ?? "")
        _composePath = State(initialValue: project?["composePath"].string ?? "")
        _imageRepository = State(initialValue: project?["imageRepository"].string ?? "")
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 14) {
                Image(systemName: "folder")
                    .font(.system(size: 20, weight: .medium))
                    .foregroundStyle(DeployStyle.green)
                    .frame(width: 48, height: 48)
                    .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 13))
                VStack(alignment: .leading, spacing: 5) {
                    Text(project == nil ? "添加项目" : "编辑项目配置")
                        .font(.system(size: 21, weight: .bold))
                        .foregroundStyle(DeployStyle.ink)
                    Text("填写代码来源和构建配置，用于后续部署。")
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
                        SectionHeading("基本信息", "为项目设置便于识别的名称")
                        HStack(alignment: .top, spacing: 16) {
                            VStack(alignment: .leading, spacing: 7) {
                                Text("名称").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                TextField("例如 API 服务", text: $name)
                                    .textFieldStyle(.roundedBorder)
                            }
                            VStack(alignment: .leading, spacing: 7) {
                                Text("说明").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                TextField("项目用途，可选", text: $description)
                                    .textFieldStyle(.roundedBorder)
                            }
                        }
                    }
                    .padding(16)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .deployCard(fill: DeployStyle.overviewPanel)

                    VStack(alignment: .leading, spacing: 15) {
                        SectionHeading("代码来源", "选择本地目录或 Git 仓库")
                        VStack(alignment: .leading, spacing: 7) {
                            Text("来源类型").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                            Picker("来源类型", selection: $sourceKind) {
                                Text("本地目录").tag("local")
                                Text("Git 仓库").tag("git")
                            }
                            .labelsHidden()
                            .frame(maxWidth: .infinity, alignment: .leading)
                        }
                    if sourceKind == "local" {
                            VStack(alignment: .leading, spacing: 7) {
                                Text("本地目录").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                HStack(spacing: 10) {
                                    TextField("选择或输入项目目录", text: $localPath)
                                        .textFieldStyle(.roundedBorder)
                                    Button("选择目录…") {
                                        let panel = NSOpenPanel()
                                        panel.canChooseDirectories = true
                                        panel.canChooseFiles = false
                                        if panel.runModal() == .OK { localPath = panel.url?.path ?? "" }
                                    }
                                    .buttonStyle(DeploySecondaryButtonStyle())
                                }
                            }
                    } else {
                            VStack(alignment: .leading, spacing: 7) {
                                Text("仓库 URL").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                TextField("输入 HTTPS 或 SSH 仓库地址", text: $repositoryURL)
                                    .textFieldStyle(.roundedBorder)
                            }
                            HStack(alignment: .top, spacing: 16) {
                                VStack(alignment: .leading, spacing: 7) {
                                    Text("引用类型").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                    Picker("引用类型", selection: $refKind) {
                                        Text("分支").tag("branch")
                                        Text("标签").tag("tag")
                                        Text("提交").tag("commit")
                                    }
                                    .labelsHidden()
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                }
                                VStack(alignment: .leading, spacing: 7) {
                                    Text("引用").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                    TextField("例如 main", text: $refValue)
                                        .textFieldStyle(.roundedBorder)
                                }
                            }
                        }
                    }
                    .padding(16)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .deployCard(fill: DeployStyle.overviewPanel)

                    VStack(alignment: .leading, spacing: 15) {
                        SectionHeading("构建配置", "路径相对于项目目录填写，可按需留空")
                        HStack(alignment: .top, spacing: 16) {
                            VStack(alignment: .leading, spacing: 7) {
                                Text("上下文路径").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                TextField("默认 .", text: $contextPath)
                                    .textFieldStyle(.roundedBorder)
                            }
                            VStack(alignment: .leading, spacing: 7) {
                                Text("Dockerfile 路径").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                TextField("例如 Dockerfile", text: $dockerfilePath)
                                    .textFieldStyle(.roundedBorder)
                            }
                        }
                        HStack(alignment: .top, spacing: 16) {
                            VStack(alignment: .leading, spacing: 7) {
                                Text("Compose 路径").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                TextField("例如 compose.yml", text: $composePath)
                                    .textFieldStyle(.roundedBorder)
                            }
                            VStack(alignment: .leading, spacing: 7) {
                                Text("镜像名称").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                TextField("例如 team/service", text: $imageRepository)
                                    .textFieldStyle(.roundedBorder)
                            }
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
            HStack {
                Text("名称和代码来源为必填项")
                    .font(.system(size: 11))
                    .foregroundStyle(DeployStyle.muted)
                Spacer()
                Button("取消") { dismiss() }
                    .buttonStyle(DeploySecondaryButtonStyle())
                    .keyboardShortcut(.cancelAction)
                Button("保存") {
                    Task {
                        let input: [String: Any] = [
                            "name": name, "description": description, "sourceKind": sourceKind,
                            "localPath": localPath, "repositoryUrl": repositoryURL,
                            "refKind": refKind, "refValue": refValue, "contextPath": contextPath,
                            "dockerfilePath": dockerfilePath, "composePath": composePath,
                            "imageRepository": imageRepository
                        ]
                        let result = await model.perform(project == nil ? "CreateProject" : "UpdateProject",
                                                         project == nil ? [input] : [project!.id, input])
                        if result != nil { dismiss() }
                    }
                }
                .buttonStyle(DeployPrimaryButtonStyle())
                .keyboardShortcut(.defaultAction)
                .disabled(name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ||
                          (sourceKind == "local" ? localPath.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty :
                           repositoryURL.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ||
                           refValue.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty))
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 16)
            .background(DeployStyle.overviewPanel)
        }
        .frame(width: 650, height: sourceKind == "git" ? 760 : 750)
        .environment(\.colorScheme, .light)
    }
}

struct DockerfileGeneratorView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let project: J
    @State private var content = ""
    @State private var detail = ""
    @State private var overwrite = false

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            SectionHeading("Dockerfile 草案", project["name"].string)
            HStack {
                Button("内置模板") { generate("auto") }
                Button("DeepSeek") { generate("deepseek") }
                Spacer()
                Text(detail).font(.caption).foregroundStyle(.secondary)
            }
            TextEditor(text: $content)
                .font(.system(.body, design: .monospaced))
                .border(.separator)
            Toggle("覆盖现有 Dockerfile", isOn: $overwrite)
            HStack {
                Spacer()
                Button("取消") { dismiss() }
                Button("写入项目") {
                    Task {
                        let input: [String: Any] = ["projectId": project.id, "content": content, "overwrite": overwrite]
                        if await model.perform("SaveGeneratedDockerfile", [input]) != nil { dismiss() }
                    }
                }
                .disabled(content.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
        }
        .padding(20)
        .frame(width: 740, height: 600)
        .task { generate("auto") }
    }

    private func generate(_ mode: String) {
        Task {
            if let result = await model.perform("GenerateDockerfile", [["projectId": project.id, "mode": mode]], refreshAfter: false) {
                content = result["content"].string
                detail = result["detail"].string
            }
        }
    }
}
