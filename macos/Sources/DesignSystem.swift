import SwiftUI

enum DeployStyle {
    static let ink = Color(red: 0.10, green: 0.15, blue: 0.12)
    static let muted = Color(red: 0.45, green: 0.52, blue: 0.48)
    static let canvas = Color(red: 0.949, green: 0.961, blue: 0.953)
    static let panel = Color.white
    static let overviewPanel = Color(red: 0.976, green: 0.987, blue: 0.978)
    static let overviewHeader = Color(red: 0.930, green: 0.958, blue: 0.936)
    static let line = Color(red: 0.86, green: 0.90, blue: 0.87)
    static let green = Color(red: 0.16, green: 0.49, blue: 0.32)
    static let greenBright = Color(red: 0.22, green: 0.64, blue: 0.41)
    static let greenSoft = Color(red: 0.90, green: 0.96, blue: 0.92)
    static let nav = Color(red: 0.09, green: 0.14, blue: 0.11)

    static let cardShadow = Color(red: 0.11, green: 0.18, blue: 0.13).opacity(0.055)
}

struct SurfaceCard: ViewModifier {
    var radius: CGFloat = 14
    var fill: Color = DeployStyle.panel

    func body(content: Content) -> some View {
        content
            .background(fill, in: RoundedRectangle(cornerRadius: radius, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: radius, style: .continuous)
                    .strokeBorder(DeployStyle.line, lineWidth: 1)
            }
            .shadow(color: DeployStyle.cardShadow, radius: 22, y: 9)
    }
}

extension View {
    func deployCard(radius: CGFloat = 14, fill: Color = DeployStyle.panel) -> some View {
        modifier(SurfaceCard(radius: radius, fill: fill))
    }
}

struct DeployGroupBoxStyle: GroupBoxStyle {
    var fill: Color = DeployStyle.panel

    func makeBody(configuration: Configuration) -> some View {
        VStack(alignment: .leading, spacing: 11) {
            configuration.label
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(DeployStyle.ink)
                .frame(maxWidth: .infinity, alignment: .leading)
            configuration.content
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(18)
        .deployCard(fill: fill)
    }
}

struct DeployPrimaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 12, weight: .semibold))
            .foregroundStyle(.white)
            .padding(.horizontal, 15)
            .frame(height: 34)
            .background(configuration.isPressed ? DeployStyle.green.opacity(0.8) : DeployStyle.green,
                        in: RoundedRectangle(cornerRadius: 9, style: .continuous))
            .shadow(color: DeployStyle.green.opacity(configuration.isPressed ? 0.08 : 0.18), radius: 9, y: 4)
    }
}

struct DeploySecondaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 12, weight: .medium))
            .foregroundStyle(DeployStyle.green)
            .padding(.horizontal, 12)
            .frame(height: 32)
            .background(configuration.isPressed ? DeployStyle.greenSoft : .white,
                        in: RoundedRectangle(cornerRadius: 8, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 8, style: .continuous)
                    .strokeBorder(DeployStyle.line, lineWidth: 1)
            }
    }
}

struct WorkspaceListHeader: View {
    let title: String
    let count: Int

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(title)
                .font(.system(size: 16, weight: .bold))
                .foregroundStyle(DeployStyle.ink)
            Text("\(count) 个条目")
                .font(.caption)
                .foregroundStyle(DeployStyle.muted)
        }
    }
}
