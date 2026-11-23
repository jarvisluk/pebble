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
let botAvatars = ["blob", "squircle", "cat", "bear", "cloud", "ghost", "star", "sprout"]

func normalizeBotHex(_ s: String?) -> String? {
    let clean = (s ?? "").trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    guard clean.count == 7, clean.hasPrefix("#") else { return nil }
    let hexDigits = CharacterSet(charactersIn: "0123456789abcdef")
    guard clean.dropFirst().unicodeScalars.allSatisfy({ hexDigits.contains($0) }) else { return nil }
    return clean
}

func colorToHex(_ c: Color) -> String? {
    var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0
    guard UIColor(c).getRed(&r, green: &g, blue: &b, alpha: &a) else { return nil }
    let ri = max(0, min(255, Int((r * 255).rounded())))
    let gi = max(0, min(255, Int((g * 255).rounded())))
    let bi = max(0, min(255, Int((b * 255).rounded())))
    return String(format: "#%02x%02x%02x", ri, gi, bi)
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
        self.accent = Color(hex: dark ? "#f3f1ee" : "#1d1b19")
        page = Color(hex: dark ? "#000000" : "#efece8")
        card = Color(hex: dark ? "#171717" : "#ffffff")
        s1 = Color(hex: dark ? "#232323" : "#f5f3f0")
        s2 = Color(hex: dark ? "#303030" : "#e9e5e0")
        s3 = Color(hex: dark ? "#404040" : "#dcd7d0")
        text = Color(hex: dark ? "#f3f1ee" : "#1d1b19")
        text2 = Color(hex: dark ? "#ccc7c1" : "#57524d")
        mute = Color(hex: dark ? "#948e88" : "#9b958f")
        accentSoft = Color(hex: dark ? "#2a2a2a" : "#e9e5e0")
        sel = Color(hex: dark ? "#262626" : "#eeebe6")
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
    func bot(_ id: String, _ index: Int, custom: String? = nil) -> (accent: Color, text: Color, soft: Color) {
        let hex = botColorHex(id, index, custom: custom)
        return (Color(hex: hex), contrastText(hex), Color(hex: dark ? "#2a2a2a" : mixHex(hex, "#ffffff", 0.16)))
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

// MARK: 头像：Pebble C 参数化动态头像（默认）+ 7 种自定义矢量造型，无底色圆。

private struct Mulberry32 {
    private var a: UInt32
    init(seed: UInt32) { self.a = seed }
    mutating func next() -> Double {
        a = a &+ 0x6d2b79f5
        var t = a
        t = (t ^ (t >> 15)) &* (t | 1)
        t ^= t &+ ((t ^ (t >> 7)) &* (t | 61))
        let out = t ^ (t >> 14)
        return Double(out) / 4294967296.0
    }
}

private struct PebbleVert {
    var s: Double
    var d: Double
    var p: Double
}

private struct PebbleParams {
    var n: Int
    var verts: [PebbleVert]
    var rot0: Double
    var tilt: Double
    var sx: Double
    var corner: Double
    var side: Double
    var breath: Double
    var blinkEvery: Double
    var blinkOff: Double
    var lookEvery: Double
    var lookOff: Double
    var lookSeed: UInt32
    var bob: Double
}

private func pebbleLerp(_ a: Double, _ b: Double, _ t: Double) -> Double { a + (b - a) * t }

private func pebbleParams(for id: String) -> PebbleParams {
    let key = id.isEmpty ? "?" : id
    var r = Mulberry32(seed: fnv1a(key))
    let q = r.next()
    let n: Int = q < 0.6 ? 4 : (q < 0.85 ? 5 : 6)
    var verts: [PebbleVert] = []
    verts.reserveCapacity(n)
    for _ in 0..<n {
        verts.append(PebbleVert(
            s: 0.86 + r.next() * 0.24,
            d: (r.next() - 0.5) * 0.35,
            p: r.next() * 6.28
        ))
    }
    _ = r.next() // rr0
    let sign: Double = r.next() < 0.5 ? -1 : 1
    let tilt = sign * pebbleLerp(6, 12, r.next()) / 57.3
    _ = r.next() // sideR
    return PebbleParams(
        n: n,
        verts: verts,
        rot0: .pi / 2 - .pi / Double(n),
        tilt: tilt,
        sx: pebbleLerp(0.95, 1.08, r.next()),
        corner: pebbleLerp(0.36, 0.44, r.next()),
        side: -sign,
        breath: 0.5 + r.next() * 0.4,
        blinkEvery: 2.8 + r.next() * 3.5,
        blinkOff: r.next() * 10,
        lookEvery: 2.2 + r.next() * 3,
        lookOff: r.next() * 10,
        lookSeed: fnv1a(key + "#l"),
        bob: r.next() * 6.28
    )
}

private func drawPebble(in ctx: inout GraphicsContext, size: CGSize, params P: PebbleParams, color: Color, eye: Color, t: Double, busy: Bool) {
    let k = size.width / 100.0
    let circMap: [Int: Double] = [3: 54, 4: 49, 5: 46, 6: 45]
    let circ = circMap[P.n] ?? 49
    let breathe = 1.0 + 0.018 * sin(t * P.breath * .pi * (busy ? 2.6 : 1.0))
    let bobY = sin(t * 1.2 + P.bob) * (busy ? 1.2 : 0.6)
    let asym = 0.35

    var pts: [CGPoint] = []
    pts.reserveCapacity(P.n)
    var minX = Double.infinity, maxX = -Double.infinity
    var minY = Double.infinity, maxY = -Double.infinity
    for i in 0..<P.n {
        let v = P.verts[i]
        let a = P.rot0 + (Double(i) / Double(P.n)) * .pi * 2.0 + v.d * asym * 0.5
        let rr = circ * (1.0 + (v.s - 1.0) * asym) * breathe
        let px = cos(a) * rr * P.sx
        let py = sin(a) * rr / P.sx
        minX = min(minX, px); maxX = max(maxX, px)
        minY = min(minY, py); maxY = max(maxY, py)
        pts.append(CGPoint(x: px, y: py))
    }
    let ox = 50.0 - (minX + maxX) / 2.0
    let oy = 50.0 + bobY - (minY + maxY) / 2.0
    for i in 0..<P.n {
        pts[i] = CGPoint(x: (pts[i].x + ox) * k, y: (pts[i].y + oy) * k)
    }

    let c = CGFloat(min(0.5, P.corner))
    func cut(_ a: CGPoint, _ b: CGPoint) -> CGPoint {
        CGPoint(x: a.x + (b.x - a.x) * c, y: a.y + (b.y - a.y) * c)
    }
    var bodyPath = Path()
    for i in 0..<P.n {
        let prev = pts[(i - 1 + P.n) % P.n]
        let curr = pts[i]
        let next = pts[(i + 1) % P.n]
        let p1 = cut(curr, prev)
        let p2 = cut(curr, next)
        if i == 0 {
            bodyPath.move(to: p1)
        } else {
            bodyPath.addLine(to: p1)
        }
        bodyPath.addQuadCurve(to: p2, control: curr)
    }
    bodyPath.closeSubpath()

    let bodyRotDeg = P.tilt * 57.3 + sin(t * 0.6 + P.bob) * (busy ? 3.0 : 1.5)
    var bodyCtx = ctx
    bodyCtx.translateBy(x: 50 * k, y: 50 * k)
    bodyCtx.rotate(by: .degrees(bodyRotDeg))
    bodyCtx.translateBy(x: -50 * k, y: -50 * k)
    bodyCtx.fill(bodyPath, with: .color(color))

    let bp = (t + P.blinkOff).truncatingRemainder(dividingBy: P.blinkEvery) / P.blinkEvery
    let blink = bp > 0.965 ? max(0.12, abs(cos((bp - 0.965) / 0.035 * .pi))) : 1.0

    var lx: Double
    var ly: Double
    if busy {
        lx = sin(t * 4.5) * 7.0
        ly = -1.0
    } else {
        let seg = (t + P.lookOff) / P.lookEvery
        let idx = Int(floor(seg))
        let f = seg - Double(idx)
        func target(_ i: Int) -> (Double, Double) {
            let seed = P.lookSeed ^ UInt32(truncatingIfNeeded: (i &+ 1) &* 2654435761)
            var q = Mulberry32(seed: seed)
            let r1 = q.next()
            let r2 = q.next()
            let r3 = q.next()
            return (P.side * (3.0 + r1 * 6.0) - (r2 < 0.2 ? P.side * 8.0 : 0.0), -2.0 + r3 * 4.0)
        }
        let a = target(idx)
        let b = target(idx + 1)
        let e = f < 0.85 ? 0.0 : (1.0 - cos((f - 0.85) / 0.15 * .pi)) / 2.0
        lx = pebbleLerp(a.0, b.0, e)
        ly = pebbleLerp(a.1, b.1, e)
    }

    let ew = 5.0
    let eh = 9.5 * max(0.12, blink)
    let gap = 19.0
    let lean = -P.side * 14.0
    let ex = 50.0 + lx
    let ey = 47.0 + bobY + ly
    let rx = min(ew, eh)

    for cx in [ex - gap / 2.0, ex + gap / 2.0] {
        var eCtx = ctx
        eCtx.translateBy(x: cx * k, y: ey * k)
        eCtx.rotate(by: .degrees(lean))
        let rect = CGRect(x: -ew * k, y: -eh * k, width: ew * 2.0 * k, height: eh * 2.0 * k)
        eCtx.fill(Path(roundedRect: rect, cornerRadius: rx * k), with: .color(eye))
    }
}

struct BotAvatar: View {
    @Environment(\.palette) private var p
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var id: String
    var size: CGFloat
    var index: Int = -1
    var avatar: String? = nil
    var color: String? = nil
    var busy = false
    @State private var pulse = false
    @State private var startTime = Date()

    var body: some View {
        let shape = (avatar ?? "").trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        let kind = shape.isEmpty ? "blob" : shape
        let blob = Color(hex: botColorHex(id, index, custom: color))
        let isDefaultBlob = !["squircle", "cat", "bear", "cloud", "ghost", "star", "sprout"].contains(kind)
        Group {
            if isDefaultBlob {
                let params = pebbleParams(for: id)
                TimelineView(.animation(minimumInterval: 1.0 / 30.0, paused: reduceMotion)) { timeline in
                    let t = reduceMotion ? 0.0 : max(0.0, timeline.date.timeIntervalSince(startTime))
                    Canvas { ctx, sz in
                        drawPebble(in: &ctx, size: sz, params: params, color: blob, eye: p.eye, t: t, busy: busy)
                    }
                }
            } else {
                Canvas { ctx, sz in
                    let k = sz.width / 100
                    func drawEye(cx: CGFloat, cy: CGFloat, rx: CGFloat, ry: CGFloat, rot: Double = 0) {
                        var e = ctx
                        e.translateBy(x: cx * k, y: cy * k)
                        if rot != 0 { e.rotate(by: .degrees(rot)) }
                        e.fill(Path(ellipseIn: CGRect(x: -rx * k, y: -ry * k, width: rx * 2 * k, height: ry * 2 * k)), with: .color(p.eye))
                    }
                    func pt(_ x: CGFloat, _ y: CGFloat) -> CGPoint { CGPoint(x: x * k, y: y * k) }

                    switch kind {
                    case "squircle":
                var path = Path()
                path.move(to: pt(50, 10))
                path.addCurve(to: pt(90, 50), control1: pt(78, 10), control2: pt(90, 22))
                path.addCurve(to: pt(50, 90), control1: pt(90, 78), control2: pt(78, 90))
                path.addCurve(to: pt(10, 50), control1: pt(22, 90), control2: pt(10, 78))
                path.addCurve(to: pt(50, 10), control1: pt(10, 22), control2: pt(22, 10))
                path.closeSubpath()
                ctx.fill(path, with: .color(blob))
                drawEye(cx: 38, cy: 48, rx: 5.5, ry: 9.5)
                drawEye(cx: 62, cy: 48, rx: 5.5, ry: 9.5)
            case "cat":
                var path = Path()
                path.move(to: pt(20, 34))
                path.addCurve(to: pt(38, 22), control1: pt(18, 16), control2: pt(24, 10))
                path.addCurve(to: pt(62, 22), control1: pt(46, 19), control2: pt(54, 19))
                path.addCurve(to: pt(80, 34), control1: pt(76, 10), control2: pt(82, 16))
                path.addCurve(to: pt(86, 70), control1: pt(88, 44), control2: pt(90, 58))
                path.addCurve(to: pt(50, 91), control1: pt(80, 86), control2: pt(66, 91))
                path.addCurve(to: pt(14, 70), control1: pt(34, 91), control2: pt(20, 86))
                path.addCurve(to: pt(20, 34), control1: pt(10, 58), control2: pt(12, 44))
                path.closeSubpath()
                ctx.fill(path, with: .color(blob))
                drawEye(cx: 38, cy: 55, rx: 5.5, ry: 9)
                drawEye(cx: 62, cy: 55, rx: 5.5, ry: 9)
            case "bear":
                var path = Path()
                path.move(to: pt(26, 35))
                path.addCurve(to: pt(19, 14), control1: pt(15, 33), control2: pt(11, 21))
                path.addCurve(to: pt(39, 23), control1: pt(27, 7), control2: pt(38, 14))
                path.addCurve(to: pt(61, 23), control1: pt(46, 21), control2: pt(54, 21))
                path.addCurve(to: pt(81, 14), control1: pt(62, 14), control2: pt(73, 7))
                path.addCurve(to: pt(74, 35), control1: pt(89, 21), control2: pt(85, 33))
                path.addCurve(to: pt(84, 73), control1: pt(85, 45), control2: pt(89, 60))
                path.addCurve(to: pt(50, 91), control1: pt(77, 86), control2: pt(64, 91))
                path.addCurve(to: pt(16, 73), control1: pt(36, 91), control2: pt(23, 86))
                path.addCurve(to: pt(26, 35), control1: pt(11, 60), control2: pt(15, 45))
                path.closeSubpath()
                ctx.fill(path, with: .color(blob))
                drawEye(cx: 38, cy: 55, rx: 5.5, ry: 9)
                drawEye(cx: 62, cy: 55, rx: 5.5, ry: 9)
            case "cloud":
                var path = Path()
                path.move(to: pt(28, 80))
                path.addCurve(to: pt(9, 58), control1: pt(15, 80), control2: pt(8, 70))
                path.addCurve(to: pt(26, 39), control1: pt(10, 47), control2: pt(18, 40))
                path.addCurve(to: pt(54, 15), control1: pt(27, 24), control2: pt(39, 14))
                path.addCurve(to: pt(78, 38), control1: pt(67, 16), control2: pt(77, 25))
                path.addCurve(to: pt(92, 60), control1: pt(87, 40), control2: pt(93, 49))
                path.addCurve(to: pt(69, 80), control1: pt(91, 72), control2: pt(82, 80))
                path.closeSubpath()
                ctx.fill(path, with: .color(blob))
                drawEye(cx: 39, cy: 53, rx: 5.2, ry: 8.5)
                drawEye(cx: 61, cy: 53, rx: 5.2, ry: 8.5)
            case "ghost":
                var path = Path()
                path.move(to: pt(50, 10))
                path.addCurve(to: pt(88, 52), control1: pt(74, 10), control2: pt(88, 26))
                path.addLine(to: pt(88, 84))
                path.addCurve(to: pt(78, 87), control1: pt(88, 89), control2: pt(82, 91))
                path.addCurve(to: pt(63, 87), control1: pt(73, 82), control2: pt(68, 82))
                path.addCurve(to: pt(50, 87), control1: pt(58, 92), control2: pt(53, 92))
                path.addCurve(to: pt(37, 87), control1: pt(47, 92), control2: pt(42, 92))
                path.addCurve(to: pt(22, 87), control1: pt(32, 82), control2: pt(27, 82))
                path.addCurve(to: pt(12, 84), control1: pt(18, 91), control2: pt(12, 89))
                path.addLine(to: pt(12, 52))
                path.addCurve(to: pt(50, 10), control1: pt(12, 26), control2: pt(26, 10))
                path.closeSubpath()
                ctx.fill(path, with: .color(blob))
                drawEye(cx: 38, cy: 46, rx: 5.5, ry: 9.5)
                drawEye(cx: 62, cy: 46, rx: 5.5, ry: 9.5)
            case "star":
                var path = Path()
                path.move(to: pt(50, 8))
                path.addCurve(to: pt(64, 27), control1: pt(55, 8), control2: pt(59, 23))
                path.addCurve(to: pt(90, 34), control1: pt(69, 31), control2: pt(87, 28))
                path.addCurve(to: pt(77, 57), control1: pt(93, 40), control2: pt(78, 51))
                path.addCurve(to: pt(79, 85), control1: pt(76, 63), control2: pt(85, 80))
                path.addCurve(to: pt(50, 79), control1: pt(73, 90), control2: pt(58, 79))
                path.addCurve(to: pt(21, 85), control1: pt(42, 79), control2: pt(27, 90))
                path.addCurve(to: pt(23, 57), control1: pt(15, 80), control2: pt(24, 63))
                path.addCurve(to: pt(10, 34), control1: pt(22, 51), control2: pt(7, 40))
                path.addCurve(to: pt(36, 27), control1: pt(13, 28), control2: pt(31, 31))
                path.addCurve(to: pt(50, 8), control1: pt(41, 23), control2: pt(45, 8))
                path.closeSubpath()
                ctx.fill(path, with: .color(blob))
                drawEye(cx: 40, cy: 49, rx: 5, ry: 8.5)
                drawEye(cx: 60, cy: 49, rx: 5, ry: 8.5)
            case "sprout":
                var l1 = Path()
                l1.move(to: pt(50, 28))
                l1.addCurve(to: pt(25, 13), control1: pt(48, 16), control2: pt(36, 9))
                l1.addCurve(to: pt(50, 28), control1: pt(26, 23), control2: pt(37, 29))
                l1.closeSubpath()
                ctx.fill(l1, with: .color(blob))

                var l2 = Path()
                l2.move(to: pt(50, 28))
                l2.addCurve(to: pt(76, 12), control1: pt(53, 15), control2: pt(65, 8))
                l2.addCurve(to: pt(50, 28), control1: pt(75, 23), control2: pt(63, 29))
                l2.closeSubpath()
                ctx.fill(l2, with: .color(blob))

                var body = Path()
                body.move(to: pt(50, 28))
                body.addCurve(to: pt(84, 62), control1: pt(66, 28), control2: pt(84, 40))
                body.addCurve(to: pt(50, 92), control1: pt(84, 82), control2: pt(69, 92))
                body.addCurve(to: pt(16, 62), control1: pt(31, 92), control2: pt(16, 82))
                body.addCurve(to: pt(50, 28), control1: pt(16, 40), control2: pt(34, 28))
                body.closeSubpath()
                ctx.fill(body, with: .color(blob))
                drawEye(cx: 39, cy: 59, rx: 5.2, ry: 8.8)
                drawEye(cx: 61, cy: 59, rx: 5.2, ry: 8.8)
            default:
                break
            }
                }
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

struct BotAppearancePicker: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    var botId: String
    var index: Int = -1
    @Binding var avatar: String
    @Binding var color: String
    var accent: Color? = nil

    private var effectiveAvatar: String {
        let s = avatar.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        return s.isEmpty ? "blob" : s
    }

    private var effectiveHex: String {
        botColorHex(botId, index, custom: color)
    }

    private var hasCustom: Bool {
        effectiveAvatar != "blob" || normalizeBotHex(color) != nil
    }

    var body: some View {
        let ring = accent ?? p.accent
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text(hub.t("avatarShape")).font(.system(size: 13)).foregroundStyle(.secondary)
                Spacer()
                if hasCustom {
                    Button(hub.t("resetDefault")) {
                        avatar = ""
                        color = ""
                    }
                    .font(.system(size: 13))
                    .buttonStyle(.borderless)
                }
            }
            LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 8), count: 4), spacing: 8) {
                ForEach(botAvatars, id: \.self) { kind in
                    let active = effectiveAvatar == kind
                    Button {
                        avatar = kind == "blob" ? "" : kind
                    } label: {
                        BotAvatar(id: botId, size: 32, index: index, avatar: kind, color: effectiveHex)
                            .frame(maxWidth: .infinity)
                            .padding(.vertical, 6)
                            .background(p.s1, in: RoundedRectangle(cornerRadius: 10))
                            .overlay(
                                RoundedRectangle(cornerRadius: 10)
                                    .stroke(active ? ring : .clear, lineWidth: 2)
                            )
                    }
                    .buttonStyle(.plain)
                }
            }
            Text(hub.t("botColorLabel")).font(.system(size: 13)).foregroundStyle(.secondary)
            HStack(spacing: 12) {
                ForEach(botColors, id: \.self) { hex in
                    let active = normalizeBotHex(color) == hex
                    Button { color = hex } label: {
                        Circle().fill(Color(hex: hex)).frame(width: 26, height: 26)
                            .overlay {
                                if active {
                                    Image(systemName: "checkmark").font(.system(size: 12, weight: .bold)).foregroundStyle(.white)
                                }
                            }
                    }
                    .buttonStyle(.plain)
                }
                Spacer()
                ColorPicker("", selection: Binding(
                    get: { Color(hex: effectiveHex) },
                    set: { if let h = colorToHex($0) { color = h } }
                ), supportsOpacity: false)
                .labelsHidden()
            }
        }
        .padding(.vertical, 4)
    }
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

