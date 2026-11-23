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
    @State private var showChat = false
    @State private var chatBotId: String

    private let stage = Color(hex: "#111110")
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
        HStack(spacing: 0) {
            desktopColumn
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(stage)

            if isWide, showChat {
                Divider()
                sidebarChatView
                    .frame(width: 360)
                    .transition(.move(edge: .trailing))
            }
        }
        .animation(.easeOut(duration: 0.22), value: showChat)
        .background(stage.ignoresSafeArea())
        .sheet(isPresented: Binding(
            get: { !isWide && showChat },
            set: { if !$0 { showChat = false } }
        )) {
            sidebarChatView
                .presentationDetents([.medium, .large])
        }
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

    private var desktopColumn: some View {
        VStack(spacing: 8) {
            topBar
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
            }
            stageView
            controls
        }
        .padding(.top, 8)
    }

    private var topBar: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                Button { dismiss() } label: {
                    Image(systemName: "xmark")
                        .font(.system(size: 14, weight: .semibold))
                        .foregroundStyle(pillText)
                        .frame(width: 32, height: 32)
                        .background(pillBg, in: Circle())
                }

                if targets.count > 1 {
                    Menu {
                        ForEach(targets) { tr in
                            Button {
                                target = tr.id
                            } label: {
                                Label(tr.name ?? tr.id, systemImage: tr.id == target ? "checkmark" : "desktopcomputer")
                            }
                        }
                    } label: {
                        HStack(spacing: 4) {
                            Text(targets.first(where: { $0.id == target })?.name ?? target)
                            Image(systemName: "chevron.down").font(.system(size: 11))
                        }
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(pillText)
                        .padding(.horizontal, 12).frame(height: 32)
                        .background(pillBg, in: Capsule())
                    }
                }

                if streamCtrl.connected {
                    Text("H.264 · \(streamCtrl.fps) fps")
                        .font(.system(size: 12, design: .monospaced))
                        .foregroundStyle(pillMute)
                        .padding(.horizontal, 10).frame(height: 32)
                        .background(pillBg, in: Capsule())
                }

                Button { streaming.toggle() } label: {
                    Image(systemName: streaming ? "pause.fill" : "play.fill")
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(pillText)
                        .frame(width: 32, height: 32)
                        .background(pillBg, in: Circle())
                }

                Button { showChat.toggle() } label: {
                    HStack(spacing: 5) {
                        Image(systemName: "bubble.left.and.bubble.right")
                            .font(.system(size: 13, weight: .semibold))
                        Text(hub.t("deskChatSidebar"))
                            .font(.system(size: 13, weight: .semibold))
                    }
                    .foregroundStyle(showChat ? Color(hex: "#171717") : pillText)
                    .padding(.horizontal, 12).frame(height: 32)
                    .background(showChat ? pillText : pillBg, in: Capsule())
                }
            }
            .padding(.horizontal, 12)
        }
    }

    @ViewBuilder
    private var sidebarChatView: some View {
        NavigationStack {
            if let bid = resolvedBotId {
                ChatView(
                    botId: bid,
                    inDesktopSidebar: true,
                    onSelectBot: { nextId in
                        chatBotId = nextId
                    },
                    onCloseSidebar: {
                        showChat = false
                    }
                )
                .id(bid)
            } else {
                ContentUnavailableView(hub.t("noBotsYet"), systemImage: "bubble.left.and.bubble.right")
                    .toolbar {
                        ToolbarItem(placement: .cancellationAction) {
                            Button(hub.t("close")) { showChat = false }
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
        .padding(.bottom, 8)
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
