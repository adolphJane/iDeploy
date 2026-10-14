import SwiftUI

struct AppButton: View {
    enum Emphasis { case primary, secondary }

    let title: String
    var systemImage: String? = nil
    var emphasis: Emphasis = .secondary
    let action: () -> Void

    @ViewBuilder var body: some View {
        if emphasis == .primary {
            Button(action: action) { label }
                .buttonStyle(DeployPrimaryButtonStyle())
                .accessibilityLabel(title)
        } else {
            Button(action: action) { label }
                .buttonStyle(DeploySecondaryButtonStyle())
                .accessibilityLabel(title)
        }
    }

    @ViewBuilder private var label: some View {
            if let systemImage {
                Label(title, systemImage: systemImage)
            } else {
                Text(title)
            }
    }
}

struct AppToolbarButton: View {
    let title: String
    let systemImage: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Image(systemName: systemImage)
        }
        .buttonStyle(DeploySecondaryButtonStyle())
        .help(title)
        .accessibilityLabel(title)
    }
}

struct AppTextField: View {
    let title: String
    @Binding var text: String
    var helpText: String? = nil
    var error: String? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: AppSpacing.xxs) {
            TextField(title, text: $text)
                .textFieldStyle(.roundedBorder)
            if let error, !error.isEmpty {
                Text(error)
                    .font(AppTypography.caption)
                    .foregroundStyle(AppColors.danger)
                    .accessibilityLabel("\(title)：\(error)")
            } else if let helpText, !helpText.isEmpty {
                Text(helpText)
                    .font(AppTypography.caption)
                    .foregroundStyle(AppColors.textSecondary)
            }
        }
    }
}

struct AppSearchField: View {
    @Binding var text: String
    var prompt: String = "搜索"

    var body: some View {
        HStack(spacing: AppSpacing.xs) {
            Image(systemName: "magnifyingglass")
                .foregroundStyle(AppColors.textSecondary)
                .accessibilityHidden(true)
            TextField(prompt, text: $text)
                .textFieldStyle(.plain)
                .accessibilityLabel(prompt)
        }
        .padding(.horizontal, AppSpacing.sm)
        .padding(.vertical, AppSpacing.xs)
        .background(AppColors.surface, in: RoundedRectangle(cornerRadius: AppRadius.medium))
        .overlay {
            RoundedRectangle(cornerRadius: AppRadius.medium)
                .strokeBorder(AppColors.separator, lineWidth: 1)
        }
    }
}

private struct AppControlsPreview: View {
    @State private var name = "orders-api"
    @State private var query = ""

    var body: some View {
        VStack(alignment: .leading, spacing: AppSpacing.md) {
            HStack {
                AppButton(title: "发布新版本", systemImage: "shippingbox", emphasis: .primary) {}
                AppButton(title: "刷新") {}
                AppToolbarButton(title: "更多操作", systemImage: "ellipsis") {}
            }
            AppTextField(title: "部署名称", text: $name, helpText: "在列表中显示")
            AppSearchField(text: $query, prompt: "搜索部署")
        }
        .padding(AppSpacing.xl)
        .frame(width: 420)
    }
}

struct AppControlsPreviewProvider: PreviewProvider {
    static var previews: some View { AppControlsPreview() }
}
