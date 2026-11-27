import SwiftUI

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

private final class PebbleParamsBox {
    let value: PebbleParams
    init(_ value: PebbleParams) { self.value = value }
}

private let pebbleParamsCache: NSCache<NSString, PebbleParamsBox> = {
    let c = NSCache<NSString, PebbleParamsBox>()
    c.countLimit = 256
    return c
}()

private func pebbleLerp(_ a: Double, _ b: Double, _ t: Double) -> Double { a + (b - a) * t }

private func pebbleParams(for id: String) -> PebbleParams {
    let key = id.isEmpty ? "?" : id
    if let cached = pebbleParamsCache.object(forKey: key as NSString) {
        return cached.value
    }
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
    let built = PebbleParams(
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
    pebbleParamsCache.setObject(PebbleParamsBox(built), forKey: key as NSString)
    return built
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
    let circ: Double = switch P.n {
    case 3: 54
    case 4: 49
    case 5: 46
    default: 45
    }
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
    @Environment(\.hubWindowVisible) private var windowVisible
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

    private func pressEffect(at now: TimeInterval) -> PebblePressFX? {
        guard pressActive else { return nil }
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
    }

    var body: some View {
        let blob = Color(hex: botColorHex(id, index, custom: color))
        let seed = resolvePebbleSeed(id: id, avatar: avatar)
        let params = pebbleParams(for: seed)
        let animating = windowVisible && (pressActive || (busy && !reduceMotion))
        let shouldPulse = busy && windowVisible && !reduceMotion

        Group {
            if animating {
                TimelineView(.animation(minimumInterval: pressActive ? (1.0 / 60.0) : (1.0 / 20.0), paused: false)) { timeline in
                    let t = reduceMotion ? 0.0 : max(0.0, timeline.date.timeIntervalSince(startTime))
                    let fx = pressEffect(at: timeline.date.timeIntervalSinceReferenceDate)
                    Canvas { ctx, sz in
                        drawPebble(in: &ctx, size: sz, params: params, color: blob, eye: p.eye, t: t, busy: busy, fx: fx)
                    }
                }
            } else {
                Canvas { ctx, sz in
                    drawPebble(in: &ctx, size: sz, params: params, color: blob, eye: p.eye, t: 0, busy: busy, fx: nil)
                }
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
        .onChange(of: windowVisible) { _, vis in
            if !vis {
                clearTask?.cancel()
                pressDown = false
                pressActive = false
            }
        }
        .overlay(alignment: .bottomTrailing) {
            if busy {
                let d = max(9, (size * 0.24).rounded())
                Circle().fill(p.busy)
                    .frame(width: d, height: d)
                    .overlay(Circle().stroke(p.page, lineWidth: 2))
                    .opacity(shouldPulse && pulse ? 0.65 : 1)
                    .task(id: shouldPulse) {
                        if shouldPulse {
                            pulse = false
                            withAnimation(.easeInOut(duration: 0.6).repeatForever(autoreverses: true)) {
                                pulse = true
                            }
                        } else {
                            withAnimation(.linear(duration: 0.01)) {
                                pulse = false
                            }
                        }
                    }
            }
        }
        .accessibilityHidden(true)
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
                Text(hub.t("avatarShape")).font(.system(size: AppFont.secondary)).foregroundStyle(.secondary)
                Spacer()
                if hasCustom {
                    Button(hub.t("resetDefault")) {
                        avatar = ""
                        color = ""
                    }
                    .font(.system(size: AppFont.secondary))
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
            Text(hub.t("botColorLabel")).font(.system(size: AppFont.secondary)).foregroundStyle(.secondary)
            FlowLayout(spacing: 8) {
                ForEach(botColors, id: \.self) { hex in
                    let active = normalizeBotHex(color) == hex
                    Button { color = hex } label: {
                        Circle().fill(Color(hex: hex)).frame(width: 25, height: 25)
                            .overlay {
                                if active {
                                    Image(systemName: "checkmark").font(.system(size: AppFont.caption, weight: .bold)).foregroundStyle(.white)
                                }
                            }
                    }
                    .buttonStyle(.plain)
                }
                ColorPicker("", selection: Binding(
                    get: { Color(hex: effectiveHex) },
                    set: { if let h = colorToHex($0) { color = h } }
                ), supportsOpacity: false)
                .labelsHidden()
            }
            .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
        }
        .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 4)
    }
}
