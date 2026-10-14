import AppKit
import SwiftUI

// The current workspace palette is owned by DeployStyle. These names let new
// preview components reuse that palette without changing existing pages.
enum AppColors {
    static let window = DeployStyle.canvas
    static let surface = DeployStyle.panel
    static let secondarySurface = DeployStyle.overviewPanel
    static let header = DeployStyle.overviewHeader
    static let sidebar = DeployStyle.nav
    static let textPrimary = DeployStyle.ink
    static let textSecondary = DeployStyle.muted
    static let separator = DeployStyle.line
    static let border = DeployStyle.line
    static let selection = DeployStyle.greenSoft
    static let hover = DeployStyle.greenSoft.opacity(0.45)
    static let focus = Color(nsColor: .keyboardFocusIndicatorColor)
    static let accent = DeployStyle.green
    static let success = DeployStyle.green
    static let warning = Color(nsColor: .systemOrange)
    static let danger = Color(nsColor: .systemRed)

    static let brand = DeployStyle.green
    static let brandBright = DeployStyle.greenBright
    static let brandSoft = DeployStyle.greenSoft
    static let brandSidebar = DeployStyle.nav
}
