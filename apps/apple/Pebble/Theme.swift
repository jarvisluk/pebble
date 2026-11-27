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
    return Color(hex: lum > 0.62 ? "#171717" : "#ffffff")
}

func fnv1a(_ s: String) -> UInt32 {
    var h: UInt32 = 2166136261
    for u in s.utf16 { h = (h ^ UInt32(u)) &* 16777619 }
    return h
}

let pebbleColor = "#7aaa98"
let botColors = ["#d9774b", "#e0a23a", "#7fa14a", pebbleColor, "#5b8bb5", "#9a7bc0", "#c4605e"]

func normalizeBotHex(_ s: String?) -> String? {
    let clean = (s ?? "").trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    guard clean.count == 7, clean.hasPrefix("#") else { return nil }
    let hexDigits = CharacterSet(charactersIn: "0123456789abcdef")
    guard clean.dropFirst().unicodeScalars.allSatisfy({ hexDigits.contains($0) }) else { return nil }
    return clean
}

func colorToHex(_ c: Color) -> String? {
    platformColorToHex(c)
}

func botColorHex(_ id: String, _ index: Int = -1, custom: String? = nil) -> String {
    if let c = normalizeBotHex(custom) { return c }
    let h = fnv1a(id.isEmpty ? "?" : id)
    return botColors[index >= 0 ? index % botColors.count : Int(h % UInt32(botColors.count))]
}

/// 颜色全部沿用 DESIGN.md Token，原生控件只换皮不换结构。
struct Palette {
    var dark: Bool
    var page, card, s1, s2, s3, text, text2, mute: Color
    var accent, accentSoft, sel: Color
    var pink, pinkInk, lilac, lilacInk, sage, sageInk, busy, danger, eye: Color

    init(dark: Bool) {
        self.dark = dark
        self.accent = Color(hex: dark ? "#f3f1ee" : "#171717")
        page = Color(hex: dark ? "#000000" : "#ffffff")
        card = Color(hex: dark ? "#171717" : "#f6f6f7")
        s1 = Color(hex: dark ? "#232323" : "#ededef")
        s2 = Color(hex: dark ? "#303030" : "#e2e2e7")
        s3 = Color(hex: dark ? "#404040" : "#d1d1d8")
        text = Color(hex: dark ? "#f3f1ee" : "#171717")
        text2 = Color(hex: dark ? "#ccc7c1" : "#52525b")
        mute = Color(hex: dark ? "#948e88" : "#8e8e98")
        accentSoft = Color(hex: dark ? "#2a2a2a" : "#e6e6eb")
        sel = Color(hex: dark ? "#262626" : "#e6e6eb")
        pink = Color(hex: dark ? "#2c1616" : "#feecec")
        pinkInk = Color(hex: dark ? "#ff6961" : "#dc2626")
        lilac = Color(hex: dark ? "#24203a" : "#ebe7fe")
        lilacInk = Color(hex: dark ? "#b7aaf5" : "#6a55c9")
        sage = Color(hex: dark ? "#14261c" : "#e6f6ec")
        sageInk = Color(hex: dark ? "#30d158" : "#16a34a")
        busy = Color(hex: dark ? "#ff9f0a" : "#f59e0b")
        danger = Color(hex: dark ? "#ff453a" : "#dc2626")
        eye = Color(hex: "#1c1917")
    }

    /// 当前 Bot 局部强调色（仅 Bot 下属页面使用）。
    func bot(_ id: String, _ index: Int, custom: String? = nil) -> (accent: Color, text: Color, soft: Color) {
        let hex = botColorHex(id, index, custom: custom)
        return (Color(hex: hex), contrastText(hex), Color(hex: dark ? "#2a2a2a" : mixHex(hex, "#ffffff", 0.14)))
    }

    func botCollab(_ id: String, _ index: Int, custom: String? = nil) -> (bg: Color, ink: Color) {
        let hex = botColorHex(id, index, custom: custom)
        return (
            bg: Color(hex: dark ? mixHex(hex, "#171717", 0.28) : mixHex(hex, "#ffffff", 0.16)),
            ink: Color(hex: dark ? mixHex(hex, "#ffffff", 0.72) : mixHex(hex, "#171717", 0.62))
        )
    }
}

private struct PaletteKey: EnvironmentKey {
    static let defaultValue = Palette(dark: false)
}

extension EnvironmentValues {
    var palette: Palette {
        get { self[PaletteKey.self] }
        set { self[PaletteKey.self] = newValue }
    }
}
