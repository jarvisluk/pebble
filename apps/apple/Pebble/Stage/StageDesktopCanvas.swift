import SwiftUI

struct StageDesktopCanvas: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    @Environment(\.hubWindowVisible) private var windowVisible

    var isActive: Bool
    var resolvedBotId: String?
    var inWideStage: Bool
    @Binding var target: String
    @Binding var streaming: Bool
    @Binding var showText: Bool
    @Binding var draft: String
    @Binding var streamMode: StreamMode
    @Binding var fallbackImage: PlatformImage?
    @Binding var frameFps: Int
    @Binding var error: String
    var streamCtrl: H264StreamController
    var draftFocused: FocusState<Bool>.Binding

    enum StreamMode: Equatable {
        case wsHEVC
        case wsH264
        case framePolling
    }

    @State private var fallbackMeta: H264StreamMeta?

    private let stageBg = Color.black
    private let pillBg = Color(hex: "#1f1d1b")
    private let pillText = Color(hex: "#f3f1ee")
    private let pillMute = Color(hex: "#a6a09a")

    private var hasBoundTarget: Bool {
        guard let bid = resolvedBotId,
              let bound = hub.bots.first(where: { $0.id == bid })?.desktopTarget,
              !bound.isEmpty else {
            return false
        }
        return true
    }

    private var activeTakeover: TakeoverRequest? {
        hub.takeovers.first { $0.target == target } ?? hub.takeovers.first
    }

    private var preferredStreamMode: StreamMode {
        if !HubStore.isLocalDesktopTarget(target) || !hub.supports("desktop.ws_h264") {
            return .framePolling
        }
        if !hub.supports("desktop.ws_hevc") {
            return .wsH264
        }
        return .wsHEVC
    }

    var body: some View {
        let canvas = VStack(spacing: 0) {
            if hub.activeDesktopTarget.temporary {
                HStack(spacing: 10) {
                    Text(hub.t("tempDesktopBanner", ["name": hub.activeDesktopTarget.name ?? hub.activeDesktopTarget.target]))
                        .font(.system(size: AppFont.secondary, weight: .semibold)).foregroundStyle(pillText)
                        .lineLimit(1)
                    Spacer(minLength: 4)
                    Button(hub.t("switchBackLocal")) {
                        target = "local"
                        Task { try? await hub.setActiveDesktopTarget("local") }
                    }
                    .buttonStyle(.bordered).controlSize(.small).tint(pillText)
                }
                .padding(.horizontal, 12).padding(.vertical, 8)
                .background(pillBg, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                .padding(.horizontal, 12)
                .padding(.top, 8)
            }
            if let to = activeTakeover {
                HStack(spacing: 10) {
                    Label(hub.t("takeoverBanner", ["bot": to.botName ?? to.botId ?? "Bot", "reason": to.reason ?? ""]), systemImage: "hand.raised.fill")
                        .font(.system(size: AppFont.body)).foregroundStyle(p.pinkInk)
                    Spacer(minLength: 4)
                    Button(hub.t("doneResumeAI")) { Task { try? await hub.resolveTakeover(to.id) } }
                        .buttonStyle(.borderedProminent).controlSize(.small)
                }
                .padding(10)
                .background(p.pink, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                .padding(.horizontal, 12)
                .padding(.top, 8)
            }

            stageView
                .frame(maxWidth: .infinity, maxHeight: .infinity)

            if showText {
                desktopTextBar
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
            }
        }
        .background(stageBg)
        .onAppear {
            streamMode = preferredStreamMode
        }
        .onChange(of: hub.activeDesktopTarget.target) { _, newTarget in
            guard !hasBoundTarget else { return }
            let next = newTarget.isEmpty ? "local" : newTarget
            if target != next {
                target = next
            }
        }
        .onChange(of: target) { _, newTarget in
            let next = newTarget.isEmpty ? "local" : newTarget
            fallbackImage = nil
            fallbackMeta = nil
            frameFps = 0
            error = ""
            streamMode = preferredStreamMode
            if hub.activeDesktopTarget.target != next {
                Task { try? await hub.setActiveDesktopTarget(next) }
            }
        }
        .task(id: "\(target)-\(streaming)-\(isActive)-\(windowVisible)") {
            guard isActive, windowVisible else { return }
            try? await Task.sleep(nanoseconds: 80_000_000)
            guard !Task.isCancelled, isActive, windowVisible else { return }
            await pollState()
        }
        .task(id: "\(target)-\(streaming)-\(streamMode)-\(isActive)-\(windowVisible)") {
            if !isActive || !streaming || !windowVisible {
                streamCtrl.stop()
                return
            }
            try? await Task.sleep(nanoseconds: 80_000_000)
            guard !Task.isCancelled, isActive, streaming, windowVisible else { return }

            if streamMode != .framePolling && (!HubStore.isLocalDesktopTarget(target) || !hub.supports("desktop.ws_h264")) {
                streamMode = .framePolling
                return
            }

            switch streamMode {
            case .wsHEVC, .wsH264:
                fallbackImage = nil
                fallbackMeta = nil
                frameFps = 0
                let codec = streamMode == .wsHEVC ? "hevc" : "h264"
                if let req = hub.makeDesktopWSRequest(target: target, codec: codec) {
                    streamCtrl.start(request: req)
                } else {
                    streamCtrl.stop()
                }
            case .framePolling:
                streamCtrl.stop()
                await pollFrameLoop()
            }
        }
        .onDisappear { streamCtrl.stop() }
        .onChange(of: streamCtrl.screenRecordingDenied) { _, denied in
            if denied {
                streaming = false
                error = hub.t("deskScreenRecordingDenied")
            }
        }
        .onChange(of: streamCtrl.preFirstFrameFailureSeq) { _, _ in
            guard isActive, streaming, windowVisible else { return }
            if streamMode == .wsHEVC {
                streamMode = .wsH264
            } else if streamMode == .wsH264 {
                streamMode = .framePolling
            }
        }

        canvas
    }

    private var stageView: some View {
        GeometryReader { geo in
            let isFrameReady = streamMode == .framePolling && fallbackImage != nil
            let isDisplayReady = isFrameReady || streamCtrl.connected
            let displayErr = !error.isEmpty ? error : (streamMode == .framePolling ? "" : streamCtrl.error)
            ZStack {
                stageBg
                HubDesktopStageSurface(
                    controller: streamCtrl,
                    isFramePolling: streamMode == .framePolling,
                    fallbackImage: fallbackImage,
                    boxSize: geo.size,
                    mapPoint: { pt, size in map(pt, in: size) },
                    onInput: { send($0) }
                )
                if !isDisplayReady {
                    VStack(spacing: 10) {
                        ProgressView().tint(pillText)
                        Text(displayErr.isEmpty ? hub.t("deskH264Connecting") : displayErr)
                            .font(.system(size: AppFont.body)).foregroundStyle(pillMute).multilineTextAlignment(.center)
                        if !displayErr.isEmpty {
                            Button(hub.t("retry")) {
                                error = ""
                                fallbackImage = nil
                                streamMode = preferredStreamMode
                                streaming = true
                            }
                            .buttonStyle(.borderedProminent)
                            .controlSize(.small)
                        }
                    }
                    .padding(20)
                }
                if isDisplayReady, !displayErr.isEmpty {
                    VStack {
                        Spacer()
                        Text(displayErr).font(.system(size: AppFont.secondary)).foregroundStyle(pillText)
                            .padding(.horizontal, 12).padding(.vertical, 6)
                            .background(pillBg, in: Capsule())
                            .padding(10)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private var desktopTextBar: some View {
        let row = HStack(alignment: .bottom, spacing: 10) {
            Button {
                showText = false
                draftFocused.wrappedValue = false
            } label: {
                Image(systemName: "xmark")
                    .font(.system(size: AppFont.cardTitle, weight: .semibold))
                    .frame(width: composerControlHeight, height: composerControlHeight)
                    .contentShape(Circle())
            }
            .composerGlassButton()
            .accessibilityLabel(hub.t("close"))

            TextField(hub.t("typeDesktopPh"), text: $draft)
                .textFieldStyle(.plain)
                .font(.system(size: AppFont.body))
                .focused(draftFocused)
                .padding(.horizontal, 14)
                .padding(.vertical, 8)
                .composerGlassField()
                .onSubmit { sendText() }

            Button { sendText() } label: {
                Image(systemName: "arrow.up")
                    .font(.system(size: AppFont.cardTitle, weight: .bold))
                    .frame(width: composerControlHeight, height: composerControlHeight)
                    .contentShape(Circle())
            }
            .composerSendButton(accent: pillText)
            .disabled(draft.isEmpty)
        }

        row.glassRowContainer(spacing: 10)
    }

    private func map(_ loc: CGPoint, in box: CGSize) -> CGPoint? {
        let meta = streamMode == .framePolling ? fallbackMeta : streamCtrl.meta
        guard let m = meta, m.imageWidth > 0, m.imageHeight > 0, box.width > 0, box.height > 0 else { return nil }
        let scale = min(box.width / m.imageWidth, box.height / m.imageHeight)
        let dw = m.imageWidth * scale, dh = m.imageHeight * scale
        let rx = (loc.x - (box.width - dw) / 2) / dw
        let ry = (loc.y - (box.height - dh) / 2) / dh
        guard (0...1).contains(rx), (0...1).contains(ry) else { return nil }
        return CGPoint(x: m.logicalX + rx * m.logicalWidth, y: m.logicalY + ry * m.logicalHeight)
    }

    func send(_ body: [String: Any]) {
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

    private func pollFrameLoop() async {
        var consecErrors = 0
        var windowFrames = 0
        var fpsWindowStart = Date()
        while !Task.isCancelled && streaming && windowVisible && isActive && streamMode == .framePolling {
            let t0 = Date()
            var sleepNs: UInt64 = 350_000_000
            do {
                let fr = try await hub.fetchDesktopFrame(target)
                if Task.isCancelled || !streaming || !windowVisible || !isActive || streamMode != .framePolling {
                    break
                }
                let decoded: PlatformImage? = await Task.detached(priority: .userInitiated) {
                    guard !fr.jpegBase64.isEmpty,
                          let data = Data(base64Encoded: fr.jpegBase64) else { return nil }
                    return PlatformImage(data: data)
                }.value
                if Task.isCancelled || !streaming || !windowVisible || !isActive || streamMode != .framePolling {
                    break
                }
                if let img = decoded {
                    let iw = (fr.imageWidth ?? 0) > 0 ? fr.imageWidth! : Double(img.size.width)
                    let ih = (fr.imageHeight ?? 0) > 0 ? fr.imageHeight! : Double(img.size.height)
                    fallbackMeta = H264StreamMeta(
                        imageWidth: iw,
                        imageHeight: ih,
                        logicalX: fr.logicalX ?? 0,
                        logicalY: fr.logicalY ?? 0,
                        logicalWidth: (fr.logicalWidth ?? 0) > 0 ? fr.logicalWidth! : 1440,
                        logicalHeight: (fr.logicalHeight ?? 0) > 0 ? fr.logicalHeight! : 900,
                        fps: max(1, frameFps),
                        codec: "jpeg",
                        preset: "frame",
                        bitrateKbps: 0
                    )
                    fallbackImage = img
                    consecErrors = 0
                    windowFrames += 1
                    let elapsedWindow = Date().timeIntervalSince(fpsWindowStart)
                    if elapsedWindow >= 1.0 {
                        frameFps = max(1, Int((Double(windowFrames) / elapsedWindow).rounded()))
                        windowFrames = 0
                        fpsWindowStart = Date()
                    }
                    if !error.isEmpty { error = "" }
                }
                let elapsedNs = UInt64(max(0, Date().timeIntervalSince(t0)) * 1_000_000_000)
                sleepNs = elapsedNs >= 230_000_000 ? 120_000_000 : (350_000_000 - elapsedNs)
            } catch {
                if Task.isCancelled { break }
                if let he = error as? HubError, he.code == "screen_recording_denied" {
                    streaming = false
                    self.error = hub.t("deskScreenRecordingDenied")
                    break
                }
                consecErrors += 1
                if fallbackImage == nil && consecErrors >= 3 {
                    self.error = errText(error, hub.t("connFailed"))
                }
                sleepNs = 1_200_000_000
            }
            try? await Task.sleep(nanoseconds: sleepNs)
        }
    }

    private func pollState() async {
        while !Task.isCancelled && streaming && windowVisible && isActive {
            if let st = try? await hub.fetchDesktopState(target) {
                if st.status?.screenRecording == "denied" {
                    streaming = false
                    error = hub.t("deskScreenRecordingDenied")
                } else if error == hub.t("deskScreenRecordingDenied") {
                    error = ""
                }
            }
            let ready = streamCtrl.connected || (streamMode == .framePolling && fallbackImage != nil)
            let interval: UInt64 = ready ? 10_000_000_000 : 2_000_000_000
            try? await Task.sleep(nanoseconds: interval)
        }
    }
}
