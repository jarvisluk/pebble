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

        WindowGroup(hub.t("remoteDesktop"), id: "desktop", for: DesktopWindowRequest.self) { $req in
            MacDesktopWindowRootView(request: req ?? DesktopWindowRequest(target: "local"))
                .frame(minWidth: 720, minHeight: 480)
                .environment(hub)
                .environment(router)
        }
        .defaultSize(width: 1180, height: 760)

        MenuBarExtra {
            MacMenuBarContent()
                .environment(hub)
                .environment(host)
        } label: {
            MacMenuBarLabel(appDelegate: appDelegate)
                .environment(router)
        }
    }
    #endif
}

#if os(macOS)
struct DesktopWindowRequest: Codable, Hashable {
    var target: String
    var initialTab: StageKind = .desktop
    var botId: String? = nil
    var initialPath: String? = nil
}

private struct MacMenuBarLabel: View {
    @Environment(Router.self) private var router
    @Environment(\.openWindow) private var openWindow
    var appDelegate: MacAppDelegate

    var body: some View {
        Label("Pebble", systemImage: "bubble.left.and.bubble.right.fill")
            .onAppear {
                router.onOpenDesktopWindow = { req in
                    openWindow(id: "desktop", value: req)
                }
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
        Group {
            if showSetup {
                MacSetupView()
            } else {
                RootView()
            }
        }
        .environment(\.palette, activePalette)
        .tint(activePalette.accent)
        .font(.system(size: AppFont.body))
        .preferredColorScheme(scheme0)
        .macSeamlessWindowToolbar()
        .onAppear {
            router.onOpenDesktopWindow = { req in
                openWindow(id: "desktop", value: req)
            }
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

private struct MacDesktopWindowRootView: View {
    var request: DesktopWindowRequest
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.colorScheme) private var scheme
    @State private var currentTab: StageKind

    init(request: DesktopWindowRequest) {
        self.request = request
        _currentTab = State(initialValue: request.initialTab)
    }

    private var scheme0: ColorScheme? {
        hub.themePref == "light" ? .light : hub.themePref == "dark" ? .dark : nil
    }

    private var activePalette: Palette {
        let dark = hub.themePref == "dark" || (hub.themePref == "system" && scheme == .dark)
        return Palette(dark: dark)
    }

    var body: some View {
        let isDesktop = currentTab == .desktop
        let effectivePalette = isDesktop ? Palette(dark: true) : activePalette
        DesktopView(
            initialTarget: request.target,
            initialBotId: request.botId ?? router.selected,
            initialTab: request.initialTab,
            initialPath: request.initialPath,
            onTabChange: { next in
                if currentTab != next {
                    currentTab = next
                }
            }
        )
        .environment(\.palette, effectivePalette)
        .tint(effectivePalette.accent)
        .font(.system(size: AppFont.body))
        .preferredColorScheme(isDesktop ? .dark : scheme0)
        .macSeamlessWindowToolbar()
        .onChange(of: request.initialTab) { _, next in
            currentTab = next
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
    var inSettings = false
    var settingsTab: SettingsTab? = nil
    var settingsDirty = false
    var requestLeaveSettings = false
    #if os(macOS)
    @ObservationIgnored var onOpenDesktopWindow: ((DesktopWindowRequest) -> Void)?
    #endif

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
        if compact {
            detailOpen = false
            inSettings = true
            settingsTab = nil
        } else {
            withAnimation(.easeInOut(duration: 0.3)) {
                detailOpen = false
                inSettings = true
                settingsTab = .general
                preferredColumn = .sidebar
            }
        }
    }

    func leaveSettings() {
        withAnimation(.easeInOut(duration: 0.3)) {
            inSettings = false
            settingsDirty = false
            requestLeaveSettings = false
            preferredColumn = selected == nil ? .sidebar : .detail
        }
    }

    func openDesktop(_ target: String = "local") {
        let resolved = target.trimmingCharacters(in: .whitespacesAndNewlines)
        let dest = resolved.isEmpty ? "local" : resolved
        sheet = nil
        #if os(macOS)
        if let onOpenDesktopWindow {
            onOpenDesktopWindow(DesktopWindowRequest(target: dest, initialTab: .desktop, botId: selected))
            return
        }
        #endif
        desktop = DesktopPresentation(target: dest, initialTab: .desktop, botId: selected)
    }

    func openFiles(botId: String, path: String? = nil) {
        sheet = nil
        #if os(macOS)
        if let onOpenDesktopWindow {
            onOpenDesktopWindow(DesktopWindowRequest(target: "local", initialTab: .files, botId: botId, initialPath: path))
            return
        }
        #endif
        desktop = DesktopPresentation(target: "local", initialTab: .files, botId: botId, initialPath: path)
    }

    func handleStageOpen(_ ev: StageOpenEvent) {
        let targetBot = ev.botId ?? selected
        if let bid = targetBot, !bid.isEmpty, bid != selected {
            openBot(bid)
        }
        let kind = StageKind(rawValue: ev.kind) ?? .files
        sheet = nil
        #if os(macOS)
        if let onOpenDesktopWindow {
            onOpenDesktopWindow(DesktopWindowRequest(target: "local", initialTab: kind, botId: targetBot, initialPath: ev.path))
            return
        }
        #endif
        desktop = DesktopPresentation(target: "local", initialTab: kind, botId: targetBot, initialPath: ev.path)
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
        #else
        let compact = hsc == .compact
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
                NavigationStack {
                    ConnectView(startScan: false, isGate: true)
                }
                #endif
            } else if router.inSettings && !compact {
                NavigationSplitView(preferredCompactColumn: $r.preferredColumn) {
                    SettingsSidebarView(compact: compact)
                        .navigationSplitViewColumnWidth(min: 280, ideal: 320, max: 380)
                } detail: {
                    NavigationStack {
                        SettingsView(tab: router.settingsTab ?? .general)
                    }
                }
                #if os(macOS)
                .macSeamlessWindowToolbar()
                #endif
                .transition(.opacity)
            } else {
                NavigationSplitView(preferredCompactColumn: $r.preferredColumn) {
                    BotListView(compact: compact)
                        #if os(macOS)
                        .navigationSplitViewColumnWidth(min: 220, ideal: 250, max: 310)
                        #else
                        .navigationSplitViewColumnWidth(min: 210, ideal: 240, max: 300)
                        #endif
                } detail: {
                    NavigationStack {
                        if let id = router.selected, hub.bots.contains(where: { $0.id == id }) {
                            ChatView(botId: id)
                                .id(id)
                                .transition(.opacity)
                        } else {
                            ContentUnavailableView(hub.isConnected ? hub.t("selectBotHint") : hub.t("connectHost"),
                                                   systemImage: "bubble.left.and.bubble.right")
                                .frame(maxWidth: .infinity, maxHeight: .infinity)
                                .background(palette.page.ignoresSafeArea())
                        }
                    }
                    .animation(.easeInOut(duration: 0.2), value: router.selected)
                }
                #if os(macOS)
                .macSeamlessWindowToolbar()
                #endif
                .transition(.opacity)
                .inspector(isPresented: $r.detailOpen) {
                    if r.detailOpen, let id = router.selected, hub.bots.contains(where: { $0.id == id }) {
                        #if os(iOS)
                        NavigationStack {
                            BotDetailView(botId: id, isPresented: $r.detailOpen)
                        }
                        .id(id)
                        .inspectorColumnWidth(min: 340, ideal: 380, max: 460)
                        .overlay(alignment: .leading) {
                            Rectangle()
                                .fill(platformSeparatorColor)
                                .frame(width: 0.5)
                                .ignoresSafeArea()
                        }
                        .environment(\.palette, palette)
                        .environment(router)
                        .environment(hub)
                        .tint(palette.accent)
                        .buttonBorderShape(.capsule)
                        .font(.system(size: AppFont.body))
                        .preferredColorScheme(scheme0)
                        #else
                        BotDetailView(botId: id, isPresented: $r.detailOpen)
                            .id(id)
                            .inspectorColumnWidth(min: 280, ideal: 320, max: 400)
                            .background(palette.page.ignoresSafeArea())
                            .environment(\.palette, palette)
                            .environment(router)
                            .environment(hub)
                            .tint(palette.accent)
                            .font(.system(size: AppFont.body))
                            .preferredColorScheme(scheme0)
                        #endif
                    }
                }
            }
        }
        .tint(palette.accent)
        #if os(iOS)
        .buttonBorderShape(.capsule)
        #endif
        .font(.system(size: AppFont.body))
        .environment(\.palette, palette)
        .preferredColorScheme(scheme0)
        .sheet(item: $r.sheet) { route in
            SheetHost(route: route)
                .environment(\.palette, palette)
                .environment(router)
                .environment(hub)
                .tint(palette.accent)
                #if os(iOS)
                .buttonBorderShape(.capsule)
                #endif
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
        #if os(macOS)
        switch route {
        case let .connect(scan):
            NavigationStack {
                ConnectView(startScan: scan)
            }
            .frame(minWidth: 480, minHeight: 540)
            .background(p.page)
        case .createBot:
            CreateBotView()
                .frame(width: 520, height: 640)
                .background(p.page)
        case .devices:
            DevicesView()
                .frame(width: 520, height: 600)
                .background(p.page)
        }
        #else
        NavigationStack {
            switch route {
            case let .connect(scan): ConnectView(startScan: scan)
            case .createBot: CreateBotView()
            case .devices: DevicesView()
            }
        }
        #endif
    }
}

/// Sheet 工具栏：左「关闭」，右可选主操作。
struct SheetToolbar: ToolbarContent {
    @Environment(Router.self) private var router
    var actionTitle: String?
    var actionDisabled = false
    var loading = false
    var onClose: (() -> Void)? = nil
    var action: (() -> Void)?

    var body: some ToolbarContent {
        ToolbarItem(placement: .cancellationAction) {
            Button(L.t("close")) { (onClose ?? { router.sheet = nil })() }
        }
        if let actionTitle, let action {
            ToolbarItem(placement: .confirmationAction) {
                if loading { ProgressView().controlSize(.small) } else {
                    Button(actionTitle, action: action).disabled(actionDisabled)
                }
            }
        }
    }
}
