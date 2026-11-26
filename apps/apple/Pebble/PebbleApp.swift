import SwiftUI

@main
struct PebbleApp: App {
    #if os(iOS)
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @State private var hub = HubStore()
    @State private var router = Router()

    var body: some Scene {
        WindowGroup {
            RootView()
                .hubTrackWindowVisibility()
                .hubApplyTheme(hub.themePref)
                .environment(hub)
                .environment(router)
        }
    }
    #elseif os(macOS)
    @NSApplicationDelegateAdaptor(MacAppDelegate.self) private var appDelegate
    @State private var hub = HubStore(autoConnect: false)
    @State private var host = DesktopHostManager.shared
    @State private var router = Router()

    var body: some Scene {
        WindowGroup("Pebble", id: "main") {
            MacWindowRootView(appDelegate: appDelegate)
                .frame(minWidth: 760, minHeight: 540)
                .environment(hub)
                .environment(host)
                .environment(router)
        }
        .defaultSize(width: 1220, height: 780)
        .commands {
            CommandGroup(replacing: .appSettings) {
                Button(hub.t("settings")) {
                    NotificationCenter.default.post(name: .hubOpenSettings, object: nil)
                }
                .keyboardShortcut(",", modifiers: .command)
            }
            CommandGroup(after: .newItem) {
                Button(hub.t("newBot")) {
                    NotificationCenter.default.post(name: .hubNewBot, object: nil)
                }
                .keyboardShortcut("n", modifiers: .command)

                Button(hub.t("searchBot")) {
                    NotificationCenter.default.post(name: .hubFocusSearch, object: nil)
                }
                .keyboardShortcut("k", modifiers: .command)
            }
        }

        MenuBarExtra {
            MacMenuBarContent()
                .environment(hub)
                .environment(host)
        } label: {
            MacMenuBarLabel(appDelegate: appDelegate)
        }
    }
    #endif
}

#if os(macOS)
private struct MacMenuBarLabel: View {
    var appDelegate: MacAppDelegate
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        Label("Pebble", image: "MenuBarIcon")
            .onAppear {
                appDelegate.onReopenWindow = {
                    openWindow(id: "main")
                }
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.15) {
                    let hasMain = NSApp.windows.contains { $0.canBecomeMain && $0.isVisible }
                    if !hasMain {
                        openWindow(id: "main")
                    }
                }
            }
    }
}
private struct MacWindowRootView: View {
    @Environment(HubStore.self) private var hub
    @Environment(DesktopHostManager.self) private var host
    @Environment(Router.self) private var router
    @Environment(\.openWindow) private var openWindow

    var appDelegate: MacAppDelegate
    @State private var showMacPermsUnbox = false
    @State private var checkingMacPerms = false

    private var showSetup: Bool {
        !host.ready || host.snapshot.mode.isEmpty || host.snapshot.activeUrl.isEmpty
    }

    private func checkAndShowMacPermsUnbox() async {
        guard !checkingMacPerms, !showMacPermsUnbox, !PermissionsManager.shared.unboxDone else { return }
        checkingMacPerms = true
        defer { checkingMacPerms = false }
        await PermissionsManager.shared.refresh()
        if PermissionsManager.shared.hasUngrantedCore {
            showMacPermsUnbox = true
        } else {
            PermissionsManager.shared.markUnboxDone()
        }
    }

