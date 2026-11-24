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

let pebbleColor = "#7aaa98"
let botColors = ["#d9774b", "#e0a23a", "#7fa14a", pebbleColor, "#5b8bb5", "#9a7bc0", "#c4605e"]
let botAvatars = ["blob", "pebble-1", "pebble-2", "pebble-3", "pebble-4", "pebble-5", "pebble-6", "pebble-7"]

private let legacyBotAvatarMap: [String: String] = [
    "squircle": "pebble-1",
    "cat": "pebble-2",
    "bear": "pebble-3",
    "cloud": "pebble-4",
    "ghost": "pebble-5",
    "star": "pebble-6",
    "sprout": "pebble-7",
]

private let pebbleVariantSeeds: [String: String] = [
    "pebble-1": "pebble-v6",
    "pebble-2": "pebble-v1",
    "pebble-3": "pebble-v30",
    "pebble-4": "pebble-v11",
    "pebble-5": "pebble-v21",
    "pebble-6": "pebble-v17",
    "pebble-7": "pebble-v15",
]

func normalizeBotAvatar(_ raw: String?) -> String {
    let s = (raw ?? "").trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    if s.isEmpty || s == "blob" { return "blob" }
    if let mapped = legacyBotAvatarMap[s] { return mapped }
    return botAvatars.contains(s) ? s : "blob"
}

func resolvePebbleSeed(id: String, avatar: String?) -> String {
    let kind = normalizeBotAvatar(avatar)
    if kind != "blob", let seed = pebbleVariantSeeds[kind] {
        return seed
    }
    return id.isEmpty ? "?" : id
}

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
        pink = Color(hex: dark ? "#2c1616" : "#fde8e8")
        pinkInk = Color(hex: dark ? "#ff6961" : "#dc2626")
        lilac = Color(hex: dark ? "#24203a" : "#e0dbfb")
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

private struct PebblePressFX {
    var sx: Double
    var sy: Double
    var dy: Double
    var eye: Double
}

