import SwiftUI

typealias PlatformImage = UIImage
extension Image { init(platform img: UIImage) { self.init(uiImage: img) } }

struct DesktopView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @Environment(\.horizontalSizeClass) private var hsc

    var initialTarget: String
    var initialBotId: String? = nil

    @State private var target: String
    @State private var streaming = true
    @State private var error = ""
    @State private var showText = false
    @State private var draft = ""
    @State private var pressStart = Date()
    @State private var isDragging = false
    @State private var lastDragSent = Date.distantPast
    @State private var streamCtrl = H264StreamController()
    @State private var columnVisibility: NavigationSplitViewVisibility = .detailOnly
    @State private var showChatSheet = false
    @State private var chatBotId: String
    @FocusState private var draftFocused: Bool

    private let stage = Color.black
    private let pillBg = Color(hex: "#1f1d1b")
    private let pillText = Color(hex: "#f3f1ee")
    private let pillMute = Color(hex: "#a6a09a")

    init(initialTarget: String, initialBotId: String? = nil) {
        self.initialTarget = initialTarget
        self.initialBotId = initialBotId
        _target = State(initialValue: initialTarget.isEmpty ? "local" : initialTarget)
        _chatBotId = State(initialValue: initialBotId ?? "")
    }

    private var targets: [DesktopTarget] {
        hub.desktopTargets.isEmpty ? [DesktopTarget(id: "local", name: hub.t("localDesktopName"), local: true, supported: true)] : hub.desktopTargets
    }

    private var activeTakeover: TakeoverRequest? {
        hub.takeovers.first { $0.target == target } ?? hub.takeovers.first
    }

    private var resolvedBotId: String? {
        if !chatBotId.isEmpty, hub.bots.contains(where: { $0.id == chatBotId }) { return chatBotId }
        if let sel = router.selected, hub.bots.contains(where: { $0.id == sel }) { return sel }
        return hub.bots.first?.id
    }

    private var isWide: Bool { hsc != .compact }

    var body: some View {
        Group {
            if isWide {
                NavigationSplitView(columnVisibility: $columnVisibility) {
                    sidebarChatView(closable: false)
                        .navigationSplitViewColumnWidth(min: 320, ideal: 360, max: 420)
                } detail: {
                    NavigationStack {
                        desktopStage
                    }
                }
            } else {
                NavigationStack {
                    desktopStage
                }
                .sheet(isPresented: $showChatSheet) {
                    NavigationStack {
                        sidebarChatView(closable: true)
                    }
                    .presentationDetents([.medium, .large])
                }
            }
        }
        .tint(Color(hex: "#f3f1ee"))
        .preferredColorScheme(.dark)
        .task { _ = try? await hub.refreshDesktopTargets() }
        .task(id: "\(target)-\(streaming)") {
            await pollState()
        }
        .task(id: "\(target)-\(streaming)-ws") {
            if streaming, let req = hub.makeDesktopWSRequest(target: target) {
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

    private var desktopStage: some View {
        VStack(spacing: 0) {
            if let to = activeTakeover {
                HStack(spacing: 10) {
                    Label(hub.t("takeoverBanner", ["bot": to.botName ?? to.botId ?? "Bot", "reason": to.reason ?? ""]), systemImage: "hand.raised.fill")
                        .font(.system(size: 14)).foregroundStyle(p.pinkInk)
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
        .background(stage.ignoresSafeArea())
        .navigationTitle(targets.first(where: { $0.id == target })?.name ?? hub.t("remoteDesktop"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                Button { dismiss() } label: {
                    Label(hub.t("close"), systemImage: "chevron.backward")
                }
            }
            ToolbarItemGroup(placement: .topBarTrailing) {
                if !isWide {
                    Button { showChatSheet = true } label: {
                        Label(hub.t("deskChatSidebar"), systemImage: "bubble.left.and.bubble.right")
                    }
                }

                Button { streaming.toggle() } label: {
                    Label(streaming ? hub.t("collapse") : hub.t("connect"), systemImage: streaming ? "pause.fill" : "play.fill")
                }

                Menu {
                    Toggle(isOn: Binding(
                        get: { showText },
                        set: { v in
                            showText = v
                            if v { draftFocused = true }
                        }
                    )) {
                        Label(hub.t("aaInput"), systemImage: "keyboard")
                    }
                    Divider()
                    Button(hub.t("keyEnter")) { send(["kind": "key", "key": "Return"]) }
                    Button(hub.t("keyBackspace")) { send(["kind": "key", "key": "Backspace"]) }
                    Button("Esc") { send(["kind": "key", "key": "Escape"]) }
                    Button("Tab") { send(["kind": "key", "key": "Tab"]) }
                    Divider()
                    Button(hub.t("keyScrollUp")) { send(["kind": "scroll", "deltaY": 8]) }
                    Button(hub.t("keyScrollDown")) { send(["kind": "scroll", "deltaY": -8]) }
                } label: {
                    Label(hub.t("aaInput"), systemImage: "keyboard")
                }

                Menu {
                    if streamCtrl.connected {
                        Section {
                            Text("H.264 · \(streamCtrl.fps) fps")
                        }
                    }
                    if targets.count > 1 {
                        Picker("", selection: $target) {
                            ForEach(targets) { tr in
                                Text(tr.name ?? tr.id).tag(tr.id)
                            }
                        }
                    }
                } label: {
                    Label(hub.t("settings"), systemImage: "slider.horizontal.3")
                }
            }
        }
    }

    @ViewBuilder
    private var desktopTextBar: some View {
        let row = HStack(alignment: .bottom, spacing: 10) {
            Button {
                showText = false
                draftFocused = false
            } label: {
                Image(systemName: "xmark")
                    .font(.system(size: 15, weight: .semibold))
                    .frame(width: 28, height: 28)
            }
            .composerGlassButton()
            .accessibilityLabel(hub.t("close"))

            TextField(hub.t("typeDesktopPh"), text: $draft)
                .textFieldStyle(.plain)
                .font(.system(size: 16))
                .focused($draftFocused)
                .padding(.horizontal, 14)
                .padding(.vertical, 9)
                .composerGlassField()
                .onSubmit { sendText() }

            Button { sendText() } label: {
                Image(systemName: "arrow.up")
                    .font(.system(size: 17, weight: .bold))
                    .frame(width: 28, height: 28)
            }
            .composerSendButton(accent: p.accent)
            .disabled(draft.isEmpty)
        }

        if #available(iOS 26.0, *) {
            GlassEffectContainer(spacing: 10) { row }
        } else {
            row
        }
    }

    @ViewBuilder
    private func sidebarChatView(closable: Bool) -> some View {
        if let bid = resolvedBotId {
            ChatView(
                botId: bid,
                inDesktopSidebar: true,
                onSelectBot: { nextId in
                    chatBotId = nextId
                },
                onCloseSidebar: closable ? { showChatSheet = false } : nil
            )
            .id(bid)
        } else {
            ContentUnavailableView(hub.t("noBotsYet"), systemImage: "bubble.left.and.bubble.right")
                .toolbar {
                    if closable {
                        ToolbarItem(placement: .cancellationAction) {
                            Button(hub.t("close")) { showChatSheet = false }
                        }
                    }
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
                            .font(.system(size: 15)).foregroundStyle(pillMute).multilineTextAlignment(.center)
                        if !displayErr.isEmpty {
                            Button(hub.t("retryConnect")) {
                                error = ""
                                if let req = hub.makeDesktopWSRequest(target: target) {
                                    streaming = true
                                    streamCtrl.start(request: req)
                                }
                            }
                            .buttonStyle(.borderedProminent)
                            .controlSize(.small)
                        }
                    }
                    .padding(20)
                }
                if streamCtrl.connected, !displayErr.isEmpty {
                    VStack {
                        Spacer()
                        Text(displayErr).font(.system(size: 13)).foregroundStyle(pillText)
                            .padding(.horizontal, 12).padding(.vertical, 6)
                            .background(pillBg, in: Capsule())
                            .padding(10)
                    }
                }
            }
            .contentShape(Rectangle())
            .gesture(
                DragGesture(minimumDistance: 0)
                    .onChanged { v in
                        if v.translation == .zero {
                            pressStart = Date()
                            isDragging = false
                        } else if !isDragging, hypot(v.translation.width, v.translation.height) >= 10 {
                            isDragging = true
                            if let startPt = map(v.startLocation, in: geo.size) {
                                send(["kind": "down", "x": startPt.x, "y": startPt.y, "button": "left"])
                            }
                        } else if isDragging, Date().timeIntervalSince(lastDragSent) >= 0.016 {
                            lastDragSent = Date()
                            if let pt = map(v.location, in: geo.size) {
                                send(["kind": "move", "x": pt.x, "y": pt.y])
                            }
                        }
                    }
                    .onEnded { v in
                        if isDragging {
                            isDragging = false
                            if let pt = map(v.location, in: geo.size) {
                                send(["kind": "up", "x": pt.x, "y": pt.y, "button": "left"])
                            }
                            return
                        }
                        let kind = Date().timeIntervalSince(pressStart) > 0.5 ? "right_click" : "click"
                        if let pt = map(v.location, in: geo.size) { send(["kind": kind, "x": pt.x, "y": pt.y]) }
                    }
            )
        }
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
        while !Task.isCancelled && streaming {
            if let st = try? await hub.fetchDesktopState(target) {
                if st.status?.screenRecording == "denied" {
                    streaming = false
                    error = hub.t("deskScreenRecordingDenied")
                } else if error == hub.t("deskScreenRecordingDenied") {
                    error = ""
                }
            }
            try? await Task.sleep(nanoseconds: 2_000_000_000)
        }
    }
}
