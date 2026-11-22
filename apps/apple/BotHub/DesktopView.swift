import SwiftUI

#if os(iOS)
typealias PlatformImage = UIImage
extension Image { init(platform img: UIImage) { self.init(uiImage: img) } }
#else
typealias PlatformImage = NSImage
extension Image { init(platform img: NSImage) { self.init(nsImage: img) } }
#endif

struct DesktopView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    var initialTarget: String

    @State private var target: String
    @State private var frame: DesktopFrame?
    @State private var image: PlatformImage?
    @State private var streaming = true
    @State private var error = ""
    @State private var showText = false
    @State private var draft = ""
    @State private var pressStart = Date()

    private let stage = Color(hex: "#111110")

    init(initialTarget: String) {
        self.initialTarget = initialTarget
        _target = State(initialValue: initialTarget.isEmpty ? "local" : initialTarget)
    }

    private var targets: [DesktopTarget] {
        hub.desktopTargets.isEmpty ? [DesktopTarget(id: "local", name: hub.t("localDesktopName"), local: true, supported: true)] : hub.desktopTargets
    }

    private var activeTakeover: TakeoverRequest? {
        hub.takeovers.first { $0.target == target } ?? hub.takeovers.first
    }

    var body: some View {
        VStack(spacing: 10) {
            if targets.count > 1 {
                Picker(hub.t("remoteDesktop"), selection: Binding(get: { target }, set: { target = $0; frame = nil; image = nil })) {
                    ForEach(targets) { Text($0.name ?? $0.id).tag($0.id) }
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .padding(.horizontal, 12)
            }
            if let to = activeTakeover {
                HStack(spacing: 10) {
                    Label(hub.t("takeoverBanner", ["bot": to.botName ?? to.botId ?? "Bot", "reason": to.reason ?? ""]), systemImage: "hand.raised.fill")
                        .font(.system(size: 13)).foregroundStyle(p.pinkInk)
                    Spacer(minLength: 4)
                    Button(hub.t("doneResumeAI")) { Task { try? await hub.resolveTakeover(to.id) } }
                        .buttonStyle(.borderedProminent).controlSize(.small)
                }
                .padding(12)
                .background(p.pink, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                .padding(.horizontal, 12)
            }
            stageView
            controls
        }
        .padding(.top, 8)
        .navigationTitle(hub.t("remoteDesktop"))
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .toolbar {
            SheetToolbar()
            ToolbarItem(placement: .primaryAction) {
                Button { streaming.toggle() } label: {
                    Label(streaming ? hub.t("pause") : hub.t("resume"), systemImage: streaming ? "pause.fill" : "play.fill")
                }
            }
        }
        .task { _ = try? await hub.refreshDesktopTargets() }
        .task(id: target) { await pollState() }
        .task(id: "\(target)-\(streaming)") { if streaming { await frameLoop() } }
    }

    private var stageView: some View {
        GeometryReader { geo in
            ZStack {
                stage
                if let image {
                    Image(platform: image).resizable().aspectRatio(contentMode: .fit)
                } else {
                    VStack(spacing: 10) {
                        ProgressView().tint(p.accent)
                        Text(error.isEmpty ? hub.t("fetchingDesktop") : error)
                            .font(.system(size: 13)).foregroundStyle(Color(hex: "#a6a09a")).multilineTextAlignment(.center)
                    }
                    .padding(20)
                }
                if image != nil, !error.isEmpty {
                    VStack {
                        Spacer()
                        Text(error).font(.system(size: 12)).foregroundStyle(Color(hex: "#f3f1ee"))
                            .padding(.horizontal, 12).padding(.vertical, 6)
                            .background(Color(hex: "#1f1d1b"), in: Capsule())
                            .padding(10)
                    }
                }
            }
            .contentShape(Rectangle())
            .gesture(
                DragGesture(minimumDistance: 0)
                    .onChanged { v in if v.translation == .zero { pressStart = Date() } }
                    .onEnded { v in
                        guard hypot(v.translation.width, v.translation.height) < 10 else { return }
                        let kind = Date().timeIntervalSince(pressStart) > 0.5 ? "right_click" : "click"
                        if let pt = map(v.location, in: geo.size) { send(["kind": kind, "x": pt.x, "y": pt.y]) }
                    }
            )
        }
        .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
        .padding(.horizontal, 12)
    }

    private var controls: some View {
        VStack(spacing: 8) {
            if showText {
                HStack(spacing: 8) {
                    TextField(hub.t("typeDesktopPh"), text: $draft)
                        .textFieldStyle(.roundedBorder)
                        .onSubmit { sendText() }
                    Button(hub.t("send")) { sendText() }.buttonStyle(.borderedProminent)
                }
                .padding(.horizontal, 12)
            }
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    Toggle(isOn: $showText) { Label(hub.t("aaInput"), systemImage: "keyboard") }
                        .toggleStyle(.button)
                    let items: [(String, String, [String: Any])] = [
                        (hub.t("keyEnter"), "return", ["kind": "key", "key": "Return"]),
                        (hub.t("keyBackspace"), "delete.left", ["kind": "key", "key": "Backspace"]),
                        ("Esc", "escape", ["kind": "key", "key": "Escape"]),
                        ("Tab", "arrow.right.to.line", ["kind": "key", "key": "Tab"]),
                        (hub.t("keyScrollUp"), "chevron.up", ["kind": "scroll", "deltaY": 8]),
                        (hub.t("keyScrollDown"), "chevron.down", ["kind": "scroll", "deltaY": -8]),
                    ]
                    ForEach(items, id: \.0) { it in
                        Button { send(it.2) } label: { Label(it.0, systemImage: it.1) }
                    }
                }
                .buttonStyle(.bordered)
                .padding(.horizontal, 12)
            }
        }
        .padding(.bottom, 10)
    }

    private func map(_ loc: CGPoint, in box: CGSize) -> CGPoint? {
        guard let f = frame, let iw = f.imageWidth, let ih = f.imageHeight, iw > 0, ih > 0, box.width > 0, box.height > 0 else { return nil }
        let scale = min(box.width / iw, box.height / ih)
        let dw = iw * scale, dh = ih * scale
        let rx = (loc.x - (box.width - dw) / 2) / dw
        let ry = (loc.y - (box.height - dh) / 2) / dh
        guard (0...1).contains(rx), (0...1).contains(ry) else { return nil }
        return CGPoint(x: (f.logicalX ?? 0) + rx * (f.logicalWidth ?? iw), y: (f.logicalY ?? 0) + ry * (f.logicalHeight ?? ih))
    }

    private func send(_ body: [String: Any]) {
        var b = body
        b["target"] = target
        Task {
            do { try await hub.sendDesktopInput(b) } catch { self.error = errText(error, hub.t("opFailed")) }
        }
    }

    private func sendText() {
        guard !draft.isEmpty else { return }
        let txt = draft
        draft = ""
        send(["kind": "type", "text": txt])
    }

    private func pollState() async {
        while !Task.isCancelled {
            if let st = try? await hub.fetchDesktopState(target), st.status?.screenRecording == "denied" {
                streaming = false
                error = hub.t("deskScreenRecordingDenied")
            }
            try? await Task.sleep(nanoseconds: 2_000_000_000)
        }
    }

    private func frameLoop() async {
        while !Task.isCancelled {
            do {
                let f = try await hub.fetchDesktopFrame(target)
                if Task.isCancelled { break }
                if let data = Data(base64Encoded: f.jpegBase64), let img = PlatformImage(data: data) {
                    frame = f
                    image = img
                }
                error = ""
            } catch {
                if Task.isCancelled { break }
                let msg = errText(error, hub.t("captureFailed"))
                self.error = msg
                if (error as? HubError)?.status == 403 || msg.contains("屏幕录制") || msg.contains("Screen Recording") || msg.contains("screen_recording_denied") {
                    streaming = false
                    break
                }
                try? await Task.sleep(nanoseconds: 1_500_000_000)
            }
            try? await Task.sleep(nanoseconds: 140_000_000)
        }
    }
}
