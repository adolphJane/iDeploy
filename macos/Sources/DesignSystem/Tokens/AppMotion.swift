import SwiftUI

enum AppMotion {
    static func selection(reduceMotion: Bool) -> Animation? {
        reduceMotion ? nil : .easeOut(duration: 0.16)
    }

    static func feedback(reduceMotion: Bool) -> Animation? {
        reduceMotion ? nil : .easeInOut(duration: 0.20)
    }
}
