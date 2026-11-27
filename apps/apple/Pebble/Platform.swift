import Observation
import SwiftUI
#if canImport(UIKit)
import UIKit
typealias PlatformImage = UIImage
typealias PlatformColor = UIColor
#elseif canImport(AppKit)
import AppKit
typealias PlatformImage = NSImage
typealias PlatformColor = NSColor
#endif

extension Image {
    init(platform img: PlatformImage) {
        #if canImport(UIKit)
        self.init(uiImage: img)
        #elseif canImport(AppKit)
        self.init(nsImage: img)
        #endif
    }
}

enum AppFont {
    #if os(macOS)
    static let caption: CGFloat = 12
    static let secondary: CGFloat = 13
    static let body: CGFloat = 14
    static let cardTitle: CGFloat = 15
    static let title: CGFloat = 17
    static let hero: CGFloat = 20
    #else
    static let caption: CGFloat = 12
    static let secondary: CGFloat = 13
    static let body: CGFloat = 15
    static let cardTitle: CGFloat = 16
    static let title: CGFloat = 17
    static let hero: CGFloat = 20
    #endif
}

var platformSeparatorColor: Color {
    #if canImport(UIKit)
    return Color(uiColor: .separator)
    #elseif canImport(AppKit)
    return Color(nsColor: .separatorColor)
    #else
    return Color.gray.opacity(0.3)
    #endif
}

func platformColorToHex(_ c: Color) -> String? {
    var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0
    #if canImport(UIKit)
    guard UIColor(c).getRed(&r, green: &g, blue: &b, alpha: &a) else { return nil }
    #elseif canImport(AppKit)
    guard let srgb = NSColor(c).usingColorSpace(.sRGB) else { return nil }
    srgb.getRed(&r, green: &g, blue: &b, alpha: &a)
    #endif
    let ri = max(0, min(255, Int((r * 255).rounded())))
    let gi = max(0, min(255, Int((g * 255).rounded())))
    let bi = max(0, min(255, Int((b * 255).rounded())))
    return String(format: "#%02x%02x%02x", ri, gi, bi)
}

func copyToClipboard(_ s: String) {
    #if canImport(UIKit)
    UIPasteboard.general.string = s
    #elseif canImport(AppKit)
    let pb = NSPasteboard.general
    pb.clearContents()
    pb.setString(s, forType: .string)
    #endif
}

func readClipboardString() -> String {
    #if canImport(UIKit)
    return UIPasteboard.general.string ?? ""
    #elseif canImport(AppKit)
    return NSPasteboard.general.string(forType: .string) ?? ""
    #else
    return ""
    #endif
}

func openExternalURL(_ url: URL) {
    #if canImport(UIKit)
    UIApplication.shared.open(url)
    #elseif canImport(AppKit)
    NSWorkspace.shared.open(url)
    #endif
}

private struct HubCompactKey: EnvironmentKey {
    static let defaultValue = false
}

private struct HubWindowVisibleKey: EnvironmentKey {
    static let defaultValue = true
}

extension EnvironmentValues {
    var hubCompact: Bool {
        get { self[HubCompactKey.self] }
        set { self[HubCompactKey.self] = newValue }
    }

    var hubWindowVisible: Bool {
        get { self[HubWindowVisibleKey.self] }
        set { self[HubWindowVisibleKey.self] = newValue }
    }
}

var platformNotificationSettingsURL: URL? {
    #if os(iOS)
    return URL(string: UIApplication.openSettingsURLString)
    #elseif os(macOS)
    return URL(string: "x-apple.systempreferences:com.apple.Notifications-Settings.extension")
    #else
    return nil
    #endif
}

var platformAppSettingsURL: URL? {
    #if os(iOS)
    return URL(string: UIApplication.openSettingsURLString)
    #elseif os(macOS)
    return URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy")
    #else
    return nil
    #endif
}

var platformSwitchControlSize: ControlSize {
    #if os(macOS)
    return .small
    #else
    return .regular
    #endif
}

var platformNotificationSectionTitleKey: String {
    #if os(iOS)
    return "pushSection"
    #else
    return "pushSectionMac"
    #endif
}

