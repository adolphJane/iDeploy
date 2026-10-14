import SwiftUI

// Sizes document the current desktop UI. Existing pages remain the source of
// truth until a separate, approved accessibility migration updates them.
enum AppTypography {
    static let largeTitle: Font = .system(size: 26, weight: .bold)
    static let title: Font = .system(size: 18, weight: .bold)
    static let sectionTitle: Font = .system(size: 16, weight: .bold)
    static let headline: Font = .system(size: 12, weight: .semibold)
    static let body: Font = .system(size: 12)
    static let secondaryBody: Font = .system(size: 11)
    static let caption: Font = .system(size: 10)
    static let label: Font = .system(size: 12, weight: .medium)
    static let monospaced: Font = .system(size: 11, design: .monospaced)
}
