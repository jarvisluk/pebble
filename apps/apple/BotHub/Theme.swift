import SwiftUI

// 颜色 Token 与 DESIGN.md / apps/mobile/src/theme.ts 保持一致。

extension Color {
    init(hex: String) {
        let (r, g, b) = rgb(hex)
        self.init(.sRGB, red: r / 255, green: g / 255, blue: b / 255, opacity: 1)
    }
}

private func rgb(_ hex: String) -> (Double, Double, Double) {
    var s = hex.trimmingCharacters(in: .whitespaces)
    if s.hasPrefix("#") { s.removeFirst() }
    if s.count == 3 { s = s.map { "\($0)\($0)" }.joined() }
    let v = UInt32(s, radix: 16) ?? 0
    return (Double((v >> 16) & 0xff), Double((v >> 8) & 0xff), Double(v & 0xff))
}

func mixHex(_ a: String, _ b: String, _ wa: Double) -> String {
    let x = rgb(a), y = rgb(b)
    let w = max(0, min(1, wa))
    func ch(_ p: Double, _ q: Double) -> String { String(format: "%02x", Int((p * w + q * (1 - w)).rounded())) }
    return "#\(ch(x.0, y.0))\(ch(x.1, y.1))\(ch(x.2, y.2))"
}

func contrastText(_ hex: String) -> Color {
    let c = rgb(hex)
    let lum = (0.2126 * c.0 + 0.7152 * c.1 + 0.0722 * c.2) / 255
    return Color(hex: lum > 0.62 ? "#1d1b19" : "#ffffff")
}

func fnv1a(_ s: String) -> UInt32 {
    var h: UInt32 = 2166136261
    for u in s.utf16 { h = (h ^ UInt32(u)) &* 16777619 }
    return h
}

let botColors = ["#f08a2c", "#8a7ad8", "#86a33a", "#b85a4a", "#7a7465", "#e0685a"]

func botColorHex(_ id: String, _ index: Int = -1) -> String {
    let h = fnv1a(id.isEmpty ? "?" : id)
    return botColors[index >= 0 ? index % botColors.count : Int(h % UInt32(botColors.count))]
}

/// 颜色全部沿用 DESIGN.md Token，原生控件只换皮不换结构。
struct Palette {
    static let defaultAccent = "#d9774b"
    static let accentPresets = ["#d9774b", "#3b82f6", "#10b981", "#8b5cf6", "#e11d48", "#f59e0b"]

    var dark: Bool
    var page, card, s1, s2, s3, text, text2, mute: Color
    var accent, accentSoft, sel: Color
    var pink, pinkInk, lilac, lilacInk, sage, sageInk, busy, danger, eye: Color

    init(dark: Bool, accent: String) {
        self.dark = dark
        self.accent = Color(hex: accent)
        page = Color(hex: dark ? "#000000" : "#efece8")
        card = Color(hex: dark ? "#171717" : "#ffffff")
        s1 = Color(hex: dark ? "#232323" : "#f5f3f0")
        s2 = Color(hex: dark ? "#303030" : "#e9e5e0")
        s3 = Color(hex: dark ? "#404040" : "#dcd7d0")
        text = Color(hex: dark ? "#f3f1ee" : "#1d1b19")
        text2 = Color(hex: dark ? "#ccc7c1" : "#57524d")
        mute = Color(hex: dark ? "#948e88" : "#9b958f")
        accentSoft = Color(hex: dark ? "#2a2a2a" : mixHex(accent, "#ffffff", 0.16))
        sel = Color(hex: dark ? "#262626" : mixHex(accent, "#ffffff", 0.10))
        pink = Color(hex: dark ? "#2a1a18" : "#f7d9d3")
        pinkInk = Color(hex: dark ? "#f0a59b" : "#b2443a")
        lilac = Color(hex: dark ? "#24203a" : "#e0dbfb")
        lilacInk = Color(hex: dark ? "#b7aaf5" : "#6a55c9")
        sage = Color(hex: dark ? "#1c2214" : "#e2e9cc")
        sageInk = Color(hex: dark ? "#a9c46a" : "#6b8a2a")
        busy = Color(hex: "#f08a2c")
        danger = Color(hex: dark ? "#f0605a" : "#d9483b")
        eye = Color(hex: "#1c1917")
    }

    /// 当前 Bot 局部强调色（仅 Bot 下属页面使用）。
    func bot(_ id: String, _ index: Int) -> (accent: Color, text: Color, soft: Color) {
        let hex = botColorHex(id, index)
        return (Color(hex: hex), contrastText(hex), Color(hex: dark ? "#2a2a2a" : mixHex(hex, "#ffffff", 0.16)))
    }
}

private struct PaletteKey: EnvironmentKey {
    static let defaultValue = Palette(dark: false, accent: Palette.defaultAccent)
}

extension EnvironmentValues {
    var palette: Palette {
        get { self[PaletteKey.self] }
        set { self[PaletteKey.self] = newValue }
    }
}

// MARK: 头像：blob 加两只眼睛，无底色圆。

struct BotAvatar: View {
    @Environment(\.palette) private var p
    var id: String
    var size: CGFloat
    var index: Int = -1
    var busy = false
    @State private var pulse = false