private func drawPebble(in ctx: inout GraphicsContext, size: CGSize, params P: PebbleParams, color: Color, eye: Color, t: Double, busy: Bool, fx: PebblePressFX? = nil) {
    let k = min(size.width, size.height) / 100.0
    if let fx {
        ctx.translateBy(x: 0, y: fx.dy * k)
        ctx.translateBy(x: 50 * k, y: 90 * k)
        ctx.scaleBy(x: fx.sx, y: fx.sy)
        ctx.translateBy(x: -50 * k, y: -90 * k)
    }
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
    let eh = 9.5 * max(0.12, blink * (fx?.eye ?? 1.0))
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
    var bounceTrigger: Int = 0
    @State private var pulse = false
    @State private var startTime = Date()
    @State private var pressDown = false
    @State private var pressT0: TimeInterval = 0
    @State private var pressT1: TimeInterval = 0
    @State private var pressActive = false
    @State private var clearTask: Task<Void, Never>?

    private func scheduleStopPress() {
        clearTask?.cancel()
        clearTask = Task {
            try? await Task.sleep(for: .seconds(1.5))
            if !Task.isCancelled && !pressDown {
                pressActive = false
            }
        }
    }

    var body: some View {
        let blob = Color(hex: botColorHex(id, index, custom: color))
        let seed = resolvePebbleSeed(id: id, avatar: avatar)
        let params = pebbleParams(for: seed)
        TimelineView(.animation(minimumInterval: pressActive ? (1.0 / 60.0) : (1.0 / 30.0), paused: reduceMotion && !pressActive)) { timeline in
            let t = reduceMotion ? 0.0 : max(0.0, timeline.date.timeIntervalSince(startTime))
            let fx: PebblePressFX? = {
                guard pressActive else { return nil }
                let now = timeline.date.timeIntervalSinceReferenceDate
                let d = pressDown ? min(1.0, max(0.0, (now - pressT0) / 0.08)) : 0.0
                let r = pressDown ? 0.0 : max(0.0, now - pressT1)
                if !pressDown && r > 1.4 { return nil }
                let b = pressDown ? 0.0 : exp(-5.0 * r) * sin(r * 22.0)
                let hop = (!pressDown && r < 0.32) ? sin((r / 0.32) * .pi) : 0.0
                let eye = pressDown ? 0.0 : (r < 0.14 ? r / 0.14 : 1.0)
                return PebblePressFX(
                    sx: 1.0 + 0.08 * d - 0.07 * b,
                    sy: 1.0 - 0.10 * d + 0.09 * b,
                    dy: -5.0 * hop,
                    eye: eye
                )
            }()
            Canvas { ctx, sz in
                drawPebble(in: &ctx, size: sz, params: params, color: blob, eye: p.eye, t: t, busy: busy, fx: fx)
            }
        }
        .frame(width: size, height: size)
        .contentShape(Rectangle())
        .simultaneousGesture(
            DragGesture(minimumDistance: 0)
                .onChanged { _ in
                    if !pressDown {
                        clearTask?.cancel()
                        pressDown = true
                        pressT0 = Date().timeIntervalSinceReferenceDate
                        pressActive = true
                    }
                }
                .onEnded { _ in
                    if pressDown {
                        pressDown = false
                        pressT1 = Date().timeIntervalSinceReferenceDate
                        scheduleStopPress()
                    }
                }
        )
        .onChange(of: bounceTrigger) { _, _ in
            clearTask?.cancel()
            pressDown = false
            pressT1 = Date().timeIntervalSinceReferenceDate
            pressActive = true
            scheduleStopPress()
        }
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
    /// 原生 Form 套 DESIGN 页面底色与卡片圆角。
    func hubForm() -> some View { modifier(HubSurface(form: true)) }

    /// 窄宽度：insetGrouped + page 底；宽屏：系统悬浮侧栏；macOS：实色侧栏卡片 + 1px 细分隔。
    @ViewBuilder
    func hubColumnList(compact: Bool, page: Color) -> some View {
        #if os(iOS)
        if compact {
            self.listStyle(.insetGrouped).scrollContentBackground(.hidden).background(page)
        } else {
            self.listStyle(.sidebar)
        }
        #else
        self.modifier(HubMacColumnListModifier())
        #endif
    }

    func monoInput() -> some View {
        self.font(.system(size: AppFont.secondary, design: .monospaced))
            .autocorrectionDisabled()
            .noAutocap()
    }

    @ViewBuilder
    func composerGlassButton() -> some View {
        #if os(iOS)
        let base = self.buttonBorderShape(.circle)
        if #available(iOS 26.0, *) {
            base.buttonStyle(.glass)
        } else {
            base.buttonStyle(.bordered)
        }
        #else
        self.buttonStyle(MacComposerCircleButtonStyle())
        #endif
    }

    @ViewBuilder
    func composerGlassField() -> some View {
        #if os(iOS)
        let shape = RoundedRectangle(cornerRadius: 22, style: .continuous)
        if #available(iOS 26.0, *) {
            self.glassEffect(.regular.interactive(), in: shape)
        } else {
            self.background(.regularMaterial, in: shape)
        }
        #else
        self.modifier(MacComposerFieldModifier())
        #endif
    }

    @ViewBuilder
    func composerSendButton(accent: Color) -> some View {
        #if os(iOS)
        let base = self.buttonBorderShape(.circle).tint(accent)
        if #available(iOS 26.0, *) {
            base.buttonStyle(.glassProminent)
        } else {
            base.buttonStyle(.borderedProminent)
        }
        #else
        self.buttonStyle(MacComposerSendButtonStyle(accent: accent))
        #endif
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

