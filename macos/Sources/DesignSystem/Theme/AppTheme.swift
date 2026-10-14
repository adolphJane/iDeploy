import SwiftUI

enum AppTheme {
    static let primaryAction = AppColors.brand
    static let contentBackground = AppColors.window
    static let contentSurface = AppColors.secondarySurface
    static let navigationBackground = AppColors.brandSidebar
    static let selectedRowBackground = AppColors.selection

    static func iconBackground(for selected: Bool) -> Color {
        selected ? AppColors.brandSoft : AppColors.brandSoft.opacity(0.78)
    }
}
