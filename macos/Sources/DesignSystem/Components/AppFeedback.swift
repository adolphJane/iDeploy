import SwiftUI

struct AppEmptyState: View {
    let title: String
    let detail: String
    let systemImage: String

    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: systemImage)
                .font(.system(size: 26, weight: .light))
                .foregroundStyle(AppColors.brand)
                .frame(width: 58, height: 58)
                .background(AppColors.brandSoft, in: RoundedRectangle(cornerRadius: 17))
                .accessibilityHidden(true)
            Text(title).font(.headline).foregroundStyle(AppColors.textPrimary)
            Text(detail).font(.subheadline)
                .foregroundStyle(AppColors.textSecondary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, minHeight: 250)
        .deployCard()
    }
}

struct AppErrorState: View {
    let title: String
    let detail: String
    var retry: (() -> Void)? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: AppSpacing.xs) {
            Label(title, systemImage: "exclamationmark.triangle")
                .font(AppTypography.headline)
                .foregroundStyle(AppColors.danger)
            Text(detail).font(AppTypography.secondaryBody)
                .foregroundStyle(AppColors.textSecondary)
                .textSelection(.enabled)
            if let retry { AppButton(title: "重试", action: retry) }
        }
        .padding(AppSpacing.md)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(AppColors.danger.opacity(0.08),
                    in: RoundedRectangle(cornerRadius: AppRadius.medium))
    }
}

struct AppLoadingView: View {
    let title: String

    var body: some View {
        HStack(spacing: AppSpacing.sm) {
            ProgressView().controlSize(.small)
            Text(title).font(AppTypography.secondaryBody)
        }
        .foregroundStyle(AppColors.textSecondary)
        .accessibilityElement(children: .combine)
    }
}

struct AppProgressView: View {
    let title: String
    let fraction: Double

    var body: some View {
        VStack(alignment: .leading, spacing: AppSpacing.xs) {
            HStack {
                Text(title).font(AppTypography.headline)
                Spacer()
                Text("\(Int(max(0, min(1, fraction)) * 100))%")
                    .font(AppTypography.caption)
                    .foregroundStyle(AppColors.textSecondary)
            }
            ProgressView(value: max(0, min(1, fraction)))
                .tint(AppColors.brand)
        }
        .accessibilityElement(children: .combine)
    }
}

private struct AppFeedbackPreview: View {
    var body: some View {
        VStack(spacing: AppSpacing.md) {
            AppEmptyState(title: "还没有部署", detail: "添加部署后可在这里查看运行状态。", systemImage: "shippingbox")
            AppLoadingView(title: "正在读取服务器状态…")
            AppProgressView(title: "发布中", fraction: 0.48)
            AppErrorState(title: "连接失败", detail: "请检查服务器连接后重试。") {}
        }
        .padding(AppSpacing.xl)
        .frame(width: 480)
    }
}

struct AppFeedbackPreviewProvider: PreviewProvider {
    static var previews: some View { AppFeedbackPreview() }
}