var platformAppVersionString: String {
    let info = Bundle.main.infoDictionary
    let ver = (info?["CFBundleShortVersionString"] as? String)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
    let build = (info?["CFBundleVersion"] as? String)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
    if ver.isEmpty { return "1.0.0" }
    if !build.isEmpty && build != ver { return "\(ver) (\(build))" }
    return ver
}

var platformCurrentShortVersion: String {
    let info = Bundle.main.infoDictionary
    let ver = (info?["CFBundleShortVersionString"] as? String)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
    return ver.isEmpty ? "0.1.0" : ver
}

func isSemVerNewer(_ candidate: String, than current: String) -> Bool {
    func parse(_ raw: String) -> (Int, Int, Int, [String])? {
        var s = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if s.hasPrefix("v") || s.hasPrefix("V") { s.removeFirst() }
        if s.isEmpty { return nil }
        if let plus = s.firstIndex(of: "+") { s = String(s[..<plus]) }
        let core: String
        let pre: [String]
        if let dash = s.firstIndex(of: "-") {
            core = String(s[..<dash])
            let p = String(s[s.index(after: dash)...])
            if p.isEmpty { return nil }
            pre = p.split(separator: ".", omittingEmptySubsequences: false).map(String.init)
            if pre.contains(where: { $0.isEmpty }) { return nil }
        } else {
            core = s
            pre = []
        }
        let nums = core.split(separator: ".", omittingEmptySubsequences: false)
        guard nums.count == 3,
              let maj = Int(nums[0]), let min = Int(nums[1]), let pat = Int(nums[2]),
              maj >= 0, min >= 0, pat >= 0 else { return nil }
        return (maj, min, pat, pre)
    }
    guard let a = parse(candidate), let b = parse(current) else { return false }
    if a.0 != b.0 { return a.0 > b.0 }
    if a.1 != b.1 { return a.1 > b.1 }
    if a.2 != b.2 { return a.2 > b.2 }
    if a.3.isEmpty && b.3.isEmpty { return false }
    if a.3.isEmpty != b.3.isEmpty { return a.3.isEmpty }
    let limit = min(a.3.count, b.3.count)
    for i in 0..<limit {
        let ai = a.3[i], bi = b.3[i]
        if ai == bi { continue }
        let an = Int(ai), bn = Int(bi)
        switch (an, bn) {
        case let (x?, y?): return x > y
        case (_?, nil): return false
        case (nil, _?): return true
        default: return ai > bi
        }
    }
    return a.3.count > b.3.count
}

@MainActor
func platformManagedHostLabel(hub: HubStore) -> String? {
    #if os(macOS)
    guard hub.managedMode else { return nil }
    let label = DesktopHostManager.shared.snapshot.mode == "remote"
        ? hub.t("macModeRemoteTag")
        : hub.t("macModeLocalTag")
    switch hub.connState {
    case .connecting: return "\(label) · \(hub.t("connectingShort"))"
    case .unauthorized: return "\(label) · \(hub.t("needsToken"))"
    case .error: return "\(label) · \(hub.t("connLost"))"
    case .connected: return hub.online ? label : "\(label) · \(hub.t("reconnecting"))"
    case .idle: return hub.t("disconnected")
    }
    #else
    return nil
    #endif
}

struct PlatformNotificationExtrasRows: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p

    var body: some View {
        #if os(iOS)
        let la = LiveActivityManager.shared
        let pm = PushManager.shared
        HStack {
            Text(hub.t("pushLiveActivity"))
            Spacer()
            Text(la.activitiesEnabled ? hub.t("pushEnabled") : hub.t("pushDisabled"))
                .foregroundStyle(la.activitiesEnabled ? p.sageInk : .secondary)
        }
        HStack {
            Text(hub.t("pushServerChannel"))
            Spacer()
            Text(pushModeLabel(pm.serverStatus.mode))
                .foregroundStyle(pm.serverStatus.configured ? p.sageInk : .secondary)
        }
        #else
        EmptyView()
        #endif
    }

    private func pushModeLabel(_ mode: String) -> String {
        switch mode {
        case "direct": return hub.t("pushModeDirect")
        case "relay": return hub.t("pushModeRelay")
        default: return hub.t("pushModeDisabled")
        }
    }
}