struct BotAppearancePicker: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    var botId: String
    var index: Int = -1
    @Binding var avatar: String
    @Binding var color: String
    var accent: Color? = nil

    private var effectiveAvatar: String {
        normalizeBotAvatar(avatar)
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
                        BotAvatar(id: botId, size: 36, index: index, avatar: kind, color: effectiveHex)
                            .frame(maxWidth: .infinity)
                            .padding(.vertical, 6)
                            .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                            .overlay(
                                RoundedRectangle(cornerRadius: 10, style: .continuous)
                                    .stroke(active ? ring : .clear, lineWidth: 1.5)
                            )
                    }
                    .buttonStyle(.plain)
                }
            }
            Text(hub.t("botColorLabel")).font(.system(size: 13)).foregroundStyle(.secondary)
            #if os(macOS)
            HStack(spacing: 6) {
                ForEach(botColors, id: \.self) { hex in
                    let active = normalizeBotHex(color) == hex
                    Button { color = hex } label: {
                        Circle().fill(Color(hex: hex)).frame(width: 24, height: 24)
                            .overlay {
                                if active {
                                    Image(systemName: "checkmark").font(.system(size: 11, weight: .bold)).foregroundStyle(.white)
                                }
                            }
                    }
                    .buttonStyle(.plain)
                }
                Spacer(minLength: 4)
                ColorPicker("", selection: Binding(
                    get: { Color(hex: effectiveHex) },
                    set: { if let h = colorToHex($0) { color = h } }
                ), supportsOpacity: false)
                .labelsHidden()
                .controlSize(.small)
            }
            #else
            HStack(spacing: 10) {
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
            #endif
        }
        .padding(.vertical, 4)
    }
}

struct HubSurface: ViewModifier {
    @Environment(\.palette) private var p
    var form: Bool

    func body(content: Content) -> some View {
        #if os(macOS)
        if #available(macOS 15.0, *) {
            content
                .buttonStyle(HubMacRowButtonStyle())
                .labeledContentStyle(HubMacLabeledContentStyle())
                .toggleStyle(HubMacSwitchToggleStyle())
                .textFieldStyle(HubMacTextFieldStyle())
                .formStyle(HubMacFormStyle())
                .background(p.page.ignoresSafeArea())
                .font(.system(size: AppFont.body))
        } else {
            content
                .formStyle(.grouped)
                .scrollContentBackground(.hidden)
                .background(p.page.ignoresSafeArea())
                .font(.system(size: AppFont.body))
        }
        #else
        content
            .scrollContentBackground(.hidden)
            .background(p.page)
            .font(.system(size: AppFont.body))
        #endif
    }
}

#if os(macOS)
private struct MacSeamlessTitlebarBridge: NSViewRepresentable {
    func makeNSView(context: Context) -> NSView {
        let view = NSView()
        DispatchQueue.main.async { apply(to: view.window) }
        return view
    }

    func updateNSView(_ nsView: NSView, context: Context) {
        DispatchQueue.main.async { apply(to: nsView.window) }
    }

    private func apply(to window: NSWindow?) {
        guard let window else { return }
        if !window.titlebarAppearsTransparent {
            window.titlebarAppearsTransparent = true
        }
        if window.titlebarSeparatorStyle != .none {
            window.titlebarSeparatorStyle = .none
        }
    }
}

struct MacSeamlessWindowToolbarModifier: ViewModifier {
    func body(content: Content) -> some View {
        if #available(macOS 15.0, *) {
            content
                .toolbarBackgroundVisibility(.hidden, for: .windowToolbar)
                .background(MacSeamlessTitlebarBridge())
        } else {
            content
                .background(MacSeamlessTitlebarBridge())
        }
    }
}

extension View {
    func macSeamlessWindowToolbar() -> some View {
        modifier(MacSeamlessWindowToolbarModifier())
    }
}

private struct HubMacColumnListModifier: ViewModifier {
    func body(content: Content) -> some View {
        content
            .listStyle(.sidebar)
    }
}

private struct MacComposerCircleButtonStyle: ButtonStyle {
    @Environment(\.palette) private var p
    @Environment(\.isEnabled) private var isEnabled
    @State private var hovering = false

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .frame(width: 34, height: 34)
            .foregroundStyle(isEnabled ? p.text : p.mute)
            .background(
                Circle().fill(configuration.isPressed ? p.s2 : (hovering ? p.s1 : p.card))
            )
            .overlay(Circle().stroke(p.s2, lineWidth: 1))
            .contentShape(Circle())
            .onHover { hovering = $0 }
    }
}

private struct MacComposerFieldModifier: ViewModifier {
    @Environment(\.palette) private var p

    func body(content: Content) -> some View {
        let shape = RoundedRectangle(cornerRadius: 20, style: .continuous)
        content
            .background(p.card, in: shape)
            .overlay(shape.stroke(p.s2, lineWidth: 1))
    }
}

private struct MacComposerSendButtonStyle: ButtonStyle {
    @Environment(\.palette) private var p
    @Environment(\.isEnabled) private var isEnabled
    var accent: Color

