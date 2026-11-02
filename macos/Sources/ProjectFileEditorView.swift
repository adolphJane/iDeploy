import AppKit
import SwiftUI

struct ProjectFileEditorView: View {
    @Environment(\.dismiss) private var dismiss
    let project: J
    let kind: String
    let onSaved: () -> Void

    @State private var loaded = J(nil)
    @State private var content = ""
    @State private var originalContent = ""
    @State private var loading = false
    @State private var saving = false
    @State private var error = ""
    @State private var copied = false
    @State private var showingDiscard = false
    @State private var showingReload = false

    private var isGit: Bool { project["sourceKind"].string == "git" }
    private var isDirty: Bool { !loaded.isNull && content != originalContent }
    private var displayName: String { kind == "dockerfile" ? "Dockerfile" : "docker-compose.yml" }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 14) {
                Image(systemName: "doc.text")
                    .font(.system(size: 20))
                    .foregroundStyle(DeployStyle.green)
                    .frame(width: 48, height: 48)
                    .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 13))
                VStack(alignment: .leading, spacing: 5) {
                    Text(displayName)
                        .font(.system(size: 21, weight: .bold))
                        .foregroundStyle(DeployStyle.ink)
                    Text(project["name"].string)
                        .font(.system(size: 12))
                        .foregroundStyle(DeployStyle.muted)
                }
                Spacer()
                if isGit {
                    Text("Git 仓库 · 只读")
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundStyle(DeployStyle.green)
                } else if isDirty {
                    Text("未保存")
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundStyle(.orange)
                }
            }
            .padding(20)
            .background(DeployStyle.overviewHeader)
            Rectangle().fill(DeployStyle.line).frame(height: 1)

            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    VStack(alignment: .leading, spacing: 5) {
                        Text(loaded["path"].string.isEmpty ? "正在读取文件路径…" : loaded["path"].string)
                            .font(.system(size: 12, weight: .medium, design: .monospaced))
                            .foregroundStyle(DeployStyle.ink)
                            .textSelection(.enabled)
                        Text(isGit ? "此文件来自配置的 Git 引用。修改请在源码仓库提交，后续发布才会使用。"
                             : "保存会更新本地项目文件；下次发布时使用新内容，不会立即改变运行中的容器。")
                            .font(.system(size: 11))
                            .foregroundStyle(DeployStyle.muted)
                    }
                    Spacer()
                    if !loaded.isNull {
                        Button(copied ? "已复制" : "复制内容") {
                            NSPasteboard.general.clearContents()
                            copied = NSPasteboard.general.setString(content, forType: .string)
                        }
                        .buttonStyle(DeploySecondaryButtonStyle())
                    }
                }

                if loading {
                    HStack(spacing: 10) {
                        ProgressView().controlSize(.small)
                        Text(isGit ? "正在读取 Git 仓库文件…" : "正在读取项目文件…")
                    }
                    .foregroundStyle(DeployStyle.muted)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if !error.isEmpty && loaded.isNull {
                    VStack(alignment: .leading, spacing: 12) {
                        Text(error).foregroundStyle(.red).textSelection(.enabled)
                        Button("重试") { load() }.buttonStyle(DeploySecondaryButtonStyle())
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
                } else if !loaded.isNull {
                    if isGit {
                        ScrollView {
                            Text(content)
                                .font(.system(size: 12, design: .monospaced))
                                .foregroundStyle(DeployStyle.ink)
                                .textSelection(.enabled)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .padding(12)
                        }
                        .background(DeployStyle.panel)
                    } else {
                        TextEditor(text: $content)
                            .font(.system(size: 12, design: .monospaced))
                            .foregroundStyle(DeployStyle.ink)
                            .scrollContentBackground(.hidden)
                            .padding(8)
                            .background(DeployStyle.panel)
                            .disabled(saving)
                            .accessibilityLabel("编辑 \(displayName)")
                    }
                }
                if !error.isEmpty && !loaded.isNull {
                    HStack(alignment: .top, spacing: 12) {
                        Text(error).font(.system(size: 11)).foregroundStyle(.red).textSelection(.enabled)
                        Spacer()
                        Button("重新读取") {
                            if isDirty { showingReload = true }
                            else { load() }
                        }
                        .buttonStyle(DeploySecondaryButtonStyle())
                    }
                }
            }
            .padding(20)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .background(DeployStyle.canvas)
            Rectangle().fill(DeployStyle.line).frame(height: 1)
            HStack {
                Text(isGit ? "只读预览" : "保存时会检查文件是否在此期间被其他程序修改")
                    .font(.system(size: 11))
                    .foregroundStyle(DeployStyle.muted)
                Spacer()
                Button("关闭") { close() }
                    .buttonStyle(DeploySecondaryButtonStyle())
                    .keyboardShortcut(.cancelAction)
                if !isGit {
                    Button(saving ? "保存中…" : "保存文件") { save() }
                        .buttonStyle(DeployPrimaryButtonStyle())
                        .keyboardShortcut("s", modifiers: .command)
                        .disabled(!isDirty || saving || loading)
                }
            }
            .padding(20)
            .background(DeployStyle.overviewPanel)
        }
        .frame(width: 880, height: 620)
        .environment(\.colorScheme, .light)
        .interactiveDismissDisabled(isDirty)
        .task { load() }
        .confirmationDialog("放弃未保存的修改？", isPresented: $showingDiscard) {
            Button("放弃修改", role: .destructive) { dismiss() }
        } message: {
            Text("关闭后，本次对文件的修改不会保存。")
        }
        .confirmationDialog("重新读取文件？", isPresented: $showingReload) {
            Button("重新读取", role: .destructive) { load() }
        } message: {
            Text("当前未保存的编辑会被磁盘上的文件内容替换。")
        }
    }

    private func load() {
        guard !loading else { return }
        loading = true
        error = ""
        let input: [String: Any] = ["projectId": project.id, "kind": kind]
        Task {
            defer { loading = false }
            do {
                let result = try await NativeBridge.call("ReadProjectFile", [input], timeoutSeconds: 150)
                loaded = result
                content = result["content"].string
                originalContent = content
                copied = false
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    private func save() {
        guard isDirty && !saving && !isGit else { return }
        saving = true
        error = ""
        let input: [String: Any] = [
            "projectId": project.id, "kind": kind,
            "content": content, "expectedHash": loaded["hash"].string
        ]
        Task {
            defer { saving = false }
            do {
                let result = try await NativeBridge.call("SaveProjectFile", [input])
                loaded = result
                content = result["content"].string
                originalContent = content
                onSaved()
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    private func close() {
        if isDirty { showingDiscard = true }
        else { dismiss() }
    }
}

#Preview("项目文件编辑") {
    ProjectFileEditorView(project: J(["id": "preview", "name": "示例项目", "sourceKind": "local"]),
                          kind: "dockerfile", onSaved: {})
}
