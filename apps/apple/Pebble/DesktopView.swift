import SwiftUI

struct DesktopView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @Environment(\.colorScheme) private var scheme
    @Environment(\.dismiss) private var dismiss
    @Environment(\.hubCompact) private var compact
    @Environment(\.hubWindowVisible) private var windowVisible

    private static let chatSidebarAnimation = Animation.spring(response: 0.28, dampingFraction: 0.88)

    var initialTarget: String
    var initialBotId: String? = nil
    var initialTab: StageKind = .desktop
    var initialPath: String? = nil
    var onTabChange: ((StageKind) -> Void)? = nil
    var onClose: (() -> Void)? = nil

    private enum StreamMode: Equatable {
        case wsHEVC
        case wsH264
        case framePolling
    }

    @State private var activeTab: StageKind
    @State private var target: String
    @State private var streaming = true
    @State private var streamMode: StreamMode = .wsHEVC
    @State private var fallbackImage: PlatformImage?
    @State private var fallbackMeta: H264StreamMeta?
    @State private var frameFps = 0
    @State private var error = ""
    @State private var showText = false
    @State private var draft = ""
    @State private var pressStart = Date()
    @State private var isDragging = false
    @State private var lastDragSent = Date.distantPast
    @State private var streamCtrl = H264StreamController()
    @State private var showChatSidebar = false
    @State private var showChatSheet = false
    @State private var chatBotId: String
    @State private var showHiddenFiles = false
    @State private var filesRefreshSeq = 0
    @FocusState private var draftFocused: Bool

    private let stage = Color.black
    private let pillBg = Color(hex: "#1f1d1b")
    private let pillText = Color(hex: "#f3f1ee")
    private let pillMute = Color(hex: "#a6a09a")

    init(initialTarget: String, initialBotId: String? = nil, initialTab: StageKind = .desktop, initialPath: String? = nil, onTabChange: ((StageKind) -> Void)? = nil, onClose: (() -> Void)? = nil) {
        self.initialTarget = initialTarget
        self.initialBotId = initialBotId
        self.initialTab = initialTab
        self.initialPath = initialPath
        self.onTabChange = onTabChange
        self.onClose = onClose
        _activeTab = State(initialValue: initialTab)
        _target = State(initialValue: initialTarget.isEmpty ? "local" : initialTarget)
        _chatBotId = State(initialValue: initialBotId ?? "")
    }

    private var targets: [DesktopTarget] {
        var list = hub.desktopTargets.isEmpty ? [DesktopTarget(id: "local", name: hub.t("localDesktopName"), local: true, supported: true)] : hub.desktopTargets
        if !target.isEmpty, !list.contains(where: { $0.id == target }) {
            list.append(DesktopTarget(id: target, name: target, local: !target.contains(":virtual:") || target.hasPrefix("virtual:"), supported: true))
        }
        return list
    }

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

    private var resolvedBotId: String? {
        if !chatBotId.isEmpty, hub.bots.contains(where: { $0.id == chatBotId }) { return chatBotId }
        if let sel = router.selected, hub.bots.contains(where: { $0.id == sel }) { return sel }
        return hub.bots.first?.id
    }

    private var isWide: Bool { !compact }

    private var preferredStreamMode: StreamMode {
        if !HubStore.isLocalDesktopTarget(target) || !hub.supports("desktop.ws_h264") {
            return .framePolling
        }
        if !hub.supports("desktop.ws_hevc") {
            return .wsH264
        }
        return .wsHEVC
    }

    private func setChatSidebar(_ open: Bool) {
        withAnimation(Self.chatSidebarAnimation) {
            showChatSidebar = open
        }
    }

    var body: some View {
        let effectivePalette = activeTab == .desktop ? Palette(dark: true) : p
        NavigationStack {
            HStack(spacing: 0) {
                if isWide && showChatSidebar {
                    sidebarChatView(closable: false)
                        .frame(width: 340)
                        .background(effectivePalette.card, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
                        .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
                        .padding(.leading, 12)
                        .padding(.vertical, 12)
                        .padding(.trailing, activeTab == .desktop ? 12 : 0)
                        .environment(\.palette, effectivePalette)
                        .environment(\.colorScheme, activeTab == .desktop ? .dark : scheme)
                        .transition(.move(edge: .leading).combined(with: .opacity))
                }
                stageContent
                    .frame(minWidth: 0, maxWidth: .infinity, maxHeight: .infinity)
            }
            .clipped()
            .background((activeTab == .desktop ? stage : p.page).ignoresSafeArea())
            .macSeamlessWindowToolbar()
            .navigationTitle("")
            .inlineNavTitle()
            .toolbar { stageToolbar }
        }
        .environment(\.colorScheme, activeTab == .desktop ? .dark : scheme)
        .sheet(isPresented: $showChatSheet) {
            sidebarChatView(closable: true)
                .presentationDetents([.medium, .large])
        }
        .tint(activeTab == .desktop ? Color(hex: "#f3f1ee") : p.accent)
        .task { _ = try? await hub.refreshDesktopTargets() }
        .onAppear {
            if !hub.supports("stage.files") && activeTab == .files {
                activeTab = .desktop
            }
            streamMode = preferredStreamMode
            onTabChange?(activeTab)
        }
        .onChange(of: activeTab) { _, next in
            onTabChange?(next)
        }
        .onChange(of: hub.supports("stage.files")) { _, canFiles in
            if !canFiles && activeTab == .files {
                activeTab = .desktop
            }
        }
        .onChange(of: initialTab) { _, next in
            activeTab = (!hub.supports("stage.files") && next == .files) ? .desktop : next
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
        .task(id: "\(target)-\(streaming)-\(activeTab.rawValue)-\(windowVisible)") {
            guard activeTab == .desktop, windowVisible else { return }
            try? await Task.sleep(nanoseconds: 80_000_000)
            guard !Task.isCancelled, activeTab == .desktop, windowVisible else { return }
            await pollState()
        }
        .task(id: "\(target)-\(streaming)-\(streamMode)-\(activeTab.rawValue)-\(windowVisible)") {
            if activeTab != .desktop || !streaming || !windowVisible {
                streamCtrl.stop()
                return
            }
            try? await Task.sleep(nanoseconds: 80_000_000)
            guard !Task.isCancelled, activeTab == .desktop, streaming, windowVisible else { return }

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
            guard activeTab == .desktop, streaming, windowVisible else { return }
            if streamMode == .wsHEVC {
                streamMode = .wsH264
            } else if streamMode == .wsH264 {
                streamMode = .framePolling
            }
        }
    }

    @ViewBuilder
    private var stageContent: some View {
        ZStack {
            WorkspaceFilesView(
                botId: resolvedBotId ?? "",
                initialPath: initialPath,
                showHidden: $showHiddenFiles,
                refreshTrigger: filesRefreshSeq,
                inWideStage: isWide,
                onQuotePath: { relPath in
                    hub.insertIntoChat("@\(relPath) ")
                    if isWide {
                        setChatSidebar(true)
                    } else {
                        showChatSheet = true
                    }
                }
            )
            .opacity(activeTab == .files ? 1 : 0)
            .allowsHitTesting(activeTab == .files)

            if activeTab == .desktop {
                desktopStage
                    .environment(\.palette, Palette(dark: true))
                    .environment(\.colorScheme, .dark)
            } else if activeTab != .files {
                ContentUnavailableView(
                    activeTab.rawValue,
                    systemImage: "square.grid.2x2",
                    description: Text(hub.t("stagePlaceholderDesc", ["kind": activeTab.rawValue]))
                )
                .background(p.page)
            }
        }
    }

    @ToolbarContentBuilder
    private var stageToolbar: some ToolbarContent {
        HubSplitLeadingToolbar {
            Button {
                if let onClose { onClose() } else { dismiss() }
            } label: {
                Label(isWide ? hub.t("backToChat") : hub.t("close"), systemImage: isWide ? "chevron.backward" : "xmark")
                    .labelStyle(.iconOnly)
            }
            .help(isWide ? hub.t("backToChat") : hub.t("close"))
            .accessibilityLabel(isWide ? hub.t("backToChat") : hub.t("close"))
        } second: {
            Button {
                if isWide {
                    setChatSidebar(!showChatSidebar)
                } else {
                    showChatSheet = true
                }
            } label: {
                let isOpen = isWide ? showChatSidebar : showChatSheet
                Label(
                    hub.t("deskChatSidebar"),
                    systemImage: isOpen ? "bubble.left.and.bubble.right.fill" : "bubble.left.and.bubble.right"
                )
                .labelStyle(.iconOnly)
            }
            .help(hub.t("deskChatSidebar"))
            .accessibilityLabel(hub.t("deskChatSidebar"))
        }
        ToolbarItem(placement: .principal) {
            stageModePicker
        }
        ToolbarItemGroup(placement: .hubTrailing) {
            if activeTab == .files {
                Menu {
                    Toggle(isOn: $showHiddenFiles) {
                        Label(hub.t("showHiddenFiles"), systemImage: "eye")
                    }
                    Button {
                        filesRefreshSeq += 1
                    } label: {
                        Label(hub.t("refreshFiles"), systemImage: "arrow.clockwise")
                    }
                } label: {
                    Label(hub.t("more"), systemImage: "ellipsis.circle").labelStyle(.iconOnly)
                }
                .menuIndicator(.hidden)
            } else if activeTab == .desktop {
                if !compact {
                    Button { streaming.toggle() } label: {
                        Label(streaming ? hub.t("collapse") : hub.t("connect"), systemImage: streaming ? "pause.fill" : "play.fill")
                    }
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
                    Label(hub.t("aaInput"), systemImage: "keyboard").labelStyle(.iconOnly)
                }
                .menuIndicator(.hidden)

                Menu {
                    if compact {
                        Button { streaming.toggle() } label: {
                            Label(streaming ? hub.t("collapse") : hub.t("connect"), systemImage: streaming ? "pause.fill" : "play.fill")
                        }
                        Divider()
                    }
                    if streamMode == .framePolling && fallbackImage != nil {
                        Section {
                            Text(hub.t("deskFrameModeLabel", ["fps": "\(frameFps)"]))
                        }
                    } else if streamCtrl.connected {
                        let codecLabel = streamCtrl.meta?.codec == "hevc" ? "HEVC" : "H.264"
                        Section {
                            Text("\(codecLabel) · \(streamCtrl.fps) fps")
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
                    Label(hub.t("settings"), systemImage: "slider.horizontal.3").labelStyle(.iconOnly)
                }
                .menuIndicator(.hidden)
            }
        }
    }

    @ViewBuilder
    private var stageModePicker: some View {
        if hub.supports("stage.files") {
            Picker("", selection: $activeTab) {
                Text(hub.t("stageFilesTab")).tag(StageKind.files)
                Text(hub.t("stageDesktopTab")).tag(StageKind.desktop)
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .fixedSize()
        } else {
            Text(hub.t("stageDesktopTab"))
                .font(.system(size: AppFont.cardTitle, weight: .semibold))
        }
    }

    private var desktopStage: some View {
        VStack(spacing: 0) {
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
        .background(stage.ignoresSafeArea())
    }

    @ViewBuilder
    private var desktopTextBar: some View {
        let row = HStack(alignment: .bottom, spacing: 10) {
            Button {
                showText = false
                draftFocused = false
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
                .focused($draftFocused)
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
            .composerSendButton(accent: p.accent)
            .disabled(draft.isEmpty)
        }

        row.glassRowContainer(spacing: 10)
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
                onCloseSidebar: closable ? {
                    if isWide {
                        setChatSidebar(false)
                    } else {
                        showChatSheet = false
                    }
                } : nil
            )
            .id(bid)
        } else {
            ContentUnavailableView(hub.t("noBotsYet"), systemImage: "bubble.left.and.bubble.right")
                .toolbar {
                    if closable {
                        ToolbarItem(placement: .cancellationAction) {
                            Button(hub.t("close")) {
                                if isWide {
                                    setChatSidebar(false)
                                } else {
                                    showChatSheet = false
                                }
                            }
                        }
                    }
                }
        }
    }

    private var stageView: some View {
        GeometryReader { geo in
            let isFrameReady = streamMode == .framePolling && fallbackImage != nil
            let isDisplayReady = isFrameReady || streamCtrl.connected
            let displayErr = !error.isEmpty ? error : (streamMode == .framePolling ? "" : streamCtrl.error)
            ZStack {
                stage
                #if os(macOS)
                H264DisplayView(
                    controller: streamCtrl,
                    mapPoint: { pt, size in map(pt, in: size) },
                    onInput: { send($0) }
                )
                #else
                H264DisplayView(controller: streamCtrl)
                #endif
                if streamMode == .framePolling, let img = fallbackImage {
                    Image(platform: img)
                        .resizable()
                        .aspectRatio(contentMode: .fit)
                        .allowsHitTesting(false)
                }
                if !isDisplayReady {
                    VStack(spacing: 10) {
                        ProgressView().tint(p.accent)
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
            .contentShape(Rectangle())
            #if os(iOS)
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
            #endif
        }
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

    private func pollFrameLoop() async {
        var consecErrors = 0
        var windowFrames = 0
        var fpsWindowStart = Date()
        while !Task.isCancelled && streaming && windowVisible && activeTab == .desktop && streamMode == .framePolling {
            let t0 = Date()
            var sleepNs: UInt64 = 350_000_000
            do {
                let fr = try await hub.fetchDesktopFrame(target)
                if Task.isCancelled || !streaming || !windowVisible || activeTab != .desktop || streamMode != .framePolling {
                    break
                }
                let decoded: PlatformImage? = await Task.detached(priority: .userInitiated) {
                    guard !fr.jpegBase64.isEmpty,
                          let data = Data(base64Encoded: fr.jpegBase64) else { return nil }
                    return PlatformImage(data: data)
                }.value
                if Task.isCancelled || !streaming || !windowVisible || activeTab != .desktop || streamMode != .framePolling {
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
        while !Task.isCancelled && streaming && windowVisible {
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
