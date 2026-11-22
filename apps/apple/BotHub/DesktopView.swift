import SwiftUI

typealias PlatformImage = UIImage
extension Image { init(platform img: UIImage) { self.init(uiImage: img) } }

struct DesktopView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    var initialTarget: String

    @State private var target: String
    @State private var preset: String = "auto"
    @State private var streaming = true
    @State private var error = ""
    @State private var showText = false
    @State private var draft = ""
    @State private var pressStart = Date()
    @State private var streamCtrl = H264StreamController()

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
                Picker(hub.t("remoteDesktop"), selection: Binding(get: { target }, set: { target = $0 })) {
                    ForEach(targets) { Text($0.name ?? $0.id).tag($0.id) }
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .padding(.horizontal, 12)
            }
            Picker("", selection: Binding(get: { preset }, set: { next in
                preset = next
                streamCtrl.setPreset(next)
            })) {
                Text(hub.t("deskPresetAuto")).tag("auto")
                Text(hub.t("deskPresetHigh")).tag("high")
                Text(hub.t("deskPresetMedium")).tag("medium")
                Text(hub.t("deskPresetLow")).tag("low")
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .padding(.horizontal, 12)
            if let to = activeTakeover {
                HStack(spacing: 10) {
                    Label(hub.t("takeoverBanner", ["bot": to.botName ?? to.botId ?? "Bot", "reason": to.reason ?? ""]), systemImage: "hand.raised.fill")
                        .font(.system(size: 15)).foregroundStyle(p.pinkInk)
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
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            SheetToolbar()
            ToolbarItem(placement: .primaryAction) {
                HStack(spacing: 8) {
                    if streamCtrl.connected {
                        Text("H.264 · \(streamCtrl.fps) fps")
                            .font(.system(size: 12, design: .monospaced))
                            .foregroundStyle(.secondary)
                    }
                    Button { streaming.toggle() } label: {
                        Label(streaming ? hub.t("pause") : hub.t("resume"), systemImage: streaming ? "pause.fill" : "play.fill")
                    }
                }
            }
        }
        .task { _ = try? await hub.refreshDesktopTargets() }
        .task(id: target) { await pollState() }
        .task(id: "\(target)-\(streaming)") {
            if streaming, let req = hub.makeDesktopWSRequest(target: target, preset: preset) {
                streamCtrl.start(request: req)
            } else {
                streamCtrl.stop()
            }
        }
        .onDisappear { streamCtrl.stop() }
        .onChange(of: streamCtrl.screenRecordingDenied) { _, denied in
            if denied {
                streaming = false
                error = hub.t("deskScreenRecordingDenied")
            }
        }
    }

    private var stageView: some View {
        GeometryReader { geo in
            let displayErr = !error.isEmpty ? error : streamCtrl.error
            ZStack {
                stage
                H264DisplayView(controller: streamCtrl)
                if !streamCtrl.connected {
                    VStack(spacing: 10) {
                        ProgressView().tint(p.accent)
                        Text(displayErr.isEmpty ? hub.t("deskH264Connecting") : displayErr)
                            .font(.system(size: 15)).foregroundStyle(Color(hex: "#a6a09a")).multilineTextAlignment(.center)
                    }
                    .padding(20)
                }
                if streamCtrl.connected, !displayErr.isEmpty {
                    VStack {
                        Spacer()
                        Text(displayErr).font(.system(size: 13)).foregroundStyle(Color(hex: "#f3f1ee"))
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
        guard let m = streamCtrl.meta, m.imageWidth > 0, m.imageHeight > 0, box.width > 0, box.height > 0 else { return nil }
        let scale = min(box.width / m.imageWidth, box.height / m.imageHeight)
        let dw = m.imageWidth * scale, dh = m.imageHeight * scale
        let rx = (loc.x - (box.width - dw) / 2) / dw
        let ry = (loc.y - (box.height - dh) / 2) / dh
        guard (0...1).contains(rx), (0...1).contains(ry) else { return nil }
        return CGPoint(x: m.logicalX + rx * m.logicalWidth, y: m.logicalY + ry * m.logicalHeight)
    }

    private func send(_ body: [String: Any]) {
        if streamCtrl.connected {
            streamCtrl.sendInput(body)
            return
        }
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
}