struct PlatformDesktopHostModeSection: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p

    var body: some View {
        #if os(macOS)
        let host = DesktopHostManager.shared
        let snap = host.snapshot
        Section(hub.t("macHostSection")) {
            LabeledContent(hub.t("macCurrentMode")) {
                HStack(spacing: 6) {
                    if host.daemonOutdated {
                        Tag(text: hub.t("nodeOutdatedTag"), bg: p.pink, fg: p.pinkInk)
                    }
                    Text(snap.mode == "remote" ? hub.t("macModeRemoteTag") : hub.t("macModeLocalTag"))
                        .foregroundStyle(.secondary)
                }
            }
            if !snap.dataDir.isEmpty {
                LabeledContent(hub.t("macDataDir")) {
                    Text(snap.dataDir)
                        .font(.system(size: AppFont.caption, design: .monospaced))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }
            if snap.mode == "remote" {
                Toggle(hub.t("macAllowNode"), isOn: Binding(
                    get: { snap.allowNode },
                    set: { next in Task { await host.setLocalNode(enabled: next) } }
                ))
                if snap.allowNode {
                    LabeledContent(hub.t("macNodeStatus")) {
                        let statusText = snap.nodeConnected ? hub.t("macNodeConnected")
                            : snap.nodeStatus == "waiting_auth" ? hub.t("macNodeWaiting")
                            : hub.t("macNodeDisconnected")
                        Text(statusText)
                            .foregroundStyle(snap.nodeConnected ? p.sageInk : .secondary)
                    }
                }
                if snap.canFailover {
                    Button(hub.t("macFailover")) {
                        Task { await host.failoverToLocal() }
                    }
                }
            } else if snap.canFailback {
                Button(hub.t("macFailback")) {
                    Task { await host.failbackToRemote() }
                }
            }
            Button(hub.t("macOpenDataDir")) {
                host.openDataDir()
            }
            Button(hub.t("ubPermReopenBtn")) {
                NotificationCenter.default.post(name: .hubOpenUnboxPermissions, object: nil)
            }
            Button(hub.t("macSwitchMode")) {
                Task { await host.resetMode() }
            }
        }
        #else
        EmptyView()
        #endif
    }
}

#if os(macOS)
private final class MacSeamlessTitlebarView: NSView {
    weak var appliedWindow: NSWindow?

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        applyIfNeeded()
    }

    func applyIfNeeded() {
        guard let window, appliedWindow !== window else { return }
        appliedWindow = window
        DispatchQueue.main.async { [weak window] in
            guard let window else { return }
            if !window.titlebarAppearsTransparent {
                window.titlebarAppearsTransparent = true
            }
            if window.titlebarSeparatorStyle != .none {
                window.titlebarSeparatorStyle = .none
            }
        }
    }
}

private struct MacSeamlessTitlebarBridge: NSViewRepresentable {
    func makeNSView(context: Context) -> MacSeamlessTitlebarView {
        MacSeamlessTitlebarView()
    }

    func updateNSView(_ nsView: MacSeamlessTitlebarView, context: Context) {
        nsView.applyIfNeeded()
    }
}

private struct MacSeamlessWindowToolbarModifier: ViewModifier {
    func body(content: Content) -> some View {
        if #available(macOS 15.0, *) {
            content
                .toolbarBackgroundVisibility(.hidden, for: .windowToolbar)
                .background(MacSeamlessTitlebarBridge())
        } else {
            content
                .background(MacSeamlessTitlebarBridge())
        }
    }
}

private struct MacClearInitialFocusBridge: NSViewRepresentable {
    final class Coordinator {
        var didClear = false
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> NSView {
        let view = NSView()
        scheduleClear(for: view, coordinator: context.coordinator)
        return view
    }

    func updateNSView(_ nsView: NSView, context: Context) {
        scheduleClear(for: nsView, coordinator: context.coordinator)
    }

    private func scheduleClear(for view: NSView, coordinator: Coordinator) {
        guard !coordinator.didClear else { return }
        DispatchQueue.main.async { [weak view] in
            guard let window = view?.window, !coordinator.didClear else { return }
            coordinator.didClear = true
            window.makeFirstResponder(nil)
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.05) { [weak window] in
                window?.makeFirstResponder(nil)
            }
        }
    }
}

