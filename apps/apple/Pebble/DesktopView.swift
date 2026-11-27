import SwiftUI

struct DesktopView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @Environment(\.hubCompact) private var compact

    private static let chatSidebarAnimation = Animation.spring(response: 0.28, dampingFraction: 0.88)

    var initialTarget: String
    var initialBotId: String? = nil
    var onClose: (() -> Void)? = nil

    @State private var target: String
    @State private var chatBotId: String
    @State private var showChatSidebar = true
    @State private var didInitSidebar = false
    @State private var showChatSheet = false

    @State private var streaming = true
    @State private var streamMode: StageDesktopCanvas.StreamMode = .wsHEVC
    @State private var fallbackImage: PlatformImage?
    @State private var frameFps = 0
    @State private var error = ""
    @State private var showText = false
    @State private var draft = ""
    @State private var streamCtrl = H264StreamController()
    @State private var showHiddenFiles = false
    @State private var filesRefreshSeq = 0
    @FocusState private var draftFocused: Bool

    init(initialTarget: String, initialBotId: String? = nil, onClose: (() -> Void)? = nil) {
        self.initialTarget = initialTarget
        self.initialBotId = initialBotId
        self.onClose = onClose
        _target = State(initialValue: initialTarget.isEmpty ? "local" : initialTarget)
        _chatBotId = State(initialValue: initialBotId ?? "")
    }

    private var isWide: Bool { !compact }

    private var resolvedBotId: String {
        if !chatBotId.isEmpty, hub.bots.contains(where: { $0.id == chatBotId }) { return chatBotId }
        if let sel = router.selected, hub.bots.contains(where: { $0.id == sel }) { return sel }
        return hub.bots.first?.id ?? ""
    }

    private var stageState: BotStageState {
        let cur = router.botStages[resolvedBotId] ?? BotStageState()
        return StageStateOps.ensureNonEmpty(cur, newTabTitle: hub.t("stageNewTab"))
    }

    private var activeTab: StageTab {
        stageState.activeTab ?? StageStateOps.newTab(title: hub.t("stageNewTab"))
    }

    private var hasDesktopTab: Bool {
        stageState.tabs.contains(where: { $0.kind == .desktop })
    }

    private var targets: [DesktopTarget] {
        var list = hub.desktopTargets.isEmpty
            ? [DesktopTarget(id: "local", name: hub.t("localDesktopName"), local: true, supported: true)]
            : hub.desktopTargets
        if !target.isEmpty, !list.contains(where: { $0.id == target }) {
            list.append(DesktopTarget(id: target, name: target, local: !target.contains(":virtual:") || target.hasPrefix("virtual:"), supported: true))
        }
        return list
    }

    private func setChatSidebar(_ open: Bool) {
        withAnimation(Self.chatSidebarAnimation) {
            showChatSidebar = open
        }
    }

    var body: some View {
        NavigationStack {
            HStack(spacing: 0) {
                if isWide && showChatSidebar {
                    sidebarChatView(closable: false)
                        .frame(width: 340)
                        .background(p.card, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
                        .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
                        .padding(.leading, 12)
                        .padding(.vertical, 12)
                        .transition(.move(edge: .leading).combined(with: .opacity))
                }

                VStack(spacing: 0) {
                    StageTabBar(
                        tabs: stageState.tabs,
                        activeTabId: activeTab.id,
                        inCard: isWide,
                        onSelect: { tabId in
                            router.selectStageTab(botId: resolvedBotId, tabId: tabId, newTabTitle: hub.t("stageNewTab"))
                        },
                        onClose: { tabId in
                            router.closeStageTab(botId: resolvedBotId, tabId: tabId, newTabTitle: hub.t("stageNewTab"))
                        },
                        onNewTab: {
                            let tab = StageStateOps.newTab(title: hub.t("stageNewTab"))
                            router.openStage(botId: resolvedBotId, tab: tab, replaceNew: false)
                        }
                    )

                    stageBody
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
                .frame(minWidth: 0, maxWidth: .infinity, maxHeight: .infinity)
                .background(isWide ? p.card : p.page, in: RoundedRectangle(cornerRadius: isWide ? 20 : 0, style: .continuous))
                .clipShape(RoundedRectangle(cornerRadius: isWide ? 20 : 0, style: .continuous))
                .padding(.leading, isWide ? 12 : 0)
                .padding(.trailing, isWide ? 12 : 0)
                .padding(.vertical, isWide ? 12 : 0)
            }
            .clipped()
            .background(p.page.ignoresSafeArea())
            .macSeamlessWindowToolbar()
            .navigationTitle("")
            .inlineNavTitle()
            .toolbar { stageToolbar }
        }
        .sheet(isPresented: $showChatSheet) {
            sidebarChatView(closable: true)
                .presentationDetents([.medium, .large])
        }
        .tint(p.accent)
        .task { _ = try? await hub.refreshDesktopTargets() }
        .onAppear {
            if !didInitSidebar {
                didInitSidebar = true
                showChatSidebar = !compact
            }
            if router.botStages[resolvedBotId]?.tabs.isEmpty ?? true {
                let tab = StageStateOps.newTab(title: hub.t("stageNewTab"))
                router.openStage(botId: resolvedBotId, tab: tab, replaceNew: false)
            }
            if let t = activeTab.target, !t.isEmpty {
                target = t
            }
            router.clearUnreadLatestStage(botId: resolvedBotId)
        }
        .onChange(of: activeTab.id) { _, _ in
            if activeTab.kind == .desktop, let t = activeTab.target, !t.isEmpty, target != t {
                target = t
            }
        }
        .onChange(of: resolvedBotId) { _, nextBotId in
            if router.botStages[nextBotId]?.tabs.isEmpty ?? true {
                let tab = StageStateOps.newTab(title: hub.t("stageNewTab"))
                router.openStage(botId: nextBotId, tab: tab, replaceNew: false)
            }
            router.clearUnreadLatestStage(botId: nextBotId)
        }
        .onChange(of: hub.supports("stage.files")) { _, canFiles in
            if !canFiles {
                router.stripFileTabs(newTabTitle: hub.t("stageNewTab"))
            }
        }
    }

    @ViewBuilder
    private var stageBody: some View {
        ZStack {
            if hasDesktopTab {
                StageDesktopCanvas(
                    isActive: activeTab.kind == .desktop,
                    resolvedBotId: resolvedBotId,
                    inWideStage: isWide,
                    target: $target,
                    streaming: $streaming,
                    showText: $showText,
                    draft: $draft,
                    streamMode: $streamMode,
                    fallbackImage: $fallbackImage,
                    frameFps: $frameFps,
                    error: $error,
                    streamCtrl: streamCtrl,
                    draftFocused: $draftFocused
                )
                .opacity(activeTab.kind == .desktop ? 1 : 0)
                .allowsHitTesting(activeTab.kind == .desktop)
            }

            if activeTab.kind == .new {
                StageNewTabView(
                    canUseFiles: hub.supports("stage.files"),
                    inWideStage: isWide,
                    onOpenFiles: {
                        let tab = StageStateOps.filesTab(title: hub.t("stageFilesTab"))
                        router.openStage(botId: resolvedBotId, tab: tab, replaceNew: true)
                    },
                    onOpenDesktop: {
                        let botTarget = hub.bots.first(where: { $0.id == resolvedBotId })?.desktopTarget
                        let dest = (botTarget?.isEmpty == false ? botTarget! : nil)
                            ?? (hub.activeDesktopTarget.target.isEmpty ? "local" : hub.activeDesktopTarget.target)
                        target = dest
                        let tab = StageStateOps.desktopTab(target: dest, title: hub.t("stageDesktopTab"))
                        router.openStage(botId: resolvedBotId, tab: tab, replaceNew: true)
                    }
                )
            } else if activeTab.kind == .files, hub.supports("stage.files") {
                WorkspaceFilesView(
                    botId: resolvedBotId,
                    initialDir: activeTab.path ?? "",
                    showHidden: $showHiddenFiles,
                    refreshTrigger: filesRefreshSeq,
                    inWideStage: isWide,
                    onOpenFile: { relPath in
                        let tab = StageStateOps.fileTab(path: relPath)
                        router.openStage(botId: resolvedBotId, tab: tab, replaceNew: false)
                    }
                )
            } else if activeTab.kind == .file, hub.supports("stage.files") {
                StageFilePreviewView(
                    botId: resolvedBotId,
                    filePath: activeTab.path ?? "",
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
            } else if activeTab.kind != .desktop {
                ContentUnavailableView(
                    activeTab.title,
                    systemImage: StageStateOps.symbolName(for: activeTab.kind, rawKind: activeTab.rawKind),
                    description: Text(hub.t("stagePlaceholderDesc", ["kind": activeTab.rawKind]))
                )
                .frame(maxWidth: .infinity, maxHeight: .infinity)
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

        ToolbarItemGroup(placement: .hubTrailing) {
            if activeTab.kind == .files {
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
            } else if activeTab.kind == .desktop {
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
                    Button(hub.t("keyEnter")) { sendDesktop(["kind": "key", "key": "Return"]) }
                    Button(hub.t("keyBackspace")) { sendDesktop(["kind": "key", "key": "Backspace"]) }
                    Button("Esc") { sendDesktop(["kind": "key", "key": "Escape"]) }
                    Button("Tab") { sendDesktop(["kind": "key", "key": "Tab"]) }
                    Divider()
                    Button(hub.t("keyScrollUp")) { sendDesktop(["kind": "scroll", "deltaY": 8]) }
                    Button(hub.t("keyScrollDown")) { sendDesktop(["kind": "scroll", "deltaY": -8]) }
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

    private func sendDesktop(_ body: [String: Any]) {
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

    @ViewBuilder
    private func sidebarChatView(closable: Bool) -> some View {
        if !resolvedBotId.isEmpty {
            ChatView(
                botId: resolvedBotId,
                inDesktopSidebar: true,
                onSelectBot: { nextId in
                    chatBotId = nextId
                    router.openBot(nextId)
                },
                onCloseSidebar: closable ? {
                    if isWide {
                        setChatSidebar(false)
                    } else {
                        showChatSheet = false
                    }
                } : nil
            )
            .id(resolvedBotId)
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
}
