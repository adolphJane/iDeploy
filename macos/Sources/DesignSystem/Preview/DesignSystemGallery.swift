import SwiftUI

// Preview-only gallery. It is intentionally not referenced by the app's scenes.
struct DesignSystemGallery: View {
    @State private var search = ""
    @State private var field = "orders-api"
    @State private var setting = true

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: AppSpacing.xxl) {
                Text("iDeploy 当前界面规范")
                    .font(AppTypography.largeTitle)

                AppSectionHeader(title: "颜色", subtitle: "取自现有 DeployStyle") { EmptyView() }
                HStack(spacing: AppSpacing.sm) {
                    colorSample("画布", AppColors.window)
                    colorSample("白卡片", AppColors.surface)
                    colorSample("淡绿卡片", AppColors.secondarySurface)
                    colorSample("主绿", AppColors.brand)
                    colorSample("选中底", AppColors.brandSoft)
                    colorSample("深绿侧栏", AppColors.brandSidebar)
                }

                AppSectionHeader(title: "字体") { EmptyView() }
                VStack(alignment: .leading, spacing: AppSpacing.xs) {
                    Text("标题 Title").font(AppTypography.title)
                    Text("分组 Section Title").font(AppTypography.sectionTitle)
                    Text("正文 Body · orders-api 在 prod-01 上运行").font(AppTypography.body)
                    Text("registry.example.com/orders-api:v2.4.7").font(AppTypography.monospaced)
                }

                AppSectionHeader(title: "按钮、Toolbar 与输入") { EmptyView() }
                HStack {
                    AppButton(title: "发布新版本", systemImage: "shippingbox", emphasis: .primary) {}
                    AppButton(title: "刷新") {}
                    AppButton(title: "已禁用") {}.disabled(true)
                    AppToolbarButton(title: "更多操作", systemImage: "ellipsis") {}
                }
                AppTextField(title: "部署名称", text: $field)
                AppSearchField(text: $search, prompt: "搜索部署")

                AppSectionHeader(title: "Sidebar 与列表") { EmptyView() }
                HStack(alignment: .top, spacing: AppSpacing.xl) {
                    VStack(spacing: AppSpacing.xxs) {
                        AppSidebarRow(title: "部署", systemImage: "shippingbox", selected: true) {}
                        AppSidebarRow(title: "服务器", systemImage: "server.rack", selected: false) {}
                    }
                    .frame(width: 170)
                    VStack(spacing: AppSpacing.xxs) {
                        AppListRow(title: "orders-api", subtitle: "server-01", systemImage: "shippingbox",
                                   status: "正常", selected: true) {}
                        AppListRow(title: "payment-worker", subtitle: "server-02", systemImage: "shippingbox",
                                   selected: false) {}
                    }
                }

                AppSectionHeader(title: "详情与 Inspector") { EmptyView() }
                HStack(alignment: .top, spacing: AppSpacing.md) {
                    AppDetailPanel(title: "部署配置") {
                        LabeledContent("项目", value: "orders-api")
                        LabeledContent("服务器", value: "prod-01")
                    }
                    AppInspectorPanel(title: "版本") {
                        Text("v2.4.7").font(AppTypography.monospaced)
                    }
                    .frame(width: 200)
                }

                AppSectionHeader(title: "状态与反馈") { EmptyView() }
                HStack {
                    AppStatusBadge(title: "正常", tone: .success)
                    AppStatusBadge(title: "失败", tone: .danger)
                    AppStatusBadge(title: "未检查", tone: .neutral)
                }
                AppEmptyState(title: "还没有部署", detail: "添加部署后可查看发布状态。", systemImage: "shippingbox")
                AppLoadingView(title: "正在读取…")
                AppProgressView(title: "发布中", fraction: 0.48)
                AppErrorState(title: "连接失败", detail: "请检查连接后重试。") {}

                AppSectionHeader(title: "设置") { EmptyView() }
                AppSettingsRow(title: "登录后启动", detail: "登录 macOS 后自动打开应用") {
                    Toggle("登录后启动", isOn: $setting).labelsHidden()
                }
            }
            .padding(AppSpacing.contentInset)
            .frame(maxWidth: 860, alignment: .leading)
        }
        .background(AppTheme.contentBackground)
    }

    private func colorSample(_ title: String, _ color: Color) -> some View {
        VStack(alignment: .leading, spacing: AppSpacing.xxs) {
            RoundedRectangle(cornerRadius: AppRadius.medium)
                .fill(color)
                .frame(height: 44)
                .overlay {
                    RoundedRectangle(cornerRadius: AppRadius.medium)
                        .strokeBorder(AppColors.separator, lineWidth: 1)
                }
            Text(title).font(AppTypography.caption)
        }
        .frame(maxWidth: .infinity)
    }
}

struct DesignSystemGalleryPreviewProvider: PreviewProvider {
    static var previews: some View {
        Group {
            DesignSystemGallery()
                .frame(width: 1100, height: 900)
                .preferredColorScheme(.light)
                .previewDisplayName("Light · Wide")
            DesignSystemGallery()
                .frame(width: 800, height: 700)
                .preferredColorScheme(.dark)
                .previewDisplayName("Dark · 现状差距检查")
        }
    }
}