private final class MacWindowObserverView: NSView {
    var onVisibilityChange: ((Bool) -> Void)?
    private weak var observedWindow: NSWindow?
    private var tokens: [NSObjectProtocol] = []

    deinit {
        removeObservers()
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        bindWindow(window)
        DispatchQueue.main.async { [weak self] in
            self?.evaluateVisibility()
        }
    }

    private func removeObservers() {
        let nc = NotificationCenter.default
        for t in tokens { nc.removeObserver(t) }
        tokens.removeAll()
        observedWindow = nil
    }

    private func bindWindow(_ win: NSWindow?) {
        guard win !== observedWindow || tokens.isEmpty else { return }
        removeObservers()
        observedWindow = win
        let nc = NotificationCenter.default
        tokens.append(nc.addObserver(forName: NSApplication.didHideNotification, object: NSApp, queue: .main) { [weak self] _ in
            self?.evaluateVisibility()
        })
        tokens.append(nc.addObserver(forName: NSApplication.didUnhideNotification, object: NSApp, queue: .main) { [weak self] _ in
            self?.evaluateVisibility()
        })
        guard let win else { return }
        let names: [Notification.Name] = [
            NSWindow.didChangeOcclusionStateNotification,
            NSWindow.didMiniaturizeNotification,
            NSWindow.didDeminiaturizeNotification,
        ]
        for name in names {
            tokens.append(nc.addObserver(forName: name, object: win, queue: .main) { [weak self] _ in
                self?.evaluateVisibility()
            })
        }
        tokens.append(nc.addObserver(forName: NSWindow.willCloseNotification, object: win, queue: .main) { [weak self] _ in
            self?.onVisibilityChange?(false)
        })
    }

    func evaluateVisibility() {
        guard let win = window else { return }
        let isVisible = !(NSApp?.isHidden ?? false)
            && win.isVisible
            && !win.isMiniaturized
            && win.occlusionState.contains(.visible)
        onVisibilityChange?(isVisible)
    }
}

private struct MacWindowVisibilityBridge: NSViewRepresentable {
    @Binding var visible: Bool

    func makeNSView(context: Context) -> MacWindowObserverView {
        let view = MacWindowObserverView()
        view.onVisibilityChange = { next in
            DispatchQueue.main.async {
                if visible != next { visible = next }
            }
        }
        return view
    }

    func updateNSView(_ nsView: MacWindowObserverView, context: Context) {
        nsView.onVisibilityChange = { next in
            DispatchQueue.main.async {
                if visible != next { visible = next }
            }
        }
    }
}

private struct MacWindowVisibilityModifier: ViewModifier {
    @State private var visible = true

    func body(content: Content) -> some View {
        content
            .environment(\.hubWindowVisible, visible)
            .background(MacWindowVisibilityBridge(visible: $visible))
    }
}
#else
private struct IOSWindowVisibilityModifier: ViewModifier {
    @Environment(\.scenePhase) private var scenePhase

    func body(content: Content) -> some View {
        content.environment(\.hubWindowVisible, scenePhase == .active)
    }
}
#endif

extension ToolbarItemPlacement {
    static var hubLeading: ToolbarItemPlacement {
        #if os(iOS)
        return .topBarLeading
        #else
        return .navigation
        #endif
    }

    static var hubTrailing: ToolbarItemPlacement {
        #if os(iOS)
        return .topBarTrailing
        #else
        return .primaryAction
        #endif
    }
}

struct HubSplitLeadingToolbar<First: View, Second: View>: ToolbarContent {
    @ViewBuilder var first: () -> First
    @ViewBuilder var second: () -> Second

    var body: some ToolbarContent {
        #if os(iOS)
        ToolbarItem(placement: .topBarLeading) {
            first()
        }
        if #available(iOS 26.0, *) {
            ToolbarSpacer(.fixed, placement: .topBarLeading)
        }
        ToolbarItem(placement: .topBarLeading) {
            second()
        }
        #elseif os(macOS)
        // 与右侧工具栏按钮一致：使用系统工具栏玻璃样式，不自定义 `.glass` 圆形按钮。
        ToolbarItem(placement: .navigation) {
            first()
        }
        if #available(macOS 26.0, *) {
            ToolbarSpacer(.fixed, placement: .navigation)
        }
        ToolbarItem(placement: .navigation) {
            second()
        }
        #endif
    }
}

