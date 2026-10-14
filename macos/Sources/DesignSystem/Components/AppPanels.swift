import SwiftUI

struct AppSectionHeader<Trailing: View>: View {
    let title: String
    var subtitle: String? = nil
    @ViewBuilder let trailing: () -> Trailing

    var body: some View {
        HStack(alignment: .top, spacing: AppSpacing.sm) {
            VStack(alignment: .leading, spacing: AppSpacing.xxs) {
                Text(title).font(AppTypography.title)
                if let subtitle, !subtitle.isEmpty {
                    Text(subtitle)
                        .font(AppTypography.body)
                        .foregroundStyle(AppColors.textSecondary)
                }
            }
            Spacer(minLength: AppSpacing.sm)
            trailing()
        }
        .foregroundStyle(AppColors.textPrimary)
    }
}

struct AppDetailPanel<Content: View>: View {
    let title: String
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: AppSpacing.sm) {
            Text(title).font(.system(size: 14, weight: .semibold))
            Divider()
            content()
        }
        .padding(18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .deployCard(fill: AppTheme.contentSurface)
    }
}

struct AppInspectorPanel<Content: View>: View {
    let title: String
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: AppSpacing.sm) {
            Text(title).font(AppTypography.headline)
            content()
        }
        .padding(AppSpacing.md)
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .deployCard(fill: AppTheme.contentSurface)
    }
}

struct AppSettingsRow<Control: View>: View {
    let title: String
    let detail: String
    @ViewBuilder let control: () -> Control

    var body: some View {
        HStack(alignment: .center, spacing: AppSpacing.lg) {
            VStack(alignment: .leading, spacing: AppSpacing.xxs) {
                Text(title).font(AppTypography.headline)
                Text(detail).font(AppTypography.caption)
                    .foregroundStyle(AppColors.textSecondary)
            }
            Spacer(minLength: AppSpacing.md)
            control()
        }
        .padding(.vertical, AppSpacing.xs)
        .accessibilityElement(children: .contain)
    }
}

private struct AppPanelsPreview: View {
    @State private var enabled = true
    var body: some View {
        VStack(spacing: AppSpacing.md) {
            AppSectionHeader(title: "运行状态", subtitle: "部署详情") {
                AppButton(title: "刷新") {}
            }
            AppDetailPanel(title: "基本信息") {
                LabeledContent("项目", value: "orders-api")
                LabeledContent("服务器", value: "prod-01")
            }
            AppInspectorPanel(title: "版本信息") {
                Text("当前版本 v2.4.7")
            }
            AppSettingsRow(title: "登录后启动", detail: "登录 macOS 后自动打开应用") {
                Toggle("登录后启动", isOn: $enabled).labelsHidden()
            }
        }
        .padding(AppSpacing.xl)
        .frame(width: 500)
    }
}

struct AppPanelsPreviewProvider: PreviewProvider {
    static var previews: some View { AppPanelsPreview() }
}
