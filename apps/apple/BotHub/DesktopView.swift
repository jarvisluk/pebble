import SwiftUI

struct DesktopView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @Environment(\.horizontalSizeClass) private var hsc

    var initialTarget: String
    var initialBotId: String? = nil
    var initialTab: StageKind = .desktop
    var initialPath: String? = nil

    @State private var activeTab: StageKind
    @State private var target: String
    @State private var streaming = true
    @State private var error = ""
    @State private var showText = false
    @State private var draft = ""
    @State private var pressStart = Date()
    @State private var isDragging = false
    @State private var lastDragSent = Date.distantPast
    @State private var streamCtrl = H264StreamController()
    #if os(macOS)
    @State private var showMacChatSidebar = false
    #else
    @State private var columnVisibility: NavigationSplitViewVisibility = .detailOnly
    #endif
    @State private var showChatSheet = false
    @State private var chatBotId: String
    @State private var showHiddenFiles = false
    @State private var filesRefreshSeq = 0
    @FocusState private var draftFocused: Bool

    private let stage = Color.black
    private let pillBg = Color(hex: "#1f1d1b")
    private let pillText = Color(hex: "#f3f1ee")
    private let pillMute = Color(hex: "#a6a09a")

    init(initialTarget: String, initialBotId: String? = nil, initialTab: StageKind = .desktop, initialPath: String? = nil) {
        self.initialTarget = initialTarget
        self.initialBotId = initialBotId
        self.initialTab = initialTab
        self.initialPath = initialPath
        _activeTab = State(initialValue: initialTab)
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

    private var isWide: Bool {
        #if os(macOS)
        return true
        #else
        return hsc != .compact
        #endif
    }

    var body: some View {
        Group {
            #if os(macOS)
            HStack(spacing: 0) {
                if showMacChatSidebar {
                    sidebarChatView(closable: true)
                        .frame(width: 340)
                    Rectangle().fill(p.s2).frame(width: 1)
                }
                stageContent
                    .frame(minWidth: 0, maxWidth: .infinity, maxHeight: .infinity)
            }
            #else
            if isWide {
                NavigationSplitView(columnVisibility: $columnVisibility) {
                    sidebarChatView(closable: false)
                        .navigationSplitViewColumnWidth(min: 320, ideal: 360, max: 420)
                } detail: {
                    NavigationStack {
                        stageContent
                    }
                }
            } else {
                NavigationStack {
                    stageContent
                }
                .sheet(isPresented: $showChatSheet) {
                    NavigationStack {
                        sidebarChatView(closable: true)
                    }
                    .presentationDetents([.medium, .large])
                }
            }
            #endif
        }
        .tint(activeTab == .desktop ? Color(hex: "#f3f1ee") : p.accent)
        .task { _ = try? await hub.refreshDesktopTargets() }
        .onChange(of: initialTab) { _, next in
            activeTab = next
        }
        .onChange(of: hub.activeDesktopTarget.target) { _, newTarget in
            let next = newTarget.isEmpty ? "local" : newTarget
            if target != next {
                target = next
            }
        }
        .onChange(of: target) { _, newTarget in
            let next = newTarget.isEmpty ? "local" : newTarget
            if hub.activeDesktopTarget.target != next {
                Task { try? await hub.setActiveDesktopTarget(next) }
            }
        }
        .task(id: "\(target)-\(streaming)-\(activeTab.rawValue)") {
            guard activeTab == .desktop else { return }
            try? await Task.sleep(nanoseconds: 80_000_000)
            guard !Task.isCancelled, activeTab == .desktop else { return }
            await pollState()
        }
        .task(id: "\(target)-\(streaming)-ws-\(activeTab.rawValue)") {
            if activeTab != .desktop || !streaming {
                streamCtrl.stop()
                return
            }
            try? await Task.sleep(nanoseconds: 80_000_000)
            guard !Task.isCancelled, activeTab == .desktop, streaming else { return }
            if let req = hub.makeDesktopWSRequest(target: target) {
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

    @ViewBuilder
    private var stageContent: some View {
        ZStack {
            WorkspaceFilesView(
                botId: resolvedBotId ?? "",
                initialPath: initialPath,
                showHidden: $showHiddenFiles,
                refreshTrigger: filesRefreshSeq,
                onQuotePath: { relPath in
                    hub.insertIntoChat("@\(relPath) ")
                    #if os(macOS)
                    showMacChatSidebar = true
                    #else
                    if isWide {
                        columnVisibility = .all
                    } else {
                        showChatSheet = true
                    }
                    #endif
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
        .navigationTitle("")
        .inlineNavTitle()
        .toolbar {
            #if os(iOS)
            ToolbarItem(placement: .topBarLeading) {
                Button { dismiss() } label: {
                    Label(hub.t("close"), systemImage: "xmark")
                }
            }
            #endif
            ToolbarItem(placement: .principal) {
                stageModePicker
            }
            ToolbarItemGroup(placement: .hubTrailing) {
                #if os(macOS)
                Button { showMacChatSidebar.toggle() } label: {
                    Label(hub.t("deskChatSidebar"), systemImage: "bubble.left.and.bubble.right")
                }
                #else
                if !isWide {
                    Button { showChatSheet = true } label: {
                        Label(hub.t("deskChatSidebar"), systemImage: "bubble.left.and.bubble.right")
                    }
                }
                #endif
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
                        Label(hub.t("aaInput"), systemImage: "keyboard").labelStyle(.iconOnly)
                    }
                    .menuIndicator(.hidden)

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
                        Label(hub.t("settings"), systemImage: "slider.horizontal.3").labelStyle(.iconOnly)
                    }
                    .menuIndicator(.hidden)
                }
            }
        }
    }

    private var stageModePicker: some View {
        Picker("", selection: $activeTab) {
            Text(hub.t("stageFilesTab")).tag(StageKind.files)
            Text(hub.t("stageDesktopTab")).tag(StageKind.desktop)
        }
        .pickerStyle(.segmented)
        .labelsHidden()
        .frame(maxWidth: 200)
    }

    private var desktopStage: some View {
        VStack(spacing: 0) {
            if hub.activeDesktopTarget.temporary {
                HStack(spacing: 10) {
                    Text(hub.t("tempDesktopBanner", ["name": hub.activeDesktopTarget.name ?? hub.activeDesktopTarget.target]))
                        .font(.system(size: 13, weight: .semibold)).foregroundStyle(pillText)
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
                    #if os(macOS)
                    showMacChatSidebar = false
                    #else
                    showChatSheet = false
                    #endif
                } : nil
            )
            .id(bid)
        } else {
            ContentUnavailableView(hub.t("noBotsYet"), systemImage: "bubble.left.and.bubble.right")
                .toolbar {
                    if closable {
                        ToolbarItem(placement: .cancellationAction) {
                            Button(hub.t("close")) {
                                #if os(macOS)
                                showMacChatSidebar = false
                                #else
                                showChatSheet = false
                                #endif
                            }
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
                #if os(macOS)
                H264DisplayView(
                    controller: streamCtrl,
                    mapPoint: { pt, size in map(pt, in: size) },
                    onInput: { send($0) }
                )
                #else
                H264DisplayView(controller: streamCtrl)
                #endif
                if !streamCtrl.connected {
                    VStack(spacing: 10) {
                        ProgressView().tint(p.accent)
                        Text(displayErr.isEmpty ? hub.t("deskH264Connecting") : displayErr)
                            .font(.system(size: 15)).foregroundStyle(pillMute).multilineTextAlignment(.center)
                        if !displayErr.isEmpty {
                            Button(hub.t("retry")) {
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
