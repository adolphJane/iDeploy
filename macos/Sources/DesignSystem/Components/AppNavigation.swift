import SwiftUI

struct AppObjectIcon: View {
    let systemName: String
    var selected = false

    var body: some View {
        Image(systemName: systemName)
            .font(.system(size: 15, weight: .medium))
            .foregroundStyle(AppColors.brand)
            .frame(width: 30, height: 30)
            .background(AppTheme.iconBackground(for: selected),
                        in: RoundedRectangle(cornerRadius: AppRadius.small))
            .accessibilityHidden(true)
    }
}

// An optional classification label for a real field. The current deployment
// model has no production/staging property, so pages must not invent one.
struct AppEnvironmentBadge: View {
    let name: String
    var accessibilityPrefix = "标签"

    var body: some View {
        Text(name)
            .font(AppTypography.caption.weight(.semibold))
            .foregroundStyle(AppColors.brand)
            .padding(.horizontal, AppSpacing.xs)
            .padding(.vertical, AppSpacing.xxs)
            .background(AppColors.brandSoft, in: RoundedRectangle(cornerRadius: AppRadius.small))
            .accessibilityLabel("\(accessibilityPrefix)：\(name)")
    }
}

struct AppStatusBadge: View {
    enum Tone { case success, warning, danger, neutral }
    let title: String
    var tone: Tone = .neutral

    private var toneColor: Color {
        switch tone {
        case .success: AppColors.success
        case .warning: AppColors.warning
        case .danger: AppColors.danger
        case .neutral: AppColors.textSecondary
        }
    }

    var body: some View {
        Text(title)
            .font(AppTypography.caption.weight(.semibold))
            .foregroundStyle(toneColor)
            .padding(.horizontal, 9)
            .padding(.vertical, 5)
            .background(toneColor.opacity(0.12), in: Capsule())
            .accessibilityLabel(title)
    }
}

struct AppSidebarRow: View {
    let title: String
    let systemImage: String
    let selected: Bool
    let action: () -> Void
    @State private var hovering = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        Button(action: action) {
            HStack(spacing: AppSpacing.sm) {
                Image(systemName: systemImage)
                    .font(.system(size: 15, weight: .medium))
                    .frame(width: 19)
                Text(title)
                    .font(.system(size: 13, weight: selected ? .semibold : .medium))
                Spacer(minLength: 0)
                if selected {
                    Capsule().fill(AppColors.brandBright).frame(width: 3, height: 17)
                }
            }
            .foregroundStyle(selected ? Color.white.opacity(0.94) : Color.white.opacity(0.64))
            .padding(.horizontal, AppSpacing.sm)
            .frame(height: 42)
            .background(selected ? AppColors.brandBright.opacity(0.18) : hovering ? Color.white.opacity(0.06) : .clear,
                        in: RoundedRectangle(cornerRadius: AppRadius.medium))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .animation(AppMotion.selection(reduceMotion: reduceMotion), value: hovering)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

struct AppListRow: View {
    let title: String
    let subtitle: String
    let systemImage: String
    var environment: String? = nil
    var status: String? = nil
    let selected: Bool
    let action: () -> Void
    @State private var hovering = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        Button(action: action) {
            HStack(spacing: 11) {
                AppObjectIcon(systemName: systemImage, selected: selected)
                VStack(alignment: .leading, spacing: 5) {
                    Text(title)
                        .font(AppTypography.headline)
                        .foregroundStyle(AppColors.textPrimary)
                        .lineLimit(1)
                    HStack(spacing: AppSpacing.xs) {
                        if let environment { AppEnvironmentBadge(name: environment) }
                        Text(subtitle)
                            .font(AppTypography.caption)
                            .foregroundStyle(AppColors.textSecondary)
                            .lineLimit(1)
                    }
                }
                Spacer(minLength: 0)
                if let status { AppStatusBadge(title: status, tone: .success) }
                if selected {
                    Capsule().fill(AppColors.brandBright).frame(width: 3, height: 20)
                }
            }
            .padding(.horizontal, AppSpacing.rowInset)
            .frame(maxWidth: .infinity, minHeight: 56, alignment: .leading)
            .background(selected ? AppTheme.selectedRowBackground : hovering ? AppColors.hover : .clear,
                        in: RoundedRectangle(cornerRadius: AppRadius.medium))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .animation(AppMotion.selection(reduceMotion: reduceMotion), value: hovering)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([title, environment, subtitle, status].compactMap { $0 }.joined(separator: "，"))
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

private struct AppNavigationPreview: View {
    var body: some View {
        HStack(alignment: .top, spacing: AppSpacing.md) {
            VStack(spacing: AppSpacing.xxs) {
                AppSidebarRow(title: "部署", systemImage: "shippingbox", selected: true) {}
                AppSidebarRow(title: "服务器", systemImage: "server.rack", selected: false) {}
            }
            .padding(AppSpacing.sm)
            .frame(width: 190)
            .background(AppColors.sidebar)
            VStack(spacing: AppSpacing.xxs) {
                AppListRow(title: "示例部署", subtitle: "服务器 · 工作负载", systemImage: "shippingbox", selected: true) {}
                AppListRow(title: "另一个部署", subtitle: "服务器 · 工作负载", systemImage: "shippingbox", selected: false) {}
            }
            .padding(AppSpacing.sm)
            .frame(width: 300)
            .background(AppColors.window)
        }
    }
}

struct AppNavigationPreviewProvider: PreviewProvider {
    static var previews: some View { AppNavigationPreview() }
}
