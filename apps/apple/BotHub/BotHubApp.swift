import SwiftUI

@main
struct BotHubApp: App {
    #if os(iOS)
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @State private var hub = HubStore()
    @State private var router = Router()

    var body: some Scene {
        WindowGroup {
            RootView()
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
        Label("Pebble", systemImage: "bubble.left.and.bubble.right.fill")
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
    @Environment(\.colorScheme) private var scheme

    var appDelegate: MacAppDelegate

    private var scheme0: ColorScheme? {
        hub.themePref == "light" ? .light : hub.themePref == "dark" ? .dark : nil
    }

    private var activePalette: Palette {
        let dark = hub.themePref == "dark" || (hub.themePref == "system" && scheme == .dark)
        return Palette(dark: dark)
    }

    private var showSetup: Bool {
        !host.ready || host.snapshot.mode.isEmpty || host.snapshot.activeUrl.isEmpty
    }

    var body: some View {
        let isMacDesktopStage = router.desktop != nil && router.stageTab == .desktop
        let effectivePalette = isMacDesktopStage ? Palette(dark: true) : activePalette
        Group {
            if showSetup {
                MacSetupView()
            } else {
                RootView()
            }
        }
        .environment(\.palette, effectivePalette)
        .tint(effectivePalette.accent)
        .font(.system(size: AppFont.body))
        .preferredColorScheme(isMacDesktopStage ? .dark : scheme0)
        .macSeamlessWindowToolbar()
        .onAppear {
            appDelegate.onReopenWindow = {
                openWindow(id: "main")
            }
            host.startDaemonIfNeeded()
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
            if !showSetup {
                router.openSettings(compact: false)
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: .hubNewBot)) { _ in
            if !showSetup && hub.isConnected {
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
    case devices

    var id: String {
        switch self {
        case let .connect(scan): return "connect-\(scan)"
        case .createBot: return "create"
        case .devices: return "devices"
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

    func openBot(_ id: String) {
        inSettings = false
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

    func leaveSettings() {
        inSettings = false
        settingsDirty = false
        requestLeaveSettings = false
        preferredColumn = selected == nil ? .sidebar : .detail
    }

    func openDesktop(_ target: String = "local") {
        let resolved = target.trimmingCharacters(in: .whitespacesAndNewlines)
        let dest = resolved.isEmpty ? "local" : resolved
        sheet = nil
        inSettings = false
        detailOpen = false
        stageTab = .desktop
        desktop = DesktopPresentation(target: dest, initialTab: .desktop, botId: selected)
    }

    func openFiles(botId: String, path: String? = nil) {
        sheet = nil
        inSettings = false
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
        inSettings = false
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
    @Environment(\.colorScheme) private var scheme
    @Environment(\.horizontalSizeClass) private var hsc
    @Environment(\.scenePhase) private var phase

    private var scheme0: ColorScheme? { hub.themePref == "light" ? .light : hub.themePref == "dark" ? .dark : nil }

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
        let dark = hub.themePref == "dark" || (hub.themePref == "system" && scheme == .dark)
        let palette = Palette(dark: dark)
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
                .macSeamlessWindowToolbar()
                .inspector(isPresented: $r.detailOpen) {
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
                        .environment(router)
                        .environment(hub)
                        .tint(palette.accent)
                        .buttonBorderShape(.capsule)
                        .font(.system(size: AppFont.body))
                        .preferredColorScheme(scheme0)
                    }
                }
            }
        }
        .tint(palette.accent)
        .buttonBorderShape(.capsule)
        .font(.system(size: AppFont.body))
        .environment(\.palette, palette)
        .environment(\.hubCompact, compact)
        .preferredColorScheme(scheme0)
        .sheet(item: $r.sheet) { route in
            SheetHost(route: route)
                .environment(\.palette, palette)
                .environment(\.hubCompact, compact)
                .environment(router)
                .environment(hub)
                .tint(palette.accent)
                .buttonBorderShape(.capsule)
                .font(.system(size: AppFont.body))
                .preferredColorScheme(scheme0)
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
            .environment(router)
            .environment(hub)
            .tint(palette.accent)
            .buttonBorderShape(.capsule)
            .preferredColorScheme(scheme0)
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
                if let pendingBotId = BotHubSharedStore.consumePendingInterrupt() {
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
        guard url.scheme?.lowercased() == "bothub" else { return }
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

    var body: some View {
        Group {
            switch route {
            case let .connect(scan): ConnectView(startScan: scan)
            case .createBot: CreateBotView()
            case .devices: DevicesView()
            }
        }
        .hubSheetFrame(
            width: route == .createBot || route == .devices ? 520 : 480,
            height: route == .createBot ? 620 : route == .devices ? 600 : 540
        )
        .background(p.page)
    }
}
