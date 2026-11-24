import SwiftUI

@main
struct BotHubApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @State private var hub = HubStore()

    var body: some Scene {
        WindowGroup {
            RootView().environment(hub)
        }
    }
}

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
        sheet = nil
        desktop = DesktopPresentation(target: resolved.isEmpty ? "local" : resolved, initialTab: .desktop, botId: selected)
    }

    func openFiles(botId: String, path: String? = nil) {
        sheet = nil
        desktop = DesktopPresentation(target: "local", initialTab: .files, botId: botId, initialPath: path)
    }

    func handleStageOpen(_ ev: StageOpenEvent) {
        let targetBot = ev.botId ?? selected
        if let bid = targetBot, !bid.isEmpty, bid != selected {
            openBot(bid)
        }
        let kind = StageKind(rawValue: ev.kind) ?? .files
        sheet = nil
        desktop = DesktopPresentation(target: "local", initialTab: kind, botId: targetBot, initialPath: ev.path)
    }
}

struct RootView: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.colorScheme) private var scheme
    @Environment(\.horizontalSizeClass) private var hsc
    @Environment(\.scenePhase) private var phase
    @State private var router = Router()

    private var scheme0: ColorScheme? { hub.themePref == "light" ? .light : hub.themePref == "dark" ? .dark : nil }

    private var showGate: Bool {
        hub.activeServer == nil
            || hub.connState == .unauthorized
            || (hub.connState == .error && hub.bots.isEmpty)
            || (hub.connState == .connecting && hub.bots.isEmpty)
    }

    var body: some View {
        let dark = hub.themePref == "dark" || (hub.themePref == "system" && scheme == .dark)
        let palette = Palette(dark: dark)
        let compact = hsc == .compact
        @Bindable var r = router
        Group {
            if showGate {
                NavigationStack {
                    ConnectView(startScan: false, isGate: true)
                }
            } else if router.inSettings && !compact {
                NavigationSplitView(preferredCompactColumn: $r.preferredColumn) {
                    SettingsSidebarView(compact: compact)
                        .navigationSplitViewColumnWidth(min: 280, ideal: 320, max: 380)
                } detail: {
                    NavigationStack {
                        SettingsView(tab: router.settingsTab ?? .general)
                    }
                }
                .transition(.opacity)
            } else {
                NavigationSplitView(preferredCompactColumn: $r.preferredColumn) {
                    BotListView(compact: compact)
                        .navigationSplitViewColumnWidth(min: 300, ideal: 340, max: 420)
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
                                .background(palette.page)
                        }
                    }
                    .animation(.easeInOut(duration: 0.2), value: router.selected)
                }
                .transition(.opacity)
                .inspector(isPresented: $r.detailOpen) {
                    if let id = router.selected, hub.bots.contains(where: { $0.id == id }) {
                        NavigationStack {
                            BotDetailView(botId: id, isPresented: $r.detailOpen)
                        }
                        .id(id)
                        .inspectorColumnWidth(min: 340, ideal: 380, max: 460)
                        .overlay(alignment: .leading) {
                            Rectangle()
                                .fill(Color(uiColor: .separator))
                                .frame(width: 0.5)
                                .ignoresSafeArea()
                        }
                        .environment(\.palette, palette)
                        .environment(router)
                        .environment(hub)
                        .tint(palette.accent)
                        .buttonBorderShape(.capsule)
                        .font(.system(size: 16))
                        .preferredColorScheme(scheme0)
                    }
                }
            }
        }
        .tint(palette.accent)
        .buttonBorderShape(.capsule)
        .font(.system(size: 16))
        .environment(\.palette, palette)
        .environment(router)
        .preferredColorScheme(scheme0)
        .sheet(item: $r.sheet) { route in
            SheetHost(route: route)
                .environment(\.palette, palette)
                .environment(router)
                .environment(hub)
                .tint(palette.accent)
                .buttonBorderShape(.capsule)
                .font(.system(size: 16))
                .preferredColorScheme(scheme0)
        }
        .onChange(of: router.selected) { _, id in
            if id == nil { router.detailOpen = false }
        }
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
                if let pendingBotId = BotHubSharedStore.consumePendingInterrupt() {
                    Task { _ = try? await hub.interruptBot(pendingBotId) }
                }
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
            if let q = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems,
               let link = q.first(where: { $0.name == "link" })?.value, !link.isEmpty {
                Task { try? await hub.addAndConnectServer(label: "", address: link, token: "") }
            }
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
    var route: SheetRoute

    var body: some View {
        NavigationStack {
            switch route {
            case let .connect(scan): ConnectView(startScan: scan)
            case .createBot: CreateBotView()
            case .devices: DevicesView()
            }
        }
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