extension View {
    @ViewBuilder
    func inlineNavTitle() -> some View {
        #if os(iOS)
        self.navigationBarTitleDisplayMode(.inline)
        #else
        self
        #endif
    }

    @ViewBuilder
    func noAutocap() -> some View {
        #if os(iOS)
        self.textInputAutocapitalization(.never)
        #else
        self
        #endif
    }

    @ViewBuilder
    func numberPadKeyboard() -> some View {
        #if os(iOS)
        self.keyboardType(.numberPad)
        #else
        self
        #endif
    }

    @ViewBuilder
    func hubListSectionSpacing(_ spacing: CGFloat) -> some View {
        #if os(iOS)
        self.listSectionSpacing(spacing)
        #else
        self
        #endif
    }

    @ViewBuilder
    func glassRowContainer(spacing: CGFloat = 10) -> some View {
        #if os(iOS)
        if #available(iOS 26.0, *) {
            GlassEffectContainer(spacing: spacing) {
                self
            }
        } else {
            self
        }
        #else
        self
        #endif
    }

    @ViewBuilder
    func macSeamlessWindowToolbar() -> some View {
        #if os(macOS)
        modifier(MacSeamlessWindowToolbarModifier())
        #else
        self
        #endif
    }

    @ViewBuilder
    func hubBorderlessMenu() -> some View {
        #if os(macOS)
        self.menuStyle(.borderlessButton)
        #else
        self
        #endif
    }

    @ViewBuilder
    func hubSheetFrame(width: CGFloat, height: CGFloat) -> some View {
        #if os(macOS)
        self.frame(width: width, height: height)
        #else
        self
        #endif
    }

    @ViewBuilder
    func hubDropFileURLs(_ onDrop: @escaping ([URL]) -> Void) -> some View {
        #if os(macOS)
        self.onDrop(of: [.fileURL], isTargeted: nil) { providers in
            for provider in providers {
                _ = provider.loadObject(ofClass: URL.self) { url, _ in
                    guard let url else { return }
                    Task { @MainActor in
                        onDrop([url])
                    }
                }
            }
            return !providers.isEmpty
        }
        #else
        self
        #endif
    }

    @ViewBuilder
    func hubSubmitOnReturn(disabled: Bool = false, action: @escaping () -> Void) -> some View {
        #if os(macOS)
        self.onKeyPress(.return, phases: .down) { press in
            if press.modifiers.contains(.shift) {
                return .ignored
            }
            if !disabled {
                action()
            }
            return .handled
        }
        #else
        self
        #endif
    }

    @ViewBuilder
    func hubClearInitialFocus() -> some View {
        #if os(macOS)
        self.background(MacClearInitialFocusBridge())
        #else
        self
        #endif
    }

    @ViewBuilder
    func hubTrackWindowVisibility() -> some View {
        #if os(macOS)
        modifier(MacWindowVisibilityModifier())
        #else
        modifier(IOSWindowVisibilityModifier())
        #endif
    }

    /// iOS/iPadOS 使用原生 `.inspector`；macOS 上避开 SystemSplitView+NavigationSplitView 的两阶段约束崩溃循环。
    @ViewBuilder
    func hubSplitInspector<Panel: View>(isPresented: Binding<Bool>, @ViewBuilder panel: @escaping () -> Panel) -> some View {
        #if os(macOS)
        self
        #else
        self.inspector(isPresented: isPresented, content: panel)
        #endif
    }

    /// macOS 在主对话区域右侧嵌入纯 SwiftUI 侧边工作面板，替代系统 `.inspector`。
    @ViewBuilder
    func hubMacTrailingPanel<Panel: View>(isPresented: Bool, width: CGFloat = 360, @ViewBuilder panel: @escaping () -> Panel) -> some View {
        #if os(macOS)
        HStack(spacing: 0) {
            self.frame(maxWidth: .infinity, maxHeight: .infinity)
            if isPresented {
                panel()
                    .frame(width: width)
                    .frame(maxHeight: .infinity)
                    .overlay(alignment: .leading) {
                        Rectangle()
                            .fill(platformSeparatorColor)
                            .frame(width: 0.5)
                            .ignoresSafeArea()
                    }
                    .transition(.move(edge: .trailing).combined(with: .opacity))
            }
        }
        .animation(.easeInOut(duration: 0.2), value: isPresented)
        #else
        self
        #endif
    }
}