    var body: some View {
        let isMacDesktopStage = router.desktop != nil && router.stageTab == .desktop
        let effectiveDark = PlatformAppearance.shared.resolvedDark(themePref: hub.themePref, forceDark: isMacDesktopStage)
        let effectivePalette = Palette(dark: effectiveDark)
        Group {
            if showSetup {
                MacSetupView()
            } else if showMacPermsUnbox {
                UnboxPermissionsView(onDone: { showMacPermsUnbox = false })
            } else {
                RootView()
            }
        }
        .environment(\.palette, effectivePalette)
        .tint(effectivePalette.accent)
        .font(.system(size: AppFont.body))
        .environment(\.colorScheme, effectiveDark ? .dark : .light)
        .hubApplyTheme(hub.themePref, forceDark: isMacDesktopStage)
        .hubTrackWindowVisibility()
        .macSeamlessWindowToolbar()
        .onAppear {
            appDelegate.onReopenWindow = {
                openWindow(id: "main")
            }
            host.startDaemonIfNeeded()
        }
        .onChange(of: host.ready) { _, ready in
            if ready {
                Task { await PermissionsManager.shared.refresh() }
            }
        }
        .onChange(of: showSetup) { wasSetup, isSetup in
            if isSetup {
                showMacPermsUnbox = false
            } else if wasSetup && !PermissionsManager.shared.unboxDone {
                Task { await checkAndShowMacPermsUnbox() }
            }
        }
        .onChange(of: hub.settings?.onboarding?.needsSetup) { _, needs in
            if needs == true && !showSetup && !PermissionsManager.shared.unboxDone {
                Task { await checkAndShowMacPermsUnbox() }
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: .hubOpenUnboxPermissions)) { _ in
            if !showSetup {
                showMacPermsUnbox = true
            }
        }
        .task(id: "\(host.snapshot.mode)|\(host.snapshot.activeUrl)|\(host.snapshot.activeToken)") {
            let snap = host.snapshot
            if !snap.mode.isEmpty && !snap.activeUrl.isEmpty {
                await hub.connectManagedEndpoint(
                    url: snap.activeUrl,
                    token: snap.activeToken,
                    isRemote: snap.mode == "remote"
                )
            } else {
                hub.disconnectManagedEndpoint()
                router.closeBot()
                router.desktop = nil
                router.sheet = nil
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: .hubOpenSettings)) { _ in
            if !showSetup && !showMacPermsUnbox {
                router.openSettings(compact: false)
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: .hubNewBot)) { _ in
            if !showSetup && !showMacPermsUnbox && hub.isConnected {
                router.sheet = .createBot
            }
        }
    }
}

private struct MacMenuBarContent: View {
    @Environment(HubStore.self) private var hub
    @Environment(DesktopHostManager.self) private var host
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        Button(hub.t("macOpenApp")) {
            NSApp.activate(ignoringOtherApps: true)
            openWindow(id: "main")
        }
        Button(hub.t("macSwitchMode")) {
            Task {
                await host.resetMode()
                NSApp.activate(ignoringOtherApps: true)
                openWindow(id: "main")
            }
        }
        if !host.snapshot.dataDir.isEmpty {
            Button(hub.t("macOpenDataDir")) {
                host.openDataDir()
            }
        }
        if host.snapshot.mode == "remote" {
            Divider()
            Toggle(hub.t("macAllowNode"), isOn: Binding(
                get: { host.snapshot.allowNode },
                set: { next in Task { await host.setLocalNode(enabled: next) } }
            ))
        }
        if host.snapshot.canFailover {
            Button(hub.t("macFailover")) {
                Task { await host.failoverToLocal() }
            }
        }
        if host.snapshot.canFailback {
            Button(hub.t("macFailback")) {
                Task { await host.failbackToRemote() }
            }
        }
        Divider()
        Button(hub.t("macQuit")) {
            host.shutdown()
            NSApp.terminate(nil)
        }
    }
}
#endif

enum SheetRoute: Identifiable, Equatable {
    case connect(scan: Bool)
    case createBot
    case botInfo(String)

    var id: String {
        switch self {
        case let .connect(scan): return "connect-\(scan)"
        case .createBot: return "create"
        case let .botInfo(botId): return "botInfo-\(botId)"
        }
    }
}

enum SettingsGroup: String, CaseIterable, Identifiable {
    case app
    case host

    var id: String { rawValue }

    var titleKey: String {
        switch self {
        case .app: return "settingsGroupApp"
        case .host: return "settingsGroupHost"
        }
    }

    var tabs: [SettingsTab] {
        switch self {
        case .app: return [.general]
        case .host: return [.models, .memory, .desktop, .remote]
        }
    }
}

enum SettingsTab: String, CaseIterable, Identifiable, Hashable {
    case general
    case models
    case memory
    case desktop
    case remote

    var id: String { rawValue }

    var group: SettingsGroup {
        switch self {
        case .general: return .app
        case .models, .memory, .desktop, .remote: return .host
        }
    }