    func makeBody(configuration: Configuration) -> some View {
        let fg: Color = isEnabled ? contrastText(colorToHex(accent) ?? "#1d1b19") : p.mute
        let bg: Color = isEnabled ? (configuration.isPressed ? accent.opacity(0.82) : accent) : p.s2
        configuration.label
            .frame(width: 34, height: 34)
            .foregroundStyle(fg)
            .background(Circle().fill(bg))
            .contentShape(Circle())
    }
}

struct HubMacTextFieldStyle: TextFieldStyle {
    @Environment(\.palette) private var p

    func _body(configuration: TextField<Self._Label>) -> some View {
        configuration
            .textFieldStyle(.plain)
            .font(.system(size: AppFont.body))
            .foregroundStyle(p.text)
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(p.s1, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).stroke(p.s2, lineWidth: 1))
    }
}

struct HubMacLabeledContentStyle: LabeledContentStyle {
    @Environment(\.palette) private var p

    func makeBody(configuration: Configuration) -> some View {
        HStack(alignment: .center, spacing: 12) {
            configuration.label
                .foregroundStyle(p.text)
            Spacer(minLength: 12)
            configuration.content
                .foregroundStyle(p.text2)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct HubMacSwitchToggleStyle: ToggleStyle {
    @Environment(\.palette) private var p

    func makeBody(configuration: Configuration) -> some View {
        HStack(alignment: .center, spacing: 12) {
            configuration.label
                .foregroundStyle(p.text)
            Spacer(minLength: 12)
            Toggle("", isOn: configuration.$isOn)
                .labelsHidden()
                .toggleStyle(.switch)
                .controlSize(.small)
        }
    }
}

struct HubMacRowButtonStyle: ButtonStyle {
    @Environment(\.palette) private var p
    @Environment(\.isEnabled) private var isEnabled
    @State private var hovering = false

    func makeBody(configuration: Configuration) -> some View {
        let fg: Color = configuration.role == .destructive ? p.danger : (isEnabled ? p.text : p.mute)
        HStack(spacing: 8) {
            configuration.label
            Spacer(minLength: 8)
        }
        .foregroundStyle(fg)
        .padding(.vertical, 2)
        .padding(.horizontal, 4)
        .background(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(configuration.isPressed ? p.s2 : (hovering ? p.s1 : .clear))
        )
        .contentShape(Rectangle())
        .onHover { hovering = $0 }
    }
}

@available(macOS 15.0, *)
struct HubMacFormStyle: FormStyle {
    @Environment(\.palette) private var p

    func makeBody(configuration: Configuration) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                Group(sections: configuration.content) { sections in
                    ForEach(sections) { section in
                        VStack(alignment: .leading, spacing: 6) {
                            if !section.header.isEmpty {
                                section.header
                                    .font(.system(size: AppFont.caption, weight: .semibold))
                                    .foregroundStyle(p.text2)
                                    .padding(.horizontal, 4)
                            }
                            if !section.content.isEmpty {
                                Group(subviews: section.content) { subviews in
                                    VStack(alignment: .leading, spacing: 0) {
                                        ForEach(Array(subviews.enumerated()), id: \.element.id) { idx, subview in
                                            if idx > 0 {
                                                Rectangle()
                                                    .fill(p.s2)
                                                    .frame(height: 1)
                                                    .padding(.leading, 14)
                                            }
                                            subview
                                                .frame(maxWidth: .infinity, alignment: .leading)
                                                .padding(.horizontal, 14)
                                                .padding(.vertical, 10)
                                        }
                                    }
                                    .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
                                    .background(p.card, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                                    .overlay(
                                        RoundedRectangle(cornerRadius: 14, style: .continuous)
                                            .stroke(p.s2.opacity(0.8), lineWidth: 1)
                                    )
                                }
                            }
                            if !section.footer.isEmpty {
                                section.footer
                                    .font(.system(size: AppFont.caption))
                                    .foregroundStyle(p.mute)
                                    .padding(.horizontal, 4)
                            }
                        }
                    }
                }
            }
            .padding(16)
            .frame(minWidth: 0, maxWidth: 760, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
    }
}
#endif

