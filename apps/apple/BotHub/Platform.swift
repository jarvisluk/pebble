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

extension EnvironmentValues {
    var hubCompact: Bool {
        get { self[HubCompactKey.self] }
        set { self[HubCompactKey.self] = newValue }
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

var platformSwitchControlSize: ControlSize {
    #if os(macOS)
    return .small
    #else
    return .regular
    #endif
}

#if os(macOS)
private struct MacSeamlessTitlebarBridge: NSViewRepresentable {
    func makeNSView(context: Context) -> NSView {
        let view = NSView()
        DispatchQueue.main.async { apply(to: view.window) }
        return view
    }

    func updateNSView(_ nsView: NSView, context: Context) {
        DispatchQueue.main.async { apply(to: nsView.window) }
    }

    private func apply(to window: NSWindow?) {
        guard let window else { return }
        if !window.titlebarAppearsTransparent {
            window.titlebarAppearsTransparent = true
        }
        if window.titlebarSeparatorStyle != .none {
            window.titlebarSeparatorStyle = .none
        }
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
}

extension Notification.Name {
    static let hubFocusSearch = Notification.Name("bothub.focusSearch")
    static let hubOpenSettings = Notification.Name("bothub.openSettings")
    static let hubNewBot = Notification.Name("bothub.newBot")
}