func normalizeThemePref(_ raw: String?) -> String {
    switch raw {
    case "light", "dark": return raw!
    default: return "system"
    }
}

@MainActor
@Observable
final class PlatformAppearance {
    static let shared = PlatformAppearance()

    private(set) var systemDark: Bool

    #if os(macOS)
    @ObservationIgnored private var appearanceObservation: NSKeyValueObservation?
    @ObservationIgnored private var themeNotificationToken: NSObjectProtocol?

    private init() {
        systemDark = Self.readSystemDark()
        themeNotificationToken = DistributedNotificationCenter.default().addObserver(
            forName: Notification.Name("AppleInterfaceThemeChangedNotification"),
            object: nil,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor [weak self] in
                guard let self else { return }
                let dark = Self.readSystemDark()
                if self.systemDark != dark {
                    self.systemDark = dark
                }
            }
        }
        installAppObserverIfNeeded()
    }

    func installAppObserverIfNeeded() {
        guard appearanceObservation == nil, let app = NSApp else { return }
        appearanceObservation = app.observe(\.effectiveAppearance, options: [.new]) { [weak self] app, _ in
            Task { @MainActor [weak self] in
                guard let self, app.appearance == nil else { return }
                let dark = app.effectiveAppearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
                if self.systemDark != dark {
                    self.systemDark = dark
                }
            }
        }
    }

    func applyNativeAppearance(themePref: String) {
        installAppObserverIfNeeded()
        guard let app = NSApp else { return }
        let desired: NSAppearance?
        switch normalizeThemePref(themePref) {
        case "light": desired = NSAppearance(named: .aqua)
        case "dark": desired = NSAppearance(named: .darkAqua)
        default: desired = nil
        }
        if app.appearance?.name != desired?.name {
            app.appearance = desired
        }
    }

    func apply(themePref: String) {
        applyNativeAppearance(themePref: themePref)
        if normalizeThemePref(themePref) == "system" {
            let dark = Self.readSystemDark()
            if systemDark != dark {
                systemDark = dark
            }
        }
    }

    func updateSystemDark(_ dark: Bool) {
        if systemDark != dark {
            systemDark = dark
        }
    }

    private static func readSystemDark() -> Bool {
        if let app = NSApp, app.appearance == nil {
            return app.effectiveAppearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
        }
        return UserDefaults.standard.string(forKey: "AppleInterfaceStyle") == "Dark"
    }
    #elseif os(iOS)
    private init() {
        systemDark = Self.readSystemDark()
    }

    func applyNativeAppearance(themePref: String, window: UIWindow? = nil) {
        let style: UIUserInterfaceStyle
        switch normalizeThemePref(themePref) {
        case "light": style = .light
        case "dark": style = .dark
        default: style = .unspecified
        }
        if let window, window.overrideUserInterfaceStyle != style {
            window.overrideUserInterfaceStyle = style
        }
        for scene in UIApplication.shared.connectedScenes {
            guard let windowScene = scene as? UIWindowScene else { continue }
            for win in windowScene.windows where win.overrideUserInterfaceStyle != style {
                win.overrideUserInterfaceStyle = style
            }
        }
    }

    func apply(themePref: String) {
        applyNativeAppearance(themePref: themePref)
        if normalizeThemePref(themePref) == "system" {
            let dark = Self.readSystemDark()
            if systemDark != dark {
                systemDark = dark
            }
        }
    }

    func syncSystemDark(from window: UIWindow? = nil) {
        let dark = Self.readSystemDark(preferredWindow: window)
        if systemDark != dark {
            systemDark = dark
        }
    }

    private static func readSystemDark(preferredWindow: UIWindow? = nil) -> Bool {
        if let style = preferredWindow?.windowScene?.traitCollection.userInterfaceStyle, style != .unspecified {
            return style == .dark
        }
        for scene in UIApplication.shared.connectedScenes {
            guard let windowScene = scene as? UIWindowScene else { continue }
            let style = windowScene.traitCollection.userInterfaceStyle
            if style != .unspecified {
                return style == .dark
            }
        }
        return UITraitCollection.current.userInterfaceStyle == .dark
    }
    #else
    private init() {
        systemDark = false
    }

    func apply(themePref: String) {}
    #endif

    func resolvedDark(themePref: String, forceDark: Bool = false) -> Bool {
        if forceDark { return true }
        switch normalizeThemePref(themePref) {
        case "dark": return true
        case "light": return false
        default: return systemDark
        }
    }
}