    var titleKey: String {
        switch self {
        case .general: return "tabGeneral"
        case .models: return "tabModels"
        case .memory: return "tabMemory"
        case .desktop: return "tabDesktop"
        case .remote: return "tabRemote"
        }
    }

    var icon: String {
        switch self {
        case .general: return "gearshape"
        case .models: return "cpu"
        case .memory: return "brain"
        case .desktop: return "display"
        case .remote: return "network"
        }
    }
}

struct DesktopPresentation: Identifiable, Equatable {
    let id = UUID()
    var target: String
    var initialTab: StageKind = .desktop
    var botId: String? = nil
    var initialPath: String? = nil
}

@MainActor
@Observable
final class Router {
    var selected: String?
    var preferredColumn: NavigationSplitViewColumn = .sidebar
    var sheet: SheetRoute?
    var detailOpen = false
    var desktop: DesktopPresentation?
    var stageTab: StageKind = .files
    var inSettings = false
    var settingsTab: SettingsTab? = nil
    var settingsDirty = false
    var requestLeaveSettings = false
    var pendingSettingsTab: SettingsTab? = nil
    @ObservationIgnored var settingsSaveAction: (() async -> Bool)? = nil
    @ObservationIgnored var settingsDiscardAction: (() -> Void)? = nil

    func openBot(_ id: String) {
        if case .botInfo = sheet { sheet = nil }
        leaveSettings()
        selected = id
        preferredColumn = .detail
    }

    func closeBot() {
        detailOpen = false
        selected = nil
        preferredColumn = .sidebar
    }

    func openSettings(compact: Bool) {
        desktop = nil
        if compact {
            detailOpen = false
            inSettings = true
            settingsTab = nil
        } else {
            detailOpen = false
            inSettings = true
            if settingsTab == nil {
                settingsTab = .general
            }
            preferredColumn = .sidebar
        }
    }

    func selectSettingsTab(_ tab: SettingsTab) {
        if settingsTab == tab { return }
        if settingsDirty {
            pendingSettingsTab = tab
        } else {
            settingsTab = tab
        }
    }

    func clearSettingsDirty() {
        settingsDirty = false
        requestLeaveSettings = false
        pendingSettingsTab = nil
        settingsSaveAction = nil
        settingsDiscardAction = nil
    }

    func requestSettingsBack(fallback: (() -> Void)? = nil) {
        if settingsDirty {
            requestLeaveSettings = true
        } else if let fallback {
            fallback()
        } else {
            leaveSettings()
        }
    }

    func requestExitSettings() {
        requestSettingsBack()
    }

    func leaveSettings() {
        inSettings = false
        clearSettingsDirty()
        preferredColumn = selected == nil ? .sidebar : .detail
    }

    func openDesktop(_ target: String = "local") {
        let resolved = target.trimmingCharacters(in: .whitespacesAndNewlines)
        let dest = resolved.isEmpty ? "local" : resolved
        sheet = nil
        leaveSettings()
        detailOpen = false
        stageTab = .desktop
        desktop = DesktopPresentation(target: dest, initialTab: .desktop, botId: selected)
    }

    func openFiles(botId: String, path: String? = nil) {
        sheet = nil
        leaveSettings()
        detailOpen = false
        stageTab = .files
        desktop = DesktopPresentation(target: "local", initialTab: .files, botId: botId, initialPath: path)
    }

    func handleStageOpen(_ ev: StageOpenEvent) {
        let targetBot = ev.botId ?? selected
        if let bid = targetBot, !bid.isEmpty, bid != selected {
            openBot(bid)
        }
        let kind = StageKind(rawValue: ev.kind) ?? .files
        sheet = nil
        leaveSettings()
        detailOpen = false
        stageTab = kind
        desktop = DesktopPresentation(target: "local", initialTab: kind, botId: targetBot, initialPath: ev.path)
    }

    func closeStage() {
        withAnimation(.easeInOut(duration: 0.22)) {
            desktop = nil
        }
    }
}