    var body: some View {
        let h = fnv1a(id.isEmpty ? "?" : id)
        let tilt = Double(Int((h >> 4) % 25) - 12)
        let gx = CGFloat(Int((h >> 9) % 7) - 3)
        let gy = CGFloat(Int((h >> 12) % 5) - 2)
        let lean = (h >> 15) % 2 == 1 ? -14.0 : 12.0
        let blob = Color(hex: botColorHex(id, index))
        Canvas { ctx, sz in
            let k = sz.width / 100
            ctx.translateBy(x: 50 * k, y: 50 * k)
            ctx.rotate(by: .degrees(tilt))
            ctx.translateBy(x: -50 * k, y: -50 * k)
            var path = Path()
            path.move(to: CGPoint(x: 52 * k, y: 9 * k))
            path.addCurve(to: CGPoint(x: 93 * k, y: 55 * k), control1: CGPoint(x: 80 * k, y: 9 * k), control2: CGPoint(x: 95 * k, y: 28 * k))
            path.addCurve(to: CGPoint(x: 47 * k, y: 91 * k), control1: CGPoint(x: 91 * k, y: 81 * k), control2: CGPoint(x: 72 * k, y: 93 * k))
            path.addCurve(to: CGPoint(x: 7 * k, y: 46 * k), control1: CGPoint(x: 21 * k, y: 89 * k), control2: CGPoint(x: 5 * k, y: 72 * k))
            path.addCurve(to: CGPoint(x: 52 * k, y: 9 * k), control1: CGPoint(x: 9 * k, y: 22 * k), control2: CGPoint(x: 26 * k, y: 9 * k))
            path.closeSubpath()
            ctx.fill(path, with: .color(blob))
            for (cx, cy) in [(39 + gx, 45 + gy), (62 + gx, 43 + gy)] {
                var e = ctx
                e.translateBy(x: cx * k, y: cy * k)
                e.rotate(by: .degrees(lean))
                e.fill(Path(ellipseIn: CGRect(x: -6 * k, y: -10.5 * k, width: 12 * k, height: 21 * k)), with: .color(p.eye))
            }
        }
        .frame(width: size, height: size)
        .overlay(alignment: .bottomTrailing) {
            if busy {
                let d = max(9, (size * 0.24).rounded())
                Circle().fill(p.busy)
                    .frame(width: d, height: d)
                    .overlay(Circle().stroke(p.page, lineWidth: 2))
                    .opacity(pulse ? 0.65 : 1)
                    .onAppear { withAnimation(.easeInOut(duration: 0.6).repeatForever()) { pulse = true } }
            }
        }
        .accessibilityHidden(true)
    }
}

// MARK: 通用小件

struct PressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label.scaleEffect(configuration.isPressed ? 0.97 : 1)
            .animation(.easeOut(duration: 0.15), value: configuration.isPressed)
    }
}

struct Tag: View {
    var text: String
    var bg: Color
    var fg: Color
    var body: some View {
        Text(text)
            .font(.system(size: 13, weight: .semibold))
            .foregroundStyle(fg)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .background(bg, in: Capsule())
    }
}

/// 表单内的错误 / 成功提示。
struct StatusText: View {
    @Environment(\.palette) private var p
    var text: String
    var error = true
    var body: some View {
        Label(text, systemImage: error ? "exclamationmark.triangle.fill" : "checkmark.circle.fill")
            .font(.system(size: 15))
            .foregroundStyle(error ? p.danger : p.sageInk)
    }
}

extension View {
    /// macOS 表单用分组样式，与 iOS 观感一致。
    /// 原生 Form/List 套 DESIGN 页面底色与卡片圆角。
    func hubForm() -> some View { modifier(HubSurface(form: true)) }
    func hubList(sidebar: Bool = false) -> some View { modifier(HubSurface(form: false, sidebar: sidebar)) }

    func monoInput() -> some View {
        self.font(.system(size: 15, design: .monospaced))
            .autocorrectionDisabled()
            .textInputAutocapitalization(.never)
    }
}

/// 简单的换行流式布局。
struct FlowLayout: Layout {
    var spacing: CGFloat = 8

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let maxW = proposal.width ?? .infinity
        var x: CGFloat = 0, y: CGFloat = 0, rowH: CGFloat = 0, width: CGFloat = 0
        for v in subviews {
            let s = v.sizeThatFits(.unspecified)
            if x > 0, x + s.width > maxW { x = 0; y += rowH + spacing; rowH = 0 }
            x += s.width + spacing
            rowH = max(rowH, s.height)
            width = max(width, x - spacing)
        }
        return CGSize(width: proposal.width ?? width, height: y + rowH)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX, y = bounds.minY, rowH: CGFloat = 0
        for v in subviews {
            let s = v.sizeThatFits(.unspecified)
            if x > bounds.minX, x + s.width > bounds.maxX { x = bounds.minX; y += rowH + spacing; rowH = 0 }
            v.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(s))
            x += s.width + spacing
            rowH = max(rowH, s.height)
        }
    }
}

func copyToClipboard(_ s: String) {
    UIPasteboard.general.string = s
}

struct HubSurface: ViewModifier {
    @Environment(\.palette) private var p
    var form: Bool
    var sidebar = false

    func body(content: Content) -> some View {
        content
            .scrollContentBackground(.hidden)
            .background(sidebar ? p.card : p.page)
            .font(.system(size: 16))
    }
}