#if os(macOS)
private final class MacThemeObserverView: NSView {
    var themePref: String = "system"
    var forceDark: Bool = false

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        apply(themePref: themePref, forceDark: forceDark)
        syncSystemDarkIfFollowing()
    }

    override func viewDidChangeEffectiveAppearance() {
        super.viewDidChangeEffectiveAppearance()
        syncSystemDarkIfFollowing()
    }

    func apply(themePref: String, forceDark: Bool) {
        self.themePref = themePref
        self.forceDark = forceDark
        PlatformAppearance.shared.applyNativeAppearance(themePref: themePref)
        if let win = window {
            let desired: NSAppearance? = forceDark ? NSAppearance(named: .darkAqua) : nil
            if win.appearance?.name != desired?.name {
                win.appearance = desired
            }
        }
        syncSystemDarkIfFollowing()
    }

    private func syncSystemDarkIfFollowing() {
        guard !forceDark,
              normalizeThemePref(themePref) == "system",
              NSApp?.appearance == nil,
              window?.appearance == nil else { return }
        let dark = effectiveAppearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
        DispatchQueue.main.async {
            PlatformAppearance.shared.updateSystemDark(dark)
        }
    }
}

private struct MacThemeObserverBridge: NSViewRepresentable {
    var themePref: String
    var forceDark: Bool

    func makeNSView(context: Context) -> MacThemeObserverView {
        let view = MacThemeObserverView()
        view.apply(themePref: themePref, forceDark: forceDark)
        return view
    }

    func updateNSView(_ nsView: MacThemeObserverView, context: Context) {
        nsView.apply(themePref: themePref, forceDark: forceDark)
    }
}
#elseif os(iOS)
private final class IOSThemeObserverView: UIView {
    var themePref: String = "system"
    private var traitRegistration: (any UITraitChangeRegistration)?

    override init(frame: CGRect) {
        super.init(frame: frame)
        isHidden = true
        isUserInteractionEnabled = false
        traitRegistration = registerForTraitChanges([UITraitUserInterfaceStyle.self]) { (view: IOSThemeObserverView, _) in
            DispatchQueue.main.async { [weak view] in
                PlatformAppearance.shared.syncSystemDark(from: view?.window)
            }
        }
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    override func didMoveToWindow() {
        super.didMoveToWindow()
        PlatformAppearance.shared.applyNativeAppearance(themePref: themePref, window: window)
        DispatchQueue.main.async { [weak self] in
            PlatformAppearance.shared.syncSystemDark(from: self?.window)
        }
    }

    func apply(themePref: String) {
        self.themePref = themePref
        PlatformAppearance.shared.applyNativeAppearance(themePref: themePref, window: window)
    }
}

private struct IOSThemeObserverBridge: UIViewRepresentable {
    var themePref: String

    func makeUIView(context: Context) -> IOSThemeObserverView {
        let view = IOSThemeObserverView()
        view.apply(themePref: themePref)
        return view
    }

    func updateUIView(_ uiView: IOSThemeObserverView, context: Context) {
        uiView.apply(themePref: themePref)
    }
}
#endif

extension View {
    @ViewBuilder
    func hubApplyTheme(_ themePref: String, forceDark: Bool = false) -> some View {
        #if os(macOS)
        self.background(MacThemeObserverBridge(themePref: themePref, forceDark: forceDark))
        #elseif os(iOS)
        self.background(IOSThemeObserverBridge(themePref: themePref))
        #else
        self
        #endif
    }
}

extension Notification.Name {
    static let hubFocusSearch = Notification.Name("pebble.focusSearch")
    static let hubOpenSettings = Notification.Name("pebble.openSettings")
    static let hubNewBot = Notification.Name("pebble.newBot")
    static let hubOpenUnboxPermissions = Notification.Name("pebble.openUnboxPermissions")
}
