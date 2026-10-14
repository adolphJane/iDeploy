import SwiftUI

enum AppShadows {
    static let cardColor = DeployStyle.cardShadow
    static let cardRadius: CGFloat = 22
    static let cardYOffset: CGFloat = 9

    // Kept for preview compatibility; new cards should use deployCard().
    static let floatingColor = cardColor
    static let floatingRadius = cardRadius
    static let floatingYOffset = cardYOffset
}