struct RootView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.horizontalSizeClass) private var hsc
    @Environment(\.scenePhase) private var phase
    @Environment(\.hubWindowVisible) private var windowVisible

    private var showGate: Bool {
        if hub.managedMode {
            return hub.connState == .unauthorized || (hub.connState == .error && hub.bots.isEmpty)
        }
        return hub.activeServer == nil
            || hub.connState == .unauthorized
            || (hub.connState == .error && hub.bots.isEmpty)
            || (hub.connState == .connecting && hub.bots.isEmpty)
    }

    var body: some View {
        let dark = PlatformAppearance.shared.resolvedDark(themePref: hub.themePref)
        let palette = Palette(dark: dark)
        let resolvedScheme: ColorScheme = dark ? .dark : .light
        #if os(macOS)
        let compact = false
        let macStage = true
        #else
        let compact = hsc == .compact
        let macStage = false
        #endif
        @Bindable var r = router
        Group {
            if showGate {
                #if os(macOS)
                VStack(spacing: 14) {
                    StatusText(text: hub.connError.isEmpty ? hub.t("connFailed") : hub.connError)
                    HStack(spacing: 10) {
                        Button(hub.t("retry")) {
                            Task { await hub.reconnect() }
                        }
                        .buttonStyle(.borderedProminent)
                        Button(hub.t("macSwitchMode")) {
                            Task { await DesktopHostManager.shared.resetMode() }
                        }
                        .buttonStyle(.bordered)
                    }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(palette.page)
                #else
                ConnectView(startScan: false, isGate: true)
                #endif
            } else if macStage, let pres = router.desktop {
                DesktopView(
                    initialTarget: pres.target,
                    initialBotId: pres.botId ?? router.selected,
                    initialTab: pres.initialTab,
                    initialPath: pres.initialPath,
                    onTabChange: { next in
                        if router.stageTab != next { router.stageTab = next }
                    },
                    onClose: { router.closeStage() }
                )
                .id(pres.id)
                .transition(.opacity)
            } else {
                NavigationSplitView(preferredCompactColumn: $r.preferredColumn) {
                    Group {
                        if router.inSettings && !compact {
                            SettingsSidebarView(compact: compact)
                        } else {
                            BotListView(compact: compact)
                        }
                    }
                    .navigationSplitViewColumnWidth(min: 260, ideal: 300, max: 400)
                } detail: {
                    NavigationStack {
                        if router.inSettings && !compact {
                            SettingsView(tab: router.settingsTab ?? .general)
                        } else if let id = router.selected, hub.bots.contains(where: { $0.id == id }) {
                            ChatView(botId: id)
                                .id(id)
                                .hubMacTrailingPanel(isPresented: r.detailOpen, width: 360) {
                                    BotDetailView(botId: id, isPresented: $r.detailOpen)
                                        .id(id)
                                }
                                .transition(.opacity)
                        } else {
                            VStack(spacing: 8) {
                                Image(systemName: "bubble.left.and.bubble.right")
                                    .font(.system(size: 28))
                                    .foregroundStyle(palette.mute)
                                Text(hub.isConnected ? hub.t("selectBotHint") : hub.t("connectHost"))
                                    .font(.system(size: AppFont.title, weight: .semibold))
                                    .foregroundStyle(palette.text)
                                    .multilineTextAlignment(.center)
                            }
                            .padding(24)
                            .frame(maxWidth: .infinity, maxHeight: .infinity)
                            .background(palette.page.ignoresSafeArea())
                            .navigationTitle("")
                            .inlineNavTitle()
                        }
                    }
                    .animation(.easeInOut(duration: 0.2), value: router.selected)
                }
                .hubSplitInspector(isPresented: $r.detailOpen) {
                    if r.detailOpen, let id = router.selected, hub.bots.contains(where: { $0.id == id }) {
                        BotDetailView(botId: id, isPresented: $r.detailOpen)
                        .id(id)
                        .inspectorColumnWidth(min: 320, ideal: 360, max: 440)
                        .overlay(alignment: .leading) {
                            Rectangle()
                                .fill(platformSeparatorColor)
                                .frame(width: 0.5)
                                .ignoresSafeArea()
                        }
                        .environment(\.palette, palette)
                        .environment(\.hubCompact, compact)
                        .environment(\.hubWindowVisible, windowVisible)
                        .environment(router)
                        .environment(hub)
                        .tint(palette.accent)
                        .buttonBorderShape(.capsule)
                        .font(.system(size: AppFont.body))
                        .environment(\.colorScheme, resolvedScheme)
                    }
                }
            }
        }
        .tint(palette.accent)
        .buttonBorderShape(.capsule)
        .font(.system(size: AppFont.body))
        .environment(\.palette, palette)
        .environment(\.hubCompact, compact)
        .environment(\.colorScheme, resolvedScheme)
        .sheet(item: $r.sheet) { route in
            SheetHost(route: route)
                .environment(\.palette, palette)
                .environment(\.hubCompact, compact)
                .environment(\.hubWindowVisible, windowVisible)
                .environment(router)
                .environment(hub)
                .tint(palette.accent)
                .buttonBorderShape(.capsule)
                .font(.system(size: AppFont.body))
                .environment(\.colorScheme, resolvedScheme)
        }
        .onChange(of: router.selected) { _, id in
            if id == nil { router.detailOpen = false }
        }
        #if os(iOS)
        .fullScreenCover(item: $r.desktop) { pres in
            DesktopView(
                initialTarget: pres.target,
                initialBotId: pres.botId ?? router.selected,
                initialTab: pres.initialTab,
                initialPath: pres.initialPath
            )
            .environment(\.palette, palette)
            .environment(\.hubCompact, compact)
            .environment(\.hubWindowVisible, windowVisible)
            .environment(router)
            .environment(hub)
            .tint(palette.accent)
            .buttonBorderShape(.capsule)
            .environment(\.colorScheme, resolvedScheme)
        }
        #endif
        .onChange(of: hub.pendingStageOpen) { _, ev in
            guard let ev else { return }
            router.handleStageOpen(ev)
        }
        .onChange(of: showGate) { _, gate in
            if gate {
                router.sheet = nil
                router.desktop = nil
                router.closeBot()
            }
        }
        .onChange(of: phase) { old, new in
            if new == .active {
                if old == .background { Task { await hub.resume() } }
                #if os(iOS)
                if let pendingBotId = PebbleSharedStore.consumePendingInterrupt() {
                    Task { _ = try? await hub.interruptBot(pendingBotId) }
                }
                #endif
            }
        }
        .onAppear {
            PushManager.shared.onDeepLink = { url in
                handleDeepLink(url)
            }
        }
        .onOpenURL { url in
            handleDeepLink(url)
        }
    }

    private func handleDeepLink(_ url: URL) {
        guard let scheme = url.scheme?.lowercased(), scheme == "pebble" || scheme == "bothub" else { return }
        let host = (url.host ?? "").lowercased()
        let parts = url.pathComponents.filter { $0 != "/" }
        switch host {
        case "connect":
            #if os(iOS)
            if let q = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems,
               let link = q.first(where: { $0.name == "link" })?.value, !link.isEmpty {
                Task { try? await hub.addAndConnectServer(label: "", address: link, token: "") }
            }
            #else
            if let q = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems,
               let link = q.first(where: { $0.name == "link" })?.value, !link.isEmpty {
                Task { await DesktopHostManager.shared.startRemote(link: link) }
            }
            #endif
        case "bot":
            if let botId = parts.first, !botId.isEmpty {
                router.openBot(botId)
            }
        case "desktop":
            let target = URLComponents(url: url, resolvingAgainstBaseURL: false)?
                .queryItems?.first(where: { $0.name == "target" })?.value ?? "local"
            router.openDesktop(target)
        case "interrupt":
            if let botId = parts.first, !botId.isEmpty {
                Task { _ = try? await hub.interruptBot(botId) }
            }
        default:
            break
        }
    }
}

struct SheetHost: View {
    @Environment(\.palette) private var p
    var route: SheetRoute

    private var frameSize: (CGFloat, CGFloat) {
        switch route {
        case .createBot: return (520, 620)
        case .botInfo: return (520, 640)
        case .connect: return (480, 540)
        }
    }

    var body: some View {
        Group {
            switch route {
            case let .connect(scan): ConnectView(startScan: scan)
            case .createBot: CreateBotView()
            case let .botInfo(botId): BotInfoSheet(botId: botId)
            }
        }
        .hubSheetFrame(width: frameSize.0, height: frameSize.1)
        .background(p.page)
    }
}
