import SwiftUI

@main
struct BotHubApp: App {
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
    case settings
    case devices

    var id: String {
        switch self {
        case let .connect(scan): return "connect-\(scan)"
        case .createBot: return "create"
        case .settings: return "settings"
        case .devices: return "devices"
        }
    }
}

struct DesktopPresentation: Identifiable, Equatable {
    let id = UUID()
    var target: String
}

@MainActor
@Observable
final class Router {
    var selected: String?
    var preferredColumn: NavigationSplitViewColumn = .sidebar
    var sheet: SheetRoute?
    var detailOpen = false
    var desktop: DesktopPresentation?

    func openBot(_ id: String) {
        selected = id
        preferredColumn = .detail
    }

    func closeBot() {
        detailOpen = false
        selected = nil
        preferredColumn = .sidebar
    }

    func openDesktop(_ target: String = "local") {
        let resolved = target.trimmingCharacters(in: .whitespacesAndNewlines)
        sheet = nil
        desktop = DesktopPresentation(target: resolved.isEmpty ? "local" : resolved)
    }
}

struct RootView: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.colorScheme) private var scheme
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
        @Bindable var r = router
        Group {
            if showGate {
                NavigationStack {
                    ConnectView(startScan: false, isGate: true)
                }
            } else {
                NavigationSplitView(preferredCompactColumn: $r.preferredColumn) {
                    BotListView()
                        .navigationSplitViewColumnWidth(min: 300, ideal: 340, max: 420)
                } detail: {
                    NavigationStack {
                        if let id = router.selected, hub.bots.contains(where: { $0.id == id }) {
                            ChatView(botId: id).id(id)
                        } else {
                            ContentUnavailableView(hub.isConnected ? hub.t("selectBotHint") : hub.t("connectHost"),
                                                   systemImage: "bubble.left.and.bubble.right")
                                .frame(maxWidth: .infinity, maxHeight: .infinity)
                                .background(palette.page)
                        }
                    }
                }
                .inspector(isPresented: $r.detailOpen) {
                    if let id = router.selected, hub.bots.contains(where: { $0.id == id }) {
                        NavigationStack {
                            BotDetailView(botId: id, isPresented: $r.detailOpen)
                        }
                        .id(id)
                        .inspectorColumnWidth(min: 340, ideal: 380, max: 460)
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
            DesktopView(initialTarget: pres.target, initialBotId: router.selected)
                .environment(\.palette, palette)
                .environment(router)
                .environment(hub)
                .tint(palette.accent)
                .buttonBorderShape(.capsule)
                .font(.system(size: 16))
                .preferredColorScheme(scheme0)
        }
        .onChange(of: showGate) { _, gate in
            if gate {
                router.sheet = nil
                router.desktop = nil
                router.closeBot()
            }
        }
        .onChange(of: phase) { old, new in
            if new == .active, old == .background { Task { await hub.resume() } }
        }
        .onOpenURL { url in
            // bothub://connect?link=<tailcat 或 http 链接>
            if let q = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems,
               let link = q.first(where: { $0.name == "link" })?.value, !link.isEmpty {
                Task { try? await hub.addAndConnectServer(label: "", address: link, token: "") }
            }
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
            case .settings: SettingsView()
            case .devices: DevicesView()
            }
        }
        .wideSheet(route.isWide)
    }
}

extension View {
    /// iPad 上设置与远程桌面用更大的页面尺寸，其余表单保持默认大小。
    @ViewBuilder func wideSheet(_ wide: Bool) -> some View {
        if wide, #available(iOS 18.0, *) { presentationSizing(.page) } else { self }
    }
}

extension SheetRoute {
    var isWide: Bool {
        if case .settings = self { return true }
        return false
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
